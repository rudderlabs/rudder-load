package main

import (
	"context"
	"os"
	"os/exec"
)

type CommandExecutor interface {
	run(ctx context.Context, name string, args ...string) error
}

type commandExecutor struct{}

func (d *commandExecutor) run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
