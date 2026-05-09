package integration_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/app/bootstrap"
	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	"odin-os/internal/executors/router"
	"odin-os/internal/runtime/checkpoints"
	jobsvc "odin-os/internal/runtime/jobs"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/leases"
)

func TestN8NIntakeExecutionMetadata(t *testing.T) {
	ctx := context.Background()
	repoRoot := projectRoot(t)
	runtimeRoot := t.TempDir()

	app, err := bootstrap.Load(ctx, repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	projectManifest, ok := app.Registry.Lookup("pbs")
	if !ok {
		t.Fatal("expected pbs project in registry")
	}

	service := jobsvc.Service{
		Store:    app.Store,
		Registry: app.Registry,
		Executors: map[string]contract.Executor{
			"fake_headless": intakeTestExecutor{
				result: contract.ExecutionResult{
					Status: "completed",
					Output: "intake execution complete",
				},
			},
		},
		ExecutorConfig: intakeTestExecutorConfig(),
		Transitions:    projects.Service{Store: app.Store},
		Leases: leases.Manager{
			Store:        app.Store,
			Git:          intakeTestGit{},
			WorktreeRoot: filepath.Join(t.TempDir(), "worktrees"),
		},
		Now: time.Now,
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: projectManifest.Key,
	}, "Investigate PBS intake metadata")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	if _, err := app.Store.CreateTaskIntake(ctx, sqlite.CreateTaskIntakeParams{
		TaskID:      task.ID,
		Source:      "n8n",
		IntakeType:  "ci_failure",
		DedupKey:    "ci_failure:pbs:1234",
		RequestedBy: "n8n",
		PayloadJSON: `{"run_id":"1234","workflow_id":"pbs-ci-1"}`,
	}); err != nil {
		t.Fatalf("CreateTaskIntake() error = %v", err)
	}

	project, err := app.Store.GetProjectByKey(ctx, projectManifest.Key)
	if err != nil {
		t.Fatalf("GetProjectByKey() error = %v", err)
	}
	if _, err := service.Transitions.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}

	var captured contract.TaskSpec
	service.Executors["fake_headless"] = intakeTestExecutor{
		result: contract.ExecutionResult{
			Status: "completed",
			Output: "intake execution complete",
		},
		onRun: func(spec contract.TaskSpec) {
			captured = spec
		},
	}

	outcome, err := service.ExecuteTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}
	if outcome.Run == nil || outcome.Run.Status != "completed" {
		t.Fatalf("Run = %+v, want completed run", outcome.Run)
	}

	if captured.Prompt != task.Title {
		t.Fatalf("Prompt = %q, want %q", captured.Prompt, task.Title)
	}
	if captured.Metadata["intake_source"] != "n8n" {
		t.Fatalf("intake_source = %q, want n8n", captured.Metadata["intake_source"])
	}
	if captured.Metadata["intake_type"] != "ci_failure" {
		t.Fatalf("intake_type = %q, want ci_failure", captured.Metadata["intake_type"])
	}
	if !strings.Contains(captured.Metadata["intake_payload_json"], `"workflow_id":"pbs-ci-1"`) {
		t.Fatalf("intake_payload_json = %q, want raw payload", captured.Metadata["intake_payload_json"])
	}

	packet, err := app.Store.GetLatestTaskWakePacket(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if packet.Trigger != string(checkpoints.TriggerHandoff) {
		t.Fatalf("WakePacket.Trigger = %q, want %q", packet.Trigger, checkpoints.TriggerHandoff)
	}
	if !strings.Contains(packet.Summary, "n8n ci_failure intake") {
		t.Fatalf("WakePacket.Summary = %q, want concise intake summary", packet.Summary)
	}
	if !strings.Contains(packet.PayloadJSON, `"intake_summary":"n8n ci_failure intake"`) {
		t.Fatalf("WakePacket.PayloadJSON = %q, want intake summary payload", packet.PayloadJSON)
	}
}

type intakeTestExecutor struct {
	result  contract.ExecutionResult
	onRun   func(contract.TaskSpec)
	runFunc func(contract.TaskSpec) (contract.ExecutionResult, error)
}

func (intakeTestExecutor) Key() string { return "fake_headless" }

func (intakeTestExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (intakeTestExecutor) Health(context.Context) (contract.HealthReport, error) {
	return contract.HealthReport{Status: contract.HealthStatusHealthy}, nil
}

func (intakeTestExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsResume:       true,
		SupportsCancel:       true,
		SupportsTools:        true,
		SupportsCostEstimate: true,
		SupportsHeadlessPlan: true,
		TaskKinds:            []contract.TaskKind{contract.TaskKindGeneral},
		Scopes:               []string{"project"},
	}, nil
}

func (executor intakeTestExecutor) RunTask(_ context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	if executor.onRun != nil {
		executor.onRun(spec)
	}
	if executor.runFunc != nil {
		return executor.runFunc(spec)
	}
	return executor.result, nil
}

func (intakeTestExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, nil
}

func (intakeTestExecutor) CancelTask(context.Context, contract.TaskHandle) error { return nil }

func (intakeTestExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{Currency: "USD"}, nil
}

func intakeTestExecutorConfig() router.Config {
	return router.Config{
		Version: 1,
		Executors: []router.ExecutorConfig{
			{
				Key:      "fake_headless",
				Adapter:  "fake_headless",
				Class:    contract.ExecutorClassPlanBackedCLI,
				Enabled:  true,
				Priority: 1,
			},
		},
		Routes: []router.RouteConfig{
			{
				Name: "project-general",
				Match: router.RouteMatch{
					TaskKinds: []contract.TaskKind{contract.TaskKindGeneral},
					Scopes:    []string{"project"},
				},
				Preferred: []string{"fake_headless"},
			},
		},
	}
}

type intakeTestGit struct{}

func (intakeTestGit) BranchExists(context.Context, string, string) (bool, error) { return false, nil }
func (intakeTestGit) CreateBranch(context.Context, string, string, string) error { return nil }
func (intakeTestGit) AddWorktree(context.Context, string, string, string) error  { return nil }
func (intakeTestGit) RemoveWorktree(context.Context, string, string) error       { return nil }
func (intakeTestGit) WorktreeDirty(context.Context, string) (bool, error)        { return false, nil }
