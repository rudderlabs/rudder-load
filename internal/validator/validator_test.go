package validator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"rudder-load/internal/parser"
)

func TestLoadTestConfig_Validate(t *testing.T) {
	tests := []struct {
		name           string
		config         *parser.LoadTestConfig
		wantErr        bool
		expectedErrMsg string
	}{
		{
			name: "valid config",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":       "source1,source2",
					"HOT_SOURCES":   "60,40",
					"HTTP_ENDPOINT": "https://example.com",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid namespace",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "Test_NS",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
			},
			wantErr:        true,
			expectedErrMsg: "namespace must contain only lowercase alphanumeric characters and '-'",
		},
		{
			name: "invalid name",
			config: &parser.LoadTestConfig{
				Name:      "test_load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
			},
			wantErr:        true,
			expectedErrMsg: "load name must contain only alphanumeric characters and '-'",
		},
		{
			name: "invalid duration",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "90", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":       "source1",
					"HOT_SOURCES":   "100",
					"HTTP_ENDPOINT": "https://example.com",
				},
			},
			wantErr:        true,
			expectedErrMsg: "duration must include 'h', 'm', or 's' (e.g., '1h30m')",
		},
		{
			name: "negative duration",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "-90", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":       "source1",
					"HOT_SOURCES":   "100",
					"HTTP_ENDPOINT": "https://example.com",
				},
			},
			wantErr:        true,
			expectedErrMsg: "duration must include 'h', 'm', or 's' (e.g., '1h30m')",
		},
		{
			name: "invalid replicas",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 0},
				},
				EnvOverrides: map[string]string{
					"SOURCES":       "source1",
					"HOT_SOURCES":   "100",
					"HTTP_ENDPOINT": "https://example.com",
				},
			},
			wantErr:        true,
			expectedErrMsg: "replicas must be greater than 0",
		},
		{
			name: "negative replicas",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: -1},
				},
				EnvOverrides: map[string]string{
					"SOURCES":       "source1",
					"HOT_SOURCES":   "100",
					"HTTP_ENDPOINT": "https://example.com",
				},
			},
			wantErr:        true,
			expectedErrMsg: "replicas must be greater than 0",
		},
		{
			name: "empty sources",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES": "",
				},
			},
			wantErr:        true,
			expectedErrMsg: "invalid sources: contains empty source: ",
		},
		{
			name: "multiple empty sources",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES": ",",
				},
			},
			wantErr:        true,
			expectedErrMsg: "invalid sources: contains empty source: ,",
		},
		{
			name: "invalid http endpoint",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":       "source1,source2",
					"HOT_SOURCES":   "50,50",
					"HTTP_ENDPOINT": "not-a-url",
				},
			},
			wantErr:        true,
			expectedErrMsg: "invalid http endpoint: not-a-url",
		},
		{
			name: "valid hot sources config",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":       "source1,source2",
					"HOT_SOURCES":   "60,40",
					"HTTP_ENDPOINT": "https://example.com",
				},
			},
			wantErr: false,
		},
		{
			name: "valid hot sources with one source",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":       "source1",
					"HOT_SOURCES":   "100",
					"HTTP_ENDPOINT": "https://example.com",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid hot sources - negative percentage",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":     "source1,source2",
					"HOT_SOURCES": "-60,40",
				},
			},
			wantErr:        true,
			expectedErrMsg: "hot sources percentage must be between 0 and 100: -60,40",
		},
		{
			name: "invalid hot sources - percentage over 100",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":     "source1,source2",
					"HOT_SOURCES": "120,40",
				},
			},
			wantErr:        true,
			expectedErrMsg: "hot sources percentage must be between 0 and 100: 120,40",
		},
		{
			name: "invalid hot sources - sum not 100",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":     "source1,source2",
					"HOT_SOURCES": "60,20",
				},
			},
			wantErr:        true,
			expectedErrMsg: "hot sources percentages must sum up to 100: 60,20",
		},
		{
			name: "invalid hot sources - length mismatch with sources",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":     "source1,source2",
					"HOT_SOURCES": "60,20,20",
				},
			},
			wantErr:        true,
			expectedErrMsg: "sources and hot sources must have the same length: source1,source2, 60,20,20",
		},
		{
			name: "invalid hot sources - non-numeric value",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":     "source1,source2",
					"HOT_SOURCES": "60,abc",
				},
			},
			wantErr:        true,
			expectedErrMsg: "hot sources percentage must be an integer: 60,abc",
		},
		{
			name: "invalid hot sources - empty string",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":     "source1",
					"HOT_SOURCES": "",
				},
			},
			wantErr:        true,
			expectedErrMsg: "invalid hot sources: ",
		},
		{
			name: "invalid hot sources - only comma",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":     "source1,source2",
					"HOT_SOURCES": ",",
				},
			},
			wantErr:        true,
			expectedErrMsg: "invalid hot sources: ,",
		},
		{
			name: "invalid hot sources - contains whitespace",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":     "source1,source2",
					"HOT_SOURCES": "60, 40",
				},
			},
			wantErr:        true,
			expectedErrMsg: "hot sources percentage must be an integer: 60, 40",
		},
		{
			name: "invalid hot sources - empty value between commas",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":     "source1,source2,source3",
					"HOT_SOURCES": "60,,40",
				},
			},
			wantErr:        true,
			expectedErrMsg: "invalid hot sources: 60,,40",
		},
		{
			name: "invalid hot sources - trailing comma",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":     "source1,source2,source3",
					"HOT_SOURCES": "60,40,",
				},
			},
			wantErr:        true,
			expectedErrMsg: "invalid hot sources: 60,40,",
		},
		{
			name: "invalid hot sources - decimal number",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
				EnvOverrides: map[string]string{
					"SOURCES":     "source1,source2",
					"HOT_SOURCES": "60.5,39.5",
				},
			},
			wantErr:        true,
			expectedErrMsg: "hot sources percentage must be an integer: 60.5,39.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLoadTestConfig(tt.config)
			if tt.wantErr {
				require.ErrorContains(t, err, tt.expectedErrMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHttpEndpointValidator(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantErr  bool
	}{
		{
			name:     "valid http endpoint",
			endpoint: "http://example.com",
			wantErr:  false,
		},
		{
			name:     "valid https endpoint",
			endpoint: "https://example.com",
			wantErr:  false,
		},
		{
			name:     "valid endpoint with path",
			endpoint: "https://example.com/api/v1",
			wantErr:  false,
		},
		{
			name:     "valid endpoint with subdomain",
			endpoint: "https://api.example.com",
			wantErr:  false,
		},
		{
			name:     "valid endpoint with port",
			endpoint: "https://example.com:8080",
			wantErr:  false,
		},
		{
			name:     "valid endpoint with query params",
			endpoint: "https://example.com/api?v=1",
			wantErr:  false,
		},
		{
			name:     "invalid endpoint - missing protocol",
			endpoint: "example.com",
			wantErr:  true,
		},
		{
			name:     "invalid endpoint - wrong protocol",
			endpoint: "ftp://example.com",
			wantErr:  true,
		},
		{
			name:     "invalid endpoint - empty string",
			endpoint: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHttpEndpoint(tt.endpoint)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
