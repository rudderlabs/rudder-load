package parser

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeEnvVars(t *testing.T) {
	tests := []struct {
		name           string
		configEnvVars  map[string]string
		envFileVars    map[string]string
		expectedResult map[string]string
	}{
		{
			name: "config overrides env file",
			configEnvVars: map[string]string{
				"SOURCES":     "config-source",
				"CONCURRENCY": "500",
			},
			envFileVars: map[string]string{
				"SOURCES":            "env-file-source",
				"HTTP_ENDPOINT":      "https://example.com",
				"CONCURRENCY":        "100",
				"MESSAGE_GENERATORS": "200",
			},
			expectedResult: map[string]string{
				"SOURCES":            "config-source",       // From config (overrides env file)
				"HTTP_ENDPOINT":      "https://example.com", // From env file
				"CONCURRENCY":        "500",                 // From config (overrides env file)
				"MESSAGE_GENERATORS": "200",                 // From env file
			},
		},
		{
			name:          "empty config",
			configEnvVars: map[string]string{},
			envFileVars: map[string]string{
				"SOURCES":       "env-file-source",
				"HTTP_ENDPOINT": "https://example.com",
			},
			expectedResult: map[string]string{
				"SOURCES":       "env-file-source",
				"HTTP_ENDPOINT": "https://example.com",
			},
		},
		{
			name: "empty env file",
			configEnvVars: map[string]string{
				"SOURCES": "config-source",
			},
			envFileVars: map[string]string{},
			expectedResult: map[string]string{
				"SOURCES": "config-source",
			},
		},
		{
			name:           "both empty",
			configEnvVars:  map[string]string{},
			envFileVars:    map[string]string{},
			expectedResult: map[string]string{},
		},
		{
			name:           "both nil",
			configEnvVars:  nil,
			envFileVars:    nil,
			expectedResult: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeEnvVars(tt.configEnvVars, tt.envFileVars)
			require.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestLoadEnvConfig(t *testing.T) {
	tests := []struct {
		name           string
		filePath       string
		setupFunc      func() error
		expectedError  string
		expectedResult map[string]string
	}{
		{
			name:           "no file",
			filePath:       ".no.env",
			expectedResult: map[string]string{},
		},
		{
			name:     "invalid file",
			filePath: ".invalid.env",
			setupFunc: func() error {
				return os.WriteFile(".invalid.env", []byte("INVALID VALUE\n!MALFORMED"), 0644)
			},
			expectedError: "unexpected character",
		},
		{
			name:     "valid file",
			filePath: ".valid.env",
			setupFunc: func() error {
				return os.WriteFile(".valid.env", []byte("VALID=VALUE"), 0644)
			},
			expectedResult: map[string]string{
				"VALID": "VALUE",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupFunc != nil {
				defer os.Remove(tt.filePath)
				tt.setupFunc()
			}
			result, err := LoadEnvConfig(tt.filePath)
			if tt.expectedError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedResult, result)
			}
		})
	}
}
