package repl

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
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

	views, err := shell.jobs.List(context.Background(), shell.state.Scope)
	if err != nil {
		t.Fatalf("jobs.List() error = %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("jobs len = %d, want 0", len(views))
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

	views, err := shell.jobs.List(context.Background(), scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	})
	if err != nil {
		t.Fatalf("jobs.List() error = %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("jobs len = %d, want 1", len(views))
	}
	if !strings.Contains(output.String(), "created task") {
		t.Fatalf("output = %q, want creation message", output.String())
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

	for _, want := range []string{"/transition", "/observe", "/compare", "/workflows", "/tradeboard"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("help output = %q, want %q", output.String(), want)
		}
	}
}

func TestShellWorkflowsListsRegistryWorkflows(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	env.RegistrySnapshot = registry.Snapshot{
		ByKind: map[registry.Kind][]registry.Item{
			registry.KindWorkflow: {
				{
					Key:        "flica-tradeboard",
					Title:      "Marcus FLICA TradeBoard Workflow",
					Summary:    "Operator-invoked TradeBoard workflow.",
					Status:     "active",
					Entrypoint: "command:/tradeboard",
				},
			},
		},
	}
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/workflows", &output); err != nil {
		t.Fatalf("HandleLine(/workflows) error = %v", err)
	}

	for _, want := range []string{"flica-tradeboard", "status=active", "entrypoint=command:/tradeboard"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("workflows output = %q, want %q", output.String(), want)
		}
	}
}

func TestShellWorkflowsShowsRegistryWorkflow(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	env.RegistrySnapshot = registry.Snapshot{
		ByKind: map[registry.Kind][]registry.Item{
			registry.KindWorkflow: {
				{
					Key:        "flica-schedule",
					Title:      "Marcus FLICA Schedule Workflow",
					Summary:    "Schedule workflow.",
					Status:     "active",
					Entrypoint: "command:/tradeboard sync-status",
					Composes:   []string{"command:/tradeboard sync-status", "pbs-flight-api"},
					Sections: map[string]string{
						registry.SectionPurpose: "Produce or validate the canonical Schedule Snapshot.\n\nKeep this compact.",
					},
					Source: registry.SourceInfo{RelativePath: "workflows/flica-schedule.md"},
				},
			},
		},
	}
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/workflows flica-schedule", &output); err != nil {
		t.Fatalf("HandleLine(/workflows flica-schedule) error = %v", err)
	}

	for _, want := range []string{
		"key=flica-schedule",
		"title=Marcus FLICA Schedule Workflow",
		"entrypoint=command:/tradeboard sync-status",
		"source=workflows/flica-schedule.md",
		"purpose=Produce or validate the canonical Schedule Snapshot. Keep this compact.",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("workflow detail output = %q, want %q", output.String(), want)
		}
	}
}

func TestShellApprovalsShowsActionBinding(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	project, err := env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "action-approvals",
		Name:          "Action Approvals",
		Scope:         "project",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		GitHubRepo:    "example/action-approvals",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := env.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "approve-flica-action",
		Title:       "Approve FLICA action",
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
	action, payload, err := env.Store.CreateActionWithPayload(ctx, sqlite.CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        run.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          "sha256:pending",
		PayloadJSON:          `{"pairing":"W7084C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload() error = %v", err)
	}
	if _, err := env.Store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		ActionID:    &action.ID,
		PayloadHash: payload.PayloadHash,
		Status:      "pending",
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/approvals", &output); err != nil {
		t.Fatalf("HandleLine(/approvals) error = %v", err)
	}

	for _, want := range []string{"approve-flica-action pending", "action_id=", "payload_hash=sha256:pending"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("approvals output = %q, want %q", output.String(), want)
		}
	}
}

func TestShellActionsListsRecentActions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	action, payload, _, _ := seedShellAction(t, ctx, env, "sha256:test")

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/actions", &output); err != nil {
		t.Fatalf("HandleLine(/actions) error = %v", err)
	}

	for _, want := range []string{
		"action_id=" + strconv.FormatInt(action.ID, 10),
		"workflow=flica-tradeboard",
		"type=tradeboard_action",
		"lifecycle=prepared",
		"payload_hash=" + payload.PayloadHash,
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("actions output = %q, want %q", output.String(), want)
		}
	}
}

func TestShellActionsShowsActionDetail(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	action, payload, approval, run := seedShellAction(t, ctx, env, "sha256:detail")

	if _, err := env.Store.AppendActionEvidence(ctx, sqlite.AppendActionEvidenceParams{
		ActionID:     action.ID,
		EventType:    "action.prepared",
		EventVersion: 1,
		PayloadHash:  payload.PayloadHash,
		ApprovalID:   &approval.ID,
		RunID:        &run.ID,
		Source:       "test",
		EvidenceJSON: `{"status":"prepared"}`,
	}); err != nil {
		t.Fatalf("AppendActionEvidence() error = %v", err)
	}

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/actions "+strconv.FormatInt(action.ID, 10), &output); err != nil {
		t.Fatalf("HandleLine(/actions id) error = %v", err)
	}

	for _, want := range []string{
		"action_id=" + strconv.FormatInt(action.ID, 10),
		"workflow=flica-tradeboard",
		"workflow_run_id=" + strconv.FormatInt(run.ID, 10),
		"payload_hash=" + payload.PayloadHash,
		"payload_schema=flica.tradeboard_action.v1",
		"submit_path=command:/tradeboard post",
		"readback_path=huginn:flica-my-requests",
		"proof_requirement=external_readback",
		`payload_json={"pairing":"W7084C"}`,
		"approval_id=" + strconv.FormatInt(approval.ID, 10),
		"status=pending",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("action detail output = %q, want %q", output.String(), want)
		}
	}
}

func TestShellActionsShowsEvidenceTimeline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	action, payload, approval, run := seedShellAction(t, ctx, env, "sha256:evidence")

	for _, eventType := range []string{"action.prepared", "action.submitted"} {
		if _, err := env.Store.AppendActionEvidence(ctx, sqlite.AppendActionEvidenceParams{
			ActionID:     action.ID,
			EventType:    eventType,
			EventVersion: 1,
			PayloadHash:  payload.PayloadHash,
			ApprovalID:   &approval.ID,
			RunID:        &run.ID,
			Source:       "test",
			EvidenceJSON: `{"status":"recorded"}`,
		}); err != nil {
			t.Fatalf("AppendActionEvidence(%s) error = %v", eventType, err)
		}
	}

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/actions "+strconv.FormatInt(action.ID, 10)+" evidence", &output); err != nil {
		t.Fatalf("HandleLine(/actions id evidence) error = %v", err)
	}

	first := strings.Index(output.String(), "type=action.prepared")
	second := strings.Index(output.String(), "type=action.submitted")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("evidence output = %q, want ordered prepared before submitted", output.String())
	}
	for _, want := range []string{
		"payload_hash=" + payload.PayloadHash,
		"approval_id=" + strconv.FormatInt(approval.ID, 10),
		"run_id=" + strconv.FormatInt(run.ID, 10),
		"source=test",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("evidence output = %q, want %q", output.String(), want)
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

func TestShellTradeboardUsageShowsCommandHelp(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/tradeboard", &output); err != nil {
		t.Fatalf("HandleLine(/tradeboard) error = %v", err)
	}
	if !strings.Contains(output.String(), "tradeboard") {
		t.Fatalf("output = %q, want tradeboard usage", output.String())
	}
}

func TestShellTradeboardPickupRequiresConfirm(t *testing.T) {
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/tradeboard pickup W123 bcid=123.456", &output); err != nil {
		t.Fatalf("HandleLine(/tradeboard pickup) error = %v", err)
	}
	if !strings.Contains(output.String(), "requires confirm") {
		t.Fatalf("output = %q, want confirm requirement", output.String())
	}
}

func TestShellTradeboardSyncStatusShowsFlicaSyncState(t *testing.T) {
	skipIfLoopbackListenUnavailable(t)

	env := newTestEnvironment(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ops/flica/status" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "unexpected path"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"last_sync":          "2026-04-28T00:00:00Z",
			"last_sync_status":   "success",
			"flica_sync_running": false,
		})
	}))
	t.Cleanup(server.Close)

	t.Setenv("ODIN_TRADEBOARD_API_BASE_URL", server.URL)

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/tradeboard sync-status", &output); err != nil {
		t.Fatalf("HandleLine(/tradeboard sync-status) error = %v", err)
	}
	if !strings.Contains(output.String(), "last_sync=2026-04-28T00:00:00Z") {
		t.Fatalf("output = %q, want sync status output", output.String())
	}
}

func TestShellTradeboardPickupRequiresSuccessfulFlicaSync(t *testing.T) {
	skipIfLoopbackListenUnavailable(t)

	env := newTestEnvironment(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ops/flica/status" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "unexpected path"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"last_sync":          "2026-04-20T00:00:00Z",
			"last_sync_status":   "warn: stale",
			"flica_sync_running": false,
		})
	}))
	t.Cleanup(server.Close)
	t.Setenv("ODIN_TRADEBOARD_API_BASE_URL", server.URL)

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/tradeboard pickup W123 bcid=123.456 confirm", &output); err == nil {
		t.Fatalf("HandleLine(/tradeboard pickup) expected preflight failure")
	}
	if !strings.Contains(output.String(), "flica sync preflight failed") {
		t.Fatalf("output = %q, want preflight failure", output.String())
	}
}

func TestTradeboardHTTPTimeoutDefaultsForBrowserLatency(t *testing.T) {
	t.Setenv("ODIN_TRADEBOARD_API_TIMEOUT_SECONDS", "")

	if got := tradeboardHTTPTimeout(); got != 180*time.Second {
		t.Fatalf("tradeboardHTTPTimeout() = %v, want 180s", got)
	}

	t.Setenv("ODIN_TRADEBOARD_API_TIMEOUT_SECONDS", "2")
	if got := tradeboardHTTPTimeout(); got != 5*time.Second {
		t.Fatalf("tradeboardHTTPTimeout() = %v, want 5s minimum", got)
	}

	t.Setenv("ODIN_TRADEBOARD_API_TIMEOUT_SECONDS", "999")
	if got := tradeboardHTTPTimeout(); got != 600*time.Second {
		t.Fatalf("tradeboardHTTPTimeout() = %v, want 600s maximum", got)
	}
}

func skipIfLoopbackListenUnavailable(t *testing.T) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("loopback listener unavailable in this environment: %v", err)
		}
		t.Fatalf("loopback listener preflight failed: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close loopback listener: %v", err)
	}
}

func seedShellAction(t *testing.T, ctx context.Context, env Environment, payloadHash string) (sqlite.Action, sqlite.ActionPayload, sqlite.Approval, sqlite.Run) {
	t.Helper()

	project, err := env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "shell-actions",
		Name:          "Shell Actions",
		Scope:         "project",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		GitHubRepo:    "example/shell-actions",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := env.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "inspect-flica-action",
		Title:       "Inspect FLICA action",
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
	action, payload, err := env.Store.CreateActionWithPayload(ctx, sqlite.CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        run.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          payloadHash,
		PayloadJSON:          `{"pairing":"W7084C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload() error = %v", err)
	}
	approval, err := env.Store.RequestApproval(ctx, sqlite.RequestApprovalParams{
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
	return action, payload, approval, run
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
		"odin-core": "system_project",
		"alpha":     "github_backed_project",
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
		Store:        store,
		Registry:     registry,
		SessionStore: SessionStore{Path: filepath.Join(stateDir, "cli-session.json")},
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
