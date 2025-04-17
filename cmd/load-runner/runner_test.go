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

	"rudder-load/internal/metrics"
	"rudder-load/internal/parser"
)

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

type MockMetricsClient struct {
	mock.Mock
}

func (m *MockMetricsClient) GetMetrics(ctx context.Context, mts []parser.Metric) ([]metrics.MetricsResponse, error) {
	args := m.Called(ctx, mts)
	return args.Get(0).([]metrics.MetricsResponse), args.Error(1)
}

func (m *MockMetricsClient) Query(ctx context.Context, query string, time int64) (metrics.QueryResponse, error) {
	args := m.Called(ctx, query, time)
	return args.Get(0).(metrics.QueryResponse), args.Error(1)
}

func (m *MockMetricsClient) QueryRange(ctx context.Context, query string, start int64, end int64, step string) (metrics.QueryResponse, error) {
	args := m.Called(ctx, query, start, end, step)
	return args.Get(0).(metrics.QueryResponse), args.Error(1)
}

type MockPortForwarder struct {
	mock.Mock
}

func (m *MockPortForwarder) Start(ctx context.Context, namespace string) error {
	args := m.Called(ctx, namespace)
	return args.Error(0)
}

func (m *MockPortForwarder) Stop() error {
	args := m.Called()
	return args.Error(0)
}

// MockFileSystem is a mock implementation of file system operations
type MockFileSystem struct {
	mock.Mock
}

func (m *MockFileSystem) MkdirAll(path string, perm os.FileMode) error {
	args := m.Called(path, perm)
	return args.Error(0)
}

func (m *MockFileSystem) WriteFile(filename string, data []byte, perm os.FileMode) error {
	args := m.Called(filename, data, perm)
	return args.Error(0)
}

// Variables to store original functions for restoration
var (
	osMkdirAll  = os.MkdirAll
	osWriteFile = os.WriteFile
)

type testCase struct {
	name       string
	config     *parser.LoadTestConfig
	setupMock  func(*MockHelmClient, *MockMetricsClient, *MockPortForwarder)
	wantErr    bool
	errMessage string
}

func TestLoadTestRunner_Run(t *testing.T) {
	testCases := []testCase{
		{
			name:   "successful run",
			config: &parser.LoadTestConfig{Name: "test", Phases: []parser.RunPhase{{Duration: "10s"}}},
			setupMock: func(h *MockHelmClient, m *MockMetricsClient, p *MockPortForwarder) {
				h.On("Install", mock.Anything, mock.Anything).Return(nil)
				h.On("Uninstall", mock.Anything).Return(nil)
				p.On("Start", mock.Anything, mock.Anything).Return(nil)
				p.On("Stop").Return(nil)
				m.On("GetMetrics", mock.Anything, mock.Anything).Return([]metrics.MetricsResponse{}, nil)
			},
			wantErr: false,
		},
		{
			name: "successful run with multiple phases",
			config: &parser.LoadTestConfig{
				Name: "test-scenario",
				Phases: []parser.RunPhase{
					{Duration: "100ms"},
					{Duration: "100ms"},
				},
			},
			setupMock: func(h *MockHelmClient, m *MockMetricsClient, p *MockPortForwarder) {
				h.On("Install", mock.Anything, mock.Anything).Return(nil)
				h.On("Uninstall", mock.Anything).Return(nil)
				m.On("GetMetrics", mock.Anything, mock.Anything).Return([]metrics.MetricsResponse{}, nil)
				p.On("Start", mock.Anything, "mimir").Return(nil)
				p.On("Stop").Return(nil)
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
			setupMock: func(h *MockHelmClient, m *MockMetricsClient, p *MockPortForwarder) {
				h.On("Install", mock.Anything, mock.Anything).Return(nil)
				h.On("Upgrade", mock.Anything, mock.Anything, mock.Anything).Return(nil)
				h.On("Uninstall", mock.Anything).Return(nil)
				m.On("GetMetrics", mock.Anything, mock.Anything).Return([]metrics.MetricsResponse{}, nil)
				p.On("Start", mock.Anything, "mimir").Return(nil)
				p.On("Stop").Return(nil)
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
			setupMock: func(h *MockHelmClient, m *MockMetricsClient, p *MockPortForwarder) {
				h.On("Install", mock.Anything, mock.Anything).Return(errors.New("install failed"))
				p.On("Start", mock.Anything, "mimir").Return(nil)
				p.On("Stop").Return(nil)
			},
			wantErr:    true,
			errMessage: "install failed",
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
			setupMock: func(h *MockHelmClient, m *MockMetricsClient, p *MockPortForwarder) {
				h.On("Install", mock.Anything, mock.Anything).Return(nil)
				h.On("Upgrade", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("upgrade failed"))
				h.On("Uninstall", mock.Anything).Return(nil)
				p.On("Start", mock.Anything, "mimir").Return(nil)
				p.On("Stop").Return(nil)
			},
			wantErr:    true,
			errMessage: "upgrade failed",
		},
		{
			name: "invalid duration",
			config: &parser.LoadTestConfig{
				Name: "test-scenario",
				Phases: []parser.RunPhase{
					{Duration: "invalid"},
				},
			},
			setupMock: func(h *MockHelmClient, m *MockMetricsClient, p *MockPortForwarder) {
				h.On("Install", mock.Anything, mock.Anything).Return(nil)
				h.On("Uninstall", mock.Anything).Return(nil)
				p.On("Start", mock.Anything, "mimir").Return(nil)
				p.On("Stop").Return(nil)
			},
			wantErr:    true,
			errMessage: `time: invalid duration "invalid"`,
		},
		{
			name: "failed to start port-forwarding",
			config: &parser.LoadTestConfig{
				Name: "test-scenario",
				Phases: []parser.RunPhase{
					{Duration: "invalid"},
				},
			},
			setupMock: func(h *MockHelmClient, m *MockMetricsClient, p *MockPortForwarder) {
				p.On("Start", mock.Anything, "mimir").Return(errors.New("failed to connect service"))
			},
			wantErr:    true,
			errMessage: `failed to start port-forward: failed to connect service`,
		},
	}

	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "temp-values-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	os.WriteFile(filepath.Join(tempDir, "http_values.yaml"), []byte("test content"), 0644)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockHelmClient := new(MockHelmClient)
			mockMetricsClient := new(MockMetricsClient)
			portForwarder := new(MockPortForwarder)
			tc.setupMock(mockHelmClient, mockMetricsClient, portForwarder)

			tc.config.ChartFilePath = tempDir
			runner := NewLoadTestRunner(tc.config, mockHelmClient, mockMetricsClient, portForwarder, logger.NOP)
			err := runner.Run(context.Background())

			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMessage)
			} else {
				require.NoError(t, err)
			}

			mockHelmClient.AssertExpectations(t)
			mockMetricsClient.AssertExpectations(t)
			portForwarder.AssertExpectations(t)
		})
	}
}

func TestLoadTestRunner_RunCancellation(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "temp-values-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	os.WriteFile(filepath.Join(tempDir, "http_values.yaml"), []byte("test content"), 0644)

	config := &parser.LoadTestConfig{
		Name:          "test-scenario",
		ChartFilePath: tempDir,
		Phases: []parser.RunPhase{
			{Duration: "1h"}, // Long duration to ensure we can cancel
		},
	}

	mockHelmClient := new(MockHelmClient)
	mockMetricsClient := new(MockMetricsClient)
	mockPortForwarder := new(MockPortForwarder)
	mockHelmClient.On("Install", mock.Anything, mock.Anything).Return(nil)
	mockHelmClient.On("Uninstall", mock.Anything).Return(nil)
	mockPortForwarder.On("Start", mock.Anything, mock.Anything).Return(nil)
	mockPortForwarder.On("Stop", mock.Anything).Return(nil)

	runner := NewLoadTestRunner(config, mockHelmClient, mockMetricsClient, mockPortForwarder, logger.NOP)

	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error)

	go func() {
		errChan <- runner.Run(ctx)
	}()

	cancel()

	err = <-errChan
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
				Reporting: parser.Reporting{
					Metrics: []parser.Metric{
						{Name: "test_metric"},
					},
					Interval: "10ms",
				},
			}

			logger := logger.NOP
			helmClient := new(MockHelmClient)
			mimirClient := new(MockMetricsClient)
			runner := NewLoadTestRunner(config, helmClient, mimirClient, metrics.NewPortForwarder(time.Second*5, logger), logger)

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

func TestLoadTestRunner_MonitorMetrics(t *testing.T) {
	tests := []struct {
		name        string
		config      *parser.LoadTestConfig
		mockMetrics []metrics.MetricsResponse
		mockError   error
		expectCall  bool
	}{
		{
			name: "empty interval",
			config: &parser.LoadTestConfig{
				Reporting: parser.Reporting{
					Interval: "",
					Metrics: []parser.Metric{
						{Name: "rps"},
					},
				},
			},
			mockMetrics: nil,
			mockError:   nil,
			expectCall:  false,
		},
		{
			name: "invalid interval",
			config: &parser.LoadTestConfig{
				Reporting: parser.Reporting{
					Interval: "invalid",
					Metrics: []parser.Metric{
						{Name: "rps"},
					},
				},
			},
			mockMetrics: nil,
			mockError:   nil,
			expectCall:  false,
		},
		{
			name: "successful metrics collection",
			config: &parser.LoadTestConfig{
				Reporting: parser.Reporting{
					Interval: "10ms",
					Metrics: []parser.Metric{
						{Name: "rps"},
						{Name: "latency"},
					},
				},
			},
			mockMetrics: []metrics.MetricsResponse{
				{Key: "rps", Value: 100},
				{Key: "latency", Value: 50},
			},
			expectCall: true,
		},
		{
			name: "metrics collection with error",
			config: &parser.LoadTestConfig{
				Reporting: parser.Reporting{
					Interval: "10ms",
					Metrics: []parser.Metric{
						{Name: "rps"},
					},
				},
			},
			mockError:  fmt.Errorf("failed to get metrics"),
			expectCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMetricsClient := new(MockMetricsClient)
			mockMetricsClient.On("GetMetrics", mock.Anything, tt.config.Reporting.Metrics).Return(tt.mockMetrics, tt.mockError)

			runner := &LoadTestRunner{
				config:        tt.config,
				metricsClient: mockMetricsClient,
				logger:        logger.NOP,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			runner.monitorMetrics(ctx)

			if tt.expectCall {
				mockMetricsClient.AssertExpectations(t)
			} else {
				mockMetricsClient.AssertNotCalled(t, "GetMetrics", mock.Anything, mock.Anything)
			}
		})
	}
}

func TestLoadTestRunner_WriteMetricsToFile(t *testing.T) {
	// Create test metrics data
	testMetrics := []metricsRecord{
		{
			Timestamp: time.Now(),
			Metrics: []metrics.MetricsResponse{
				{Key: "test_metric", Value: 100},
			},
		},
	}

	tests := []struct {
		name          string
		setupMock     func(*MockFileSystem)
		metricsData   []metricsRecord
		expectedError string
	}{
		{
			name: "successful write",
			setupMock: func(mockFS *MockFileSystem) {
				mockFS.On("MkdirAll", "metrics_reports", os.FileMode(0755)).Return(nil)
				mockFS.On("WriteFile", mock.Anything, mock.Anything, os.FileMode(0644)).Return(nil)
			},
			metricsData:   testMetrics,
			expectedError: "",
		},
		{
			name: "empty metrics data",
			setupMock: func(mockFS *MockFileSystem) {
				// No mock setup needed as the function should return early
			},
			metricsData:   []metricsRecord{},
			expectedError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock file system
			mockFS := new(MockFileSystem)
			tt.setupMock(mockFS)

			// Replace the real functions with our mock
			osMkdirAll = mockFS.MkdirAll
			osWriteFile = mockFS.WriteFile

			runner := &LoadTestRunner{
				config: &parser.LoadTestConfig{
					Name: "test-metrics",
				},
				logger:      logger.NOP,
				metricsData: tt.metricsData,
				metricsFile: "metrics.json",
			}

			err := runner.writeMetricsToFile()

			if tt.expectedError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
			}

			// Restore the original functions
			osMkdirAll = os.MkdirAll
			osWriteFile = os.WriteFile
		})
	}
}
