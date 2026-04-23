package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"
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
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}
	if migrationCount != len(migrations) {
		t.Fatalf("schema_migrations count = %d, want %d", migrationCount, len(migrations))
	}
}

func TestCompanionStoreRejectsInvalidPolicyJSON(t *testing.T) {
	t.Parallel()

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

	_, err = store.UpsertCompanion(ctx, UpsertCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 "bad-json",
		Title:               "Bad JSON",
		Kind:                "assistant",
		Charter:             "invalid",
		Status:              "active",
		InitiativeScopeJSON: `{"initiatives":[]}`,
		ToolPolicyJSON:      `not-json`,
		MemoryPolicyJSON:    `{}`,
		PlanningPolicyJSON:  `{}`,
	})
	if err == nil {
		t.Fatalf("UpsertCompanion() error = nil, want invalid json error")
	}
}

func TestCompanionStoreDefaultsEmptyPolicyJSON(t *testing.T) {
	t.Parallel()

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
		Key:                 "defaults",
		Title:               "Defaults",
		Kind:                "assistant",
		Charter:             "Normalize blank JSON fields.",
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

func TestCompanionStoreListsCompanionsByWorkspace(t *testing.T) {
	t.Parallel()

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

	first, err := store.UpsertCompanion(ctx, UpsertCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 "finance",
		Title:               "Finance Advisor",
		Kind:                "advisor",
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

	second, err := store.UpsertCompanion(ctx, UpsertCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 "ops",
		Title:               "Operations Specialist",
		Kind:                "specialist",
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

	companionList, err := store.ListCompanionsByWorkspace(ctx, ListCompanionsParams{WorkspaceID: workspace.ID})
	if err != nil {
		t.Fatalf("ListCompanionsByWorkspace() error = %v", err)
	}
	if len(companionList) < 2 {
		t.Fatalf("ListCompanionsByWorkspace() len = %d, want at least 2", len(companionList))
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
		t.Fatalf("ListCompanionsByWorkspace() = %+v, want finance and ops companions", companionList)
	}
}

func TestRegisterManagedProjectPreservesExistingDefaultCompanion(t *testing.T) {
	t.Parallel()

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

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE companions
		SET title = ?, charter = ?, tool_policy_json = ?, memory_policy_json = ?, planning_policy_json = ?
		WHERE workspace_id = ? AND key = ?
	`, "Custom Assistant", "Custom charter", `{"allow":["merge_to_main"]}`, `{"mode":"global"}`, `{"mode":"planning"}`, workspace.ID, workspace.DefaultCompanionKey); err != nil {
		t.Fatalf("customize default companion error = %v", err)
	}

	_, err = store.RegisterManagedProject(ctx, ManagedProjectRegistrationParams{
		Workspace: CreateWorkspaceParams{
			Key:                 workspace.Key,
			Name:                workspace.Name,
			OwnerRef:            workspace.OwnerRef,
			DefaultCompanionKey: workspace.DefaultCompanionKey,
			Status:              workspace.Status,
			PolicyJSON:          workspace.PolicyJSON,
		},
		Project: CreateProjectParams{
			Key:           "alpha",
			Name:          "Alpha",
			Scope:         "project",
			GitRoot:       filepath.Join(t.TempDir(), "alpha"),
			DefaultBranch: "main",
			ManifestPath:  "config/projects.yaml",
		},
	})
	if err != nil {
		t.Fatalf("RegisterManagedProject() error = %v", err)
	}

	after, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	if after.Title != "Custom Assistant" {
		t.Fatalf("RegisterManagedProject() overwrote title = %q, want %q", after.Title, "Custom Assistant")
	}
	if after.Charter != "Custom charter" {
		t.Fatalf("RegisterManagedProject() overwrote charter = %q, want %q", after.Charter, "Custom charter")
	}
	if after.ToolPolicyJSON != `{"allow":["merge_to_main"]}` {
		t.Fatalf("RegisterManagedProject() overwrote tool policy = %q, want %q", after.ToolPolicyJSON, `{"allow":["merge_to_main"]}`)
	}
	if after.MemoryPolicyJSON != `{"mode":"global"}` {
		t.Fatalf("RegisterManagedProject() overwrote memory policy = %q, want %q", after.MemoryPolicyJSON, `{"mode":"global"}`)
	}
	if after.PlanningPolicyJSON != `{"mode":"planning"}` {
		t.Fatalf("RegisterManagedProject() overwrote planning policy = %q, want %q", after.PlanningPolicyJSON, `{"mode":"planning"}`)
	}
}

func TestEnsureDefaultCompanionPreservesConcurrentCustomCreate(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")

	first, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(first) error = %v", err)
	}
	defer first.Close()

	second, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(second) error = %v", err)
	}
	defer second.Close()

	if err := first.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(first) error = %v", err)
	}

	workspace, err := first.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}

	if _, err := first.DB().ExecContext(ctx, `
		DELETE FROM companions
		WHERE workspace_id = ? AND key = ?
	`, workspace.ID, workspace.DefaultCompanionKey); err != nil {
		t.Fatalf("delete default companion error = %v", err)
	}

	tx, err := first.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO companions (
			workspace_id,
			key,
			title,
			kind,
			charter,
			status,
			initiative_scope_json,
			tool_policy_json,
			memory_policy_json,
			planning_policy_json,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, workspace.ID, workspace.DefaultCompanionKey, "Custom Assistant", "assistant", "Custom charter", "active", `{"initiatives":["alpha"]}`, `{"allow":["merge_to_main"]}`, `{"mode":"global"}`, `{"mode":"planning"}`, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("seed custom companion in tx error = %v", err)
	}

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- second.EnsureDefaultCompanion(ctx, workspace.ID, workspace.DefaultCompanionKey)
	}()
	<-started

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	if err := <-done; err != nil {
		t.Fatalf("EnsureDefaultCompanion() error = %v", err)
	}

	after, err := first.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	if after.Title != "Custom Assistant" {
		t.Fatalf("EnsureDefaultCompanion() overwrote title = %q, want %q", after.Title, "Custom Assistant")
	}
	if after.Charter != "Custom charter" {
		t.Fatalf("EnsureDefaultCompanion() overwrote charter = %q, want %q", after.Charter, "Custom charter")
	}
}

func TestEnsureDefaultCompanionCreatesMissingRow(t *testing.T) {
	t.Parallel()

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

	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM companions
		WHERE workspace_id = ? AND key = ?
	`, workspace.ID, workspace.DefaultCompanionKey); err != nil {
		t.Fatalf("delete default companion error = %v", err)
	}

	if err := store.EnsureDefaultCompanion(ctx, workspace.ID, workspace.DefaultCompanionKey); err != nil {
		t.Fatalf("EnsureDefaultCompanion() error = %v", err)
	}

	after, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	if after.Title != "Primary Assistant" {
		t.Fatalf("EnsureDefaultCompanion() title = %q, want %q", after.Title, "Primary Assistant")
	}
}

func TestMigrateBackfillsDefaultCompanionForDefaultWorkspace(t *testing.T) {
	t.Parallel()

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

	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	if companion.Key != workspace.DefaultCompanionKey {
		t.Fatalf("companion.Key = %q, want %q", companion.Key, workspace.DefaultCompanionKey)
	}
}
