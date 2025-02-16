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
)

type config struct {
	duration       string
	namespace      string
	loadName       string
	chartFilesPath string
}

func main() {
	cfg := parseFlags()
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.duration, "d", "", "Duration to run (e.g., 1h, 30m, 5s)")
	flag.StringVar(&cfg.namespace, "n", "", "Kubernetes namespace")
	flag.StringVar(&cfg.loadName, "l", "", "Load scenario name")
	flag.StringVar(&cfg.chartFilesPath, "f", "", "Path to the chart files (e.g., artifacts/helm)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s -d 1m -n test -l test-staging    # Run test-staging load for 1 minute on test namespace\n", os.Args[0])
	}

	flag.Parse()

	if cfg.duration == "" || cfg.namespace == "" || cfg.loadName == "" {
		flag.Usage()
		os.Exit(1)
	}

	return cfg
}

func run(cfg config) error {
	if err := validateInputs(cfg); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	duration, err := parseDuration(cfg.duration)
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	return runLoadTest(ctx, cfg, duration)
}

func validateInputs(cfg config) error {
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(cfg.namespace) {
		return fmt.Errorf("namespace must contain only lowercase alphanumeric characters and '-'")
	}

	if !regexp.MustCompile(`^[a-zA-Z0-9-]+$`).MatchString(cfg.loadName) {
		return fmt.Errorf("load name must contain only alphanumeric characters and '-'")
	}

	return nil
}

func parseDuration(d string) (time.Duration, error) {
	if !regexp.MustCompile(`^(\d+[hms])+$`).MatchString(d) {
		return 0, fmt.Errorf("invalid duration format. Must include 'h', 'm', or 's' (e.g., '1h30m')")
	}

	duration, err := time.ParseDuration(d)
	if err != nil {
		return 0, err
	}

	if duration <= 0 {
		return 0, fmt.Errorf("duration must be greater than 0")
	}

	return duration, nil
}

func runLoadTest(ctx context.Context, cfg config, duration time.Duration) error {
	releaseName := fmt.Sprintf("rudder-load-%s", cfg.loadName)

	chartFilesPath := cfg.chartFilesPath
	fmt.Printf("Chart path before: %s\n", chartFilesPath)
	if chartFilesPath == "" {
		chartFilesPath = "./artifacts/helm"
	}
	fmt.Printf("Chart path after: %s\n", chartFilesPath)

	fmt.Printf("Installing Helm chart for load scenario: %s\n", cfg.loadName)
	installArgs := []string{
		"install",
		releaseName,
		chartFilesPath,
		"--namespace", cfg.namespace,
		"--set", fmt.Sprintf("namespace=%s", cfg.namespace),
		"--values", fmt.Sprintf("%s/%s_values_copy.yaml", chartFilesPath, cfg.loadName),
	}

	if err := runCommand(ctx, "helm", installArgs...); err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	defer func() {
		fmt.Println("Uninstalling Helm chart for the load scenario...")
		uninstallArgs := []string{
			"uninstall",
			releaseName,
			"--namespace", cfg.namespace,
		}
		if err := runCommand(context.Background(), "helm", uninstallArgs...); err != nil {
			fmt.Fprintf(os.Stderr, "Error during cleanup: %v\n", err)
		}
		fmt.Println("Done!")
	}()

	fmt.Printf("Chart will run for %s\n", duration)
	fmt.Printf("To view logs, run: kubectl logs -n %s -l app=%s -f\n", cfg.namespace, releaseName)

	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled by user")
	case <-time.After(duration):
		return nil
	}
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
