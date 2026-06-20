package detector

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestDetector_Detect(t *testing.T) {
	store := NewBaselineStore()
	d := NewDetector(store, 1) // threshold of 1

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-ns",
			UID:       types.UID("pod-uid-1"),
			OwnerReferences: []metav1.OwnerReference{
				{
					Controller: boolPtr(true),
					Kind:       "ReplicaSet",
					Name:       "test-pod-deploy-5c7ff8b5b",
				},
			},
			Labels: map[string]string{
				"pod-template-hash": "5c7ff8b5b",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					Image:        "nginx:latest",
					RestartCount: 2,
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode: 137,
							Reason:   "OOMKilled",
							Message:  "Limit exceeded",
						},
					},
				},
			},
		},
	}

	// 1. First run: pod is NOT in baseline.
	// It should seed without alerting.
	alerts := d.Detect(pod)
	if len(alerts) != 0 {
		t.Fatalf("expected 0 alerts on first-seen container, got %d", len(alerts))
	}

	// Verify it was indeed seeded
	val, exists := store.Get(pod.UID, "app")
	if !exists || val != 2 {
		t.Fatalf("expected baseline to be seeded to 2, got %d (exists=%t)", val, exists)
	}

	// 2. Second run: restart count has NOT changed.
	// It should not alert.
	alerts = d.Detect(pod)
	if len(alerts) != 0 {
		t.Fatalf("expected 0 alerts when restart count is unchanged, got %d", len(alerts))
	}

	// 3. Third run: restart count goes up to 3 (delta=1, threshold=1).
	// It should alert.
	pod.Status.ContainerStatuses[0].RestartCount = 3
	alerts = d.Detect(pod)
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}

	a := alerts[0]
	if a.Container != "app" || a.RestartCount != 3 || a.ExitCode != 137 || a.Reason != "OOMKilled" {
		t.Fatalf("unexpected alert details: %+v", a)
	}
	if a.Owner != "Deployment/test-pod-deploy" {
		t.Fatalf("expected owner to be resolved to Deployment/test-pod-deploy, got %q", a.Owner)
	}

	// Baseline store should be updated to 3
	val, _ = store.Get(pod.UID, "app")
	if val != 3 {
		t.Fatalf("expected baseline to update to 3, got %d", val)
	}

	// 4. Fourth run: restart count goes up to 4, but let's test a higher threshold
	d2 := NewDetector(store, 2) // threshold of 2
	pod.Status.ContainerStatuses[0].RestartCount = 4 // delta is 1 (4 - 3)
	alerts = d2.Detect(pod)
	// Since delta is 1, and threshold is 2, it should NOT alert, but it SHOULD update the baseline to 4!
	if len(alerts) != 0 {
		t.Fatalf("expected 0 alerts since delta is below threshold, got %d", len(alerts))
	}

	val, _ = store.Get(pod.UID, "app")
	if val != 4 {
		t.Fatalf("expected baseline to update to 4 even if below alert threshold, got %d", val)
	}
}

func boolPtr(b bool) *bool {
	return &b
}
