package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rudderlabs/rudder-go-kit/logger"
	obskit "github.com/rudderlabs/rudder-observability-kit/go/labels"
)

type LoadTestRunner struct {
	config     *LoadTestConfig
	helmClient HelmClient
	logger     logger.Logger
}

func NewLoadTestRunner(config *LoadTestConfig, helmClient HelmClient, logger logger.Logger) *LoadTestRunner {
	return &LoadTestRunner{config: config, helmClient: helmClient, logger: logger}
}

func (r *LoadTestRunner) Run(ctx context.Context) error {
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
