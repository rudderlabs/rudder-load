package main

import (
	"flag"
	"os"
	"testing"

	"github.com/rudderlabs/rudder-go-kit/logger"
	"github.com/stretchr/testify/assert"
)

func TestCLI_ParseFlags(t *testing.T) {
	// Save original args and restore after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	tests := []struct {
		name        string
		args        []string
		want        *CLIArgs
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
			want: &CLIArgs{
				duration:       "1h",
				namespace:      "test-namespace",
				loadName:       "test-load",
				chartFilesPath: "path/to/chart",
				testFile:       "",
			},
			wantErr: false,
		},
		{
			name: "valid test file arg",
			args: []string{
				"cmd",
				"-t", "tests/test.yaml",
			},
			want: &CLIArgs{
				testFile: "tests/test.yaml",
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

			cli := NewCLI(logger.NewLogger())
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
		args        *CLIArgs
		wantErr     bool
		errContains string
	}{
		{
			name: "valid direct args",
			args: &CLIArgs{
				duration:       "1h",
				namespace:      "test-namespace",
				loadName:       "test-load",
				chartFilesPath: "path/to/chart",
			},
			wantErr: false,
		},
		{
			name: "valid test file",
			args: &CLIArgs{
				testFile: "tests/test.yaml",
			},
			wantErr: false,
		},
		{
			name: "missing duration",
			args: &CLIArgs{
				namespace:      "test-namespace",
				loadName:       "test-load",
				chartFilesPath: "path/to/chart",
			},
			wantErr:     true,
			errContains: "invalid options",
		},
		{
			name:        "missing all required fields",
			args:        &CLIArgs{},
			wantErr:     true,
			errContains: "invalid options",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cli := NewCLI(logger.NewLogger())
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
