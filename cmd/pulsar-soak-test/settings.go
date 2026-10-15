package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	kitconfig "github.com/rudderlabs/rudder-go-kit/config"

	"rudder-load/internal/pulsarsoak"
)

// envPrefix prefixes the env var of every setting.
const envPrefix = "PULSAR_SOAK_TEST"

const (
	minRunLength = 10 * time.Minute
	maxRunLength = 72 * time.Hour
)

// The config keys of the settings. The env var of each is PULSAR_SOAK_TEST_ followed by the key in upper snake case,
// e.g. Helm.chartPath is PULSAR_SOAK_TEST_HELM_CHART_PATH.
const (
	keyNamespace       = "namespace"
	keyRelease         = "Helm.release"
	keyChartPath       = "Helm.chartPath"
	keyProfilePath     = "Helm.profilePath"
	keyDeploymentName  = "Load.deploymentName"
	keyHTTPEndpoint    = "Load.httpEndpoint"
	keyWriteKeys       = "Load.writeKeys"
	keySrcRouter       = "SrcRouter.deployment"
	keyDiscardEnv      = "SrcRouter.discardEnv"
	keySlackChannel    = "Slack.channel"
	keyRudderAssist    = "Slack.rudderAssistUserID"
	keyServerEng       = "Slack.serverEngGroupID"
	keyWorkspaceURL    = "Slack.workspaceURL"
	keySlackBot        = "Slack.botToken"
	keyGrafanaURL      = "Grafana.url"
	keyGrafanaToken    = "Grafana.token"
	keyAppMetricsUID   = "Grafana.appMetricsUID"
	keyKubeMetricsUID  = "Grafana.kubeMetricsUID"
	keyLogsUID         = "Grafana.logsUID"
	keyPulsarCluster   = "Pulsar.cluster"
	keyPulsarNamespace = "Pulsar.kubeNamespace"
	keyBriefTemplate   = "Brief.templatePath"
	keyRunLength       = "runLength"
	keyDryRun          = "dryRun"
)

// The defaults that aren't empty.
const (
	defaultRelease     = "rudder-load-pulsar-soak-test"
	defaultChartPath   = "artifacts/helm"
	defaultProfilePath = "artifacts/helm/pulsar_soak_test_values.yaml"
	defaultDiscardEnv  = "SRC_ROUTER_GATEWAY_DISCARD_MESSAGES"
	defaultRunLength   = "24h"
)

var (
	dnsLabelRE     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	dnsSubdomainRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
	// rudder-load's producer only starts in pods whose name starts with rudder-load-.
	deploymentNameRE = regexp.MustCompile(`^rudder-load(-[a-z0-9]+)*$`)
	envNameRE        = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	channelRE        = regexp.MustCompile(`^[CG][A-Z0-9]{2,20}$`)
	userIDRE         = regexp.MustCompile(`^[UW][A-Z0-9]{2,20}$`)
	groupIDRE        = regexp.MustCompile(`^S[A-Z0-9]{2,20}$`)
	uidRE            = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
	labelValueRE     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,252}$`)
	writeKeyRE       = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	tokenRE          = regexp.MustCompile(`^[\x21-\x7e]+$`)
	repositoryRE     = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	runIDRE          = regexp.MustCompile(`^[0-9]{1,20}$`)
)

// Settings are everything the Pulsar soak test reads from its environment.
type Settings struct {
	Core              pulsarsoak.Config
	SlackToken        string
	GrafanaURL        string
	GrafanaToken      string
	BriefTemplatePath string
	// GitHubOutput is the $GITHUB_OUTPUT file, if any; TempDir is where the secret values file goes, the runner's
	// temporary directory when there is one.
	GitHubOutput string
	TempDir      string
}

// envName returns the env var of a config key.
func envName(key string) string { return kitconfig.ConfigKeyToEnv(envPrefix, key) }

// loadSettings reads and checks every setting, and reports every problem at once. Error messages never include the
// value of a secret.
func loadSettings(conf *kitconfig.Config) (Settings, error) {
	var (
		s    Settings
		errs []error
	)
	get := func(key, def string) string { return strings.TrimSpace(conf.GetStringVar(def, key)) }
	bad := func(key, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s %s", envName(key), fmt.Sprintf(format, args...)))
	}
	// match checks a value against a pattern; the value is quoted in the message unless it's secret.
	match := func(key, value string, re *regexp.Regexp, what string) string {
		switch {
		case value == "":
			bad(key, "is required: %s", what)
		case !re.MatchString(value):
			bad(key, "must be %s, got %q", what, value)
		}
		return value
	}

	c := &s.Core
	c.Namespace = match(keyNamespace, get(keyNamespace, ""), dnsLabelRE, "a Kubernetes namespace")
	c.Release = match(keyRelease, get(keyRelease, defaultRelease), dnsLabelRE, "a Helm release name")
	if len(c.Release) > 53 {
		bad(keyRelease, "must be at most 53 characters")
	}
	c.ChartPath = get(keyChartPath, defaultChartPath)
	if !isFile(filepath.Join(c.ChartPath, "Chart.yaml")) {
		bad(keyChartPath, "must be a Helm chart directory, got %q", c.ChartPath)
	}
	c.ProfilePath = get(keyProfilePath, defaultProfilePath)
	if !isFile(c.ProfilePath) {
		bad(keyProfilePath, "must be a values file, got %q", c.ProfilePath)
	}
	// Optional: without it, the profile's deployment.name stands.
	if c.DeploymentName = get(keyDeploymentName, ""); c.DeploymentName != "" {
		match(keyDeploymentName, c.DeploymentName, deploymentNameRE,
			"a Deployment name that starts with rudder-load-, like rudder-load-pulsar-soak-test")
		if len(c.DeploymentName) > 40 {
			bad(keyDeploymentName, "must be at most 40 characters, so the pod names fit in a hostname")
		}
	}
	c.HTTPEndpoint = get(keyHTTPEndpoint, "")
	if err := checkURL(c.HTTPEndpoint, "http", "https"); err != nil {
		bad(keyHTTPEndpoint, "must be the URL rudder-load sends to: %v", err)
	}
	c.WriteKeys = writeKeys(get(keyWriteKeys, ""), func(format string, args ...any) {
		bad(keyWriteKeys, format, args...)
	})

	c.SrcRouterDeployment = match(keySrcRouter, get(keySrcRouter, ""), dnsSubdomainRE, "src-router's Deployment")
	c.SrcRouterDiscardEnv = match(keyDiscardEnv, get(keyDiscardEnv, defaultDiscardEnv), envNameRE,
		"an env var name")

	c.SlackChannel = match(keySlackChannel, get(keySlackChannel, ""), channelRE, "a Slack channel ID like C0123ABCD")
	c.RudderAssistUserID = match(keyRudderAssist, get(keyRudderAssist, ""), userIDRE,
		"a Slack user ID like U0123ABCD")
	c.ServerEngGroupID = match(keyServerEng, get(keyServerEng, ""), groupIDRE,
		"a Slack user group ID like S0123ABCD")
	c.SlackWorkspaceURL = strings.TrimSuffix(get(keyWorkspaceURL, ""), "/")
	if c.SlackWorkspaceURL != "" {
		if err := checkBaseURL(c.SlackWorkspaceURL); err != nil {
			bad(keyWorkspaceURL, "must be the workspace's URL, like https://example.slack.com: %v", err)
		} else if u, _ := url.Parse(c.SlackWorkspaceURL); !strings.HasSuffix(u.Hostname(), ".slack.com") {
			bad(keyWorkspaceURL, "must be a slack.com URL, got %q", c.SlackWorkspaceURL)
		}
	}
	s.SlackToken = secret(get(keySlackBot, ""), func(format string, args ...any) {
		bad(keySlackBot, format, args...)
	})

	s.GrafanaURL = strings.TrimSuffix(get(keyGrafanaURL, ""), "/")
	if err := checkBaseURL(s.GrafanaURL); err != nil {
		bad(keyGrafanaURL, "must be Grafana's URL, like https://example.grafana.net: %v", err)
	}
	s.GrafanaToken = secret(get(keyGrafanaToken, ""), func(format string, args ...any) {
		bad(keyGrafanaToken, format, args...)
	})
	c.AppMetricsUID = match(keyAppMetricsUID, get(keyAppMetricsUID, ""), uidRE, "a Grafana datasource UID")
	c.KubeMetricsUID = match(keyKubeMetricsUID, get(keyKubeMetricsUID, ""), uidRE, "a Grafana datasource UID")
	c.LogsUID = match(keyLogsUID, get(keyLogsUID, ""), uidRE, "a Grafana datasource UID")
	c.PulsarCluster = match(keyPulsarCluster, get(keyPulsarCluster, ""), labelValueRE,
		"the cloud_streamnative_io_cluster label value")
	c.PulsarKubeNamespace = match(keyPulsarNamespace, get(keyPulsarNamespace, ""), labelValueRE,
		"the namespace label of the Pulsar pods, like cluster:namespace")

	s.BriefTemplatePath = get(keyBriefTemplate, "")
	if !isFile(s.BriefTemplatePath) {
		bad(keyBriefTemplate, "must be the brief template file, got %q", s.BriefTemplatePath)
	}

	runLength := get(keyRunLength, defaultRunLength)
	if d, err := time.ParseDuration(runLength); err != nil || d < minRunLength || d > maxRunLength {
		bad(keyRunLength, "must be a duration from %s to %s, like 24h, got %q", minRunLength, maxRunLength, runLength)
	} else {
		c.RunLength = d
	}
	switch dryRun := get(keyDryRun, "false"); dryRun {
	case "true", "false":
		c.DryRun = dryRun == "true"
	default:
		bad(keyDryRun, "must be true or false, got %q", dryRun)
	}

	runURL, err := githubRunURL(get("GITHUB_SERVER_URL", ""), get("GITHUB_REPOSITORY", ""), get("GITHUB_RUN_ID", ""))
	if err != nil {
		errs = append(errs, err)
	}
	c.RunURL = runURL
	s.GitHubOutput = get("GITHUB_OUTPUT", "")
	s.TempDir = get("RUNNER_TEMP", "")
	return s, errors.Join(errs...)
}

// writeKeys splits the comma-separated list of write keys and checks each, without ever showing one.
func writeKeys(list string, bad func(string, ...any)) pulsarsoak.WriteKeys {
	if list == "" {
		bad("is required: the comma-separated write keys rudder-load sends to")
		return nil
	}
	var keys pulsarsoak.WriteKeys
	for i, k := range strings.Split(list, ",") {
		k = strings.TrimSpace(k)
		if !writeKeyRE.MatchString(k) {
			bad("has an invalid write key at position %d: write keys are letters, digits, - and _", i+1)
			continue
		}
		if slices.Contains(keys, k) {
			bad("has the write key at position %d twice", i+1)
			continue
		}
		keys = append(keys, k)
	}
	return keys
}

// secret checks a token without ever showing it.
func secret(v string, bad func(string, ...any)) string {
	switch {
	case v == "":
		bad("is required")
	case len(v) > 4096 || !tokenRE.MatchString(v):
		bad("must be printable ASCII without spaces")
	}
	return v
}

// checkURL checks an absolute URL with one of the schemes, without credentials or characters helm or Slack would
// read as separators.
func checkURL(raw string, schemes ...string) error {
	if raw == "" {
		return errors.New("it is required")
	}
	if strings.ContainsAny(raw, ",\\|<> \t\r\n") {
		return fmt.Errorf("%q has a character that can't be passed on", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%q doesn't parse", raw)
	}
	switch {
	case !slices.Contains(schemes, u.Scheme):
		return fmt.Errorf("%q must use %s", raw, strings.Join(schemes, " or "))
	case u.Host == "":
		return fmt.Errorf("%q has no host", raw)
	case u.User != nil:
		return errors.New("it must not hold credentials")
	}
	return nil
}

// checkBaseURL checks an https URL to call APIs under: no credentials, query or fragment.
func checkBaseURL(raw string) error {
	if err := checkURL(raw, "https"); err != nil {
		return err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%q doesn't parse", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%q must not have a query or a fragment", raw)
	}
	return nil
}

// githubRunURL returns the link to the workflow run when GitHub Actions sets the three variables, and "" when none is
// set.
func githubRunURL(server, repository, runID string) (string, error) {
	if server == "" && repository == "" && runID == "" {
		return "", nil
	}
	var errs []error
	if err := checkBaseURL(server); err != nil {
		errs = append(errs, fmt.Errorf("GITHUB_SERVER_URL must be GitHub's URL: %w", err))
	}
	if !repositoryRE.MatchString(repository) {
		errs = append(errs, fmt.Errorf("GITHUB_REPOSITORY must be owner/name, got %q", repository))
	}
	if !runIDRE.MatchString(runID) {
		errs = append(errs, fmt.Errorf("GITHUB_RUN_ID must be a number, got %q", runID))
	}
	if err := errors.Join(errs...); err != nil {
		return "", fmt.Errorf("GITHUB_SERVER_URL, GITHUB_REPOSITORY and GITHUB_RUN_ID link the workflow run "+
			"when all are set, and none may be set without the others: %w", err)
	}
	return strings.TrimSuffix(server, "/") + "/" + repository + "/actions/runs/" + runID, nil
}

func isFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
