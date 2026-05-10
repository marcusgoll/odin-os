package intake

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSourceEnvelopeValidatesAndBuildsFacts(t *testing.T) {
	observedAt := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	envelope := SourceEnvelope{
		SourceFamily:     "cli",
		ExternalObjectID: "manual-1",
		EventKind:        "request",
		ObservedAt:       observedAt,
		Subject:          "Build universal intake proposal",
		Body:             "Preserve raw input and prepare a reviewable proposal.",
		Actor:            "operator",
		SourceURI:        "odin://manual/intake/manual-1",
		EvidenceRefs:     []string{"stdin"},
		AdapterFacts: map[string]any{
			"cli": map[string]any{"payload_policy": "stored_in_source_facts_json"},
		},
	}

	facts, err := envelope.SourceFactsJSON()
	if err != nil {
		t.Fatalf("SourceFactsJSON() error = %v", err)
	}
	if !json.Valid([]byte(facts)) {
		t.Fatalf("facts json is invalid: %s", facts)
	}

	dedupe := envelope.DedupeKey("default")
	if dedupe == "" || dedupe == "manual-1" {
		t.Fatalf("DedupeKey() = %q, want Odin-owned derived key", dedupe)
	}
}

func TestSourceEnvelopeRejectsMissingCoreFields(t *testing.T) {
	envelope := SourceEnvelope{SourceFamily: "cli", EventKind: "request"}
	if err := envelope.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing subject/actor error")
	}
}

func TestSourceEnvelopeRejectsUnnamespacedAdapterFacts(t *testing.T) {
	envelope := SourceEnvelope{
		SourceFamily: "cli",
		EventKind:    "request",
		Subject:      "Build universal intake proposal",
		Actor:        "operator",
		AdapterFacts: map[string]any{"payload_policy": "stored_in_source_facts_json"},
	}
	if err := envelope.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want adapter facts namespace error")
	}
}
