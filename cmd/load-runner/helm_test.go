package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/rudderlabs/rudder-go-kit/logger"
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
	helmClient := NewHelmClient(mockExecutor, logger.NOP)
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

func TestHelmClient_Install_WithEnvOverrides(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	helmClient := NewHelmClient(mockExecutor, logger.NOP)
	ctx := context.Background()

	config := &parser.LoadTestConfig{
		Name:          "test-load",
		ReleaseName:   "test-release",
		Namespace:     "test-ns",
		ChartFilePath: "/path/to/chart",
		EnvOverrides: map[string]string{
			"MESSAGE_GENERATORS":    "200",
			"MAX_EVENTS_PER_SECOND": "15000",
		},
	}

	mockExecutor.On("run", ctx, "helm", mock.MatchedBy(func(args []string) bool {
		if len(args) != 15 {
			return false
		}

		fixedArgs := []string{
			"install",
			"test-release",
			"/path/to/chart",
			"--namespace", "test-ns",
			"--set", "namespace=test-ns",
			"--set", "deployment.name=test-release",
			"--values", "/path/to/chart/test-load_values_copy.yaml",
		}

		for i, arg := range fixedArgs {
			if args[i] != arg {
				return false
			}
		}

		// Check that both env vars are present, regardless of order
		envVarArgs := args[11:]
		envVarSet := make(map[string]bool)

		for i := 0; i < len(envVarArgs); i += 2 {
			if i+1 < len(envVarArgs) && envVarArgs[i] == "--set" {
				envVarSet[envVarArgs[i+1]] = true
			}
		}

		return envVarSet["deployment.env.MESSAGE_GENERATORS=200"] &&
			envVarSet["deployment.env.MAX_EVENTS_PER_SECOND=15000"]
	})).Return(nil)

	// Execute
	err := helmClient.Install(ctx, config)

	// Assert
	assert.NoError(t, err)
	mockExecutor.AssertExpectations(t)
}

func TestHelmClient_Upgrade(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	helmClient := NewHelmClient(mockExecutor, logger.NOP)
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

func TestHelmClient_Upgrade_WithGlobalEnvOverrides(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	helmClient := NewHelmClient(mockExecutor, logger.NOP)
	ctx := context.Background()

	config := &parser.LoadTestConfig{
		Name:          "test-load",
		ReleaseName:   "test-release",
		Namespace:     "test-ns",
		ChartFilePath: "/path/to/chart",
		EnvOverrides: map[string]string{
			"MESSAGE_GENERATORS":    "200",
			"MAX_EVENTS_PER_SECOND": "15000",
		},
	}

	phase := parser.RunPhase{
		Replicas: 5,
	}

	mockExecutor.On("run", ctx, "helm", mock.MatchedBy(func(args []string) bool {
		if len(args) != 17 {
			return false
		}

		fixedArgs := []string{
			"upgrade",
			"test-release",
			"/path/to/chart",
			"--namespace", "test-ns",
			"--set", "namespace=test-ns",
			"--set", "deployment.replicas=5",
			"--set", "deployment.name=test-release",
			"--values", "/path/to/chart/test-load_values_copy.yaml",
		}

		for i, arg := range fixedArgs {
			if args[i] != arg {
				return false
			}
		}

		// Check that both env vars are present, regardless of order
		envVarArgs := args[11:]
		envVarSet := make(map[string]bool)

		for i := 0; i < len(envVarArgs); i += 2 {
			if i+1 < len(envVarArgs) && envVarArgs[i] == "--set" {
				envVarSet[envVarArgs[i+1]] = true
			}
		}

		return envVarSet["deployment.env.MESSAGE_GENERATORS=200"] &&
			envVarSet["deployment.env.MAX_EVENTS_PER_SECOND=15000"]
	})).Return(nil)

	// Execute
	err := helmClient.Upgrade(ctx, config, phase)

	// Assert
	assert.NoError(t, err)
	mockExecutor.AssertExpectations(t)
}

func TestHelmClient_Upgrade_WithPhaseEnvOverrides(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	helmClient := NewHelmClient(mockExecutor, logger.NOP)
	ctx := context.Background()

	config := &parser.LoadTestConfig{
		Name:          "test-load",
		ReleaseName:   "test-release",
		Namespace:     "test-ns",
		ChartFilePath: "/path/to/chart",
		EnvOverrides: map[string]string{
			"MESSAGE_GENERATORS":    "200",
			"MAX_EVENTS_PER_SECOND": "15000",
		},
	}

	phase := parser.RunPhase{
		Replicas: 5,
		EnvOverrides: map[string]string{
			"MESSAGE_GENERATORS": "300",
			"CONCURRENCY":        "500",
		},
	}

	mockExecutor.On("run", ctx, "helm", mock.MatchedBy(func(args []string) bool {
		if len(args) != 19 {
			return false
		}

		fixedArgs := []string{
			"upgrade",
			"test-release",
			"/path/to/chart",
			"--namespace", "test-ns",
			"--set", "namespace=test-ns",
			"--set", "deployment.replicas=5",
			"--set", "deployment.name=test-release",
			"--values", "/path/to/chart/test-load_values_copy.yaml",
		}

		for i, arg := range fixedArgs {
			if args[i] != arg {
				return false
			}
		}

		// Check that all env vars are present, regardless of order
		envVarArgs := args[11:]
		envVarSet := make(map[string]bool)

		for i := 0; i < len(envVarArgs); i += 2 {
			if i+1 < len(envVarArgs) && envVarArgs[i] == "--set" {
				envVarSet[envVarArgs[i+1]] = true
			}
		}

		return envVarSet["deployment.env.MAX_EVENTS_PER_SECOND=15000"] &&
			envVarSet["deployment.env.MESSAGE_GENERATORS=300"] &&
			envVarSet["deployment.env.CONCURRENCY=500"]
	})).Return(nil)

	// Execute
	err := helmClient.Upgrade(ctx, config, phase)

	// Assert
	assert.NoError(t, err)
	mockExecutor.AssertExpectations(t)
}

func TestHelmClient_Install_WithCommaEscaping(t *testing.T) {
	mockExecutor := new(MockExecutor)
	helmClient := NewHelmClient(mockExecutor, logger.NOP)
	ctx := context.Background()

	config := &parser.LoadTestConfig{
		Name:          "test-load",
		ReleaseName:   "test-release",
		Namespace:     "test-ns",
		ChartFilePath: "/path/to/chart",
		EnvOverrides: map[string]string{
			"COMMA_VALUE":       "value1,value2,value3",
			"NORMAL_VALUE":      "normal",
			"MORE_COMMA_VALUES": "a,b,c",
		},
	}

	mockExecutor.On("run", ctx, "helm", mock.MatchedBy(func(args []string) bool {
		// Check for fixed arguments
		fixedArgs := []string{
			"install",
			"test-release",
			"/path/to/chart",
			"--namespace", "test-ns",
			"--set", "namespace=test-ns",
			"--set", "deployment.name=test-release",
			"--values", "/path/to/chart/test-load_values_copy.yaml",
		}

		for i, arg := range fixedArgs {
			if i >= len(args) || args[i] != arg {
				return false
			}
		}

		envVarSet := make(map[string]bool)
		for i := len(fixedArgs); i < len(args); i += 2 {
			if i+1 < len(args) && args[i] == "--set" {
				envVarSet[args[i+1]] = true
			}
		}

		return envVarSet["deployment.env.COMMA_VALUE=value1\\,value2\\,value3"] &&
			envVarSet["deployment.env.NORMAL_VALUE=normal"] &&
			envVarSet["deployment.env.MORE_COMMA_VALUES=a\\,b\\,c"]
	})).Return(nil)

	err := helmClient.Install(ctx, config)

	// Assert
	assert.NoError(t, err)
	mockExecutor.AssertExpectations(t)
}

func TestHelmClient_Upgrade_WithCommaEscaping(t *testing.T) {
	mockExecutor := new(MockExecutor)
	helmClient := NewHelmClient(mockExecutor, logger.NOP)
	ctx := context.Background()

	config := &parser.LoadTestConfig{
		Name:          "test-load",
		ReleaseName:   "test-release",
		Namespace:     "test-ns",
		ChartFilePath: "/path/to/chart",
		EnvOverrides: map[string]string{
			"GLOBAL_COMMA_VALUE": "global1,global2",
		},
	}

	phase := parser.RunPhase{
		Replicas: 5,
		EnvOverrides: map[string]string{
			"PHASE_COMMA_VALUE": "phase1,phase2,phase3",
		},
	}

	mockExecutor.On("run", ctx, "helm", mock.MatchedBy(func(args []string) bool {
		// Check for fixed arguments
		fixedArgs := []string{
			"upgrade",
			"test-release",
			"/path/to/chart",
			"--namespace", "test-ns",
			"--set", "namespace=test-ns",
			"--set", "deployment.replicas=5",
			"--set", "deployment.name=test-release",
			"--values", "/path/to/chart/test-load_values_copy.yaml",
		}

		for i, arg := range fixedArgs {
			if i >= len(args) || args[i] != arg {
				return false
			}
		}

		envVarSet := make(map[string]bool)
		for i := len(fixedArgs); i < len(args); i += 2 {
			if i+1 < len(args) && args[i] == "--set" {
				envVarSet[args[i+1]] = true
			}
		}

		return envVarSet["deployment.env.GLOBAL_COMMA_VALUE=global1\\,global2"] &&
			envVarSet["deployment.env.PHASE_COMMA_VALUE=phase1\\,phase2\\,phase3"]
	})).Return(nil)

	err := helmClient.Upgrade(ctx, config, phase)

	// Assert
	assert.NoError(t, err)
	mockExecutor.AssertExpectations(t)
}

func TestHelmClient_Uninstall(t *testing.T) {
	// Setup
	mockExecutor := new(MockExecutor)
	helmClient := NewHelmClient(mockExecutor, logger.NOP)

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
	helmClient := NewHelmClient(mockExecutor, logger.NOP)
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

func TestCalculateLoadParameters(t *testing.T) {
	tests := []struct {
		name            string
		envVars         map[string]string
		expectedArgs    []string
		expectedEnvVars map[string]string
		wantErr         bool
		errContains     string
	}{
		{
			name: "auto calculation enabled with valid MAX_EVENTS_PER_SECOND",
			envVars: map[string]string{
				"RESOURCE_CALCULATION":  "auto",
				"MAX_EVENTS_PER_SECOND": "10000",
			},
			expectedArgs: []string{
				"--set", "deployment.resources.cpuRequests=3",
				"--set", "deployment.resources.cpuLimits=3",
				"--set", "deployment.resources.memoryRequests=6Gi",
				"--set", "deployment.resources.memoryLimits=6Gi",
			},
			expectedEnvVars: map[string]string{
				"RESOURCE_CALCULATION":  "auto",
				"MAX_EVENTS_PER_SECOND": "10000",
				"CONCURRENCY":           "6000",
				"MESSAGE_GENERATORS":    "1500",
			},
			wantErr:     false,
			errContains: "",
		},
		{
			name: "auto calculation enabled with low MAX_EVENTS_PER_SECOND",
			envVars: map[string]string{
				"RESOURCE_CALCULATION":  "auto",
				"MAX_EVENTS_PER_SECOND": "1000",
			},
			expectedArgs: []string{
				"--set", "deployment.resources.cpuRequests=1",
				"--set", "deployment.resources.cpuLimits=1",
				"--set", "deployment.resources.memoryRequests=2Gi",
				"--set", "deployment.resources.memoryLimits=2Gi",
			},
			expectedEnvVars: map[string]string{
				"RESOURCE_CALCULATION":  "auto",
				"MAX_EVENTS_PER_SECOND": "1000",
				"CONCURRENCY":           "2000",
				"MESSAGE_GENERATORS":    "500",
			},
			wantErr:     false,
			errContains: "",
		},
		{
			name: "auto calculation disabled",
			envVars: map[string]string{
				"MAX_EVENTS_PER_SECOND": "10000",
			},
			expectedArgs: []string{},
			expectedEnvVars: map[string]string{
				"MAX_EVENTS_PER_SECOND": "10000",
			},
			wantErr:     false,
			errContains: "",
		},
		{
			name:            "empty env vars",
			envVars:         map[string]string{},
			expectedArgs:    []string{},
			expectedEnvVars: map[string]string{},
			wantErr:         false,
			errContains:     "",
		},
		{
			name: "overprovision calculation with 10%",
			envVars: map[string]string{
				"RESOURCE_CALCULATION":  "overprovision,10",
				"MAX_EVENTS_PER_SECOND": "10000",
			},
			expectedArgs: []string{
				"--set", "deployment.resources.cpuRequests=3.3",
				"--set", "deployment.resources.cpuLimits=3.3",
				"--set", "deployment.resources.memoryRequests=7Gi",
				"--set", "deployment.resources.memoryLimits=7Gi",
			},
			expectedEnvVars: map[string]string{
				"RESOURCE_CALCULATION":  "overprovision,10",
				"MAX_EVENTS_PER_SECOND": "10000",
				"CONCURRENCY":           "6600",
				"MESSAGE_GENERATORS":    "1650",
			},
			wantErr:     false,
			errContains: "",
		},
		{
			name: "overprovision calculation with 50%",
			envVars: map[string]string{
				"RESOURCE_CALCULATION":  "overprovision,50",
				"MAX_EVENTS_PER_SECOND": "10000",
			},
			expectedArgs: []string{
				"--set", "deployment.resources.cpuRequests=4.5",
				"--set", "deployment.resources.cpuLimits=4.5",
				"--set", "deployment.resources.memoryRequests=9Gi",
				"--set", "deployment.resources.memoryLimits=9Gi",
			},
			expectedEnvVars: map[string]string{
				"RESOURCE_CALCULATION":  "overprovision,50",
				"MAX_EVENTS_PER_SECOND": "10000",
				"CONCURRENCY":           "9000",
				"MESSAGE_GENERATORS":    "2250",
			},
			wantErr:     false,
			errContains: "",
		},
		{
			name: "overprovision calculation with 100%",
			envVars: map[string]string{
				"RESOURCE_CALCULATION":  "overprovision,100",
				"MAX_EVENTS_PER_SECOND": "10000",
			},
			expectedArgs: []string{
				"--set", "deployment.resources.cpuRequests=6",
				"--set", "deployment.resources.cpuLimits=6",
				"--set", "deployment.resources.memoryRequests=12Gi",
				"--set", "deployment.resources.memoryLimits=12Gi",
			},
			expectedEnvVars: map[string]string{
				"RESOURCE_CALCULATION":  "overprovision,100",
				"MAX_EVENTS_PER_SECOND": "10000",
				"CONCURRENCY":           "12000",
				"MESSAGE_GENERATORS":    "3000",
			},
			wantErr:     false,
			errContains: "",
		},
		{
			name: "invalid MAX_EVENTS_PER_SECOND",
			envVars: map[string]string{
				"RESOURCE_CALCULATION":  "overprovision,100",
				"MAX_EVENTS_PER_SECOND": "10000.34",
			},
			expectedArgs:    nil,
			expectedEnvVars: nil,
			wantErr:         true,
			errContains:     "failed to convert MAX_EVENTS_PER_SECOND to int",
		},
		{
			name: "invalid resource calculation value",
			envVars: map[string]string{
				"RESOURCE_CALCULATION":  "manual",
				"MAX_EVENTS_PER_SECOND": "10000",
			},
			expectedArgs:    nil,
			expectedEnvVars: nil,
			wantErr:         true,
			errContains:     "invalid RESOURCE_CALCULATION value: manual, expected: auto or overprovision,<percentage>",
		},
		{
			name: "invalid overprovision value format",
			envVars: map[string]string{
				"RESOURCE_CALCULATION":  "overprovision,2,3",
				"MAX_EVENTS_PER_SECOND": "10000",
			},
			expectedArgs:    nil,
			expectedEnvVars: nil,
			wantErr:         true,
			errContains:     "invalid RESOURCE_CALCULATION format for overprovision, expecting: overprovision,<percentage>",
		},
		{
			name: "non integer overprovision percentage",
			envVars: map[string]string{
				"RESOURCE_CALCULATION":  "overprovision,99.3",
				"MAX_EVENTS_PER_SECOND": "10000",
			},
			expectedArgs:    nil,
			expectedEnvVars: nil,
			wantErr:         true,
			errContains:     "failed to convert overprovision percentage to int",
		},
		{
			name: "overprovision percentage out of range",
			envVars: map[string]string{
				"RESOURCE_CALCULATION":  "overprovision,101",
				"MAX_EVENTS_PER_SECOND": "10000",
			},
			expectedArgs:    nil,
			expectedEnvVars: nil,
			wantErr:         true,
			errContains:     "overprovision percentage must be between 1 and 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			inputEnvVars := make(map[string]string)
			for k, v := range tt.envVars {
				inputEnvVars[k] = v
			}

			args, err := calculateLoadParameters([]string{}, inputEnvVars, logger.NOP)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			assert.Equal(t, tt.expectedArgs, args, "args mismatch")

			for k, v := range tt.expectedEnvVars {
				assert.Equal(t, v, inputEnvVars[k], "env var %s mismatch", k)
			}

			assert.Equal(t, len(tt.expectedEnvVars), len(inputEnvVars), "unexpected env vars added")
		})
	}
}
