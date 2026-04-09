package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func TestContextPacketAppendOnlyAndLookup(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "context-packets.db")
	defer store.Close()

	project, task, run := seedContextPacketTask(t, ctx, store)

	first, err := store.CreateContextPacket(ctx, CreateContextPacketParams{
		TaskID:        &task.ID,
		RunID:         &run.ID,
		PacketKind:    "wake",
		PacketScope:   "task_wake_packet",
		Trigger:       "approval_wait",
		CheckpointKey: "approval-1",
		Status:        "active",
		Summary:       "waiting for approval",
		PayloadJSON:   `{"objective":"resume after approval"}`,
	})
	if err != nil {
		t.Fatalf("CreateContextPacket(first) error = %v", err)
	}

	second, err := store.CreateContextPacket(ctx, CreateContextPacketParams{
		TaskID:             &task.ID,
		RunID:              &run.ID,
		PacketKind:         "wake",
		PacketScope:        "task_wake_packet",
		Trigger:            "restart",
		CheckpointKey:      "restart-1",
		Status:             "active",
		SupersedesPacketID: &first.ID,
		Summary:            "resumed after restart",
		PayloadJSON:        `{"objective":"resume after restart"}`,
	})
	if err != nil {
		t.Fatalf("CreateContextPacket(second) error = %v", err)
	}

	gotFirst, err := store.GetContextPacket(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetContextPacket(first) error = %v", err)
	}
	if gotFirst.Trigger != "approval_wait" {
		t.Fatalf("GetContextPacket(first).Trigger = %q, want %q", gotFirst.Trigger, "approval_wait")
	}
	if gotFirst.SupersedesPacketID != nil {
		t.Fatalf("GetContextPacket(first).SupersedesPacketID = %v, want nil", gotFirst.SupersedesPacketID)
	}

	latest, err := store.GetLatestTaskWakePacket(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if latest.ID != second.ID {
		t.Fatalf("GetLatestTaskWakePacket().ID = %d, want %d", latest.ID, second.ID)
	}
	if latest.PacketScope != "task_wake_packet" {
		t.Fatalf("GetLatestTaskWakePacket().PacketScope = %q, want %q", latest.PacketScope, "task_wake_packet")
	}
	if latest.SupersedesPacketID == nil || *latest.SupersedesPacketID != first.ID {
		t.Fatalf("GetLatestTaskWakePacket().SupersedesPacketID = %v, want %d", latest.SupersedesPacketID, first.ID)
	}

	packets, err := store.ListContextPackets(ctx, ListContextPacketsParams{TaskID: &task.ID})
	if err != nil {
		t.Fatalf("ListContextPackets() error = %v", err)
	}
	if len(packets) != 2 {
		t.Fatalf("ListContextPackets() len = %d, want 2", len(packets))
	}
}

func TestMigrateExistingDatabaseAddsContextPacketEnvelope(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "migrate-context-packets.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		t.Fatalf("create schema_migrations error = %v", err)
	}

	initialMigration, err := loadMigrationByVersion(1)
	if err != nil {
		t.Fatalf("loadMigrationByVersion(1) error = %v", err)
	}
	if err := store.applyMigration(ctx, initialMigration); err != nil {
		t.Fatalf("applyMigration(0001) error = %v", err)
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
		t.Fatalf("Migrate() error = %v", err)
	}

	_, task, run := seedContextPacketTask(t, ctx, reopened)

	packet, err := reopened.CreateContextPacket(ctx, CreateContextPacketParams{
		TaskID:        &task.ID,
		RunID:         &run.ID,
		PacketKind:    "wake",
		PacketScope:   "task_wake_packet",
		Trigger:       "completion",
		CheckpointKey: "done-1",
		Status:        "sealed",
		Summary:       "run completed",
		PayloadJSON:   `{"objective":"done"}`,
	})
	if err != nil {
		t.Fatalf("CreateContextPacket() after migrate error = %v", err)
	}

	if packet.PacketScope != "task_wake_packet" {
		t.Fatalf("CreateContextPacket().PacketScope = %q, want %q", packet.PacketScope, "task_wake_packet")
	}
	if packet.Trigger != "completion" {
		t.Fatalf("CreateContextPacket().Trigger = %q, want %q", packet.Trigger, "completion")
	}
}

func openMigratedTestStore(t *testing.T, name string) *Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), name)
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}

	return store
}

func seedContextPacketTask(t *testing.T, ctx context.Context, store *Store) (Project, Task, Run) {
	t.Helper()

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "cfipros",
		Name:          "CFI Pros",
		Scope:         "project",
		GitRoot:       "/home/orchestrator/projects/cfipros",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "wake-packet",
		Title:       "Prepare wake packet",
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

	return project, task, run
}

func loadMigrationByVersion(version int) (migration, error) {
	migrations, err := loadMigrations()
	if err != nil {
		return migration{}, err
	}

	for _, migration := range migrations {
		if migration.Version == version {
			return migration, nil
		}
	}

	return migration{}, fmt.Errorf("migration version %d not found", version)
}
