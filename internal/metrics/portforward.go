package metrics

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/rudderlabs/rudder-go-kit/logger"
)

type PortForwarder struct {
	timeout        time.Duration
	cmd            *exec.Cmd
	commandCreator func(ctx context.Context, name string, arg ...string) *exec.Cmd
	logger         logger.Logger
}

func NewPortForwarder(timeout time.Duration, logger logger.Logger) *PortForwarder {
	return &PortForwarder{
		timeout:        timeout,
		commandCreator: exec.CommandContext,
		logger:         logger,
	}
}

func (p *PortForwarder) Start(ctx context.Context, namespace string) error {
	// TODO: Make localPort configurable and use free port for remotePort
	// e.g. https://github.com/rudderlabs/rudder-go-kit/blob/v0.48.2/testhelper/freeport.go#L14
	const localPort = 9898
	const remotePort = 8080

	p.cmd = p.commandCreator(ctx, "kubectl", "port-forward", "service/query-frontend", "-n", namespace, fmt.Sprintf("%d:%d", localPort, remotePort))
	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start port-forward command: %w", err)
	}

	p.logger.Infon("Waiting for port-forward to become ready...")
	deadline := time.Now().Add(p.timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if p.cmd.Process != nil {
			if p.cmd.ProcessState != nil && p.cmd.ProcessState.Exited() {
				return fmt.Errorf("port-forward process exited prematurely")
			}

			// TODO: check health endpoint here for more certainty
			p.logger.Infon("Port-forward started",
				logger.NewIntField("port", localPort), logger.NewIntField("pid", int64(p.cmd.Process.Pid)),
			)
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Continue to next iteration
		}
	}
	return fmt.Errorf("timeout waiting for port-forward to start after %v", p.timeout)
}

func (p *PortForwarder) Stop() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}

	if err := p.cmd.Process.Kill(); err != nil {
		return err
	}

	return nil
}
