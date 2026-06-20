package detector

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"github.com/colt005/kivert/internal/metrics"
)

type baselineKey struct {
	uid       types.UID
	container string
}

// BaselineStore manages a thread-safe store of container restart counts.
type BaselineStore struct {
	mu    sync.RWMutex
	store map[baselineKey]int32
}

// NewBaselineStore returns an initialized BaselineStore.
func NewBaselineStore() *BaselineStore {
	return &BaselineStore{
		store: make(map[baselineKey]int32),
	}
}

// Get retrieves the last-seen restart count for a container.
// Returns the count and true if it exists, otherwise 0 and false.
func (s *BaselineStore) Get(uid types.UID, container string) (int32, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count, exists := s.store[baselineKey{uid: uid, container: container}]
	return count, exists
}

// Set stores the last-seen restart count for a container.
func (s *BaselineStore) Set(uid types.UID, container string, count int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store[baselineKey{uid: uid, container: container}] = count
	metrics.BaselineStoreSize.Set(float64(len(s.store)))
}

// DeletePod removes all container entries associated with a pod UID.
func (s *BaselineStore) DeletePod(uid types.UID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.store {
		if k.uid == uid {
			delete(s.store, k)
		}
	}
	metrics.BaselineStoreSize.Set(float64(len(s.store)))
}

// Seed populates the baseline store with the current restart counts of the provided pods.
// Seeding does not generate alerts.
func (s *BaselineStore) Seed(pods []corev1.Pod) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, pod := range pods {
		s.seedPodNoLock(pod)
	}
	metrics.BaselineStoreSize.Set(float64(len(s.store)))
}

// seedPodNoLock populates baseline for a pod assuming mutex is already held.
func (s *BaselineStore) seedPodNoLock(pod corev1.Pod) {
	uid := pod.UID
	
	// Seed standard containers
	for _, status := range pod.Status.ContainerStatuses {
		s.store[baselineKey{uid: uid, container: status.Name}] = status.RestartCount
	}

	// Seed init containers
	for _, status := range pod.Status.InitContainerStatuses {
		s.store[baselineKey{uid: uid, container: status.Name}] = status.RestartCount
	}

	// Seed ephemeral containers
	for _, status := range pod.Status.EphemeralContainerStatuses {
		s.store[baselineKey{uid: uid, container: status.Name}] = status.RestartCount
	}
}

// Size returns the number of tracked entries.
func (s *BaselineStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.store)
}
