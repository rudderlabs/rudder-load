package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseLoadTestConfig(t *testing.T) {
	// Create a temporary YAML file for testing
	yamlContent := `
name: test-load
namespace: test-ns
chartFilePath: /test/path
env:
  MESSAGE_GENERATORS: "200"
  MAX_EVENTS_PER_SECOND: "15000"
phases:
  - duration: 1h30m
    replicas: 2
  - duration: 45m
    replicas: 5
    env:
      MESSAGE_GENERATORS: "300"
      CONCURRENCY: "500"
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test-config.yaml")
	err := os.WriteFile(tmpFile, []byte(yamlContent), 0644)
	require.NoError(t, err)

	tests := []struct {
		name    string
		args    *CLIArgs
		want    *LoadTestConfig
		wantErr bool
	}{
		{
			name: "from file",
			args: &CLIArgs{
				TestFile: tmpFile,
			},
			want: &LoadTestConfig{
				Name:          "test-load",
				Namespace:     "test-ns",
				ChartFilePath: "/test/path",
				EnvOverrides: map[string]string{
					"MESSAGE_GENERATORS":    "200",
					"MAX_EVENTS_PER_SECOND": "15000",
				},
				Phases: []RunPhase{
					{Duration: "1h30m", Replicas: 2},
					{
						Duration: "45m",
						Replicas: 5,
						EnvOverrides: map[string]string{
							"MESSAGE_GENERATORS": "300",
							"CONCURRENCY":        "500",
						},
					},
				},
				FromFile: true,
			},
			wantErr: false,
		},
		{
			name: "from cli args",
			args: &CLIArgs{
				LoadName:       "cli-load",
				Namespace:      "cli-ns",
				ChartFilesPath: "/cli/path",
				Duration:       "2h",
			},
			want: &LoadTestConfig{
				Name:          "cli-load",
				Namespace:     "cli-ns",
				ChartFilePath: "/cli/path",
				Phases: []RunPhase{
					{Duration: "2h", Replicas: 1},
				},
				FromFile: false,
			},
			wantErr: false,
		},
		{
			name: "from cli args with env vars",
			args: &CLIArgs{
				LoadName:       "cli-load",
				Namespace:      "cli-ns",
				ChartFilesPath: "/cli/path",
				Duration:       "2h",
				EnvVars: map[string]string{
					"MESSAGE_GENERATORS":    "300",
					"MAX_EVENTS_PER_SECOND": "20000",
				},
			},
			want: &LoadTestConfig{
				Name:          "cli-load",
				Namespace:     "cli-ns",
				ChartFilePath: "/cli/path",
				Phases: []RunPhase{
					{Duration: "2h", Replicas: 1},
				},
				EnvOverrides: map[string]string{
					"MESSAGE_GENERATORS":    "300",
					"MAX_EVENTS_PER_SECOND": "20000",
				},
				FromFile: false,
			},
			wantErr: false,
		},
		{
			name: "from file with cli env vars override",
			args: &CLIArgs{
				TestFile: tmpFile,
				EnvVars: map[string]string{
					"MESSAGE_GENERATORS": "400",
					"NEW_VAR":            "value",
				},
			},
			want: &LoadTestConfig{
				Name:          "test-load",
				Namespace:     "test-ns",
				ChartFilePath: "/test/path",
				EnvOverrides: map[string]string{
					"MESSAGE_GENERATORS":    "400",   // CLI value overrides file value
					"MAX_EVENTS_PER_SECOND": "15000", // Kept from file
					"NEW_VAR":               "value", // Added from CLI
				},
				Phases: []RunPhase{
					{Duration: "1h30m", Replicas: 2},
					{
						Duration: "45m",
						Replicas: 5,
						EnvOverrides: map[string]string{
							"MESSAGE_GENERATORS": "300",
							"CONCURRENCY":        "500",
						},
					},
				},
				FromFile: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLoadTestConfig(tt.args)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLoadTestConfig_SetDefaults(t *testing.T) {
	tests := []struct {
		name   string
		config *LoadTestConfig
		want   *LoadTestConfig
	}{
		{
			name: "valid config",
			config: &LoadTestConfig{
				Name: "test-load",
			},
			want: &LoadTestConfig{
				Name:          "test-load",
				ReleaseName:   "rudder-load-test-load",
				ChartFilePath: "./artifacts/helm",
			},
		},
		{
			name: "valid config with chart file path",
			config: &LoadTestConfig{
				Name:          "test-load",
				ChartFilePath: "/custom/path",
			},
			want: &LoadTestConfig{
				Name:          "test-load",
				ChartFilePath: "/custom/path",
				ReleaseName:   "rudder-load-test-load",
			},
		},
		{
			name: "valid config with env overrides",
			config: &LoadTestConfig{
				Name: "test-load",
				EnvOverrides: map[string]string{
					"MESSAGE_GENERATORS":    "200",
					"MAX_EVENTS_PER_SECOND": "15000",
				},
			},
			want: &LoadTestConfig{
				Name:          "test-load",
				ReleaseName:   "rudder-load-test-load",
				ChartFilePath: "./artifacts/helm",
				EnvOverrides: map[string]string{
					"MESSAGE_GENERATORS":    "200",
					"MAX_EVENTS_PER_SECOND": "15000",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.SetDefaults()
			require.Equal(t, tt.want.ReleaseName, tt.config.ReleaseName)
			require.Equal(t, tt.want.ChartFilePath, tt.config.ChartFilePath)
			if tt.want.EnvOverrides != nil {
				require.Equal(t, tt.want.EnvOverrides, tt.config.EnvOverrides)
			}
		})
	}
}

func TestLoadTestConfig_Reporting(t *testing.T) {
	yamlContent := `
name: test-load
namespace: test-ns
chartFilePath: /test/path
reporting:
  namespace: monitoring
  interval: 30s
  metrics:
    - name: request_latency
      query: "rate(http_request_duration_seconds_sum[5m])"
    - name: error_rate
      query: "rate(http_requests_total{status=~'5..'}[5m])"
phases:
  - duration: 1h30m
    replicas: 2
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test-config.yaml")
	err := os.WriteFile(tmpFile, []byte(yamlContent), 0644)
	require.NoError(t, err)

	tests := []struct {
		name    string
		args    *CLIArgs
		want    *LoadTestConfig
		wantErr bool
	}{
		{
			name: "from file with reporting config",
			args: &CLIArgs{
				TestFile: tmpFile,
			},
			want: &LoadTestConfig{
				Name:          "test-load",
				Namespace:     "test-ns",
				ChartFilePath: "/test/path",
				Phases: []RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				Reporting: Reporting{
					Namespace: "monitoring",
					Interval:  "30s",
					Metrics: []Metric{
						{
							Name:  "request_latency",
							Query: "rate(http_request_duration_seconds_sum[5m])",
						},
						{
							Name:  "error_rate",
							Query: "rate(http_requests_total{status=~'5..'}[5m])",
						},
					},
				},
				FromFile: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLoadTestConfig(tt.args)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLoadTestConfig_SetEnvOverrides(t *testing.T) {
	// Create a temporary .env file for testing
	envContent := `
MESSAGE_GENERATORS=100
MAX_EVENTS_PER_SECOND=5000
CONCURRENCY=50
`
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	err := os.WriteFile(envFile, []byte(envContent), 0644)
	require.NoError(t, err)

	// Change to the temporary directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalDir)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)

	tests := []struct {
		name          string
		config        *LoadTestConfig
		expectedEnv   map[string]string
		expectedError bool
	}{
		{
			name: "with existing env overrides",
			config: &LoadTestConfig{
				Name: "test-load",
				EnvOverrides: map[string]string{
					"MESSAGE_GENERATORS": "200",
				},
			},
			expectedEnv: map[string]string{
				"MESSAGE_GENERATORS":    "200",  // From config (overrides .env)
				"MAX_EVENTS_PER_SECOND": "5000", // From .env
				"CONCURRENCY":           "50",   // From .env
			},
			expectedError: false,
		},
		{
			name: "without existing env overrides",
			config: &LoadTestConfig{
				Name: "test-load",
			},
			expectedEnv: map[string]string{
				"MESSAGE_GENERATORS":    "100",  // From .env
				"MAX_EVENTS_PER_SECOND": "5000", // From .env
				"CONCURRENCY":           "50",   // From .env
			},
			expectedError: false,
		},
		{
			name: "with phases containing env overrides",
			config: &LoadTestConfig{
				Name: "test-load",
				EnvOverrides: map[string]string{
					"MESSAGE_GENERATORS": "200",
				},
				Phases: []RunPhase{
					{
						Duration: "1h",
						Replicas: 2,
						EnvOverrides: map[string]string{
							"CONCURRENCY": "100",
						},
					},
				},
			},
			expectedEnv: map[string]string{
				"MESSAGE_GENERATORS":    "200",  // From config (overrides .env)
				"MAX_EVENTS_PER_SECOND": "5000", // From .env
				"CONCURRENCY":           "50",   // From .env (phase overrides are not merged here)
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.SetEnvOverrides()
			if tt.expectedError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expectedEnv, tt.config.EnvOverrides)
		})
	}
}

func TestMergePhaseEnvOverrides(t *testing.T) {
	tests := []struct {
		name           string
		globalEnv      map[string]string
		phaseEnv       map[string]string
		expectedResult map[string]string
	}{
		{
			name: "phase overrides global",
			globalEnv: map[string]string{
				"MESSAGE_GENERATORS":    "200",
				"MAX_EVENTS_PER_SECOND": "5000",
			},
			phaseEnv: map[string]string{
				"MESSAGE_GENERATORS": "300",
				"CONCURRENCY":        "100",
			},
			expectedResult: map[string]string{
				"MESSAGE_GENERATORS":    "300",  // From phase (overrides global)
				"MAX_EVENTS_PER_SECOND": "5000", // From global
				"CONCURRENCY":           "100",  // From phase
			},
		},
		{
			name:      "empty global env",
			globalEnv: map[string]string{},
			phaseEnv: map[string]string{
				"MESSAGE_GENERATORS": "300",
				"CONCURRENCY":        "100",
			},
			expectedResult: map[string]string{
				"MESSAGE_GENERATORS": "300",
				"CONCURRENCY":        "100",
			},
		},
		{
			name: "empty phase env",
			globalEnv: map[string]string{
				"MESSAGE_GENERATORS":    "200",
				"MAX_EVENTS_PER_SECOND": "5000",
			},
			phaseEnv: map[string]string{},
			expectedResult: map[string]string{
				"MESSAGE_GENERATORS":    "200",
				"MAX_EVENTS_PER_SECOND": "5000",
			},
		},
		{
			name:           "both empty",
			globalEnv:      map[string]string{},
			phaseEnv:       map[string]string{},
			expectedResult: map[string]string{},
		},
		{
			name:           "both nil",
			globalEnv:      nil,
			phaseEnv:       nil,
			expectedResult: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of the global env to avoid modifying the original
			globalEnvCopy := make(map[string]string)
			for k, v := range tt.globalEnv {
				globalEnvCopy[k] = v
			}

			// Merge phase env overrides with global env
			result := MergeEnvVars(tt.phaseEnv, globalEnvCopy)
			require.Equal(t, tt.expectedResult, result)
		})
	}
}
