package profile

import (
	"context"
	"path/filepath"
	"testing"

	memoryroot "odin-os/internal/memory"
	"odin-os/internal/store/sqlite"
)

func TestProfileBootstrapsDefaultWorkspaceProfile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	service := Service{Store: store, WorkspaceKey: DefaultWorkspaceKey}

	got, err := service.Get(ctx)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.WorkspaceKey != DefaultWorkspaceKey {
		t.Fatalf("WorkspaceKey = %q, want %q", got.WorkspaceKey, DefaultWorkspaceKey)
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

	service := Service{Store: store, WorkspaceKey: DefaultWorkspaceKey}

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

	service := Service{Store: store, WorkspaceKey: DefaultWorkspaceKey}

	quietHours := "22:00-07:00"
	requireApproval := true
	if _, err := service.Update(ctx, UpdateParams{
		QuietHours:                             &quietHours,
		RequireHumanApprovalForExternalEffects: &requireApproval,
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	reloaded := Service{Store: store, WorkspaceKey: DefaultWorkspaceKey}
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

func TestProfileUpdateRecordsWorkspaceOwnedMemory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	service := Service{Store: store, WorkspaceKey: DefaultWorkspaceKey}
	quietHours := "22:00-07:00"

	profile, err := service.Update(ctx, UpdateParams{QuietHours: &quietHours})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	entries, err := store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workspace",
		ScopeKey:   profile.WorkspaceKey,
		MemoryType: memoryroot.MemoryTypeOperatingProfileUpdate,
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("workspace memory len = %d, want 1", len(entries))
	}
	if entries[0].Summary == "" {
		t.Fatalf("memory summary empty, want profile update note")
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
