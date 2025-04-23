package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rudderlabs/rudder-go-kit/logger"
	obskit "github.com/rudderlabs/rudder-observability-kit/go/labels"

	"rudder-load/internal/metrics"
	"rudder-load/internal/parser"
	"rudder-load/internal/validator"
)

type commandExecutor interface {
	run(ctx context.Context, name string, args ...string) error
}

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

	err = cfg.SetEnvOverrides()
	if err != nil {
		return fmt.Errorf("failed to set env overrides: %w", err)
	}

	cfg.SetDefaults()

	if err := validator.ValidateLoadTestConfig(cfg); err != nil {
		return fmt.Errorf("invalid inputs: %w", err)
	}

	// Create the appropriate loadTestManager based on the local execution flag
	var loadTestManager loadTestManager
	var metricsClient *metrics.MetricsClient

	if args.LocalExecution {
		log.Infon("Using Docker Compose for local execution")
		loadTestManager = NewDockerComposeClient(&CommandExecutor{}, log)
		// Use local metrics client for local execution
		metricsClient = metrics.NewLocalMetricsClient("http://localhost:9102/metrics")
	} else {
		log.Infon("Using Helm for Kubernetes execution")
		loadTestManager = NewHelmClient(&CommandExecutor{}, log)
		// Use Mimir client for remote execution
		metricsClient = metrics.NewMetricsClient("http://localhost:9898")
	}

	portForwardingTimeoutString := parser.GetEnvOrDefault("PORT_FORWARDING_TIMEOUT", "5s")
	portForwardingTimeout, err := parseDuration(portForwardingTimeoutString)
	if err != nil {
		return fmt.Errorf("failed to parse port forwarding timeout: %w", err)
	}
	portForwarder := metrics.NewPortForwarder(portForwardingTimeout, log)
	runner := NewLoadTestRunner(cfg, loadTestManager, metricsClient, portForwarder, log)
	if err := runner.Run(ctx); err != nil {
		return fmt.Errorf("failed to run load test: %w", err)
	}

	return nil
}
