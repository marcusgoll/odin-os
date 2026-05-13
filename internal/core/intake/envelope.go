package intake

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const DedupeRecipeVersion = "odin-intake-v1"

type SourceEnvelope struct {
	SourceFamily     string         `json:"source_family"`
	ExternalObjectID string         `json:"external_object_id,omitempty"`
	EventKind        string         `json:"event_kind"`
	ObservedAt       time.Time      `json:"observed_at,omitempty"`
	Subject          string         `json:"subject"`
	Body             string         `json:"body,omitempty"`
	Summary          string         `json:"summary,omitempty"`
	Actor            string         `json:"actor"`
	SourceURI        string         `json:"source_uri,omitempty"`
	EvidenceRefs     []string       `json:"evidence_refs,omitempty"`
	AdapterFacts     map[string]any `json:"adapter_facts,omitempty"`
}

func (envelope SourceEnvelope) Validate() error {
	if strings.TrimSpace(envelope.SourceFamily) == "" {
		return fmt.Errorf("source_family is required")
	}
	if strings.TrimSpace(envelope.EventKind) == "" {
		return fmt.Errorf("event_kind is required")
	}
	if strings.TrimSpace(envelope.Subject) == "" {
		return fmt.Errorf("subject is required")
	}
	if strings.TrimSpace(envelope.Actor) == "" {
		return fmt.Errorf("actor is required")
	}
	for key := range envelope.AdapterFacts {
		if !strings.Contains(key, ".") && key != envelope.SourceFamily {
			return fmt.Errorf("adapter_facts key %q must be namespaced or match source_family", key)
		}
	}
	return nil
}

func (envelope SourceEnvelope) SourceFactsJSON() (string, error) {
	if err := envelope.Validate(); err != nil {
		return "", err
	}
	facts := map[string]any{
		"source_family":      envelope.SourceFamily,
		"external_object_id": envelope.ExternalObjectID,
		"event_kind":         envelope.EventKind,
		"subject":            envelope.Subject,
		"body":               envelope.Body,
		"summary":            envelope.Summary,
		"actor":              envelope.Actor,
		"source_uri":         envelope.SourceURI,
		"evidence_refs":      envelope.EvidenceRefs,
		"adapter_facts":      envelope.AdapterFacts,
	}
	if !envelope.ObservedAt.IsZero() {
		facts["observed_at"] = envelope.ObservedAt.UTC().Format(time.RFC3339)
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func (envelope SourceEnvelope) DedupeKey(workspaceID string) string {
	parts := []string{
		strings.ToLower(strings.TrimSpace(workspaceID)),
		strings.ToLower(strings.TrimSpace(envelope.SourceFamily)),
		strings.ToLower(strings.TrimSpace(envelope.EventKind)),
	}
	switch {
	case strings.TrimSpace(envelope.ExternalObjectID) != "":
		parts = append(parts, "external_object_id", normalizedFingerprint(envelope.ExternalObjectID))
	case strings.TrimSpace(envelope.SourceURI) != "":
		parts = append(parts, "source_uri", normalizedFingerprint(envelope.SourceURI))
	default:
		parts = append(parts,
			"content",
			normalizedFingerprint(envelope.Subject),
			normalizedFingerprint(envelope.Body),
			normalizedFingerprint(envelope.Summary),
		)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "odin-intake:" + hex.EncodeToString(sum[:])[:24]
}

func normalizedFingerprint(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}
