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
			"cli": map[string]any{
				"payload_policy": "stored_in_source_facts_json",
				"requested_by":   "operator",
			},
		},
	}

	facts, err := envelope.SourceFactsJSON()
	if err != nil {
		t.Fatalf("SourceFactsJSON() error = %v", err)
	}
	if !json.Valid([]byte(facts)) {
		t.Fatalf("facts json is invalid: %s", facts)
	}
	var decoded struct {
		SourceFamily     string                            `json:"source_family"`
		ExternalObjectID string                            `json:"external_object_id"`
		EventKind        string                            `json:"event_kind"`
		ObservedAt       string                            `json:"observed_at"`
		Subject          string                            `json:"subject"`
		Body             string                            `json:"body"`
		Summary          string                            `json:"summary"`
		Actor            string                            `json:"actor"`
		SourceURI        string                            `json:"source_uri"`
		EvidenceRefs     []string                          `json:"evidence_refs"`
		AdapterFacts     map[string]map[string]interface{} `json:"adapter_facts"`
	}
	if err := json.Unmarshal([]byte(facts), &decoded); err != nil {
		t.Fatalf("Unmarshal(facts) error = %v", err)
	}
	if decoded.SourceFamily != "cli" || decoded.ExternalObjectID != "manual-1" || decoded.EventKind != "request" {
		t.Fatalf("facts identity = %+v, want cli/manual-1/request", decoded)
	}
	if decoded.ObservedAt != "2026-05-10T12:00:00Z" || decoded.Subject != "Build universal intake proposal" || decoded.Body != "Preserve raw input and prepare a reviewable proposal." || decoded.Summary != "" {
		t.Fatalf("facts content = %+v, want canonical observed/content fields", decoded)
	}
	if decoded.Actor != "operator" || decoded.SourceURI != "odin://manual/intake/manual-1" || len(decoded.EvidenceRefs) != 1 || decoded.EvidenceRefs[0] != "stdin" {
		t.Fatalf("facts provenance = %+v, want actor/source/evidence", decoded)
	}
	cliFacts := decoded.AdapterFacts["cli"]
	if cliFacts == nil || cliFacts["payload_policy"] != "stored_in_source_facts_json" {
		t.Fatalf("adapter facts = %+v, want cli payload policy", decoded.AdapterFacts)
	}
	if cliFacts["requested_by"] != "operator" {
		t.Fatalf("adapter facts = %+v, want cli requested_by", decoded.AdapterFacts)
	}

	dedupe := envelope.DedupeKey("default")
	if dedupe == "" || dedupe == "manual-1" {
		t.Fatalf("DedupeKey() = %q, want Odin-owned derived key", dedupe)
	}
}

func TestSourceEnvelopeDedupeKeyUsesStableExternalIdentity(t *testing.T) {
	base := SourceEnvelope{
		SourceFamily:     "cli",
		ExternalObjectID: " Manual-1 ",
		EventKind:        "Request",
		Subject:          "Original subject",
		Body:             "Original body",
		Summary:          "Original summary",
		Actor:            "operator",
		SourceURI:        "odin://manual/intake/manual-1",
	}
	changedContent := SourceEnvelope{
		SourceFamily:     " CLI ",
		ExternalObjectID: "manual-1",
		EventKind:        "request",
		Subject:          "Different subject",
		Body:             "Different body",
		Summary:          "Different summary",
		Actor:            "operator",
		SourceURI:        "odin://manual/intake/different",
	}
	if got, want := base.DedupeKey(" Default "), changedContent.DedupeKey("default"); got != want {
		t.Fatalf("DedupeKey() = %q, want normalized external identity key %q", got, want)
	}

	changedExternalID := base
	changedExternalID.ExternalObjectID = "manual-2"
	if got, unchanged := changedExternalID.DedupeKey("default"), base.DedupeKey("default"); got == unchanged {
		t.Fatalf("DedupeKey() = %q, want different key when external id changes", got)
	}
}

func TestSourceEnvelopeDedupeKeyUsesSourceURIFallback(t *testing.T) {
	base := SourceEnvelope{
		SourceFamily: "cli",
		EventKind:    "request",
		Subject:      "Original subject",
		Body:         "Original body",
		Summary:      "Original summary",
		Actor:        "operator",
		SourceURI:    " Odin://Manual/Intake/Manual-1 ",
	}
	changedContent := SourceEnvelope{
		SourceFamily: "CLI",
		EventKind:    "Request",
		Subject:      "Different subject",
		Body:         "Different body",
		Summary:      "Different summary",
		Actor:        "operator",
		SourceURI:    "odin://manual/intake/manual-1",
	}
	if got, want := base.DedupeKey(" Default "), changedContent.DedupeKey("default"); got != want {
		t.Fatalf("DedupeKey() = %q, want normalized source_uri key %q", got, want)
	}

	changedURI := base
	changedURI.SourceURI = "odin://manual/intake/manual-2"
	if got, unchanged := changedURI.DedupeKey("default"), base.DedupeKey("default"); got == unchanged {
		t.Fatalf("DedupeKey() = %q, want different key when source uri changes", got)
	}
}

func TestSourceEnvelopeDedupeKeyUsesContentFallback(t *testing.T) {
	base := SourceEnvelope{
		SourceFamily: "cli",
		EventKind:    "request",
		Subject:      "  Build   Universal Intake Proposal ",
		Body:         "Preserve raw input.",
		Summary:      "Prepare a reviewable proposal.",
		Actor:        "operator",
	}
	normalized := SourceEnvelope{
		SourceFamily: " CLI ",
		EventKind:    "Request",
		Subject:      "build universal intake proposal",
		Body:         " preserve   raw input. ",
		Summary:      "prepare a reviewable proposal.",
		Actor:        "operator",
	}
	if got, want := base.DedupeKey(" Default "), normalized.DedupeKey("default"); got != want {
		t.Fatalf("DedupeKey() = %q, want normalized content key %q", got, want)
	}

	changedSubject := base
	changedSubject.Subject = "Build universal intake contract"
	if got, unchanged := changedSubject.DedupeKey("default"), base.DedupeKey("default"); got == unchanged {
		t.Fatalf("DedupeKey() = %q, want different key when content changes", got)
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
