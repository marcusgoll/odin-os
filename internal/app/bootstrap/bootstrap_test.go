package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"odin-os/internal/registry"
)

func bootstrapRepoRoot(t *testing.T) string {
	t.Helper()

	sourceRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	if os.Getenv("ODIN_FORCE_PORTABLE_TEST_REPO") != "1" {
		if _, err := os.Stat("/home/orchestrator/pbs/.git"); err == nil {
			return sourceRoot
		}
	}

	root := t.TempDir()
	for _, dir := range []string{
		filepath.Join(root, ".git"),
		filepath.Join(root, "config"),
		filepath.Join(root, "registry", "agents"),
		filepath.Join(root, "registry", "skills"),
		filepath.Join(root, "registry", "workflows"),
		filepath.Join(root, "registry", "commands"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "config", "projects.yaml"), []byte(`
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: ..
    default_branch: main
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: true
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`), 0o644); err != nil {
		t.Fatalf("WriteFile(projects.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "executors.yaml"), []byte("version: 1\nexecutors: []\nroutes: []\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(executors.yaml) error = %v", err)
	}
	return root
}

func TestLoadInitializesFreshRuntimeReadinessState(t *testing.T) {
	repoRoot := bootstrapRepoRoot(t)
	runtimeRoot := t.TempDir()

	app, err := Load(context.Background(), repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	if len(app.RegistryDiagnostics) != 0 {
		t.Fatalf("RegistryDiagnostics = %+v, want none", app.RegistryDiagnostics)
	}

	assertCountAtLeast(t, app.Store.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM registry_versions"), 1)
	assertCountAtLeast(t, app.Store.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM executor_health"), 1)
	assertCountAtLeast(t, app.Store.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM projection_freshness"), 1)
}

func TestLoadRecordsExpectedExecutorHealthEvenWhenUnavailable(t *testing.T) {
	repoRoot := bootstrapRepoRoot(t)
	runtimeRoot := t.TempDir()
	t.Setenv("ODIN_CODEX_DRIVER", filepath.Join(runtimeRoot, "missing-codex-driver"))

	app, err := Load(context.Background(), repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	var status string
	if err := app.Store.DB().QueryRowContext(context.Background(), `
		SELECT status
		FROM executor_health
		WHERE executor = 'codex_headless'
		ORDER BY checked_at DESC, id DESC
		LIMIT 1
	`).Scan(&status); err != nil {
		t.Fatalf("query expected executor health error = %v", err)
	}
	if status != "unavailable" {
		t.Fatalf("codex_headless status = %q, want unavailable", status)
	}
}

func TestBootstrapRetainsCapabilityService(t *testing.T) {
	repoRoot := bootstrapRepoRoot(t)
	runtimeRoot := t.TempDir()

	app, err := Load(context.Background(), repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	if app.CapabilityService == nil {
		t.Fatal("CapabilityService = nil, want live service")
	}

	active := app.CapabilityService.Active()
	if active.Digest == "" {
		t.Fatal("Active().Digest = empty, want snapshot digest")
	}
	if len(active.Diagnostics) != 0 {
		t.Fatalf("Active().Diagnostics = %+v, want none", active.Diagnostics)
	}
}

func TestSnapshotDigestIgnoresAbsoluteSourcePaths(t *testing.T) {
	base := registry.Snapshot{
		Items: []registry.Item{
			{
				Kind:   registry.KindSkill,
				Key:    "skill.example",
				Title:  "Example Skill",
				Scopes: []string{"project"},
				Source: registry.SourceInfo{
					Path:         "/tmp/a/registry/skills/example.md",
					RelativePath: "skills/example.md",
				},
			},
		},
		Diagnostics: []registry.Diagnostic{
			{
				Path:    "/tmp/a/registry/skills/example.md",
				Code:    "read_error",
				Message: "registry file could not be read",
			},
		},
	}

	other := registry.Snapshot{
		Items: []registry.Item{
			{
				Kind:   registry.KindSkill,
				Key:    "skill.example",
				Title:  "Example Skill",
				Scopes: []string{"project"},
				Source: registry.SourceInfo{
					Path:         "/opt/alt/registry/skills/example.md",
					RelativePath: "skills/example.md",
				},
			},
		},
		Diagnostics: []registry.Diagnostic{
			{
				Path:    "/opt/alt/registry/skills/example.md",
				Code:    "read_error",
				Message: "registry file could not be read",
			},
		},
	}

	baseDigest, err := snapshotDigest(base)
	if err != nil {
		t.Fatalf("snapshotDigest(base) error = %v", err)
	}
	otherDigest, err := snapshotDigest(other)
	if err != nil {
		t.Fatalf("snapshotDigest(other) error = %v", err)
	}

	if baseDigest != otherDigest {
		t.Fatalf("snapshotDigest() = %q and %q, want equal", baseDigest, otherDigest)
	}
}

func TestLoadIncludesConfiguredProjectsOverlay(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeRoot := t.TempDir()

	for _, dir := range []string{
		filepath.Join(repoRoot, "config"),
		filepath.Join(repoRoot, "registry", "agents"),
		filepath.Join(repoRoot, "registry", "skills"),
		filepath.Join(repoRoot, "registry", "workflows"),
		filepath.Join(repoRoot, "registry", "commands"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}
	for _, gitRoot := range []string{
		filepath.Join(repoRoot, "repo"),
		filepath.Join(repoRoot, "local-demo"),
	} {
		if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Join(gitRoot, ".git"), err)
		}
	}

	if err := os.WriteFile(filepath.Join(repoRoot, "config", "projects.yaml"), []byte(`
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: ../repo
    default_branch: main
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: true
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`), 0o644); err != nil {
		t.Fatalf("WriteFile(projects.yaml) error = %v", err)
	}

	overlayPath := filepath.Join(repoRoot, "projects.overlay.yaml")
	if err := os.WriteFile(overlayPath, []byte(`
version: 1
projects:
  - key: local-demo
    name: Local Demo
    project_class: local_git_project
    git_root: local-demo
    default_branch: main
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: false
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`), 0o644); err != nil {
		t.Fatalf("WriteFile(overlay) error = %v", err)
	}

	for _, kind := range []string{"agents", "skills", "workflows", "commands"} {
		path := filepath.Join(repoRoot, "registry", kind, "example.md")
		var contents string
		switch kind {
		case "agents":
			contents = `---
kind: agent
key: example
title: Example
summary: Example
role: operator
scopes:
  - global
tools:
  - project_status
---

## Purpose
Example

## When to Use
Example

## Inputs
Example

## Procedure
Example

## Outputs
Example

## Constraints
Example

## Success Criteria
Example
`
		case "skills":
			contents = `---
kind: skill
key: example
title: Example
summary: Example
version: "1.0.0"
enabled: true
strictness: rigid
applies_to:
  - intake
scopes:
  - global
permissions:
  - repo.read
handler_type: command
handler_ref: scripts/skills/example.sh
timeout_seconds: 15
input_schema:
  type: object
output_schema:
  type: object
---

## Purpose
Example

## When to Use
Example

## Inputs
Example

## Procedure
Example

## Outputs
Example

## Constraints
Example

## Success Criteria
Example
`
		case "workflows":
			contents = `---
kind: workflow
key: example
title: Example
summary: Example
entrypoint: triage-agent
composes:
  - triage-skill
---

## Purpose
Example

## When to Use
Example

## Inputs
Example

## Procedure
Example

## Outputs
Example

## Constraints
Example

## Success Criteria
Example
`
		case "commands":
			contents = `---
kind: command
key: example
title: Example
summary: Example
command: example
scopes:
  - global
---

## Purpose
Example

## When to Use
Example

## Inputs
Example

## Procedure
Example

## Outputs
Example

## Constraints
Example

## Success Criteria
Example
`
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	if err := os.WriteFile(filepath.Join(repoRoot, "config", "executors.yaml"), []byte("version: 1\nexecutors: []\nroutes: []\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(executors.yaml) error = %v", err)
	}

	t.Setenv("ODIN_PROJECTS_OVERLAY", overlayPath)

	app, err := Load(context.Background(), repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	if _, ok := app.Registry.Lookup("local-demo"); !ok {
		t.Fatal("Lookup(local-demo) missing from overlay")
	}
}

func TestLoadReadOnlyDoesNotInitializeReadinessState(t *testing.T) {
	repoRoot := bootstrapRepoRoot(t)
	runtimeRoot := t.TempDir()

	app, err := LoadReadOnly(context.Background(), repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("LoadReadOnly() error = %v", err)
	}
	defer app.Store.Close()

	assertCountExactly(t, app.Store.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM registry_versions"), 0)
	assertCountExactly(t, app.Store.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM executor_health"), 0)
	assertCountExactly(t, app.Store.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM projection_freshness"), 0)
}

func TestLoadSerializesConcurrentBootstrapForFreshRuntime(t *testing.T) {
	repoRoot := bootstrapRepoRoot(t)
	runtimeRoot := t.TempDir()

	var entered int32
	lockAcquired := make(chan struct{})
	release := make(chan struct{})
	testBootstrapHooks.afterLockAcquired = func() {
		if atomic.AddInt32(&entered, 1) != 1 {
			return
		}
		close(lockAcquired)
		<-release
	}
	t.Cleanup(func() {
		testBootstrapHooks.afterLockAcquired = nil
	})

	type result struct {
		app App
		err error
	}

	firstResult := make(chan result, 1)
	secondResult := make(chan result, 1)

	go func() {
		app, err := Load(context.Background(), repoRoot, runtimeRoot)
		firstResult <- result{app: app, err: err}
	}()

	<-lockAcquired

	go func() {
		app, err := Load(context.Background(), repoRoot, runtimeRoot)
		secondResult <- result{app: app, err: err}
	}()

	select {
	case result := <-secondResult:
		if result.app.Store != nil {
			_ = result.app.Store.Close()
		}
		t.Fatalf("second Load() completed before first bootstrap released the lock: err=%v", result.err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	first := <-firstResult
	if first.err != nil {
		t.Fatalf("first Load() error = %v", first.err)
	}
	defer first.app.Store.Close()

	second := <-secondResult
	if second.err != nil {
		t.Fatalf("second Load() error = %v", second.err)
	}
	defer second.app.Store.Close()
}

func TestLoadReturnsBootstrapTimeoutWhenLockWaitExceedsConfiguredLimit(t *testing.T) {
	repoRoot := bootstrapRepoRoot(t)
	runtimeRoot := t.TempDir()
	t.Setenv("ODIN_BOOTSTRAP_TIMEOUT", "50ms")

	lockAcquired := make(chan struct{})
	release := make(chan struct{})
	var entered int32
	testBootstrapHooks.afterLockAcquired = func() {
		if atomic.AddInt32(&entered, 1) != 1 {
			return
		}
		close(lockAcquired)
		<-release
	}
	t.Cleanup(func() {
		testBootstrapHooks.afterLockAcquired = nil
	})

	firstResult := make(chan error, 1)
	go func() {
		app, err := Load(context.Background(), repoRoot, runtimeRoot)
		if err == nil {
			_ = app.Store.Close()
		}
		firstResult <- err
	}()

	<-lockAcquired

	_, err := Load(context.Background(), repoRoot, runtimeRoot)
	close(release)

	if err == nil {
		t.Fatal("second Load() error = nil, want bootstrap timeout")
	}

	var timeoutErr *BootstrapTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("Load() error = %v, want BootstrapTimeoutError", err)
	}

	if firstErr := <-firstResult; firstErr != nil {
		t.Fatalf("first Load() error = %v", firstErr)
	}
}

func assertCountAtLeast(t *testing.T, row rowScanner, minimum int) {
	t.Helper()

	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if count < minimum {
		t.Fatalf("count = %d, want at least %d", count, minimum)
	}
}

func assertCountExactly(t *testing.T, row rowScanner, want int) {
	t.Helper()

	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if count != want {
		t.Fatalf("count = %d, want %d", count, want)
	}
}

type rowScanner interface {
	Scan(...any) error
}
