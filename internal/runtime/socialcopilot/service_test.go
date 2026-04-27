package socialcopilot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"odin-os/internal/store/sqlite"
)

func TestEnsurePollingJobCreatesOneScheduledWorkflowTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSocialCopilotStore(t)
	defer store.Close()
	core := seedOdinCoreProject(t, ctx, store)

	service := Service{
		Store: store,
		Now: func() time.Time {
			return time.Date(2026, 4, 24, 2, 0, 0, 0, time.UTC)
		},
	}

	status, err := service.EnsurePollingJob(ctx, EnsureJobParams{
		WorkflowKey: "marcus-social-growth-workflow",
		WatchScope: WatchScopeInput{
			MarcusOwnedSurfaces: []string{"timeline"},
		},
		Cadence: 30 * time.Minute,
	})
	if err != nil {
		t.Fatalf("EnsurePollingJob() error = %v", err)
	}

	if status.Task.ProjectID != core.ID {
		t.Fatalf("Task.ProjectID = %d, want odin-core project %d", status.Task.ProjectID, core.ID)
	}
	if status.Task.Key != "workflow-marcus-social-growth-workflow-social-copilot-loop" {
		t.Fatalf("Task.Key = %q, want fixed workflow-owned key", status.Task.Key)
	}
	if status.Task.Scope != "workflow" {
		t.Fatalf("Task.Scope = %q, want workflow", status.Task.Scope)
	}
	if status.Task.Status != "scheduled" {
		t.Fatalf("Task.Status = %q, want scheduled", status.Task.Status)
	}
	if status.Task.RequestedBy != "workflow:marcus-social-growth-workflow" {
		t.Fatalf("Task.RequestedBy = %q, want workflow ownership", status.Task.RequestedBy)
	}
	if len(status.WatchScope.Targets) != 1 || status.WatchScope.Targets[0].StableKey != "marcus_own_timeline" {
		t.Fatalf("WatchScope = %+v, want one Marcus own timeline target", status.WatchScope.Targets)
	}
}

func TestEnsurePollingJobReusesExistingWorkflowTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSocialCopilotStore(t)
	defer store.Close()
	seedOdinCoreProject(t, ctx, store)

	service := Service{Store: store, Now: time.Now}
	first, err := service.EnsurePollingJob(ctx, EnsureJobParams{
		WorkflowKey: "marcus-social-growth-workflow",
		WatchScope: WatchScopeInput{
			MarcusOwnedSurfaces: []string{"timeline"},
		},
		Cadence: time.Hour,
	})
	if err != nil {
		t.Fatalf("EnsurePollingJob(first) error = %v", err)
	}

	second, err := service.EnsurePollingJob(ctx, EnsureJobParams{
		WorkflowKey: "marcus-social-growth-workflow",
		WatchScope: WatchScopeInput{
			MarcusOwnedSurfaces: []string{"mentions"},
		},
		Cadence: time.Hour,
	})
	if err != nil {
		t.Fatalf("EnsurePollingJob(second) error = %v", err)
	}

	if second.Task.ID != first.Task.ID {
		t.Fatalf("second task ID = %d, want reused task ID %d", second.Task.ID, first.Task.ID)
	}
}

func TestReplaceWatchScopeAppendsWorkflowJobMetadataPacket(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSocialCopilotStore(t)
	defer store.Close()
	seedOdinCoreProject(t, ctx, store)

	service := Service{Store: store, Now: time.Now}
	status, err := service.ReplaceWatchScope(ctx, "marcus-social-growth-workflow", WatchScopeInput{
		MarcusOwnedSurfaces: []string{"timeline", "mentions"},
		ExplicitTargetURLs:  []string{"https://x.com/example/status/12345"},
		WatchlistEntries: []WatchlistEntryInput{{
			Kind:   "account",
			Target: "@AviationDaily",
		}},
	})
	if err != nil {
		t.Fatalf("ReplaceWatchScope() error = %v", err)
	}

	packets, err := store.ListContextPackets(ctx, sqlite.ListContextPacketsParams{
		TaskID:      &status.Task.ID,
		PacketScope: "workflow_job_metadata",
	})
	if err != nil {
		t.Fatalf("ListContextPackets() error = %v", err)
	}
	if len(packets) != 1 {
		t.Fatalf("metadata packets len = %d, want 1", len(packets))
	}
	if packets[0].Trigger != "watch_scope_replace" {
		t.Fatalf("Trigger = %q, want watch_scope_replace", packets[0].Trigger)
	}
	if packets[0].CheckpointKey != "social-copilot/marcus-social-growth-workflow/social-copilot-loop" {
		t.Fatalf("CheckpointKey = %q, want social copilot checkpoint key", packets[0].CheckpointKey)
	}

	var metadata jobMetadata
	if err := json.Unmarshal([]byte(packets[0].PayloadJSON), &metadata); err != nil {
		t.Fatalf("json.Unmarshal(metadata) error = %v", err)
	}
	if metadata.WorkflowKey != "marcus-social-growth-workflow" {
		t.Fatalf("WorkflowKey = %q, want Marcus workflow", metadata.WorkflowKey)
	}
	if len(metadata.WatchScope.Targets) != 4 {
		t.Fatalf("WatchScope.Targets len = %d, want 4", len(metadata.WatchScope.Targets))
	}
	if metadata.AccountActions != "none" {
		t.Fatalf("AccountActions = %q, want none", metadata.AccountActions)
	}
}

func TestReplaceWatchScopePreservesCheckpointsForRemainingTargetsOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSocialCopilotStore(t)
	defer store.Close()
	seedOdinCoreProject(t, ctx, store)

	service := Service{Store: store, Now: time.Now}
	first, err := service.ReplaceWatchScope(ctx, "marcus-social-growth-workflow", WatchScopeInput{
		ExplicitTargetURLs: []string{
			"https://x.com/example/status/12345",
			"https://x.com/example/status/67890",
		},
	})
	if err != nil {
		t.Fatalf("ReplaceWatchScope(first) error = %v", err)
	}

	firstMetadata := jobMetadata{
		WorkflowKey: "marcus-social-growth-workflow",
		WatchScope:  first.WatchScope,
		TargetStates: map[string]TargetState{
			"x_post:12345": {
				StableKey:                  "x_post:12345",
				LastCheckedAt:              "2026-04-24T01:00:00Z",
				LastObservationFingerprint: "same-observation",
				NextEligibleAt:             "2026-04-24T02:00:00Z",
				PendingMemoryID:            42,
			},
			"x_post:67890": {
				StableKey:                  "x_post:67890",
				LastCheckedAt:              "2026-04-24T01:05:00Z",
				LastObservationFingerprint: "removed-observation",
			},
		},
		AccountActions: "none",
	}
	firstPayload, err := json.Marshal(firstMetadata)
	if err != nil {
		t.Fatalf("json.Marshal(firstMetadata) error = %v", err)
	}
	if _, err := store.CreateContextPacket(ctx, sqlite.CreateContextPacketParams{
		TaskID:        &first.Task.ID,
		PacketKind:    "social_copilot_job_metadata",
		PacketScope:   "workflow_job_metadata",
		Trigger:       "wake_checkpoint",
		CheckpointKey: "social-copilot/marcus-social-growth-workflow/social-copilot-loop",
		Status:        "active",
		Summary:       "seeded checkpoint",
		PayloadJSON:   string(firstPayload),
	}); err != nil {
		t.Fatalf("CreateContextPacket(seed) error = %v", err)
	}

	replaced, err := service.ReplaceWatchScope(ctx, "marcus-social-growth-workflow", WatchScopeInput{
		ExplicitTargetURLs: []string{"https://x.com/example/status/12345"},
		WatchlistEntries: []WatchlistEntryInput{{
			Kind:   "account",
			Target: "@AviationDaily",
		}},
	})
	if err != nil {
		t.Fatalf("ReplaceWatchScope(second) error = %v", err)
	}

	state, ok := replaced.TargetStates["x_post:12345"]
	if !ok {
		t.Fatalf("TargetStates missing retained x_post:12345: %+v", replaced.TargetStates)
	}
	if state.LastObservationFingerprint != "same-observation" || state.PendingMemoryID != 42 {
		t.Fatalf("retained target state = %+v, want preserved checkpoint fields", state)
	}
	if _, ok := replaced.TargetStates["x_post:67890"]; ok {
		t.Fatalf("removed target state x_post:67890 was preserved: %+v", replaced.TargetStates)
	}
	if _, ok := replaced.TargetStates["x_account:aviationdaily"]; !ok {
		t.Fatalf("new account state missing: %+v", replaced.TargetStates)
	}
}

func TestManualWakeHonorsCooldownForSameObservation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSocialCopilotStore(t)
	defer store.Close()
	seedOdinCoreProject(t, ctx, store)

	fixedNow := time.Date(2026, 4, 24, 2, 0, 0, 0, time.UTC)
	service := Service{Store: store, Now: func() time.Time { return fixedNow }}
	status, err := service.ReplaceWatchScope(ctx, "marcus-social-growth-workflow", WatchScopeInput{
		ExplicitTargetURLs: []string{"https://x.com/example/status/12345"},
	})
	if err != nil {
		t.Fatalf("ReplaceWatchScope() error = %v", err)
	}

	seedMetadata := jobMetadata{
		WorkflowKey: "marcus-social-growth-workflow",
		WatchScope:  status.WatchScope,
		TargetStates: map[string]TargetState{
			"x_post:12345": {
				StableKey:                  "x_post:12345",
				LastObservationFingerprint: "same-observation",
				NextEligibleAt:             fixedNow.Add(30 * time.Minute).Format(time.RFC3339),
			},
		},
		AccountActions: "none",
	}
	seedJobMetadataPacket(t, ctx, store, status.Task.ID, seedMetadata)

	result, err := service.Wake(ctx, WakeParams{
		WorkflowKey: "marcus-social-growth-workflow",
		Trigger:     "manual",
		Reason:      "same-observation-proof",
		Observations: []Observation{{
			StableTargetKey:       "x_post:12345",
			Fingerprint:           "same-observation",
			RecommendedMemoryType: "social_draft",
			Summary:               "Draft the same reply recommendation.",
			CooldownUntil:         fixedNow.Add(30 * time.Minute),
		}},
	})
	if err != nil {
		t.Fatalf("Wake() error = %v", err)
	}
	if result.CreatedMemoryCount != 0 {
		t.Fatalf("CreatedMemoryCount = %d, want 0 inside cooldown", result.CreatedMemoryCount)
	}
	if !slices.Contains(result.SuppressedTargets, "x_post:12345") {
		t.Fatalf("SuppressedTargets = %v, want x_post:12345", result.SuppressedTargets)
	}

	summaries, err := store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_draft",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("social_draft summaries len = %d, want no duplicate pending item", len(summaries))
	}
}

func TestManualWakeAllowsMateriallyChangedObservation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSocialCopilotStore(t)
	defer store.Close()
	seedOdinCoreProject(t, ctx, store)

	fixedNow := time.Date(2026, 4, 24, 2, 0, 0, 0, time.UTC)
	service := Service{Store: store, Now: func() time.Time { return fixedNow }}
	status, err := service.ReplaceWatchScope(ctx, "marcus-social-growth-workflow", WatchScopeInput{
		ExplicitTargetURLs: []string{"https://x.com/example/status/12345"},
	})
	if err != nil {
		t.Fatalf("ReplaceWatchScope() error = %v", err)
	}

	seedJobMetadataPacket(t, ctx, store, status.Task.ID, jobMetadata{
		WorkflowKey: "marcus-social-growth-workflow",
		WatchScope:  status.WatchScope,
		TargetStates: map[string]TargetState{
			"x_post:12345": {
				StableKey:                  "x_post:12345",
				LastObservationFingerprint: "old-observation",
				NextEligibleAt:             fixedNow.Add(30 * time.Minute).Format(time.RFC3339),
			},
		},
		AccountActions: "none",
	})

	result, err := service.Wake(ctx, WakeParams{
		WorkflowKey: "marcus-social-growth-workflow",
		Trigger:     "manual",
		Reason:      "changed-observation-proof",
		Observations: []Observation{{
			StableTargetKey:       "x_post:12345",
			Fingerprint:           "new-observation",
			RecommendedMemoryType: "social_draft",
			Summary:               "Draft a new reply recommendation.",
			Fields:                map[string]string{"channel": "x", "content_kind": "reply"},
			CooldownUntil:         fixedNow.Add(30 * time.Minute),
		}},
	})
	if err != nil {
		t.Fatalf("Wake() error = %v", err)
	}
	if result.CreatedMemoryCount != 1 {
		t.Fatalf("CreatedMemoryCount = %d, want 1 for materially changed observation", result.CreatedMemoryCount)
	}

	summaries, err := store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_draft",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("social_draft summaries len = %d, want 1", len(summaries))
	}
	details := decodeMemoryDetails(t, summaries[0].DetailsJSON)
	for key, want := range map[string]string{
		"approval":                "pending",
		"watched_target_key":      "x_post:12345",
		"observation_fingerprint": "new-observation",
		"channel":                 "x",
		"content_kind":            "reply",
	} {
		if got := details.Fields[key]; got != want {
			t.Fatalf("details.Fields[%q] = %q, want %q; details=%+v", key, got, want, details.Fields)
		}
	}
}

func TestWakeRevalidatesPendingMemoryHintBeforeReuse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSocialCopilotStore(t)
	defer store.Close()
	seedOdinCoreProject(t, ctx, store)

	fixedNow := time.Date(2026, 4, 24, 2, 0, 0, 0, time.UTC)
	service := Service{Store: store, Now: func() time.Time { return fixedNow }}
	status, err := service.ReplaceWatchScope(ctx, "marcus-social-growth-workflow", WatchScopeInput{
		ExplicitTargetURLs: []string{"https://x.com/example/status/12345"},
	})
	if err != nil {
		t.Fatalf("ReplaceWatchScope() error = %v", err)
	}

	staleHint := recordWorkflowMemory(t, ctx, store, "social_draft", "Resolved old draft", map[string]string{
		"approval":                "approved",
		"watched_target_key":      "x_post:12345",
		"observation_fingerprint": "changed-observation",
	})
	usablePending := recordWorkflowMemory(t, ctx, store, "social_draft", "Pending draft to revise", map[string]string{
		"approval":                "pending",
		"watched_target_key":      "x_post:12345",
		"observation_fingerprint": "changed-observation",
	})
	seedJobMetadataPacket(t, ctx, store, status.Task.ID, jobMetadata{
		WorkflowKey: "marcus-social-growth-workflow",
		WatchScope:  status.WatchScope,
		TargetStates: map[string]TargetState{
			"x_post:12345": {
				StableKey:       "x_post:12345",
				PendingMemoryID: staleHint.ID,
			},
		},
		AccountActions: "none",
	})

	result, err := service.Wake(ctx, WakeParams{
		WorkflowKey: "marcus-social-growth-workflow",
		Trigger:     "manual",
		Reason:      "revalidate-hint-proof",
		Observations: []Observation{{
			StableTargetKey:       "x_post:12345",
			Fingerprint:           "changed-observation",
			RecommendedMemoryType: "social_draft",
			Summary:               "Revise pending draft instead of duplicating it.",
			CooldownUntil:         fixedNow.Add(30 * time.Minute),
		}},
	})
	if err != nil {
		t.Fatalf("Wake() error = %v", err)
	}
	if result.CreatedMemoryCount != 0 {
		t.Fatalf("CreatedMemoryCount = %d, want existing pending memory reused", result.CreatedMemoryCount)
	}
	state := result.TargetStates["x_post:12345"]
	if state.PendingMemoryID != usablePending.ID {
		t.Fatalf("PendingMemoryID = %d, want rediscovered pending memory %d", state.PendingMemoryID, usablePending.ID)
	}

	summaries, err := store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_draft",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("social_draft summaries len = %d, want no duplicate beyond seeded rows", len(summaries))
	}
}

func TestWakeDoesNotRecordPublishedSocialOutcome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSocialCopilotStore(t)
	defer store.Close()
	seedOdinCoreProject(t, ctx, store)

	service := Service{Store: store, Now: time.Now}
	if _, err := service.ReplaceWatchScope(ctx, "marcus-social-growth-workflow", WatchScopeInput{
		ExplicitTargetURLs: []string{"https://x.com/example/status/12345"},
	}); err != nil {
		t.Fatalf("ReplaceWatchScope() error = %v", err)
	}

	if _, err := service.Wake(ctx, WakeParams{
		WorkflowKey: "marcus-social-growth-workflow",
		Trigger:     "manual",
		Reason:      "no-publish-proof",
		Observations: []Observation{{
			StableTargetKey:       "x_post:12345",
			Fingerprint:           "recommendation",
			RecommendedMemoryType: "social_draft",
			Summary:               "Draft a reply recommendation only.",
		}},
	}); err != nil {
		t.Fatalf("Wake() error = %v", err)
	}

	outcomes, err := store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(social_outcome) error = %v", err)
	}
	if len(outcomes) != 0 {
		t.Fatalf("social_outcome summaries len = %d, want none from wake", len(outcomes))
	}
}

func openSocialCopilotStore(t *testing.T) *sqlite.Store {
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

func seedJobMetadataPacket(t *testing.T, ctx context.Context, store *sqlite.Store, taskID int64, metadata jobMetadata) {
	t.Helper()

	payload, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("json.Marshal(metadata) error = %v", err)
	}
	if _, err := store.CreateContextPacket(ctx, sqlite.CreateContextPacketParams{
		TaskID:        &taskID,
		PacketKind:    "social_copilot_job_metadata",
		PacketScope:   "workflow_job_metadata",
		Trigger:       "wake_checkpoint",
		CheckpointKey: "social-copilot/marcus-social-growth-workflow/social-copilot-loop",
		Status:        "active",
		Summary:       "seeded metadata",
		PayloadJSON:   string(payload),
	}); err != nil {
		t.Fatalf("CreateContextPacket(seed) error = %v", err)
	}
}

func recordWorkflowMemory(t *testing.T, ctx context.Context, store *sqlite.Store, memoryType string, summary string, fields map[string]string) sqlite.MemorySummary {
	t.Helper()

	details, err := json.Marshal(memoryDetails{
		Source:              "test",
		SelectedWorkflowKey: "marcus-social-growth-workflow",
		Scope:               "workflow",
		ScopeKey:            "marcus-social-growth-workflow",
		Fields:              fields,
	})
	if err != nil {
		t.Fatalf("json.Marshal(memory details) error = %v", err)
	}
	record, err := store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		Scope:       "workflow",
		ScopeKey:    "marcus-social-growth-workflow",
		MemoryType:  memoryType,
		Summary:     summary,
		DetailsJSON: string(details),
	})
	if err != nil {
		t.Fatalf("RecordMemorySummary() error = %v", err)
	}
	return record
}

func decodeMemoryDetails(t *testing.T, raw string) memoryDetails {
	t.Helper()

	var details memoryDetails
	if err := json.Unmarshal([]byte(raw), &details); err != nil {
		t.Fatalf("json.Unmarshal(memory details) error = %v", err)
	}
	return details
}

func seedOdinCoreProject(t *testing.T, ctx context.Context, store *sqlite.Store) sqlite.Project {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(odin-core) error = %v", err)
	}
	return project
}
