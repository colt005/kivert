package enrich

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/colt005/kivert/internal/alert"
	"github.com/colt005/kivert/internal/config"
	"github.com/colt005/kivert/internal/metrics"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// LogEnricher handles fetching and enriching alerts with pod logs.
type LogEnricher struct {
	clientset kubernetes.Interface
	cfg       *config.Config
}

// NewLogEnricher creates a new LogEnricher.
func NewLogEnricher(clientset kubernetes.Interface, cfg *config.Config) *LogEnricher {
	return &LogEnricher{
		clientset: clientset,
		cfg:       cfg,
	}
}

// Enrich best-effort fetches logs for the given pod container, redacts them, and attaches them to the alert.
func (e *LogEnricher) Enrich(ctx context.Context, a *alert.Alert) {
	if !e.cfg.Logs.Enabled || !e.cfg.Logs.IncludeInAlert {
		return
	}

	logger := log.FromContext(ctx).WithValues("namespace", a.Namespace, "pod", a.Pod, "container", a.Container)

	var logContent string
	var truncated bool
	var err error

	// 1. Try to fetch previous logs if enabled
	if e.cfg.Logs.Previous {
		logContent, truncated, err = e.fetchLogs(ctx, a.Namespace, a.Pod, a.Container, true)
		if err != nil {
			logger.V(1).Info("failed to fetch previous logs, falling back to current logs", "error", err.Error())
			// Fallback to current logs
			logContent, truncated, err = e.fetchLogs(ctx, a.Namespace, a.Pod, a.Container, false)
		}
	} else {
		logContent, truncated, err = e.fetchLogs(ctx, a.Namespace, a.Pod, a.Container, false)
	}

	if err != nil {
		metrics.LogFetchFailures.Inc()
		logger.Error(err, "failed to fetch logs for alert enrichment")
		return
	}

	// 2. Redact logs before attaching
	redactedContent := Redact(logContent, e.cfg.Logs.RedactPatterns)

	a.Logs = redactedContent
	a.LogsTruncated = truncated
}

func (e *LogEnricher) fetchLogs(ctx context.Context, namespace, podName, containerName string, previous bool) (string, bool, error) {
	tailLines := e.cfg.Logs.TailLines
	limitBytes := e.cfg.Logs.LimitBytes

	opts := &corev1.PodLogOptions{
		Container:  containerName,
		Previous:   previous,
		TailLines:  &tailLines,
		LimitBytes: &limitBytes,
	}

	req := e.clientset.CoreV1().Pods(namespace).GetLogs(podName, opts)

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(e.cfg.Logs.TimeoutSeconds)*time.Second)
	defer cancel()

	stream, err := req.Stream(timeoutCtx)
	if err != nil {
		return "", false, fmt.Errorf("failed to open log stream (previous=%t): %w", previous, err)
	}
	defer stream.Close()

	// Read limitBytes + 1 to check for truncation
	data, err := io.ReadAll(io.LimitReader(stream, limitBytes+1))
	if err != nil {
		return "", false, fmt.Errorf("failed to read log stream: %w", err)
	}

	truncated := false
	if int64(len(data)) > limitBytes {
		truncated = true
		data = data[:limitBytes]
	}

	return string(data), truncated, nil
}
