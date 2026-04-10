package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/app/backup"
	"odin-os/internal/app/bootstrap"
	"odin-os/internal/app/lifecycle"
	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/learning/evaluator"
	"odin-os/internal/learning/promotion"
	"odin-os/internal/learning/proposals"
	"odin-os/internal/learning/replay"
	"odin-os/internal/migration/extractor"
	"odin-os/internal/registry"
	"odin-os/internal/registry/loader"
	"odin-os/internal/runtime/checkpoints"
	healthsvc "odin-os/internal/runtime/health"
	jobsvc "odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/projections"
	recoverysvc "odin-os/internal/runtime/recovery"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/telemetry/metrics"
	"odin-os/internal/tools/broker"
	"odin-os/internal/tools/budgets"
	"odin-os/internal/tools/catalog"
	"odin-os/internal/vcs/branches"
	gitadapter "odin-os/internal/vcs/git"
	"odin-os/internal/vcs/leases"
	"odin-os/internal/vcs/worktrees"
	worktreemgr "odin-os/internal/vcs/worktrees"
)

func TestAlphaAcceptance(t *testing.T) {
	ctx := context.Background()
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)
	now := time.Date(2026, 4, 9, 23, 0, 0, 0, time.UTC)

	t.Run("repo structure matches canonical layout", func(t *testing.T) {
		required := []string{
			"cmd/odin",
			"internal/app",
			"internal/cli",
			"internal/api",
			"internal/core",
			"internal/runtime",
			"internal/registry",
			"internal/learning",
			"internal/memory",
			"internal/workers",
			"internal/executors",
			"internal/tools",
			"internal/vcs",
			"internal/adapters",
			"internal/store",
			"internal/telemetry",
			"registry/agents",
			"registry/skills",
			"registry/workflows",
			"registry/commands",
			"prompts",
			"memory",
			"config",
			"data",
			"runs",
			"state",
			"docs/adr",
			"docs/contracts",
			"docs/operations",
			"scripts/migrate",
			"scripts/dev",
			"tests/unit",
			"tests/integration",
			"tests/replay",
		}
		for _, relativePath := range required {
			requirePathExists(t, filepath.Join(repoRoot, relativePath))
		}
	})

	t.Run("markdown registry system works", func(t *testing.T) {
		snapshot, err := loader.LoadDir(filepath.Join(repoRoot, "registry"))
		if err != nil {
			t.Fatalf("LoadDir(registry) error = %v", err)
		}
		if len(snapshot.Diagnostics) != 0 {
			t.Fatalf("registry diagnostics = %+v, want none", snapshot.Diagnostics)
		}
		for _, kind := range []registry.Kind{
			registry.KindAgent,
			registry.KindSkill,
			registry.KindWorkflow,
			registry.KindCommand,
		} {
			if len(snapshot.ByKind[kind]) == 0 {
				t.Fatalf("snapshot.ByKind[%s] = 0, want at least one item", kind)
			}
		}
	})

	t.Run("sqlite is canonical runtime authority", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		app, err := bootstrap.Load(ctx, repoRoot, runtimeRoot)
		if err != nil {
			t.Fatalf("bootstrap.Load() error = %v", err)
		}
		defer app.Store.Close()

		jobs := jobsvc.Service{
			Store:    app.Store,
			Registry: app.Registry,
			Now:      func() time.Time { return now },
		}

		task, err := jobs.CreateTaskFromAct(ctx, scope.Resolution{
			Kind:       scope.ScopeOdinCore,
			ProjectKey: "odin-core",
		}, "alpha acceptance runtime authority")
		if err != nil {
			t.Fatalf("CreateTaskFromAct() error = %v", err)
		}

		reopened, err := sqlite.Open(filepath.Join(runtimeRoot, "data", "odin.db"))
		if err != nil {
			t.Fatalf("sqlite.Open(reopen) error = %v", err)
		}
		defer reopened.Close()

		gotTask, err := reopened.GetTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}
		if gotTask.Title != task.Title {
			t.Fatalf("GetTask().Title = %q, want %q", gotTask.Title, task.Title)
		}
	})

	t.Run("fresh runtime becomes ready without manual seeding", func(t *testing.T) {
		runtimeRoot := t.TempDir()

		output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "healthcheck")
		if err != nil {
			t.Fatalf("runOdinCommand(healthcheck fresh runtime) error = %v\n%s", err, output)
		}
		if !strings.Contains(output, "ready") {
			t.Fatalf("fresh runtime healthcheck output = %q, want ready", output)
		}
	})

	t.Run("managed projects support local and github classes", func(t *testing.T) {
		localRepo := createGitRepository(t)
		githubRepo := createGitRepository(t)
		manifestPath := filepath.Join(t.TempDir(), "projects.yaml")
		writeProjectManifest(t, manifestPath, localRepo, githubRepo)

		registry, diagnostics, err := projects.Register(manifestPath)
		if err != nil {
			t.Fatalf("projects.Register() error = %v", err)
		}
		if len(diagnostics) != 0 {
			t.Fatalf("project diagnostics = %+v, want none", diagnostics)
		}

		localProject, ok := registry.Lookup("local-demo")
		if !ok || localProject.ProjectClass != projects.ProjectClassLocalGit {
			t.Fatalf("local-demo = %+v, want local_git_project", localProject)
		}
		gitHubProject, ok := registry.Lookup("github-demo")
		if !ok || gitHubProject.ProjectClass != projects.ProjectClassGitHubBacked || gitHubProject.GitHub.Repo == "" {
			t.Fatalf("github-demo = %+v, want github_backed_project with repo", gitHubProject)
		}
	})

	t.Run("odin-core is handled as a special system project", func(t *testing.T) {
		registry, diagnostics, err := projects.Register(filepath.Join(repoRoot, "config", "projects.yaml"))
		if err != nil {
			t.Fatalf("projects.Register(real) error = %v", err)
		}
		if len(diagnostics) != 0 {
			t.Fatalf("project diagnostics = %+v, want none", diagnostics)
		}

		systemProject, ok := registry.SystemProject()
		if !ok {
			t.Fatal("SystemProject() missing odin-core project")
		}
		if systemProject.Key != "odin-core" || !systemProject.SystemProject || systemProject.ProjectClass != projects.ProjectClassSystem {
			t.Fatalf("SystemProject() = %+v, want odin-core system project", systemProject)
		}

		resolved := scope.Resolve(scope.ResolveInput{
			ExplicitTarget: &scope.Target{
				ProjectKey:    systemProject.Key,
				SystemProject: systemProject.SystemProject,
			},
		})
		if resolved.Kind != scope.ScopeOdinCore {
			t.Fatalf("Resolve(odin-core).Kind = %q, want %q", resolved.Kind, scope.ScopeOdinCore)
		}
	})

	t.Run("cli shell supports ask and act with explicit scope visibility", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "hello there\n/project odin-core\n/mode act\nalpha acceptance cli task\n/quit\n")
		if err != nil {
			t.Fatalf("runOdinCommand(interactive) error = %v\n%s", err, output)
		}
		if !strings.Contains(output, "scope=global mode=ask") {
			t.Fatalf("interactive output missing global ask header: %q", output)
		}
		if strings.Contains(output, "Phase 05") {
			t.Fatalf("interactive output still uses placeholder ask response: %q", output)
		}
		if !strings.Contains(output, "project=odin-core scope=odin-core") {
			t.Fatalf("interactive output missing project switch: %q", output)
		}
		if !strings.Contains(output, "mode=act") {
			t.Fatalf("interactive output missing act mode confirmation: %q", output)
		}
		if !strings.Contains(output, "created task") {
			t.Fatalf("interactive output missing task creation: %q", output)
		}
		if !strings.Contains(output, "run") {
			t.Fatalf("interactive output missing immediate run visibility: %q", output)
		}

		store := openRuntimeStore(t, runtimeRoot)
		defer store.Close()
		views, err := projections.ListTaskStatusViews(ctx, store.DB())
		if err != nil {
			t.Fatalf("ListTaskStatusViews() error = %v", err)
		}
		if len(views) == 0 {
			t.Fatalf("task views = 0, want created task from act mode")
		}
		runViews, err := projections.ListRunSummaryViews(ctx, store.DB())
		if err != nil {
			t.Fatalf("ListRunSummaryViews() error = %v", err)
		}
		if len(runViews) == 0 {
			t.Fatalf("run views = 0, want immediate act run visibility")
		}
	})

	t.Run("explicit operational cli commands expose read-only runtime state", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		store := openRuntimeStore(t, runtimeRoot)
		_, task, run := seedTaskRunFixture(t, ctx, store, "odin-core", string(scope.ScopeOdinCore), "cli-read-task", "read-only cli check", "codex_headless", now)
		if _, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
			TaskID:      task.ID,
			RunID:       &run.ID,
			Status:      "pending",
			RequestedBy: "operator",
		}); err != nil {
			t.Fatalf("RequestApproval() error = %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("Close(runtime store) error = %v", err)
		}

		statusOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "status", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(status --json) error = %v\n%s", err, statusOutput)
		}
		if !strings.Contains(statusOutput, "\"pending_approvals\": 1") {
			t.Fatalf("status output = %q, want pending approval count", statusOutput)
		}

		scopeOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "scope", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(scope --json) error = %v\n%s", err, scopeOutput)
		}
		if !strings.Contains(scopeOutput, "\"scope\": \"global\"") {
			t.Fatalf("scope output = %q, want global scope", scopeOutput)
		}

		projectOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "project", "list")
		if err != nil {
			t.Fatalf("runOdinCommand(project list) error = %v\n%s", err, projectOutput)
		}
		if !strings.Contains(projectOutput, "odin-core") {
			t.Fatalf("project output = %q, want odin-core", projectOutput)
		}

		jobsOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "jobs", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(jobs --json) error = %v\n%s", err, jobsOutput)
		}
		if !strings.Contains(jobsOutput, task.Key) {
			t.Fatalf("jobs output = %q, want task key", jobsOutput)
		}

		runsOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "runs", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(runs --json) error = %v\n%s", err, runsOutput)
		}
		if !strings.Contains(runsOutput, run.Executor) {
			t.Fatalf("runs output = %q, want executor", runsOutput)
		}

		approvalsOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "approvals", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(approvals --json) error = %v\n%s", err, approvalsOutput)
		}
		if !strings.Contains(approvalsOutput, task.Key) {
			t.Fatalf("approvals output = %q, want task key", approvalsOutput)
		}

		logsOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "logs", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(logs --json) error = %v\n%s", err, logsOutput)
		}
		if !strings.Contains(logsOutput, "task.created") {
			t.Fatalf("logs output = %q, want task event", logsOutput)
		}
	})

	t.Run("explicit mutating cli commands can select project transition and run task", func(t *testing.T) {
		runtimeRoot := t.TempDir()

		projectOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "project", "select", "pbs")
		if err != nil {
			t.Fatalf("runOdinCommand(project select pbs) error = %v\n%s", err, projectOutput)
		}
		if !strings.Contains(projectOutput, "project=pbs") {
			t.Fatalf("project output = %q, want pbs selection", projectOutput)
		}

		transitionOutput, err := runOdinCommand(
			t,
			repoRoot,
			odinBinary,
			runtimeRoot,
			nil,
			"",
			"transition",
			"set",
			"limited_action",
			"allow=run_task",
			"confirm",
			"because",
			"acceptance",
			"task",
			"run",
		)
		if err != nil {
			t.Fatalf("runOdinCommand(transition set limited_action) error = %v\n%s", err, transitionOutput)
		}
		if !strings.Contains(transitionOutput, "project=pbs") || !strings.Contains(transitionOutput, "state=limited_action") {
			t.Fatalf("transition output = %q, want pbs limited_action state", transitionOutput)
		}

		cleanupAcceptanceWorktree(t, "/home/orchestrator/pbs", "pbs", 1, 1, 1)

		taskOutput, err := runOdinCommand(
			t,
			repoRoot,
			odinBinary,
			runtimeRoot,
			nil,
			"",
			"task",
			"run",
			"--project",
			"pbs",
			"--title",
			"acceptance cli task run",
			"--json",
		)
		if err != nil {
			t.Fatalf("runOdinCommand(task run --json) error = %v\n%s", err, taskOutput)
		}

		var payload struct {
			Task struct {
				Key    string `json:"key"`
				Status string `json:"status"`
				Scope  string `json:"scope"`
			} `json:"task"`
			Run struct {
				Executor string `json:"executor"`
				Status   string `json:"status"`
			} `json:"run"`
		}
		if err := json.Unmarshal([]byte(taskOutput), &payload); err != nil {
			t.Fatalf("task output json = %v\n%s", err, taskOutput)
		}
		if payload.Task.Key == "" || payload.Task.Status != "completed" || payload.Task.Scope != "project" {
			t.Fatalf("task payload = %+v, want completed project task", payload.Task)
		}
		if payload.Run.Executor == "" || payload.Run.Status != "completed" {
			t.Fatalf("run payload = %+v, want completed run", payload.Run)
		}
	})

	t.Run("executor abstraction supports headless cli and api lanes", func(t *testing.T) {
		cfg, err := executorrouter.LoadConfig(filepath.Join(repoRoot, "config", "executors.yaml"))
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}
		selector := executorrouter.Selector{
			Config:    cfg,
			Executors: executorrouter.DefaultCatalog(),
		}

		cliDecision, err := selector.Select(ctx, contract.TaskSpec{
			ID:     "cli-task",
			Kind:   contract.TaskKindBuild,
			Scope:  "project",
			Prompt: "build the repository",
			Requirements: contract.Requirements{
				AllowedClasses:    []contract.ExecutorClass{contract.ExecutorClassPlanBackedCLI},
				NeedsHeadlessPlan: true,
			},
		})
		if err != nil {
			t.Fatalf("Select(cli) error = %v", err)
		}
		cliConfig, _ := cfg.ExecutorByKey(cliDecision.ExecutorKey)
		if cliConfig.Class != contract.ExecutorClassPlanBackedCLI {
			t.Fatalf("cli executor class = %q, want %q", cliConfig.Class, contract.ExecutorClassPlanBackedCLI)
		}

		apiDecision, err := selector.Select(ctx, contract.TaskSpec{
			ID:     "api-task",
			Kind:   contract.TaskKindGeneral,
			Scope:  "project",
			Prompt: "summarize runtime state",
			Requirements: contract.Requirements{
				AllowedClasses: []contract.ExecutorClass{contract.ExecutorClassAPI},
			},
		})
		if err != nil {
			t.Fatalf("Select(api) error = %v", err)
		}
		apiConfig, _ := cfg.ExecutorByKey(apiDecision.ExecutorKey)
		if apiConfig.Class != contract.ExecutorClassAPI {
			t.Fatalf("api executor class = %q, want %q", apiConfig.Class, contract.ExecutorClassAPI)
		}
	})

	t.Run("dynamic tool loading is working", func(t *testing.T) {
		snapshot, err := loader.LoadDir(filepath.Join(repoRoot, "registry"))
		if err != nil {
			t.Fatalf("LoadDir(registry) error = %v", err)
		}
		suiteBroker := broker.New(snapshot, catalog.BuiltinDefinitions(), budgets.Limits{
			Tool: budgets.Tool{
				MaxSelections:  6,
				MaxInvocations: 4,
				MaxCostUnits:   16,
			},
			Context: budgets.Context{
				MaxExpandedDefinitions: 6,
				MaxCompactedResults:    4,
				MaxCompactedBytes:      4096,
			},
		})

		odinCoreCards := suiteBroker.Catalog("odin-core")
		if !hasCapability(odinCoreCards, "project_status") || !hasCapability(odinCoreCards, "triage-skill") {
			t.Fatalf("odin-core catalog missing expected capabilities: %+v", odinCoreCards)
		}
		projectCards := suiteBroker.Catalog("project")
		if !hasCapability(projectCards, "triage-agent") {
			t.Fatalf("project catalog missing triage-agent: %+v", projectCards)
		}

		toolExpansion, err := suiteBroker.Expand("project_status")
		if err != nil {
			t.Fatalf("Expand(project_status) error = %v", err)
		}
		if toolExpansion.Tool == nil || len(toolExpansion.Tool.Schema) == 0 {
			t.Fatalf("tool expansion = %+v, want tool schema", toolExpansion)
		}

		skillExpansion, err := suiteBroker.Expand("triage-skill")
		if err != nil {
			t.Fatalf("Expand(triage-skill) error = %v", err)
		}
		if skillExpansion.Skill == nil || skillExpansion.Skill.Sections[registry.SectionProcedure] == "" {
			t.Fatalf("skill expansion = %+v, want procedure section", skillExpansion)
		}

		result, err := suiteBroker.InvokeTool("project_status", map[string]string{"project_key": "odin-core"})
		if err != nil {
			t.Fatalf("InvokeTool(project_status) error = %v", err)
		}
		compacted, err := suiteBroker.Compact(result)
		if err != nil {
			t.Fatalf("Compact() error = %v", err)
		}
		if compacted.Bytes <= 0 {
			t.Fatalf("CompactedResult.Bytes = %d, want > 0", compacted.Bytes)
		}
	})

	t.Run("context compaction and wake packets work", func(t *testing.T) {
		store := openTempStore(t)
		defer store.Close()

		project, task, run := seedTaskRunFixture(t, ctx, store, "alpha", "project", "alpha-compaction-task", "Alpha compaction task", "codex_headless", now)
		compaction, err := checkpoints.Service{Store: store}.Compact(ctx, checkpoints.CompactParams{
			TaskID:          task.ID,
			RunID:           &run.ID,
			Trigger:         checkpoints.TriggerApprovalWait,
			CheckpointKey:   "alpha-acceptance",
			Objective:       task.Title,
			TaskStatus:      "blocked",
			BlockingReason:  "waiting for operator approval",
			NextSteps:       []string{"resume after approval"},
			Constraints:     []string{"approval required"},
			ManifestSummary: "managed project",
			PolicySummary:   "alpha acceptance",
			OpenTaskSummary: "one blocked task",
			ApprovalSummary: "approval pending",
		})
		if err != nil {
			t.Fatalf("Compact() error = %v", err)
		}
		if compaction.WakePacket.Trigger != string(checkpoints.TriggerApprovalWait) {
			t.Fatalf("WakePacket.Trigger = %q, want %q", compaction.WakePacket.Trigger, checkpoints.TriggerApprovalWait)
		}

		resumeState, err := checkpoints.Service{Store: store}.LoadResumeState(ctx, project.ID, task.ID)
		if err != nil {
			t.Fatalf("LoadResumeState() error = %v", err)
		}
		if resumeState.WakePacketID != compaction.WakePacket.ID || len(resumeState.NextSteps) == 0 {
			t.Fatalf("ResumeState = %+v, want wake packet and next steps", resumeState)
		}
	})

	t.Run("mutating tasks use isolated worktrees and task-owned branches", func(t *testing.T) {
		store := openTempStore(t)
		defer store.Close()

		repoRootPath := createGitRepository(t)
		project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
			Key:           "alpha",
			Name:          "Alpha",
			Scope:         "project",
			GitRoot:       repoRootPath,
			DefaultBranch: "main",
			ManifestPath:  "config/projects.yaml",
		})
		if err != nil {
			t.Fatalf("CreateProject() error = %v", err)
		}
		task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
			ProjectID:   project.ID,
			Key:         "alpha-worktree-task",
			Title:       "Alpha worktree task",
			Status:      "running",
			Scope:       "project",
			RequestedBy: "operator",
		})
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		run, err := store.StartRun(ctx, sqlite.StartRunParams{
			TaskID:   task.ID,
			Executor: "codex_headless",
			Attempt:  1,
			Status:   "running",
		})
		if err != nil {
			t.Fatalf("StartRun() error = %v", err)
		}

		leaseManager := leases.Manager{
			Store:        store,
			Git:          gitadapter.Adapter{},
			WorktreeRoot: t.TempDir(),
		}

		assignment, err := leaseManager.Prepare(ctx, leases.Request{
			Mutating:      true,
			ProjectID:     project.ID,
			ProjectKey:    project.Key,
			TaskID:        task.ID,
			RunID:         run.ID,
			RepoRoot:      repoRootPath,
			DefaultBranch: "main",
			Try:           1,
		})
		if err != nil {
			t.Fatalf("Prepare() error = %v", err)
		}
		wantBranch := branches.Name(branches.NameParams{
			ProjectKey: project.Key,
			TaskID:     task.ID,
			RunID:      run.ID,
			Try:        1,
		})
		if assignment.BranchName != wantBranch {
			t.Fatalf("BranchName = %q, want %q", assignment.BranchName, wantBranch)
		}
		if assignment.WorktreePath == repoRootPath {
			t.Fatalf("WorktreePath = repo root, want isolated worktree")
		}
		requirePathExists(t, assignment.WorktreePath)

		if assignment.LeaseID == nil {
			t.Fatal("LeaseID = nil, want active mutable lease")
		}
		if _, err := store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
			LeaseID: *assignment.LeaseID,
			State:   "released",
		}); err != nil {
			t.Fatalf("ReleaseWorktreeLease() error = %v", err)
		}

		cleanupResult, err := worktreemgr.Manager{
			Store: store,
			Git:   gitadapter.Adapter{},
		}.Cleanup(ctx, time.Now().Add(time.Minute))
		if err != nil {
			t.Fatalf("Cleanup() error = %v", err)
		}
		if len(cleanupResult.Removed) != 1 {
			t.Fatalf("Cleanup().Removed = %d, want 1", len(cleanupResult.Removed))
		}
	})

	t.Run("observability and doctor surfaces are useful", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		store := openRuntimeStore(t, runtimeRoot)
		defer store.Close()
		seedHealthyObservability(t, ctx, store, now)

		report, err := healthsvc.Service{
			DB:  store.DB(),
			Now: func() time.Time { return now },
		}.Doctor(ctx, true)
		if err != nil {
			t.Fatalf("Doctor() error = %v", err)
		}
		if report.Status != healthsvc.StatusHealthy || len(report.Checks) < 6 {
			t.Fatalf("Doctor() = %+v, want healthy report with checks", report)
		}

		snapshot, err := metrics.Service{DB: store.DB()}.Collect(ctx)
		if err != nil {
			t.Fatalf("Collect() error = %v", err)
		}
		if snapshot.GeneratedAt.IsZero() {
			t.Fatalf("metrics snapshot = %+v, want generated timestamp", snapshot)
		}

		output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "doctor", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(doctor --json) error = %v\n%s", err, output)
		}
		if !strings.Contains(output, "\"status\":") {
			t.Fatalf("doctor output = %q, want JSON status field", output)
		}
	})

	t.Run("self-heal playbooks run and are audited", func(t *testing.T) {
		store := openTempStore(t)
		defer store.Close()
		store.Now = func() time.Time { return now }
		seedHealthyObservability(t, ctx, store, now)
		if _, err := store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
			Surface:     "doctor",
			Status:      "stale",
			DetailsJSON: `{"source":"acceptance"}`,
		}); err != nil {
			t.Fatalf("RecordProjectionFreshness(stale) error = %v", err)
		}
		if _, err := store.DB().ExecContext(ctx, `
			UPDATE projection_freshness
			SET refreshed_at = ?, updated_at = ?
			WHERE surface = 'doctor'
		`, now.Add(-2*time.Hour).Format(time.RFC3339Nano), now.Add(-2*time.Hour).Format(time.RFC3339Nano)); err != nil {
			t.Fatalf("force stale projection error = %v", err)
		}

		result, err := recoverysvc.Service{
			Store:           store,
			RegistryRoot:    filepath.Join(repoRoot, "registry"),
			ExecutorCatalog: executorrouter.DefaultCatalog(),
			HealthConfig:    healthsvc.DefaultConfig(),
			Now:             func() time.Time { return now },
		}.RunCycle(ctx)
		if err != nil {
			t.Fatalf("RunCycle() error = %v", err)
		}
		if len(result.Outcomes) == 0 {
			t.Fatalf("RunCycle().Outcomes = 0, want recovery outcome")
		}

		events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
		if err != nil {
			t.Fatalf("ListEvents() error = %v", err)
		}
		if !hasEventType(events, "recovery.action_executed") {
			t.Fatalf("events missing recovery.action_executed: %+v", events)
		}
	})

	t.Run("migration extraction from odin-orchestrator works", func(t *testing.T) {
		sourceRoot := "/home/orchestrator/odin-orchestrator"
		requirePathExists(t, sourceRoot)

		docsRoot := filepath.Join(t.TempDir(), "docs")
		stateRoot := filepath.Join(t.TempDir(), "state")
		result, err := extractor.Service{}.Run(extractor.Options{
			SourceRoot: sourceRoot,
			DocsRoot:   docsRoot,
			StateRoot:  stateRoot,
		})
		if err != nil {
			t.Fatalf("extractor.Run() error = %v", err)
		}
		if len(result.Inventory.Candidates) == 0 {
			t.Fatalf("extractor inventory = 0 candidates, want useful migration inventory")
		}
		requirePathExists(t, result.InventoryPath)
		requirePathExists(t, result.InventoryReportPath)
		requirePathExists(t, result.DuplicateReportPath)
	})

	t.Run("projects onboard in shadow mode and limited-action mode", func(t *testing.T) {
		shadowDecision := projects.AuthorizeTransitionAction(projects.TransitionAuthRequest{
			Transition: projects.RuntimeTransition{
				State:      projects.TransitionStateShadow,
				Controller: projects.TransitionControllerLegacyOdin,
			},
			Actor:       projects.TransitionControllerLegacyOdin,
			ActionClass: projects.ActionClassIsolatedMutation,
			ActionKey:   "prepare_worktree",
		})
		if shadowDecision.Allowed {
			t.Fatalf("shadow mutation decision = %+v, want denied", shadowDecision)
		}

		limitedAllowed := projects.AuthorizeTransitionAction(projects.TransitionAuthRequest{
			Transition: projects.RuntimeTransition{
				State:          projects.TransitionStateLimitedAction,
				Controller:     projects.TransitionControllerOdinOS,
				LimitedActions: []string{"prepare_worktree"},
			},
			Actor:       projects.TransitionControllerOdinOS,
			ActionClass: projects.ActionClassIsolatedMutation,
			ActionKey:   "prepare_worktree",
		})
		if !limitedAllowed.Allowed {
			t.Fatalf("limited_action allowlisted mutation = %+v, want allowed", limitedAllowed)
		}

		limitedBlocked := projects.AuthorizeTransitionAction(projects.TransitionAuthRequest{
			Transition: projects.RuntimeTransition{
				State:          projects.TransitionStateLimitedAction,
				Controller:     projects.TransitionControllerOdinOS,
				LimitedActions: []string{"prepare_worktree"},
			},
			Actor:       projects.TransitionControllerOdinOS,
			ActionClass: projects.ActionClassIsolatedMutation,
			ActionKey:   "merge_default_branch",
		})
		if limitedBlocked.Allowed {
			t.Fatalf("limited_action unallowlisted mutation = %+v, want denied", limitedBlocked)
		}
	})

	t.Run("self-improvement exists only through proposals evaluation and promotion", func(t *testing.T) {
		store := openTempStore(t)
		defer store.Close()

		proposalService := proposals.Service{Store: store}
		promotionService := promotion.Service{
			Store:     store,
			Evaluator: evaluator.Service{ApprovalThreshold: 0},
		}

		proposal, err := proposalService.Create(ctx, proposals.CreateInput{
			ProposalType:      "routing_rule_refinement",
			Scope:             "global",
			TargetKey:         "default",
			Summary:           "Prefer the cheaper healthy API route",
			Hypothesis:        "routing should improve",
			ChangePayloadJSON: `{"preferred":["openai_api"]}`,
			CreatedBy:         "operator",
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		proposal, err = proposalService.Submit(ctx, proposal.ID)
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}

		if _, err := promotionService.Promote(ctx, proposal.ID, "operator"); err == nil {
			t.Fatal("Promote() before evaluation succeeded, want rejection")
		}

		_, proposal, err = promotionService.Evaluate(ctx, proposal.ID, replay.Fixture{
			Key:  "alpha-acceptance",
			Mode: replay.ModeReplay,
			Baseline: replay.Metrics{
				SuccessRate:           0.70,
				Cost:                  10,
				LatencyMS:             1000,
				PolicyViolations:      1,
				OperatorInterventions: 3,
			},
			Candidate: replay.Metrics{
				SuccessRate:           0.90,
				Cost:                  5,
				LatencyMS:             500,
				PolicyViolations:      0,
				OperatorInterventions: 1,
			},
		})
		if err != nil {
			t.Fatalf("Evaluate() error = %v", err)
		}
		if proposal.Status != "approved" {
			t.Fatalf("proposal status after evaluation = %q, want approved", proposal.Status)
		}

		proposal, err = proposalService.ApprovePromotion(ctx, proposal.ID)
		if err != nil {
			t.Fatalf("ApprovePromotion() error = %v", err)
		}

		activePromotion, err := promotionService.Promote(ctx, proposal.ID, "operator")
		if err != nil {
			t.Fatalf("Promote() error = %v", err)
		}

		activePromotions, err := promotionService.ListActive(ctx)
		if err != nil {
			t.Fatalf("ListActive() error = %v", err)
		}
		if len(activePromotions) != 1 || activePromotions[0].ID != activePromotion.ID {
			t.Fatalf("active promotions = %+v, want promoted record", activePromotions)
		}

		rolledBack, err := promotionService.Rollback(ctx, activePromotion.ID, "operator", "alpha acceptance rollback")
		if err != nil {
			t.Fatalf("Rollback() error = %v", err)
		}
		if rolledBack.Status != "rolled_back" {
			t.Fatalf("Rollback().Status = %q, want rolled_back", rolledBack.Status)
		}
	})

	t.Run("system runs on the homelab with restart safety and backups", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		store := openRuntimeStore(t, runtimeRoot)
		store.Now = func() time.Time { return now }
		project, task, run := seedTaskRunFixture(t, ctx, store, "alpha", "project", "alpha-homelab-task", "Alpha homelab task", "codex_headless", now)
		seedHealthyObservability(t, ctx, store, now)

		t.Setenv("ODIN_ROOT", runtimeRoot)
		t.Setenv("ODIN_HTTP_ADDR", "127.0.0.1:0")

		ctxServe, cancel := context.WithCancel(ctx)
		time.AfterFunc(150*time.Millisecond, cancel)
		var serveOutput bytes.Buffer
		err := lifecycle.Run(ctxServe, repoRoot, []string{"serve"}, strings.NewReader(""), &serveOutput)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("lifecycle.Run(serve) error = %v", err)
		}

		gotRun, err := store.GetRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
		if gotRun.Status != "interrupted" {
			t.Fatalf("GetRun().Status = %q, want interrupted", gotRun.Status)
		}

		packet, err := store.GetLatestTaskWakePacket(ctx, project.ID, task.ID)
		if err != nil {
			t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
		}
		if packet.Trigger != string(checkpoints.TriggerRestart) {
			t.Fatalf("WakePacket.Trigger = %q, want restart", packet.Trigger)
		}

		healthcheckOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "healthcheck")
		if err != nil {
			t.Fatalf("runOdinCommand(healthcheck) error = %v\n%s", err, healthcheckOutput)
		}
		if !strings.Contains(healthcheckOutput, "ready") {
			t.Fatalf("healthcheck output = %q, want ready", healthcheckOutput)
		}

		archivePath := filepath.Join(t.TempDir(), "odin-alpha-backup.tar.gz")
		restoreRoot := filepath.Join(t.TempDir(), "restore")
		backupService := backup.Service{
			RepoRoot:    repoRoot,
			RuntimeRoot: runtimeRoot,
		}
		if err := backupService.CreateArchive(ctx, archivePath); err != nil {
			t.Fatalf("CreateArchive() error = %v", err)
		}
		if err := backupService.VerifyArchive(ctx, archivePath); err != nil {
			t.Fatalf("VerifyArchive() error = %v", err)
		}
		if err := backupService.RestoreArchive(ctx, archivePath, restoreRoot); err != nil {
			t.Fatalf("RestoreArchive() error = %v", err)
		}
		requirePathExists(t, filepath.Join(restoreRoot, "data", "odin.db"))
	})
}

func cleanupAcceptanceWorktree(t *testing.T, repoRoot string, projectKey string, taskID int64, runID int64, attempt int) {
	t.Helper()

	path := worktrees.ResolvePath(worktrees.PathParams{
		Root:       worktrees.DefaultRoot(),
		ProjectKey: projectKey,
		TaskID:     taskID,
		RunID:      runID,
		Try:        attempt,
	})
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("RemoveAll(%s) error = %v", path, err)
	}

	command := exec.Command("git", "-C", repoRoot, "worktree", "prune")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree prune: %v\n%s", err, output)
	}
}
