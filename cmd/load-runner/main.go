package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"time"

	kitconfig "github.com/rudderlabs/rudder-go-kit/config"
	"github.com/rudderlabs/rudder-go-kit/logger"
	obskit "github.com/rudderlabs/rudder-observability-kit/go/labels"
)

type conf struct {
	duration       time.Duration
	namespace      string
	loadName       string
	chartFilesPath string
}

const (
	defaultReleaseNamePrefix = "rudder-load"
	defaultChartFilesPath    = "./artifacts/helm"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	kitConf := kitconfig.New(kitconfig.WithEnvPrefix("LOAD_RUNNER"))
	loggerFactory := logger.NewFactory(kitConf)
	log := loggerFactory.NewLogger()

	if err := run(ctx, kitConf, log); err != nil {
		log.Errorn("Error running load test", obskit.Error(err))
		os.Exit(1)
	}
}

func run(ctx context.Context, kitConf *kitconfig.Config, logger logger.Logger) error {
	var cfg conf
	cfg.duration = kitConf.GetDuration("duration", 1, time.Minute) // set default test duration to 1 minute
	cfg.namespace = kitConf.GetString("namespace", "")             // Kubernetes namespace
	cfg.loadName = kitConf.GetString("loadName", "")               // Load scenario name
	cfg.chartFilesPath = kitConf.GetString("chartFilesPath", "")   // Path to the chart files (e.g., artifacts/helm)

	if err := validateInputs(&cfg); err != nil {
		return fmt.Errorf("invalid inputs: %w", err)
	}

	return runLoadTest(ctx, &cfg, logger)
}

func validateInputs(cfg *conf) error {
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(cfg.namespace) {
		return fmt.Errorf("namespace must contain only lowercase alphanumeric characters and '-'")
	}

	if !regexp.MustCompile(`^[a-zA-Z0-9-]+$`).MatchString(cfg.loadName) {
		return fmt.Errorf("load name must contain only alphanumeric characters and '-'")
	}

	return nil
}

func runLoadTest(ctx context.Context, cfg *conf, log logger.Logger) error {
	releaseName := fmt.Sprintf("%s-%s", defaultReleaseNamePrefix, cfg.loadName)

	chartFilesPath := cfg.chartFilesPath
	log.Infon("Chart path before", logger.NewStringField("chartFilesPath", chartFilesPath))
	if chartFilesPath == "" {
		chartFilesPath = defaultChartFilesPath
	}
	log = log.Withn(
		logger.NewStringField("chartFilesPath", chartFilesPath),
		logger.NewStringField("scenario", cfg.loadName),
	)
	log.Infon("Chart path after")

	log.Infon("Installing Helm chart for load scenario")
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
		log.Infon("Uninstalling Helm chart for the load scenario...")
		uninstallArgs := []string{
			"uninstall",
			releaseName,
			"--namespace", cfg.namespace,
		}
		if err := runCommand(context.Background(), "helm", uninstallArgs...); err != nil {
			log.Errorn("Error during cleanup", obskit.Error(err))
		}
		log.Infon("Done!")
	}()

	log.Infon("Starting run",
		logger.NewDurationField("duration", cfg.duration),
		logger.NewStringField("logs", fmt.Sprintf("kubectl logs -n %s -l app=%s -f", cfg.namespace, releaseName)),
	)

	select {
	case <-ctx.Done():
		return fmt.Errorf("operation cancelled by user")
	case <-time.After(cfg.duration):
		return nil
	}
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
