package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"time"

	"github.com/rudderlabs/rudder-go-kit/logger"
)

type config struct {
	duration       string
	parsedDuration time.Duration
	namespace      string
	loadName       string
	chartFilesPath string
}

const (
	defaultReleaseNamePrefix = "rudder-load"
	defaultChartFilesPath    = "./artifacts/helm"
)

var log = logger.NewLogger()

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := parseFlags()
	if err != nil {
		return fmt.Errorf("invalid options: %w", err)
	}

	if err := validateInputs(cfg); err != nil {
		return fmt.Errorf("invalid inputs: %w", err)
	}

	duration, err := parseDuration(cfg.duration)
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}
	cfg.parsedDuration = duration

	return runLoadTest(ctx, cfg)
}

func parseFlags() (*config, error) {
	var cfg config
	flag.StringVar(&cfg.duration, "d", "", "Duration to run (e.g., 1h, 30m, 5s)")
	flag.StringVar(&cfg.namespace, "n", "", "Kubernetes namespace")
	flag.StringVar(&cfg.loadName, "l", "", "Load scenario name")
	flag.StringVar(&cfg.chartFilesPath, "f", "", "Path to the chart files (e.g., artifacts/helm)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "\nUsage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s -d 1m -n test -l test-staging    # Run test-staging load for 1 minute on test namespace\n", os.Args[0])
	}

	flag.Parse()

	if cfg.duration == "" || cfg.namespace == "" || cfg.loadName == "" {
		if cfg.duration == "" {
			fmt.Fprintf(os.Stderr, "Error: duration is required\n")
		}
		if cfg.namespace == "" {
			fmt.Fprintf(os.Stderr, "Error: namespace is required\n")
		}
		if cfg.loadName == "" {
			fmt.Fprintf(os.Stderr, "Error: load name is required\n")
		}

		flag.Usage()
		return nil, fmt.Errorf("invalid options")
	}

	return &cfg, nil
}

func validateInputs(cfg *config) error {
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(cfg.namespace) {
		return fmt.Errorf("namespace must contain only lowercase alphanumeric characters and '-'")
	}

	if !regexp.MustCompile(`^[a-zA-Z0-9-]+$`).MatchString(cfg.loadName) {
		return fmt.Errorf("load name must contain only alphanumeric characters and '-'")
	}

	if !regexp.MustCompile(`^(\d+[hms])+$`).MatchString(cfg.duration) {
		return fmt.Errorf("duration must include 'h', 'm', or 's' (e.g., '1h30m')")
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

func runLoadTest(ctx context.Context, cfg *config) error {
	releaseName := fmt.Sprintf("%s-%s", defaultReleaseNamePrefix, cfg.loadName)

	chartFilesPath := cfg.chartFilesPath
	log.Info("Chart path before: %s", chartFilesPath)
	if chartFilesPath == "" {
		chartFilesPath = defaultChartFilesPath
	}
	log.Info("Chart path after: %s", chartFilesPath)

	log.Info("Installing Helm chart for load scenario: %s", cfg.loadName)
	installArgs := []string{
		"install",
		releaseName,
		chartFilesPath,
		"--namespace", cfg.namespace,
		"--set", fmt.Sprintf("namespace=%s", cfg.namespace),
		"--set", fmt.Sprintf("deployment.name=%s", releaseName),
		"--values", fmt.Sprintf("%s/%s_values_copy.yaml", chartFilesPath, cfg.loadName),
	}

	if err := runCommand(ctx, "helm", installArgs...); err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	defer func() {
		log.Info("Uninstalling Helm chart for the load scenario...")
		uninstallArgs := []string{
			"uninstall",
			releaseName,
			"--namespace", cfg.namespace,
		}
		if err := runCommand(context.Background(), "helm", uninstallArgs...); err != nil {
			log.Error("Error during cleanup: %v", err)
		}
		log.Info("Done!")
	}()

	log.Info("Chart will run for %s", cfg.parsedDuration)
	log.Info("To view logs, run: kubectl logs -n %s -l app=%s -f", cfg.namespace, releaseName)

	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled by user")
	case <-time.After(cfg.parsedDuration):
		return nil
	}
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
