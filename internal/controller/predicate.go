package controller

import (
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/colt005/kivert/internal/detector"
)

// PodRestartPredicate filters events before enqueuing them to the reconciler queue.
type PodRestartPredicate struct {
	predicate.Funcs
	baselineStore *detector.BaselineStore
}

// NewPodRestartPredicate returns a PodRestartPredicate.
func NewPodRestartPredicate(store *detector.BaselineStore) *PodRestartPredicate {
	return &PodRestartPredicate{
		baselineStore: store,
	}
}

// Create returns true to allow new Pod events to be processed and their baseline seeded.
func (p *PodRestartPredicate) Create(e event.CreateEvent) bool {
	return true
}

// Delete cleans up the pod's entries from the baseline store and returns false
// to prevent enqueuing redundant reconcile tasks for deleted pods.
func (p *PodRestartPredicate) Delete(e event.DeleteEvent) bool {
	if pod, ok := e.Object.(*corev1.Pod); ok {
		p.baselineStore.DeletePod(pod.UID)
	}
	return false
}

// Update returns true only if the restart count of any container has increased.
func (p *PodRestartPredicate) Update(e event.UpdateEvent) bool {
	oldPod, okOld := e.ObjectOld.(*corev1.Pod)
	newPod, okNew := e.ObjectNew.(*corev1.Pod)

	if !okOld || !okNew {
		return false
	}

	return restartCountIncreased(oldPod, newPod)
}

// Generic returns false since generic events are not expected.
func (p *PodRestartPredicate) Generic(e event.GenericEvent) bool {
	return false
}

func restartCountIncreased(oldPod, newPod *corev1.Pod) bool {
	// Check standard containers
	for _, newStatus := range newPod.Status.ContainerStatuses {
		oldStatus := findContainerStatus(oldPod.Status.ContainerStatuses, newStatus.Name)
		if newStatus.RestartCount > oldStatus.RestartCount {
			return true
		}
	}

	// Check init containers
	for _, newStatus := range newPod.Status.InitContainerStatuses {
		oldStatus := findContainerStatus(oldPod.Status.InitContainerStatuses, newStatus.Name)
		if newStatus.RestartCount > oldStatus.RestartCount {
			return true
		}
	}

	// Check ephemeral containers
	for _, newStatus := range newPod.Status.EphemeralContainerStatuses {
		oldStatus := findContainerStatus(oldPod.Status.EphemeralContainerStatuses, newStatus.Name)
		if newStatus.RestartCount > oldStatus.RestartCount {
			return true
		}
	}

	return false
}

func findContainerStatus(statuses []corev1.ContainerStatus, name string) corev1.ContainerStatus {
	for _, status := range statuses {
		if status.Name == name {
			return status
		}
	}
	return corev1.ContainerStatus{}
}
