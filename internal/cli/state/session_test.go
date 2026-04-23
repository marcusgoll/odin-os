package state

import (
	"path/filepath"
	"testing"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
)

func TestSessionStoreLoadAndSave(t *testing.T) {
	store := SessionStore{Path: filepath.Join(t.TempDir(), "cli-session.json")}
	want := Cache{ProjectKey: "odin-core", Mode: ModeAsk, SelectedWorkflowKey: "marcus-social-growth-workflow"}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != want {
		t.Fatalf("Cache = %+v, want %+v", got, want)
	}
}

func TestResolveStartupStateFallsBackToGlobalAsk(t *testing.T) {
	got := ResolveStartupState(Cache{ProjectKey: "missing", Mode: ModeAct}, projects.Registry{})
	if got.Scope.Kind != scope.ScopeGlobal || got.Mode != ModeAsk {
		t.Fatalf("State = %+v, want global ask", got)
	}
}

func TestResolveStartupStateRestoresSelectedWorkflow(t *testing.T) {
	got := ResolveStartupState(Cache{SelectedWorkflowKey: "marcus-social-growth-workflow"}, projects.Registry{})
	if got.SelectedWorkflowKey != "marcus-social-growth-workflow" {
		t.Fatalf("SelectedWorkflowKey = %q, want marcus-social-growth-workflow", got.SelectedWorkflowKey)
	}
}
