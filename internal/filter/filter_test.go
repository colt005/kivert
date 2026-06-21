package filter

import (
	"testing"
	"time"

	"github.com/colt005/kivert/internal/alert"
	"github.com/colt005/kivert/internal/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFilter_ShouldAlert_Namespace(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Watch.AllNamespaces = false
	cfg.Watch.Namespaces = []string{"prod", "staging"}
	cfg.Watch.ExcludeNamespaces = []string{"kube-system", "staging"} // staging is both, but exclude takes precedence

	f, err := NewFilter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		namespace string
		expected  bool
	}{
		{"prod", true},
		{"staging", false},     // excluded
		{"kube-system", false}, // excluded
		{"default", false},     // not in include list
	}

	for _, tc := range tests {
		a := alert.Alert{Namespace: tc.namespace, Pod: "pod1", Container: "c1"}
		if got := f.ShouldAlert(a, nil); got != tc.expected {
			t.Errorf("Namespace %s: expected %t, got %t", tc.namespace, tc.expected, got)
		}
	}
}

func TestFilter_ShouldAlert_LabelSelector(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Watch.AllNamespaces = true
	cfg.Watch.LabelSelector = "alert-on-restart=true"

	f, err := NewFilter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	podWithLabel := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"alert-on-restart": "true"},
		},
	}

	podWithoutLabel := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"alert-on-restart": "false"},
		},
	}

	a := alert.Alert{Namespace: "default", Pod: "pod1", Container: "c1"}

	if !f.ShouldAlert(a, podWithLabel) {
		t.Error("expected to alert on pod matching label selector")
	}

	if f.ShouldAlert(a, podWithoutLabel) {
		t.Error("expected to skip pod not matching label selector")
	}
}

func TestFilter_ShouldAlert_Reason(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Watch.AllNamespaces = true
	cfg.Alerting.IncludeReasons = []string{"OOMKilled", "CrashLoopBackOff"}
	cfg.Alerting.CooldownSeconds = 0 // Disable cooldown to allow multiple checks on same container

	f, err := NewFilter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		reason   string
		expected bool
	}{
		{"OOMKilled", true},
		{"CrashLoopBackOff", true},
		{"Error", false},
		{"Unknown", false},
	}

	for _, tc := range tests {
		a := alert.Alert{Namespace: "default", Pod: "pod1", Container: "c1", Reason: tc.reason}
		if got := f.ShouldAlert(a, nil); got != tc.expected {
			t.Errorf("Reason %s: expected %t, got %t", tc.reason, tc.expected, got)
		}
	}
}

func TestFilter_ShouldAlert_Cooldown(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Watch.AllNamespaces = true
	cfg.Alerting.CooldownSeconds = 1 // 1 second cooldown

	f, err := NewFilter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	a1 := alert.Alert{Namespace: "ns1", Pod: "pod1", Container: "c1"}
	a2 := alert.Alert{Namespace: "ns1", Pod: "pod1", Container: "c2"} // different container

	// 1. Initial alert for c1 -> should succeed
	if !f.ShouldAlert(a1, nil) {
		t.Error("expected first alert to pass")
	}

	// 2. Immediate duplicate alert for c1 -> should fail due to cooldown
	if f.ShouldAlert(a1, nil) {
		t.Error("expected second alert within cooldown window to be filtered")
	}

	// 3. Immediate alert for c2 -> should succeed because it's a different container
	if !f.ShouldAlert(a2, nil) {
		t.Error("expected alert for different container to pass")
	}

	// 4. Wait for cooldown to expire
	time.Sleep(1100 * time.Millisecond)

	// 5. Alert for c1 again -> should succeed
	if !f.ShouldAlert(a1, nil) {
		t.Error("expected alert to pass after cooldown expired")
	}
}
