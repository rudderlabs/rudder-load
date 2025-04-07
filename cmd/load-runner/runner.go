package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/rudderlabs/rudder-go-kit/logger"
	obskit "github.com/rudderlabs/rudder-observability-kit/go/labels"

	"rudder-load/internal/metrics"
	"rudder-load/internal/parser"
)

type LoadTestRunner struct {
	config      *parser.LoadTestConfig
	helmClient  HelmClient
	mimirClient metrics.MimirClient
	portForward *metrics.PortForward
	logger      logger.Logger
}

func NewLoadTestRunner(config *parser.LoadTestConfig, helmClient HelmClient, mimirClient metrics.MimirClient, logger logger.Logger) *LoadTestRunner {
	return &LoadTestRunner{
		config:      config,
		helmClient:  helmClient,
		mimirClient: mimirClient,
		portForward: metrics.NewPortForward(time.Second * 5),
		logger:      logger,
	}
}

func (r *LoadTestRunner) Run(ctx context.Context) error {
	if err := r.createValuesFileCopy(ctx); err != nil {
		return err
	}

	if r.config.Reporting.Metrics != nil {
		stopPortForward, err := r.startPortForward(ctx, r.config.Reporting.Namespace)
		if err != nil {
			return err
		}
		defer stopPortForward()

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
	}()

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

		select {
		case <-time.After(duration):
			r.logger.Infon("Phase completed", logger.NewIntField("phase", int64(i+1)), logger.NewStringField("duration", phase.Duration))
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled by user")
		}
	}

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
	if err := r.portForward.Start(ctx, namespace); err != nil {
		return nil, fmt.Errorf("failed to start port-forward: %w", err)
	}
	stopPortForward := func() {
		if err := r.portForward.Stop(); err != nil {
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
		}
	}
}
