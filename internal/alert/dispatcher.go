package alert

import (
	"context"
	"sync"
	"time"

	"github.com/colt005/kivert/internal/config"
	"github.com/colt005/kivert/internal/metrics"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type namedAlerter struct {
	name    string
	alerter Alerter
}

func (n *namedAlerter) Name() string {
	return n.name
}

func (n *namedAlerter) Send(ctx context.Context, a Alert) error {
	return n.alerter.Send(ctx, a)
}

// Dispatcher manages a buffered queue and dispatches alerts to registered channels.
type Dispatcher struct {
	cfg      *config.Config
	channels []Alerter
	queue    chan Alert
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewDispatcher builds a Dispatcher and resolves all configured and enabled channels.
func NewDispatcher(cfg *config.Config) (*Dispatcher, error) {
	var active []Alerter
	for _, chCfg := range cfg.Channels {
		if !chCfg.Enabled {
			continue
		}

		al, err := Build(chCfg.Type, chCfg.Config)
		if err != nil {
			return nil, err
		}

		active = append(active, &namedAlerter{
			name:    chCfg.Name,
			alerter: al,
		})
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Dispatcher{
		cfg:      cfg,
		channels: active,
		queue:    make(chan Alert, 1024),
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// Start spawns the specified number of background worker goroutines to process alerts.
func (d *Dispatcher) Start(workers int) {
	for i := 0; i < workers; i++ {
		d.wg.Add(1)
		go d.worker()
	}
}

// Stop cancels the context, closes the queue, and waits for workers to finish.
func (d *Dispatcher) Stop() {
	d.cancel()
	close(d.queue)
	d.wg.Wait()
}

// Enqueue non-blockingly places an alert onto the dispatch queue.
// If the queue is full, the alert is dropped and an error is logged.
func (d *Dispatcher) Enqueue(a Alert) {
	select {
	case d.queue <- a:
	default:
		logger := log.FromContext(context.Background())
		logger.Error(nil, "alert queue is full, dropping alert", "namespace", a.Namespace, "pod", a.Pod, "container", a.Container)
	}
}

func (d *Dispatcher) worker() {
	defer d.wg.Done()
	for {
		select {
		case a, ok := <-d.queue:
			if !ok {
				return
			}
			d.dispatch(a)
		case <-d.ctx.Done():
			return
		}
	}
}

func (d *Dispatcher) dispatch(a Alert) {
	logger := log.FromContext(context.Background()).WithValues(
		"namespace", a.Namespace,
		"pod", a.Pod,
		"container", a.Container,
	)

	if d.cfg.Alerting.DryRun {
		logger.Info("dryRun is enabled: alert logged but not sent", "alert", a)
		return
	}

	if len(d.channels) == 0 {
		logger.Info("no active alert channels configured")
		return
	}

	var wg sync.WaitGroup
	for _, ch := range d.channels {
		wg.Add(1)
		go func(ch Alerter) {
			defer wg.Done()

			// Bound the dispatch call. We give a generous default of 30 seconds
			// in case the channel doesn't enforce its own timeout.
			ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
			defer cancel()

			err := ch.Send(ctx, a)
			if err != nil {
				metrics.AlertSendFailures.WithLabelValues(ch.Name()).Inc()
				logger.Error(err, "failed to send alert", "channel", ch.Name())
			} else {
				metrics.AlertsSent.WithLabelValues(ch.Name()).Inc()
				logger.Info("successfully sent alert", "channel", ch.Name())
			}
		}(ch)
	}
	wg.Wait()
}
