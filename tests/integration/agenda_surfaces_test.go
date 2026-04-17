package integration_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/core/initiatives"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

func TestAgendaRootCommandE2E(t *testing.T) {
	t.Parallel()

	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)
	runtimeRoot := t.TempDir()
	now := time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()
	seedAgendaRuntimeState(t, context.Background(), store, now)

	output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, map[string]string{
		"ODIN_NOW": now.Format(time.RFC3339Nano),
	}, "", "agenda", "--json")
	if err != nil {
		t.Fatalf("runOdinCommand(agenda --json) error = %v\n%s", err, output)
	}

	var view projections.AgendaView
	if err := json.Unmarshal([]byte(output), &view); err != nil {
		t.Fatalf("json.Unmarshal(agenda output) error = %v\n%s", err, output)
	}
	if len(view.DueWork) != 2 {
		t.Fatalf("DueWork = %+v, want 2 entries", view.DueWork)
	}
	if view.DueWork[0].DueStatus != "overdue" || view.DueWork[1].DueStatus != "due" {
		t.Fatalf("DueWork = %+v, want overdue then due", view.DueWork)
	}
	if len(view.BlockedWork) < 2 {
		t.Fatalf("BlockedWork = %+v, want at least 2 entries", view.BlockedWork)
	}
	if len(view.Approvals) != 1 || view.Approvals[0].TaskKey != "odin-core-approval" {
		t.Fatalf("Approvals = %+v, want odin-core-approval", view.Approvals)
	}
}

func TestAgendaServeSurfaceE2E(t *testing.T) {
	t.Parallel()

	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)
	runtimeRoot := t.TempDir()
	now := time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()
	seedAgendaRuntimeState(t, context.Background(), store, now)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, odinBinary, "serve")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"ODIN_ROOT="+runtimeRoot,
		"ODIN_HTTP_ADDR=127.0.0.1:0",
		"ODIN_NOW="+now.Format(time.RFC3339Nano),
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe() error = %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("StderrPipe() error = %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() error = %v", err)
	}
	stopped := false
	defer func() {
		if stopped {
			return
		}
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
	}()

	addr := waitForServeAddress(t, stdout, stderr)
	response, err := http.Get("http://" + addr + "/agenda")
	if err != nil {
		t.Fatalf("GET /agenda error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("/agenda status = %d, want 200", response.StatusCode)
	}

	var view projections.AgendaView
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatalf("Decode(/agenda) error = %v", err)
	}
	if len(view.DueWork) != 2 {
		t.Fatalf("DueWork = %+v, want 2 entries", view.DueWork)
	}
	if view.DueWork[0].DueStatus != "overdue" || view.DueWork[1].DueStatus != "due" {
		t.Fatalf("DueWork = %+v, want overdue then due", view.DueWork)
	}
	if len(view.Approvals) != 1 || view.Approvals[0].TaskKey != "odin-core-approval" {
		t.Fatalf("Approvals = %+v, want odin-core-approval", view.Approvals)
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("Signal(os.Interrupt) error = %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("cmd.Wait() error = %v", err)
	}
	stopped = true
}

func seedAgendaRuntimeState(t *testing.T, ctx context.Context, store *sqlite.Store, now time.Time) {
	t.Helper()

	store.Now = func() time.Time { return now }
	workspace, err := workspaces.Service{Store: store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       filepath.Join(t.TempDir(), "odin-core"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(odin-core) error = %v", err)
	}
	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              "life-admin",
		Title:            "Life Admin",
		Kind:             string(initiatives.KindRoutine),
		Status:           "active",
		Summary:          "Agenda integration fixture",
		OwnerCompanionID: &companion.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative(life-admin) error = %v", err)
	}

	createAgendaIntegrationFollowUp(t, ctx, store, project.ID, workspace.ID, initiative.ID, companion.ID, "Review mail", now)
	createAgendaIntegrationFollowUp(t, ctx, store, project.ID, workspace.ID, initiative.ID, companion.ID, "File taxes", now.Add(-48*time.Hour))

	approvalTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "odin-core-approval",
		Title:        "Approval task",
		Status:       "blocked",
		Scope:        "odin-core",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "automation",
	})
	if err != nil {
		t.Fatalf("CreateTask(odin-core-approval) error = %v", err)
	}
	if _, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      approvalTask.ID,
		Status:      "pending",
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	wakeTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "odin-core-wake",
		Title:        "Wake task",
		Status:       "blocked",
		Scope:        "odin-core",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "follow_up",
	})
	if err != nil {
		t.Fatalf("CreateTask(odin-core-wake) error = %v", err)
	}
	if _, err := store.CreateContextPacket(ctx, sqlite.CreateContextPacketParams{
		TaskID:        &wakeTask.ID,
		PacketKind:    "wake",
		PacketScope:   "task_wake_packet",
		Trigger:       "follow_up_wait",
		CheckpointKey: "agenda-integration-wake",
		Status:        "active",
		Summary:       "waiting on follow-up context",
		PayloadJSON:   fmt.Sprintf(`{"task_id":%d,"task_key":"%s","scope":"odin-core","objective":"Resume wake work","status":"waiting","trigger":"follow_up_wait","blocking_reason":"waiting on supporting context"}`, wakeTask.ID, wakeTask.Key),
	}); err != nil {
		t.Fatalf("CreateContextPacket() error = %v", err)
	}
}

func createAgendaIntegrationFollowUp(t *testing.T, ctx context.Context, store *sqlite.Store, projectID, workspaceID, initiativeID, companionID int64, title string, nextDueAt time.Time) {
	t.Helper()

	if _, err := store.CreateFollowUpObligation(ctx, sqlite.CreateFollowUpObligationParams{
		WorkspaceID:     workspaceID,
		InitiativeID:    &initiativeID,
		CompanionID:     &companionID,
		TargetProjectID: projectID,
		Title:           title,
		Status:          "active",
		CadenceJSON:     `{"mode":"once"}`,
		NextDueAt:       nextDueAt,
		PolicyJSON:      `{}`,
	}); err != nil {
		t.Fatalf("CreateFollowUpObligation(%s) error = %v", title, err)
	}
}

func waitForServeAddress(t *testing.T, stdout io.ReadCloser, stderr io.ReadCloser) string {
	t.Helper()

	stderrBytes := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(stderr)
		stderrBytes <- string(data)
	}()

	scanner := bufio.NewScanner(stdout)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			errOutput := <-stderrBytes
			t.Fatalf("serve did not report address; stderr=%q", errOutput)
		default:
		}
		if !scanner.Scan() {
			errOutput := <-stderrBytes
			t.Fatalf("serve exited before reporting address; stderr=%q", errOutput)
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "serving on ") {
			return strings.TrimPrefix(line, "serving on ")
		}
	}
}
