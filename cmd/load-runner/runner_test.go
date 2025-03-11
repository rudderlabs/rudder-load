package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rudderlabs/rudder-go-kit/logger"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"rudder-load/internal/parser"
)

// MockHelmClient implements HelmClient interface for testing
type MockHelmClient struct {
	mock.Mock
}

func (m *MockHelmClient) Install(ctx context.Context, config *parser.LoadTestConfig) error {
	args := m.Called(ctx, config)
	return args.Error(0)
}

func (m *MockHelmClient) Uninstall(config *parser.LoadTestConfig) error {
	args := m.Called(config)
	return args.Error(0)
}

func (m *MockHelmClient) Upgrade(ctx context.Context, config *parser.LoadTestConfig, phase parser.RunPhase) error {
	args := m.Called(ctx, config, phase)
	return args.Error(0)
}

func TestLoadTestRunner_Run(t *testing.T) {
	testCases := []struct {
		name          string
		config        *parser.LoadTestConfig
		setupMock     func(*MockHelmClient)
		expectedError string
	}{
		{
			name: "successful run with multiple phases",
			config: &parser.LoadTestConfig{
				Name: "test-scenario",
				Phases: []parser.RunPhase{
					{Duration: "100ms"},
					{Duration: "100ms"},
				},
			},
			setupMock: func(m *MockHelmClient) {
				m.On("Install", mock.Anything, mock.Anything).Return(nil)
				m.On("Uninstall", mock.Anything).Return(nil)
			},
		},
		{
			name: "successful run with file config",
			config: &parser.LoadTestConfig{
				Name:     "test-scenario",
				FromFile: true,
				Phases: []parser.RunPhase{
					{Duration: "100ms"},
				},
			},
			setupMock: func(m *MockHelmClient) {
				m.On("Install", mock.Anything, mock.Anything).Return(nil)
				m.On("Upgrade", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				m.On("Uninstall", mock.Anything).Return(nil)
			},
		},
		{
			name: "install failure",
			config: &parser.LoadTestConfig{
				Name: "test-scenario",
				Phases: []parser.RunPhase{
					{Duration: "100ms"},
				},
			},
			setupMock: func(m *MockHelmClient) {
				m.On("Install", mock.Anything, mock.Anything).Return(errors.New("install failed"))
			},
			expectedError: "install failed",
		},
		{
			name: "upgrade failure",
			config: &parser.LoadTestConfig{
				Name:     "test-scenario",
				FromFile: true,
				Phases: []parser.RunPhase{
					{Duration: "100ms"},
				},
			},
			setupMock: func(m *MockHelmClient) {
				m.On("Install", mock.Anything, mock.Anything).Return(nil)
				m.On("Upgrade", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("upgrade failed"))
				m.On("Uninstall", mock.Anything).Return(nil)
			},
			expectedError: "upgrade failed",
		},
		{
			name: "invalid duration",
			config: &parser.LoadTestConfig{
				Name: "test-scenario",
				Phases: []parser.RunPhase{
					{Duration: "invalid"},
				},
			},
			setupMock: func(m *MockHelmClient) {
				m.On("Install", mock.Anything, mock.Anything).Return(nil)
				m.On("Uninstall", mock.Anything).Return(nil)
			},
			expectedError: `time: invalid duration "invalid"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockHelmClient := new(MockHelmClient)
			tc.setupMock(mockHelmClient)

			runner := NewLoadTestRunner(tc.config, mockHelmClient, logger.NOP)
			err := runner.Run(context.Background())

			if tc.expectedError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedError)
			} else {
				require.NoError(t, err)
			}

			mockHelmClient.AssertExpectations(t)
		})
	}
}

func TestLoadTestRunner_RunCancellation(t *testing.T) {
	config := &parser.LoadTestConfig{
		Name: "test-scenario",
		Phases: []parser.RunPhase{
			{Duration: "1h"}, // Long duration to ensure we can cancel
		},
	}

	mockHelmClient := new(MockHelmClient)
	mockHelmClient.On("Install", mock.Anything, mock.Anything).Return(nil)
	mockHelmClient.On("Uninstall", mock.Anything).Return(nil)

	runner := NewLoadTestRunner(config, mockHelmClient, logger.NOP)

	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error)

	go func() {
		errChan <- runner.Run(ctx)
	}()

	// Cancel the context after a short delay
	time.Sleep(100 * time.Millisecond)
	cancel()

	err := <-errChan
	require.Error(t, err)
	require.Contains(t, err.Error(), "operation cancelled by user")
	mockHelmClient.AssertExpectations(t)
}

func TestParseDuration(t *testing.T) {
	testCases := []struct {
		name          string
		input         string
		expected      time.Duration
		expectedError string
	}{
		{
			name:     "valid duration - seconds",
			input:    "30s",
			expected: 30 * time.Second,
		},
		{
			name:     "valid duration - minutes",
			input:    "5m",
			expected: 5 * time.Minute,
		},
		{
			name:          "invalid duration format",
			input:         "invalid",
			expectedError: "invalid duration",
		},
		{
			name:          "zero duration",
			input:         "0s",
			expectedError: "duration must be greater than 0",
		},
		{
			name:          "negative duration",
			input:         "-1s",
			expectedError: "duration must be greater than 0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			duration, err := parseDuration(tc.input)

			if tc.expectedError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedError)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, duration)
			}
		})
	}
}

func TestLoadTestRunner_CreateValuesFileCopy(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "temp-values-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name          string
		fileName      string
		setup         func(dir string, fileName string) error
		expectedError bool
		errorContains string
	}{
		{
			name:     "successful copy",
			fileName: "http_values",
			setup: func(dir string, fileName string) error {
				return os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s.yaml", fileName)), []byte("test content"), 0644)
			},
			expectedError: false,
		},
		{
			name:     "overwrite existing file",
			fileName: "http_values",
			setup: func(dir string, fileName string) error {
				err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s.yaml", fileName)), []byte("test content"), 0644)
				if err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s_copy.yaml", fileName)), []byte("overwrite content"), 0644)
			},
			expectedError: false,
		},
		{
			name: "source file missing",
			setup: func(dir string, fileName string) error {
				return nil
			},
			expectedError: true,
			errorContains: "failed to read source values file",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testDir, err := os.MkdirTemp(tempDir, "case-")
			require.NoError(t, err)

			err = tc.setup(testDir, tc.fileName)
			require.NoError(t, err, "Setup failed")

			config := &parser.LoadTestConfig{
				Name:          "http",
				ChartFilePath: testDir,
			}

			logger := logger.NOP
			helmClient := new(MockHelmClient)
			runner := NewLoadTestRunner(config, helmClient, logger)

			err = runner.createValuesFileCopy(context.Background())

			if tc.expectedError {
				require.Error(t, err)
				if tc.errorContains != "" {
					require.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(t, err)

				copyPath := filepath.Join(testDir, fmt.Sprintf("%s_copy.yaml", tc.fileName))
				_, err := os.Stat(copyPath)
				require.NoError(t, err, "Copy file should exist")

				sourceContent, err := os.ReadFile(filepath.Join(testDir, fmt.Sprintf("%s.yaml", tc.fileName)))
				require.NoError(t, err)

				copyContent, err := os.ReadFile(copyPath)
				require.NoError(t, err)

				require.Equal(t, sourceContent, copyContent, "File contents should match")
			}
		})
	}
}
