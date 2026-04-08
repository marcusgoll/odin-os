package lifecycle

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunWritesScaffoldMessage(t *testing.T) {
	var stdout bytes.Buffer

	err := Run(context.Background(), &stdout)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Odin OS scaffold") {
		t.Fatalf("Run() output %q does not contain scaffold message", output)
	}
}
