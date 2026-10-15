// Command pulsar-soak-test reconciles the automated Pulsar soak test. A scheduled workflow runs
//
//	go run ./cmd/pulsar-soak-test tick
//
// every 15 minutes; each tick reads the state, observes the cluster and the metrics, does what is due and exits.
// Every setting comes from PULSAR_SOAK_TEST_* env vars; see the README.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	kitconfig "github.com/rudderlabs/rudder-go-kit/config"
	"github.com/rudderlabs/rudder-go-kit/logger"
	obskit "github.com/rudderlabs/rudder-observability-kit/go/labels"
	"k8s.io/klog/v2"

	"rudder-load/internal/pulsarsoak"
	"rudder-load/internal/pulsarsoak/grafana"
	"rudder-load/internal/pulsarsoak/helm"
	"rudder-load/internal/pulsarsoak/kube"
	"rudder-load/internal/pulsarsoak/slack"
)

// tickTimeout bounds a whole tick, well within the 15 minutes between ticks.
const tickTimeout = 10 * time.Minute

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

func run(args []string, out io.Writer) int {
	conf := kitconfig.New(kitconfig.WithEnvPrefix(envPrefix))
	log := logger.NewFactory(conf).NewLogger().Child("pulsar-soak-test")
	// fail prints an error annotation. Its message must be the Pulsar soak test's own text: workflow commands are on.
	fail := func(code int, message string) int {
		if err := annotate(out, "error", message); err != nil {
			log.Errorn("Can't print the annotation", obskit.Error(err))
		}
		return code
	}

	if len(args) != 1 || args[0] != "tick" {
		return fail(2, "usage: go run ./cmd/pulsar-soak-test tick")
	}
	settings, err := loadSettings(conf)
	if err != nil {
		log.Errorn("Invalid Pulsar soak test settings", obskit.Error(err))
		return fail(2, "invalid Pulsar soak test settings: "+err.Error())
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, tickTimeout)
	defer cancel()

	// Everything from here on may print what Slack, Grafana or Kubernetes answered.
	guard, err := stopCommands(out)
	if err != nil {
		log.Errorn("Can't guard the log", obskit.Error(err))
		return 1
	}
	klog.LogToStderr(false)
	klog.SetOutput(out)
	err = tick(ctx, settings, log, out)
	if err != nil {
		log.Errorn("The Pulsar soak test tick failed", obskit.Error(err))
	}
	klog.Flush()
	if gerr := guard.resume(); gerr != nil {
		log.Errorn("Can't resume workflow commands", obskit.Error(gerr))
	}
	if err != nil {
		return fail(1, "the Pulsar soak test tick failed; the log above has the details")
	}
	return 0
}

func tick(ctx context.Context, s Settings, log logger.Logger, out io.Writer) error {
	cfg := s.Core
	log.Infon("Starting a Pulsar soak test tick",
		obskit.Namespace(cfg.Namespace),
		logger.NewStringField("release", cfg.Release),
		logger.NewStringField("chart", cfg.ChartPath),
		logger.NewStringField("profile", cfg.ProfilePath),
		logger.NewStringField("deploymentName", cfg.DeploymentName),
		logger.NewStringField("httpEndpoint", cfg.HTTPEndpoint),
		logger.NewIntField("writeKeys", int64(len(cfg.WriteKeys))),
		logger.NewStringField("srcRouter", cfg.SrcRouterDeployment),
		logger.NewStringField("pulsarCluster", cfg.PulsarCluster),
		logger.NewDurationField("runLength", cfg.RunLength),
		logger.NewBoolField("dryRun", cfg.DryRun),
		logger.NewStringField("runUrl", cfg.RunURL),
	)

	cs, err := kube.NewClientset(kube.DefaultTimeout)
	if err != nil {
		return err
	}
	metrics, err := grafana.New(s.GrafanaURL, s.GrafanaToken)
	if err != nil {
		return err
	}
	cluster := kube.NewCluster(cs, cfg.Namespace, cfg.Release, cfg.SrcRouterDeployment, cfg.SrcRouterDiscardEnv)
	ticker := pulsarsoak.NewTicker(cfg, pulsarsoak.Deps{
		Store:    kube.NewStateStore(cs, cfg.Namespace, cfg.Release+"-state"),
		Helm:     helm.New(helm.ExecRunner{Output: out}, s.TempDir),
		Slack:    slack.New(s.SlackToken, cfg.SlackChannel),
		Observer: pulsarsoak.NewLiveObserver(cfg, metrics, cluster),
		ReadBrief: func() (string, error) {
			b, err := os.ReadFile(s.BriefTemplatePath)
			if err != nil {
				return "", fmt.Errorf("reading the brief template: %w", err)
			}
			return string(b), nil
		},
		OnAnnounce: func(ts string) error {
			if !slack.ValidTS(ts) {
				return errors.New("the announcement's ts isn't valid")
			}
			return writeOutput(s.GitHubOutput, "thread_ts", ts)
		},
		Log: log,
	})
	return ticker.Tick(ctx)
}
