package validator

import (
	"testing"
	"github.com/stretchr/testify/require"

	"rudder-load/internal/parser"
)

func TestLoadTestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *parser.LoadTestConfig
		wantErr bool
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLoadTestConfig(tt.config)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}