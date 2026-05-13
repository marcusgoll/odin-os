package knowledge

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestServiceMergesProjectAndGlobalKnowledge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	project := createProject(t, ctx, store, "alpha")
	service := Service{Store: store}

	globalEntry, err := service.Record(ctx, Scope{Value: "global", Key: "global"}, "knowledge", "Global preference", `{"source":"test"}`, nil)
	if err != nil {
		t.Fatalf("Record(global) error = %v", err)
	}
	projectEntry, err := service.Record(ctx, Scope{ProjectID: &project.ID, Value: "project", Key: project.Key}, "knowledge", "Project convention", `{"source":"test"}`, nil)
	if err != nil {
		t.Fatalf("Record(project) error = %v", err)
	}

	globalEntries, err := service.List(ctx, Scope{Value: "global", Key: "global"}, "knowledge")
	if err != nil {
		t.Fatalf("List(global) error = %v", err)
	}
	if len(globalEntries) != 1 || globalEntries[0].ID != globalEntry.ID {
		t.Fatalf("global entries = %+v, want only global knowledge", globalEntries)
	}

	projectEntries, err := service.List(ctx, Scope{ProjectID: &project.ID, Value: "project", Key: project.Key}, "knowledge")
	if err != nil {
		t.Fatalf("List(project) error = %v", err)
	}
	if len(projectEntries) != 2 {
		t.Fatalf("project entries len = %d, want 2", len(projectEntries))
	}
	if projectEntries[0].ID != projectEntry.ID {
		t.Fatalf("project entries[0] = %+v, want project knowledge first", projectEntries[0])
	}
	if projectEntries[1].ID != globalEntry.ID {
		t.Fatalf("project entries[1] = %+v, want global knowledge fallback second", projectEntries[1])
	}
}

func TestRecordFromContextPackProposalRequiresAcceptedStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	project := createProject(t, ctx, store, "alpha")
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "goal-1",
		Title:       "Prepare memory proposal",
		ActionKey:   "memory.propose",
		Status:      "queued",
		Scope:       project.Scope,
		RequestedBy: "test",
		WorkKind:    "knowledge",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	packet, err := store.CreateContextPacket(ctx, sqlite.CreateContextPacketParams{
		TaskID:      &task.ID,
		PacketKind:  "context_pack",
		PacketScope: "operator_context_pack",
		Trigger:     "knowledge_context_pack_proposed",
		Status:      "review_required",
		Summary:     "Context pack for task goal-1",
		PayloadJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("CreateContextPacket() error = %v", err)
	}
	service := Service{Store: store}

	_, err = service.RecordFromContextPackProposal(ctx, ContextPackProposalMemoryParams{
		ProposalID: packet.ID,
		Status:     "review_required",
		ProjectID:  &project.ID,
		TaskID:     &task.ID,
		Scope:      Scope{ProjectID: &project.ID, Value: project.Scope, Key: project.Key},
		MemoryType: "context_pack",
		Summary:    "Context pack for task goal-1",
	})
	if err == nil || !strings.Contains(err.Error(), "requires accepted proposal") {
		t.Fatalf("RecordFromContextPackProposal(review_required) error = %v, want accepted proposal guard", err)
	}
	if _, err := store.ReviewContextPacket(ctx, sqlite.ReviewContextPacketParams{
		PacketID:   packet.ID,
		Status:     "active",
		Decision:   "accept",
		ReviewedBy: "operator",
	}); err != nil {
		t.Fatalf("ReviewContextPacket(accept) error = %v", err)
	}

	recorded, err := service.RecordFromContextPackProposal(ctx, ContextPackProposalMemoryParams{
		ProposalID: packet.ID,
		Status:     "active",
		ProjectID:  &project.ID,
		TaskID:     &task.ID,
		Scope:      Scope{ProjectID: &project.ID, Value: project.Scope, Key: project.Key},
		MemoryType: "context_pack",
		Summary:    "Context pack for task goal-1",
		DetailsJSON: `{
			"source": "test"
		}`,
	})
	if err != nil {
		t.Fatalf("RecordFromContextPackProposal(active) error = %v", err)
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(recorded.DetailsJSON), &details); err != nil {
		t.Fatalf("json.Unmarshal(details) error = %v\n%s", err, recorded.DetailsJSON)
	}
	if got := int64(details["source_context_pack_id"].(float64)); got != packet.ID {
		t.Fatalf("source_context_pack_id = %d, want %d", got, packet.ID)
	}

	_, err = service.RecordFromContextPackProposal(ctx, ContextPackProposalMemoryParams{
		ProposalID: packet.ID,
		Status:     "active",
		ProjectID:  &project.ID,
		TaskID:     &task.ID,
		Scope:      Scope{ProjectID: &project.ID, Value: project.Scope, Key: project.Key},
		MemoryType: "alternate_context_pack",
		Summary:    "Alternate context pack for task goal-1",
	})
	if err == nil || !strings.Contains(err.Error(), "type must be context_pack") {
		t.Fatalf("RecordFromContextPackProposal(alternate type) error = %v, want canonical type guard", err)
	}

	repeated, err := service.RecordFromContextPackProposal(ctx, ContextPackProposalMemoryParams{
		ProposalID: packet.ID,
		Status:     "active",
		ProjectID:  &project.ID,
		TaskID:     &task.ID,
		Scope:      Scope{ProjectID: &project.ID, Value: project.Scope, Key: project.Key},
		MemoryType: "context_pack",
		Summary:    "Context pack for task goal-1",
	})
	if err != nil {
		t.Fatalf("RecordFromContextPackProposal(repeat) error = %v", err)
	}
	if repeated.ID != recorded.ID {
		t.Fatalf("repeat ID = %d, want existing %d", repeated.ID, recorded.ID)
	}
}

func openTestStore(t *testing.T) *sqlite.Store {
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

func createProject(t *testing.T, ctx context.Context, store *sqlite.Store, key string) sqlite.Project {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           key,
		Name:          key,
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), key),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(%s) error = %v", key, err)
	}
	return project
}
