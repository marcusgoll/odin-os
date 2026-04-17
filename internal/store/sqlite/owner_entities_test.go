package sqlite

import (
	"context"
	"strings"
	"testing"
)

func TestWorkspaceCreateNormalizesDefaultCompanionKey(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "workspace-normalization.db")
	defer store.Close()

	workspace, err := store.CreateWorkspace(ctx, CreateWorkspaceParams{
		Key:                 "marcus",
		Name:                "Marcus",
		OwnerRef:            "marcus",
		Status:              "active",
		DefaultCompanionKey: "  planner  ",
		PolicyJSON:          "",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	if workspace.DefaultCompanionKey != "planner" {
		t.Fatalf("CreateWorkspace().DefaultCompanionKey = %q, want %q", workspace.DefaultCompanionKey, "planner")
	}

	got, err := store.GetWorkspaceByKey(ctx, "marcus")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey() error = %v", err)
	}
	if got.DefaultCompanionKey != "planner" {
		t.Fatalf("GetWorkspaceByKey().DefaultCompanionKey = %q, want %q", got.DefaultCompanionKey, "planner")
	}
	if got.PolicyJSON != "{}" {
		t.Fatalf("GetWorkspaceByKey().PolicyJSON = %q, want %q", got.PolicyJSON, "{}")
	}
}

func TestInitiativeOwnerCompanionForeignKeyRejectsCrossWorkspaceReference(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "initiative-owner-fk.db")
	defer store.Close()

	workspaceA, err := store.CreateWorkspace(ctx, CreateWorkspaceParams{
		Key:      "alpha",
		Name:     "Alpha",
		OwnerRef: "alpha",
		Status:   "active",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace(alpha) error = %v", err)
	}
	workspaceB, err := store.CreateWorkspace(ctx, CreateWorkspaceParams{
		Key:      "beta",
		Name:     "Beta",
		OwnerRef: "beta",
		Status:   "active",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace(beta) error = %v", err)
	}
	companion, err := store.CreateCompanion(ctx, CreateCompanionParams{
		WorkspaceID: workspaceA.ID,
		Key:         "planner",
		Title:       "Planner",
		Kind:        "planning",
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("CreateCompanion() error = %v", err)
	}

	_, err = store.DB().ExecContext(ctx, `
		INSERT INTO initiatives (
			workspace_id,
			key,
			title,
			kind,
			status,
			summary,
			linked_project_id,
			owner_companion_id,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, ?)
	`,
		workspaceB.ID,
		"scope",
		"Cross Workspace Scope",
		"delivery",
		"active",
		"",
		companion.ID,
		formatTime(store.now()),
		formatTime(store.now()),
	)
	if err == nil {
		t.Fatalf("cross-workspace initiative insert error = nil, want foreign key failure")
	}
	if !strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		t.Fatalf("cross-workspace initiative insert error = %v, want foreign key failure", err)
	}
}
