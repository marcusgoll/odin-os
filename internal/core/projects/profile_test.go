package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectSpecFlowProfileRequiresMultipleStrongSignals(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".claude", "commands"))

	profile := DetectProjectProfile(root)
	if profile.SpecFlowCompatible {
		t.Fatalf("SpecFlowCompatible = true for a Claude-only project")
	}

	mustMkdir(t, filepath.Join(root, ".spec-flow"))
	mustWrite(t, filepath.Join(root, "CLAUDE.md"), []byte("# Project instructions\n"))

	profile = DetectProjectProfile(root)
	if !profile.SpecFlowCompatible {
		t.Fatalf("SpecFlowCompatible = false, want true")
	}
	if len(profile.Evidence) < 3 {
		t.Fatalf("Evidence = %#v, want at least three strong signals", profile.Evidence)
	}
}

func TestDetectSpecFlowProfileRequiresSpecFlowDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".claude", "commands"))
	mustMkdir(t, filepath.Join(root, "specs"))
	mustMkdir(t, filepath.Join(root, "epics"))
	mustWrite(t, filepath.Join(root, "CLAUDE.md"), []byte("# Project instructions\n"))

	profile := DetectProjectProfile(root)
	if profile.SpecFlowCompatible {
		t.Fatalf("SpecFlowCompatible = true without .spec-flow/")
	}
}

func TestDetectSpecFlowProfileHandlesMissingRoot(t *testing.T) {
	t.Parallel()

	profile := DetectProjectProfile("")
	if profile.SpecFlowCompatible {
		t.Fatalf("SpecFlowCompatible = true for empty root")
	}
	if len(profile.Evidence) != 0 {
		t.Fatalf("Evidence = %#v, want none", profile.Evidence)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
}

func mustWrite(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
