package projects

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestListSpecFlowArtifactsReturnsKnownEvidenceFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeArtifact(t, filepath.Join(root, "specs", "001-login", "spec.md"), []byte("spec"))
	writeArtifact(t, filepath.Join(root, "specs", "001-login", "plan.md"), []byte("plan"))
	writeArtifact(t, filepath.Join(root, "specs", "001-login", "tasks.md"), []byte("tasks"))
	writeArtifact(t, filepath.Join(root, "epics", "auth", "state.yaml"), []byte("state: active\n"))
	writeArtifact(t, filepath.Join(root, "specs", "001-login", "notes.md"), []byte("notes"))

	artifacts, err := ListSpecFlowArtifacts(root)
	if err != nil {
		t.Fatalf("ListSpecFlowArtifacts() error = %v", err)
	}

	got := artifactPaths(artifacts)
	for _, want := range []string{
		"epics/auth/state.yaml",
		"specs/001-login/plan.md",
		"specs/001-login/spec.md",
		"specs/001-login/tasks.md",
	} {
		if !slices.Contains(got, want) {
			t.Fatalf("artifacts = %#v, want %s", got, want)
		}
	}
	if slices.Contains(got, "specs/001-login/notes.md") {
		t.Fatalf("artifacts = %#v, want notes.md ignored", got)
	}
}

func TestListSpecFlowArtifactsHandlesMissingRoots(t *testing.T) {
	t.Parallel()

	artifacts, err := ListSpecFlowArtifacts(t.TempDir())
	if err != nil {
		t.Fatalf("ListSpecFlowArtifacts() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("artifacts = %#v, want none", artifacts)
	}
}

func artifactPaths(artifacts []SpecFlowArtifact) []string {
	paths := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		paths = append(paths, artifact.Path)
	}
	return paths
}

func writeArtifact(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
