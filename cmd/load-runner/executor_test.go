package main

import (
	"context"
	"testing"
	"time"
)

func TestDefaultCommandExecutor_Run(t *testing.T) {
	executor := &DefaultCommandExecutor{}

	t.Run("successful command execution", func(t *testing.T) {
		ctx := context.Background()
		// Using "echo" as it's available on most systems
		err := executor.run(ctx, "echo", "test")
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	})

	t.Run("command not found", func(t *testing.T) {
		ctx := context.Background()
		err := executor.run(ctx, "nonexistentcommand")
		if err == nil {
			t.Error("Expected error for nonexistent command, got nil")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		// Using "sleep" command that will exceed the timeout
		err := executor.run(ctx, "sleep", "1")
		if err == nil {
			t.Error("Expected error due to context cancellation, got nil")
		}
	})
}
