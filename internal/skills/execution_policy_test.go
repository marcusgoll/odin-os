package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveHandlerPathAllowsHandlerUnderScriptsSkills(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "allowed-skill.sh")
	writeExecutable(t, handlerPath, "#!/usr/bin/env bash\nprintf '%s\\n' '{\"status\":\"ok\"}'\n")

	resolved, err := service.resolveHandlerPath("scripts/skills/allowed-skill.sh")
	if err != nil {
		t.Fatalf("resolveHandlerPath() error = %v", err)
	}
	if resolved != handlerPath {
		t.Fatalf("resolveHandlerPath() = %q, want %q", resolved, handlerPath)
	}
}

func TestResolveHandlerPathDeniesHandlerElsewhereInRepo(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "other", "outside-skill.sh")
	writeExecutable(t, handlerPath, "#!/usr/bin/env bash\nprintf '%s\\n' '{\"status\":\"ok\"}'\n")

	_, err := service.resolveHandlerPath("scripts/other/outside-skill.sh")
	if err == nil {
		t.Fatal("resolveHandlerPath() error = nil, want denial")
	}
	if !strings.Contains(err.Error(), "scripts/skills") {
		t.Fatalf("resolveHandlerPath() error = %v, want allowed-root denial", err)
	}
}

func TestResolveHandlerPathDeniesBlankHandlerRef(t *testing.T) {
	t.Parallel()

	service := newTestService(t)

	for _, handlerRef := range []string{"", "   \t\n  "} {
		handlerRef := handlerRef
		t.Run(strings.TrimSpace(handlerRef), func(t *testing.T) {
			t.Parallel()

			_, err := service.resolveHandlerPath(handlerRef)
			if err == nil {
				t.Fatal("resolveHandlerPath() error = nil, want denial")
			}
			if !strings.Contains(err.Error(), "required") {
				t.Fatalf("resolveHandlerPath() error = %v, want required-field denial", err)
			}
		})
	}
}

func TestResolveHandlerPathDeniesSymlinkEscapingScriptsSkills(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	targetPath := filepath.Join(service.RepoRoot, "scripts", "other", "outside-skill.sh")
	writeExecutable(t, targetPath, "#!/usr/bin/env bash\nprintf '%s\\n' '{\"status\":\"ok\"}'\n")

	symlinkPath := filepath.Join(service.RepoRoot, "scripts", "skills", "symlink-skill.sh")
	if err := os.MkdirAll(filepath.Dir(symlinkPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(symlinkPath), err)
	}
	if err := os.Symlink("../other/outside-skill.sh", symlinkPath); err != nil {
		t.Fatalf("writeSymlink() error = %v", err)
	}

	_, err := service.resolveHandlerPath("scripts/skills/symlink-skill.sh")
	if err == nil {
		t.Fatal("resolveHandlerPath() error = nil, want denial")
	}
	if !strings.Contains(err.Error(), "scripts/skills") {
		t.Fatalf("resolveHandlerPath() error = %v, want allowed-root denial", err)
	}
}
