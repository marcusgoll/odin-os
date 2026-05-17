package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/app/bootstrap"
	"odin-os/internal/core/followups"
	"odin-os/internal/core/initiatives"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/store/sqlite"
)

func TestFollowUpLoopMaterializesDueObligationExactlyOnce(t *testing.T) {
	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: false
`)

	dueAt := time.Now().UTC().Add(-time.Hour)
	store, workspace, obligation := seedFollowUpServeFixture(t, root, dueAt)
	store.Close()
	if err := runServeOnceForFollowUpTest(t, root); err != nil && !strings.Contains(err.Error(), "listener exploded") {
		t.Fatalf("Run(serve) error = %v", err)
	}
	if err := runServeOnceForFollowUpTest(t, root); err != nil && !strings.Contains(err.Error(), "listener exploded") {
		t.Fatalf("Run(serve) second pass error = %v", err)
	}

	reloaded, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer reloaded.Close()

	var taskCount int
	if err := reloaded.DB().QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM tasks
		WHERE follow_up_obligation_id = ?
	`, obligation.ID).Scan(&taskCount); err != nil {
		t.Fatalf("COUNT(tasks) error = %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("task count = %d, want 1", taskCount)
	}

	task, err := reloaded.GetTaskByFollowUpOccurrence(context.Background(), obligation.ID, obligation.OccurrenceKey())
	if err != nil {
		t.Fatalf("GetTaskByFollowUpOccurrence() error = %v", err)
	}
	if task.Status != "blocked" {
		t.Fatalf("task.Status = %q, want blocked", task.Status)
	}
	if task.WorkspaceID == nil || *task.WorkspaceID != workspace.ID {
		t.Fatalf("task.WorkspaceID = %v, want %d", task.WorkspaceID, workspace.ID)
	}

	events, err := reloaded.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	foundMaterialized := false
	materializedCount := 0
	for _, event := range events {
		if string(event.Type) != "follow_up.materialized" {
			continue
		}
		materializedCount++
		foundMaterialized = true
		var payload struct {
			TaskStatus string `json:"task_status"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("json.Unmarshal(materialized event) error = %v", err)
		}
		if payload.TaskStatus != "blocked" {
			t.Fatalf("follow_up.materialized task_status = %q, want blocked", payload.TaskStatus)
		}
	}
	if !foundMaterialized {
		t.Fatal("expected follow_up.materialized event")
	}
	if materializedCount != 1 {
		t.Fatalf("follow_up.materialized event count = %d, want 1", materializedCount)
	}
}

func TestFollowUpLoopPausesLinkedObligationsForArchivedInitiative(t *testing.T) {
	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: false
`)

	store, _, obligation := seedFollowUpServeFixture(t, root, time.Now().UTC().Add(-time.Hour))
	workspace, err := store.GetWorkspaceByKey(context.Background(), workspaces.DefaultWorkspaceKey)
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	if _, err := (initiatives.Service{Store: store}).ArchiveInitiative(context.Background(), workspace.ID, "life-admin"); err != nil {
		t.Fatalf("ArchiveInitiative() error = %v", err)
	}
	store.Close()

	if err := runServeOnceForFollowUpTest(t, root); err != nil && !strings.Contains(err.Error(), "listener exploded") {
		t.Fatalf("Run(serve) error = %v", err)
	}

	reloaded, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer reloaded.Close()

	paused, err := reloaded.GetFollowUpObligation(context.Background(), obligation.ID)
	if err != nil {
		t.Fatalf("GetFollowUpObligation() error = %v", err)
	}
	if paused.Status != string(followups.StatusPaused) {
		t.Fatalf("paused.Status = %q, want paused", paused.Status)
	}

	events, err := reloaded.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	foundPaused := false
	for _, event := range events {
		if string(event.Type) != "follow_up.paused" {
			continue
		}
		foundPaused = true
		var payload struct {
			InitiativeStatus string `json:"initiative_status"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("json.Unmarshal(paused event) error = %v", err)
		}
		if payload.InitiativeStatus != "archived" {
			t.Fatalf("follow_up.paused initiative_status = %q, want archived", payload.InitiativeStatus)
		}
	}
	if !foundPaused {
		t.Fatal("expected follow_up.paused event")
	}

	var taskCount int
	if err := reloaded.DB().QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM tasks
		WHERE follow_up_obligation_id = ?
	`, obligation.ID).Scan(&taskCount); err != nil {
		t.Fatalf("COUNT(tasks) error = %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("task count = %d, want 0", taskCount)
	}
}

func TestFollowUpLoopBlockedObligationsSurfaceWithoutDispatchingSideEffects(t *testing.T) {
	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: false
`)

	store, _, obligation := seedFollowUpServeFixture(t, root, time.Now().UTC().Add(-time.Hour))
	store.Close()

	if err := runServeOnceForFollowUpTest(t, root); err != nil && !strings.Contains(err.Error(), "listener exploded") {
		t.Fatalf("Run(serve) error = %v", err)
	}

	reloaded, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer reloaded.Close()

	task, err := reloaded.GetTaskByFollowUpOccurrence(context.Background(), obligation.ID, obligation.OccurrenceKey())
	if err != nil {
		t.Fatalf("GetTaskByFollowUpOccurrence() error = %v", err)
	}
	if task.Status != "blocked" {
		t.Fatalf("task.Status = %q, want blocked", task.Status)
	}
	if task.CurrentRunID != nil {
		t.Fatalf("task.CurrentRunID = %v, want nil", task.CurrentRunID)
	}

	var runCount int
	if err := reloaded.DB().QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM runs
		WHERE task_id = ?
	`, task.ID).Scan(&runCount); err != nil {
		t.Fatalf("COUNT(runs) error = %v", err)
	}
	if runCount != 0 {
		t.Fatalf("run count = %d, want 0", runCount)
	}
}

func seedFollowUpServeFixture(t *testing.T, root string, dueAt time.Time) (*sqlite.Store, sqlite.Workspace, followups.FollowUpObligation) {
	t.Helper()

	app, err := bootstrap.Load(context.Background(), root, root)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}

	workspace, err := app.Store.GetWorkspaceByKey(context.Background(), workspaces.DefaultWorkspaceKey)
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := app.Store.GetCompanionByKey(context.Background(), workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	project, err := app.Store.GetProjectByKey(context.Background(), "odin-core")
	if err != nil {
		project, err = app.Store.CreateProject(context.Background(), sqlite.CreateProjectParams{
			Key:           "odin-core",
			Name:          "Odin Core",
			Scope:         "odin-core",
			GitRoot:       filepath.Join(root, "repos", "odin-core"),
			DefaultBranch: "main",
			ManifestPath:  "config/projects.yaml",
		})
		if err != nil {
			t.Fatalf("CreateProject(odin-core) error = %v", err)
		}
	}
	initiative, err := app.Store.UpsertInitiative(context.Background(), sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              "life-admin",
		Title:            "Life Admin",
		Kind:             "routine",
		Status:           "active",
		OwnerCompanionID: &companion.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	obligation, err := (followups.Service{Store: app.Store}).Create(context.Background(), followups.CreateParams{
		WorkspaceID:     workspace.ID,
		InitiativeID:    &initiative.ID,
		CompanionID:     &companion.ID,
		TargetProjectID: &project.ID,
		Title:           "Review mail",
		Cadence:         followups.Cadence{Mode: followups.CadenceModeOnce},
		NextDueAt:       dueAt,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	return app.Store, workspace, obligation
}

func runServeOnceForFollowUpTest(t *testing.T, root string) error {
	t.Helper()

	originalListen := serveListen
	serveListen = func(string, string) (net.Listener, error) {
		return followUpTestListener{root: root}, nil
	}
	defer func() {
		serveListen = originalListen
	}()

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"serve"}, strings.NewReader(""), &stdout)
	return err
}

type followUpTestListener struct {
	root string
}

func (listener followUpTestListener) Accept() (net.Conn, error) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		store, err := sqlite.Open(filepath.Join(listener.root, "data", "odin.db"))
		if err == nil {
			var eventCount int
			err = store.DB().QueryRowContext(context.Background(), `
				SELECT COUNT(*)
				FROM events
				WHERE event_type IN ('follow_up.materialized', 'follow_up.paused')
			`).Scan(&eventCount)
			_ = store.Close()
			if err == nil && eventCount > 0 {
				return nil, errors.New("listener exploded")
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, errors.New("listener exploded: timed out waiting for follow-up startup event")
}

func (followUpTestListener) Close() error   { return nil }
func (followUpTestListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4zero, Port: 0} }
