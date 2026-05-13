package sqlite

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
)

func TestCreateIntakeItemPreservesDuplicateRawArrivalsBeforeWork(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTaskIntakeStore(t, "intake-items.db")
	defer store.Close()

	store.Now = func() time.Time {
		return time.Date(2026, 4, 24, 22, 30, 0, 0, time.UTC)
	}

	first, err := store.CreateIntakeItem(ctx, CreateIntakeItemParams{
		WorkspaceID:         "default",
		SourceFamily:        "n8n",
		ExternalObjectID:    "evt-1",
		EventKind:           "ci_failure",
		Subject:             "pbs build failed",
		DedupeKey:           "default:n8n:ci-failure:pbs",
		DedupeRecipeVersion: "intake-v1",
		SourceFactsJSON:     `{"source_family":"n8n","external_object_id":"evt-1","event_kind":"ci_failure","project":"pbs","requested_by":"n8n","payload_policy":"stored_in_source_facts_json"}`,
		Status:              "received",
		Scope:               "project",
		ScopeKey:            "pbs",
		Summary:             "PBS CI failed",
	})
	if err != nil {
		t.Fatalf("CreateIntakeItem(first) error = %v", err)
	}

	second, err := store.CreateIntakeItem(ctx, CreateIntakeItemParams{
		WorkspaceID:         "default",
		SourceFamily:        "n8n",
		ExternalObjectID:    "evt-2",
		EventKind:           "ci_failure",
		Subject:             "pbs build failed",
		DedupeKey:           "default:n8n:ci-failure:pbs",
		DedupeRecipeVersion: "intake-v1",
		SourceFactsJSON:     `{"source_family":"n8n","external_object_id":"evt-2","event_kind":"ci_failure","project":"pbs","requested_by":"n8n","payload_policy":"stored_in_source_facts_json"}`,
		Status:              "received",
		Scope:               "project",
		ScopeKey:            "pbs",
		Summary:             "PBS CI failed again",
	})
	if err != nil {
		t.Fatalf("CreateIntakeItem(second) error = %v", err)
	}

	if first.ID == second.ID {
		t.Fatalf("duplicate arrivals used same Intake Item ID %d", first.ID)
	}
	if first.DedupeKey != second.DedupeKey {
		t.Fatalf("dedupe keys = %q and %q, want same canonical identity", first.DedupeKey, second.DedupeKey)
	}

	items, err := store.ListIntakeItems(ctx, ListIntakeItemsParams{
		WorkspaceID: "default",
	})
	if err != nil {
		t.Fatalf("ListIntakeItems() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("ListIntakeItems() len = %d, want 2 duplicate raw arrivals", len(items))
	}
	for _, item := range items {
		if item.Status != "received" {
			t.Fatalf("IntakeItem status = %q, want received", item.Status)
		}
		if !json.Valid([]byte(item.SourceFactsJSON)) {
			t.Fatalf("SourceFactsJSON is not valid JSON: %s", item.SourceFactsJSON)
		}
	}

	events, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var intakeCreated int
	for _, event := range events {
		switch event.Type {
		case runtimeevents.EventIntakeItemCreated:
			intakeCreated++
			var payload map[string]any
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatalf("intake created payload unmarshal: %v\n%s", err, string(event.Payload))
			}
			if payload["requested_by"] == "" || payload["payload_policy"] != "stored_in_source_facts_json" {
				t.Fatalf("intake created payload = %+v, want requested_by and payload policy provenance", payload)
			}
		case runtimeevents.EventTaskCreated:
			t.Fatalf("raw intake created governed work event: %+v", event)
		}
	}
	if intakeCreated != 2 {
		t.Fatalf("intake created events = %d, want 2", intakeCreated)
	}
}

func TestIntakeAttachmentsPersistMetadataAndBytes(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTaskIntakeStore(t, "intake-attachments.db")
	defer store.Close()

	item, err := store.CreateIntakeItem(ctx, CreateIntakeItemParams{
		WorkspaceID:         "default",
		SourceFamily:        "mobile_api",
		ExternalObjectID:    "mobile-photo-1",
		EventKind:           "photo",
		Subject:             "Panel photo",
		DedupeKey:           "mobile:photo:1",
		DedupeRecipeVersion: "mobile-api-v1",
		SourceFactsJSON:     `{"source":"mobile_api","payload_policy":"stored_in_source_facts_json"}`,
		Status:              "received",
		Summary:             "Panel photo",
	})
	if err != nil {
		t.Fatalf("CreateIntakeItem() error = %v", err)
	}

	attachment, err := store.CreateIntakeAttachment(ctx, CreateIntakeAttachmentParams{
		IntakeItemID: item.ID,
		Kind:         "photo",
		Filename:     "panel.jpg",
		ContentType:  "image/jpeg",
		SizeBytes:    10,
		SHA256:       "abc123",
		Status:       "stored",
		Bytes:        []byte("image-data"),
	})
	if err != nil {
		t.Fatalf("CreateIntakeAttachment() error = %v", err)
	}
	if attachment.ID == 0 || attachment.IntakeItemID != item.ID || attachment.Status != "stored" {
		t.Fatalf("attachment = %+v, want stored attachment linked to intake %d", attachment, item.ID)
	}

	attachments, err := store.ListIntakeAttachments(ctx, ListIntakeAttachmentsParams{IntakeItemID: item.ID})
	if err != nil {
		t.Fatalf("ListIntakeAttachments() error = %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("attachments len = %d, want 1", len(attachments))
	}
	if attachments[0].Filename != "panel.jpg" || string(attachments[0].Bytes) != "image-data" {
		t.Fatalf("attachment = %+v, want metadata and bytes preserved", attachments[0])
	}
}
