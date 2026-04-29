package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

func TestActionsShellShowsFixtureEvidence(t *testing.T) {
	ctx := context.Background()
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)
	runtimeRoot := t.TempDir()
	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	now := time.Date(2026, 4, 29, 15, 30, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	_, task, run := seedTaskRunFixture(t, ctx, store, "flica-fixture", "project", "flica-action-fixture", "FLICA action fixture", "codex_headless", now)

	action, payload, err := store.CreateActionWithPayload(ctx, sqlite.CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        run.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          "sha256:fixture",
		PayloadJSON:          `{"action_type":"tradeboard_action","operation":"post","pairing":"W7084C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload() error = %v", err)
	}

	approval, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		ActionID:    &action.ID,
		PayloadHash: payload.PayloadHash,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	for _, eventType := range []runtimeevents.Type{
		runtimeevents.EventActionPrepared,
		runtimeevents.EventActionExternallyReadBack,
	} {
		if _, err := store.AppendActionEvidence(ctx, sqlite.AppendActionEvidenceParams{
			ActionID:     action.ID,
			EventType:    string(eventType),
			EventVersion: 1,
			PayloadHash:  payload.PayloadHash,
			ApprovalID:   &approval.ID,
			RunID:        &run.ID,
			Source:       "fixture",
			EvidenceJSON: `{"status":"recorded"}`,
		}); err != nil {
			t.Fatalf("AppendActionEvidence(%s) error = %v", eventType, err)
		}
	}

	stdout, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "/help\n/actions\n/actions 1\n/actions 1 evidence\n/exit\n")
	if err != nil {
		t.Fatalf("odin actions fixture error = %v\n%s", err, stdout)
	}

	for _, want := range []string{
		"/actions",
		"workflow=flica-tradeboard",
		"payload_hash=sha256:fixture",
		"payload_json={\"action_type\":\"tradeboard_action\",\"operation\":\"post\",\"pairing\":\"W7084C\"}",
		"approval_id=",
		"action.prepared",
		"action.externally_read_back",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("odin actions output missing %q:\n%s", want, stdout)
		}
	}
}
