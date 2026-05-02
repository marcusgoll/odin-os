package commands

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"odin-os/internal/runtime/triggers"
)

func TestRunTriggerHelpPrintsUsage(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	err := RunTrigger(context.Background(), triggers.Service{}, []string{"--help"}, &stdout)
	if err != nil {
		t.Fatalf("RunTrigger(--help) error = %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "usage: odin trigger") {
		t.Fatalf("stdout = %q, want trigger usage", got)
	}
}

func TestRunTriggerNoArgsReturnsUsageError(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	err := RunTrigger(context.Background(), triggers.Service{}, nil, &stdout)
	if err == nil {
		t.Fatal("RunTrigger() error = nil, want usage error")
	}
	if got := err.Error(); !strings.Contains(got, "usage: odin trigger") {
		t.Fatalf("error = %q, want trigger usage", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty output", stdout.String())
	}
}
