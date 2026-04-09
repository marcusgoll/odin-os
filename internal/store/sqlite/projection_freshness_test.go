package sqlite

import (
	"context"
	"testing"
	"time"
)

func TestProjectionFreshnessUpsertAndList(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "projection-freshness.db")
	defer store.Close()

	recorded, err := store.RecordProjectionFreshness(ctx, RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "healthy",
		DetailsJSON: `{"source":"runtime"}`,
	})
	if err != nil {
		t.Fatalf("RecordProjectionFreshness(first) error = %v", err)
	}

	updated, err := store.RecordProjectionFreshness(ctx, RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "degraded",
		DetailsJSON: `{"reason":"stale"}`,
	})
	if err != nil {
		t.Fatalf("RecordProjectionFreshness(second) error = %v", err)
	}

	if updated.Surface != "doctor" {
		t.Fatalf("Surface = %q, want doctor", updated.Surface)
	}
	if updated.Status != "degraded" {
		t.Fatalf("Status = %q, want degraded", updated.Status)
	}
	if !updated.UpdatedAt.After(recorded.UpdatedAt) && !updated.UpdatedAt.Equal(recorded.UpdatedAt) {
		t.Fatalf("UpdatedAt = %v, want >= %v", updated.UpdatedAt, recorded.UpdatedAt)
	}

	records, err := store.ListProjectionFreshness(ctx)
	if err != nil {
		t.Fatalf("ListProjectionFreshness() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("ListProjectionFreshness() len = %d, want 1", len(records))
	}
}

func TestProjectionFreshnessListsStaleRecordsByTimestamp(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "projection-freshness-stale.db")
	defer store.Close()

	recorded, err := store.RecordProjectionFreshness(ctx, RecordProjectionFreshnessParams{
		Surface:     "metrics",
		Status:      "healthy",
		DetailsJSON: `{"source":"runtime"}`,
	})
	if err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}

	staleAt := time.Now().UTC().Add(-2 * time.Hour)
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE projection_freshness
		SET refreshed_at = ?, updated_at = ?
		WHERE surface = ?
	`, formatTime(staleAt), formatTime(staleAt), recorded.Surface); err != nil {
		t.Fatalf("force stale projection freshness error = %v", err)
	}

	stale, err := store.ListStaleProjectionFreshness(ctx, time.Now().UTC().Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("ListStaleProjectionFreshness() error = %v", err)
	}
	if len(stale) != 1 {
		t.Fatalf("ListStaleProjectionFreshness() len = %d, want 1", len(stale))
	}
	if stale[0].Surface != "metrics" {
		t.Fatalf("stale surface = %q, want metrics", stale[0].Surface)
	}
}
