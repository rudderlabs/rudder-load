package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	kitconfig "github.com/rudderlabs/rudder-go-kit/config"
	"github.com/stretchr/testify/require"

	"rudder-load/internal/pulsarsoak"
)

const (
	fakeSlackBot = "fake-slack-bot-value"
	fakeGrafana  = "fake-grafana-value"
)

// validEnv sets a complete, valid environment, with files in a temp dir, and returns the dir.
func validEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chart := filepath.Join(dir, "chart")
	require.NoError(t, os.Mkdir(chart, 0o700), "creating the chart dir")
	for _, f := range []string{filepath.Join(chart, "Chart.yaml"), filepath.Join(dir, "profile.yaml"),
		filepath.Join(dir, "brief.txt")} {
		require.NoError(t, os.WriteFile(f, []byte("x"), 0o600), "writing %s", f)
	}
	for k, v := range map[string]string{
		"PULSAR_SOAK_TEST_NAMESPACE":                   "loadtest-ns",
		"PULSAR_SOAK_TEST_HELM_CHART_PATH":             chart,
		"PULSAR_SOAK_TEST_HELM_PROFILE_PATH":           filepath.Join(dir, "profile.yaml"),
		"PULSAR_SOAK_TEST_LOAD_HTTP_ENDPOINT":          "https://ingest.example.com",
		"PULSAR_SOAK_TEST_LOAD_WRITE_KEYS":             "wk1Secret, wk2Secret",
		"PULSAR_SOAK_TEST_SRC_ROUTER_DEPLOYMENT":       "loadtest-srcrouter",
		"PULSAR_SOAK_TEST_SLACK_CHANNEL":               "C0CHANNEL",
		"PULSAR_SOAK_TEST_SLACK_RUDDER_ASSIST_USER_ID": "U0ASSIST",
		"PULSAR_SOAK_TEST_SLACK_SERVER_ENG_GROUP_ID":   "S0123SERVER",
		"PULSAR_SOAK_TEST_SLACK_BOT_TOKEN":             fakeSlackBot,
		"PULSAR_SOAK_TEST_GRAFANA_URL":                 "https://example.grafana.net/",
		"PULSAR_SOAK_TEST_GRAFANA_TOKEN":               fakeGrafana,
		"PULSAR_SOAK_TEST_GRAFANA_APP_METRICS_UID":     "app-uid",
		"PULSAR_SOAK_TEST_GRAFANA_KUBE_METRICS_UID":    "kube-uid",
		"PULSAR_SOAK_TEST_GRAFANA_LOGS_UID":            "logs-uid",
		"PULSAR_SOAK_TEST_PULSAR_CLUSTER":              "test-cluster",
		"PULSAR_SOAK_TEST_PULSAR_KUBE_NAMESPACE":       "test-cluster:o-abc",
		"PULSAR_SOAK_TEST_BRIEF_TEMPLATE_PATH":         filepath.Join(dir, "brief.txt"),
		"GITHUB_SERVER_URL":                            "",
		"GITHUB_REPOSITORY":                            "",
		"GITHUB_RUN_ID":                                "",
		"GITHUB_OUTPUT":                                "",
		"RUNNER_TEMP":                                  "",
		"PULSAR_SOAK_TEST_HELM_RELEASE":                "",
		"PULSAR_SOAK_TEST_LOAD_DEPLOYMENT_NAME":        "",
		"PULSAR_SOAK_TEST_SRC_ROUTER_DISCARD_ENV":      "",
		"PULSAR_SOAK_TEST_SLACK_WORKSPACE_URL":         "",
		"PULSAR_SOAK_TEST_RUN_LENGTH":                  "",
		"PULSAR_SOAK_TEST_DRY_RUN":                     "",
	} {
		t.Setenv(k, v)
	}
	return dir
}

func load(t *testing.T) (Settings, error) {
	t.Helper()
	return loadSettings(kitconfig.New(kitconfig.WithEnvPrefix(envPrefix)))
}

func TestLoadSettings(t *testing.T) {
	dir := validEnv(t)
	s, err := load(t)
	require.NoError(t, err, "valid settings")
	require.Equal(t, pulsarsoak.Config{
		Namespace:           "loadtest-ns",
		Release:             "rudder-load-pulsar-soak-test",
		ChartPath:           filepath.Join(dir, "chart"),
		ProfilePath:         filepath.Join(dir, "profile.yaml"),
		HTTPEndpoint:        "https://ingest.example.com",
		WriteKeys:           pulsarsoak.WriteKeys{"wk1Secret", "wk2Secret"},
		SrcRouterDeployment: "loadtest-srcrouter",
		SrcRouterDiscardEnv: "SRC_ROUTER_GATEWAY_DISCARD_MESSAGES",
		SlackChannel:        "C0CHANNEL",
		RudderAssistUserID:  "U0ASSIST",
		ServerEngGroupID:    "S0123SERVER",
		AppMetricsUID:       "app-uid",
		KubeMetricsUID:      "kube-uid",
		LogsUID:             "logs-uid",
		PulsarCluster:       "test-cluster",
		PulsarKubeNamespace: "test-cluster:o-abc",
		RunLength:           24 * time.Hour,
	}, s.Core, "the configuration, with defaults")
	require.Equal(t, fakeSlackBot, s.SlackToken, "the Slack token")
	require.Equal(t, fakeGrafana, s.GrafanaToken, "the Grafana token")
	require.Equal(t, "https://example.grafana.net", s.GrafanaURL, "the Grafana URL, without the trailing slash")
	require.Equal(t, filepath.Join(dir, "brief.txt"), s.BriefTemplatePath, "the brief template")
}

func TestLoadSettingsOptional(t *testing.T) {
	validEnv(t)
	t.Setenv("PULSAR_SOAK_TEST_HELM_RELEASE", "rudder-load-other-soak")
	t.Setenv("PULSAR_SOAK_TEST_LOAD_DEPLOYMENT_NAME", "rudder-load-loadtest")
	t.Setenv("PULSAR_SOAK_TEST_SRC_ROUTER_DISCARD_ENV", "DISCARD")
	t.Setenv("PULSAR_SOAK_TEST_SLACK_WORKSPACE_URL", "https://example.slack.com/")
	t.Setenv("PULSAR_SOAK_TEST_RUN_LENGTH", "1h")
	t.Setenv("PULSAR_SOAK_TEST_DRY_RUN", "true")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_REPOSITORY", "org/repo")
	t.Setenv("GITHUB_RUN_ID", "123456")
	t.Setenv("GITHUB_OUTPUT", "/tmp/out")
	t.Setenv("RUNNER_TEMP", "/tmp/runner")

	s, err := load(t)
	require.NoError(t, err, "valid settings")
	require.Equal(t, "rudder-load-other-soak", s.Core.Release, "release")
	require.Equal(t, "rudder-load-loadtest", s.Core.DeploymentName, "deployment name")
	require.Equal(t, "DISCARD", s.Core.SrcRouterDiscardEnv, "discard env var")
	require.Equal(t, "https://example.slack.com", s.Core.SlackWorkspaceURL, "workspace URL")
	require.Equal(t, time.Hour, s.Core.RunLength, "run length")
	require.True(t, s.Core.DryRun, "dry run")
	require.Equal(t, "https://github.com/org/repo/actions/runs/123456", s.Core.RunURL, "run URL")
	require.Equal(t, "/tmp/out", s.GitHubOutput, "GITHUB_OUTPUT")
	require.Equal(t, "/tmp/runner", s.TempDir, "RUNNER_TEMP")
}

func TestLoadSettingsDefaultPaths(t *testing.T) {
	validEnv(t)
	t.Setenv("PULSAR_SOAK_TEST_HELM_CHART_PATH", "")
	t.Setenv("PULSAR_SOAK_TEST_HELM_PROFILE_PATH", "")
	t.Chdir(filepath.Join("..", ".."))

	_, err := load(t)
	require.NotContains(t, errString(err), "PULSAR_SOAK_TEST_HELM_CHART_PATH", "the chart defaults to artifacts/helm")
	if _, statErr := os.Stat(defaultProfilePath); statErr != nil {
		require.ErrorContains(t, err, `PULSAR_SOAK_TEST_HELM_PROFILE_PATH must be a values file, got `+
			`"artifacts/helm/pulsar_soak_test_values.yaml"`, "the profile defaults to its path")
	} else {
		require.NoError(t, err, "the defaults point at the repo's chart and profile")
	}
}

func TestLoadSettingsMissing(t *testing.T) {
	validEnv(t)
	required := []string{
		"PULSAR_SOAK_TEST_NAMESPACE", "PULSAR_SOAK_TEST_LOAD_HTTP_ENDPOINT", "PULSAR_SOAK_TEST_LOAD_WRITE_KEYS",
		"PULSAR_SOAK_TEST_SRC_ROUTER_DEPLOYMENT", "PULSAR_SOAK_TEST_SLACK_CHANNEL",
		"PULSAR_SOAK_TEST_SLACK_RUDDER_ASSIST_USER_ID", "PULSAR_SOAK_TEST_SLACK_SERVER_ENG_GROUP_ID",
		"PULSAR_SOAK_TEST_SLACK_BOT_TOKEN", "PULSAR_SOAK_TEST_GRAFANA_URL", "PULSAR_SOAK_TEST_GRAFANA_TOKEN",
		"PULSAR_SOAK_TEST_GRAFANA_APP_METRICS_UID", "PULSAR_SOAK_TEST_GRAFANA_KUBE_METRICS_UID",
		"PULSAR_SOAK_TEST_GRAFANA_LOGS_UID", "PULSAR_SOAK_TEST_PULSAR_CLUSTER",
		"PULSAR_SOAK_TEST_PULSAR_KUBE_NAMESPACE", "PULSAR_SOAK_TEST_BRIEF_TEMPLATE_PATH",
	}
	for _, k := range required {
		t.Setenv(k, "")
	}
	_, err := load(t)
	require.Error(t, err, "required settings are missing")
	for _, k := range required {
		require.Contains(t, err.Error(), k, "every missing setting is reported at once")
	}
}

func TestLoadSettingsInvalid(t *testing.T) {
	tests := []struct {
		env, value, want string
	}{
		{"PULSAR_SOAK_TEST_NAMESPACE", "Soak_NS", "must be a Kubernetes namespace"},
		{"PULSAR_SOAK_TEST_HELM_RELEASE", strings.Repeat("a", 54), "at most 53"},
		{"PULSAR_SOAK_TEST_HELM_CHART_PATH", "/nowhere", "must be a Helm chart directory"},
		{"PULSAR_SOAK_TEST_HELM_PROFILE_PATH", "/nowhere.yaml", "must be a values file"},
		{"PULSAR_SOAK_TEST_LOAD_DEPLOYMENT_NAME", "my-load", "starts with rudder-load-"},
		{"PULSAR_SOAK_TEST_LOAD_DEPLOYMENT_NAME", "rudder-load-" + strings.Repeat("a", 30), "at most 40"},
		{"PULSAR_SOAK_TEST_LOAD_HTTP_ENDPOINT", "https://a.example.com/x,y", "can't be passed on"},
		{"PULSAR_SOAK_TEST_LOAD_HTTP_ENDPOINT", "ftp://example.com", "must use http or https"},
		{"PULSAR_SOAK_TEST_LOAD_HTTP_ENDPOINT", "https://user:pw@example.com", "credentials"},
		{"PULSAR_SOAK_TEST_LOAD_WRITE_KEYS", "good,", "invalid write key at position 2"},
		{"PULSAR_SOAK_TEST_LOAD_WRITE_KEYS", "dup,dup", "position 2 twice"},
		{"PULSAR_SOAK_TEST_SRC_ROUTER_DEPLOYMENT", "Src_Router", "must be src-router's Deployment"},
		{"PULSAR_SOAK_TEST_SRC_ROUTER_DISCARD_ENV", "1BAD", "must be an env var name"},
		{"PULSAR_SOAK_TEST_SLACK_CHANNEL", "#general", "a Slack channel ID"},
		{"PULSAR_SOAK_TEST_SLACK_RUDDER_ASSIST_USER_ID", "<!channel>", "a Slack user ID"},
		{"PULSAR_SOAK_TEST_SLACK_SERVER_ENG_GROUP_ID", "U0123", "a Slack user group ID"},
		{"PULSAR_SOAK_TEST_SLACK_WORKSPACE_URL", "http://example.slack.com", "must use https"},
		{"PULSAR_SOAK_TEST_SLACK_WORKSPACE_URL", "https://example.com", "must be a slack.com URL"},
		{"PULSAR_SOAK_TEST_GRAFANA_URL", "https://example.grafana.net/?x=1", "query"},
		{"PULSAR_SOAK_TEST_GRAFANA_APP_METRICS_UID", "a/b", "a Grafana datasource UID"},
		{"PULSAR_SOAK_TEST_PULSAR_CLUSTER", `x"}`, "cloud_streamnative_io_cluster"},
		{"PULSAR_SOAK_TEST_PULSAR_KUBE_NAMESPACE", "a b", "namespace label"},
		{"PULSAR_SOAK_TEST_BRIEF_TEMPLATE_PATH", "/nowhere.txt", "must be the brief template file"},
		{"PULSAR_SOAK_TEST_RUN_LENGTH", "5m", "from 10m0s to 72h0m0s"},
		{"PULSAR_SOAK_TEST_RUN_LENGTH", "24", "like 24h"},
		{"PULSAR_SOAK_TEST_DRY_RUN", "yes", "true or false"},
		{"GITHUB_RUN_ID", "12", "GITHUB_SERVER_URL must be GitHub's URL"},
	}
	for _, tt := range tests {
		t.Run(tt.env+"="+tt.value, func(t *testing.T) {
			validEnv(t)
			t.Setenv(tt.env, tt.value)
			_, err := load(t)
			require.ErrorContains(t, err, tt.env, "names the setting")
			require.ErrorContains(t, err, tt.want, "says what's wrong")
		})
	}
}

func TestLoadSettingsNeverShowsSecrets(t *testing.T) {
	validEnv(t)
	t.Setenv("PULSAR_SOAK_TEST_SLACK_BOT_TOKEN", "xoxb-secret with-space")
	t.Setenv("PULSAR_SOAK_TEST_GRAFANA_TOKEN", "glsa secret-grafana")
	t.Setenv("PULSAR_SOAK_TEST_LOAD_WRITE_KEYS", "goodSecretKey,bad secret key")
	_, err := load(t)
	require.Error(t, err, "invalid secrets")
	for _, s := range []string{"xoxb-secret", "secret-grafana", "goodSecretKey", "bad secret key"} {
		require.NotContains(t, err.Error(), s, "the error doesn't show the secret")
	}
	require.Contains(t, err.Error(), "PULSAR_SOAK_TEST_SLACK_BOT_TOKEN must be printable ASCII", "but names it")
	require.Contains(t, err.Error(), "PULSAR_SOAK_TEST_GRAFANA_TOKEN", "and the Grafana token")
	require.Contains(t, err.Error(), "PULSAR_SOAK_TEST_LOAD_WRITE_KEYS has an invalid write key at position 2",
		"and the write keys")
}

func TestEnvNames(t *testing.T) {
	want := map[string]string{
		keyNamespace:       "PULSAR_SOAK_TEST_NAMESPACE",
		keyRelease:         "PULSAR_SOAK_TEST_HELM_RELEASE",
		keyChartPath:       "PULSAR_SOAK_TEST_HELM_CHART_PATH",
		keyProfilePath:     "PULSAR_SOAK_TEST_HELM_PROFILE_PATH",
		keyDeploymentName:  "PULSAR_SOAK_TEST_LOAD_DEPLOYMENT_NAME",
		keyHTTPEndpoint:    "PULSAR_SOAK_TEST_LOAD_HTTP_ENDPOINT",
		keyWriteKeys:       "PULSAR_SOAK_TEST_LOAD_WRITE_KEYS",
		keySrcRouter:       "PULSAR_SOAK_TEST_SRC_ROUTER_DEPLOYMENT",
		keyDiscardEnv:      "PULSAR_SOAK_TEST_SRC_ROUTER_DISCARD_ENV",
		keySlackChannel:    "PULSAR_SOAK_TEST_SLACK_CHANNEL",
		keyRudderAssist:    "PULSAR_SOAK_TEST_SLACK_RUDDER_ASSIST_USER_ID",
		keyServerEng:       "PULSAR_SOAK_TEST_SLACK_SERVER_ENG_GROUP_ID",
		keyWorkspaceURL:    "PULSAR_SOAK_TEST_SLACK_WORKSPACE_URL",
		keySlackBot:        "PULSAR_SOAK_TEST_SLACK_BOT_TOKEN",
		keyGrafanaURL:      "PULSAR_SOAK_TEST_GRAFANA_URL",
		keyGrafanaToken:    "PULSAR_SOAK_TEST_GRAFANA_TOKEN",
		keyAppMetricsUID:   "PULSAR_SOAK_TEST_GRAFANA_APP_METRICS_UID",
		keyKubeMetricsUID:  "PULSAR_SOAK_TEST_GRAFANA_KUBE_METRICS_UID",
		keyLogsUID:         "PULSAR_SOAK_TEST_GRAFANA_LOGS_UID",
		keyPulsarCluster:   "PULSAR_SOAK_TEST_PULSAR_CLUSTER",
		keyPulsarNamespace: "PULSAR_SOAK_TEST_PULSAR_KUBE_NAMESPACE",
		keyBriefTemplate:   "PULSAR_SOAK_TEST_BRIEF_TEMPLATE_PATH",
		keyRunLength:       "PULSAR_SOAK_TEST_RUN_LENGTH",
		keyDryRun:          "PULSAR_SOAK_TEST_DRY_RUN",
	}
	for key, env := range want {
		require.Equal(t, env, envName(key), "the env var of %s, as the README lists it", key)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
