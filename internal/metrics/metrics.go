package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// RestartsObserved tracks the total number of pod container restarts observed.
	RestartsObserved = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "kivert_restarts_observed_total",
			Help: "Total number of pod container restarts observed by Kivert.",
		},
	)

	// AlertsSent tracks the total number of alerts successfully sent, partitioned by channel name.
	AlertsSent = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kivert_alerts_sent_total",
			Help: "Total number of alerts successfully sent by Kivert.",
		},
		[]string{"channel"},
	)

	// AlertSendFailures tracks the total number of alerts that failed to send, partitioned by channel name.
	AlertSendFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kivert_alert_send_failures_total",
			Help: "Total number of alert send failures.",
		},
		[]string{"channel"},
	)

	// LogFetchFailures tracks the total number of failures encountered when fetching pod logs.
	LogFetchFailures = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "kivert_log_fetch_failures_total",
			Help: "Total number of failures when fetching pod logs.",
		},
	)

	// BaselineStoreSize tracks the number of container entries currently tracked in the baseline store.
	BaselineStoreSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "kivert_baseline_store_size",
			Help: "Total number of entries in the baseline store.",
		},
	)
)

func init() {
	// Register custom metrics with controller-runtime's global registry.
	crmetrics.Registry.MustRegister(
		RestartsObserved,
		AlertsSent,
		AlertSendFailures,
		LogFetchFailures,
		BaselineStoreSize,
	)
}
