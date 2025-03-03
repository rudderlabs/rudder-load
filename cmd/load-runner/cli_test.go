package main

import (
	"flag"
	"os"
	"testing"

	"github.com/rudderlabs/rudder-go-kit/logger"
	"github.com/stretchr/testify/assert"

	"rudder-load/internal/parser"
)

func TestCLI_ParseFlags(t *testing.T) {
	// Save original args and restore after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	tests := []struct {
		name        string
		args        []string
		want        *parser.CLIArgs
		wantErr     bool
		errContains string
	}{
		{
			name: "valid direct args",
			args: []string{
				"cmd",
				"-d", "1h",
				"-n", "test-namespace",
				"-l", "test-load",
				"-f", "path/to/chart",
			},
			want: &parser.CLIArgs{
				Duration:       "1h",
				Namespace:      "test-namespace",
				LoadName:       "test-load",
				ChartFilesPath: "path/to/chart",
				TestFile:       "",
				EnvVars:        map[string]string{},
			},
			wantErr: false,
		},
		{
			name: "valid test file arg",
			args: []string{
				"cmd",
				"-t", "tests/test.yaml",
			},
			want: &parser.CLIArgs{
				TestFile: "tests/test.yaml",
				EnvVars:  map[string]string{},
			},
			wantErr: false,
		},
		{
			name: "with environment variables",
			args: []string{
				"cmd",
				"-d", "1h",
				"-n", "test-namespace",
				"-l", "test-load",
				"-e", "MESSAGE_GENERATORS=200",
				"-e", "MAX_EVENTS_PER_SECOND=15000",
			},
			want: &parser.CLIArgs{
				Duration:  "1h",
				Namespace: "test-namespace",
				LoadName:  "test-load",
				TestFile:  "",
				EnvVars: map[string]string{
					"MESSAGE_GENERATORS":    "200",
					"MAX_EVENTS_PER_SECOND": "15000",
				},
			},
			wantErr: false,
		},
		{
			name: "test file with environment variables",
			args: []string{
				"cmd",
				"-t", "tests/test.yaml",
				"-e", "MESSAGE_GENERATORS=300",
				"-e", "CONCURRENCY=500",
			},
			want: &parser.CLIArgs{
				TestFile: "tests/test.yaml",
				EnvVars: map[string]string{
					"MESSAGE_GENERATORS": "300",
					"CONCURRENCY":        "500",
				},
			},
			wantErr: false,
		},
		{
			name: "missing required args without test file",
			args: []string{
				"cmd",
				"-n", "test-namespace",
				"-l", "test-load",
			},
			want:        nil,
			wantErr:     true,
			errContains: "invalid options",
		},
		{
			name: "missing namespace",
			args: []string{
				"cmd",
				"-d", "1h",
				"-l", "test-load",
			},
			want:        nil,
			wantErr:     true,
			errContains: "invalid options",
		},
		{
			name: "missing load name",
			args: []string{
				"cmd",
				"-d", "1h",
				"-n", "test-namespace",
			},
			want:        nil,
			wantErr:     true,
			errContains: "invalid options",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset flags before each test
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

			// Set up test args
			os.Args = tt.args

			cli := NewCLI(logger.NOP)
			got, err := cli.ParseFlags()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCLI_ValidateArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        *parser.CLIArgs
		wantErr     bool
		errContains string
	}{
		{
			name: "valid direct args",
			args: &parser.CLIArgs{
				Duration:       "1h",
				Namespace:      "test-namespace",
				LoadName:       "test-load",
				ChartFilesPath: "path/to/chart",
			},
			wantErr: false,
		},
		{
			name: "valid test file",
			args: &parser.CLIArgs{
				TestFile: "tests/test.yaml",
			},
			wantErr: false,
		},
		{
			name: "valid with environment variables",
			args: &parser.CLIArgs{
				Duration:  "1h",
				Namespace: "test-namespace",
				LoadName:  "test-load",
				EnvVars: map[string]string{
					"MESSAGE_GENERATORS": "200",
				},
			},
			wantErr: false,
		},
		{
			name: "missing duration",
			args: &parser.CLIArgs{
				Namespace:      "test-namespace",
				LoadName:       "test-load",
				ChartFilesPath: "path/to/chart",
			},
			wantErr:     true,
			errContains: "invalid options",
		},
		{
			name:        "missing all required fields",
			args:        &parser.CLIArgs{},
			wantErr:     true,
			errContains: "invalid options",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cli := NewCLI(logger.NOP)
			err := cli.ValidateArgs(tt.args)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			assert.NoError(t, err)
		})
	}
}
