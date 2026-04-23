package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/runtime/projections"
)

func TestConversationTranscriptsRecordAndListByScope(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	globalProject, project, task, run := seedMemoryFixture(t, ctx, store)

	globalTranscript, err := store.RecordConversationTranscript(ctx, RecordConversationTranscriptParams{
		Scope:       "global",
		ScopeKey:    "global",
		Mode:        "ask",
		Prompt:      "hello there",
		Response:    "hi",
		ToolSummary: `{"tools":[]}`,
	})
	if err != nil {
		t.Fatalf("RecordConversationTranscript(global) error = %v", err)
	}
	if globalTranscript.ProjectID != nil {
		t.Fatalf("global transcript ProjectID = %v, want nil", *globalTranscript.ProjectID)
	}

	projectTranscript, err := store.RecordConversationTranscript(ctx, RecordConversationTranscriptParams{
		ProjectID:   &project.ID,
		TaskID:      &task.ID,
		RunID:       &run.ID,
		Scope:       "project",
		ScopeKey:    project.Key,
		Mode:        "act",
		Prompt:      "implement memory",
		Response:    "completed",
		ToolSummary: `{"executor":"codex_headless"}`,
	})
	if err != nil {
		t.Fatalf("RecordConversationTranscript(project) error = %v", err)
	}
	if projectTranscript.ProjectID == nil || *projectTranscript.ProjectID != project.ID {
		t.Fatalf("project transcript ProjectID = %v, want %d", projectTranscript.ProjectID, project.ID)
	}

	globalOnly, err := store.ListConversationTranscripts(ctx, ListConversationTranscriptsParams{
		Scope:    "global",
		ScopeKey: "global",
	})
	if err != nil {
		t.Fatalf("ListConversationTranscripts(global) error = %v", err)
	}
	if len(globalOnly) != 1 || globalOnly[0].ID != globalTranscript.ID {
		t.Fatalf("global transcripts = %+v, want only global transcript", globalOnly)
	}

	projectOnly, err := store.ListConversationTranscripts(ctx, ListConversationTranscriptsParams{
		ProjectID: &project.ID,
		Scope:     "project",
		ScopeKey:  project.Key,
	})
	if err != nil {
		t.Fatalf("ListConversationTranscripts(project) error = %v", err)
	}
	if len(projectOnly) != 1 || projectOnly[0].ID != projectTranscript.ID {
		t.Fatalf("project transcripts = %+v, want only project transcript", projectOnly)
	}

	coreOnly, err := store.ListConversationTranscripts(ctx, ListConversationTranscriptsParams{
		ProjectID: &globalProject.ID,
		Scope:     "odin-core",
		ScopeKey:  globalProject.Key,
	})
	if err != nil {
		t.Fatalf("ListConversationTranscripts(odin-core) error = %v", err)
	}
	if len(coreOnly) != 0 {
		t.Fatalf("odin-core transcripts = %+v, want none", coreOnly)
	}
}

func TestMemorySummariesRecordSeparatelyFromTranscripts(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	_, project, _, _ := seedMemoryFixture(t, ctx, store)

	transcript, err := store.RecordConversationTranscript(ctx, RecordConversationTranscriptParams{
		ProjectID:   &project.ID,
		Scope:       "project",
		ScopeKey:    project.Key,
		Mode:        "ask",
		Prompt:      "remember this convention",
		Response:    "noted",
		ToolSummary: `{"tools":[]}`,
	})
	if err != nil {
		t.Fatalf("RecordConversationTranscript() error = %v", err)
	}

	summary, err := store.RecordMemorySummary(ctx, RecordMemorySummaryParams{
		ProjectID:          &project.ID,
		SourceTranscriptID: &transcript.ID,
		Scope:              "project",
		ScopeKey:           project.Key,
		MemoryType:         "project_summary",
		Summary:            "Alpha uses worktree isolation for mutating tasks.",
		DetailsJSON:        `{"source":"compaction"}`,
	})
	if err != nil {
		t.Fatalf("RecordMemorySummary() error = %v", err)
	}
	if summary.SourceTranscriptID == nil || *summary.SourceTranscriptID != transcript.ID {
		t.Fatalf("summary.SourceTranscriptID = %v, want %d", summary.SourceTranscriptID, transcript.ID)
	}

	transcripts, err := store.ListConversationTranscripts(ctx, ListConversationTranscriptsParams{
		ProjectID: &project.ID,
		Scope:     "project",
		ScopeKey:  project.Key,
	})
	if err != nil {
		t.Fatalf("ListConversationTranscripts() error = %v", err)
	}
	if len(transcripts) != 1 {
		t.Fatalf("transcripts len = %d, want 1", len(transcripts))
	}

	summaries, err := store.ListMemorySummaries(ctx, ListMemorySummariesParams{
		ProjectID: &project.ID,
		Scope:     "project",
		ScopeKey:  project.Key,
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != summary.ID {
		t.Fatalf("summaries = %+v, want only recorded summary", summaries)
	}
}

func TestUpdateMemorySummaryDetails(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	_, project, _, _ := seedMemoryFixture(t, ctx, store)

	summary, err := store.RecordMemorySummary(ctx, RecordMemorySummaryParams{
		ProjectID:   &project.ID,
		Scope:       "project",
		ScopeKey:    project.Key,
		MemoryType:  "social_draft",
		Summary:     "Draft awaiting approval",
		DetailsJSON: `{"source":"cli","scope":"project","scope_key":"alpha","fields":{"approval":"pending","channel":"x","content_kind":"post"}}`,
	})
	if err != nil {
		t.Fatalf("RecordMemorySummary() error = %v", err)
	}

	updated, err := store.UpdateMemorySummaryDetails(ctx, UpdateMemorySummaryDetailsParams{
		MemoryID:    summary.ID,
		DetailsJSON: `{"source":"cli","scope":"project","scope_key":"alpha","fields":{"approval":"approved","channel":"x","content_kind":"post"}}`,
	})
	if err != nil {
		t.Fatalf("UpdateMemorySummaryDetails() error = %v", err)
	}
	if updated.ID != summary.ID {
		t.Fatalf("updated.ID = %d, want %d", updated.ID, summary.ID)
	}
	if updated.DetailsJSON != `{"source":"cli","scope":"project","scope_key":"alpha","fields":{"approval":"approved","channel":"x","content_kind":"post"}}` {
		t.Fatalf("updated.DetailsJSON = %q, want updated details", updated.DetailsJSON)
	}
	if !updated.UpdatedAt.After(updated.CreatedAt) && !updated.UpdatedAt.Equal(updated.CreatedAt) {
		t.Fatalf("updated timestamps invalid: created=%s updated=%s", updated.CreatedAt, updated.UpdatedAt)
	}

	summaries, err := store.ListMemorySummaries(ctx, ListMemorySummariesParams{
		ProjectID: &project.ID,
		Scope:     "project",
		ScopeKey:  project.Key,
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries len = %d, want 1", len(summaries))
	}
	if summaries[0].DetailsJSON != updated.DetailsJSON {
		t.Fatalf("stored details = %q, want %q", summaries[0].DetailsJSON, updated.DetailsJSON)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var updatedEventFound bool
	for _, event := range events {
		if event.Type != runtimeevents.EventMemorySummaryUpdated {
			continue
		}
		if event.StreamType != runtimeevents.StreamMemorySummary {
			t.Fatalf("memory update event stream type = %q, want %q", event.StreamType, runtimeevents.StreamMemorySummary)
		}
		var payload runtimeevents.MemorySummaryUpdatedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("json.Unmarshal(memory update payload) error = %v", err)
		}
		if payload.Scope != "project" || payload.ScopeKey != project.Key || payload.MemoryType != "social_draft" {
			t.Fatalf("memory update payload = %+v, want project social_draft payload", payload)
		}
		updatedEventFound = true
	}
	if !updatedEventFound {
		t.Fatal("memory summary updated event not found")
	}
}

func TestConversationAndMemoryWritesEmitEvents(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	_, project, _, _ := seedMemoryFixture(t, ctx, store)

	globalTranscript, err := store.RecordConversationTranscript(ctx, RecordConversationTranscriptParams{
		Scope:       "global",
		ScopeKey:    "global",
		Mode:        "ask",
		Prompt:      "remember preference",
		Response:    "stored",
		ToolSummary: `{"tools":[]}`,
	})
	if err != nil {
		t.Fatalf("RecordConversationTranscript(global) error = %v", err)
	}

	transcript, err := store.RecordConversationTranscript(ctx, RecordConversationTranscriptParams{
		ProjectID:   &project.ID,
		Scope:       "project",
		ScopeKey:    project.Key,
		Mode:        "ask",
		Prompt:      "hello",
		Response:    "hi",
		ToolSummary: `{"tools":[]}`,
	})
	if err != nil {
		t.Fatalf("RecordConversationTranscript() error = %v", err)
	}
	if _, err := store.RecordMemorySummary(ctx, RecordMemorySummaryParams{
		ProjectID:          &project.ID,
		SourceTranscriptID: &transcript.ID,
		Scope:              "project",
		ScopeKey:           project.Key,
		MemoryType:         "project_summary",
		Summary:            "Alpha conversations can be compacted into memory.",
		DetailsJSON:        `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordMemorySummary() error = %v", err)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	var globalTranscriptFound bool
	var projectTranscriptFound bool
	var summaryEvents int
	for _, event := range events {
		switch event.Type {
		case runtimeevents.EventConversationTranscriptRecorded:
			if event.StreamType != runtimeevents.StreamConversation {
				t.Fatalf("conversation event stream type = %q, want %q", event.StreamType, runtimeevents.StreamConversation)
			}
			var payload runtimeevents.ConversationTranscriptRecordedPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatalf("json.Unmarshal(conversation payload) error = %v", err)
			}
			switch {
			case event.StreamID == globalTranscript.ID:
				if payload.Scope != "global" || payload.ScopeKey != "global" || payload.Mode != "ask" {
					t.Fatalf("global transcript payload = %+v, want global ask payload", payload)
				}
				globalTranscriptFound = true
			case event.StreamID == transcript.ID:
				if payload.Scope != "project" || payload.ScopeKey != project.Key || payload.Mode != "ask" {
					t.Fatalf("project transcript payload = %+v, want project ask payload", payload)
				}
				projectTranscriptFound = true
			}
		case runtimeevents.EventMemorySummaryRecorded:
			summaryEvents++
			if event.StreamType != runtimeevents.StreamMemorySummary {
				t.Fatalf("memory summary event stream type = %q, want %q", event.StreamType, runtimeevents.StreamMemorySummary)
			}
			var payload runtimeevents.MemorySummaryRecordedPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatalf("json.Unmarshal(memory payload) error = %v", err)
			}
			if payload.Scope != "project" || payload.ScopeKey != project.Key || payload.MemoryType != "project_summary" {
				t.Fatalf("memory summary payload = %+v, want project summary payload", payload)
			}
			if payload.SourceTranscriptID == nil || *payload.SourceTranscriptID != transcript.ID {
				t.Fatalf("payload.SourceTranscriptID = %v, want %d", payload.SourceTranscriptID, transcript.ID)
			}
		}
	}
	if !globalTranscriptFound {
		t.Fatalf("global transcript event not found")
	}
	if !projectTranscriptFound {
		t.Fatalf("project transcript event not found")
	}
	if summaryEvents != 1 {
		t.Fatalf("memory summary event count = %d, want 1", summaryEvents)
	}
}

func TestFinishRunIfRunningPreservesCancelledRun(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "Alpha task",
		Status:      "running",
		Scope:       "project",
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
	run, err = store.FinishRun(ctx, FinishRunParams{
		RunID:   run.ID,
		Status:  "cancelled",
		Summary: "cancelled by operator",
	})
	if err != nil {
		t.Fatalf("FinishRun(cancelled) error = %v", err)
	}

	preserved, finished, err := store.FinishRunIfRunning(ctx, FinishRunParams{
		RunID:   run.ID,
		Status:  "failed",
		Summary: "executor failed after cancellation",
	})
	if err != nil {
		t.Fatalf("FinishRunIfRunning() error = %v", err)
	}
	if finished {
		t.Fatal("FinishRunIfRunning() finished = true, want false for preserved cancelled run")
	}
	if preserved.Status != "cancelled" {
		t.Fatalf("preserved.Status = %q, want cancelled", preserved.Status)
	}
	if preserved.Summary != "cancelled by operator" {
		t.Fatalf("preserved.Summary = %q, want original cancellation summary", preserved.Summary)
	}
}

func TestConversationTranscriptRejectsMismatchedLineage(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	_, project, task, _ := seedMemoryFixture(t, ctx, store)

	otherProject, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "beta",
		Name:          "Beta",
		Scope:         "project",
		GitRoot:       "/tmp/beta",
		DefaultBranch: "main",
		GitHubRepo:    "acme/beta",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(beta) error = %v", err)
	}
	otherTask, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   otherProject.ID,
		Key:         "beta-memory",
		Title:       "beta memory fixture",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask(beta) error = %v", err)
	}
	otherRun, err := store.StartRun(ctx, StartRunParams{
		TaskID:   otherTask.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(beta) error = %v", err)
	}

	for _, tc := range []struct {
		name   string
		params RecordConversationTranscriptParams
	}{
		{
			name: "task belongs to different project",
			params: RecordConversationTranscriptParams{
				ProjectID: &project.ID,
				TaskID:    &otherTask.ID,
				Scope:     "project",
				ScopeKey:  project.Key,
				Mode:      "act",
				Prompt:    "bad lineage",
				Response:  "bad lineage",
			},
		},
		{
			name: "run belongs to different task",
			params: RecordConversationTranscriptParams{
				ProjectID: &project.ID,
				TaskID:    &task.ID,
				RunID:     &otherRun.ID,
				Scope:     "project",
				ScopeKey:  project.Key,
				Mode:      "act",
				Prompt:    "bad lineage",
				Response:  "bad lineage",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.RecordConversationTranscript(ctx, tc.params); err == nil {
				t.Fatalf("RecordConversationTranscript() error = nil, want lineage validation failure")
			}
		})
	}
}

func TestMemorySummaryRejectsMismatchedSourceLineage(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	_, project, task, run := seedMemoryFixture(t, ctx, store)

	transcript, err := store.RecordConversationTranscript(ctx, RecordConversationTranscriptParams{
		ProjectID:   &project.ID,
		TaskID:      &task.ID,
		RunID:       &run.ID,
		Scope:       "project",
		ScopeKey:    project.Key,
		Mode:        "act",
		Prompt:      "persist episode",
		Response:    "completed",
		ToolSummary: `{"executor":"codex_headless"}`,
		Executor:    "codex_headless",
	})
	if err != nil {
		t.Fatalf("RecordConversationTranscript() error = %v", err)
	}

	otherProject, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "beta",
		Name:          "Beta",
		Scope:         "project",
		GitRoot:       "/tmp/beta",
		DefaultBranch: "main",
		GitHubRepo:    "acme/beta",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(beta) error = %v", err)
	}
	otherTask, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   otherProject.ID,
		Key:         "beta-memory",
		Title:       "beta memory fixture",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask(beta) error = %v", err)
	}
	otherRun, err := store.StartRun(ctx, StartRunParams{
		TaskID:   otherTask.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(beta) error = %v", err)
	}

	for _, tc := range []struct {
		name   string
		params RecordMemorySummaryParams
	}{
		{
			name: "project-scoped source requires matching project",
			params: RecordMemorySummaryParams{
				SourceTranscriptID: &transcript.ID,
				Scope:              "project",
				ScopeKey:           project.Key,
				MemoryType:         "episode",
				Summary:            "mismatch",
				DetailsJSON:        `{"source":"test"}`,
			},
		},
		{
			name: "source transcript project mismatch",
			params: RecordMemorySummaryParams{
				ProjectID:          &otherProject.ID,
				SourceTranscriptID: &transcript.ID,
				TaskID:             &otherTask.ID,
				Scope:              "project",
				ScopeKey:           otherProject.Key,
				MemoryType:         "episode",
				Summary:            "mismatch",
				DetailsJSON:        `{"source":"test"}`,
			},
		},
		{
			name: "source transcript run mismatch",
			params: RecordMemorySummaryParams{
				ProjectID:          &project.ID,
				SourceTranscriptID: &transcript.ID,
				TaskID:             &task.ID,
				RunID:              &otherRun.ID,
				Scope:              "project",
				ScopeKey:           project.Key,
				MemoryType:         "episode",
				Summary:            "mismatch",
				DetailsJSON:        `{"source":"test"}`,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.RecordMemorySummary(ctx, tc.params); err == nil {
				t.Fatalf("RecordMemorySummary() error = nil, want lineage validation failure")
			}
		})
	}
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

func openTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func seedMemoryFixture(t *testing.T, ctx context.Context, store *Store) (Project, Project, Task, Run) {
	t.Helper()

	coreProject, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		GitHubRepo:    "example/odin-os",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(odin-core) error = %v", err)
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-memory",
		Title:       "memory fixture",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := store.StartRun(ctx, StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	return coreProject, project, task, run
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
