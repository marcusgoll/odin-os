package factory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/store/sqlite"
)

func TestAdmitOperatorStartCreatesFactoryLaneWorkItem(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openFactoryStore(t)
	defer store.Close()

	service := Service{
		Store: store,
		Jobs:  jobs.Service{Store: store, Registry: writeFactoryRegistry(t)},
	}
	result, err := service.AdmitOperatorStart(ctx, AdmitOperatorInput{
		ProjectKey:  "alpha",
		Title:       "Implement factory lane status",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("AdmitOperatorStart() error = %v", err)
	}
	if !result.Created {
		t.Fatal("Created = false, want true")
	}
	if result.Task.Status != "queued" {
		t.Fatalf("Task.Status = %q, want queued", result.Task.Status)
	}
	if result.Task.WorkKind != WorkKindFactoryLane {
		t.Fatalf("Task.WorkKind = %q, want %q", result.Task.WorkKind, WorkKindFactoryLane)
	}
	if result.Task.ExecutionIntent != "mutation" || result.Task.ExecutionIntentSource != "factory_lane:operator" {
		t.Fatalf("Task intent = %q/%q, want mutation/factory_lane:operator", result.Task.ExecutionIntent, result.Task.ExecutionIntentSource)
	}
	if result.Trigger != "operator" || result.Autonomy != AutonomyMergeWhenGreen || result.Phase != "admitted" {
		t.Fatalf("result lane fields = %q/%q/%q", result.Trigger, result.Autonomy, result.Phase)
	}

	var artifacts []laneArtifact
	if err := json.Unmarshal([]byte(result.Task.ArtifactsJSON), &artifacts); err != nil {
		t.Fatalf("ArtifactsJSON unmarshal error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts len = %d, want 1", len(artifacts))
	}
	artifact := artifacts[0]
	if artifact.Type != WorkKindFactoryLane || artifact.ProfileKey != ProfileKey || artifact.Trigger != "operator" || artifact.Autonomy != AutonomyMergeWhenGreen || artifact.Phase != "admitted" {
		t.Fatalf("artifact = %+v, want factory lane admission artifact", artifact)
	}
}

func TestPromoteAcceptedIntakeCreatesFactoryLaneWorkItem(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openFactoryStore(t)
	defer store.Close()

	service := Service{
		Store: store,
		Jobs:  jobs.Service{Store: store, Registry: writeFactoryRegistry(t)},
	}
	result, err := service.PromoteAcceptedIntake(ctx, sqlite.IntakeItem{
		ID:       42,
		Scope:    "project",
		ScopeKey: "alpha",
		Subject:  "Implement reviewed intake factory lane",
	}, "Implement reviewed intake factory lane", []string{"factory lane accepts reviewed intake"})
	if err != nil {
		t.Fatalf("PromoteAcceptedIntake() error = %v", err)
	}
	if !result.Created {
		t.Fatal("Created = false, want true")
	}
	if result.Task.Key != "intake-review-42" || result.Task.WorkKind != WorkKindFactoryLane {
		t.Fatalf("Task key/work_kind = %q/%q, want intake-review-42/%s", result.Task.Key, result.Task.WorkKind, WorkKindFactoryLane)
	}
	if result.Task.ExecutionIntent != "mutation" || result.Task.ExecutionIntentSource != "factory_lane:intake_review" {
		t.Fatalf("Task intent = %q/%q, want mutation/factory_lane:intake_review", result.Task.ExecutionIntent, result.Task.ExecutionIntentSource)
	}
	if result.Task.RequestedBy != "intake_review:intake-42" {
		t.Fatalf("Task.RequestedBy = %q, want intake_review:intake-42", result.Task.RequestedBy)
	}
	if len(result.Task.AcceptanceCriteria) != 1 || result.Task.AcceptanceCriteria[0] != "factory lane accepts reviewed intake" {
		t.Fatalf("Task.AcceptanceCriteria = %#v, want passed acceptance", result.Task.AcceptanceCriteria)
	}
	if result.Trigger != "intake_review" || result.Autonomy != AutonomyMergeWhenGreen || result.Phase != "admitted" {
		t.Fatalf("result lane fields = %q/%q/%q", result.Trigger, result.Autonomy, result.Phase)
	}

	var artifacts []laneArtifact
	if err := json.Unmarshal([]byte(result.Task.ArtifactsJSON), &artifacts); err != nil {
		t.Fatalf("ArtifactsJSON unmarshal error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts len = %d, want 1", len(artifacts))
	}
	artifact := artifacts[0]
	if artifact.Type != WorkKindFactoryLane || artifact.ProfileKey != ProfileKey || artifact.Trigger != "intake_review" || artifact.Autonomy != AutonomyMergeWhenGreen || artifact.Phase != "admitted" {
		t.Fatalf("artifact = %+v, want intake-review factory lane artifact", artifact)
	}
}

func TestPromoteAcceptedIntakeIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openFactoryStore(t)
	defer store.Close()

	service := Service{
		Store: store,
		Jobs:  jobs.Service{Store: store, Registry: writeFactoryRegistry(t)},
	}
	item := sqlite.IntakeItem{ID: 42, Scope: "project", ScopeKey: "alpha", Subject: "Implement idempotent factory promotion"}
	first, err := service.PromoteAcceptedIntake(ctx, item, item.Subject, nil)
	if err != nil {
		t.Fatalf("first PromoteAcceptedIntake() error = %v", err)
	}
	second, err := service.PromoteAcceptedIntake(ctx, item, item.Subject, nil)
	if err != nil {
		t.Fatalf("second PromoteAcceptedIntake() error = %v", err)
	}
	if !first.Created || second.Created {
		t.Fatalf("Created first/second = %t/%t, want true/false", first.Created, second.Created)
	}
	if first.Task.ID != second.Task.ID || second.Task.Key != "intake-review-42" {
		t.Fatalf("tasks = %+v then %+v, want same intake-review task", first.Task, second.Task)
	}

	rows, err := store.DB().QueryContext(ctx, `SELECT COUNT(*) FROM tasks WHERE key = 'intake-review-42'`)
	if err != nil {
		t.Fatalf("count tasks query error = %v", err)
	}
	defer rows.Close()
	var count int
	if !rows.Next() {
		t.Fatal("count tasks returned no row")
	}
	if err := rows.Scan(&count); err != nil {
		t.Fatalf("count scan error = %v", err)
	}
	if count != 1 {
		t.Fatalf("task count = %d, want 1", count)
	}
}

func TestPromoteAcceptedIntakeDefersHighRiskTitlesToJobsClassifier(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openFactoryStore(t)
	defer store.Close()

	registry := writeFactoryRegistry(t)
	jobService := jobs.Service{
		Store:          store,
		Registry:       registry,
		ExecutorConfig: loadFactoryExecutorConfig(t),
		Executors: map[string]contract.Executor{
			"codex_headless": contract.NewStaticExecutor("codex_headless", contract.ExecutorClassPlanBackedCLI, contract.HealthReport{Status: contract.HealthStatusHealthy}, contract.Capabilities{
				SupportsHeadlessPlan: true,
				TaskKinds:            []contract.TaskKind{contract.TaskKindGeneral},
				Scopes:               []string{"project", "odin-core", "new-project"},
			}),
		},
	}
	service := Service{Store: store, Jobs: jobService}
	admission, err := service.PromoteAcceptedIntake(ctx, sqlite.IntakeItem{
		ID:       43,
		Scope:    "project",
		ScopeKey: "alpha",
		Subject:  "Force push branch",
	}, "Force push branch", nil)
	if err != nil {
		t.Fatalf("PromoteAcceptedIntake() error = %v", err)
	}
	if admission.Task.WorkKind != WorkKindFactoryLane {
		t.Fatalf("Task.WorkKind = %q, want %q", admission.Task.WorkKind, WorkKindFactoryLane)
	}
	if admission.Task.ExecutionIntent != "destructive" || admission.Task.ExecutionIntentSource != "safety_classifier" {
		t.Fatalf("Task intent = %q/%q, want destructive/safety_classifier", admission.Task.ExecutionIntent, admission.Task.ExecutionIntentSource)
	}

	if err := jobService.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v", err)
	}
	blocked, err := store.GetTask(ctx, admission.Task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if blocked.Status != "blocked" || blocked.BlockedReason != "approval_required" {
		t.Fatalf("blocked task = %+v, want blocked approval_required", blocked)
	}
	if blocked.ExecutionIntent != "destructive" || blocked.ExecutionIntentSource != "safety_classifier" {
		t.Fatalf("blocked intent = %q/%q, want destructive/safety_classifier", blocked.ExecutionIntent, blocked.ExecutionIntentSource)
	}
	approval, err := store.GetLatestTaskApproval(ctx, admission.Task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskApproval() error = %v", err)
	}
	if approval.Status != "pending" {
		t.Fatalf("approval status = %q, want pending", approval.Status)
	}
}

func TestStatusReadsFactoryLaneTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openFactoryStore(t)
	defer store.Close()

	service := Service{
		Store: store,
		Jobs:  jobs.Service{Store: store, Registry: writeFactoryRegistry(t)},
	}
	admission, err := service.AdmitOperatorStart(ctx, AdmitOperatorInput{
		ProjectKey:  "alpha",
		Title:       "Implement status readback",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("AdmitOperatorStart() error = %v", err)
	}

	statusByID, err := service.Status(ctx, "task-"+strconv.FormatInt(admission.Task.ID, 10))
	if err != nil {
		t.Fatalf("Status(task-id) error = %v", err)
	}
	if statusByID.Task.ID != admission.Task.ID {
		t.Fatalf("Status(task-id).Task.ID = %d, want %d", statusByID.Task.ID, admission.Task.ID)
	}
	statusByKey, err := service.Status(ctx, admission.Task.Key)
	if err != nil {
		t.Fatalf("Status(key) error = %v", err)
	}
	if statusByKey.Task.ID != admission.Task.ID {
		t.Fatalf("Status(key).Task.ID = %d, want %d", statusByKey.Task.ID, admission.Task.ID)
	}
	if statusByKey.Trigger != "operator" || statusByKey.Autonomy != AutonomyMergeWhenGreen || statusByKey.Phase != "admitted" {
		t.Fatalf("status lane fields = %q/%q/%q", statusByKey.Trigger, statusByKey.Autonomy, statusByKey.Phase)
	}

	views, err := store.DB().QueryContext(ctx, `SELECT COUNT(*) FROM tasks`)
	if err != nil {
		t.Fatalf("count tasks query error = %v", err)
	}
	defer views.Close()
	var count int
	if !views.Next() {
		t.Fatal("count tasks returned no row")
	}
	if err := views.Scan(&count); err != nil {
		t.Fatalf("count scan error = %v", err)
	}
	if count != 1 {
		t.Fatalf("task count after status = %d, want 1", count)
	}
}

func TestStatusRejectsNonFactoryAndInvalidFactoryTasks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		workKind      string
		artifactsJSON string
		wantError     string
	}{
		{
			name:          "non_factory_work_kind",
			workKind:      "project",
			artifactsJSON: `[{"type":"factory_lane","profile_key":"software-factory-lane-workflow","trigger":"operator","autonomy":"merge_when_green","phase":"admitted"}]`,
			wantError:     "is not \"factory_lane\"",
		},
		{
			name:          "missing_artifacts",
			workKind:      WorkKindFactoryLane,
			artifactsJSON: "",
			wantError:     "missing factory lane artifact",
		},
		{
			name:          "malformed_artifacts",
			workKind:      WorkKindFactoryLane,
			artifactsJSON: `{not-json`,
			wantError:     "malformed factory artifacts",
		},
		{
			name:          "missing_factory_lane_artifact",
			workKind:      WorkKindFactoryLane,
			artifactsJSON: `[{"type":"other","profile_key":"software-factory-lane-workflow","trigger":"operator","autonomy":"merge_when_green","phase":"admitted"}]`,
			wantError:     "missing factory lane artifact",
		},
		{
			name:          "wrong_profile",
			workKind:      WorkKindFactoryLane,
			artifactsJSON: `[{"type":"factory_lane","profile_key":"other-profile","trigger":"operator","autonomy":"merge_when_green","phase":"admitted"}]`,
			wantError:     "factory artifact profile",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			store := openFactoryStore(t)
			defer store.Close()
			project := createFactoryTestProject(t, ctx, store)
			task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
				ProjectID:             project.ID,
				Key:                   tc.name,
				Title:                 "Status rejection fixture",
				Status:                "queued",
				Scope:                 "project",
				RequestedBy:           "operator",
				WorkKind:              tc.workKind,
				ArtifactsJSON:         tc.artifactsJSON,
				ExecutionIntent:       "mutation",
				ExecutionIntentSource: "test",
			})
			if err != nil {
				t.Fatalf("CreateTask() error = %v", err)
			}

			_, err = (Service{Store: store}).Status(ctx, strconv.FormatInt(task.ID, 10))
			if err == nil {
				t.Fatal("Status() error = nil, want invalid factory task error")
			}
			if !strings.Contains(err.Error(), "invalid factory task") || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("Status() error = %q, want invalid factory task containing %q", err.Error(), tc.wantError)
			}
		})
	}
}

func TestAdmitOperatorStartBlocksGovernanceWorkBehindApproval(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		title      string
		wantIntent string
	}{
		{name: "governance_high_risk", title: "Deploy code to production", wantIntent: "governance"},
		{name: "force_push_destructive", title: "Force push branch", wantIntent: "destructive"},
		{name: "delete_cache_destructive", title: "Delete cache", wantIntent: "destructive"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			store := openFactoryStore(t)
			defer store.Close()

			registry := writeFactoryRegistry(t)
			jobService := jobs.Service{
				Store:          store,
				Registry:       registry,
				ExecutorConfig: loadFactoryExecutorConfig(t),
				Executors: map[string]contract.Executor{
					"codex_headless": contract.NewStaticExecutor("codex_headless", contract.ExecutorClassPlanBackedCLI, contract.HealthReport{Status: contract.HealthStatusHealthy}, contract.Capabilities{
						SupportsHeadlessPlan: true,
						TaskKinds:            []contract.TaskKind{contract.TaskKindGeneral},
						Scopes:               []string{"project", "odin-core", "new-project"},
					}),
				},
			}
			service := Service{Store: store, Jobs: jobService}
			admission, err := service.AdmitOperatorStart(ctx, AdmitOperatorInput{
				ProjectKey:  "alpha",
				Title:       tc.title,
				RequestedBy: "operator",
			})
			if err != nil {
				t.Fatalf("AdmitOperatorStart() error = %v", err)
			}
			if admission.Task.Status != "queued" {
				t.Fatalf("admitted status = %q, want queued before scheduler admission", admission.Task.Status)
			}
			if admission.Task.ExecutionIntent != tc.wantIntent || admission.Task.ExecutionIntentSource != "safety_classifier" {
				t.Fatalf("admitted intent = %q/%q, want %s/safety_classifier", admission.Task.ExecutionIntent, admission.Task.ExecutionIntentSource, tc.wantIntent)
			}

			if err := jobService.ExecuteNextQueued(ctx); err != nil {
				t.Fatalf("ExecuteNextQueued() error = %v", err)
			}
			blocked, err := store.GetTask(ctx, admission.Task.ID)
			if err != nil {
				t.Fatalf("GetTask() error = %v", err)
			}
			if blocked.Status != "blocked" || blocked.BlockedReason != "approval_required" {
				t.Fatalf("blocked task = %+v, want blocked approval_required", blocked)
			}
			if blocked.ExecutionIntent != tc.wantIntent || blocked.ExecutionIntentSource != "safety_classifier" {
				t.Fatalf("blocked intent = %q/%q, want %s/safety_classifier", blocked.ExecutionIntent, blocked.ExecutionIntentSource, tc.wantIntent)
			}
			approval, err := store.GetLatestTaskApproval(ctx, admission.Task.ID)
			if err != nil {
				t.Fatalf("GetLatestTaskApproval() error = %v", err)
			}
			if approval.Status != "pending" {
				t.Fatalf("approval status = %q, want pending", approval.Status)
			}
		})
	}
}

func createFactoryTestProject(t *testing.T, ctx context.Context, store *sqlite.Store) sqlite.Project {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), "alpha"),
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "registry/projects/alpha.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	return project
}

func loadFactoryExecutorConfig(t *testing.T) executorrouter.Config {
	t.Helper()

	config, err := executorrouter.LoadConfig(filepath.Clean(filepath.Join("..", "..", "..", "config", "executors.yaml")))
	if err != nil {
		t.Fatalf("LoadConfig(executors) error = %v", err)
	}
	return config
}

func openFactoryStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func writeFactoryRegistry(t *testing.T) projects.Registry {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "projects.yaml")
	alphaGitRoot := filepath.Join(root, "alpha")
	odinGitRoot := filepath.Join(root, "odin-core")
	for _, gitRoot := range []string{alphaGitRoot, odinGitRoot} {
		if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir git root: %v", err)
		}
	}
	configYAML := `
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
  - key: alpha
    name: Alpha
    project_class: github_backed_project
    git_root: alpha
    default_branch: main
    github:
      repo: acme/alpha
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
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
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
