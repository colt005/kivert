package detector

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"github.com/colt005/kivert/internal/alert"
	"github.com/colt005/kivert/internal/metrics"
)

// Detector processes Pod update events and detects container restarts.
type Detector struct {
	baselineStore    *BaselineStore
	restartThreshold int32
}

// NewDetector creates a new Detector.
func NewDetector(store *BaselineStore, restartThreshold int32) *Detector {
	return &Detector{
		baselineStore:    store,
		restartThreshold: restartThreshold,
	}
}

// Detect checks if any container in the pod has restarted, updates the baseline,
// and returns a slice of alerts for containers exceeding the threshold.
func (d *Detector) Detect(pod *corev1.Pod) []alert.Alert {
	var alerts []alert.Alert

	// Process standard containers
	for _, status := range pod.Status.ContainerStatuses {
		if a, ok := d.processContainer(pod, status, "container"); ok {
			alerts = append(alerts, a)
		}
	}

	// Process init containers
	for _, status := range pod.Status.InitContainerStatuses {
		if a, ok := d.processContainer(pod, status, "init-container"); ok {
			alerts = append(alerts, a)
		}
	}

	// Process ephemeral containers
	for _, status := range pod.Status.EphemeralContainerStatuses {
		if a, ok := d.processContainer(pod, status, "ephemeral-container"); ok {
			alerts = append(alerts, a)
		}
	}

	return alerts
}

func (d *Detector) processContainer(pod *corev1.Pod, status corev1.ContainerStatus, containerType string) (alert.Alert, bool) {
	uid := pod.UID
	containerName := status.Name
	currentCount := status.RestartCount

	// 1. Look up baseline
	baselineCount, exists := d.baselineStore.Get(uid, containerName)
	if !exists {
		// If it doesn't exist, we seed the baseline with the current count to prevent alert storm.
		d.baselineStore.Set(uid, containerName, currentCount)
		return alert.Alert{}, false
	}

	// 2. Compute delta
	if currentCount <= baselineCount {
		return alert.Alert{}, false
	}

	delta := currentCount - baselineCount

	// Update the baseline store immediately to prevent duplicate alerts
	d.baselineStore.Set(uid, containerName, currentCount)

	// Increment Prometheus observed metric
	metrics.RestartsObserved.Add(float64(delta))

	// 3. Compare with threshold
	if delta < d.restartThreshold {
		return alert.Alert{}, false
	}

	// 4. Build Alert
	exitCode, reason, message := extractTerminationContext(status)
	owner := ResolveOwner(pod)

	a := alert.Alert{
		Namespace:    pod.Namespace,
		Pod:          pod.Name,
		Container:    containerName,
		Node:         pod.Spec.NodeName,
		Image:        status.Image,
		RestartCount: currentCount,
		ExitCode:     exitCode,
		Reason:       reason,
		Owner:        owner,
		Message:      message,
		Timestamp:    time.Now(),
	}

	return a, true
}

// extractTerminationContext retrieves the exit code, reason, and message from the container status.
func extractTerminationContext(status corev1.ContainerStatus) (int32, string, string) {
	// Check the last termination state first, as this is the one that triggered the restart
	if status.LastTerminationState.Terminated != nil {
		t := status.LastTerminationState.Terminated
		reason := t.Reason
		if reason == "" {
			reason = "Unknown"
		}
		// If exit code is 137, it's often OOMKilled but the reason field might say OOMKilled or Error.
		// Let's make sure if the reason is OOMKilled or exit code is 137 and reason is empty, we set it.
		if reason == "OOMKilled" || t.ExitCode == 137 {
			reason = "OOMKilled"
		}
		return t.ExitCode, reason, t.Message
	}

	// Fallback to current state if last termination state is not populated
	if status.State.Terminated != nil {
		t := status.State.Terminated
		reason := t.Reason
		if reason == "" {
			reason = "Unknown"
		}
		if reason == "OOMKilled" || t.ExitCode == 137 {
			reason = "OOMKilled"
		}
		return t.ExitCode, reason, t.Message
	}

	if status.State.Waiting != nil {
		w := status.State.Waiting
		return 0, w.Reason, w.Message
	}

	return 0, "Unknown", ""
}

// ResolveOwner resolves the logical owner of a pod (e.g. Deployment/name) using OwnerReferences.
func ResolveOwner(pod *corev1.Pod) string {
	if len(pod.OwnerReferences) == 0 {
		return ""
	}

	// Find the controller owner reference
	var controllerRef *metav1.OwnerReference
	for _, ref := range pod.OwnerReferences {
		if ref.Controller != nil && *ref.Controller {
			controllerRef = &ref
			break
		}
	}

	if controllerRef == nil {
		// Fallback to the first owner reference if no controller reference is set
		controllerRef = &pod.OwnerReferences[0]
	}

	kind := controllerRef.Kind
	name := controllerRef.Name

	// If the owner is a ReplicaSet, try to resolve to a Deployment using standard naming convention
	if kind == "ReplicaSet" {
		hash := pod.Labels["pod-template-hash"]
		if hash != "" && strings.HasSuffix(name, "-"+hash) {
			deploymentName := strings.TrimSuffix(name, "-"+hash)
			return fmt.Sprintf("Deployment/%s", deploymentName)
		}
		// Fallback if hash doesn't match convention
		if idx := strings.LastIndex(name, "-"); idx != -1 {
			return fmt.Sprintf("Deployment/%s", name[:idx])
		}
	}

	// If the owner is a Job, check if it's owned by a CronJob
	// Since we only have the pod object, we check if the Job name looks like a CronJob execution (e.g., job-12345678)
	// For standard Kubernetes Job, we can just return Job/name.

	return fmt.Sprintf("%s/%s", kind, name)
}
