package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rudderlabs/rudder-go-kit/logger"
	obskit "github.com/rudderlabs/rudder-observability-kit/go/labels"

	"rudder-load/internal/parser"
	"rudder-load/internal/validator"
	"rudder-load/internal/metrics"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	log := logger.NewLogger()

	if err := run(ctx, log); err != nil {
		log.Errorn("Error running load test", obskit.Error(err))
		os.Exit(1)
	}
}

func run(ctx context.Context, log logger.Logger) error {
	cli := NewCLI(log)

	args, err := cli.ParseFlags()
	if err != nil {
		return fmt.Errorf("invalid options: %w", err)
	}

	cfg, err := parser.ParseLoadTestConfig(args)
	if err != nil {
		return fmt.Errorf("failed to load test config: %w", err)
	}

	if err := validator.ValidateLoadTestConfig(cfg); err != nil {
		return fmt.Errorf("invalid inputs: %w", err)
	}

	err = cfg.SetEnvOverrides()
	if err != nil {
		return fmt.Errorf("failed to set env overrides: %w", err)
	}

	cfg.SetDefaults()

	helmClient := NewHelmClient(&commandExecutor{})
	mimirClient := metrics.NewMimirClient("http://localhost:9898")
	runner := NewLoadTestRunner(cfg, helmClient, mimirClient, log)

	if err := runner.Run(ctx); err != nil {
		return fmt.Errorf("failed to run load test: %w", err)
	}

	return nil
}
