package lifecycle

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"odin-os/internal/app/bootstrap"
	"odin-os/internal/store/sqlite"
)

func TestReviewQueueDefaultSourcesIncludeGovernedDecisionSources(t *testing.T) {
	sources := defaultReviewQueueSources()
	got := make([]string, 0, len(sources))
	for _, source := range sources {
		got = append(got, source.Name())
	}

	want := []string{
		"intake",
		"goal",
		"approval",
		"skill_artifact",
		"context_pack",
		"memory_proposal",
		"recovery",
		"failed_work",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaultReviewQueueSources() names = %#v, want %#v", got, want)
	}
}

func TestRecoveryReviewQueueSourceListsRecoveryIncidentsWithEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	incident, err := store.OpenIncident(ctx, sqlite.OpenIncidentParams{
		Severity:    "error",
		Status:      "open",
		Summary:     "wake packet envelope is invalid",
		DetailsJSON: `{"fault_key":"wake_packet_invalid","subject_key":"task:alpha","decision_mode":"incident_only","next_action":"review wake packet evidence"}`,
	})
	if err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}

	entries, err := (recoveryReviewQueueSource{}).ListReviewQueueEntries(ctx, bootstrap.App{Store: store}, newReviewQueueSourceState())
	if err != nil {
		t.Fatalf("ListReviewQueueEntries() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want one recovery review entry", entries)
	}
	entry := entries[0]
	if entry.QueueID != "recovery:1" || entry.SourceID != incident.ID {
		t.Fatalf("entry ids = %+v, want recovery incident id", entry)
	}
	if entry.Type != "recovery_incident" || entry.SourceType != "recovery" {
		t.Fatalf("entry type = %+v, want recovery incident source", entry)
	}
	if entry.Risk != "high" || len(entry.AllowedActions) != 0 {
		t.Fatalf("entry risk/actions = %+v/%+v, want high read-only review", entry.Risk, entry.AllowedActions)
	}
	if entry.ObjectKey != "wake_packet_invalid:task:alpha" {
		t.Fatalf("entry.ObjectKey = %q, want fault and subject key", entry.ObjectKey)
	}
	if entry.Decision != "incident_only" || entry.RecoveryRecommendation != "review wake packet evidence" {
		t.Fatalf("entry decision/recommendation = %q/%q, want recovery evidence", entry.Decision, entry.RecoveryRecommendation)
	}
}
