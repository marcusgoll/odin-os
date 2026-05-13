package knowledge

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestContextPackProposalPersistsMemoryOnlyAfterAcceptReview(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openMigratedStore(t)
	defer store.Close()
	task := createKnowledgeTask(t, ctx, store, "odin-core", "goal-1")
	service := Service{Store: store}

	proposal, err := service.ProposeContextPack(ctx, ContextPackParams{
		TaskRef:    task.Key,
		ProjectKey: "odin-core",
		Limit:      3,
	})
	if err != nil {
		t.Fatalf("ProposeContextPack() error = %v", err)
	}
	if proposal.Packet.Status != "review_required" {
		t.Fatalf("proposal status = %q, want review_required", proposal.Packet.Status)
	}
	assertMemorySummaryCount(t, ctx, store, 0)

	rejected, err := service.ReviewContextPackProposal(ctx, proposal.Packet.ID, ContextPackReviewRejectDecision)
	if err != nil {
		t.Fatalf("ReviewContextPackProposal(reject) error = %v", err)
	}
	if rejected.Status != "rejected" {
		t.Fatalf("rejected status = %q, want rejected", rejected.Status)
	}
	assertMemorySummaryCount(t, ctx, store, 0)

	acceptedProposal, err := service.ProposeContextPack(ctx, ContextPackParams{
		TaskRef:    task.Key,
		ProjectKey: "odin-core",
		Limit:      3,
	})
	if err != nil {
		t.Fatalf("ProposeContextPack(accepted) error = %v", err)
	}
	accepted, err := service.ReviewContextPackProposal(ctx, acceptedProposal.Packet.ID, ContextPackReviewAcceptDecision)
	if err != nil {
		t.Fatalf("ReviewContextPackProposal(accept) error = %v", err)
	}
	if accepted.Status != "active" {
		t.Fatalf("accepted status = %q, want active", accepted.Status)
	}

	summaries := assertMemorySummaryCount(t, ctx, store, 1)
	summary := summaries[0]
	if summary.MemoryType != ContextPackPacketKind || summary.TaskID == nil || *summary.TaskID != task.ID {
		t.Fatalf("memory summary = %+v, want context pack task memory", summary)
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(summary.DetailsJSON), &details); err != nil {
		t.Fatalf("json.Unmarshal(memory details) error = %v\n%s", err, summary.DetailsJSON)
	}
	if got := int64(details["source_context_pack_id"].(float64)); got != acceptedProposal.Packet.ID {
		t.Fatalf("source_context_pack_id = %d, want %d", got, acceptedProposal.Packet.ID)
	}

	repeated, err := service.ReviewContextPackProposal(ctx, acceptedProposal.Packet.ID, ContextPackReviewAcceptDecision)
	if err != nil {
		t.Fatalf("ReviewContextPackProposal(repeat accept) error = %v", err)
	}
	if !repeated.Repeated {
		t.Fatalf("repeat accept repeated = false, want true")
	}
	assertMemorySummaryCount(t, ctx, store, 1)
}

func assertMemorySummaryCount(t *testing.T, ctx context.Context, store *sqlite.Store, want int) []sqlite.MemorySummary {
	t.Helper()

	summaries, err := store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != want {
		t.Fatalf("memory summaries len = %d, want %d: %#v", len(summaries), want, summaries)
	}
	return summaries
}

func openMigratedStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func createKnowledgeTask(t *testing.T, ctx context.Context, store *sqlite.Store, projectKey string, taskKey string) sqlite.Task {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           projectKey,
		Name:          projectKey,
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), projectKey),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         taskKey,
		Title:       "Prepare governed memory proposal",
		ActionKey:   "memory.propose",
		Status:      "queued",
		Scope:       project.Scope,
		RequestedBy: "test",
		WorkKind:    "knowledge",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return task
}
