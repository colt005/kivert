package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	// Blank import to register webhook channel
	_ "github.com/colt005/kivert/internal/alert/channels/webhook"

	"github.com/colt005/kivert/internal/alert"
	"github.com/colt005/kivert/internal/config"
	"github.com/colt005/kivert/internal/controller"
	"github.com/colt005/kivert/internal/detector"
	"github.com/colt005/kivert/internal/enrich"
	"github.com/colt005/kivert/internal/filter"
	"github.com/colt005/kivert/internal/k8s"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
}

func main() {
	// 1. Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	// 2. Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Printf("Invalid configuration: %v\n", err)
		os.Exit(1)
	}

	// 3. Setup logging
	opts := zap.Options{
		Development: false,
	}
	// Setup standard zap level based on config
	switch strings.ToLower(cfg.Controller.LogLevel) {
	case "debug":
		opts.Level = zapcore.DebugLevel
	case "info":
		opts.Level = zapcore.InfoLevel
	case "warn":
		opts.Level = zapcore.WarnLevel
	case "error":
		opts.Level = zapcore.ErrorLevel
	default:
		opts.Level = zapcore.InfoLevel
	}

	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)
	setupLog := ctrl.Log.WithName("setup")

	setupLog.Info("Starting Kivert manager", "version", "1.0.0")

	// 4. Build Kubernetes configuration
	restConfig := ctrl.GetConfigOrDie()

	// 5. Create raw clientset (used for seeding and previous container logs fetching)
	clientset, err := k8s.NewClientset(restConfig)
	if err != nil {
		setupLog.Error(err, "unable to create kubernetes clientset")
		os.Exit(1)
	}

	// 6. Initialize baseline store & detector
	baselineStore := detector.NewBaselineStore()
	det := detector.NewDetector(baselineStore, cfg.Alerting.RestartThreshold)

	// 7. Seed baseline store from API server *before* start to prevent boot-storms
	setupLog.Info("Seeding baseline store from API server...")
	ctx := ctrl.SetupSignalHandler()
	if err := seedBaselineStore(ctx, clientset, baselineStore, cfg); err != nil {
		setupLog.Error(err, "failed to seed baseline store")
		os.Exit(1)
	}
	setupLog.Info("Baseline store seeded successfully", "entriesCount", baselineStore.Size())

	// 8. Initialize filters
	flt, err := filter.NewFilter(cfg)
	if err != nil {
		setupLog.Error(err, "unable to create alert filter")
		os.Exit(1)
	}

	// 9. Initialize log enricher
	enricher := enrich.NewLogEnricher(clientset, cfg)

	// 10. Initialize alert dispatcher
	dispatcher, err := alert.NewDispatcher(cfg)
	if err != nil {
		setupLog.Error(err, "unable to create alert dispatcher")
		os.Exit(1)
	}

	// Start dispatcher background workers
	dispatcher.Start(5)
	defer dispatcher.Stop()

	// 11. Configure manager options
	mgrOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: fmt.Sprintf(":%d", cfg.Controller.Metrics.Port),
		},
		HealthProbeBindAddress: ":8081",
		LeaderElection:         cfg.Controller.LeaderElection,
		LeaderElectionID:       "kivert.kivert.io",
		LeaderElectionNamespace: getInstallNamespace(),
	}

	// Disable informer resync period (to keep handler behavior predictable as per spec)
	// controller-runtime uses cache options to set sync periods
	if cfg.Controller.ResyncPeriodSeconds <= 0 {
		mgrOpts.Cache.SyncPeriod = nil
	}

	// Restrict watch scope if allNamespaces is false
	if !cfg.Watch.AllNamespaces {
		namespaces := make(map[string]cache.Config)
		for _, ns := range cfg.Watch.Namespaces {
			namespaces[ns] = cache.Config{}
		}
		mgrOpts.Cache.DefaultNamespaces = namespaces
	}

	mgr, err := ctrl.NewManager(restConfig, mgrOpts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// 12. Register health probes
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// 13. Register Pod reconciler
	podReconciler := &controller.PodReconciler{
		Client:     mgr.GetClient(),
		Clientset:  clientset,
		Config:     cfg,
		Detector:   det,
		Filter:     flt,
		Enricher:   enricher,
		Dispatcher: dispatcher,
	}

	if err := podReconciler.SetupWithManager(mgr, baselineStore); err != nil {
		setupLog.Error(err, "unable to create pod controller")
		os.Exit(1)
	}

	// 14. Start manager
	setupLog.Info("Starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func getInstallNamespace() string {
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}
	// Fallback to serviceaccount namespace file
	if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		return strings.TrimSpace(string(data))
	}
	return "kivert-system" // Default fallback
}

func seedBaselineStore(ctx context.Context, clientset kubernetes.Interface, store *detector.BaselineStore, cfg *config.Config) error {
	var pods []corev1.Pod
	if cfg.Watch.AllNamespaces {
		list, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
			LabelSelector: cfg.Watch.LabelSelector,
		})
		if err != nil {
			return fmt.Errorf("failed to list pods for seeding (all namespaces): %w", err)
		}
		pods = list.Items
	} else {
		for _, ns := range cfg.Watch.Namespaces {
			list, err := clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
				LabelSelector: cfg.Watch.LabelSelector,
			})
			if err != nil {
				return fmt.Errorf("failed to list pods in namespace %s for seeding: %w", ns, err)
			}
			pods = append(pods, list.Items...)
		}
	}

	store.Seed(pods)
	return nil
}
