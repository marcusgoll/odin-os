package appserver

import (
	"context"
	"errors"
	"testing"

	"odin-os/internal/runner"
)

func TestRunCurrentlyRemainsBehindPlaceholderBoundary(t *testing.T) {
	t.Parallel()

	result, err := NewAdapter().Run(context.Background(), runner.Request{
		WorkItemID: "work-123",
		Role:       "builder",
		Worktree:   "/tmp/odin/worktrees/work-123",
		Prompt:     "implement the issue",
	})
	if !errors.Is(err, runner.ErrNotImplemented) {
		t.Fatalf("Run() error = %v, want %v", err, runner.ErrNotImplemented)
	}
	if result != (runner.Result{}) {
		t.Fatalf("Run() result = %#v, want zero value", result)
	}
}
