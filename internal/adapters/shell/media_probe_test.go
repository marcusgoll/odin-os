package shell

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMediaProbeParsesSignalOutput(t *testing.T) {
	t.Parallel()

	script := writeProbeScript(t, `{"signals":[{"name":"media.mounts","status":"failed","summary":"mount mismatch"}]}`)

	output, err := MediaProbe{}.Run(context.Background(), script)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(output.Signals) != 1 {
		t.Fatalf("Signals = %+v, want one signal", output.Signals)
	}
	if output.Signals[0].Name != "media.mounts" {
		t.Fatalf("signal name = %q, want media.mounts", output.Signals[0].Name)
	}
}

func TestMediaProbeRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	script := writeProbeScript(t, `not-json`)

	_, err := MediaProbe{}.Run(context.Background(), script)
	if err == nil {
		t.Fatalf("Run() error = nil, want invalid JSON failure")
	}
}

func writeProbeScript(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "probe.sh")
	content := "#!/usr/bin/env bash\ncat <<'EOF'\n" + body + "\nEOF\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
