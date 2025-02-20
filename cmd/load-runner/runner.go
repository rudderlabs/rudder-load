package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rudderlabs/rudder-go-kit/logger"
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
	r.logger.Infof("Installing Helm chart for load scenario: %s", r.config.Name)
	if err := r.helmClient.Install(ctx, r.config); err != nil {
		return err
	}

	defer func() {
		r.logger.Info("Uninstalling Helm chart for the load scenario...")
		if err := r.helmClient.Uninstall(r.config); err != nil {
			r.logger.Errorf("Failed to uninstall Helm chart: %s", err)
		}
		r.logger.Info("Done!")
	}()

	for i, phase := range r.config.Phases {
		r.logger.Infof("Running phase %d for %s", i+1, phase.Duration)

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
			r.logger.Infof("Phase %d completed for %s", i+1, phase.Duration)
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
