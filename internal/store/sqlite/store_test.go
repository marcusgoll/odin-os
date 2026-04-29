package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/runtime/projections"
)

func TestRequestApprovalCanBindActionPayload(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "action-bound-approval.db")
	defer store.Close()
	_, _, run := createProjectTaskRunFixture(t, ctx, store)

	action, payload, err := store.CreateActionWithPayload(ctx, CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        run.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          "sha256:approved",
		PayloadJSON:          `{"pairing":"W7084C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload() error = %v", err)
	}

	runID := run.ID
	approval, err := store.RequestApproval(ctx, RequestApprovalParams{
		TaskID:      run.TaskID,
		RunID:       &runID,
		ActionID:    &action.ID,
		PayloadHash: payload.PayloadHash,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	if approval.ActionID == nil || *approval.ActionID != action.ID || approval.PayloadHash != payload.PayloadHash {
		t.Fatalf("approval = %+v, want action payload binding", approval)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{RunID: &runID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var got runtimeevents.ApprovalRequestedPayload
	for _, event := range events {
		if event.Type == runtimeevents.EventApprovalRequested {
			if err := json.Unmarshal(event.Payload, &got); err != nil {
				t.Fatalf("Unmarshal approval.requested payload: %v", err)
			}
		}
	}
	if got.ActionID == nil || *got.ActionID != action.ID || got.PayloadHash != payload.PayloadHash {
		t.Fatalf("approval.requested payload = %+v, want action_id=%d payload_hash=%q", got, action.ID, payload.PayloadHash)
	}
}

func TestResolveActionApprovalRejectsPayloadMismatch(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "stale-action-approval.db")
	defer store.Close()
	_, _, run := createProjectTaskRunFixture(t, ctx, store)

	action, payload, err := store.CreateActionWithPayload(ctx, CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        run.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          "sha256:old",
		PayloadJSON:          `{"pairing":"W7084C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload() error = %v", err)
	}

	runID := run.ID
	approval, err := store.RequestApproval(ctx, RequestApprovalParams{
		TaskID:      run.TaskID,
		RunID:       &runID,
		ActionID:    &action.ID,
		PayloadHash: payload.PayloadHash,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO action_payloads (
			action_id,
			payload_schema,
			payload_schema_version,
			payload_hash,
			payload_json,
			submit_path,
			readback_path,
			proof_requirement,
			created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
	`, action.ID, "flica.tradeboard_action.v1", 1, "sha256:new", `{"pairing":"W9999"}`, "command:/tradeboard post", "huginn:flica-my-requests", "external_readback"); err != nil {
		t.Fatalf("insert replacement payload: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE actions
		SET current_payload_hash = ?, updated_at = datetime('now')
		WHERE id = ?
	`, "sha256:new", action.ID); err != nil {
		t.Fatalf("update current payload hash: %v", err)
	}

	_, err = store.ResolveApproval(ctx, ResolveApprovalParams{
		ApprovalID: approval.ID,
		Status:     "approved",
		DecisionBy: "operator",
		Reason:     "approve stale payload",
	})
	if err == nil || !errors.Is(err, ErrApprovalPayloadMismatch) || !strings.Contains(err.Error(), "approval_payload_mismatch") {
		t.Fatalf("ResolveApproval() error = %v, want approval_payload_mismatch", err)
	}

	gotApproval, err := store.GetApproval(ctx, approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if gotApproval.Status != "pending" {
		t.Fatalf("approval status = %q, want pending after mismatch", gotApproval.Status)
	}
}

func TestStorePersistsActionPayloadAndEvidence(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "actions.db")
	defer store.Close()
	_, _, run := createProjectTaskRunFixture(t, ctx, store)

	action, payload, err := store.CreateActionWithPayload(ctx, CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        run.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          "sha256:test",
		PayloadJSON:          `{"pairing":"W7084C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload() error = %v", err)
	}

	if action.WorkflowKey != "flica-tradeboard" || payload.PayloadHash != "sha256:test" {
		t.Fatalf("action=%+v payload=%+v", action, payload)
	}
	if action.WorkflowRunID != run.ID {
		t.Fatalf("action.WorkflowRunID = %d, want %d", action.WorkflowRunID, run.ID)
	}
	if action.CurrentPayloadHash != payload.PayloadHash {
		t.Fatalf("action.CurrentPayloadHash = %q, want %q", action.CurrentPayloadHash, payload.PayloadHash)
	}

	runID := run.ID
	approval, err := store.RequestApproval(ctx, RequestApprovalParams{
		TaskID:      run.TaskID,
		RunID:       &runID,
		ActionID:    &action.ID,
		PayloadHash: payload.PayloadHash,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	gotApproval, err := store.GetApproval(ctx, approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if gotApproval.ActionID == nil || *gotApproval.ActionID != action.ID || gotApproval.PayloadHash != payload.PayloadHash {
		t.Fatalf("GetApproval() = %+v, want action approval binding", gotApproval)
	}

	event, err := store.AppendActionEvidence(ctx, AppendActionEvidenceParams{
		ActionID:     action.ID,
		EventType:    "action.prepared",
		EventVersion: 1,
		PayloadHash:  "sha256:test",
		ApprovalID:   &approval.ID,
		RunID:        &runID,
		Source:       "test",
		EvidenceJSON: `{"status":"prepared"}`,
	})
	if err != nil {
		t.Fatalf("AppendActionEvidence() error = %v", err)
	}
	if event.ActionID != action.ID || event.PayloadHash == nil || *event.PayloadHash != "sha256:test" {
		t.Fatalf("event=%+v, want action payload evidence", event)
	}
	if event.ApprovalID == nil || *event.ApprovalID != approval.ID {
		t.Fatalf("event.ApprovalID = %v, want %d", event.ApprovalID, approval.ID)
	}

	gotAction, gotPayload, err := store.GetAction(ctx, action.ID)
	if err != nil {
		t.Fatalf("GetAction() error = %v", err)
	}
	if gotAction.ID != action.ID || gotPayload.ID != payload.ID {
		t.Fatalf("GetAction() = %+v %+v, want action %d payload %d", gotAction, gotPayload, action.ID, payload.ID)
	}

	actions, err := store.ListActions(ctx, ListActionsParams{WorkflowRunID: &runID})
	if err != nil {
		t.Fatalf("ListActions() error = %v", err)
	}
	if len(actions) != 1 || actions[0].ID != action.ID {
		t.Fatalf("ListActions() = %+v, want action %d", actions, action.ID)
	}

	events, err := store.ListActionEvidence(ctx, action.ID)
	if err != nil {
		t.Fatalf("ListActionEvidence() error = %v", err)
	}
	if len(events) != 1 || events[0].ID != event.ID {
		t.Fatalf("ListActionEvidence() = %+v, want event %d", events, event.ID)
	}
}

func TestStoreRejectsDuplicateActionPayloadHash(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "duplicate-action-payload.db")
	defer store.Close()
	_, _, run := createProjectTaskRunFixture(t, ctx, store)

	action, payload, err := store.CreateActionWithPayload(ctx, CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        run.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          "sha256:duplicate",
		PayloadJSON:          `{"pairing":"W7084C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload() error = %v", err)
	}

	_, err = store.DB().ExecContext(ctx, `
		INSERT INTO action_payloads (
			action_id,
			payload_schema,
			payload_schema_version,
			payload_hash,
			payload_json,
			submit_path,
			readback_path,
			proof_requirement,
			created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		action.ID,
		payload.PayloadSchema,
		payload.PayloadSchemaVersion,
		payload.PayloadHash,
		payload.PayloadJSON,
		payload.SubmitPath,
		payload.ReadbackPath,
		payload.ProofRequirement,
		formatTime(store.now()),
	)
	if err == nil {
		t.Fatal("duplicate action payload insert succeeded, want unique constraint failure")
	}
	if !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("duplicate action payload error = %v, want unique constraint failure", err)
	}
}

func TestStorePersistsKnowledgeSourceArtifactExtractionAndChunks(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "knowledge-source.db")
	defer store.Close()

	artifact, err := store.RecordKnowledgeArtifact(ctx, RecordKnowledgeArtifactParams{
		SHA256:       "sha256:abc",
		SizeBytes:    42,
		SourceType:   "text",
		MimeType:     "text/plain",
		ArtifactPath: "knowledge/artifacts/ab/abc/source.txt",
		OriginalPath: "/tmp/source.txt",
	})
	if err != nil {
		t.Fatalf("RecordKnowledgeArtifact() error = %v", err)
	}
	if artifact.SHA256 != "sha256:abc" || artifact.SizeBytes != 42 || artifact.SourceType != "text" || artifact.MimeType != "text/plain" {
		t.Fatalf("artifact = %+v, want persisted hash, size, type, and mime type", artifact)
	}
	if artifact.ArtifactPath != "knowledge/artifacts/ab/abc/source.txt" || artifact.OriginalPath != "/tmp/source.txt" {
		t.Fatalf("artifact = %+v, want persisted artifact and original paths", artifact)
	}

	duplicateArtifact, err := store.RecordKnowledgeArtifact(ctx, RecordKnowledgeArtifactParams{
		SHA256:       "sha256:abc",
		SizeBytes:    42,
		SourceType:   "text",
		MimeType:     "text/plain",
		ArtifactPath: "knowledge/artifacts/ab/abc/source.txt",
		OriginalPath: "/tmp/source.txt",
	})
	if err != nil {
		t.Fatalf("RecordKnowledgeArtifact() duplicate error = %v", err)
	}
	if duplicateArtifact.ID != artifact.ID {
		t.Fatalf("duplicate artifact ID = %d, want %d", duplicateArtifact.ID, artifact.ID)
	}

	source, err := store.UpsertKnowledgeSource(ctx, UpsertKnowledgeSourceParams{
		Key:               "pilot-contract",
		Title:             "Pilot Contract",
		Scope:             "global",
		ScopeKey:          "global",
		Restricted:        true,
		SourceKind:        "pilot_contract",
		SourceClass:       "text",
		Lifecycle:         "artifact_available",
		ManifestPath:      "memory/knowledge/pilot-contract.md",
		CurrentArtifactID: &artifact.ID,
	})
	if err != nil {
		t.Fatalf("UpsertKnowledgeSource() error = %v", err)
	}
	if source.Key != "pilot-contract" || source.Title != "Pilot Contract" || source.Scope != "global" || source.ScopeKey != "global" {
		t.Fatalf("source = %+v, want persisted key/title/scope", source)
	}
	if !source.Restricted || source.SourceKind != "pilot_contract" || source.SourceClass != "text" || source.Lifecycle != "artifact_available" {
		t.Fatalf("source = %+v, want persisted kind/class/lifecycle/restricted flag", source)
	}
	if source.ManifestPath != "memory/knowledge/pilot-contract.md" || source.CurrentArtifactID == nil || *source.CurrentArtifactID != artifact.ID {
		t.Fatalf("source = %+v, want manifest and current artifact", source)
	}

	extraction, err := store.RecordKnowledgeExtraction(ctx, RecordKnowledgeExtractionParams{
		SourceID:               source.ID,
		ArtifactID:             artifact.ID,
		ExtractorName:          "plain_text",
		ExtractorVersion:       "v1",
		Status:                 "succeeded",
		Lifecycle:              "ready",
		ExtractedTextHash:      "sha256:text",
		NormalizedMarkdownPath: "state/knowledge/normalized/pilot-contract.md",
	})
	if err != nil {
		t.Fatalf("RecordKnowledgeExtraction() error = %v", err)
	}
	if extraction.SourceID != source.ID || extraction.ArtifactID != artifact.ID || extraction.ExtractorName != "plain_text" || extraction.ExtractorVersion != "v1" {
		t.Fatalf("extraction = %+v, want persisted extraction provenance", extraction)
	}
	if extraction.Status != "succeeded" || extraction.ExtractedTextHash != "sha256:text" || extraction.NormalizedMarkdownPath != "state/knowledge/normalized/pilot-contract.md" {
		t.Fatalf("extraction = %+v, want persisted extraction status and outputs", extraction)
	}

	reloadedSource, err := store.GetKnowledgeSourceByKey(ctx, "pilot-contract")
	if err != nil {
		t.Fatalf("GetKnowledgeSourceByKey() error = %v", err)
	}
	if reloadedSource.CurrentExtractionID == nil || *reloadedSource.CurrentExtractionID != extraction.ID {
		t.Fatalf("source.CurrentExtractionID = %v, want %d", reloadedSource.CurrentExtractionID, extraction.ID)
	}
	if reloadedSource.Lifecycle != "ready" {
		t.Fatalf("source.Lifecycle = %q, want ready", reloadedSource.Lifecycle)
	}

	chunk, err := store.RecordKnowledgeChunk(ctx, RecordKnowledgeChunkParams{
		SourceID:     source.ID,
		ExtractionID: extraction.ID,
		Ordinal:      1,
		Text:         "Vacation accrual section.",
		Anchor:       "section:vacation",
		Restricted:   true,
	})
	if err != nil {
		t.Fatalf("RecordKnowledgeChunk() error = %v", err)
	}
	if chunk.SourceID != source.ID || chunk.ExtractionID != extraction.ID || chunk.Ordinal != 1 || chunk.Text != "Vacation accrual section." {
		t.Fatalf("chunk = %+v, want persisted restricted chunk", chunk)
	}
	if !chunk.Restricted || chunk.Anchor != "section:vacation" {
		t.Fatalf("chunk = %+v, want restricted chunk anchor", chunk)
	}

	results, err := store.SearchKnowledgeChunks(ctx, SearchKnowledgeChunksParams{
		Query:    "vacation",
		Scope:    "global",
		ScopeKey: "global",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("SearchKnowledgeChunks() error = %v", err)
	}
	if len(results) != 1 || results[0].ChunkID != chunk.ID {
		t.Fatalf("results = %+v, want chunk %d", results, chunk.ID)
	}
	if results[0].SourceKey != "pilot-contract" || results[0].Title != "Pilot Contract" || results[0].Text != chunk.Text {
		t.Fatalf("result = %+v, want source and chunk text", results[0])
	}
	if !results[0].Restricted || results[0].Anchor != "section:vacation" {
		t.Fatalf("result = %+v, want restricted result with citation anchor", results[0])
	}
}

func TestStoreRecordsRestrictedKnowledgeUseApprovalWithoutChangingLifecycle(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "restricted-knowledge-use-approval.db")
	defer store.Close()

	artifact, err := store.RecordKnowledgeArtifact(ctx, RecordKnowledgeArtifactParams{
		SHA256:       "sha256:restricted",
		SizeBytes:    128,
		SourceType:   "text",
		MimeType:     "text/plain",
		ArtifactPath: "knowledge/artifacts/re/restricted/source.txt",
		OriginalPath: "/tmp/restricted-source.txt",
	})
	if err != nil {
		t.Fatalf("RecordKnowledgeArtifact() error = %v", err)
	}

	source, err := store.UpsertKnowledgeSource(ctx, UpsertKnowledgeSourceParams{
		Key:               "restricted-manual",
		Title:             "Restricted Manual",
		Scope:             "global",
		ScopeKey:          "global",
		Restricted:        true,
		SourceKind:        "manual",
		SourceClass:       "text",
		Lifecycle:         "ready",
		ManifestPath:      "memory/knowledge/restricted-manual.md",
		CurrentArtifactID: &artifact.ID,
	})
	if err != nil {
		t.Fatalf("UpsertKnowledgeSource() error = %v", err)
	}

	approval, err := store.RecordRestrictedKnowledgeUseApproval(ctx, RecordRestrictedKnowledgeUseApprovalParams{
		SourceID:     source.ID,
		UseType:      "executor_context_injection",
		Reason:       "Operator approved a narrow task-scoped context injection.",
		Decision:     "approved",
		EvidenceJSON: `{"approved_by":"operator"}`,
		DecidedBy:    "operator",
	})
	if err != nil {
		t.Fatalf("RecordRestrictedKnowledgeUseApproval() error = %v", err)
	}
	if approval.SourceID != source.ID || approval.UseType != "executor_context_injection" || approval.Decision != "approved" {
		t.Fatalf("approval = %+v, want persisted restricted use approval", approval)
	}

	reloadedSource, err := store.GetKnowledgeSourceByKey(ctx, "restricted-manual")
	if err != nil {
		t.Fatalf("GetKnowledgeSourceByKey() error = %v", err)
	}
	if reloadedSource.Lifecycle != "ready" {
		t.Fatalf("Lifecycle = %q, want ready", reloadedSource.Lifecycle)
	}
}

func TestStoreRejectsInvalidActionApprovalBinding(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "invalid-action-approval-binding.db")
	defer store.Close()
	_, _, run := createProjectTaskRunFixture(t, ctx, store)

	action, _, err := store.CreateActionWithPayload(ctx, CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        run.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          "sha256:valid",
		PayloadJSON:          `{"pairing":"W7084C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload() error = %v", err)
	}

	runID := run.ID
	if _, err := store.RequestApproval(ctx, RequestApprovalParams{
		TaskID:      run.TaskID,
		RunID:       &runID,
		ActionID:    &action.ID,
		PayloadHash: "sha256:wrong",
		Status:      "pending",
		RequestedBy: "system",
	}); err == nil || !strings.Contains(err.Error(), "invalid action approval binding") {
		t.Fatalf("RequestApproval() error = %v, want invalid action approval binding", err)
	}
}

func TestStoreRejectsActionApprovalBindingForMismatchedRun(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "mismatched-action-approval-run.db")
	defer store.Close()
	project, _, run := createProjectTaskRunFixture(t, ctx, store)
	_, otherRun := createTaskRunForProject(t, ctx, store, project.ID, "other-run")

	action, _, err := store.CreateActionWithPayload(ctx, CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        run.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          "sha256:valid",
		PayloadJSON:          `{"pairing":"W7084C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload() error = %v", err)
	}

	otherRunID := otherRun.ID
	if _, err := store.RequestApproval(ctx, RequestApprovalParams{
		TaskID:      run.TaskID,
		RunID:       &otherRunID,
		ActionID:    &action.ID,
		PayloadHash: "sha256:valid",
		Status:      "pending",
		RequestedBy: "system",
	}); err == nil || !strings.Contains(err.Error(), "invalid action approval binding") {
		t.Fatalf("RequestApproval() error = %v, want invalid action approval binding", err)
	}
}

func TestStoreRejectsActionApprovalBindingForMismatchedTask(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "mismatched-action-approval-task.db")
	defer store.Close()
	project, _, run := createProjectTaskRunFixture(t, ctx, store)
	otherTask, _ := createTaskRunForProject(t, ctx, store, project.ID, "other-task")

	action, _, err := store.CreateActionWithPayload(ctx, CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        run.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          "sha256:valid",
		PayloadJSON:          `{"pairing":"W7084C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload() error = %v", err)
	}

	runID := run.ID
	if _, err := store.RequestApproval(ctx, RequestApprovalParams{
		TaskID:      otherTask.ID,
		RunID:       &runID,
		ActionID:    &action.ID,
		PayloadHash: "sha256:valid",
		Status:      "pending",
		RequestedBy: "system",
	}); err == nil || !strings.Contains(err.Error(), "invalid action approval binding") {
		t.Fatalf("RequestApproval() error = %v, want invalid action approval binding", err)
	}
}

func TestStoreRejectsActionEvidenceWithWrongPayloadHash(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "invalid-action-evidence-payload.db")
	defer store.Close()
	_, _, run := createProjectTaskRunFixture(t, ctx, store)

	action, _, err := store.CreateActionWithPayload(ctx, CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        run.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          "sha256:valid",
		PayloadJSON:          `{"pairing":"W7084C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload() error = %v", err)
	}

	runID := run.ID
	if _, err := store.AppendActionEvidence(ctx, AppendActionEvidenceParams{
		ActionID:     action.ID,
		EventType:    "action.prepared",
		EventVersion: 1,
		PayloadHash:  "sha256:wrong",
		RunID:        &runID,
		Source:       "test",
		EvidenceJSON: `{"status":"prepared"}`,
	}); !errors.Is(err, ErrInvalidActionEvidenceLink) {
		t.Fatalf("AppendActionEvidence() error = %v, want invalid action evidence link", err)
	}
}

func TestStoreRejectsActionEvidenceWithNoncanonicalPayloadHash(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "invalid-action-evidence-payload-whitespace.db")
	defer store.Close()
	_, _, run := createProjectTaskRunFixture(t, ctx, store)

	action, _, err := store.CreateActionWithPayload(ctx, CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        run.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          "sha256:valid",
		PayloadJSON:          `{"pairing":"W7084C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload() error = %v", err)
	}

	runID := run.ID
	if _, err := store.AppendActionEvidence(ctx, AppendActionEvidenceParams{
		ActionID:     action.ID,
		EventType:    "action.prepared",
		EventVersion: 1,
		PayloadHash:  " sha256:valid ",
		RunID:        &runID,
		Source:       "test",
		EvidenceJSON: `{"status":"prepared"}`,
	}); !errors.Is(err, ErrInvalidActionEvidenceLink) {
		t.Fatalf("AppendActionEvidence() error = %v, want invalid action evidence link", err)
	}
}

func TestStoreRejectsActionEvidenceWithMismatchedRun(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "invalid-action-evidence-run.db")
	defer store.Close()
	project, _, run := createProjectTaskRunFixture(t, ctx, store)
	_, otherRun := createTaskRunForProject(t, ctx, store, project.ID, "other-evidence-run")

	action, _, err := store.CreateActionWithPayload(ctx, CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        run.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          "sha256:valid",
		PayloadJSON:          `{"pairing":"W7084C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload() error = %v", err)
	}

	otherRunID := otherRun.ID
	if _, err := store.AppendActionEvidence(ctx, AppendActionEvidenceParams{
		ActionID:     action.ID,
		EventType:    "action.prepared",
		EventVersion: 1,
		RunID:        &otherRunID,
		Source:       "test",
		EvidenceJSON: `{"status":"prepared"}`,
	}); !errors.Is(err, ErrInvalidActionEvidenceLink) {
		t.Fatalf("AppendActionEvidence() error = %v, want invalid action evidence link", err)
	}
}

func TestStoreRejectsActionEvidenceWithMismatchedApproval(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "invalid-action-evidence-approval.db")
	defer store.Close()
	project, _, run := createProjectTaskRunFixture(t, ctx, store)
	otherTask, otherRun := createTaskRunForProject(t, ctx, store, project.ID, "other-evidence-approval")

	action, _, err := store.CreateActionWithPayload(ctx, CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        run.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          "sha256:valid",
		PayloadJSON:          `{"pairing":"W7084C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload() error = %v", err)
	}
	otherAction, _, err := store.CreateActionWithPayload(ctx, CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        otherRun.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          "sha256:other",
		PayloadJSON:          `{"pairing":"W7085C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload(other) error = %v", err)
	}

	otherRunID := otherRun.ID
	approval, err := store.RequestApproval(ctx, RequestApprovalParams{
		TaskID:      otherTask.ID,
		RunID:       &otherRunID,
		ActionID:    &otherAction.ID,
		PayloadHash: "sha256:other",
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval(other) error = %v", err)
	}

	runID := run.ID
	if _, err := store.AppendActionEvidence(ctx, AppendActionEvidenceParams{
		ActionID:     action.ID,
		EventType:    "action.prepared",
		EventVersion: 1,
		PayloadHash:  "sha256:valid",
		ApprovalID:   &approval.ID,
		RunID:        &runID,
		Source:       "test",
		EvidenceJSON: `{"status":"prepared"}`,
	}); !errors.Is(err, ErrInvalidActionEvidenceLink) {
		t.Fatalf("AppendActionEvidence() error = %v, want invalid action evidence link", err)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	return openMigratedTestStore(t, "odin.db")
}

func createProjectTaskRunFixture(t *testing.T, ctx context.Context, store *Store) (Project, Task, Run) {
	t.Helper()

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		GitHubRepo:    "example/odin-os",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "action-evidence",
		Title:       "Prepare action evidence",
		Status:      "running",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := store.StartRun(ctx, StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	return project, task, run
}

func createTaskRunForProject(t *testing.T, ctx context.Context, store *Store, projectID int64, key string) (Task, Run) {
	t.Helper()

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   projectID,
		Key:         key,
		Title:       "Prepare action evidence",
		Status:      "running",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(%s) error = %v", key, err)
	}

	run, err := store.StartRun(ctx, StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(%s) error = %v", key, err)
	}

	return task, run
}

func TestStoreMigrateLifecycleAndReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() first run error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() second run error = %v", err)
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		GitHubRepo:    "example/odin-os",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "phase-03",
		Title:       "Implement runtime store",
		Status:      "queued",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	task, err = store.UpdateTaskStatus(ctx, UpdateTaskStatusParams{
		TaskID: task.ID,
		Status: "running",
	})
	if err != nil {
		t.Fatalf("UpdateTaskStatus(running) error = %v", err)
	}

	run, err := store.StartRun(ctx, StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	run, err = store.FinishRun(ctx, FinishRunParams{
		RunID:   run.ID,
		Status:  "completed",
		Summary: "store baseline complete",
	})
	if err != nil {
		t.Fatalf("FinishRun() error = %v", err)
	}

	task, err = store.UpdateTaskStatus(ctx, UpdateTaskStatusParams{
		TaskID: task.ID,
		Status: "completed",
	})
	if err != nil {
		t.Fatalf("UpdateTaskStatus(completed) error = %v", err)
	}

	approval, err := store.RequestApproval(ctx, RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	approval, err = store.ResolveApproval(ctx, ResolveApprovalParams{
		ApprovalID: approval.ID,
		Status:     "approved",
		DecisionBy: "operator",
		Reason:     "safe to proceed",
	})
	if err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	incident, err := store.OpenIncident(ctx, OpenIncidentParams{
		RunID:       &run.ID,
		Severity:    "warning",
		Status:      "open",
		Summary:     "transient issue observed",
		DetailsJSON: `{"stage":"verification"}`,
	})
	if err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}

	recovery, err := store.StartRecovery(ctx, StartRecoveryParams{
		IncidentID:  &incident.ID,
		RunID:       &run.ID,
		Status:      "running",
		Strategy:    "retry-once",
		DetailsJSON: `{"attempt":1}`,
	})
	if err != nil {
		t.Fatalf("StartRecovery() error = %v", err)
	}

	recovery, err = store.CompleteRecovery(ctx, CompleteRecoveryParams{
		RecoveryID:  recovery.ID,
		Status:      "completed",
		DetailsJSON: `{"result":"success"}`,
	})
	if err != nil {
		t.Fatalf("CompleteRecovery() error = %v", err)
	}

	if _, err := store.RecordRegistryVersion(ctx, RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "abc123",
		Notes:       "phase 02 baseline",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}

	if _, err := store.RecordExecutorHealth(ctx, RecordExecutorHealthParams{
		Executor:    "codex",
		Status:      "healthy",
		LatencyMS:   42,
		DetailsJSON: `{"mode":"local"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}

	if _, err := store.CreateContextPacket(ctx, CreateContextPacketParams{
		TaskID:      &task.ID,
		RunID:       &run.ID,
		PacketKind:  "wake",
		Summary:     "handoff state",
		PayloadJSON: `{"task":"phase-03"}`,
	}); err != nil {
		t.Fatalf("CreateContextPacket() error = %v", err)
	}

	allEvents, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents(all) error = %v", err)
	}

	if len(allEvents) != 14 {
		t.Fatalf("ListEvents(all) len = %d, want 14", len(allEvents))
	}

	if allEvents[0].Type != runtimeevents.EventProjectCreated {
		t.Fatalf("first event type = %q, want %q", allEvents[0].Type, runtimeevents.EventProjectCreated)
	}

	packetEventPayload, err := runtimeevents.DecodePayload[runtimeevents.ContextPacketCreatedPayload](allEvents[len(allEvents)-1].Payload)
	if err != nil {
		t.Fatalf("DecodePayload(ContextPacketCreatedPayload) error = %v", err)
	}
	if packetEventPayload.PacketScope != "task_wake_packet" {
		t.Fatalf("context packet event scope = %q, want %q", packetEventPayload.PacketScope, "task_wake_packet")
	}
	if packetEventPayload.Trigger != "handoff" {
		t.Fatalf("context packet event trigger = %q, want %q", packetEventPayload.Trigger, "handoff")
	}

	views, err := projections.ListTaskStatusViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListTaskStatusViews() error = %v", err)
	}
	if len(views) != 1 || views[0].Status != "completed" {
		t.Fatalf("task views = %+v, want one completed task", views)
	}

	pendingApprovals, err := projections.ListPendingApprovalViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListPendingApprovalViews() error = %v", err)
	}
	if len(pendingApprovals) != 0 {
		t.Fatalf("pending approvals = %d, want 0", len(pendingApprovals))
	}

	runViews, err := projections.ListRunSummaryViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListRunSummaryViews() error = %v", err)
	}
	if len(runViews) != 1 || runViews[0].Status != "completed" {
		t.Fatalf("run views = %+v, want one completed run", runViews)
	}

	projectViews, err := projections.ListProjectTransitionViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListProjectTransitionViews() error = %v", err)
	}
	if len(projectViews) != 1 || projectViews[0].TaskCount != 1 {
		t.Fatalf("project views = %+v, want one project with one task", projectViews)
	}

	var migrationCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("schema_migrations count query error = %v", err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}
	if migrationCount != len(migrations) {
		t.Fatalf("schema_migrations count = %d, want %d", migrationCount, len(migrations))
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	defer reopened.Close()

	if err := reopened.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(reopen) error = %v", err)
	}

	gotTask, err := reopened.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "completed" {
		t.Fatalf("GetTask().Status = %q, want %q", gotTask.Status, "completed")
	}

	gotRun, err := reopened.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "completed" {
		t.Fatalf("GetRun().Status = %q, want %q", gotRun.Status, "completed")
	}

	gotApproval, err := reopened.GetApproval(ctx, approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if gotApproval.Status != "approved" {
		t.Fatalf("GetApproval().Status = %q, want %q", gotApproval.Status, "approved")
	}
}

func TestProjectTransitionStateLifecycle(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "cfipros",
		Name:          "CFIPros",
		Scope:         "project",
		GitRoot:       "/tmp/cfipros",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	transition, err := store.SetProjectTransition(ctx, SetProjectTransitionParams{
		ProjectID:          project.ID,
		State:              "inventory",
		Controller:         "legacy_odin",
		LimitedActionsJSON: "",
		Notes:              "initial enrollment",
		ChangedBy:          "operator",
	})
	if err != nil {
		t.Fatalf("SetProjectTransition(inventory) error = %v", err)
	}

	if transition.State != "inventory" {
		t.Fatalf("transition.State = %q, want %q", transition.State, "inventory")
	}
	if transition.Controller != "legacy_odin" {
		t.Fatalf("transition.Controller = %q, want %q", transition.Controller, "legacy_odin")
	}

	transition, err = store.SetProjectTransition(ctx, SetProjectTransitionParams{
		ProjectID:          project.ID,
		State:              "limited_action",
		Controller:         "odin_os",
		LimitedActionsJSON: `["isolated_mutation"]`,
		Notes:              "allow proposal work only",
		ChangedBy:          "operator",
	})
	if err != nil {
		t.Fatalf("SetProjectTransition(limited_action) error = %v", err)
	}

	if transition.State != "limited_action" {
		t.Fatalf("transition.State = %q, want %q", transition.State, "limited_action")
	}
	if transition.Controller != "odin_os" {
		t.Fatalf("transition.Controller = %q, want %q", transition.Controller, "odin_os")
	}
	if transition.LimitedActionsJSON != `["isolated_mutation"]` {
		t.Fatalf("transition.LimitedActionsJSON = %q, want %q", transition.LimitedActionsJSON, `["isolated_mutation"]`)
	}

	got, err := store.GetProjectTransition(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetProjectTransition() error = %v", err)
	}

	if got.State != "limited_action" {
		t.Fatalf("GetProjectTransition().State = %q, want %q", got.State, "limited_action")
	}

	projectEvents, err := store.ListEvents(ctx, ListEventsParams{
		ProjectID: &project.ID,
	})
	if err != nil {
		t.Fatalf("ListEvents(project) error = %v", err)
	}

	var transitionEvents int
	for _, event := range projectEvents {
		if event.Type == runtimeevents.EventProjectTransitionChanged {
			transitionEvents++
		}
	}
	if transitionEvents != 2 {
		t.Fatalf("transition event count = %d, want 2", transitionEvents)
	}
}

func TestProjectTransitionReportsAreAppendOnly(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "cfipros",
		Name:          "CFIPros",
		Scope:         "project",
		GitRoot:       "/tmp/cfipros",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	if _, err := store.SetProjectTransition(ctx, SetProjectTransitionParams{
		ProjectID:  project.ID,
		State:      "compare",
		Controller: "legacy_odin",
		ChangedBy:  "operator",
		Notes:      "compare before cutover",
	}); err != nil {
		t.Fatalf("SetProjectTransition(compare) error = %v", err)
	}

	shadowReport, err := store.RecordProjectTransitionReport(ctx, RecordProjectTransitionReportParams{
		ProjectID:   project.ID,
		ReportType:  "shadow_observation",
		Summary:     "legacy run observed",
		DetailsJSON: `{"task":"deploy","status":"completed"}`,
	})
	if err != nil {
		t.Fatalf("RecordProjectTransitionReport(shadow) error = %v", err)
	}

	compareReport, err := store.RecordProjectTransitionReport(ctx, RecordProjectTransitionReportParams{
		ProjectID:   project.ID,
		ReportType:  "compare_report",
		Summary:     "decision mismatch",
		DetailsJSON: `{"legacy_summary":"ship","odin_summary":"hold","verdict":"mismatch"}`,
	})
	if err != nil {
		t.Fatalf("RecordProjectTransitionReport(compare) error = %v", err)
	}

	if shadowReport.ID == compareReport.ID {
		t.Fatalf("report ids should differ, both were %d", shadowReport.ID)
	}

	reports, err := store.ListProjectTransitionReports(ctx, project.ID)
	if err != nil {
		t.Fatalf("ListProjectTransitionReports() error = %v", err)
	}

	if len(reports) != 2 {
		t.Fatalf("ListProjectTransitionReports() len = %d, want 2", len(reports))
	}
	if reports[0].ReportType != "shadow_observation" {
		t.Fatalf("reports[0].ReportType = %q, want %q", reports[0].ReportType, "shadow_observation")
	}
	if reports[1].ReportType != "compare_report" {
		t.Fatalf("reports[1].ReportType = %q, want %q", reports[1].ReportType, "compare_report")
	}

	projectEvents, err := store.ListEvents(ctx, ListEventsParams{
		ProjectID: &project.ID,
	})
	if err != nil {
		t.Fatalf("ListEvents(project) error = %v", err)
	}

	var shadowEvents int
	var compareEvents int
	for _, event := range projectEvents {
		switch event.Type {
		case runtimeevents.EventProjectShadowObservationRecorded:
			shadowEvents++
		case runtimeevents.EventProjectCompareReportRecorded:
			compareEvents++
		}
	}

	if shadowEvents != 1 {
		t.Fatalf("shadow event count = %d, want 1", shadowEvents)
	}
	if compareEvents != 1 {
		t.Fatalf("compare event count = %d, want 1", compareEvents)
	}
}

func TestLearningProposalLifecycleSupportsEvaluationPromotionAndRollback(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	firstProposal, err := store.CreateLearningProposal(ctx, CreateLearningProposalParams{
		ProposalType:      "routing_rule_refinement",
		Scope:             "global",
		TargetKey:         "router/default",
		Summary:           "Prefer low-latency primary route",
		Hypothesis:        "Lower latency without more policy violations",
		ChangePayloadJSON: `{"executor":"codex","priority":10}`,
		CreatedBy:         "odin",
		Status:            "draft",
	})
	if err != nil {
		t.Fatalf("CreateLearningProposal(first) error = %v", err)
	}

	firstProposal, err = store.UpdateLearningProposalStatus(ctx, UpdateLearningProposalStatusParams{
		ProposalID: firstProposal.ID,
		Status:     "submitted",
	})
	if err != nil {
		t.Fatalf("UpdateLearningProposalStatus(submitted) error = %v", err)
	}

	firstEvaluation, err := store.RecordLearningEvaluation(ctx, RecordLearningEvaluationParams{
		ProposalID:           firstProposal.ID,
		FixtureKey:           "router-latency-fixture",
		Mode:                 "replay",
		Score:                0.82,
		BaselineSummaryJSON:  `{"success_rate":0.93,"latency_ms":220,"policy_violations":0}`,
		CandidateSummaryJSON: `{"success_rate":0.94,"latency_ms":180,"policy_violations":0}`,
		ResultSummary:        "candidate improved latency while preserving policy compliance",
		Outcome:              "approved",
	})
	if err != nil {
		t.Fatalf("RecordLearningEvaluation(first) error = %v", err)
	}

	if firstEvaluation.Outcome != "approved" {
		t.Fatalf("first evaluation outcome = %q, want %q", firstEvaluation.Outcome, "approved")
	}

	firstProposal, err = store.UpdateLearningProposalStatus(ctx, UpdateLearningProposalStatusParams{
		ProposalID: firstProposal.ID,
		Status:     "approved",
	})
	if err != nil {
		t.Fatalf("UpdateLearningProposalStatus(approved) error = %v", err)
	}

	firstPromotion, err := store.PromoteLearningProposal(ctx, PromoteLearningProposalParams{
		ProposalID: firstProposal.ID,
		PromotedBy: "operator",
	})
	if err != nil {
		t.Fatalf("PromoteLearningProposal(first) error = %v", err)
	}

	if firstPromotion.Status != "active" {
		t.Fatalf("first promotion status = %q, want %q", firstPromotion.Status, "active")
	}

	secondProposal, err := store.CreateLearningProposal(ctx, CreateLearningProposalParams{
		ProposalType:      "routing_rule_refinement",
		Scope:             "global",
		TargetKey:         "router/default",
		Summary:           "Prefer lower-cost route",
		Hypothesis:        "Lower cost while keeping success rate stable",
		ChangePayloadJSON: `{"executor":"openai_api","priority":20}`,
		CreatedBy:         "odin",
		Status:            "draft",
	})
	if err != nil {
		t.Fatalf("CreateLearningProposal(second) error = %v", err)
	}

	secondProposal, err = store.UpdateLearningProposalStatus(ctx, UpdateLearningProposalStatusParams{
		ProposalID: secondProposal.ID,
		Status:     "submitted",
	})
	if err != nil {
		t.Fatalf("UpdateLearningProposalStatus(second submitted) error = %v", err)
	}

	if _, err := store.RecordLearningEvaluation(ctx, RecordLearningEvaluationParams{
		ProposalID:           secondProposal.ID,
		FixtureKey:           "router-cost-fixture",
		Mode:                 "sandbox",
		Score:                0.87,
		BaselineSummaryJSON:  `{"success_rate":0.94,"cost":0.021,"violations":0}`,
		CandidateSummaryJSON: `{"success_rate":0.94,"cost":0.015,"violations":0}`,
		ResultSummary:        "candidate reduced cost without quality regression",
		Outcome:              "approved",
	}); err != nil {
		t.Fatalf("RecordLearningEvaluation(second) error = %v", err)
	}

	secondProposal, err = store.UpdateLearningProposalStatus(ctx, UpdateLearningProposalStatusParams{
		ProposalID: secondProposal.ID,
		Status:     "approved",
	})
	if err != nil {
		t.Fatalf("UpdateLearningProposalStatus(second approved) error = %v", err)
	}

	secondPromotion, err := store.PromoteLearningProposal(ctx, PromoteLearningProposalParams{
		ProposalID: secondProposal.ID,
		PromotedBy: "operator",
	})
	if err != nil {
		t.Fatalf("PromoteLearningProposal(second) error = %v", err)
	}

	if secondPromotion.Status != "active" {
		t.Fatalf("second promotion status = %q, want %q", secondPromotion.Status, "active")
	}
	if secondPromotion.SupersedesPromotionID == nil || *secondPromotion.SupersedesPromotionID != firstPromotion.ID {
		t.Fatalf("second promotion supersedes = %v, want %d", secondPromotion.SupersedesPromotionID, firstPromotion.ID)
	}

	activePromotions, err := store.ListActiveLearningPromotions(ctx)
	if err != nil {
		t.Fatalf("ListActiveLearningPromotions() error = %v", err)
	}
	if len(activePromotions) != 1 || activePromotions[0].ID != secondPromotion.ID {
		t.Fatalf("active promotions = %+v, want second promotion %d", activePromotions, secondPromotion.ID)
	}

	rolledBack, err := store.RollbackLearningPromotion(ctx, RollbackLearningPromotionParams{
		PromotionID:    secondPromotion.ID,
		RolledBackBy:   "operator",
		RollbackReason: "cost win was too narrow under review",
	})
	if err != nil {
		t.Fatalf("RollbackLearningPromotion() error = %v", err)
	}

	if rolledBack.Status != "rolled_back" {
		t.Fatalf("rolled back promotion status = %q, want %q", rolledBack.Status, "rolled_back")
	}

	activePromotions, err = store.ListActiveLearningPromotions(ctx)
	if err != nil {
		t.Fatalf("ListActiveLearningPromotions(after rollback) error = %v", err)
	}
	if len(activePromotions) != 1 || activePromotions[0].ID != firstPromotion.ID {
		t.Fatalf("active promotions after rollback = %+v, want first promotion %d", activePromotions, firstPromotion.ID)
	}

	firstPromotionAfterRollback, err := store.GetLearningPromotion(ctx, firstPromotion.ID)
	if err != nil {
		t.Fatalf("GetLearningPromotion(first) error = %v", err)
	}
	if firstPromotionAfterRollback.Status != "active" {
		t.Fatalf("first promotion after rollback status = %q, want %q", firstPromotionAfterRollback.Status, "active")
	}

	evaluations, err := store.ListLearningEvaluations(ctx, firstProposal.ID)
	if err != nil {
		t.Fatalf("ListLearningEvaluations(first proposal) error = %v", err)
	}
	if len(evaluations) != 1 {
		t.Fatalf("ListLearningEvaluations(first proposal) len = %d, want 1", len(evaluations))
	}

	allEvents, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents(all) error = %v", err)
	}

	counts := make(map[runtimeevents.Type]int)
	for _, event := range allEvents {
		counts[event.Type]++
	}

	if counts[runtimeevents.EventLearningProposalCreated] != 2 {
		t.Fatalf("learning.proposal_created count = %d, want 2", counts[runtimeevents.EventLearningProposalCreated])
	}
	if counts[runtimeevents.EventLearningProposalSubmitted] != 2 {
		t.Fatalf("learning.proposal_submitted count = %d, want 2", counts[runtimeevents.EventLearningProposalSubmitted])
	}
	if counts[runtimeevents.EventLearningEvaluationRecorded] != 2 {
		t.Fatalf("learning.evaluation_recorded count = %d, want 2", counts[runtimeevents.EventLearningEvaluationRecorded])
	}
	if counts[runtimeevents.EventLearningPromotionApplied] != 2 {
		t.Fatalf("learning.promotion_applied count = %d, want 2", counts[runtimeevents.EventLearningPromotionApplied])
	}
	if counts[runtimeevents.EventLearningPromotionRolledBack] != 1 {
		t.Fatalf("learning.promotion_rolled_back count = %d, want 1", counts[runtimeevents.EventLearningPromotionRolledBack])
	}
}
