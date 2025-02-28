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
