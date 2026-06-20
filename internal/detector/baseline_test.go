package detector

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestBaselineStore_GetSet(t *testing.T) {
	store := NewBaselineStore()
	uid := types.UID("pod-uid-1")

	// Verify not exists initially
	if _, exists := store.Get(uid, "c1"); exists {
		t.Fatal("expected entry to not exist")
	}

	// Verify set and get
	store.Set(uid, "c1", 5)
	val, exists := store.Get(uid, "c1")
	if !exists {
		t.Fatal("expected entry to exist")
	}
	if val != 5 {
		t.Fatalf("expected restart count to be 5, got %d", val)
	}

	// Verify size
	if store.Size() != 1 {
		t.Fatalf("expected size to be 1, got %d", store.Size())
	}
}

func TestBaselineStore_DeletePod(t *testing.T) {
	store := NewBaselineStore()
	uid1 := types.UID("pod-uid-1")
	uid2 := types.UID("pod-uid-2")

	store.Set(uid1, "c1", 1)
	store.Set(uid1, "c2", 2)
	store.Set(uid2, "c1", 3)

	if store.Size() != 3 {
		t.Fatalf("expected size to be 3, got %d", store.Size())
	}

	// Delete pod 1
	store.DeletePod(uid1)

	if store.Size() != 1 {
		t.Fatalf("expected size to be 1 after deleting pod1, got %d", store.Size())
	}

	if _, exists := store.Get(uid1, "c1"); exists {
		t.Fatal("expected deleted pod entry to be removed")
	}

	val, exists := store.Get(uid2, "c1")
	if !exists || val != 3 {
		t.Fatal("expected other pod entry to remain unaffected")
	}
}

func TestBaselineStore_Seed(t *testing.T) {
	store := NewBaselineStore()

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				UID: types.UID("pod-1"),
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "c1", RestartCount: 2},
				},
				InitContainerStatuses: []corev1.ContainerStatus{
					{Name: "i1", RestartCount: 1},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				UID: types.UID("pod-2"),
			},
			Status: corev1.PodStatus{
				EphemeralContainerStatuses: []corev1.ContainerStatus{
					{Name: "e1", RestartCount: 4},
				},
			},
		},
	}

	store.Seed(pods)

	if store.Size() != 3 {
		t.Fatalf("expected size to be 3, got %d", store.Size())
	}

	val, exists := store.Get("pod-1", "c1")
	if !exists || val != 2 {
		t.Fatalf("expected c1 restart count to be 2, got %d", val)
	}

	val, exists = store.Get("pod-1", "i1")
	if !exists || val != 1 {
		t.Fatalf("expected i1 restart count to be 1, got %d", val)
	}

	val, exists = store.Get("pod-2", "e1")
	if !exists || val != 4 {
		t.Fatalf("expected e1 restart count to be 4, got %d", val)
	}
}
