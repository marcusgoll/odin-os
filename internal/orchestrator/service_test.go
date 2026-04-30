package orchestrator

import (
	"context"
	"errors"
	"testing"
)

func TestServiceRunOnceIsPlaceholder(t *testing.T) {
	t.Parallel()

	err := NewService().RunOnce(context.Background())
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("RunOnce() error = %v, want %v", err, ErrNotImplemented)
	}
}
