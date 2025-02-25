package main

import (
	"context"
	"errors"
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
