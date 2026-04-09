package health

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestSummaryIsUnknownWithoutExecutorHealth(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	defer store.Close()

	service := Service{
		DB: store.DB(),
	}

	summary, err := service.Summary(context.Background(), true)
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.Status != StatusUnknown {
		t.Fatalf("Status = %q, want %q", summary.Status, StatusUnknown)
	}
}

func TestSummaryIsDegradedWhenRegistryIsInvalid(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	defer store.Close()

	service := Service{
		DB: store.DB(),
	}

	summary, err := service.Summary(context.Background(), false)
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.Status != StatusDegraded {
		t.Fatalf("Status = %q, want %q", summary.Status, StatusDegraded)
	}
}

func openStore(t *testing.T) *sqlite.Store {
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
