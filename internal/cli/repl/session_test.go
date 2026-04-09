package repl

import (
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
)

func TestSessionStoreSaveAndLoad(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "cli-session.json")
	store := SessionStore{Path: path}

	want := Cache{
		ProjectKey: "alpha",
		Mode:       ModeAct,
	}

	if err := store.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

func TestResolveStartupStateRestoresValidProjectAndMode(t *testing.T) {
	t.Parallel()

	registry := writeRegistry(t, map[string]string{
		"odin-core": "system_project",
		"alpha":     "github_backed_project",
	})

	state := ResolveStartupState(Cache{
		ProjectKey: "alpha",
		Mode:       ModeAct,
	}, registry)

	if state.Mode != ModeAct {
		t.Fatalf("Mode = %q, want %q", state.Mode, ModeAct)
	}
	if state.Scope.Kind != scope.ScopeProject || state.Scope.ProjectKey != "alpha" {
		t.Fatalf("Scope = %+v, want project alpha", state.Scope)
	}
}

func TestResolveStartupStateDowngradesInvalidProjectAndMode(t *testing.T) {
	t.Parallel()

	registry := writeRegistry(t, map[string]string{
		"odin-core": "system_project",
	})

	state := ResolveStartupState(Cache{
		ProjectKey: "missing-project",
		Mode:       ModeAct,
	}, registry)

	if state.Mode != ModeAsk {
		t.Fatalf("Mode = %q, want %q", state.Mode, ModeAsk)
	}
	if state.Scope.Kind != scope.ScopeGlobal {
		t.Fatalf("Scope = %+v, want global", state.Scope)
	}
}

func TestResolveStartupStateDowngradesActInGlobalScope(t *testing.T) {
	t.Parallel()

	state := ResolveStartupState(Cache{
		Mode: ModeAct,
	}, projects.Registry{})

	if state.Mode != ModeAsk {
		t.Fatalf("Mode = %q, want %q", state.Mode, ModeAsk)
	}
	if state.Scope.Kind != scope.ScopeGlobal {
		t.Fatalf("Scope = %+v, want global", state.Scope)
	}
}

func writeRegistry(t *testing.T, classes map[string]string) projects.Registry {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "projects.yaml")

	content := "version: 1\nprojects:\n"
	for key, class := range classes {
		gitRoot := filepath.Join(root, key)
		if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir git root: %v", err)
		}

		content += "  - key: " + key + "\n"
		content += "    name: " + key + "\n"
		content += "    project_class: " + class + "\n"
		if class == "system_project" {
			content += "    system_project: true\n"
		}
		content += "    git_root: " + gitRoot + "\n"
		content += "    default_branch: main\n"
		if class == "github_backed_project" {
			content += "    github:\n"
			content += "      repo: acme/" + key + "\n"
		}
		content += "    policy:\n"
		content += "      allowed_commands: [status]\n"
		content += "      branch_rules:\n"
		content += "        protected_branches: [main]\n"
		content += "        require_worktree: true\n"
		content += "        require_task_branch: true\n"
		content += "        allow_default_branch_mutation: false\n"
		content += "      approval_gates:\n"
		content += "        require_for_governance_changes: true\n"
		content += "        require_for_destructive_operations: true\n"
		if class == "system_project" {
			content += "        require_for_system_project_changes: true\n"
		} else {
			content += "        require_for_system_project_changes: false\n"
		}
		content += "      merge_policy:\n"
		content += "        mode: squash\n"
		content += "        allow_direct_to_default_branch: false\n"
		content += "      destructive_operations:\n"
		content += "        allow_reset: false\n"
		content += "        allow_clean: false\n"
		content += "        allow_force_push: false\n"
		content += "        require_explicit_approval: true\n"
	}

	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	registry, diagnostics, err := projects.Register(configPath)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Register() diagnostics = %#v", diagnostics)
	}

	return registry
}
