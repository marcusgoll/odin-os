package repl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/adapters/web"
	"odin-os/internal/cli/scope"
	"odin-os/internal/core/initiatives"
	"odin-os/internal/core/projects"
	corescope "odin-os/internal/core/scope"
	"odin-os/internal/core/workspaces"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tools/invocation"
)

func TestShellRestoresValidSessionOnStartup(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	if err := env.SessionStore.Save(Cache{
		ProjectKey: "alpha",
		Mode:       ModeAct,
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if shell.state.Mode != ModeAct {
		t.Fatalf("Mode = %q, want %q", shell.state.Mode, ModeAct)
	}
	if shell.state.Scope.Kind != scope.ScopeProject || shell.state.Scope.ProjectKey != "alpha" {
		t.Fatalf("Scope = %+v, want project alpha", shell.state.Scope)
	}
}

func TestShellDowngradesInvalidSessionOnStartup(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	if err := env.SessionStore.Save(Cache{
		ProjectKey: "missing",
		Mode:       ModeAct,
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if shell.state.Mode != ModeAsk {
		t.Fatalf("Mode = %q, want %q", shell.state.Mode, ModeAsk)
	}
	if shell.state.Scope.Kind != scope.ScopeGlobal {
		t.Fatalf("Scope = %+v, want global", shell.state.Scope)
	}
}

func TestAskModeHandlesFreeTextWithoutCreatingTask(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "what scope am i in?", &output); err != nil {
		t.Fatalf("HandleLine() error = %v", err)
	}

	if !strings.Contains(output.String(), "global") {
		t.Fatalf("HandleLine() output = %q, want scope answer", output.String())
	}

	views, err := shell.jobs.List(context.Background(), shell.controlScope())
	if err != nil {
		t.Fatalf("jobs.List() error = %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("jobs len = %d, want 0", len(views))
	}
}

func TestShellControlScopeTracksProjectSelection(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}

	got := shell.controlScope()
	want := corescope.ControlScope{
		SubjectType:   corescope.SubjectTypeInitiative,
		SubjectKey:    "alpha",
		WorkspaceKey:  "default",
		InitiativeKey: "alpha",
		ProjectKey:    "alpha",
		CompanionKey:  "primary",
	}

	if got != want {
		t.Fatalf("controlScope() = %+v, want %+v", got, want)
	}
}

func TestShellControlScopeTracksNewProjectFlow(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/scope new-project", &output); err != nil {
		t.Fatalf("HandleLine(/scope new-project) error = %v", err)
	}

	got := shell.controlScope()
	want := corescope.ControlScope{
		SubjectType:   corescope.SubjectTypeNewProject,
		SubjectKey:    "odin-core",
		WorkspaceKey:  "default",
		InitiativeKey: "odin-core",
		ProjectKey:    "odin-core",
		CompanionKey:  "primary",
	}

	if got != want {
		t.Fatalf("controlScope() = %+v, want %+v", got, want)
	}
}

func TestActModeCreatesTaskInProjectScope(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "/mode act", &output); err != nil {
		t.Fatalf("HandleLine(/mode) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "Implement the shell", &output); err != nil {
		t.Fatalf("HandleLine(act input) error = %v", err)
	}

	views, err := shell.jobs.List(context.Background(), shell.controlScope())
	if err != nil {
		t.Fatalf("jobs.List() error = %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("jobs len = %d, want 1", len(views))
	}
	if !strings.Contains(output.String(), "created task") {
		t.Fatalf("output = %q, want creation message", output.String())
	}

	workspace, err := workspaces.Service{Store: env.Store}.BootstrapDefaultWorkspace(context.Background())
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}

	initiative, err := env.Store.GetInitiativeByKey(context.Background(), workspace.ID, "alpha")
	if err != nil {
		t.Fatalf("GetInitiativeByKey(alpha) error = %v", err)
	}
	if initiative.Kind != string(initiatives.KindManagedProject) {
		t.Fatalf("initiative.Kind = %q, want %q", initiative.Kind, initiatives.KindManagedProject)
	}
	if initiative.LinkedProjectID == nil {
		t.Fatalf("initiative.LinkedProjectID = nil, want project id")
	}
}

func TestActModeRejectedInGlobalScope(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/mode act", &output); err != nil {
		t.Fatalf("HandleLine() error = %v", err)
	}

	if shell.state.Mode != ModeAsk {
		t.Fatalf("Mode = %q, want ask", shell.state.Mode)
	}
	if !strings.Contains(output.String(), "global scope") {
		t.Fatalf("output = %q, want global-scope rejection", output.String())
	}
}

func TestDoctorCommandRendersStructuredTextOutput(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/doctor", &output); err != nil {
		t.Fatalf("HandleLine(/doctor) error = %v", err)
	}

	for _, want := range []string{"status=", "database=", "registry=", "executor=", "queue=", "projections=", "sources="} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want substring %q", output.String(), want)
		}
	}
}

func TestDoctorCommandSupportsJSONOutput(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	if _, err := env.Store.RecordExecutorHealth(context.Background(), sqlite.RecordExecutorHealthParams{
		Executor:    "codex",
		Status:      "healthy",
		LatencyMS:   42,
		DetailsJSON: `{"mode":"local"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}
	if _, err := env.Store.RecordRegistryVersion(context.Background(), sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "abc123",
		Notes:       "fresh compile",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}
	if _, err := env.Store.RecordProjectionFreshness(context.Background(), sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "healthy",
		DetailsJSON: `{"source":"runtime"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/doctor json", &output); err != nil {
		t.Fatalf("HandleLine(/doctor json) error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded["status"] == nil {
		t.Fatalf("decoded status missing: %#v", decoded)
	}
}

func TestShellApprovalsResolveApproveStartsContinuationRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	fixture := seedPendingApprovalFixture(t, ctx, env)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	command := fmt.Sprintf("/approvals resolve %d approve because final confirmation", fixture.Approval.ID)
	if err := shell.HandleLine(ctx, command, &output); err != nil {
		t.Fatalf("HandleLine(%q) error = %v", command, err)
	}

	receipt := output.String()
	for _, want := range []string{
		fmt.Sprintf("approval=%d", fixture.Approval.ID),
		"status=resolved",
		"result=approved",
		"run=",
		"summary=approval granted; submit continuation started",
	} {
		if !strings.Contains(receipt, want) {
			t.Fatalf("receipt = %q, want substring %q", receipt, want)
		}
	}
	if strings.Contains(receipt, "final confirmation") {
		t.Fatalf("receipt = %q, want compact output without echoed reason", receipt)
	}
	if strings.Contains(receipt, "next=") {
		t.Fatalf("receipt = %q, want no next hint on approval resolution", receipt)
	}

	approval, err := env.Store.GetApproval(ctx, fixture.Approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if approval.Status != "approved" {
		t.Fatalf("approval.Status = %q, want %q", approval.Status, "approved")
	}
	if approval.DecisionBy != "operator" {
		t.Fatalf("approval.DecisionBy = %q, want %q", approval.DecisionBy, "operator")
	}
	if approval.Reason != "final confirmation" {
		t.Fatalf("approval.Reason = %q, want %q", approval.Reason, "final confirmation")
	}

	runIDs := listShellTaskRunIDs(t, ctx, env.Store, fixture.Task.ID)
	if len(runIDs) != 2 {
		t.Fatalf("task run count = %d, want 2 after approve continuation", len(runIDs))
	}
	if runIDs[1] == fixture.PrepareRun.ID {
		t.Fatalf("continuation run reused prepare run id %d", runIDs[1])
	}
	if !strings.Contains(receipt, fmt.Sprintf("run=%d", runIDs[1])) {
		t.Fatalf("receipt = %q, want continuation run id %d", receipt, runIDs[1])
	}
}

func TestShellApprovalsResolveDenyKeepsReceiptCompact(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	fixture := seedPendingApprovalFixture(t, ctx, env)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	command := fmt.Sprintf("/approvals resolve %d deny because amount is wrong", fixture.Approval.ID)
	if err := shell.HandleLine(ctx, command, &output); err != nil {
		t.Fatalf("HandleLine(%q) error = %v", command, err)
	}

	receipt := output.String()
	for _, want := range []string{
		fmt.Sprintf("approval=%d", fixture.Approval.ID),
		"status=resolved",
		"result=denied",
		"summary=approval denied; later retry requires fresh prepare",
	} {
		if !strings.Contains(receipt, want) {
			t.Fatalf("receipt = %q, want substring %q", receipt, want)
		}
	}
	if strings.Contains(receipt, "run=") {
		t.Fatalf("receipt = %q, want no run handle on deny", receipt)
	}
	if strings.Contains(receipt, "amount is wrong") {
		t.Fatalf("receipt = %q, want compact output without echoed reason", receipt)
	}
	if strings.Contains(receipt, "next=") {
		t.Fatalf("receipt = %q, want no next hint on approval resolution", receipt)
	}

	approval, err := env.Store.GetApproval(ctx, fixture.Approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if approval.Status != "denied" {
		t.Fatalf("approval.Status = %q, want %q", approval.Status, "denied")
	}
	if approval.DecisionBy != "operator" {
		t.Fatalf("approval.DecisionBy = %q, want %q", approval.DecisionBy, "operator")
	}
	if approval.Reason != "amount is wrong" {
		t.Fatalf("approval.Reason = %q, want %q", approval.Reason, "amount is wrong")
	}

	runIDs := listShellTaskRunIDs(t, ctx, env.Store, fixture.Task.ID)
	if len(runIDs) != 1 {
		t.Fatalf("task run count = %d, want 1 after deny", len(runIDs))
	}
	if runIDs[0] != fixture.PrepareRun.ID {
		t.Fatalf("remaining run id = %d, want prepare run id %d", runIDs[0], fixture.PrepareRun.ID)
	}

	task, err := env.Store.GetTask(ctx, fixture.Task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.TerminalReason != "operator_denied" {
		t.Fatalf("task.TerminalReason = %q, want %q", task.TerminalReason, "operator_denied")
	}
}

func TestShellTransferPrepareRequiresSelectedInitiative(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/transfer prepare direction=deposit amount_usd=25.00 source_account=checking destination_account=brokerage", &output); err != nil {
		t.Fatalf("HandleLine(/transfer prepare) error = %v", err)
	}

	if !strings.Contains(output.String(), "select an initiative first") {
		t.Fatalf("output = %q, want initiative-selection requirement", output.String())
	}
}

func TestShellTransferPreparePrintsReceiptAndCreatesApprovalWait(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	fixed := time.Date(2026, 4, 22, 3, 4, 5, 0, time.UTC)
	shell.transfers.Now = func() time.Time { return fixed }

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project family-ops", &output); err != nil {
		t.Fatalf("HandleLine(/project family-ops) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/transfer prepare direction=deposit amount_usd=25.00 source_account=checking destination_account=brokerage memo=household-test", &output); err != nil {
		t.Fatalf("HandleLine(/transfer prepare) error = %v", err)
	}

	receipt := output.String()
	for _, want := range []string{
		"task=robinhood-transfer-20260422-030405",
		"run=1",
		"approval=1",
		"summary=review prepared and awaiting approval",
		"next=/runs show 1",
		"/approvals resolve 1 <approve|deny> because <reason...>",
		"/runs show <submit-run-id from resolve output>",
	} {
		if !strings.Contains(receipt, want) {
			t.Fatalf("receipt = %q, want substring %q", receipt, want)
		}
	}
	for _, unwanted := range []string{"review_url=", "screenshot_path=", "approval_id="} {
		if strings.Contains(receipt, unwanted) {
			t.Fatalf("receipt = %q, want no %q", receipt, unwanted)
		}
	}

	approval, err := env.Store.GetApproval(ctx, 1)
	if err != nil {
		t.Fatalf("GetApproval(1) error = %v", err)
	}
	if approval.Status != "pending" {
		t.Fatalf("approval.Status = %q, want %q", approval.Status, "pending")
	}

	project, err := env.Store.GetProjectByKey(ctx, "family-ops")
	if err != nil {
		t.Fatalf("GetProjectByKey(family-ops) error = %v", err)
	}
	wakePacket, err := env.Store.GetLatestTaskWakePacket(ctx, project.ID, approval.TaskID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if wakePacket.Trigger != "approval_wait" {
		t.Fatalf("wakePacket.Trigger = %q, want %q", wakePacket.Trigger, "approval_wait")
	}
}

func TestShellTransferDenySealsApprovalWaitAndMarksOperatorDenied(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	fixed := time.Date(2026, 4, 22, 3, 4, 5, 0, time.UTC)
	shell.transfers.Now = func() time.Time { return fixed }

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project family-ops", &output); err != nil {
		t.Fatalf("HandleLine(/project family-ops) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/transfer prepare direction=deposit amount_usd=25.00 source_account=checking destination_account=brokerage memo=household-test", &output); err != nil {
		t.Fatalf("HandleLine(/transfer prepare) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/approvals resolve 1 deny because amount is wrong", &output); err != nil {
		t.Fatalf("HandleLine(/approvals resolve deny) error = %v", err)
	}

	receipt := output.String()
	for _, want := range []string{
		"approval=1",
		"status=resolved",
		"result=denied",
		"summary=approval denied; later retry requires fresh prepare",
	} {
		if !strings.Contains(receipt, want) {
			t.Fatalf("receipt = %q, want substring %q", receipt, want)
		}
	}
	if strings.Contains(receipt, "run=") {
		t.Fatalf("receipt = %q, want no run handle on deny", receipt)
	}

	task, err := env.Store.GetTask(ctx, 1)
	if err != nil {
		t.Fatalf("GetTask(1) error = %v", err)
	}
	if task.Status != "blocked" {
		t.Fatalf("task.Status = %q, want %q", task.Status, "blocked")
	}
	if task.TerminalReason != "operator_denied" {
		t.Fatalf("task.TerminalReason = %q, want %q", task.TerminalReason, "operator_denied")
	}

	project, err := env.Store.GetProjectByKey(ctx, "family-ops")
	if err != nil {
		t.Fatalf("GetProjectByKey(family-ops) error = %v", err)
	}
	activePackets, err := env.Store.ListContextPackets(ctx, sqlite.ListContextPacketsParams{
		TaskID:      &task.ID,
		PacketScope: "task_wake_packet",
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("ListContextPackets(active) error = %v", err)
	}
	if len(activePackets) != 0 {
		t.Fatalf("active wake packets = %d, want 0", len(activePackets))
	}

	wakePacket, err := env.Store.GetLatestTaskWakePacket(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if wakePacket.Status != "sealed" {
		t.Fatalf("wakePacket.Status = %q, want %q", wakePacket.Status, "sealed")
	}
	if !strings.Contains(wakePacket.PayloadJSON, `"blocking_reason":"operator_denied"`) {
		t.Fatalf("wakePacket.PayloadJSON = %q, want blocking_reason operator_denied", wakePacket.PayloadJSON)
	}
}

func TestShellRunsShowActiveDisplaysPreparedTransferSummary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	fixed := time.Date(2026, 4, 22, 3, 4, 5, 0, time.UTC)
	shell.transfers.Now = func() time.Time { return fixed }

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project family-ops", &output); err != nil {
		t.Fatalf("HandleLine(/project family-ops) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/transfer prepare direction=deposit amount_usd=25.00 source_account=checking destination_account=brokerage", &output); err != nil {
		t.Fatalf("HandleLine(/transfer prepare) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/runs show active", &output); err != nil {
		t.Fatalf("HandleLine(/runs show active) error = %v", err)
	}

	details := output.String()
	for _, want := range []string{
		"run=1",
		"task=robinhood-transfer-20260422-030405",
		"status=completed",
		"executor=robinhood_transfer_prepare",
		"summary=review prepared and awaiting approval",
	} {
		if !strings.Contains(details, want) {
			t.Fatalf("details = %q, want substring %q", details, want)
		}
	}
}

func TestShellTransferApproveShowsSubmitContinuationRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	fixed := time.Date(2026, 4, 22, 3, 4, 5, 0, time.UTC)
	shell.transfers.Now = func() time.Time { return fixed }

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project family-ops", &output); err != nil {
		t.Fatalf("HandleLine(/project family-ops) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/transfer prepare direction=deposit amount_usd=25.00 source_account=checking destination_account=brokerage", &output); err != nil {
		t.Fatalf("HandleLine(/transfer prepare) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/approvals resolve 1 approve because final confirmation", &output); err != nil {
		t.Fatalf("HandleLine(/approvals resolve) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/runs show active", &output); err != nil {
		t.Fatalf("HandleLine(/runs show active) error = %v", err)
	}

	details := output.String()
	for _, want := range []string{
		"run=2",
		"task=robinhood-transfer-20260422-030405",
		"status=completed",
		"executor=robinhood_transfer_submit",
	} {
		if !strings.Contains(details, want) {
			t.Fatalf("details = %q, want substring %q", details, want)
		}
	}
}

func TestShellRunsShowIncludesRunArtifacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	project, err := env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}

	task, err := env.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "artifact-task",
		Title:       "Artifact task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := env.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	if _, err := env.Store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
		RunID:        run.ID,
		ArtifactType: "driver_result",
		Summary:      "Robinhood review ready",
		DetailsJSON:  `{"session_state":"review_ready"}`,
	}); err != nil {
		t.Fatalf("RecordRunArtifact() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project alpha) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, fmt.Sprintf("/runs show %d", run.ID), &output); err != nil {
		t.Fatalf("HandleLine(/runs show) error = %v", err)
	}

	details := output.String()
	for _, want := range []string{
		"run=1",
		"task=artifact-task",
		"status=running",
		"executor=codex",
		"artifact=driver_result",
		"summary=Robinhood review ready",
		`details={"session_state":"review_ready"}`,
	} {
		if !strings.Contains(details, want) {
			t.Fatalf("details = %q, want substring %q", details, want)
		}
	}
}

func TestShellHelpIncludesTransitionCommands(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/help", &output); err != nil {
		t.Fatalf("HandleLine(/help) error = %v", err)
	}

	for _, want := range []string{"/workspace", "/initiatives", "/companions", "/transition", "/observe", "/compare"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("help output = %q, want %q", output.String(), want)
		}
	}
}

func TestShellScopeShowsCurrentControlScopeDetails(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "/scope", &output); err != nil {
		t.Fatalf("HandleLine(/scope) error = %v", err)
	}

	for _, want := range []string{"scope=alpha", "workspace=default", "initiative=alpha", "project=alpha", "companion=primary"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("scope output = %q, want %q", output.String(), want)
		}
	}
}

func TestShellOperatorViewsRenderWorkspaceInitiativesAndCompanions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	project, err := env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
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
	workspace, err := workspaces.Service{Store: env.Store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}
	companion, err := env.Store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	if _, err := env.Store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              project.Key,
		Title:            project.Name,
		Kind:             string(initiatives.KindManagedProject),
		Status:           "active",
		Summary:          "Alpha initiative",
		OwnerCompanionID: &companion.ID,
		LinkedProjectID:  &project.ID,
	}); err != nil {
		t.Fatalf("UpsertInitiative(alpha) error = %v", err)
	}
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/workspace", &output); err != nil {
		t.Fatalf("HandleLine(/workspace) error = %v", err)
	}
	for _, want := range []string{"workspace=default", "initiatives=1", "companions=1"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("workspace output = %q, want %q", output.String(), want)
		}
	}

	output.Reset()
	if err := shell.HandleLine(ctx, "/initiatives", &output); err != nil {
		t.Fatalf("HandleLine(/initiatives) error = %v", err)
	}
	for _, want := range []string{"alpha", "managed_project", "owner=primary", "project=alpha"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("initiatives output = %q, want %q", output.String(), want)
		}
	}

	output.Reset()
	if err := shell.HandleLine(ctx, "/companions", &output); err != nil {
		t.Fatalf("HandleLine(/companions) error = %v", err)
	}
	for _, want := range []string{"primary", "assistant", "owned_initiatives=1"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("companions output = %q, want %q", output.String(), want)
		}
	}
}

func TestShellTransitionStatusShowsDefaultInventoryAuthority(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "/transition", &output); err != nil {
		t.Fatalf("HandleLine(/transition) error = %v", err)
	}

	for _, want := range []string{
		"project=alpha",
		"state=inventory",
		"controller=legacy_odin",
		"mutation_authority=legacy_odin",
		"odin_can_mutate=false",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("transition output = %q, want %q", output.String(), want)
		}
	}
}

func TestShellTransitionSetShadowRecordsEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/transition set shadow because observe only", &output); err != nil {
		t.Fatalf("HandleLine(/transition set shadow) error = %v", err)
	}

	project, err := env.Store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	transition, err := env.Store.GetProjectTransition(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetProjectTransition() error = %v", err)
	}
	if transition.State != string(projects.TransitionStateShadow) {
		t.Fatalf("transition.State = %q, want %q", transition.State, projects.TransitionStateShadow)
	}

	events, err := env.Store.ListEvents(ctx, sqlite.ListEventsParams{ProjectID: &project.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if !hasTransitionEvent(events, runtimeevents.EventProjectTransitionChanged) {
		t.Fatalf("events missing project.transition_changed: %+v", events)
	}
}

func TestShellTransitionSetCutoverRequiresConfirm(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "/transition set cutover because take ownership", &output); err != nil {
		t.Fatalf("HandleLine(/transition set cutover) error = %v", err)
	}

	if !strings.Contains(output.String(), "confirm") {
		t.Fatalf("output = %q, want confirm requirement", output.String())
	}
}

func TestShellTransitionSetLimitedActionRequiresAllowlist(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "/transition set limited_action confirm because pilot", &output); err != nil {
		t.Fatalf("HandleLine(/transition set limited_action) error = %v", err)
	}

	if !strings.Contains(output.String(), "allow=") {
		t.Fatalf("output = %q, want allowlist requirement", output.String())
	}
}

func TestShellObserveRecordsShadowObservation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/transition set shadow because observe only", &output); err != nil {
		t.Fatalf("HandleLine(/transition set shadow) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/observe legacy deploy observed", &output); err != nil {
		t.Fatalf("HandleLine(/observe) error = %v", err)
	}

	project, err := env.Store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	reports, err := env.Store.ListProjectTransitionReports(ctx, project.ID)
	if err != nil {
		t.Fatalf("ListProjectTransitionReports() error = %v", err)
	}
	if len(reports) != 1 || reports[0].ReportType != "shadow_observation" {
		t.Fatalf("reports = %+v, want one shadow_observation", reports)
	}

	events, err := env.Store.ListEvents(ctx, sqlite.ListEventsParams{ProjectID: &project.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if !hasTransitionEvent(events, runtimeevents.EventProjectShadowObservationRecorded) {
		t.Fatalf("events missing project.shadow_observation_recorded: %+v", events)
	}
}

func TestShellCompareRecordsCompareReport(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/transition set compare because compare live decisions", &output); err != nil {
		t.Fatalf("HandleLine(/transition set compare) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/compare route mismatch on candidate", &output); err != nil {
		t.Fatalf("HandleLine(/compare) error = %v", err)
	}

	project, err := env.Store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	reports, err := env.Store.ListProjectTransitionReports(ctx, project.ID)
	if err != nil {
		t.Fatalf("ListProjectTransitionReports() error = %v", err)
	}
	if len(reports) != 1 || reports[0].ReportType != "compare_report" {
		t.Fatalf("reports = %+v, want one compare_report", reports)
	}

	events, err := env.Store.ListEvents(ctx, sqlite.ListEventsParams{ProjectID: &project.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if !hasTransitionEvent(events, runtimeevents.EventProjectCompareReportRecorded) {
		t.Fatalf("events missing project.compare_report_recorded: %+v", events)
	}
}

func TestShellTransitionRejectedInGlobalScope(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/transition", &output); err != nil {
		t.Fatalf("HandleLine(/transition) error = %v", err)
	}

	if !strings.Contains(output.String(), "project scope") {
		t.Fatalf("output = %q, want project-scope rejection", output.String())
	}
}

func newTestEnvironment(t *testing.T) Environment {
	t.Helper()

	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	stateDir := filepath.Join(root, "state", "cache")
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	registry := writeRegistry(t, map[string]string{
		"odin-core":  "system_project",
		"alpha":      "github_backed_project",
		"family-ops": "local_git_project",
	})

	store, err := sqlite.Open(filepath.Join(dataDir, "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	return Environment{
		Store:              store,
		Registry:           registry,
		SessionStore:       SessionStore{Path: filepath.Join(stateDir, "cli-session.json")},
		TransferInvocation: testTransferInvocation(),
	}
}

func testTransferInvocation() invocation.Service {
	return invocation.Service{
		RobinhoodTransferDriver: web.RobinhoodTransferDriver{
			InvokeFunc: func(_ context.Context, request web.RobinhoodTransferRequest) (web.RobinhoodTransferResponse, error) {
				if request.Input.Mode == "submit" {
					return web.RobinhoodTransferResponse{
						ToolKey: web.RobinhoodTransferToolKey,
						Summary: "Robinhood transfer submitted",
						Artifacts: map[string]any{
							"session_state": "submitted",
							"current_url":   "https://robinhood.com/transfers",
							"next_action":   "verify transfer status",
						},
						RawOutput: `{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood transfer submitted","artifacts":{"session_state":"submitted","current_url":"https://robinhood.com/transfers","next_action":"verify transfer status"}}`,
					}, nil
				}
				return web.RobinhoodTransferResponse{
					ToolKey: web.RobinhoodTransferToolKey,
					Summary: "Robinhood transfer review ready",
					Artifacts: map[string]any{
						"session_state": "review_ready",
						"current_url":   "https://robinhood.com/transfer",
						"next_action":   "request approval",
					},
					RawOutput: `{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood transfer review ready","artifacts":{"session_state":"review_ready","current_url":"https://robinhood.com/transfer","next_action":"request approval"}}`,
				}, nil
			},
		},
	}
}

func hasTransitionEvent(events []runtimeevents.Record, want runtimeevents.Type) bool {
	for _, event := range events {
		if event.Type == want {
			return true
		}
	}
	return false
}

type shellApprovalFixture struct {
	Task       sqlite.Task
	PrepareRun sqlite.Run
	Approval   sqlite.Approval
}

func seedPendingApprovalFixture(t *testing.T, ctx context.Context, env Environment) shellApprovalFixture {
	t.Helper()

	project, err := env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := env.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "finance-transfer-review",
		Title:       "Prepare Robinhood transfer review",
		Status:      "blocked",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	run, err := env.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "blocked",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	approval, err := env.Store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	return shellApprovalFixture{
		Task:       task,
		PrepareRun: run,
		Approval:   approval,
	}
}

func listShellTaskRunIDs(t *testing.T, ctx context.Context, store *sqlite.Store, taskID int64) []int64 {
	t.Helper()

	rows, err := store.DB().QueryContext(ctx, `SELECT id FROM runs WHERE task_id = ? ORDER BY id ASC`, taskID)
	if err != nil {
		t.Fatalf("QueryContext(runs) error = %v", err)
	}
	defer rows.Close()

	var runIDs []int64
	for rows.Next() {
		var runID int64
		if err := rows.Scan(&runID); err != nil {
			t.Fatalf("rows.Scan() error = %v", err)
		}
		runIDs = append(runIDs, runID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() error = %v", err)
	}

	return runIDs
}
