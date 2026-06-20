package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/colt005/kivert/internal/alert"
	"github.com/colt005/kivert/internal/config"
	"github.com/colt005/kivert/internal/detector"
	"github.com/colt005/kivert/internal/enrich"
	"github.com/colt005/kivert/internal/filter"
)

// PodReconciler reconciles Pod objects to detect container restart events.
type PodReconciler struct {
	client.Client
	Clientset  kubernetes.Interface
	Config     *config.Config
	Detector   *detector.Detector
	Filter     *filter.Filter
	Enricher   *enrich.LogEnricher
	Dispatcher *alert.Dispatcher
}

// Reconcile checks the status of a Pod's containers and generates alerts if restart count increases.
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Pod from the informer cache
	pod := &corev1.Pod{}
	err := r.Get(ctx, req.NamespacedName, pod)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Pod was deleted; cleanup in predicate.go prevents memory leaks
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch Pod")
		return ctrl.Result{}, err
	}

	// 1. Detect restart deltas
	alerts := r.Detector.Detect(pod)
	if len(alerts) == 0 {
		return ctrl.Result{}, nil
	}

	// 2. Process alerts
	for _, a := range alerts {
		// 3. Filter alerts
		if !r.Filter.ShouldAlert(a, pod) {
			logger.V(1).Info("alert filtered out by policies or cooldown", "container", a.Container)
			continue
		}

		// 4. Enrich alert with logs (best-effort)
		r.Enricher.Enrich(ctx, &a)

		// 5. Dispatch alert
		r.Dispatcher.Enqueue(a)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager and applies the PodRestartPredicate.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager, store *detector.BaselineStore) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithEventFilter(NewPodRestartPredicate(store)).
		Complete(r)
}
