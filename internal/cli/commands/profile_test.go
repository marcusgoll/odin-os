package commands

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	coreprofile "odin-os/internal/core/profile"
	"odin-os/internal/store/sqlite"
)

func TestRunProfileShowsDefaultProfileState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	var stdout bytes.Buffer
	if err := RunProfile(ctx, store, []string{"show"}, &stdout); err != nil {
		t.Fatalf("RunProfile(show) error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, coreprofile.DefaultWorkspaceID) {
		t.Fatalf("show output = %q, want workspace id", output)
	}
	if !strings.Contains(output, "quiet_hours") {
		t.Fatalf("show output = %q, want quiet hours", output)
	}
}

func TestRunProfileSetsQuietHours(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	if err := RunProfile(ctx, store, []string{"set", "--quiet-hours", "22:00-07:00"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunProfile(set) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := RunProfile(ctx, store, []string{"show"}, &stdout); err != nil {
		t.Fatalf("RunProfile(show) error = %v", err)
	}

	if !strings.Contains(stdout.String(), "22:00-07:00") {
		t.Fatalf("show output = %q, want quiet hours", stdout.String())
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
