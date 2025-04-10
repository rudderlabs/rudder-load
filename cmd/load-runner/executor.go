package main

import (
	"context"
	"os"
	"os/exec"
)

type CommandExecutor struct{}

func (d *CommandExecutor) run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
