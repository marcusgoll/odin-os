package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"odin-os/internal/app/backup"
	"odin-os/internal/app/bootstrap"
	"odin-os/internal/cli/scope"
	approvals "odin-os/internal/core/approvals"
	coremedia "odin-os/internal/core/media"
	"odin-os/internal/core/projects"
	"odin-os/internal/core/workspaces"
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
	mediasvc "odin-os/internal/runtime/media"
	"odin-os/internal/runtime/projections"
	recoverysvc "odin-os/internal/runtime/recovery"
	supervisionsvc "odin-os/internal/runtime/supervision"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/telemetry/metrics"
	"odin-os/internal/tools/broker"
	"odin-os/internal/tools/budgets"
	"odin-os/internal/tools/catalog"
	"odin-os/internal/vcs/branches"
	gitadapter "odin-os/internal/vcs/git"
	"odin-os/internal/vcs/leases"
	worktreemgr "odin-os/internal/vcs/worktrees"
)

func TestMediaMaintenancePreflightDependsOnVerifiedBackup(t *testing.T) {
	ctx := context.Background()
	repoRoot := projectRoot(t)
	runtimeRoot := t.TempDir()
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)

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
	}, "media maintenance preflight")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	backupService := backup.Service{
		RepoRoot:    repoRoot,
		RuntimeRoot: runtimeRoot,
	}
	archivePath := filepath.Join(t.TempDir(), "odin-media-backup.tar.gz")
	if err := backupService.CreateArchive(ctx, archivePath); err != nil {
		t.Fatalf("CreateArchive() error = %v", err)
	}
	if err := backupService.VerifyArchive(ctx, archivePath); err != nil {
		t.Fatalf("VerifyArchive() error = %v", err)
	}

	decision := approvals.Service{}.Evaluate(coremedia.Config{
		Policies: coremedia.Policies{
			ApprovalRequired: []string{"restart_plex"},
		},
	}, "restart_plex")
	if !decision.RequiresApproval {
		t.Fatalf("RequiresApproval = false, want true")
	}

	result, err := mediasvc.MaintenanceService{
		Store:       app.Store,
		Config:      &coremedia.Config{Enabled: true, Policies: coremedia.Policies{ApprovalRequired: []string{"restart_plex"}}},
		RuntimeRoot: runtimeRoot,
		Now:         func() time.Time { return now },
	}.Preflight(ctx, mediasvc.PreflightRequest{
		TaskID: &task.ID,
		Action: "restart_plex",
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if result.BlockedReason != "" {
		t.Fatalf("BlockedReason = %q, want empty after verified backup", result.BlockedReason)
	}
	if !result.RequiresApproval {
		t.Fatalf("RequiresApproval = false, want true")
	}
	if result.ApprovalID == nil {
		t.Fatalf("ApprovalID = nil, want pending approval")
	}
	if result.EvidencePacketID == nil {
		t.Fatalf("EvidencePacketID = nil, want preflight evidence packet")
	}
}

func TestAlphaAcceptance(t *testing.T) {
	ctx := context.Background()
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)
	now := time.Now().UTC()

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

	t.Run("fresh runtime stays not ready until the daemon marks it ready", func(t *testing.T) {
		runtimeRoot := t.TempDir()

		output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "healthcheck")
		if err == nil {
			t.Fatalf("runOdinCommand(healthcheck fresh runtime) error = nil, want readiness failure\n%s", output)
		}
		if !strings.Contains(output, "runtime not ready") {
			t.Fatalf("fresh runtime healthcheck output = %q, want runtime-not-ready message", output)
		}
	})

	t.Run("healthcheck fails closed after ungraceful daemon death", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		store := openRuntimeStore(t, runtimeRoot)
		store.Now = func() time.Time { return now }
		seedHealthyObservability(t, ctx, store, now)
		driverEnv := acceptanceHarnessDriverEnv(t)

		var serveOutput bytes.Buffer
		cmd := exec.Command(odinBinary, "serve")
		cmd.Dir = repoRoot
		cmd.Env = append([]string{}, os.Environ()...)
		cmd.Env = append(cmd.Env, "ODIN_ROOT="+runtimeRoot, "ODIN_HTTP_ADDR=127.0.0.1:0")
		for key, value := range driverEnv {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
		cmd.Stdout = &serveOutput
		cmd.Stderr = &serveOutput
		if err := cmd.Start(); err != nil {
			t.Fatalf("cmd.Start(serve) error = %v", err)
		}
		t.Cleanup(func() {
			if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
				return
			}
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		})

		deadline := time.Now().Add(15 * time.Second)
		lastHealthcheckOutput := ""
		for {
			output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "healthcheck")
			lastHealthcheckOutput = output
			if err == nil && strings.Contains(output, "ready") {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("serve never became ready\nserve output:\n%s\nlast healthcheck output:\n%s", serveOutput.String(), lastHealthcheckOutput)
			}
			time.Sleep(100 * time.Millisecond)
		}

		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("Process.Kill() error = %v", err)
		}
		_ = cmd.Wait()

		output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "healthcheck")
		if err == nil {
			t.Fatalf("runOdinCommand(healthcheck after kill -9) error = nil, want readiness failure\n%s", output)
		}
		if !strings.Contains(output, "not ready:") {
			t.Fatalf("healthcheck output = %q, want not ready message", output)
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
		output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "what is my scope?\n/project odin-core\n/mode act\nalpha acceptance cli task\n/quit\n", "repl")
		if err != nil {
			t.Fatalf("runOdinCommand(interactive) error = %v\n%s", err, output)
		}
		if !strings.Contains(output, "scope=global mode=ask") {
			t.Fatalf("interactive output missing global ask header: %q", output)
		}
		if !strings.Contains(output, "scope=global") {
			t.Fatalf("interactive output missing ask scope response: %q", output)
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

		store := openRuntimeStore(t, runtimeRoot)
		defer store.Close()
		views, err := projections.ListTaskStatusViews(ctx, store.DB())
		if err != nil {
			t.Fatalf("ListTaskStatusViews() error = %v", err)
		}
		if len(views) == 0 {
			t.Fatalf("task views = 0, want created task from act mode")
		}
	})

	t.Run("sandcastle headless fixture runs through task run and shell readback", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		commandHome := t.TempDir()
		projectRepo := createGitRepository(t)
		odinRoot := writeSandcastleSpikeOdinRoot(t, repoRoot, projectRepo)
		driverPath := filepath.Join(repoRoot, "scripts", "drivers", "sandcastle-headless-fixture.sh")
		env := map[string]string{
			"HOME":                   commandHome,
			"ODIN_SANDCASTLE_DRIVER": driverPath,
		}

		output, err := runOdinCommandInDir(t, odinRoot, odinBinary, runtimeRoot, env, "", "project", "select", "sandcastle-demo")
		if err != nil {
			t.Fatalf("runOdinCommand(project select) error = %v\n%s", err, output)
		}
		if !strings.Contains(output, "project=sandcastle-demo scope=sandcastle-demo") {
			t.Fatalf("project select output = %q, want sandcastle project scope", output)
		}

		output, err = runOdinCommandInDir(t, odinRoot, odinBinary, runtimeRoot, env, "", "transition", "set", "limited_action", "allow=run_task", "confirm", "because", "sandcastle", "fixture", "e2e")
		if err != nil {
			t.Fatalf("runOdinCommand(transition set) error = %v\n%s", err, output)
		}
		if !strings.Contains(output, "project=sandcastle-demo state=limited_action") {
			t.Fatalf("transition output = %q, want limited_action state", output)
		}

		runOutput, err := runOdinCommandInDir(t, odinRoot, odinBinary, runtimeRoot, env, "", "task", "run", "--project", "sandcastle-demo", "--title", "fix sandcastle fixture task", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(task run) error = %v\n%s", err, runOutput)
		}
		var runView struct {
			Task struct {
				ID     int64  `json:"id"`
				Key    string `json:"key"`
				Status string `json:"status"`
			} `json:"task"`
			Run *struct {
				ID       int64  `json:"id"`
				Executor string `json:"executor"`
				Status   string `json:"status"`
				Summary  string `json:"summary"`
			} `json:"run"`
		}
		if err := json.Unmarshal([]byte(runOutput), &runView); err != nil {
			t.Fatalf("task run JSON decode error = %v\n%s", err, runOutput)
		}
		if runView.Run == nil {
			t.Fatalf("task run JSON = %+v, want run result", runView)
		}
		if runView.Task.Status != "completed" || runView.Run.Status != "completed" || runView.Run.Executor != "sandcastle_headless" {
			t.Fatalf("task run JSON = %+v, want completed sandcastle_headless run", runView)
		}

		readbackInput := "/project sandcastle-demo\n/runs\n/runs show " + strconv.FormatInt(runView.Run.ID, 10) + "\n/quit\n"
		readback, err := runOdinCommandInDir(t, odinRoot, odinBinary, runtimeRoot, env, readbackInput, "repl")
		if err != nil {
			t.Fatalf("runOdinCommand(repl readback) error = %v\n%s", err, readback)
		}
		for _, want := range []string{
			"project=sandcastle-demo scope=sandcastle-demo",
			runView.Task.Key + " sandcastle_headless completed",
			"status=completed executor=sandcastle_headless",
			"artifact=executor_evidence summary=executor evidence",
			"executor_lane=sandcastle_headless",
			"driver_kind=fixture",
			"operation=run",
			"external_id=sandcastle-fixture:",
			"repo_root=" + projectRepo,
			"marker_path=.odin/sandcastle-fixture-marker.json",
			"marker_written=true",
		} {
			if !strings.Contains(readback, want) {
				t.Fatalf("readback missing %q:\n%s", want, readback)
			}
		}

		worktreePath := firstLineValue(readback, "worktree_path")
		branchName := firstLineValue(readback, "branch_name")
		driverCWD := firstLineValue(readback, "driver_cwd")
		branchObserved := firstLineValue(readback, "branch_observed")
		if worktreePath == "" || branchName == "" || driverCWD == "" || branchObserved == "" {
			t.Fatalf("readback missing worktree or branch evidence:\n%s", readback)
		}
		if filepath.Clean(driverCWD) != filepath.Clean(worktreePath) {
			t.Fatalf("driver_cwd = %q, want worktree_path %q", driverCWD, worktreePath)
		}
		if branchObserved != branchName {
			t.Fatalf("branch_observed = %q, want branch_name %q", branchObserved, branchName)
		}

		markerPath := filepath.Join(worktreePath, ".odin", "sandcastle-fixture-marker.json")
		if _, err := os.Stat(markerPath); err != nil {
			t.Fatalf("marker path %s missing: %v", markerPath, err)
		}
		if _, err := os.Stat(filepath.Join(projectRepo, ".odin", "sandcastle-fixture-marker.json")); err == nil {
			t.Fatalf("marker was written to source repo root %s, want leased worktree only", projectRepo)
		} else if !os.IsNotExist(err) {
			t.Fatalf("source repo marker stat error = %v", err)
		}

		statusCmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain=v1", "--", ".odin/sandcastle-fixture-marker.json")
		statusOutput, err := statusCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git status marker error = %v\n%s", err, string(statusOutput))
		}
		if !strings.Contains(string(statusOutput), "?? ") {
			t.Fatalf("git status marker output = %q, want uncommitted marker evidence", string(statusOutput))
		}

		store := openRuntimeStore(t, runtimeRoot)
		defer store.Close()
		task, err := store.GetTask(ctx, runView.Task.ID)
		if err != nil {
			t.Fatalf("GetTask(%d) error = %v", runView.Task.ID, err)
		}
		if task.Status != "completed" {
			t.Fatalf("task status = %q, want completed", task.Status)
		}
		run, err := store.GetRun(ctx, runView.Run.ID)
		if err != nil {
			t.Fatalf("GetRun(%d) error = %v", runView.Run.ID, err)
		}
		if run.Executor != "sandcastle_headless" || run.Status != "completed" {
			t.Fatalf("run = %+v, want completed sandcastle_headless", run)
		}
		artifacts, err := store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: run.ID})
		if err != nil {
			t.Fatalf("ListRunArtifacts(%d) error = %v", run.ID, err)
		}
		if len(artifacts) != 1 || artifacts[0].ArtifactType != "executor_evidence" {
			t.Fatalf("artifacts = %+v, want one executor_evidence artifact", artifacts)
		}
	})

	t.Run("executor abstraction supports headless cli and api lanes", func(t *testing.T) {
		for key, value := range acceptanceHarnessDriverEnv(t) {
			t.Setenv(key, value)
		}
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
		if !hasCapability(odinCoreCards, "task_list") || !hasCapability(odinCoreCards, "triage-skill") {
			t.Fatalf("odin-core catalog missing expected capabilities: %+v", odinCoreCards)
		}
		projectCards := suiteBroker.Catalog("project")
		if !hasCapability(projectCards, "triage-agent") {
			t.Fatalf("project catalog missing triage-agent: %+v", projectCards)
		}

		toolExpansion, err := suiteBroker.Expand("task_list")
		if err != nil {
			t.Fatalf("Expand(task_list) error = %v", err)
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

		result, err := suiteBroker.InvokeTool("task_list", map[string]string{"scope": "odin-core"})
		if err != nil {
			t.Fatalf("InvokeTool(task_list) error = %v", err)
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
			Store:        store,
			Git:          gitadapter.Adapter{},
			WorktreeRoot: leaseManager.WorktreeRoot,
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

		snapshot, err := metrics.Service{
			DB:  store.DB(),
			Now: func() time.Time { return now },
		}.Collect(ctx)
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

	t.Run("blocked work and recoveries stay visible to operators", func(t *testing.T) {
		store := openTempStore(t)
		defer store.Close()
		store.Now = func() time.Time { return now }

		project, task, run := seedTaskRunFixture(t, ctx, store, "alpha", "project", "alpha-blocked-task", "Blocked alpha task", "codex_headless", now)
		if _, err := store.BlockTask(ctx, sqlite.BlockTaskParams{
			TaskID: task.ID,
			Reason: "approval_required",
		}); err != nil {
			t.Fatalf("BlockTask() error = %v", err)
		}
		if _, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
			TaskID:      task.ID,
			RunID:       &run.ID,
			Status:      "pending",
			RequestedBy: "system",
		}); err != nil {
			t.Fatalf("RequestApproval() error = %v", err)
		}
		if _, err := store.CreateContextPacket(ctx, sqlite.CreateContextPacketParams{
			TaskID:        &task.ID,
			RunID:         &run.ID,
			PacketKind:    "wake",
			PacketScope:   "task_wake_packet",
			Trigger:       "approval_wait",
			CheckpointKey: "blocked-approval-1",
			Status:        "active",
			Summary:       "waiting on approval",
			PayloadJSON:   `{"blocking_reason":"approval_required","next_steps":["resume once approved"]}`,
		}); err != nil {
			t.Fatalf("CreateContextPacket() error = %v", err)
		}

		incident, err := store.OpenIncident(ctx, sqlite.OpenIncidentParams{
			RunID:       &run.ID,
			Severity:    "warning",
			Status:      "open",
			Summary:     "executor degraded",
			DetailsJSON: `{"stage":"acceptance"}`,
		})
		if err != nil {
			t.Fatalf("OpenIncident() error = %v", err)
		}
		recovery, err := store.StartRecovery(ctx, sqlite.StartRecoveryParams{
			IncidentID:  &incident.ID,
			RunID:       &run.ID,
			Status:      "running",
			Strategy:    "retry-once",
			DetailsJSON: `{"attempt":1}`,
		})
		if err != nil {
			t.Fatalf("StartRecovery() error = %v", err)
		}

		taskViews, err := projections.ListTaskStatusViews(ctx, store.DB())
		if err != nil {
			t.Fatalf("ListTaskStatusViews() error = %v", err)
		}
		var blockedTaskView *projections.TaskStatusView
		for index := range taskViews {
			if taskViews[index].TaskID == task.ID {
				blockedTaskView = &taskViews[index]
				break
			}
		}
		if blockedTaskView == nil {
			t.Fatalf("task status views missing task %d", task.ID)
		}
		if blockedTaskView.BlockedReason != "approval_required" {
			t.Fatalf("BlockedReason = %q, want approval_required", blockedTaskView.BlockedReason)
		}

		blockedItems, err := projections.ListBlockedItemViews(ctx, store.DB())
		if err != nil {
			t.Fatalf("ListBlockedItemViews() error = %v", err)
		}
		var sawBlockedTask bool
		for _, item := range blockedItems {
			if item.TaskID == task.ID && item.ProjectKey == project.Key {
				sawBlockedTask = true
				break
			}
		}
		if !sawBlockedTask {
			t.Fatalf("blocked items missing task %d: %+v", task.ID, blockedItems)
		}

		recoveryViews, err := projections.ListRecoveryViews(ctx, store.DB())
		if err != nil {
			t.Fatalf("ListRecoveryViews() error = %v", err)
		}
		var sawRecovery bool
		for _, view := range recoveryViews {
			if view.RecoveryID == recovery.ID && view.RunID == run.ID {
				sawRecovery = true
				break
			}
		}
		if !sawRecovery {
			t.Fatalf("recovery views missing recovery %d for run %d: %+v", recovery.ID, run.ID, recoveryViews)
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
		sourceRoot := legacyOrchestratorSourceRoot(t)
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
		driverEnv := acceptanceHarnessDriverEnv(t)

		t.Setenv("ODIN_ROOT", runtimeRoot)
		t.Setenv("ODIN_HTTP_ADDR", "127.0.0.1:0")

		var serveOutput bytes.Buffer
		cmd := exec.Command(odinBinary, "serve")
		cmd.Dir = repoRoot
		cmd.Env = append([]string{}, os.Environ()...)
		cmd.Env = append(cmd.Env, "ODIN_ROOT="+runtimeRoot, "ODIN_HTTP_ADDR=127.0.0.1:0")
		for key, value := range driverEnv {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
		cmd.Stdout = &serveOutput
		cmd.Stderr = &serveOutput
		if err := cmd.Start(); err != nil {
			t.Fatalf("cmd.Start(serve) error = %v", err)
		}
		t.Cleanup(func() {
			if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
				return
			}
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		})

		deadline := time.Now().Add(15 * time.Second)
		lastHealthcheckOutput := ""
		for {
			output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "healthcheck")
			lastHealthcheckOutput = output
			if err == nil && strings.Contains(output, "ready") {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("serve never became ready\nserve output:\n%s\nlast healthcheck output:\n%s", serveOutput.String(), lastHealthcheckOutput)
			}
			time.Sleep(100 * time.Millisecond)
		}

		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatalf("Signal(SIGTERM) error = %v", err)
		}
		if err := cmd.Wait(); err != nil {
			t.Fatalf("cmd.Wait() error = %v\n%s", err, serveOutput.String())
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

		state, err := store.GetRuntimeState(ctx)
		if err != nil {
			t.Fatalf("GetRuntimeState() error = %v", err)
		}
		if state.Status != "stopped" {
			t.Fatalf("RuntimeState.Status = %q, want stopped", state.Status)
		}

		healthcheckOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "healthcheck")
		if err == nil {
			t.Fatalf("runOdinCommand(healthcheck) error = nil, want readiness failure after daemon stop\n%s", healthcheckOutput)
		}
		if !strings.Contains(healthcheckOutput, "not ready:") {
			t.Fatalf("healthcheck output = %q, want not ready message", healthcheckOutput)
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

	t.Run("real binary surfaces companion swarm state in status and agenda", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		store := openRuntimeStore(t, runtimeRoot)
		store.Now = func() time.Time { return now }
		seedAlphaAcceptanceCompanionSwarms(t, ctx, store, now)
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		statusOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, map[string]string{
			"ODIN_NOW": now.Format(time.RFC3339Nano),
		}, "", "status", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(status --json) error = %v\n%s", err, statusOutput)
		}

		var statusReport struct {
			CompanionSwarmCounts struct {
				Active  int `json:"active"`
				Blocked int `json:"blocked"`
				Backlog int `json:"backlog"`
			} `json:"companion_swarm_counts"`
			CompanionSwarms []projections.CompanionSwarmView `json:"companion_swarms"`
		}
		if err := json.Unmarshal([]byte(statusOutput), &statusReport); err != nil {
			t.Fatalf("json.Unmarshal(status output) error = %v\n%s", err, statusOutput)
		}
		if len(statusReport.CompanionSwarms) != 4 {
			t.Fatalf("status companion_swarms len = %d, want 4", len(statusReport.CompanionSwarms))
		}
		if statusReport.CompanionSwarmCounts.Active != 2 || statusReport.CompanionSwarmCounts.Blocked != 2 || statusReport.CompanionSwarmCounts.Backlog != 7 {
			t.Fatalf("status companion_swarm_counts = %+v, want active=2 blocked=2 backlog=7", statusReport.CompanionSwarmCounts)
		}

		gotByTaskKey := make(map[string]projections.CompanionSwarmView, len(statusReport.CompanionSwarms))
		for _, swarm := range statusReport.CompanionSwarms {
			gotByTaskKey[swarm.ParentTaskKey] = swarm
		}
		if gotByTaskKey["alpha-active-swarm"].Status != "queued" {
			t.Fatalf("active swarm status = %q, want queued", gotByTaskKey["alpha-active-swarm"].Status)
		}
		if gotByTaskKey["alpha-completed-running-swarm"].Status != "running" {
			t.Fatalf("completed-running swarm status = %q, want running", gotByTaskKey["alpha-completed-running-swarm"].Status)
		}
		if gotByTaskKey["alpha-completed-running-swarm"].ActiveChildRunCount != 1 {
			t.Fatalf("completed-running swarm active_child_run_count = %d, want 1", gotByTaskKey["alpha-completed-running-swarm"].ActiveChildRunCount)
		}
		if gotByTaskKey["alpha-approval-swarm"].BlockedReason != "approval_required" {
			t.Fatalf("approval swarm blocked_reason = %q, want approval_required", gotByTaskKey["alpha-approval-swarm"].BlockedReason)
		}
		if gotByTaskKey["alpha-budget-swarm"].BlockedReason != "budget_exhausted" {
			t.Fatalf("budget swarm blocked_reason = %q, want budget_exhausted", gotByTaskKey["alpha-budget-swarm"].BlockedReason)
		}

		agendaOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, map[string]string{
			"ODIN_NOW": now.Format(time.RFC3339Nano),
		}, "", "agenda", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(agenda --json) error = %v\n%s", err, agendaOutput)
		}

		var agenda projections.AgendaView
		if err := json.Unmarshal([]byte(agendaOutput), &agenda); err != nil {
			t.Fatalf("json.Unmarshal(agenda output) error = %v\n%s", err, agendaOutput)
		}
		if len(agenda.CompanionSwarms) != 4 {
			t.Fatalf("agenda companion_swarms len = %d, want 4", len(agenda.CompanionSwarms))
		}
		agendaByTaskKey := make(map[string]projections.CompanionSwarmView, len(agenda.CompanionSwarms))
		for _, swarm := range agenda.CompanionSwarms {
			agendaByTaskKey[swarm.ParentTaskKey] = swarm
		}
		if agendaByTaskKey["alpha-active-swarm"].Status != "queued" {
			t.Fatalf("agenda active swarm status = %q, want queued", agendaByTaskKey["alpha-active-swarm"].Status)
		}
		if agendaByTaskKey["alpha-completed-running-swarm"].Status != "running" {
			t.Fatalf("agenda completed-running swarm status = %q, want running", agendaByTaskKey["alpha-completed-running-swarm"].Status)
		}
		if agendaByTaskKey["alpha-approval-swarm"].BlockedReason != "approval_required" {
			t.Fatalf("agenda approval swarm blocked_reason = %q, want approval_required", agendaByTaskKey["alpha-approval-swarm"].BlockedReason)
		}
		if agendaByTaskKey["alpha-budget-swarm"].BlockedReason != "budget_exhausted" {
			t.Fatalf("agenda budget swarm blocked_reason = %q, want budget_exhausted", agendaByTaskKey["alpha-budget-swarm"].BlockedReason)
		}
	})

	t.Run("scheduler reconciliation applies swarm stop conditions to child work", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		store := openRuntimeStore(t, runtimeRoot)
		store.Now = func() time.Time { return now }
		seedAlphaAcceptanceCompanionSwarms(t, ctx, store, now)
		defer store.Close()

		scheduler := supervisionsvc.Service{
			Store: store,
			Jobs:  jobsvc.Service{Store: store, Now: func() time.Time { return now }},
			Now:   func() time.Time { return now },
		}
		result, err := scheduler.Tick(ctx)
		if err != nil {
			t.Fatalf("Tick() error = %v", err)
		}
		if result.Reconciled < 2 {
			t.Fatalf("Tick().Reconciled = %d, want at least 2 stop-condition updates", result.Reconciled)
		}

		lookupTaskID := func(taskKey string) int64 {
			t.Helper()
			var taskID int64
			if err := store.DB().QueryRowContext(ctx, `SELECT id FROM tasks WHERE key = ?`, taskKey).Scan(&taskID); err != nil {
				t.Fatalf("lookup task %s error = %v", taskKey, err)
			}
			return taskID
		}

		assertDelegatedChildren := func(parentTaskKey, wantStatus, wantReason string) {
			t.Helper()
			parentTaskID := lookupTaskID(parentTaskKey)
			delegations, err := store.ListDelegations(ctx, sqlite.ListDelegationsParams{
				ParentTaskID: &parentTaskID,
			})
			if err != nil {
				t.Fatalf("ListDelegations(%s) error = %v", parentTaskKey, err)
			}
			if len(delegations) == 0 {
				t.Fatalf("delegations for %s = 0, want child work", parentTaskKey)
			}
			for _, delegation := range delegations {
				if delegation.ChildTaskID == nil {
					t.Fatalf("delegation %d child_task_id = nil, want child task", delegation.ID)
				}
				childTask, err := store.GetTask(ctx, *delegation.ChildTaskID)
				if err != nil {
					t.Fatalf("GetTask(child %d) error = %v", *delegation.ChildTaskID, err)
				}
				if childTask.Status != wantStatus {
					t.Fatalf("%s child task %d status = %q, want %q", parentTaskKey, childTask.ID, childTask.Status, wantStatus)
				}
				switch wantStatus {
				case "blocked":
					if childTask.BlockedReason != wantReason {
						t.Fatalf("%s child task %d blocked_reason = %q, want %q", parentTaskKey, childTask.ID, childTask.BlockedReason, wantReason)
					}
				case "failed":
					if childTask.TerminalReason != wantReason {
						t.Fatalf("%s child task %d terminal_reason = %q, want %q", parentTaskKey, childTask.ID, childTask.TerminalReason, wantReason)
					}
				}
			}
		}

		assertDelegatedChildren("alpha-approval-swarm", "blocked", "approval_required")
		assertDelegatedChildren("alpha-budget-swarm", "failed", "swarm_budget_exhausted")
	})
}

func seedAlphaAcceptanceCompanionSwarms(t *testing.T, ctx context.Context, store *sqlite.Store, now time.Time) {
	t.Helper()

	workspace, err := workspaces.Service{Store: store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	projectRoot := filepath.Join(t.TempDir(), "alpha-swarm-project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha-swarm-project",
		Name:          "Alpha Swarm Project",
		Scope:         "project",
		GitRoot:       projectRoot,
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha-swarm-project) error = %v", err)
	}
	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              "alpha-swarm-initiative",
		Title:            "Alpha Swarm Initiative",
		Kind:             "delivery",
		Status:           "active",
		Summary:          "Acceptance fixture for companion swarm status",
		OwnerCompanionID: &companion.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative(alpha-swarm-initiative) error = %v", err)
	}

	swarmService := supervisionsvc.Service{
		Store: store,
		Jobs:  jobsvc.Service{Store: store, Now: func() time.Time { return now }},
	}

	createParentTask := func(key string) sqlite.Task {
		task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
			ProjectID:    project.ID,
			Key:          key,
			Title:        strings.ReplaceAll(key, "-", " "),
			ActionKey:    "execute",
			Status:       "queued",
			Scope:        "project",
			RequestedBy:  "operator",
			WorkspaceID:  &workspace.ID,
			InitiativeID: &initiative.ID,
			CompanionID:  &companion.ID,
			WorkKind:     "delivery",
		})
		if err != nil {
			t.Fatalf("CreateTask(%s) error = %v", key, err)
		}
		return task
	}
	createDelegationPlans := func() []supervisionsvc.DelegationPlan {
		return []supervisionsvc.DelegationPlan{
			{
				DelegationKey:         "implement",
				Role:                  "builder",
				ActionClass:           "mutation",
				ActionKey:             "implement",
				MutationMode:          "isolated_worktree",
				ArtifactTarget:        "branch",
				Objective:             "Implement the requested change",
				RequestedTools:        []string{"repo_read", "branch_proposal"},
				RequestedMemoryScopes: []string{"workspace", "initiative", "companion"},
			},
			{
				DelegationKey:         "review",
				Role:                  "reviewer",
				ActionClass:           "analysis",
				ActionKey:             "review",
				MutationMode:          "read_only",
				ArtifactTarget:        "report",
				Objective:             "Review the implementation",
				RequestedTools:        []string{"repo_read"},
				RequestedMemoryScopes: []string{"workspace", "initiative", "companion"},
			},
		}
	}
	planAndMaterialize := func(parent sqlite.Task, requestedBudget int) {
		plan, err := swarmService.PlanSwarm(ctx, supervisionsvc.PlanSwarmParams{
			ParentTaskID:    parent.ID,
			Trigger:         supervisionsvc.TriggerBuildPlusReview,
			ConvergenceMode: "review_gate",
			RequestedBudget: requestedBudget,
			DelegationPlans: createDelegationPlans(),
		})
		if err != nil {
			t.Fatalf("PlanSwarm(%s) error = %v", parent.Key, err)
		}
		if _, err := swarmService.MaterializeSwarm(ctx, plan); err != nil {
			t.Fatalf("MaterializeSwarm(%s) error = %v", parent.Key, err)
		}
	}

	planAndMaterialize(createParentTask("alpha-active-swarm"), 2)

	completedRunningParent := createParentTask("alpha-completed-running-swarm")
	completedRunning, err := swarmService.PlanSwarm(ctx, supervisionsvc.PlanSwarmParams{
		ParentTaskID:    completedRunningParent.ID,
		Trigger:         supervisionsvc.TriggerBuildPlusReview,
		ConvergenceMode: "review_gate",
		RequestedBudget: 2,
		DelegationPlans: createDelegationPlans(),
	})
	if err != nil {
		t.Fatalf("PlanSwarm(alpha-completed-running-swarm) error = %v", err)
	}
	materializedCompletedRunning, err := swarmService.MaterializeSwarm(ctx, completedRunning)
	if err != nil {
		t.Fatalf("MaterializeSwarm(alpha-completed-running-swarm) error = %v", err)
	}
	if len(materializedCompletedRunning.Tasks) == 0 {
		t.Fatal("MaterializeSwarm(alpha-completed-running-swarm) returned no child tasks")
	}
	if _, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:     materializedCompletedRunning.Tasks[0].ID,
		Executor:   "codex",
		Attempt:    1,
		Status:     "running",
		TaskStatus: "running",
	}); err != nil {
		t.Fatalf("StartRun(alpha-completed-running-swarm) error = %v", err)
	}
	for _, delegation := range materializedCompletedRunning.Delegations {
		if _, err := store.CreateDelegationArtifact(ctx, sqlite.CreateDelegationArtifactParams{
			DelegationID: delegation.ID,
			ArtifactType: "result",
			Summary:      "Completed while child run is active",
			DetailsJSON:  `{"status":"completed","confidence":0.9,"evidence_refs":["alpha/completed-running"],"unresolved_risks":[],"proposed_next_actions":[],"proposed_memory_candidates":[]}`,
		}); err != nil {
			t.Fatalf("CreateDelegationArtifact(alpha-completed-running-swarm) error = %v", err)
		}
	}

	approvalParent := createParentTask("alpha-approval-swarm")
	planAndMaterialize(approvalParent, 2)
	if _, err := store.BlockTask(ctx, sqlite.BlockTaskParams{
		TaskID: approvalParent.ID,
		Reason: "approval_required",
	}); err != nil {
		t.Fatalf("BlockTask(alpha-approval-swarm) error = %v", err)
	}

	budgetParent := createParentTask("alpha-budget-swarm")
	planAndMaterialize(budgetParent, 3)
	if _, err := store.BlockTask(ctx, sqlite.BlockTaskParams{
		TaskID: budgetParent.ID,
		Reason: "budget_exhausted",
	}); err != nil {
		t.Fatalf("BlockTask(alpha-budget-swarm) error = %v", err)
	}
}

func writeSandcastleSpikeOdinRoot(t *testing.T, repoRoot string, projectRepo string) string {
	t.Helper()

	root := t.TempDir()
	for _, dir := range []string{
		filepath.Join(root, "config"),
		filepath.Join(root, "data"),
		filepath.Join(root, "registry"),
		filepath.Join(root, "state", "cache"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}

	copyWorkspaceIntegrationTree(t, filepath.Join(repoRoot, "registry"), filepath.Join(root, "registry"))
	writeTextFile(t, filepath.Join(root, "config", "odin.yaml"), `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: true
`)
	writeTextFile(t, filepath.Join(root, "config", "projects.yaml"), `
version: 1
projects:
  - key: sandcastle-demo
    name: Sandcastle Demo
    project_class: local_git_project
    git_root: `+projectRepo+`
    default_branch: main
    policy:
      allowed_commands: [status, test, build]
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
`)
	writeTextFile(t, filepath.Join(root, "config", "executors.yaml"), `
version: 1

executors:
  - key: sandcastle_headless
    adapter: sandcastle_headless
    class: plan_backed_cli
    enabled: true
    priority: 10
    model_ref: sandcastle-fixture

routes:
  - name: sandcastle-fixture
    match:
      task_kinds: [general, plan, build, review, qa]
      scopes: [project]
    preferred: [sandcastle_headless]
    fallback: []
`)
	return root
}

func firstLineValue(output string, key string) string {
	prefix := key + "="
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}
