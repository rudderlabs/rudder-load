package helm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// recordingRunner records each command and, for upgrades, the secret values file as it was while helm ran.
type recordingRunner struct {
	calls      [][]string
	secretFile string
	secretMode os.FileMode
	secret     map[string]any
	err        error
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	for i, a := range args {
		if a != "-f" || i+1 >= len(args) || !strings.Contains(args[i+1], "pulsar-soak-test-values-") {
			continue
		}
		r.secretFile = args[i+1]
		info, err := os.Stat(r.secretFile)
		if err != nil {
			return err
		}
		r.secretMode = info.Mode().Perm()
		data, err := os.ReadFile(r.secretFile)
		if err != nil {
			return err
		}
		if err := yaml.Unmarshal(data, &r.secret); err != nil {
			return err
		}
	}
	return r.err
}

func testInstall() Install {
	return Install{
		Release:     "rudder-load-pulsar-soak-test",
		Chart:       "artifacts/helm",
		Namespace:   "ns",
		ValuesFiles: []string{"artifacts/helm/pulsar_soak_test_values.yaml"},
		Set:         []Value{{"namespace", "ns"}, {"deployment.name", "rudder-load-pulsar-soak-test"}},
		SetString: []Value{
			{"deployment.env.HTTP_ENDPOINT", "https://ingest.example.com/v1/batch"},
			{"deployment.env.RUN_DEADLINE", "2026-10-16T12:00:00Z"},
		},
		SecretValues: map[string]any{"deployment": map[string]any{"env": map[string]any{"SOURCES": "key-1,key-2"}}},
	}
}

func TestUpgradeInstall(t *testing.T) {
	dir := t.TempDir()
	r := &recordingRunner{}
	c := New(r, dir)

	require.NoError(t, c.UpgradeInstall(context.Background(), testInstall()), "installing")

	require.Len(t, r.calls, 1, "one helm call")
	require.NotEmpty(t, r.secretFile, "a secret values file was passed")
	require.Equal(t, dir, filepath.Dir(r.secretFile), "the file is in the temp dir")
	require.Equal(t, []string{
		"helm", "upgrade", "--install", "rudder-load-pulsar-soak-test", "artifacts/helm", "-n", "ns",
		"-f", "artifacts/helm/pulsar_soak_test_values.yaml", "-f", r.secretFile,
		"--set", "namespace=ns", "--set", "deployment.name=rudder-load-pulsar-soak-test",
		"--set-string", "deployment.env.HTTP_ENDPOINT=https://ingest.example.com/v1/batch",
		"--set-string", "deployment.env.RUN_DEADLINE=2026-10-16T12:00:00Z",
	}, r.calls[0], "arguments")
	require.Equal(t, os.FileMode(0o600), r.secretMode, "only the owner can read the file")
	require.Equal(t, map[string]any{"deployment": map[string]any{"env": map[string]any{"SOURCES": "key-1,key-2"}}},
		r.secret, "the file holds the secret values")
	for _, a := range r.calls[0] {
		require.NotContains(t, a, "key-1", "the write keys are not on the command line")
	}
	_, err := os.Stat(r.secretFile)
	require.ErrorIs(t, err, os.ErrNotExist, "the file is deleted afterwards")
}

func TestUpgradeInstallFailureDeletesTheFile(t *testing.T) {
	dir := t.TempDir()
	r := &recordingRunner{err: errors.New("exit status 1")}

	err := New(r, dir).UpgradeInstall(context.Background(), testInstall())
	require.Error(t, err, "helm fails")
	require.NotContains(t, err.Error(), "key-1", "the error doesn't hold the write keys")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "reading the temp dir")
	require.Empty(t, entries, "the file is deleted after a failure too")
}

func TestUpgradeInstallArgsRejectsSeparators(t *testing.T) {
	for _, v := range []Value{{"a", "x,y"}, {"a", `x\y`}, {"a,b", "x"}, {"", "x"}, {"a=b", "x"}} {
		in := testInstall()
		in.SetString = []Value{v}
		_, err := UpgradeInstallArgs(in, "")
		require.Error(t, err, "rejects %q=%q", v.Key, v.Value)
	}
	in := testInstall()
	in.Release = ""
	_, err := UpgradeInstallArgs(in, "")
	require.Error(t, err, "needs a release")
}

func TestUninstall(t *testing.T) {
	r := &recordingRunner{}
	require.NoError(t, New(r, "").Uninstall(context.Background(), "rudder-load-pulsar-soak-test", "ns"), "uninstalling")
	require.Equal(t, [][]string{
		{"helm", "uninstall", "rudder-load-pulsar-soak-test", "-n", "ns", "--ignore-not-found"},
	}, r.calls, "arguments")
}
