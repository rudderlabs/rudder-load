package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rudderlabs/rudder-go-kit/logger"

	"rudder-load/internal/parser"
)

type DockerComposeClient struct {
	executor        commandExecutor
	logger          logger.Logger
	composeFilePath string
}

func NewDockerComposeClient(executor commandExecutor, logger logger.Logger) *DockerComposeClient {
	return &DockerComposeClient{executor: executor, logger: logger}
}

// Install starts the Docker Compose services
func (d *DockerComposeClient) Install(ctx context.Context, config *parser.LoadTestConfig) error {
	// Create a temporary docker-compose file with the environment variables
	composeFile, err := d.createComposeFile(config)
	if err != nil {
		return fmt.Errorf("failed to create compose file: %w", err)
	}

	// Start the Docker Compose services
	d.logger.Infon("Starting Docker Compose services", logger.NewStringField("compose_file", composeFile))
	args := []string{"-f", composeFile, "up", "-d"}
	return d.executor.run(ctx, "docker-compose", args...)
}

// Upgrade updates the Docker Compose services with new configuration
func (d *DockerComposeClient) Upgrade(ctx context.Context, config *parser.LoadTestConfig, phase parser.RunPhase) error {
	// Create a temporary docker-compose file with the updated environment variables
	composeFile, err := d.createComposeFile(config)
	if err != nil {
		return fmt.Errorf("failed to create compose file: %w", err)
	}

	// Restart the Docker Compose services with the new configuration
	d.logger.Infon("Upgrading Docker Compose services", logger.NewStringField("compose_file", composeFile))
	args := []string{"-f", composeFile, "down"}
	if err := d.executor.run(ctx, "docker-compose", args...); err != nil {
		return err
	}

	args = []string{"-f", composeFile, "up", "-d"}
	return d.executor.run(ctx, "docker-compose", args...)
}

// Uninstall stops and removes the Docker Compose services
func (d *DockerComposeClient) Uninstall(config *parser.LoadTestConfig) error {
	// Use the saved compose file path
	composeFile := d.composeFilePath
	if composeFile == "" {
		// Fallback to the default path if the saved path is empty
		composeFile = filepath.Join(".", "docker-compose.yaml")
	}

	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		return fmt.Errorf("compose file not found: %s", composeFile)
	}

	// Stop and remove the Docker Compose services
	d.logger.Infon("Stopping Docker Compose services", logger.NewStringField("compose_file", composeFile))
	args := []string{"-f", composeFile, "down"}
	err := d.executor.run(context.Background(), "docker-compose", args...)

	// Clean up the temporary file after stopping the services
	if composeFile != filepath.Join(".", "docker-compose.yaml") {
		if err := os.Remove(composeFile); err != nil {
			d.logger.Warn("Failed to remove temporary compose file", logger.NewErrorField(err))
		}
	}

	return err
}

// createComposeFile creates a temporary docker-compose file with the environment variables
func (d *DockerComposeClient) createComposeFile(config *parser.LoadTestConfig) (string, error) {
	// Read the original docker-compose.yaml file
	originalComposeFile := filepath.Join(".", "docker-compose.yaml")
	content, err := os.ReadFile(originalComposeFile)
	if err != nil {
		return "", fmt.Errorf("failed to read compose file: %w", err)
	}

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "docker-compose-*.yaml")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer tmpFile.Close()

	// Write the content to the temporary file
	if _, err := tmpFile.Write(content); err != nil {
		return "", fmt.Errorf("failed to write to temporary file: %w", err)
	}

	// Update the environment variables in the temporary file
	composeContent := string(content)

	// Add the replicas from the first phase to the environment variables
	if len(config.Phases) > 0 {
		config.EnvOverrides["REPLICAS"] = fmt.Sprintf("%d", config.Phases[0].Replicas)
	}

	// Replace environment variables with values from the config
	for key, value := range config.EnvOverrides {
		// Escape special characters in the value
		escapedValue := strings.ReplaceAll(value, "\"", "\\\"")
		escapedValue = strings.ReplaceAll(escapedValue, "$", "\\$")

		// Create a regex pattern that matches the entire line for the environment variable
		pattern := fmt.Sprintf(`(?m)^\s*%s:\s*.*$`, regexp.QuoteMeta(key))
		re := regexp.MustCompile(pattern)
		composeContent = re.ReplaceAllString(composeContent, fmt.Sprintf("      %s: %s", key, escapedValue))
	}

	// Write the updated content to the temporary file
	if err := os.WriteFile(tmpFile.Name(), []byte(composeContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write updated compose file: %w", err)
	}

	// Save the path to the struct
	d.composeFilePath = tmpFile.Name()

	return tmpFile.Name(), nil
}
