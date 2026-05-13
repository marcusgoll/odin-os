package delegations

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	"odin-os/internal/executors/router"
	"odin-os/internal/registry"
	"odin-os/internal/runtime/checkpoints"
	jobsvc "odin-os/internal/runtime/jobs"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/leases"
)

func TestPortalDeliveryAgentCreatesParentAndChildWork(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := openDelegationEnv(t)

	parentTask, parentRun, result, err := env.Delegations.RunAgent(ctx, RunInput{
		ResolvedScope: scope.Resolution{Kind: scope.ScopeProject, ProjectKey: "cfipros"},
		AgentKey:      "portal-delivery-agent",
		RequestedBy:   "operator",
		Inputs: map[string]string{
			"portal_track": "admin-cfi",
			"surface":      "dashboard",
			"goal":         "deliver stronger admin portal dashboard",
		},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if parentTask.Key == "" || parentRun == nil {
		t.Fatalf("RunAgent() parent output incomplete: task=%+v run=%+v", parentTask, parentRun)
	}
	if result.ParentTask.ID != parentTask.ID {
		t.Fatalf("result.ParentTask.ID = %d, want %d", result.ParentTask.ID, parentTask.ID)
	}
	if result.ParentRun == nil || result.ParentRun.ID != parentRun.ID {
		t.Fatalf("result.ParentRun = %+v, want run %d", result.ParentRun, parentRun.ID)
	}
	if len(result.ChildDelegations) < 5 {
		t.Fatalf("child delegations = %d, want >= 5", len(result.ChildDelegations))
	}

	for _, child := range result.ChildDelegations {
		if child.ParentTaskID != parentTask.ID {
			t.Fatalf("delegation.ParentTaskID = %d, want %d", child.ParentTaskID, parentTask.ID)
		}
		if child.ParentRunID == nil || *child.ParentRunID != parentRun.ID {
			t.Fatalf("delegation.ParentRunID = %v, want %d", child.ParentRunID, parentRun.ID)
		}
		if child.ChildTaskID == nil || child.ChildRunID == nil {
			t.Fatalf("delegation child lineage incomplete: %+v", child)
		}
		if child.Status != "completed" {
			t.Fatalf("delegation.Status = %q, want completed", child.Status)
		}

		task, err := env.Store.GetTask(ctx, *child.ChildTaskID)
		if err != nil {
			t.Fatalf("GetTask(child) error = %v", err)
		}
		if task.RequestedBy != "agent:portal-delivery-agent" {
			t.Fatalf("child task requested_by = %q, want agent:portal-delivery-agent", task.RequestedBy)
		}

		project, err := env.Store.GetProject(ctx, task.ProjectID)
		if err != nil {
			t.Fatalf("GetProject(child) error = %v", err)
		}
		packet, err := env.Store.GetLatestTaskWakePacket(ctx, project.ID, task.ID)
		if err != nil {
			t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
		}
		var wake checkpoints.TaskWakePacket
		if err := json.Unmarshal([]byte(packet.PayloadJSON), &wake); err != nil {
			t.Fatalf("json.Unmarshal(wake packet) error = %v", err)
		}
		if len(wake.SelectedCapabilities) == 0 {
			t.Fatalf("SelectedCapabilities = empty for delegation %+v", child)
		}
	}
}

func TestRegistryDelegatableAgentProfileCreatesChildWork(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := openDelegationEnv(t)
	env.Delegations.RegistrySnapshot = registry.Snapshot{
		Items: []registry.Item{testDelegatableAgentProfile()},
		ByKey: map[string]registry.Item{
			"test-delegatable-agent": testDelegatableAgentProfile(),
		},
		ByKind: map[registry.Kind][]registry.Item{
			registry.KindAgent: {testDelegatableAgentProfile()},
		},
	}

	parentTask, parentRun, result, err := env.Delegations.RunAgent(ctx, RunInput{
		ResolvedScope: scope.Resolution{Kind: scope.ScopeProject, ProjectKey: "cfipros"},
		AgentKey:      "test-delegatable-agent",
		RequestedBy:   "operator",
		Inputs: map[string]string{
			"portal_track": "student",
			"surface":      "dashboard",
			"goal":         "prove registry-backed delegation",
		},
	})
	if err != nil {
		t.Fatalf("RunAgent(registry profile) error = %v", err)
	}
	if parentRun == nil {
		t.Fatal("parentRun = nil, want parent run")
	}
	if len(result.ChildDelegations) != 2 {
		t.Fatalf("child delegations = %d, want 2", len(result.ChildDelegations))
	}
	if parentTask.Key == "" || !strings.Contains(parentTask.Key, "test-delegatable-agent") {
		t.Fatalf("parent task key = %q, want test-delegatable-agent in key", parentTask.Key)
	}

	first := result.ChildDelegations[0]
	if first.DelegationKey != "research" ||
		first.Role != "research" ||
		first.ActionClass != "registry_profile" ||
		first.ActionKey != "student:dashboard" ||
		first.MutationMode != "read_only" ||
		first.ConvergenceMode != "merge" ||
		first.ArtifactTarget != "report" ||
		first.Executor != "codex_headless" {
		t.Fatalf("first delegation = %+v, want profile-derived research child", first)
	}
	var details map[string]string
	if err := json.Unmarshal([]byte(first.DetailsJSON), &details); err != nil {
		t.Fatalf("json.Unmarshal(details) error = %v", err)
	}
	for key, want := range map[string]string{
		"agent_key":               "test-delegatable-agent",
		"portal_track":            "student",
		"surface":                 "dashboard",
		"goal":                    "prove registry-backed delegation",
		"role":                    "research",
		"execution_intent":        "read_only",
		"execution_intent_source": "companion_delegate",
	} {
		if got := details[key]; got != want {
			t.Fatalf("details[%q] = %q, want %q", key, got, want)
		}
	}

	second := result.ChildDelegations[1]
	if second.DelegationKey != "review" || second.Role != "reviewer" || second.ActionKey != "review:student:dashboard" {
		t.Fatalf("second delegation = %+v, want profile-derived reviewer child", second)
	}
}

func TestRegistryAgentWithoutDelegationProfileIsRejectedBeforePersistence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := openDelegationEnv(t)
	env.Delegations.RegistrySnapshot = registry.Snapshot{
		Items: []registry.Item{{
			Kind:   registry.KindAgent,
			Key:    "advisory-agent",
			Title:  "Advisory Agent",
			Status: "active",
		}},
		ByKey: map[string]registry.Item{
			"advisory-agent": {
				Kind:   registry.KindAgent,
				Key:    "advisory-agent",
				Title:  "Advisory Agent",
				Status: "active",
			},
		},
		ByKind: map[registry.Kind][]registry.Item{
			registry.KindAgent: {{
				Kind:   registry.KindAgent,
				Key:    "advisory-agent",
				Title:  "Advisory Agent",
				Status: "active",
			}},
		},
	}

	_, _, _, err := env.Delegations.RunAgent(ctx, RunInput{
		ResolvedScope: scope.Resolution{Kind: scope.ScopeProject, ProjectKey: "cfipros"},
		AgentKey:      "advisory-agent",
		RequestedBy:   "operator",
		Inputs: map[string]string{
			"portal_track": "student",
			"surface":      "dashboard",
			"goal":         "should fail closed",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "not runtime-delegatable") {
		t.Fatalf("RunAgent(advisory agent) error = %v, want not runtime-delegatable", err)
	}

	var persistedTasks int
	if err := env.Store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE key LIKE '%advisory-agent%'`).Scan(&persistedTasks); err != nil {
		t.Fatalf("count advisory tasks error = %v", err)
	}
	if persistedTasks != 0 {
		t.Fatalf("persisted advisory tasks = %d, want 0 after unsupported agent rejection", persistedTasks)
	}
	delegations, err := env.Store.ListDelegations(ctx, sqlite.ListDelegationsParams{})
	if err != nil {
		t.Fatalf("ListDelegations() error = %v", err)
	}
	if len(delegations) != 0 {
		t.Fatalf("delegations len = %d, want 0 after unsupported agent rejection", len(delegations))
	}
}

func TestChildDelegationRecordsSkillTelemetryAndMemory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := openDelegationEnv(t)

	_, _, result, err := env.Delegations.RunAgent(ctx, RunInput{
		ResolvedScope: scope.Resolution{Kind: scope.ScopeProject, ProjectKey: "cfipros"},
		AgentKey:      "portal-delivery-agent",
		RequestedBy:   "operator",
		Inputs: map[string]string{
			"portal_track": "student",
			"surface":      "dashboard",
			"goal":         "deliver student portal",
		},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if len(result.LearningProposalIDs) == 0 {
		t.Fatal("LearningProposalIDs = empty, want at least one learning proposal")
	}

	var designChild sqlite.Delegation
	foundDesignChild := false
	for _, child := range result.ChildDelegations {
		if child.Role == "design_direction" {
			designChild = child
			foundDesignChild = true
			break
		}
	}
	if !foundDesignChild {
		t.Fatalf("expected a design_direction child delegation in %+v", result.ChildDelegations)
	}

	childTask, err := env.Store.GetTask(ctx, *designChild.ChildTaskID)
	if err != nil {
		t.Fatalf("GetTask(child) error = %v", err)
	}
	project, err := env.Store.GetProject(ctx, childTask.ProjectID)
	if err != nil {
		t.Fatalf("GetProject(child) error = %v", err)
	}

	transcripts, err := env.Store.ListConversationTranscripts(ctx, sqlite.ListConversationTranscriptsParams{
		ProjectID: &project.ID,
		TaskID:    designChild.ChildTaskID,
		RunID:     designChild.ChildRunID,
		Scope:     "project",
		ScopeKey:  project.Key,
		Mode:      "act",
	})
	if err != nil {
		t.Fatalf("ListConversationTranscripts() error = %v", err)
	}
	if len(transcripts) != 1 {
		t.Fatalf("transcripts len = %d, want 1", len(transcripts))
	}
	if !strings.Contains(transcripts[0].Prompt, "Skill: Pixel Perfect UI/UX Designer (pixel-perfect-ui-ux-designer)") {
		t.Fatalf("transcript prompt = %q, want delegated skill prompt content", transcripts[0].Prompt)
	}
	var toolSummary map[string]string
	if err := json.Unmarshal([]byte(transcripts[0].ToolSummary), &toolSummary); err != nil {
		t.Fatalf("json.Unmarshal(tool summary) error = %v", err)
	}
	for key, want := range map[string]string{
		"agent_key":       "portal-delivery-agent",
		"delegation_id":   intString(designChild.ID),
		"portal_track":    "student",
		"requested_skill": "pixel-perfect-ui-ux-designer",
		"effective_skill": "pixel-perfect-ui-ux-designer",
		"skill_source":    "agent_template",
	} {
		if got := toolSummary[key]; got != want {
			t.Fatalf("toolSummary[%q] = %q, want %q", key, got, want)
		}
	}

	summaries, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		ProjectID:  &project.ID,
		TaskID:     designChild.ChildTaskID,
		RunID:      designChild.ChildRunID,
		Scope:      "project",
		ScopeKey:   project.Key,
		MemoryType: "episode",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries len = %d, want 1", len(summaries))
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(summaries[0].DetailsJSON), &details); err != nil {
		t.Fatalf("json.Unmarshal(details) error = %v", err)
	}
	executionMetadata, ok := details["execution_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("execution_metadata = %#v, want object", details["execution_metadata"])
	}
	if got, _ := executionMetadata["effective_skill"].(string); got != "pixel-perfect-ui-ux-designer" {
		t.Fatalf("effective_skill = %q, want pixel-perfect-ui-ux-designer", got)
	}

	artifacts, err := env.Store.ListDelegationArtifacts(ctx, sqlite.ListDelegationArtifactsParams{
		DelegationID: designChild.ID,
	})
	if err != nil {
		t.Fatalf("ListDelegationArtifacts() error = %v", err)
	}
	if !hasArtifactType(artifacts, "run_summary") || !hasArtifactType(artifacts, "memory_summary") {
		t.Fatalf("artifacts = %+v, want run_summary and memory_summary", artifacts)
	}

	proposal, err := env.Store.GetLearningProposal(ctx, result.LearningProposalIDs[0])
	if err != nil {
		t.Fatalf("GetLearningProposal() error = %v", err)
	}
	if proposal.TargetKey != "student:dashboard" {
		t.Fatalf("proposal.TargetKey = %q, want student:dashboard", proposal.TargetKey)
	}
}

func TestPortalDeliveryAgentRunsChildWavesConcurrently(t *testing.T) {
	t.Parallel()

	executor := newWaveTestExecutor()
	env := openDelegationEnvWithExecutors(t, map[string]contract.Executor{
		"codex_headless": executor,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	type runResult struct {
		parentTask sqlite.Task
		parentRun  *sqlite.Run
		result     RunResult
		err        error
	}
	done := make(chan runResult, 1)
	go func() {
		parentTask, parentRun, result, err := env.Delegations.RunAgent(ctx, RunInput{
			ResolvedScope: scope.Resolution{Kind: scope.ScopeProject, ProjectKey: "cfipros"},
			AgentKey:      "portal-delivery-agent",
			RequestedBy:   "operator",
			Inputs: map[string]string{
				"portal_track": "student",
				"surface":      "dashboard",
				"goal":         "deliver student portal",
			},
		})
		done <- runResult{parentTask: parentTask, parentRun: parentRun, result: result, err: err}
	}()

	executor.waitForStage(t, 1)
	executor.assertStageNotStarted(t, 2)
	executor.releaseStage(1)

	executor.waitForStage(t, 2)
	executor.assertStageNotStarted(t, 3)
	executor.releaseStage(2)

	executor.waitForStage(t, 3)
	executor.releaseStage(3)

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("RunAgent() error = %v", result.err)
		}
		if len(result.result.ChildDelegations) != 5 {
			t.Fatalf("child delegations = %d, want 5", len(result.result.ChildDelegations))
		}
		gotRoles := delegationRoles(result.result.ChildDelegations)
		wantRoles := []string{"ia_audit", "design_direction", "implementation_handoff", "visual_verification", "learning_capture"}
		if strings.Join(gotRoles, ",") != strings.Join(wantRoles, ",") {
			t.Fatalf("delegation roles = %v, want %v", gotRoles, wantRoles)
		}
		if !executor.stageStartedWithRoles(1, "ia_audit", "design_direction") {
			t.Fatalf("stage 1 roles = %v, want ia_audit + design_direction", executor.rolesForStage(1))
		}
		if !executor.stageStartedWithRoles(2, "implementation_handoff", "visual_verification") {
			t.Fatalf("stage 2 roles = %v, want implementation_handoff + visual_verification", executor.rolesForStage(2))
		}
		if !executor.stageStartedWithRoles(3, "learning_capture") {
			t.Fatalf("stage 3 roles = %v, want learning_capture", executor.rolesForStage(3))
		}
	case <-ctx.Done():
		t.Fatal("RunAgent() did not complete before timeout")
	}
}

func TestPortalDeliveryAgentFailsParentWhenChildWaveFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := openDelegationEnvWithExecutors(t, map[string]contract.Executor{
		"codex_headless": failingDelegationExecutor{
			StaticExecutor: contract.NewStaticExecutor(
				"codex_headless",
				contract.ExecutorClassPlanBackedCLI,
				contract.HealthReport{Status: contract.HealthStatusHealthy},
				contract.Capabilities{
					SupportsHeadlessPlan: true,
					TaskKinds:            []contract.TaskKind{contract.TaskKindGeneral},
					Scopes:               []string{"project", "odin-core", "new-project"},
				},
			),
			failingRole: "design_direction",
		},
	})

	parentTask, parentRun, result, err := env.Delegations.RunAgent(ctx, RunInput{
		ResolvedScope: scope.Resolution{Kind: scope.ScopeProject, ProjectKey: "cfipros"},
		AgentKey:      "portal-delivery-agent",
		RequestedBy:   "operator",
		Inputs: map[string]string{
			"portal_track": "student",
			"surface":      "dashboard",
			"goal":         "deliver student portal",
		},
	})
	if err == nil {
		t.Fatal("RunAgent() error = nil, want child delegation failure")
	}
	if !strings.Contains(err.Error(), "design_direction failed") {
		t.Fatalf("RunAgent() error = %v, want design_direction failure", err)
	}
	if parentRun == nil {
		t.Fatal("parentRun = nil, want failed parent run")
	}
	if result.ParentRun == nil || result.ParentRun.Status != "failed" {
		t.Fatalf("result.ParentRun = %+v, want status failed", result.ParentRun)
	}
	if result.ParentTask.Status != "failed" {
		t.Fatalf("result.ParentTask.Status = %q, want failed", result.ParentTask.Status)
	}

	storedRun, storeErr := env.Store.GetRun(ctx, parentRun.ID)
	if storeErr != nil {
		t.Fatalf("GetRun(parent) error = %v", storeErr)
	}
	if storedRun.Status != "failed" {
		t.Fatalf("storedRun.Status = %q, want failed", storedRun.Status)
	}
	if !strings.Contains(storedRun.Summary, "design_direction failed") {
		t.Fatalf("storedRun.Summary = %q, want child failure summary", storedRun.Summary)
	}

	storedTask, storeErr := env.Store.GetTask(ctx, parentTask.ID)
	if storeErr != nil {
		t.Fatalf("GetTask(parent) error = %v", storeErr)
	}
	if storedTask.Status != "failed" {
		t.Fatalf("storedTask.Status = %q, want failed", storedTask.Status)
	}
}

type delegationEnv struct {
	Store       *sqlite.Store
	Jobs        jobsvc.Service
	Delegations Service
}

func openDelegationEnv(t *testing.T) delegationEnv {
	t.Helper()
	return openDelegationEnvWithExecutors(t, defaultDelegationExecutors())
}

func openDelegationEnvWithExecutors(t *testing.T, executors map[string]contract.Executor) delegationEnv {
	t.Helper()

	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	registry := writeDelegationRegistry(t)
	jobsService := jobsvc.Service{
		Store:          store,
		Registry:       registry,
		Executors:      executors,
		ExecutorConfig: mustLoadExecutorConfig(t),
		Transitions:    projects.Service{Store: store},
		Leases: leases.Manager{
			Store:        store,
			Git:          delegationTestGit{},
			WorktreeRoot: t.TempDir(),
		},
	}

	project, err := jobsService.CreateTask(ctx, jobsvc.CreateTaskParams{
		Resolved: scope.Resolution{
			Kind:       scope.ScopeProject,
			ProjectKey: "cfipros",
		},
		Title:       "seed project",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask(seed) error = %v", err)
	}
	cfiprosProject, err := store.GetProject(ctx, project.ProjectID)
	if err != nil {
		t.Fatalf("GetProject(seed) error = %v", err)
	}
	if _, err := jobsService.Transitions.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:      cfiprosProject.ID,
		Actor:          projects.TransitionControllerOdinOS,
		TargetState:    projects.TransitionStateLimitedAction,
		LimitedActions: []string{"run_task"},
		ChangedBy:      "test",
		Notes:          "allow agent execution",
	}); err != nil {
		t.Fatalf("SetTransitionState(limited_action) error = %v", err)
	}
	recordHealthyDelegationExecutor(t, ctx, store, "codex_headless")

	delegationService := Service{
		Store:            store,
		Jobs:             jobsService,
		Checkpoints:      checkpoints.Service{Store: store},
		RegistrySnapshot: delegationTestRegistrySnapshot(),
	}

	return delegationEnv{
		Store:       store,
		Jobs:        jobsService,
		Delegations: delegationService,
	}
}

func defaultDelegationExecutors() map[string]contract.Executor {
	return map[string]contract.Executor{
		"codex_headless": successfulDelegationExecutor{
			StaticExecutor: contract.NewStaticExecutor(
				"codex_headless",
				contract.ExecutorClassPlanBackedCLI,
				contract.HealthReport{Status: contract.HealthStatusHealthy},
				contract.Capabilities{
					SupportsHeadlessPlan: true,
					TaskKinds:            []contract.TaskKind{contract.TaskKindGeneral},
					Scopes:               []string{"project", "odin-core", "new-project"},
				},
			),
		},
	}
}

func recordHealthyDelegationExecutor(t *testing.T, ctx context.Context, store *sqlite.Store, executorKey string) {
	t.Helper()

	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    executorKey,
		Status:      "healthy",
		LatencyMS:   0,
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth(%s) error = %v", executorKey, err)
	}
}

type waveTestExecutor struct {
	*contract.StaticExecutor

	mu           sync.Mutex
	stageRoles   map[int]map[string]bool
	stageStarted map[int]chan struct{}
	stageRelease map[int]chan struct{}
}

type successfulDelegationExecutor struct {
	*contract.StaticExecutor
}

func (executor successfulDelegationExecutor) RunTask(_ context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{
		Status: "completed",
		Output: fmt.Sprintf("%s completed delegated task", spec.Metadata["child_role"]),
	}, nil
}

func (successfulDelegationExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}

func (successfulDelegationExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return contract.ErrNotImplemented
}

func (successfulDelegationExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, contract.ErrNotImplemented
}

func newWaveTestExecutor() *waveTestExecutor {
	return &waveTestExecutor{
		StaticExecutor: contract.NewStaticExecutor(
			"codex_headless",
			contract.ExecutorClassPlanBackedCLI,
			contract.HealthReport{Status: contract.HealthStatusHealthy},
			contract.Capabilities{
				SupportsHeadlessPlan: true,
				TaskKinds:            []contract.TaskKind{contract.TaskKindGeneral},
				Scopes:               []string{"project", "odin-core", "new-project"},
			},
		),
		stageRoles: map[int]map[string]bool{
			1: {},
			2: {},
			3: {},
		},
		stageStarted: map[int]chan struct{}{
			1: make(chan struct{}),
			2: make(chan struct{}),
			3: make(chan struct{}),
		},
		stageRelease: map[int]chan struct{}{
			1: make(chan struct{}),
			2: make(chan struct{}),
			3: make(chan struct{}),
		},
	}
}

type failingDelegationExecutor struct {
	*contract.StaticExecutor
	failingRole string
}

func (executor failingDelegationExecutor) RunTask(_ context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	if spec.Metadata["child_role"] == executor.failingRole {
		return contract.ExecutionResult{}, fmt.Errorf("%s failed", executor.failingRole)
	}
	return contract.ExecutionResult{
		Status: "completed",
		Output: "failing-delegation-executor completed delegated task",
	}, nil
}

func (failingDelegationExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}

func (failingDelegationExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return contract.ErrNotImplemented
}

func (failingDelegationExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, contract.ErrNotImplemented
}

func (executor *waveTestExecutor) RunTask(ctx context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	role := spec.Metadata["child_role"]
	stage := delegationRoleWave(role)

	executor.mu.Lock()
	executor.stageRoles[stage][role] = true
	if executor.stageReadyLocked(stage) {
		select {
		case <-executor.stageStarted[stage]:
		default:
			close(executor.stageStarted[stage])
		}
	}
	executor.mu.Unlock()

	select {
	case <-ctx.Done():
		return contract.ExecutionResult{}, ctx.Err()
	case <-executor.stageRelease[stage]:
		return contract.ExecutionResult{
			Status: "completed",
			Output: "wave-test completed delegated task",
		}, nil
	}
}

func (executor *waveTestExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}

func (executor *waveTestExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return contract.ErrNotImplemented
}

func (executor *waveTestExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, contract.ErrNotImplemented
}

func (executor *waveTestExecutor) waitForStage(t *testing.T, stage int) {
	t.Helper()
	select {
	case <-executor.stageStarted[stage]:
	case <-time.After(2 * time.Second):
		t.Fatalf("stage %d did not start in time; roles=%v", stage, executor.rolesForStage(stage))
	}
}

func (executor *waveTestExecutor) assertStageNotStarted(t *testing.T, stage int) {
	t.Helper()
	select {
	case <-executor.stageStarted[stage]:
		t.Fatalf("stage %d started too early; roles=%v", stage, executor.rolesForStage(stage))
	default:
	}
}

func (executor *waveTestExecutor) releaseStage(stage int) {
	select {
	case <-executor.stageRelease[stage]:
	default:
		close(executor.stageRelease[stage])
	}
}

func (executor *waveTestExecutor) stageStartedWithRoles(stage int, roles ...string) bool {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	for _, role := range roles {
		if !executor.stageRoles[stage][role] {
			return false
		}
	}
	return true
}

func (executor *waveTestExecutor) rolesForStage(stage int) []string {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	roles := make([]string, 0, len(executor.stageRoles[stage]))
	for role := range executor.stageRoles[stage] {
		roles = append(roles, role)
	}
	return roles
}

func (executor *waveTestExecutor) stageReadyLocked(stage int) bool {
	for _, role := range expectedRolesForWave(stage) {
		if !executor.stageRoles[stage][role] {
			return false
		}
	}
	return true
}

func delegationRoles(delegations []sqlite.Delegation) []string {
	roles := make([]string, 0, len(delegations))
	for _, delegation := range delegations {
		roles = append(roles, delegation.Role)
	}
	return roles
}

func expectedRolesForWave(stage int) []string {
	switch stage {
	case 1:
		return []string{"ia_audit", "design_direction"}
	case 2:
		return []string{"implementation_handoff", "visual_verification"}
	case 3:
		return []string{"learning_capture"}
	default:
		return nil
	}
}

func delegationTestRegistrySnapshot() registry.Snapshot {
	skill := registry.Item{
		Kind:    registry.KindSkill,
		Key:     "pixel-perfect-ui-ux-designer",
		Title:   "Pixel Perfect UI/UX Designer",
		Summary: "Audits product UI with project-specific taste, strong references, information hierarchy discipline, and Huginn-backed visual verification.",
		Sections: map[string]string{
			registry.SectionPurpose:         "Turn a vague dashboard critique request into a concrete product design audit with stronger hierarchy, clearer information density decisions, sharper visual taste, and an implementation-ready improvement path.",
			registry.SectionWhenToUse:       "Use this skill when the task is to audit or improve UI, UX, dashboard layout, information visibility, component hierarchy, interaction clarity, or visual polish in a real product context.",
			registry.SectionInputs:          "The skill expects the current product surface, project context, user goals, available screenshots or live pages, any existing visual language already present in the product, and any Huginn capture evidence that can validate the real interface.",
			registry.SectionProcedure:       "Inspect the product before proposing changes. Build project-specific taste from what the app is trying to be, not from one fixed personal style. Identify the dashboard's real jobs, the most important signals, and what deserves immediate visibility.",
			registry.SectionOutputs:         "The output is a practical dashboard audit with strengths, weaknesses, missing information, hierarchy and layout problems, visual style recommendations, a stronger design direction, and concrete next implementation steps backed by visual evidence when available.",
			registry.SectionConstraints:     "Do not default to generic AI dashboard tropes. Do not force one static aesthetic across projects. Do not critique visuals without tying them to product meaning and information priority.",
			registry.SectionSuccessCriteria: "The result explains what the dashboard should emphasize, what should change, why the changes matter, and how the design direction should feel for this project specifically.",
		},
	}
	return registry.Snapshot{
		Items: []registry.Item{skill},
		ByKey: map[string]registry.Item{
			skill.Key: skill,
		},
		ByKind: map[registry.Kind][]registry.Item{
			registry.KindSkill: {skill},
		},
	}
}

func testDelegatableAgentProfile() registry.Item {
	return registry.Item{
		Kind:    registry.KindAgent,
		Key:     "test-delegatable-agent",
		Title:   "Test Delegatable Agent",
		Summary: "Exercises registry-backed delegation profile compilation.",
		Status:  "active",
		Delegation: registry.DelegationProfile{
			Enabled:         true,
			OperatorSurface: "companion_delegate",
			Inputs: registry.DelegationInputs{
				Required: []string{"portal_track", "surface"},
				Optional: []string{"goal", "intent"},
			},
			ConvergenceMode: "merge",
			Children: []registry.DelegationChildProfile{
				{
					DelegationKey:      "research",
					Role:               "research",
					Wave:               1,
					ActionClass:        "registry_profile",
					ActionKeyTemplate:  "{{portal_track}}:{{surface}}",
					MutationModeSource: "intent",
					ArtifactTarget:     "report",
					Executor:           "codex_headless",
				},
				{
					DelegationKey:      "review",
					Role:               "reviewer",
					Wave:               2,
					ActionClass:        "registry_profile",
					ActionKeyTemplate:  "review:{{portal_track}}:{{surface}}",
					MutationModeSource: "intent",
					ArtifactTarget:     "report",
					Executor:           "codex_headless",
				},
			},
		},
	}
}

func writeDelegationRegistry(t *testing.T) projects.Registry {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "projects.yaml")
	for _, key := range []string{"odin-core", "cfipros"} {
		gitRoot := filepath.Join(root, key)
		if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir git root: %v", err)
		}
	}

	const config = `
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: odin-core
    default_branch: main
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: true
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
  - key: cfipros
    name: CFIPros
    project_class: github_backed_project
    git_root: cfipros
    default_branch: main
    github:
      repo: acme/cfipros
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: false
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	registry, diagnostics, err := projects.Register(configPath)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Register() diagnostics = %#v", diagnostics)
	}
	return registry
}

func mustLoadExecutorConfig(t *testing.T) router.Config {
	t.Helper()

	config, err := router.LoadConfig(filepath.Join("..", "..", "..", "config", "executors.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	return config
}

type delegationTestGit struct{}

func (delegationTestGit) BranchExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (delegationTestGit) CreateBranch(context.Context, string, string, string) error {
	return nil
}

func (delegationTestGit) AddWorktree(_ context.Context, _, worktreePath, branch string) error {
	if err := os.MkdirAll(filepath.Join(worktreePath, ".git"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(worktreePath, ".branch"), []byte(branch), 0o644)
}

func (delegationTestGit) RemoveWorktree(_ context.Context, _, worktreePath string) error {
	return os.RemoveAll(worktreePath)
}

func (delegationTestGit) WorktreeDirty(context.Context, string) (bool, error) {
	return false, nil
}

func hasArtifactType(artifacts []sqlite.DelegationArtifact, want string) bool {
	for _, artifact := range artifacts {
		if artifact.ArtifactType == want {
			return true
		}
	}
	return false
}

func intString(value int64) string {
	return strconv.FormatInt(value, 10)
}
