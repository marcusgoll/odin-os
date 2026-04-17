package profile

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestProfileBootstrapsDefaultWorkspaceProfile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	service := Service{Store: store, WorkspaceID: DefaultWorkspaceID}

	got, err := service.Get(ctx)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.WorkspaceID != DefaultWorkspaceID {
		t.Fatalf("WorkspaceID = %q, want %q", got.WorkspaceID, DefaultWorkspaceID)
	}
	if got.Preferences.QuietHours != "" {
		t.Fatalf("QuietHours = %q, want empty", got.Preferences.QuietHours)
	}
	if !got.Boundaries.ApprovalDefaults.RequireHumanApprovalForExternalEffects {
		t.Fatalf("RequireHumanApprovalForExternalEffects = false, want true")
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not bootstrapped: %+v", got)
	}
}

func TestProfileUpdatesQuietHoursAndApprovalDefaults(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	service := Service{Store: store, WorkspaceID: DefaultWorkspaceID}

	quietHours := "22:00-07:00"
	requireApproval := false

	got, err := service.Update(ctx, UpdateParams{
		QuietHours:                             &quietHours,
		RequireHumanApprovalForExternalEffects: &requireApproval,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if got.Preferences.QuietHours != quietHours {
		t.Fatalf("QuietHours = %q, want %q", got.Preferences.QuietHours, quietHours)
	}
	if got.Boundaries.ApprovalDefaults.RequireHumanApprovalForExternalEffects != requireApproval {
		t.Fatalf("RequireHumanApprovalForExternalEffects = %v, want %v", got.Boundaries.ApprovalDefaults.RequireHumanApprovalForExternalEffects, requireApproval)
	}
}

func TestProfileReadsProfileStateBackThroughService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	service := Service{Store: store, WorkspaceID: DefaultWorkspaceID}

	quietHours := "22:00-07:00"
	requireApproval := true
	if _, err := service.Update(ctx, UpdateParams{
		QuietHours:                             &quietHours,
		RequireHumanApprovalForExternalEffects: &requireApproval,
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	reloaded := Service{Store: store, WorkspaceID: DefaultWorkspaceID}
	got, err := reloaded.Get(ctx)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.Preferences.QuietHours != quietHours {
		t.Fatalf("QuietHours = %q, want %q", got.Preferences.QuietHours, quietHours)
	}
	if got.Boundaries.ApprovalDefaults.RequireHumanApprovalForExternalEffects != requireApproval {
		t.Fatalf("RequireHumanApprovalForExternalEffects = %v, want %v", got.Boundaries.ApprovalDefaults.RequireHumanApprovalForExternalEffects, requireApproval)
	}
}

func openTestStore(t *testing.T) *sqlite.Store {
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
