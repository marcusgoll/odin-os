package commands

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	coreprojects "odin-os/internal/core/projects"
	"odin-os/internal/store/sqlite"
)

func TestRunProjectListShowsTransitionAndEligibility(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openProjectTestStore(t)
	defer store.Close()

	registry := writeProjectRegistry(t, map[string]string{"alpha": "main"})
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       registry.Projects()[0].GitRoot,
		DefaultBranch: "main",
		ManifestPath:  registry.ConfigPath(),
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}
	if _, err := store.SetProjectTransition(ctx, sqlite.SetProjectTransitionParams{
		ProjectID:  project.ID,
		State:      "shadow",
		Controller: "legacy_odin",
		ChangedBy:  "test",
		Notes:      "shadow testing",
	}); err != nil {
		t.Fatalf("SetProjectTransition(alpha) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := RunProject(ctx, store, registry, []string{"list"}, &stdout); err != nil {
		t.Fatalf("RunProject(list) error = %v", err)
	}

	output := stdout.String()
	for _, want := range []string{"project=alpha", "transition=shadow", "workspace=eligible"} {
		if !strings.Contains(output, want) {
			t.Fatalf("list output = %q, want substring %q", output, want)
		}
	}
}

func TestRunProjectShowJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openProjectTestStore(t)
	defer store.Close()

	registry := writeProjectRegistry(t, map[string]string{"alpha": "main"})

	var stdout bytes.Buffer
	if err := RunProject(ctx, store, registry, []string{"show", "alpha", "--json"}, &stdout); err != nil {
		t.Fatalf("RunProject(show --json) error = %v", err)
	}

	output := stdout.String()
	for _, want := range []string{`"key": "alpha"`, `"workspace_eligible": true`, `"transition_state": "inventory"`} {
		if !strings.Contains(output, want) {
			t.Fatalf("show output = %q, want substring %q", output, want)
		}
	}
}

func TestRunProjectEnrollExplicitArgs(t *testing.T) {
	ctx := context.Background()
	store := openProjectTestStore(t)
	defer store.Close()

	registry := writeProjectRegistry(t, map[string]string{"odin-core": "main"})
	projectRoot := filepath.Join(t.TempDir(), "family-ops")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := RunProject(ctx, store, registry, []string{
		"enroll",
		"family-ops",
		"git_root=" + projectRoot,
		"default_branch=main",
		"name=Family-Ops",
	}, &stdout); err != nil {
		t.Fatalf("RunProject(enroll) error = %v", err)
	}

	updated, diagnostics, err := coreprojects.Register(registry.ConfigPath())
	if err != nil {
		t.Fatalf("Register(updated) error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Register(updated) diagnostics = %#v", diagnostics)
	}
	if _, ok := updated.Lookup("family-ops"); !ok {
		t.Fatalf("expected enrolled project in registry")
	}
	if !strings.Contains(stdout.String(), "project=family-ops") {
		t.Fatalf("enroll output = %q, want project summary", stdout.String())
	}
}

func TestRunProjectUpdateUsesRepoHintsWhenNoFieldsProvided(t *testing.T) {
	ctx := context.Background()
	store := openProjectTestStore(t)
	defer store.Close()

	registry := writeProjectRegistry(t, map[string]string{"alpha": "main"})
	newRoot := filepath.Join(t.TempDir(), "alpha-moved")
	if err := os.MkdirAll(filepath.Join(newRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll(newRoot) error = %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWD)
	}()
	if err := os.Chdir(newRoot); err != nil {
		t.Fatalf("Chdir(newRoot) error = %v", err)
	}

	initGitRepo(t, newRoot, "develop")

	var stdout bytes.Buffer
	if err := RunProject(ctx, store, registry, []string{"update", "alpha"}, &stdout); err != nil {
		t.Fatalf("RunProject(update) error = %v", err)
	}

	updated, diagnostics, err := coreprojects.Register(registry.ConfigPath())
	if err != nil {
		t.Fatalf("Register(updated) error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Register(updated) diagnostics = %#v", diagnostics)
	}
	alpha, ok := updated.Lookup("alpha")
	if !ok {
		t.Fatalf("expected alpha lookup")
	}
	if alpha.GitRoot != newRoot {
		t.Fatalf("GitRoot = %q, want %q", alpha.GitRoot, newRoot)
	}
	if alpha.DefaultBranch != "develop" {
		t.Fatalf("DefaultBranch = %q, want develop", alpha.DefaultBranch)
	}
	if !strings.Contains(stdout.String(), "default_branch=develop") {
		t.Fatalf("update output = %q, want develop branch", stdout.String())
	}
}

func openProjectTestStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func writeProjectRegistry(t *testing.T, branches map[string]string) coreprojects.Registry {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "projects.yaml")
	builder := &strings.Builder{}
	builder.WriteString("version: 1\nprojects:\n")
	for key, branch := range branches {
		projectRoot := filepath.Join(root, key)
		if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
			t.Fatalf("MkdirAll(projectRoot) error = %v", err)
		}
		builder.WriteString("  - key: " + key + "\n")
		builder.WriteString("    name: " + projectDisplayName(key) + "\n")
		builder.WriteString("    project_class: local_git_project\n")
		builder.WriteString("    git_root: " + projectRoot + "\n")
		builder.WriteString("    default_branch: " + branch + "\n")
		builder.WriteString("    policy:\n")
		builder.WriteString("      allowed_commands: [status]\n")
		builder.WriteString("      branch_rules:\n")
		builder.WriteString("        protected_branches: [main]\n")
		builder.WriteString("        require_worktree: true\n")
		builder.WriteString("        require_task_branch: true\n")
		builder.WriteString("        allow_default_branch_mutation: false\n")
		builder.WriteString("      approval_gates:\n")
		builder.WriteString("        require_for_governance_changes: true\n")
		builder.WriteString("        require_for_destructive_operations: true\n")
		builder.WriteString("        require_for_system_project_changes: false\n")
		builder.WriteString("      merge_policy:\n")
		builder.WriteString("        mode: squash\n")
		builder.WriteString("        allow_direct_to_default_branch: false\n")
		builder.WriteString("      destructive_operations:\n")
		builder.WriteString("        allow_reset: false\n")
		builder.WriteString("        allow_clean: false\n")
		builder.WriteString("        allow_force_push: false\n")
		builder.WriteString("        require_explicit_approval: true\n")
	}
	if err := os.WriteFile(configPath, []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("WriteFile(projects.yaml) error = %v", err)
	}

	registry, diagnostics, err := coreprojects.Register(configPath)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Register() diagnostics = %#v", diagnostics)
	}
	return registry
}

func initGitRepo(t *testing.T, repoRoot string, branch string) {
	t.Helper()

	runGitCommand(t, repoRoot, "init", "-b", branch)
}

func projectDisplayName(key string) string {
	parts := strings.Fields(strings.ReplaceAll(strings.TrimSpace(key), "-", " "))
	for index, part := range parts {
		if part == "" {
			continue
		}
		parts[index] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()

	commandArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", commandArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v error = %v\n%s", args, err, output)
	}
}
