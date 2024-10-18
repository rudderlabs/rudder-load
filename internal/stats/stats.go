package stats

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	errorLabel = "error"
)

type publisher interface {
	PublishTo(ctx context.Context, key string, message []byte, extra map[string]string) (int, error)
	Close() error
}

type Stats struct {
	p publisher
	f *Factory
}

type Data struct {
	Mode        string
	Concurrency int
	TotalUsers  int
}

type Factory struct {
	reg *prometheus.Registry

	// metrics
	createTopicDurationSeconds *prometheus.HistogramVec
	publishDurationSeconds     *prometheus.HistogramVec
	errorRateTotal             prometheus.Counter
	messagesTotal              prometheus.Counter
	payloadSize                prometheus.Histogram
}

func NewFactory(reg *prometheus.Registry, data Data) (*Factory, error) {
	if reg == nil {
		return nil, fmt.Errorf("prometheus registry is nil")
	}

	constLabels := map[string]string{
		"mode":        data.Mode,
		"concurrency": strconv.Itoa(data.Concurrency),
		"total_users": strconv.Itoa(data.TotalUsers),
	}

	publishDurationSecondsLabels := []string{errorLabel}
	publishDurationSeconds := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "publish_duration_seconds",
		Help:        "Publish duration in seconds",
		Buckets:     []float64{0.0005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		ConstLabels: constLabels,
	}, publishDurationSecondsLabels)
	reg.MustRegister(publishDurationSeconds)

	errorRateTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "publish_error_rate_total",
		Help:        "Total error rate",
		ConstLabels: constLabels,
	})
	reg.MustRegister(errorRateTotal)

	messagesTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "publish_messages_total",
		Help:        "Total messages sent",
		ConstLabels: constLabels,
	})
	reg.MustRegister(messagesTotal)

	payloadSize := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:        "publish_payload_size",
		Help:        "Payload size in bytes",
		Buckets:     []float64{10, 50, 100, 250, 500, 1000, 2000, 3000, 4000, 5000, 10000},
		ConstLabels: constLabels,
	})
	reg.MustRegister(payloadSize)

	return &Factory{
		reg:                    reg,
		publishDurationSeconds: publishDurationSeconds,
		errorRateTotal:         errorRateTotal,
		messagesTotal:          messagesTotal,
		payloadSize:            payloadSize,
	}, nil
}

func (f *Factory) New(p publisher) *Stats {
	return &Stats{
		p: p,
		f: f,
	}
}

func (s *Stats) PublishTo(ctx context.Context, key string, message []byte, extra map[string]string) (int, error) {
	start := time.Now()
	n, err := s.p.PublishTo(ctx, key, message, extra)
	elapsed := time.Since(start).Seconds()

	if errors.Is(err, context.Canceled) {
		return 0, err
	}

	labels := prometheus.Labels{
		errorLabel: "false",
	}
	if err != nil {
		s.f.errorRateTotal.Inc()
		labels["error"] = "true"
	} else {
		s.f.messagesTotal.Inc()
		s.f.payloadSize.Observe(float64(len(message)))
	}
	s.f.publishDurationSeconds.With(labels).Observe(elapsed)

	return n, err
}

func (s *Stats) Close() error {
	return s.p.Close()
}
