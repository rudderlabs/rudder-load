package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"rudder-load/internal/parser"
)

// MockExecutor implements CommandExecutor interface for testing
type MockExecutor struct {
	mock.Mock
}

func (m *MockExecutor) run(ctx context.Context, command string, args ...string) error {
	callArgs := m.Called(ctx, command, args)
	return callArgs.Error(0)
}

func TestHelmClient_Install(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	helmClient := NewHelmClient(mockExecutor)
	ctx := context.Background()

	config := &parser.LoadTestConfig{
		Name:          "test-load",
		ReleaseName:   "test-release",
		Namespace:     "test-ns",
		ChartFilePath: "/path/to/chart",
	}

	expectedArgs := []string{
		"install",
		"test-release",
		"/path/to/chart",
		"--namespace", "test-ns",
		"--set", "namespace=test-ns",
		"--set", "deployment.name=test-release",
		"--values", "/path/to/chart/test-load_values_copy.yaml",
	}

	// Expectations
	mockExecutor.On("run", ctx, "helm", expectedArgs).Return(nil)

	// Execute
	err := helmClient.Install(ctx, config)

	// Assert
	assert.NoError(t, err)
	mockExecutor.AssertExpectations(t)
}

func TestHelmClient_Upgrade(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	helmClient := NewHelmClient(mockExecutor)
	ctx := context.Background()

	config := &parser.LoadTestConfig{
		Name:          "test-load",
		ReleaseName:   "test-release",
		Namespace:     "test-ns",
		ChartFilePath: "/path/to/chart",
	}

	phase := parser.RunPhase{
		Replicas: 5,
	}

	expectedArgs := []string{
		"upgrade",
		"test-release",
		"/path/to/chart",
		"--namespace", "test-ns",
		"--set", "namespace=test-ns",
		"--set", "deployment.replicas=5",
		"--set", "deployment.name=test-release",
		"--values", "/path/to/chart/test-load_values_copy.yaml",
	}

	// Expectations
	mockExecutor.On("run", ctx, "helm", expectedArgs).Return(nil)

	// Execute
	err := helmClient.Upgrade(ctx, config, phase)

	// Assert
	assert.NoError(t, err)
	mockExecutor.AssertExpectations(t)
}

func TestHelmClient_Uninstall(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	helmClient := NewHelmClient(mockExecutor)

	config := &parser.LoadTestConfig{
		ReleaseName: "test-release",
		Namespace:   "test-ns",
	}

	expectedArgs := []string{
		"uninstall",
		"test-release",
		"--namespace", "test-ns",
	}

	// Expectations
	mockExecutor.On("run", mock.Anything, "helm", expectedArgs).Return(nil)

	// Execute
	err := helmClient.Uninstall(config)

	// Assert
	assert.NoError(t, err)
	mockExecutor.AssertExpectations(t)
}

func TestHelmClient_Install_Error(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	helmClient := NewHelmClient(mockExecutor)
	ctx := context.Background()

	config := &parser.LoadTestConfig{
		Name:          "test-load",
		ReleaseName:   "test-release",
		Namespace:     "test-ns",
		ChartFilePath: "/path/to/chart",
	}

	expectedError := fmt.Errorf("helm install failed")
	mockExecutor.On("run", ctx, "helm", mock.Anything).Return(expectedError)

	// Execute
	err := helmClient.Install(ctx, config)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, expectedError, err)
	mockExecutor.AssertExpectations(t)
}
