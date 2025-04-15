package validator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"rudder-load/internal/parser"
)

func TestLoadTestConfig_Validate(t *testing.T) {
	tests := []struct {
		name     string
		config   *parser.LoadTestConfig
		envSetup func()
		wantErr  bool
	}{
		{
			name: "valid config",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
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
			wantErr: true,
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
			wantErr: true,
		},
		{
			name: "invalid duration",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "90", Replicas: 2},
				},
			},
			wantErr: true,
		},
		{
			name: "negative duration",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "-90", Replicas: 2},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid replicas",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 0},
				},
			},
			wantErr: true,
		},
		{
			name: "negative replicas",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: -1},
				},
			},
			wantErr: true,
		},
		{
			name: "empty sources",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
			},
			envSetup: func() {
				t.Setenv("SOURCES", "")
			},
			wantErr: true,
		},
		{
			name: "multiple empty sources",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
			},
			envSetup: func() {
				t.Setenv("SOURCES", ",")
			},
			wantErr: true,
		},
		{
			name: "invalid http endpoint",
			config: &parser.LoadTestConfig{
				Name:      "test-load",
				Namespace: "test-ns",
				Phases: []parser.RunPhase{
					{Duration: "1h30m", Replicas: 2},
				},
			},
			envSetup: func() {
				t.Setenv("HTTP_ENDPOINT", "not-a-url")
			},
			wantErr: true,
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
					"HOT_SOURCES": "60,40",
				},
			},
			envSetup: func() {
				t.Setenv("SOURCES", "source1,source2")
				t.Setenv("HTTP_ENDPOINT", "https://example.com")
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
					"HOT_SOURCES": "100",
				},
			},
			envSetup: func() {
				t.Setenv("SOURCES", "source1")
				t.Setenv("HTTP_ENDPOINT", "https://example.com")
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
					"HOT_SOURCES": "-60,40",
				},
			},
			wantErr: true,
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
					"HOT_SOURCES": "120,40",
				},
			},
			wantErr: true,
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
					"HOT_SOURCES": "60,20",
				},
			},
			wantErr: true,
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
					"HOT_SOURCES": "60,20,20",
				},
			},
			wantErr: true,
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
					"HOT_SOURCES": "60,abc",
				},
			},
			wantErr: true,
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
					"HOT_SOURCES": "",
				},
			},
			wantErr: true,
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
					"HOT_SOURCES": ",",
				},
			},
			wantErr: true,
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
					"HOT_SOURCES": "60, 40",
				},
			},
			wantErr: true,
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
					"HOT_SOURCES": "60,,40",
				},
			},
			wantErr: true,
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
					"HOT_SOURCES": "60,40,",
				},
			},
			wantErr: true,
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
					"HOT_SOURCES": "60.5,39.5",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			if tt.envSetup != nil {
				tt.envSetup()
			}

			err := ValidateLoadTestConfig(tt.config)
			if tt.wantErr {
				require.Error(t, err)
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
