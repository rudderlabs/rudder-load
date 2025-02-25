package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/rudderlabs/rudder-go-kit/logger"
	obskit "github.com/rudderlabs/rudder-observability-kit/go/labels"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
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

	cfg, err := ParseLoadTestConfig(args)
	if err != nil {
		return fmt.Errorf("failed to load test config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid inputs: %w", err)
	}

	cfg.SetDefaults()

	helmClient := NewHelmClient(&commandExecutor{})
	runner := NewLoadTestRunner(cfg, helmClient, log)

	if err := runner.Run(ctx); err != nil {
		return fmt.Errorf("failed to run load test: %w", err)
	}

	return nil
}
