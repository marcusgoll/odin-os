package companions

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestCompanionCanonicalKinds(t *testing.T) {
	t.Parallel()

	got := []Kind{KindAssistant, KindAdvisor, KindOperator, KindSpecialist}
	want := []Kind{"assistant", "advisor", "operator", "specialist"}

	if len(got) != len(want) {
		t.Fatalf("kind count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kind[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCompanionServiceUpsertsAndLoadsCompanion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openCompanionServiceStore(t)
	defer store.Close()

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}

	service := Service{Store: store}

	created, err := service.UpsertCompanion(ctx, Companion{
		WorkspaceID:         workspace.ID,
		Key:                 "primary",
		Title:               "Primary Assistant",
		Kind:                KindAssistant,
		Charter:             "Keep the workspace aligned and safe.",
		Status:              "active",
		InitiativeScopeJSON: `{"initiatives":["alpha"]}`,
		ToolPolicyJSON:      `{"allow":["branch_proposal"]}`,
		MemoryPolicyJSON:    `{"mode":"project"}`,
		PlanningPolicyJSON:  `{"mode":"guided"}`,
	})
	if err != nil {
		t.Fatalf("UpsertCompanion() error = %v", err)
	}

	if created.WorkspaceID != workspace.ID {
		t.Fatalf("created.WorkspaceID = %d, want %d", created.WorkspaceID, workspace.ID)
	}
	if created.Key != "primary" {
		t.Fatalf("created.Key = %q, want %q", created.Key, "primary")
	}
	if created.Kind != KindAssistant {
		t.Fatalf("created.Kind = %q, want %q", created.Kind, KindAssistant)
	}
	if created.ToolPolicyJSON != `{"allow":["branch_proposal"]}` {
		t.Fatalf("created.ToolPolicyJSON = %q, want %q", created.ToolPolicyJSON, `{"allow":["branch_proposal"]}`)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE companions
		SET title = ?, kind = ?, status = ?, charter = ?, initiative_scope_json = ?, tool_policy_json = ?, memory_policy_json = ?, planning_policy_json = ?
		WHERE workspace_id = ? AND key = ?
	`, "Stale Title", "advisor", "disabled", "stale charter", `{"initiatives":[]}`, `{"allow":[]}`, `{"mode":"global"}`, `{"mode":"ad hoc"}`, workspace.ID, "primary"); err != nil {
		t.Fatalf("seed companion drift error = %v", err)
	}

	reconciled, err := service.UpsertCompanion(ctx, Companion{
		WorkspaceID:         workspace.ID,
		Key:                 "primary",
		Title:               "Primary Assistant",
		Kind:                KindAssistant,
		Charter:             "Keep the workspace aligned and safe.",
		Status:              "active",
		InitiativeScopeJSON: `{"initiatives":["alpha"]}`,
		ToolPolicyJSON:      `{"allow":["branch_proposal"]}`,
		MemoryPolicyJSON:    `{"mode":"project"}`,
		PlanningPolicyJSON:  `{"mode":"guided"}`,
	})
	if err != nil {
		t.Fatalf("UpsertCompanion() reconcile error = %v", err)
	}

	if reconciled.ID != created.ID {
		t.Fatalf("reconciled.ID = %d, want %d", reconciled.ID, created.ID)
	}
	if reconciled.Kind != KindAssistant {
		t.Fatalf("reconciled.Kind = %q, want %q", reconciled.Kind, KindAssistant)
	}
	if reconciled.Status != "active" {
		t.Fatalf("reconciled.Status = %q, want %q", reconciled.Status, "active")
	}

	got, err := service.GetCompanionByKey(ctx, workspace.ID, "primary")
	if err != nil {
		t.Fatalf("GetCompanionByKey() error = %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("GetCompanionByKey().ID = %d, want %d", got.ID, created.ID)
	}
	if got.Title != "Primary Assistant" {
		t.Fatalf("GetCompanionByKey().Title = %q, want %q", got.Title, "Primary Assistant")
	}
}

func TestCompanionServiceRejectsInvalidKind(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openCompanionServiceStore(t)
	defer store.Close()

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}

	_, err = Service{Store: store}.UpsertCompanion(ctx, Companion{
		WorkspaceID:         workspace.ID,
		Key:                 "bad-kind",
		Title:               "Bad Kind",
		Kind:                Kind("bogus"),
		Charter:             "invalid",
		Status:              "active",
		InitiativeScopeJSON: `{}`,
		ToolPolicyJSON:      `{}`,
		MemoryPolicyJSON:    `{}`,
		PlanningPolicyJSON:  `{}`,
	})
	if err == nil {
		t.Fatalf("UpsertCompanion() error = nil, want invalid kind error")
	}
}

func TestCompanionServiceDefaultsEmptyPolicyFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openCompanionServiceStore(t)
	defer store.Close()

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}

	created, err := Service{Store: store}.UpsertCompanion(ctx, Companion{
		WorkspaceID:         workspace.ID,
		Key:                 "defaults",
		Title:               "Defaults",
		Kind:                KindAssistant,
		Charter:             "Normalize blank policy fields.",
		Status:              "active",
		InitiativeScopeJSON: "",
		ToolPolicyJSON:      "",
		MemoryPolicyJSON:    "",
		PlanningPolicyJSON:  "",
	})
	if err != nil {
		t.Fatalf("UpsertCompanion() error = %v", err)
	}

	if created.InitiativeScopeJSON != `{}` {
		t.Fatalf("created.InitiativeScopeJSON = %q, want %q", created.InitiativeScopeJSON, `{}`)
	}
	if created.ToolPolicyJSON != `{}` {
		t.Fatalf("created.ToolPolicyJSON = %q, want %q", created.ToolPolicyJSON, `{}`)
	}
	if created.MemoryPolicyJSON != `{}` {
		t.Fatalf("created.MemoryPolicyJSON = %q, want %q", created.MemoryPolicyJSON, `{}`)
	}
	if created.PlanningPolicyJSON != `{}` {
		t.Fatalf("created.PlanningPolicyJSON = %q, want %q", created.PlanningPolicyJSON, `{}`)
	}
}

func TestCompanionServiceListsCompanions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openCompanionServiceStore(t)
	defer store.Close()

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}

	service := Service{Store: store}

	first, err := service.UpsertCompanion(ctx, Companion{
		WorkspaceID:         workspace.ID,
		Key:                 "finance",
		Title:               "Finance Advisor",
		Kind:                KindAdvisor,
		Charter:             "Keep finance decisions clear.",
		Status:              "active",
		InitiativeScopeJSON: `{"initiatives":["finance"]}`,
		ToolPolicyJSON:      `{}`,
		MemoryPolicyJSON:    `{}`,
		PlanningPolicyJSON:  `{}`,
	})
	if err != nil {
		t.Fatalf("UpsertCompanion(finance) error = %v", err)
	}

	second, err := service.UpsertCompanion(ctx, Companion{
		WorkspaceID:         workspace.ID,
		Key:                 "ops",
		Title:               "Operations Specialist",
		Kind:                KindSpecialist,
		Charter:             "Keep operations moving.",
		Status:              "active",
		InitiativeScopeJSON: `{"initiatives":["ops"]}`,
		ToolPolicyJSON:      `{}`,
		MemoryPolicyJSON:    `{}`,
		PlanningPolicyJSON:  `{}`,
	})
	if err != nil {
		t.Fatalf("UpsertCompanion(ops) error = %v", err)
	}

	companionList, err := service.ListCompanions(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("ListCompanions() error = %v", err)
	}
	if len(companionList) < 2 {
		t.Fatalf("ListCompanions() len = %d, want at least 2", len(companionList))
	}

	foundFirst := false
	foundSecond := false
	for _, companion := range companionList {
		switch companion.ID {
		case first.ID:
			foundFirst = true
		case second.ID:
			foundSecond = true
		}
	}
	if !foundFirst || !foundSecond {
		t.Fatalf("ListCompanions() = %+v, want finance and ops companions", companionList)
	}
}

func openCompanionServiceStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}
