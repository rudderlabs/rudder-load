package main

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/rudderlabs/rudder-go-kit/logger"

	"rudder-load/internal/parser"
)

type HelmClient interface {
	Install(ctx context.Context, config *parser.LoadTestConfig) error
	Upgrade(ctx context.Context, config *parser.LoadTestConfig, phase parser.RunPhase) error
	Uninstall(config *parser.LoadTestConfig) error
}

type helmClient struct {
	executor CommandExecutor
	logger   logger.Logger
}

func NewHelmClient(executor CommandExecutor, logger logger.Logger) *helmClient {
	return &helmClient{executor: executor, logger: logger}
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

	args = processHelmEnvVars(args, config.EnvOverrides, h.logger)

	fmt.Printf("Running helm install with args: %v\n", args)
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

	// Merge config and phase overrides, with phase taking precedence
	mergedOverrides := make(map[string]string)

	// First add config overrides
	if config.EnvOverrides != nil {
		for k, v := range config.EnvOverrides {
			mergedOverrides[k] = v
		}
	}

	// Then add phase overrides (overriding any duplicates)
	if phase.EnvOverrides != nil {
		for k, v := range phase.EnvOverrides {
			mergedOverrides[k] = v
		}
	}

	// Process the merged overrides
	args = processHelmEnvVars(args, mergedOverrides, h.logger)

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

func processHelmEnvVars(args []string, envVars map[string]string, logger logger.Logger) []string {
	args = calculateLoadParameters(args, envVars, logger)
	for key, value := range envVars {
		if strings.Contains(value, ",") {
			value = strings.ReplaceAll(value, ",", "\\,")
		}
		args = append(args, "--set", fmt.Sprintf("deployment.env.%s=%s", key, value))
	}

	return args
}

func calculateLoadParameters(args []string, envVars map[string]string, logger logger.Logger) []string {
	resourceCalculation := envVars["RESOURCE_CALCULATION"]

	// If no resource calculation is specified, return args unchanged
	if resourceCalculation == "" {
		return args
	}

	// Validate resource calculation value
	if resourceCalculation != "auto" && !strings.HasPrefix(resourceCalculation, "overprovision,") {
		logger.Warn("Invalid RESOURCE_CALCULATION value",
			"value", resourceCalculation,
			"expected", "auto or overprovision,<percentage>")
		return args
	}

	const (
		baseEventsPerResourceUnit = 5000

		concurrencyUnit = 2000

		messageGeneratorsUnit = 500

		cpuUnit = 1

		memoryUnit = 2
	)

	maxEventsPerSecond, err := strconv.Atoi(envVars["MAX_EVENTS_PER_SECOND"])
	if err != nil {
		logger.Fatal("Failed to convert MAX_EVENTS_PER_SECOND to int: %v", err)
	}

	baseMultiplier := maxEventsPerSecond/baseEventsPerResourceUnit + 1
	overprovisionFactor := 1.0

	if strings.HasPrefix(resourceCalculation, "overprovision,") {
		parts := strings.Split(resourceCalculation, ",")
		if len(parts) != 2 {
			logger.Fatal("Invalid RESOURCE_CALCULATION format for overprovision", "overprovision,<percentage>")
		}

		overprovisionPercentage, err := strconv.Atoi(parts[1])
		if err != nil {
			logger.Fatal("Failed to convert overprovision percentage to int", err)
		}

		if overprovisionPercentage < 1 || overprovisionPercentage > 100 {
			logger.Fatal("Overprovision percentage must be between 1 and 100", overprovisionPercentage)
		}

		overprovisionFactor = 1.0 + float64(overprovisionPercentage)/100.0
	}

	resourceMultiplier := math.Round(float64(baseMultiplier)*overprovisionFactor*100) / 100
	concurrency := resourceMultiplier * concurrencyUnit
	messageGenerators := resourceMultiplier * messageGeneratorsUnit
	cpu := resourceMultiplier * cpuUnit
	memory := math.Round(resourceMultiplier * memoryUnit)

	envVars["CONCURRENCY"] = strconv.Itoa(int(concurrency))
	envVars["MESSAGE_GENERATORS"] = strconv.Itoa(int(messageGenerators))
	args = append(args, "--set", fmt.Sprintf("deployment.resources.cpuRequests=%v", cpu),
		"--set", fmt.Sprintf("deployment.resources.cpuLimits=%v", cpu),
		"--set", fmt.Sprintf("deployment.resources.memoryRequests=%vGi", memory),
		"--set", fmt.Sprintf("deployment.resources.memoryLimits=%vGi", memory),
	)
	return args
}
