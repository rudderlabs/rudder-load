package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rudderlabs/rudder-go-kit/logger"
	obskit "github.com/rudderlabs/rudder-observability-kit/go/labels"

	"rudder-load/internal/metrics"
	"rudder-load/internal/parser"
)

type MetricsRecord struct {
	Timestamp time.Time                 `json:"timestamp"`
	Metrics   []metrics.MetricsResponse `json:"metrics"`
}

type LoadTestRunner struct {
	config        *parser.LoadTestConfig
	helmClient    HelmClient
	mimirClient   metrics.MimirClient
	portForwarder metrics.PortForward
	logger        logger.Logger
	metricsFile   string
	metricsMutex  sync.Mutex
	metricsData   []MetricsRecord
}

func NewLoadTestRunner(config *parser.LoadTestConfig, helmClient HelmClient, mimirClient metrics.MimirClient, portForwarder metrics.PortForward, logger logger.Logger) *LoadTestRunner {
	// Create a metrics file path based on the load test name and timestamp
	metricsFile := fmt.Sprintf("%s_metrics_%s.json", config.Name, time.Now().Format("20060102_150405"))

	return &LoadTestRunner{
		config:        config,
		helmClient:    helmClient,
		mimirClient:   mimirClient,
		portForwarder: portForwarder,
		logger:        logger,
		metricsFile:   metricsFile,
		metricsData:   make([]MetricsRecord, 0),
	}
}

func (r *LoadTestRunner) Run(ctx context.Context) error {
	if err := r.createValuesFileCopy(ctx); err != nil {
		return err
	}

	monitoringNamespace := r.config.Reporting.Namespace
	if monitoringNamespace == "" {
		monitoringNamespace = "mimir"
	}

	stopPortForward, err := r.startPortForward(ctx, monitoringNamespace)
	if err != nil {
		return err
	}
	defer stopPortForward()

	if r.config.Reporting.Metrics != nil {
		monitoringCtx, cancelMonitoring := context.WithCancel(ctx)
		defer cancelMonitoring()
		go r.monitorMetrics(monitoringCtx)
	}

	r.logger.Infon("Installing Helm chart for load scenario", logger.NewStringField("load_scenario", r.config.Name))
	if err := r.helmClient.Install(ctx, r.config); err != nil {
		return err
	}

	defer func() {
		r.logger.Infon("Uninstalling Helm chart for the load scenario...")
		if err := r.helmClient.Uninstall(r.config); err != nil {
			r.logger.Errorn("Failed to uninstall Helm chart: %s", obskit.Error(err))
		}
		r.logger.Infon("Done!")

		// Write metrics to file after test completion
		if err := r.writeMetricsToFile(); err != nil {
			r.logger.Errorn("Failed to write metrics to file: %s", obskit.Error(err))
		}
	}()

	var totalDuration = time.Duration(0)

	for i, phase := range r.config.Phases {
		r.logger.Infon("Running phase", logger.NewIntField("phase", int64(i+1)), logger.NewStringField("duration", phase.Duration))

		if r.config.FromFile {
			if err := r.helmClient.Upgrade(ctx, r.config, phase); err != nil {
				return err
			}
		}

		duration, err := parseDuration(phase.Duration)
		if err != nil {
			return err
		}
		totalDuration += duration

		select {
		case <-time.After(duration):
			r.logger.Infon("Phase completed", logger.NewIntField("phase", int64(i+1)), logger.NewStringField("duration", phase.Duration))
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled by user")
		}
	}

	summaryMetrics, err := r.mimirClient.GetMetrics(ctx, []parser.Metric{
		{Name: "average rps", Query: fmt.Sprintf("sum(avg_over_time(rudder_load_publish_rate_per_second{}[%v]))", totalDuration)},
		{Name: "error rate", Query: fmt.Sprintf("sum(rate(rudder_load_publish_error_rate_total[%v]))", totalDuration)},
	})
	if err != nil {
		r.logger.Errorn("Failed to get current metrics", obskit.Error(err))
	}
	fields := make([]logger.Field, len(summaryMetrics))
	for i, m := range summaryMetrics {
		fields[i] = logger.NewField(m.Key, m.Value)
	}
	r.logger.Infon("Load test summary metrics", fields...)

	// Add summary metrics to the metrics data
	r.recordMetrics(summaryMetrics)

	return nil
}

func parseDuration(d string) (time.Duration, error) {
	duration, err := time.ParseDuration(d)
	if err != nil {
		return 0, err
	}

	if duration <= 0 {
		return 0, fmt.Errorf("duration must be greater than 0")
	}

	return duration, nil
}

func (r *LoadTestRunner) createValuesFileCopy(ctx context.Context) error {
	const (
		valuesFileName = "http_values.yaml"
		valuesFilePerm = 0644
	)

	sourceFile := fmt.Sprintf("%s/%s", r.config.ChartFilePath, valuesFileName)
	copyFile := fmt.Sprintf("%s/%s_values_copy.yaml", r.config.ChartFilePath, r.config.Name)

	content, err := os.ReadFile(sourceFile)
	if err != nil {
		return fmt.Errorf("failed to read source values file %s: %w", sourceFile, err)
	}

	if err := os.WriteFile(copyFile, content, valuesFilePerm); err != nil {
		return fmt.Errorf("failed to write values copy file %s: %w", copyFile, err)
	}

	r.logger.Infon("Successfully created values copy file", logger.NewStringField("file", copyFile))
	return nil
}

func (r *LoadTestRunner) startPortForward(ctx context.Context, namespace string) (func(), error) {
	if err := r.portForwarder.Start(ctx, namespace); err != nil {
		return nil, fmt.Errorf("failed to start port-forward: %w", err)
	}
	stopPortForward := func() {
		if err := r.portForwarder.Stop(); err != nil {
			r.logger.Errorn("Failed to stop port-forward", obskit.Error(err))
		}
	}

	return stopPortForward, nil
}

func (r *LoadTestRunner) monitorMetrics(ctx context.Context) {
	if r.config.Reporting.Interval == "" {
		return
	}

	reportPeriod, err := time.ParseDuration(r.config.Reporting.Interval)
	if err != nil {
		r.logger.Errorn("Invalid monitoring report period, using default of 10s", obskit.Error(err))
		reportPeriod = 10 * time.Second
	}

	ticker := time.NewTicker(reportPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			metrics, err := r.mimirClient.GetMetrics(ctx, r.config.Reporting.Metrics)
			if err != nil {
				r.logger.Errorn("Failed to get current metrics", obskit.Error(err))
				continue
			}
			fields := make([]logger.Field, len(metrics))
			for i, m := range metrics {
				fields[i] = logger.NewField(m.Key, m.Value)
			}
			r.logger.Infon("Load test metrics", fields...)

			// Record metrics for file output
			r.recordMetrics(metrics)
		}
	}
}

// recordMetrics adds a new metrics record to the runner's metrics data
func (r *LoadTestRunner) recordMetrics(metrics []metrics.MetricsResponse) {
	r.metricsMutex.Lock()
	defer r.metricsMutex.Unlock()

	r.metricsData = append(r.metricsData, MetricsRecord{
		Timestamp: time.Now(),
		Metrics:   metrics,
	})
}

func (r *LoadTestRunner) writeMetricsToFile() error {
	r.metricsMutex.Lock()
	defer r.metricsMutex.Unlock()

	if len(r.metricsData) == 0 {
		return nil
	}

	// Create directory if it doesn't exist
	dir := "metrics_reports"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create metrics directory: %w", err)
	}

	filePath := filepath.Join(dir, r.metricsFile)
	data, err := json.MarshalIndent(r.metricsData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metrics data: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metrics file: %w", err)
	}

	r.logger.Infon("Metrics written to file", logger.NewStringField("file", filePath))
	return nil
}
