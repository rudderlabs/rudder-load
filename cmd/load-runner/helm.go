package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"

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

	args = processHelmEnvVars(args, config.EnvOverrides)

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
	args = processHelmEnvVars(args, mergedOverrides)

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

func processHelmEnvVars(args []string, envVars map[string]string) []string {
	args = calculateLoadParameters(args, envVars)
	for key, value := range envVars {
		if strings.Contains(value, ",") {
			value = strings.ReplaceAll(value, ",", "\\,")
		}
		args = append(args, "--set", fmt.Sprintf("deployment.env.%s=%s", key, value))
	}

	return args
}

func calculateLoadParameters(args []string, envVars map[string]string) []string {
	resourceCalculation := envVars["RESOURCE_CALCULATION"]

	// Handle auto calculation
	if resourceCalculation == "auto" {
		maxEventsPerSecond, err := strconv.Atoi(envVars["MAX_EVENTS_PER_SECOND"])
		if err != nil {
			log.Fatalf("Failed to convert MAX_EVENTS_PER_SECOND to int: %v", err)
		}
		resourceMultiplier := maxEventsPerSecond/5000 + 1
		envVars["CONCURRENCY"] = strconv.Itoa(resourceMultiplier * 2000)
		envVars["MESSAGE_GENERATORS"] = strconv.Itoa(resourceMultiplier * 500)
		args = append(args, "--set", fmt.Sprintf("deployment.resources.cpuRequests=%d", resourceMultiplier),
			"--set", fmt.Sprintf("deployment.resources.cpuLimits=%d", resourceMultiplier),
			"--set", fmt.Sprintf("deployment.resources.memoryRequests=%dGi", resourceMultiplier*2),
			"--set", fmt.Sprintf("deployment.resources.memoryLimits=%dGi", resourceMultiplier*2),
		)
		return args
	}

	// Handle overprovision calculation
	if strings.HasPrefix(resourceCalculation, "overprovision,") {
		parts := strings.Split(resourceCalculation, ",")
		if len(parts) != 2 {
			log.Fatalf("Invalid RESOURCE_CALCULATION format for overprovision. Expected 'overprovision,<percentage>'")
		}

		overprovisionPercent, err := strconv.Atoi(parts[1])
		if err != nil {
			log.Fatalf("Failed to convert overprovision percentage to int: %v", err)
		}

		if overprovisionPercent < 0 || overprovisionPercent > 100 {
			log.Fatalf("Overprovision percentage must be between 0 and 100, got: %d", overprovisionPercent)
		}

		maxEventsPerSecond, err := strconv.Atoi(envVars["MAX_EVENTS_PER_SECOND"])
		if err != nil {
			log.Fatalf("Failed to convert MAX_EVENTS_PER_SECOND to int: %v", err)
		}

		// Calculate base resource multiplier
		baseMultiplier := maxEventsPerSecond/5000 + 1

		// Apply overprovisioning
		overprovisionFactor := 1.0 + float64(overprovisionPercent)/100.0
		resourceMultiplier := math.Round(float64(baseMultiplier)*overprovisionFactor*100) / 100

		envVars["CONCURRENCY"] = strconv.Itoa(int(resourceMultiplier * 2000))
		envVars["MESSAGE_GENERATORS"] = strconv.Itoa(int(resourceMultiplier * 500))

		args = append(args, "--set", fmt.Sprintf("deployment.resources.cpuRequests=%v", resourceMultiplier),
			"--set", fmt.Sprintf("deployment.resources.cpuLimits=%v", resourceMultiplier),
			"--set", fmt.Sprintf("deployment.resources.memoryRequests=%vGi", math.Round(resourceMultiplier*2)),
			"--set", fmt.Sprintf("deployment.resources.memoryLimits=%vGi", math.Round(resourceMultiplier*2)),
		)
		return args
	}

	return args
}
