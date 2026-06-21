package filter

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/colt005/kivert/internal/alert"
	"github.com/colt005/kivert/internal/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type cooldownKey struct {
	namespace string
	pod       string
	container string
}

// Filter evaluates whether an Alert should be sent based on namespace policies,
// label selectors, reason filters, and cooldown periods.
type Filter struct {
	cfg       *config.Config
	selector  labels.Selector
	mu        sync.Mutex
	cooldowns map[cooldownKey]time.Time
	gcCounter int
}

// NewFilter creates and initializes a Filter.
func NewFilter(cfg *config.Config) (*Filter, error) {
	var selector labels.Selector = labels.Everything()
	if cfg.Watch.LabelSelector != "" {
		var err error
		selector, err = labels.Parse(cfg.Watch.LabelSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector %q: %w", cfg.Watch.LabelSelector, err)
		}
	}

	return &Filter{
		cfg:       cfg,
		selector:  selector,
		cooldowns: make(map[cooldownKey]time.Time),
	}, nil
}

// ShouldAlert evaluates the alert against configured rules.
// If it returns true, the alert is passed on and the cooldown timestamp is updated.
func (f *Filter) ShouldAlert(a alert.Alert, pod *corev1.Pod) bool {
	// 1. Exclude namespaces first
	for _, ns := range f.cfg.Watch.ExcludeNamespaces {
		if a.Namespace == ns {
			return false
		}
	}

	// 2. Include namespaces check (only if not watching all)
	if !f.cfg.Watch.AllNamespaces && len(f.cfg.Watch.Namespaces) > 0 {
		allowed := false
		for _, ns := range f.cfg.Watch.Namespaces {
			if a.Namespace == ns {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	// 3. Label Selector check
	if pod != nil {
		if !f.selector.Matches(labels.Set(pod.Labels)) {
			return false
		}
	}

	// 4. Reason filter check
	if len(f.cfg.Alerting.IncludeReasons) > 0 {
		included := false
		for _, r := range f.cfg.Alerting.IncludeReasons {
			if strings.EqualFold(a.Reason, r) {
				included = true
				break
			}
		}
		if !included {
			return false
		}
	}

	// 5. Cooldown check
	f.mu.Lock()
	defer f.mu.Unlock()

	key := cooldownKey{
		namespace: a.Namespace,
		pod:       a.Pod,
		container: a.Container,
	}

	cooldownDuration := time.Duration(f.cfg.Alerting.CooldownSeconds) * time.Second
	now := time.Now()

	if lastTime, exists := f.cooldowns[key]; exists {
		if now.Sub(lastTime) < cooldownDuration {
			return false
		}
	}

	// Update the cooldown timestamp
	f.cooldowns[key] = now

	// Run periodic garbage collection of old cooldown records to prevent memory leak
	f.gcCounter++
	if f.gcCounter >= 100 {
		f.gcCounter = 0
		for k, t := range f.cooldowns {
			if now.Sub(t) >= cooldownDuration {
				delete(f.cooldowns, k)
			}
		}
	}

	return true
}
