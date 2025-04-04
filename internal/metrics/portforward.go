package metrics

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

type PortForward struct {
	sleepDuration  time.Duration
	cmd            *exec.Cmd
	commandCreator func(ctx context.Context, name string, arg ...string) *exec.Cmd
}

func NewPortForward(sleepDuration time.Duration) *PortForward {
	return &PortForward{
		sleepDuration:  sleepDuration,
		commandCreator: exec.CommandContext,
	}
}

func (p *PortForward) Start(ctx context.Context, namespace string) error {
	p.cmd = p.commandCreator(ctx, "kubectl", "port-forward", "service/query-frontend", "-n", namespace, "9898:8080")
	if err := p.cmd.Start(); err != nil {
		return err
	}

	fmt.Println("Waiting for port-forward to start")
	time.Sleep(p.sleepDuration)

	if p.cmd.Process == nil {
		return fmt.Errorf("port-forward process not started")
	}

	fmt.Println("Port-forward started on port 9898 with pid", p.cmd.Process.Pid)

	return nil
}

func (p *PortForward) Stop() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}

	if err := p.cmd.Process.Kill(); err != nil {
		return err
	}

	return nil
}
