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
	duration       string
	namespace      string
	loadName       string
	chartFilesPath string
	testFile       string
}

type testConfig struct {
	Name          string     `yaml:"name"`
	Namespace     string     `yaml:"namespace"`
	ChartFilePath string     `yaml:"chartFilePath"`
	Phases        []runPhase `yaml:"phases"`

	releaseName string
	fromFile    bool
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

	if err := setDefaults(testConfig); err != nil {
		return fmt.Errorf("failed to set defaults: %w", err)
	}

	return runLoadTest(ctx, testConfig, log)
}

func parseFlags(log logger.Logger) (*cmdArgs, error) {
	var cfg cmdArgs
	flag.StringVar(&cfg.duration, "d", "", "Duration to run (e.g., 1h, 30m, 5s)")
	flag.StringVar(&cfg.namespace, "n", "", "Kubernetes namespace")
	flag.StringVar(&cfg.loadName, "l", "", "Load scenario name")
	flag.StringVar(&cfg.chartFilesPath, "f", "", "Path to the chart files (e.g., artifacts/helm)")
	flag.StringVar(&cfg.testFile, "t", "", "Path to the test file (e.g., tests/spike.test.yaml)")
	flag.Usage = func() {
		log.Infof("Usage: %s [options]", os.Args[0])
		log.Info("Options:")
		flag.PrintDefaults()
		log.Info("Examples:")
		log.Infof("  %s -t tests/spike.test.yaml    # Runs spike test", os.Args[0])
	}

	flag.Parse()

	if cfg.testFile == "" {
		if cfg.duration == "" || cfg.namespace == "" || cfg.loadName == "" {
			if cfg.duration == "" {
				log.Error("Error: duration is required")
			}
			if cfg.namespace == "" {
				log.Error("Error: namespace is required")
			}
			if cfg.loadName == "" {
				log.Error("Error: load name is required")
			}

			flag.Usage()
			return nil, fmt.Errorf("invalid options")
		}
	}

	return &cfg, nil
}

func loadTestConfig(cmd *cmdArgs) (*testConfig, error) {
	var testConfig testConfig
	if cmd.testFile == "" {
		testConfig.Name = cmd.loadName
		testConfig.Namespace = cmd.namespace
		testConfig.ChartFilePath = cmd.chartFilesPath
		testConfig.Phases = []runPhase{
			{Duration: cmd.duration, Replicas: 1},
		}
		testConfig.fromFile = false
		return &testConfig, nil

	}
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
	testConfig.fromFile = true
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

func setDefaults(testConfig *testConfig) error {
	testConfig.releaseName = fmt.Sprintf("%s-%s", defaultReleaseNamePrefix, testConfig.Name)
	if testConfig.ChartFilePath == "" {
		testConfig.ChartFilePath = defaultChartFilesPath
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

func runLoadTest(ctx context.Context, testConfig *testConfig, log logger.Logger) error {
	log.Infof("Installing Helm chart for load scenario: %s", testConfig.Name)
	if err := installHelmChart(ctx, testConfig); err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	defer func() {
		log.Info("Uninstalling Helm chart for the load scenario...")
		if err := uninstallHelmChart(testConfig); err != nil {
			log.Errorf("Error during cleanup: %v", err)
		}
		log.Info("Done!")
	}()

	log.Infof("To view logs, run: kubectl logs -n %s -l app=%s -f", testConfig.Namespace, testConfig.releaseName)

	for _, phase := range testConfig.Phases {
		log.Infof("Chart will run for %s", phase.Duration)
		duration, err := parseDuration(phase.Duration)
		if err != nil {
			return err
		}

		if testConfig.fromFile {
			if err := upgradeHelmChart(ctx, testConfig, phase); err != nil {
				return fmt.Errorf("failed to upgrade helm chart: %w", err)
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled by user")
		case <-time.After(duration):
		}
	}
	return nil
}

func installHelmChart(ctx context.Context, testConfig *testConfig) error {
	installArgs := []string{
		"install",
		testConfig.releaseName,
		testConfig.ChartFilePath,
		"--namespace", testConfig.Namespace,
		"--set", fmt.Sprintf("namespace=%s", testConfig.Namespace),
		"--set", fmt.Sprintf("deployment.name=%s", testConfig.releaseName),
		"--values", fmt.Sprintf("%s/%s_values_copy.yaml", testConfig.ChartFilePath, testConfig.Name),
	}
	if err := runCommand(ctx, "helm", installArgs...); err != nil {
		return err
	}
	return nil
}

func uninstallHelmChart(testConfig *testConfig) error {
	uninstallArgs := []string{
		"uninstall",
		testConfig.releaseName,
		"--namespace", testConfig.Namespace,
	}
	if err := runCommand(context.Background(), "helm", uninstallArgs...); err != nil {
		return err
	}
	return nil
}

func upgradeHelmChart(ctx context.Context, testConfig *testConfig, phase runPhase) error {
	upgradeArgs := []string{
		"upgrade",
		testConfig.releaseName,
		testConfig.ChartFilePath,
		"--namespace", testConfig.Namespace,
		"--set", fmt.Sprintf("namespace=%s", testConfig.Namespace),
		"--set", fmt.Sprintf("deployment.name=%s", testConfig.releaseName),
		"--set", fmt.Sprintf("deployment.replicas=%d", phase.Replicas),
		"--values", fmt.Sprintf("%s/%s_values_copy.yaml", testConfig.ChartFilePath, testConfig.Name),
	}
	if err := runCommand(ctx, "helm", upgradeArgs...); err != nil {
		return err
	}
	return nil
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
