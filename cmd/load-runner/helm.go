package main

import (
	"context"
	"fmt"
)

type HelmClient interface {
	Install(ctx context.Context, config *LoadTestConfig) error
	Upgrade(ctx context.Context, config *LoadTestConfig, phase RunPhase) error
	Uninstall(config *LoadTestConfig) error
}

type helmClient struct {
	executor CommandExecutor
}

func NewHelmClient(executor CommandExecutor) *helmClient {
	return &helmClient{executor: executor}
}

func (h *helmClient) Install(ctx context.Context, config *LoadTestConfig) error {
	args := []string{
		"install",
		config.ReleaseName,
		config.ChartFilePath,
		"--namespace", config.Namespace,
		"--set", fmt.Sprintf("namespace=%s", config.Namespace),
		"--set", fmt.Sprintf("deployment.name=%s", config.ReleaseName),
		"--values", fmt.Sprintf("%s/%s_values_copy.yaml", config.ChartFilePath, config.Name),
	}
	return h.executor.run(ctx, "helm", args...)
}

func (h *helmClient) Upgrade(ctx context.Context, config *LoadTestConfig, phase RunPhase) error {
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
	return h.executor.run(ctx, "helm", args...)
}

func (h *helmClient) Uninstall(config *LoadTestConfig) error {
	args := []string{
		"uninstall",
		config.ReleaseName,
		"--namespace", config.Namespace,
	}
	return h.executor.run(context.Background(), "helm", args...)
}
