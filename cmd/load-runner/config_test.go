package main

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
phases:
  - duration: 1h30m
    replicas: 2
  - duration: 45m
    replicas: 5
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
				testFile: tmpFile,
			},
			want: &LoadTestConfig{
				Name:          "test-load",
				Namespace:     "test-ns",
				ChartFilePath: "/test/path",
				Phases: []RunPhase{
					{Duration: "1h30m", Replicas: 2},
					{Duration: "45m", Replicas: 5},
				},
				FromFile: true,
			},
			wantErr: false,
		},
		{
			name: "from cli args",
			args: &CLIArgs{
				loadName:       "cli-load",
				namespace:      "cli-ns",
				chartFilesPath: "/cli/path",
				duration:       "2h",
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

func TestLoadTestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *LoadTestConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: &LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid namespace",
			config: &LoadTestConfig{
				Name:      "test-load",
				Namespace: "Test_NS",
				Phases: []RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid name",
			config: &LoadTestConfig{
				Name:      "test_load",
				Namespace: "test-ns",
				Phases: []RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid duration",
			config: &LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []RunPhase{
					{Duration: "90", Replicas: 2},
				},
			},
			wantErr: true,
		},
		{
			name: "negative duration",
			config: &LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []RunPhase{
					{Duration: "-90", Replicas: 2},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid replicas",
			config: &LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []RunPhase{
					{Duration: "1h30m", Replicas: 0},
				},
			},
			wantErr: true,
		},
		{
			name: "negative replicas",
			config: &LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []RunPhase{
					{Duration: "1h30m", Replicas: -1},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.SetDefaults()
			require.Equal(t, tt.want.ReleaseName, tt.config.ReleaseName)
			require.Equal(t, tt.want.ChartFilePath, tt.config.ChartFilePath)
		})
	}
}
