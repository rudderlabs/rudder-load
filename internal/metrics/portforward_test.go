package metrics

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rudderlabs/rudder-go-kit/logger"
)

func TestPortForward_Start(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		commandCreator func(ctx context.Context, name string, arg ...string) *exec.Cmd
		expectedError  string
	}{
		{
			name:      "successful start",
			namespace: "test-namespace",
			commandCreator: func(ctx context.Context, name string, arg ...string) *exec.Cmd {
				cmd := exec.Command("echo", "mock command")
				return cmd
			},
		},
		{
			name:      "command failed to start",
			namespace: "test-namespace",
			commandCreator: func(ctx context.Context, name string, arg ...string) *exec.Cmd {
				cmd := exec.Command("false")
				cmd.Process = &os.Process{Pid: 0}
				return cmd
			},
			expectedError: "exec: already started",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pf := NewPortForwarder(time.Millisecond*1, logger.NOP)
			pf.commandCreator = tt.commandCreator
			ctx := context.Background()
			err := pf.Start(ctx, tt.namespace)

			if tt.expectedError != "" {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if pf.cmd == nil {
				t.Error("expected cmd to be set")
			}
		})
	}
}

func TestPortForward_Stop(t *testing.T) {
	tests := []struct {
		name          string
		setupCmd      func() *exec.Cmd
		expectedError string
	}{
		{
			name: "successful stop",
			setupCmd: func() *exec.Cmd {
				cmd := exec.Command("sleep", "10")
				cmd.Process = &os.Process{Pid: 12345}
				return cmd
			},
		},
		{
			name: "no process to stop",
			setupCmd: func() *exec.Cmd {
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pf := NewPortForwarder(time.Millisecond*1, logger.NOP)
			pf.cmd = tt.setupCmd()
			pf.Start(context.Background(), "test-namespace")
			err := pf.Stop()

			if tt.expectedError != "" {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error to contain %q, got %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
