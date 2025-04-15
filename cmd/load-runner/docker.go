package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rudderlabs/rudder-go-kit/logger"

	"rudder-load/internal/parser"
)

type DockerComposeClient struct {
	executor commandExecutor
	logger   logger.Logger
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
	defer os.Remove(composeFile)

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
	defer os.Remove(composeFile)

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
	// Find the compose file
	composeFile := filepath.Join(".", "docker-compose.yaml")
	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		return fmt.Errorf("compose file not found: %s", composeFile)
	}

	// Stop and remove the Docker Compose services
	d.logger.Infon("Stopping Docker Compose services", logger.NewStringField("compose_file", composeFile))
	args := []string{"-f", composeFile, "down"}
	return d.executor.run(context.Background(), "docker-compose", args...)
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
	d.logger.Infon("Docker compose vars", logger.NewStringField("content", composeContent))
	// Replace environment variables with values from the config
	for key, value := range config.EnvOverrides {
		// Escape special characters in the value
		escapedValue := strings.ReplaceAll(value, "\"", "\\\"")
		escapedValue = strings.ReplaceAll(escapedValue, "$", "\\$")

		// Replace the environment variable in the compose file
		composeContent = strings.ReplaceAll(composeContent, key+":", fmt.Sprintf("%s: %s", key, escapedValue))
	}
	d.logger.Infon("Docker compose vars after replacing", logger.NewStringField("content", composeContent))
	// Write the updated content to the temporary file
	if err := os.WriteFile(tmpFile.Name(), []byte(composeContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write updated compose file: %w", err)
	}

	return tmpFile.Name(), nil
}
