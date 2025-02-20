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
	"gopkg.in/yaml.v3"
)

type cmdArgs struct {
	chartFilesPath string
	testFile       string
}

type testConfig struct {
	Name      string     `yaml:"name"`
	Namespace string     `yaml:"namespace"`
	Phases    []runPhase `yaml:"phases"`
}

type runPhase struct {
	Duration string `yaml:"duration"`
	Replicas int    `yaml:"replicas"`
}

const (
	defaultReleaseNamePrefix = "rudder-load"
	defaultChartFilesPath    = "./artifacts/helm"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	log := logger.NewLogger()

	if err := run(ctx, log); err != nil {
		log.Errorf("Error running load test: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log logger.Logger) error {
	cmd, err := parseFlags(log)
	if err != nil {
		return fmt.Errorf("invalid options: %w", err)
	}

	testConfig, err := loadTestConfig(cmd)
	if err != nil {
		return fmt.Errorf("failed to load test config: %w", err)
	}

	if err := validateInputs(testConfig); err != nil {
		return fmt.Errorf("invalid inputs: %w", err)
	}

	return runLoadTest(ctx, cmd, testConfig, log)
}

func parseFlags(log logger.Logger) (*cmdArgs, error) {
	var cfg cmdArgs
	flag.StringVar(&cfg.chartFilesPath, "f", "", "Path to the chart files (e.g., artifacts/helm)")
	flag.StringVar(&cfg.testFile, "t", "", "Path to the test file (e.g., tests/rampup.test.yaml)")
	flag.Usage = func() {
		log.Infof("Usage: %s [options]", os.Args[0])
		log.Info("Options:")
		flag.PrintDefaults()
		log.Info("Examples:")
		log.Infof("  %s -t tests/spike.test.yaml    # Runs spike test", os.Args[0])
	}

	flag.Parse()

	if cfg.testFile == "" {
		log.Error("Error: test file is required")
		flag.Usage()
		return nil, fmt.Errorf("invalid options")
	}

	return &cfg, nil
}

func loadTestConfig(cmd *cmdArgs) (*testConfig, error) {
	var testConfig testConfig
	data, err := os.Open(cmd.testFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read test file: %w", err)
	}
	defer data.Close()

	decoder := yaml.NewDecoder(data)
	err = decoder.Decode(&testConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to decode test file: %w", err)
	}
	return &testConfig, nil
}

func validateInputs(testConfig *testConfig) error {
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(testConfig.Namespace) {
		return fmt.Errorf("namespace must contain only lowercase alphanumeric characters and '-'")
	}

	if !regexp.MustCompile(`^[a-zA-Z0-9-]+$`).MatchString(testConfig.Name) {
		return fmt.Errorf("load name must contain only alphanumeric characters and '-'")
	}

	for _, phase := range testConfig.Phases {
		if !regexp.MustCompile(`^(\d+[hms])+$`).MatchString(phase.Duration) {
			return fmt.Errorf("duration must include 'h', 'm', or 's' (e.g., '1h30m')")
		}
		if phase.Replicas <= 0 {
			return fmt.Errorf("replicas must be greater than 0")
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

func runLoadTest(ctx context.Context, cfg *cmdArgs, testConfig *testConfig, log logger.Logger) error {
	releaseName := fmt.Sprintf("%s-%s", defaultReleaseNamePrefix, testConfig.Name)

	chartFilesPath := cfg.chartFilesPath
	log.Infof("Chart path before: %s", chartFilesPath)
	if chartFilesPath == "" {
		chartFilesPath = defaultChartFilesPath
	}
	log.Infof("Chart path after: %s", chartFilesPath)

	log.Infof("Installing Helm chart for load scenario: %s", testConfig.Name)
	installArgs := []string{
		"install",
		releaseName,
		chartFilesPath,
		"--namespace", testConfig.Namespace,
		"--set", fmt.Sprintf("namespace=%s", testConfig.Namespace),
		"--set", fmt.Sprintf("deployment.name=%s", releaseName),
		"--values", fmt.Sprintf("%s/%s_values_copy.yaml", chartFilesPath, testConfig.Name),
	}

	if err := runCommand(ctx, "helm", installArgs...); err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	defer func() {
		log.Info("Uninstalling Helm chart for the load scenario...")
		uninstallArgs := []string{
			"uninstall",
			releaseName,
			"--namespace", testConfig.Namespace,
		}
		if err := runCommand(context.Background(), "helm", uninstallArgs...); err != nil {
			log.Error("Error during cleanup: %v", err)
		}
		log.Info("Done!")
	}()

	log.Infof("To view logs, run: kubectl logs -n %s -l app=%s -f", testConfig.Namespace, releaseName)

	for _, phase := range testConfig.Phases {
		duration, err := parseDuration(phase.Duration)
		if err != nil {
			return fmt.Errorf("invalid duration: %w", err)
		}
		log.Infof("Chart will run for %s", duration)
		upgradeArgs := []string{
			"upgrade",
			releaseName,
			chartFilesPath,
			"--namespace", testConfig.Namespace,
			"--set", fmt.Sprintf("namespace=%s", testConfig.Namespace),
			"--set", fmt.Sprintf("deployment.name=%s", releaseName),
			"--set", fmt.Sprintf("deployment.replicas=%d", phase.Replicas),
			"--values", fmt.Sprintf("%s/%s_values_copy.yaml", chartFilesPath, testConfig.Name),
		}
		if err := runCommand(context.Background(), "helm", upgradeArgs...); err != nil {
			log.Error("Error during upgrade: %v", err)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled by user")
		case <-time.After(duration):

		}
	}
	return nil

}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
