package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCompanionStoreMigrationAndRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}

	created, err := store.UpsertCompanion(ctx, UpsertCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 "primary",
		Title:               "Primary Assistant",
		Kind:                "assistant",
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
	if created.Kind != "assistant" {
		t.Fatalf("created.Kind = %q, want %q", created.Kind, "assistant")
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE companions
		SET title = ?, kind = ?, charter = ?, status = ?, initiative_scope_json = ?, tool_policy_json = ?, memory_policy_json = ?, planning_policy_json = ?
		WHERE workspace_id = ? AND key = ?
	`, "Stale Title", "advisor", "stale charter", "disabled", `{"initiatives":[]}`, `{"allow":[]}`, `{"mode":"global"}`, `{"mode":"ad hoc"}`, workspace.ID, "primary"); err != nil {
		t.Fatalf("seed companion drift error = %v", err)
	}

	reconciled, err := store.UpsertCompanion(ctx, UpsertCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 "primary",
		Title:               "Primary Assistant",
		Kind:                "assistant",
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
	if reconciled.Title != "Primary Assistant" {
		t.Fatalf("reconciled.Title = %q, want %q", reconciled.Title, "Primary Assistant")
	}
	if reconciled.Kind != "assistant" {
		t.Fatalf("reconciled.Kind = %q, want %q", reconciled.Kind, "assistant")
	}

	got, err := store.GetCompanionByKey(ctx, workspace.ID, "primary")
	if err != nil {
		t.Fatalf("GetCompanionByKey() error = %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("GetCompanionByKey().ID = %d, want %d", got.ID, created.ID)
	}
	if got.ToolPolicyJSON != `{"allow":["branch_proposal"]}` {
		t.Fatalf("GetCompanionByKey().ToolPolicyJSON = %q, want %q", got.ToolPolicyJSON, `{"allow":["branch_proposal"]}`)
	}

	var migrationCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("schema_migrations count query error = %v", err)
	}
	if migrationCount != 9 {
		t.Fatalf("schema_migrations count = %d, want 9", migrationCount)
	}
}
