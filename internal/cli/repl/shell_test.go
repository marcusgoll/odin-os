package repl

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/cli/scope"
	"odin-os/internal/store/sqlite"
)

func TestShellRestoresValidSessionOnStartup(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	if err := env.SessionStore.Save(Cache{
		ProjectKey: "alpha",
		Mode:       ModeAct,
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if shell.state.Mode != ModeAct {
		t.Fatalf("Mode = %q, want %q", shell.state.Mode, ModeAct)
	}
	if shell.state.Scope.Kind != scope.ScopeProject || shell.state.Scope.ProjectKey != "alpha" {
		t.Fatalf("Scope = %+v, want project alpha", shell.state.Scope)
	}
}

func TestShellDowngradesInvalidSessionOnStartup(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	if err := env.SessionStore.Save(Cache{
		ProjectKey: "missing",
		Mode:       ModeAct,
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if shell.state.Mode != ModeAsk {
		t.Fatalf("Mode = %q, want %q", shell.state.Mode, ModeAsk)
	}
	if shell.state.Scope.Kind != scope.ScopeGlobal {
		t.Fatalf("Scope = %+v, want global", shell.state.Scope)
	}
}

func TestAskModeHandlesFreeTextWithoutCreatingTask(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "what scope am i in?", &output); err != nil {
		t.Fatalf("HandleLine() error = %v", err)
	}

	if !strings.Contains(output.String(), "global") {
		t.Fatalf("HandleLine() output = %q, want scope answer", output.String())
	}

	views, err := shell.jobs.List(context.Background(), shell.state.Scope)
	if err != nil {
		t.Fatalf("jobs.List() error = %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("jobs len = %d, want 0", len(views))
	}
}

func TestActModeCreatesTaskInProjectScope(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "/mode act", &output); err != nil {
		t.Fatalf("HandleLine(/mode) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "Implement the shell", &output); err != nil {
		t.Fatalf("HandleLine(act input) error = %v", err)
	}

	views, err := shell.jobs.List(context.Background(), scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	})
	if err != nil {
		t.Fatalf("jobs.List() error = %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("jobs len = %d, want 1", len(views))
	}
	if !strings.Contains(output.String(), "created task") {
		t.Fatalf("output = %q, want creation message", output.String())
	}
}

func TestActModeRejectedInGlobalScope(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/mode act", &output); err != nil {
		t.Fatalf("HandleLine() error = %v", err)
	}

	if shell.state.Mode != ModeAsk {
		t.Fatalf("Mode = %q, want ask", shell.state.Mode)
	}
	if !strings.Contains(output.String(), "global scope") {
		t.Fatalf("output = %q, want global-scope rejection", output.String())
	}
}

func TestDoctorCommandRendersStructuredTextOutput(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/doctor", &output); err != nil {
		t.Fatalf("HandleLine(/doctor) error = %v", err)
	}

	for _, want := range []string{"status=", "database=", "registry=", "executor=", "queue=", "projections=", "sources="} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want substring %q", output.String(), want)
		}
	}
}

func TestDoctorCommandSupportsJSONOutput(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	if _, err := env.Store.RecordExecutorHealth(context.Background(), sqlite.RecordExecutorHealthParams{
		Executor:    "codex",
		Status:      "healthy",
		LatencyMS:   42,
		DetailsJSON: `{"mode":"local"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}
	if _, err := env.Store.RecordRegistryVersion(context.Background(), sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "abc123",
		Notes:       "fresh compile",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}
	if _, err := env.Store.RecordProjectionFreshness(context.Background(), sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "healthy",
		DetailsJSON: `{"source":"runtime"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/doctor json", &output); err != nil {
		t.Fatalf("HandleLine(/doctor json) error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded["status"] == nil {
		t.Fatalf("decoded status missing: %#v", decoded)
	}
}

func newTestEnvironment(t *testing.T) Environment {
	t.Helper()

	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	stateDir := filepath.Join(root, "state", "cache")
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	registry := writeRegistry(t, map[string]string{
		"odin-core": "system_project",
		"alpha":     "github_backed_project",
	})

	store, err := sqlite.Open(filepath.Join(dataDir, "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	return Environment{
		Store:        store,
		Registry:     registry,
		SessionStore: SessionStore{Path: filepath.Join(stateDir, "cli-session.json")},
	}
}
