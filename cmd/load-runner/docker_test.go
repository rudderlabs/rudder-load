package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rudderlabs/rudder-go-kit/logger"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"rudder-load/internal/parser"
)

func TestNewDockerComposeClient(t *testing.T) {
	mockExecutor := new(MockExecutor)
	mockLogger := logger.NOP

	client := NewDockerComposeClient(mockExecutor, mockLogger)

	require.NotNil(t, client)
	require.Equal(t, mockExecutor, client.executor)
	require.Equal(t, mockLogger, client.logger)
	require.Empty(t, client.composeFilePath)
}

func TestDockerComposeClient_Install(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	dockerClient := NewDockerComposeClient(mockExecutor, logger.NOP)
	ctx := context.Background()

	// Create a temporary docker-compose.yaml file for testing
	tmpDir, err := os.MkdirTemp("", "docker-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalComposeFile := filepath.Join(tmpDir, "docker-compose.yaml")
	composeContent := `
version: '3'
services:
  producer:
    image: rudderlabs/rudder-load-producer:latest
    environment:
      MAX_EVENTS_PER_SECOND: 1000
      CONCURRENCY: 10
      MESSAGE_GENERATORS: 10
      EVENT_TYPES: "track,page,identify"
      HOT_EVENT_TYPES: "33,33,34"
`
	err = os.WriteFile(originalComposeFile, []byte(composeContent), 0644)
	require.NoError(t, err)

	// Change to the temporary directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)

	config := &parser.LoadTestConfig{
		Name: "test-load",
	}

	// Expectations
	mockExecutor.On("run", ctx, "docker-compose", mock.MatchedBy(func(args []string) bool {
		return len(args) == 4 && args[0] == "-f" && args[2] == "up" && args[3] == "-d"
	})).Return(nil)

	// Execute
	err = dockerClient.Install(ctx, config)

	// Assert
	require.NoError(t, err)
	mockExecutor.AssertExpectations(t)
	require.NotEmpty(t, dockerClient.composeFilePath)
}

func TestDockerComposeClient_Install_WithEnvOverrides(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	dockerClient := NewDockerComposeClient(mockExecutor, logger.NOP)
	ctx := context.Background()

	// Create a temporary docker-compose.yaml file for testing
	tmpDir, err := os.MkdirTemp("", "docker-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalComposeFile := filepath.Join(tmpDir, "docker-compose.yaml")
	composeContent := `
version: '3'
services:
  producer:
    image: rudderlabs/rudder-load-producer:latest
    environment:
      MAX_EVENTS_PER_SECOND: 1000
      CONCURRENCY: 10
      MESSAGE_GENERATORS: 10
      EVENT_TYPES: "track,page,identify"
      HOT_EVENT_TYPES: "33,33,34"
`
	err = os.WriteFile(originalComposeFile, []byte(composeContent), 0644)
	require.NoError(t, err)

	// Change to the temporary directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)

	config := &parser.LoadTestConfig{
		Name: "test-load",
		EnvOverrides: map[string]string{
			"MAX_EVENTS_PER_SECOND": "2000",
			"CONCURRENCY":           "20",
		},
	}

	// Expectations
	mockExecutor.On("run", ctx, "docker-compose", mock.MatchedBy(func(args []string) bool {
		return len(args) == 4 && args[0] == "-f" && args[2] == "up" && args[3] == "-d"
	})).Return(nil)

	// Execute
	err = dockerClient.Install(ctx, config)

	// Assert
	require.NoError(t, err)
	mockExecutor.AssertExpectations(t)
	require.NotEmpty(t, dockerClient.composeFilePath)

	// Verify the compose file was updated with the environment overrides
	updatedContent, err := os.ReadFile(dockerClient.composeFilePath)
	require.NoError(t, err)
	updatedContentStr := string(updatedContent)
	require.Contains(t, updatedContentStr, "MAX_EVENTS_PER_SECOND: 2000")
	require.Contains(t, updatedContentStr, "CONCURRENCY: 20")
}

func TestDockerComposeClient_Upgrade(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	dockerClient := NewDockerComposeClient(mockExecutor, logger.NOP)
	ctx := context.Background()

	// Create a temporary docker-compose.yaml file for testing
	tmpDir, err := os.MkdirTemp("", "docker-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalComposeFile := filepath.Join(tmpDir, "docker-compose.yaml")
	composeContent := `
version: '3'
services:
  producer:
    image: rudderlabs/rudder-load-producer:latest
    environment:
      MAX_EVENTS_PER_SECOND: 1000
      CONCURRENCY: 10
      MESSAGE_GENERATORS: 10
      EVENT_TYPES: "track,page,identify"
      HOT_EVENT_TYPES: "33,33,34"
`
	err = os.WriteFile(originalComposeFile, []byte(composeContent), 0644)
	require.NoError(t, err)

	// Change to the temporary directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)

	config := &parser.LoadTestConfig{
		Name: "test-load",
	}

	phase := parser.RunPhase{
		Duration: "5m",
		Replicas: 2,
	}

	// Expectations
	mockExecutor.On("run", ctx, "docker-compose", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "-f" && args[2] == "down"
	})).Return(nil)

	mockExecutor.On("run", ctx, "docker-compose", mock.MatchedBy(func(args []string) bool {
		return len(args) == 4 && args[0] == "-f" && args[2] == "up" && args[3] == "-d"
	})).Return(nil)

	// Execute
	err = dockerClient.Upgrade(ctx, config, phase)

	// Assert
	require.NoError(t, err)
	mockExecutor.AssertExpectations(t)
	require.NotEmpty(t, dockerClient.composeFilePath)
}

func TestDockerComposeClient_Upgrade_WithPhaseEnvOverrides(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	dockerClient := NewDockerComposeClient(mockExecutor, logger.NOP)
	ctx := context.Background()

	// Create a temporary docker-compose.yaml file for testing
	tmpDir, err := os.MkdirTemp("", "docker-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalComposeFile := filepath.Join(tmpDir, "docker-compose.yaml")
	composeContent := `
version: '3'
services:
  producer:
    image: rudderlabs/rudder-load-producer:latest
    environment:
      MAX_EVENTS_PER_SECOND: 1000
      CONCURRENCY: 10
      MESSAGE_GENERATORS: 10
      EVENT_TYPES: "track,page,identify"
      HOT_EVENT_TYPES: "33,33,34"
`
	err = os.WriteFile(originalComposeFile, []byte(composeContent), 0644)
	require.NoError(t, err)

	// Change to the temporary directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)

	config := &parser.LoadTestConfig{
		Name: "test-load",
		EnvOverrides: map[string]string{
			"MAX_EVENTS_PER_SECOND": "2000",
		},
	}

	phase := parser.RunPhase{
		Duration: "5m",
		Replicas: 2,
		EnvOverrides: map[string]string{
			"CONCURRENCY": "30",
		},
	}

	// Expectations
	mockExecutor.On("run", ctx, "docker-compose", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "-f" && args[2] == "down"
	})).Return(nil)

	mockExecutor.On("run", ctx, "docker-compose", mock.MatchedBy(func(args []string) bool {
		return len(args) == 4 && args[0] == "-f" && args[2] == "up" && args[3] == "-d"
	})).Return(nil)

	// Execute
	err = dockerClient.Upgrade(ctx, config, phase)

	// Assert
	require.NoError(t, err)
	mockExecutor.AssertExpectations(t)
	require.NotEmpty(t, dockerClient.composeFilePath)

	// Verify the compose file was updated with both global and phase-specific environment overrides
	updatedContent, err := os.ReadFile(dockerClient.composeFilePath)
	require.NoError(t, err)
	updatedContentStr := string(updatedContent)
	require.Contains(t, updatedContentStr, "MAX_EVENTS_PER_SECOND: 2000")
	require.Contains(t, updatedContentStr, "CONCURRENCY: 30")
}

func TestDockerComposeClient_Uninstall(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	dockerClient := NewDockerComposeClient(mockExecutor, logger.NOP)

	// Create a temporary docker-compose.yaml file for testing
	tmpDir, err := os.MkdirTemp("", "docker-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	composeFile := filepath.Join(tmpDir, "docker-compose.yaml")
	composeContent := `
version: '3'
services:
  producer:
    image: rudderlabs/rudder-load-producer:latest
    environment:
      MAX_EVENTS_PER_SECOND: 1000
`
	err = os.WriteFile(composeFile, []byte(composeContent), 0644)
	require.NoError(t, err)

	// Set the compose file path in the client
	dockerClient.composeFilePath = composeFile

	config := &parser.LoadTestConfig{
		Name: "test-load",
	}

	// Expectations
	mockExecutor.On("run", mock.Anything, "docker-compose", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "-f" && args[2] == "down"
	})).Return(nil)

	// Execute
	err = dockerClient.Uninstall(config)

	// Assert
	require.NoError(t, err)
	mockExecutor.AssertExpectations(t)
}

func TestDockerComposeClient_Uninstall_WithCustomComposeFile(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	dockerClient := NewDockerComposeClient(mockExecutor, logger.NOP)

	// Create a temporary docker-compose.yaml file for testing
	tmpDir, err := os.MkdirTemp("", "docker-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	composeFile := filepath.Join(tmpDir, "docker-compose.yaml")
	composeContent := `
version: '3'
services:
  producer:
    image: rudderlabs/rudder-load-producer:latest
    environment:
      MAX_EVENTS_PER_SECOND: 1000
`
	err = os.WriteFile(composeFile, []byte(composeContent), 0644)
	require.NoError(t, err)

	// Change to the temporary directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)

	config := &parser.LoadTestConfig{
		Name: "test-load",
	}

	// Expectations
	mockExecutor.On("run", mock.Anything, "docker-compose", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "-f" && args[2] == "down"
	})).Return(nil)

	// Execute
	err = dockerClient.Uninstall(config)

	// Assert
	require.NoError(t, err)
	mockExecutor.AssertExpectations(t)
}

func TestDockerComposeClient_createComposeFile(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	dockerClient := NewDockerComposeClient(mockExecutor, logger.NOP)

	// Create a temporary docker-compose.yaml file for testing
	tmpDir, err := os.MkdirTemp("", "docker-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalComposeFile := filepath.Join(tmpDir, "docker-compose.yaml")
	composeContent := `
version: '3'
services:
  producer:
    image: rudderlabs/rudder-load-producer:latest
    environment:
      MAX_EVENTS_PER_SECOND: 1000
      CONCURRENCY: 10
`
	err = os.WriteFile(originalComposeFile, []byte(composeContent), 0644)
	require.NoError(t, err)

	// Change to the temporary directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)

	config := &parser.LoadTestConfig{
		Name: "test-load",
	}

	// Execute
	composeFile, err := dockerClient.createComposeFile(config)

	// Assert
	require.NoError(t, err)
	require.NotEmpty(t, composeFile)
	require.Equal(t, composeFile, dockerClient.composeFilePath)

	// Verify the file exists and has the expected content
	content, err := os.ReadFile(composeFile)
	require.NoError(t, err)
	require.Contains(t, string(content), "MAX_EVENTS_PER_SECOND: 1000")
	require.Contains(t, string(content), "CONCURRENCY: 10")
}

func TestDockerComposeClient_createComposeFile_WithEnvOverrides(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	dockerClient := NewDockerComposeClient(mockExecutor, logger.NOP)

	// Create a temporary docker-compose.yaml file for testing
	tmpDir, err := os.MkdirTemp("", "docker-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalComposeFile := filepath.Join(tmpDir, "docker-compose.yaml")
	composeContent := `
version: '3'
services:
  producer:
    image: rudderlabs/rudder-load-producer:latest
    environment:
      MAX_EVENTS_PER_SECOND: 1000
      CONCURRENCY: 10
`
	err = os.WriteFile(originalComposeFile, []byte(composeContent), 0644)
	require.NoError(t, err)

	// Change to the temporary directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)

	config := &parser.LoadTestConfig{
		Name: "test-load",
		EnvOverrides: map[string]string{
			"MAX_EVENTS_PER_SECOND": "2000",
			"CONCURRENCY":           "20",
		},
	}

	// Execute
	composeFile, err := dockerClient.createComposeFile(config)

	// Assert
	require.NoError(t, err)
	require.NotEmpty(t, composeFile)
	require.Equal(t, composeFile, dockerClient.composeFilePath)

	// Verify the file exists and has the expected content with overrides
	content, err := os.ReadFile(composeFile)
	require.NoError(t, err)
	require.Contains(t, string(content), "MAX_EVENTS_PER_SECOND: 2000")
	require.Contains(t, string(content), "CONCURRENCY: 20")
}

func TestDockerComposeClient_createComposeFile_WithSpecialCharacters(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	dockerClient := NewDockerComposeClient(mockExecutor, logger.NOP)

	// Create a temporary docker-compose.yaml file for testing
	tmpDir, err := os.MkdirTemp("", "docker-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalComposeFile := filepath.Join(tmpDir, "docker-compose.yaml")
	composeContent := `
version: '3'
services:
  producer:
    image: rudderlabs/rudder-load-producer:latest
    environment:
      EVENT_TYPES: "track,page,identify"
`
	err = os.WriteFile(originalComposeFile, []byte(composeContent), 0644)
	require.NoError(t, err)

	// Change to the temporary directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)

	config := &parser.LoadTestConfig{
		Name: "test-load",
		EnvOverrides: map[string]string{
			"EVENT_TYPES": "track,page,identify,\"custom\"",
		},
	}

	// Execute
	composeFile, err := dockerClient.createComposeFile(config)

	// Assert
	require.NoError(t, err)
	require.NotEmpty(t, composeFile)
	require.Equal(t, composeFile, dockerClient.composeFilePath)

	// Verify the file exists and has the expected content with escaped special characters
	content, err := os.ReadFile(composeFile)
	require.NoError(t, err)
	require.Contains(t, string(content), "EVENT_TYPES: track,page,identify,\\\"custom\\\"")
}
