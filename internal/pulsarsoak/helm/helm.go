// Package helm installs and uninstalls releases with the helm binary.
package helm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultTimeout bounds one helm command.
const DefaultTimeout = 5 * time.Minute

// Runner runs a command to completion.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
}

// ExecRunner runs commands with os/exec, sending their output to Output.
type ExecRunner struct {
	Output io.Writer
}

// Run runs the command and waits for it.
func (r ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = r.Output
	cmd.Stderr = r.Output
	return cmd.Run()
}

// Value is one --set or --set-string value.
type Value struct {
	Key   string
	Value string
}

// Install describes a helm upgrade --install.
type Install struct {
	Release   string
	Chart     string
	Namespace string
	// ValuesFiles are passed with -f, in order.
	ValuesFiles []string
	// Set values are passed with --set, SetString values with --set-string. Neither may hold a comma or a backslash,
	// which helm would read as a separator or an escape.
	Set       []Value
	SetString []Value
	// SecretValues are written to a temporary values file, readable only by its owner, which is passed with -f after
	// ValuesFiles and deleted once helm is done. They never appear on the command line.
	SecretValues map[string]any
}

// Client runs helm commands.
type Client struct {
	runner  Runner
	tempDir string
	timeout time.Duration
}

// New returns a client that runs helm with runner. Temporary files go to tempDir, or the default when it's empty.
func New(runner Runner, tempDir string) *Client {
	return &Client{runner: runner, tempDir: tempDir, timeout: DefaultTimeout}
}

// UpgradeInstall runs helm upgrade --install.
func (c *Client) UpgradeInstall(ctx context.Context, in Install) (err error) {
	secretFile := ""
	if len(in.SecretValues) > 0 {
		f, err := writeSecretValues(c.tempDir, in.SecretValues)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, remove(f)) }()
		secretFile = f
	}
	args, err := UpgradeInstallArgs(in, secretFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := c.runner.Run(ctx, "helm", args...); err != nil {
		return fmt.Errorf("running helm upgrade --install %s: %w", in.Release, err)
	}
	return nil
}

// Uninstall runs helm uninstall. A release that doesn't exist counts as uninstalled.
func (c *Client) Uninstall(ctx context.Context, release, namespace string) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := c.runner.Run(ctx, "helm", UninstallArgs(release, namespace)...); err != nil {
		return fmt.Errorf("running helm uninstall %s: %w", release, err)
	}
	return nil
}

// UpgradeInstallArgs returns helm's arguments for in, with secretFile as the last values file if it isn't empty.
func UpgradeInstallArgs(in Install, secretFile string) ([]string, error) {
	if in.Release == "" || in.Chart == "" || in.Namespace == "" {
		return nil, errors.New("an install needs a release, a chart and a namespace")
	}
	args := []string{"upgrade", "--install", in.Release, in.Chart, "-n", in.Namespace}
	for _, f := range in.ValuesFiles {
		args = append(args, "-f", f)
	}
	if secretFile != "" {
		args = append(args, "-f", secretFile)
	}
	for _, flag := range []struct {
		name   string
		values []Value
	}{{"--set", in.Set}, {"--set-string", in.SetString}} {
		for _, v := range flag.values {
			if v.Key == "" || strings.ContainsAny(v.Key, ",=\\") || strings.ContainsAny(v.Value, ",\\") {
				return nil, fmt.Errorf("the value for %q can't go in %s", v.Key, flag.name)
			}
			args = append(args, flag.name, v.Key+"="+v.Value)
		}
	}
	return args, nil
}

// UninstallArgs returns helm's arguments to uninstall release.
func UninstallArgs(release, namespace string) []string {
	return []string{"uninstall", release, "-n", namespace, "--ignore-not-found"}
}

// remove deletes the secret values file at path.
func remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing the secret values file: %w", err)
	}
	return nil
}

// writeSecretValues writes values to a new YAML file that only its owner can read, and returns its path.
func writeSecretValues(dir string, values map[string]any) (path string, err error) {
	data, err := yaml.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("encoding the secret values: %w", err)
	}
	f, err := os.CreateTemp(dir, "pulsar-soak-test-values-*.yaml")
	if err != nil {
		return "", fmt.Errorf("creating the secret values file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("closing the secret values file: %w", cerr))
		}
		if err != nil {
			err = errors.Join(err, remove(f.Name()))
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return "", fmt.Errorf("restricting the secret values file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return "", fmt.Errorf("writing the secret values file: %w", err)
	}
	return f.Name(), nil
}
