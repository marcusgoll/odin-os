package controlscope

import "testing"

func TestControlScopeResolveWorkspace(t *testing.T) {
	t.Parallel()

	got := Service{}.ResolveWorkspace("marcus")
	want := ControlScope{
		SubjectType:  SubjectTypeWorkspace,
		SubjectKey:   "marcus",
		WorkspaceKey: "marcus",
	}

	if got != want {
		t.Fatalf("ResolveWorkspace() = %+v, want %+v", got, want)
	}
}

func TestControlScopeResolveInitiative(t *testing.T) {
	t.Parallel()

	got := Service{}.ResolveInitiative("marcus", "ops")
	want := ControlScope{
		SubjectType:   SubjectTypeInitiative,
		SubjectKey:    "ops",
		WorkspaceKey:  "marcus",
		InitiativeKey: "ops",
	}

	if got != want {
		t.Fatalf("ResolveInitiative() = %+v, want %+v", got, want)
	}
}

func TestControlScopeResolveManagedProjectWithCompanionOverlay(t *testing.T) {
	t.Parallel()

	got := Service{}.ResolveManagedProject("marcus", "ops", "alpha", "operator")
	want := ControlScope{
		SubjectType:   SubjectTypeProject,
		SubjectKey:    "alpha",
		WorkspaceKey:  "marcus",
		InitiativeKey: "ops",
		ProjectKey:    "alpha",
		CompanionKey:  "operator",
	}

	if got != want {
		t.Fatalf("ResolveManagedProject() = %+v, want %+v", got, want)
	}
}
