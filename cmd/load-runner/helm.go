package main

import (
	"context"
	"fmt"

	"rudder-load/internal/parser"
)

type HelmClient interface {
	Install(ctx context.Context, config *parser.LoadTestConfig) error
	Upgrade(ctx context.Context, config *parser.LoadTestConfig, phase parser.RunPhase) error
	Uninstall(config *parser.LoadTestConfig) error
}

type helmClient struct {
	executor CommandExecutor
}

func NewHelmClient(executor CommandExecutor) *helmClient {
	return &helmClient{executor: executor}
}

func (h *helmClient) Install(ctx context.Context, config *parser.LoadTestConfig) error {
	args := []string{
		"install",
		config.ReleaseName,
		config.ChartFilePath,
		"--namespace", config.Namespace,
		"--set", fmt.Sprintf("namespace=%s", config.Namespace),
		"--set", fmt.Sprintf("deployment.name=%s", config.ReleaseName),
		"--values", fmt.Sprintf("%s/%s_values_copy.yaml", config.ChartFilePath, config.Name),
	}

	for key, value := range config.EnvOverrides {
		args = append(args, "--set", fmt.Sprintf("deployment.env.%s=%s", key, value))
	}

	return h.executor.run(ctx, "helm", args...)
}

func (h *helmClient) Upgrade(ctx context.Context, config *parser.LoadTestConfig, phase parser.RunPhase) error {
	args := []string{
		"upgrade",
		config.ReleaseName,
		config.ChartFilePath,
		"--namespace", config.Namespace,
		"--set", fmt.Sprintf("namespace=%s", config.Namespace),
		"--set", fmt.Sprintf("deployment.replicas=%d", phase.Replicas),
		"--set", fmt.Sprintf("deployment.name=%s", config.ReleaseName),
		"--values", fmt.Sprintf("%s/%s_values_copy.yaml", config.ChartFilePath, config.Name),
	}

	for key, value := range config.EnvOverrides {
		args = append(args, "--set", fmt.Sprintf("deployment.env.%s=%s", key, value))
	}

	for key, value := range phase.EnvOverrides {
		args = append(args, "--set", fmt.Sprintf("deployment.env.%s=%s", key, value))
	}

	return h.executor.run(ctx, "helm", args...)
}

func (h *helmClient) Uninstall(config *parser.LoadTestConfig) error {
	args := []string{
		"uninstall",
		config.ReleaseName,
		"--namespace", config.Namespace,
	}
	return h.executor.run(context.Background(), "helm", args...)
}
