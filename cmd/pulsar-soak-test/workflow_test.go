package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStopCommands(t *testing.T) {
	var out bytes.Buffer
	g, err := stopCommands(&out)
	require.NoError(t, err, "turning commands off")
	out.WriteString("::set-output name=x::y\n")
	require.NoError(t, g.resume(), "turning them back on")

	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	require.Len(t, lines, 3, "start, guarded line, end")
	m := regexp.MustCompile(`^::stop-commands::([0-9a-f]{32})$`).FindStringSubmatch(lines[0])
	require.NotNil(t, m, "a random 128-bit token: %q", lines[0])
	require.Equal(t, "::"+m[1]+"::", lines[2], "the same token turns commands back on")

	other, err := stopCommands(&bytes.Buffer{})
	require.NoError(t, err, "a second guard")
	require.NotEqual(t, g.token, other.token, "every guard has its own token")
}

func TestAnnotate(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, annotate(&out, "error", "100% broken\nsecond line\r"), "annotating")
	require.Equal(t, "::error::100%25 broken%0Asecond line%0D\n", out.String(), "one escaped line")
}

func TestWriteOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	require.NoError(t, os.WriteFile(path, []byte("before=1\n"), 0o600), "the runner's file")
	require.NoError(t, writeOutput(path, "thread_ts", "1792072800.000100"), "writing")
	data, err := os.ReadFile(path)
	require.NoError(t, err, "reading")
	require.Equal(t, "before=1\nthread_ts=1792072800.000100\n", string(data), "appended")

	require.NoError(t, writeOutput("", "thread_ts", "1.2"), "without $GITHUB_OUTPUT there's nothing to do")
	require.Error(t, writeOutput(path, "thread_ts", "1.2\nother=3"), "a value can't add outputs")
	require.Error(t, writeOutput(path, "x=y", "1"), "nor a name")
}

func TestRunRejectsBadUsage(t *testing.T) {
	var out bytes.Buffer
	require.Equal(t, 2, run([]string{"start"}, &out), "an unknown command")
	require.Contains(t, out.String(), "::error::usage: go run ./cmd/pulsar-soak-test tick", "the usage")
}

func TestRunRejectsInvalidSettings(t *testing.T) {
	validEnv(t)
	t.Setenv("PULSAR_SOAK_TEST_NAMESPACE", "")
	t.Setenv("PULSAR_SOAK_TEST_SLACK_BOT_TOKEN", "xoxb secret")
	var out bytes.Buffer
	require.Equal(t, 2, run([]string{"tick"}, &out), "invalid settings fail fast")
	require.Contains(t, out.String(),
		"::error::invalid Pulsar soak test settings: PULSAR_SOAK_TEST_NAMESPACE is required",
		"the annotation says why")
	require.NotContains(t, out.String(), "xoxb secret", "without the token")
	require.NotContains(t, out.String(), "::stop-commands::", "nothing ran")
}
