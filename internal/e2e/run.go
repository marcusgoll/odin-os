package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/prompts"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tracker"
	trackergithub "odin-os/internal/tracker/github"
	trackerintake "odin-os/internal/tracker/intake"
	"odin-os/internal/vcs/leases"
)

const (
	defaultScenarioPath = "fixtures/e2e/github-readonly-intake.yaml"
	e2eUsage            = "usage: odin e2e [--scenario <path>] [--json] [--allow-live-codex] [--keep-temp]"

	defaultProjectKey  = "alpha"
	defaultProjectName = "Alpha"
	defaultGitHubRepo  = "acme/alpha"
)

// Run executes a local fixture-backed E2E scenario without loading the operator runtime.
func Run(ctx context.Context, repoRoot string, args []string, stdout io.Writer) error {
	options, err := parseArgs(args)
	if err != nil {
		return err
	}
	if options.help {
		_, err := fmt.Fprintln(stdout, e2eUsage)
		return err
	}

	runner := runner{
		repoRoot: repoRoot,
		options:  options,
		report: report{
			Status: "passed",
			GitHub: githubReport{
				Mode: "fixture",
			},
			Codex: codexReport{
				Mode: "disabled",
			},
		},
	}
	err = runner.run(ctx)
	if outputErr := writeReport(stdout, options.json, runner.report); outputErr != nil && err == nil {
		err = outputErr
	}
	return err
}

type options struct {
	scenarioPath   string
	json           bool
	help           bool
	allowLiveCodex bool
	keepTemp       bool
}

func parseArgs(args []string) (options, error) {
	parsed := options{scenarioPath: defaultScenarioPath}
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--help", "-h":
			parsed.help = true
		case "--json":
			parsed.json = true
		case "--allow-live-codex":
			parsed.allowLiveCodex = true
		case "--keep-temp":
			parsed.keepTemp = true
		case "--scenario":
			index++
			if index >= len(args) || strings.TrimSpace(args[index]) == "" {
				return options{}, errors.New("--scenario requires a path")
			}
			parsed.scenarioPath = args[index]
		default:
			return options{}, fmt.Errorf("unknown e2e flag: %s", args[index])
		}
	}
	return parsed, nil
}

type runner struct {
	repoRoot string
	options  options
	report   report
	scenario scenario
	store    *sqlite.Store
	fixture  *fixtureTracker
	odinRoot string
}

func (runner *runner) run(ctx context.Context) error {
	if err := runner.loadScenario(); err != nil {
		return runner.failStage("load_scenario", err)
	}
	runner.passStage("load_scenario", "loaded fixture scenario")

	if err := runner.enforceLocalGuards(); err != nil {
		return err
	}

	registry, err := runner.prepareTempRoot()
	if err != nil {
		return runner.failStage("prepare_temp_odin_root", err)
	}
	runner.report.OdinRoot = runner.odinRoot
	runner.passStage("prepare_temp_odin_root", "created isolated ODIN_ROOT")
	if !runner.options.keepTemp {
		defer os.RemoveAll(runner.odinRoot)
	}

	if err := runner.openStore(runner.odinRoot); err != nil {
		return runner.failStage("prepare_sqlite_store", err)
	}
	defer runner.store.Close()
	runner.passStage("prepare_sqlite_store", "migrated temp SQLite store")

	switch runner.scenario.Name {
	case "github-readonly-intake":
		err = runner.runGitHubReadOnlyIntake(ctx, registry)
	case "github-issue-delivery-dry-run":
		err = runner.runGitHubIssueDeliveryDryRun(ctx, registry)
	case "tracker-dry-run-lifecycle":
		err = runner.runTrackerDryRunLifecycle(ctx)
	case "workspace-safe-creation":
		err = runner.runWorkspaceSafeCreation(ctx)
	case "prompt-rendering-brownfield":
		err = runner.runPromptRenderingBrownfield(ctx)
	case "failure-analysis":
		err = runner.runFailureAnalysis(ctx)
	default:
		err = fmt.Errorf("unsupported e2e scenario %q", runner.scenario.Name)
	}
	if err != nil {
		return err
	}

	if runner.report.Codex.Mode == "disabled" {
		runner.passStage("codex_disabled_guard", "Codex execution disabled")
	}
	return nil
}

func (runner *runner) enforceLocalGuards() error {
	githubMode := strings.TrimSpace(runner.scenario.GitHub.Mode)
	if githubMode == "" {
		githubMode = "fixture"
	}
	runner.report.GitHub.Mode = githubMode
	if githubMode != "fixture" {
		return runner.failStage("github_fixture_guard", fmt.Errorf("github mode %q is not allowed for local e2e", githubMode))
	}

	codexMode := strings.TrimSpace(runner.scenario.Codex.Mode)
	if codexMode == "" {
		codexMode = "disabled"
	}
	runner.report.Codex.Mode = codexMode
	if codexMode == "live" && !runner.options.allowLiveCodex {
		return runner.failStage("codex_disabled_guard", errors.New("live Codex requires --allow-live-codex"))
	}
	if codexMode != "disabled" && codexMode != "live" {
		return runner.failStage("codex_disabled_guard", fmt.Errorf("unsupported codex mode %q", codexMode))
	}
	return nil
}

func (runner *runner) loadScenario() error {
	path := runner.options.scenarioPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(runner.repoRoot, path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(content, &runner.scenario); err != nil {
		return err
	}
	if strings.TrimSpace(runner.scenario.Name) == "" {
		return errors.New("scenario name is required")
	}
	runner.scenario.Project = runner.scenario.Project.withDefaults()
	runner.report.Scenario = runner.scenario.Name
	return nil
}

func (runner *runner) prepareTempRoot() (projects.Registry, error) {
	odinRoot, err := os.MkdirTemp("", "odin-e2e-*")
	if err != nil {
		return projects.Registry{}, err
	}
	runner.odinRoot = odinRoot
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(odinRoot)
		}
	}()

	configDir := filepath.Join(odinRoot, "config")
	workspaceDir := filepath.Join(odinRoot, "workspace", runner.scenario.Project.Key)
	if err := os.MkdirAll(filepath.Join(workspaceDir, ".git"), 0o755); err != nil {
		return projects.Registry{}, err
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return projects.Registry{}, err
	}

	manifestPath := filepath.Join(configDir, "projects.yaml")
	manifest := fmt.Sprintf(`version: 1
projects:
  - key: %s
    name: %s
    project_class: github_backed_project
    git_root: ../workspace/%s
    default_branch: main
    github:
      repo: %s
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
`,
		runner.scenario.Project.Key,
		quoteYAMLScalar(runner.scenario.Project.Name),
		runner.scenario.Project.Key,
		runner.scenario.Project.GitHubRepo,
	)
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		return projects.Registry{}, err
	}
	registry, diagnostics, err := projects.Register(manifestPath)
	if err != nil {
		return projects.Registry{}, err
	}
	if len(diagnostics) != 0 {
		return projects.Registry{}, fmt.Errorf("temp project registry diagnostics: %v", diagnostics)
	}
	cleanup = false
	return registry, nil
}

func quoteYAMLScalar(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Fixture Project"
	}
	encoded, err := yaml.Marshal(value)
	if err != nil {
		return value
	}
	return strings.TrimSpace(string(encoded))
}

func (runner *runner) openStore(odinRoot string) error {
	store, err := sqlite.Open(filepath.Join(odinRoot, "odin.db"))
	if err != nil {
		return err
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		return err
	}
	runner.store = store
	return nil
}

func (runner *runner) runGitHubReadOnlyIntake(ctx context.Context, registry projects.Registry) error {
	runner.passStage("load_config", "loaded temp project config")

	loadStep := runner.step("load_fixture_issues")
	issues, err := runner.loadTrackerIssues(loadStep.Fixture)
	if err != nil {
		return runner.failStage("load_fixture_issues", err)
	}
	runner.passStage("load_fixture_issues", fmt.Sprintf("loaded %d fixture issues", len(issues)))

	filterStep := runner.step("filter_eligible_issues")
	eligible := filterEligibleIssues(issues, filterStep.Expect.RequiredLabel, filterStep.Expect.ExcludedLabels)
	if filterStep.Expect.EligibleCount != 0 && len(eligible) != filterStep.Expect.EligibleCount {
		return runner.failStage("filter_eligible_issues", fmt.Errorf("eligible_count = %d, want %d", len(eligible), filterStep.Expect.EligibleCount))
	}
	runner.passStage("filter_eligible_issues", fmt.Sprintf("eligible_count=%d", len(eligible)))

	runner.fixture = &fixtureTracker{issues: eligible}
	service := trackerintake.Service{
		Store:    runner.store,
		Registry: registry,
		NewTracker: func(project projects.Manifest, _ trackerintake.SyncOptions) (tracker.Tracker, error) {
			if project.GitHub.Repo != runner.scenario.Project.GitHubRepo {
				return nil, fmt.Errorf("project repo = %q, want %q", project.GitHub.Repo, runner.scenario.Project.GitHubRepo)
			}
			return runner.fixture, nil
		},
	}
	first, err := service.SyncProject(ctx, trackerintake.SyncOptions{ProjectKey: runner.scenario.Project.Key})
	if err != nil {
		return runner.failStage("persist_external_issues", err)
	}
	second, err := service.SyncProject(ctx, trackerintake.SyncOptions{ProjectKey: runner.scenario.Project.Key})
	if err != nil {
		return runner.failStage("persist_external_issues", err)
	}
	persisted, err := runner.store.ListExternalIssues(ctx, sqlite.ListExternalIssuesParams{
		Repo:       first.Repo,
		SyncStatus: "eligible",
	})
	if err != nil {
		return runner.failStage("persist_external_issues", err)
	}
	if len(persisted) != len(eligible) {
		return runner.failStage("persist_external_issues", fmt.Errorf("stored external issues = %d, want %d", len(persisted), len(eligible)))
	}
	runner.report.Intake = intakeReport{
		Project:   first.ProjectKey,
		Repo:      first.Repo,
		Fetched:   first.Fetched,
		Persisted: first.Persisted,
		Stored:    len(persisted),
	}
	runner.passStage("persist_external_issues", fmt.Sprintf("stored=%d idempotent=%t", len(persisted), first.Fetched == second.Fetched))

	if runner.fixture.mutationCalls != runner.step("assert_no_github_mutation").Expect.Writes {
		runner.report.GitHub.Mutated = true
		return runner.failStage("assert_no_github_mutation", fmt.Errorf("github writes = %d, want 0", runner.fixture.mutationCalls))
	}
	runner.passStage("assert_no_github_mutation", "writes=0")
	return nil
}

func (runner *runner) runGitHubIssueDeliveryDryRun(ctx context.Context, registry projects.Registry) error {
	loadStep := runner.step("load_fixture_issue")
	issues, err := runner.loadTrackerIssues(loadStep.Fixture)
	if err != nil {
		return runner.failStage("load_fixture_issue", err)
	}
	eligible := filterEligibleIssues(issues, tracker.LabelReady, []string{tracker.LabelPaused, tracker.LabelBlocked})
	if loadStep.Expect.EligibleCount != 0 && len(eligible) != loadStep.Expect.EligibleCount {
		return runner.failStage("load_fixture_issue", fmt.Errorf("eligible_count = %d, want %d", len(eligible), loadStep.Expect.EligibleCount))
	}
	if len(eligible) == 0 {
		return runner.failStage("load_fixture_issue", errors.New("fixture produced no eligible issue"))
	}
	runner.passStage("load_fixture_issue", fmt.Sprintf("eligible_count=%d", len(eligible)))

	runner.fixture = &fixtureTracker{issues: eligible}
	intakeService := trackerintake.Service{
		Store:    runner.store,
		Registry: registry,
		NewTracker: func(project projects.Manifest, _ trackerintake.SyncOptions) (tracker.Tracker, error) {
			if project.GitHub.Repo != runner.scenario.Project.GitHubRepo {
				return nil, fmt.Errorf("project repo = %q, want %q", project.GitHub.Repo, runner.scenario.Project.GitHubRepo)
			}
			return runner.fixture, nil
		},
	}
	syncSummary, err := intakeService.SyncProject(ctx, trackerintake.SyncOptions{ProjectKey: runner.scenario.Project.Key})
	if err != nil {
		return runner.failStage("review_issue_into_work_item", err)
	}
	reconcileSummary, err := intakeService.ReconcileProject(ctx, trackerintake.ReconcileOptions{ProjectKey: runner.scenario.Project.Key})
	if err != nil {
		return runner.failStage("review_issue_into_work_item", err)
	}
	reviewStep := runner.step("review_issue_into_work_item")
	if reviewStep.Expect.Created != 0 && reconcileSummary.Created != reviewStep.Expect.Created {
		return runner.failStage("review_issue_into_work_item", fmt.Errorf("created = %d, want %d", reconcileSummary.Created, reviewStep.Expect.Created))
	}
	if reviewStep.Expect.Linked != 0 && reconcileSummary.Linked != reviewStep.Expect.Linked {
		return runner.failStage("review_issue_into_work_item", fmt.Errorf("linked = %d, want %d", reconcileSummary.Linked, reviewStep.Expect.Linked))
	}

	project, err := runner.store.GetProjectByKey(ctx, runner.scenario.Project.Key)
	if err != nil {
		return runner.failStage("review_issue_into_work_item", err)
	}
	if _, err := (projects.Service{Store: runner.store}).SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "e2e",
		Notes:       "fixture issue delivery dry-run proof",
	}); err != nil {
		return runner.failStage("review_issue_into_work_item", err)
	}
	task, err := runner.store.GetTaskByProjectAndKey(ctx, project.ID, externalIssueTaskKey(eligible[0]))
	if err != nil {
		return runner.failStage("review_issue_into_work_item", err)
	}
	if task.Status != "queued" || task.RequestedBy != "github_issue_intake" {
		return runner.failStage("review_issue_into_work_item", fmt.Errorf("task status/requested_by = %s/%s, want queued/github_issue_intake", task.Status, task.RequestedBy))
	}
	runner.report.Intake = intakeReport{
		Project:   syncSummary.ProjectKey,
		Repo:      syncSummary.Repo,
		Fetched:   syncSummary.Fetched,
		Persisted: syncSummary.Persisted,
		Stored:    reconcileSummary.Eligible,
	}
	runner.report.Delivery.WorkItemKey = task.Key
	runner.passStage("review_issue_into_work_item", fmt.Sprintf("created=%d linked=%d work_item=%s", reconcileSummary.Created, reconcileSummary.Linked, task.Key))

	git := &fixtureGit{}
	worktreeRoot := filepath.Join(runner.odinRoot, "worktrees")
	jobService := jobs.Service{
		Store:    runner.store,
		Registry: registry,
		Executors: map[string]contract.Executor{
			"fixture_delivery": fixtureDeliveryExecutor{},
		},
		ExecutorConfig: executorrouter.Config{
			Version: 1,
			Executors: []executorrouter.ExecutorConfig{{
				Key:      "fixture_delivery",
				Adapter:  "fixture_delivery",
				Class:    contract.ExecutorClassPlanBackedCLI,
				Enabled:  true,
				Priority: 10,
			}},
			Routes: []executorrouter.RouteConfig{{
				Name: "fixture_delivery",
				Match: executorrouter.RouteMatch{
					TaskKinds: []contract.TaskKind{contract.TaskKindGeneral},
					Scopes:    []string{"project"},
				},
				Preferred: []string{"fixture_delivery"},
			}},
		},
		Leases: leases.Manager{
			Store:        runner.store,
			Git:          git,
			WorktreeRoot: worktreeRoot,
		},
	}
	dispatch, err := jobService.DispatchTaskRunAttempt(ctx, task.ID)
	if err != nil {
		return runner.failStage("dispatch_to_isolated_worktree", err)
	}
	if !dispatch.Dispatched || dispatch.Run == nil || dispatch.Task.Status != "running" {
		return runner.failStage("dispatch_to_isolated_worktree", fmt.Errorf("dispatch = %+v, want running dispatched run", dispatch))
	}
	lease, err := runner.store.GetActiveWorktreeLeaseByTaskRun(ctx, dispatch.Task.ID, dispatch.Run.ID)
	if err != nil {
		return runner.failStage("dispatch_to_isolated_worktree", err)
	}
	dispatchStep := runner.step("dispatch_to_isolated_worktree")
	if !strings.HasPrefix(lease.BranchName, dispatchStep.Expect.BranchPrefix) {
		return runner.failStage("dispatch_to_isolated_worktree", fmt.Errorf("branch %q does not start with %q", lease.BranchName, dispatchStep.Expect.BranchPrefix))
	}
	if dispatchStep.Expect.InsideWorkspaceRoot && !isInside(worktreeRoot, lease.WorktreePath) {
		return runner.failStage("dispatch_to_isolated_worktree", fmt.Errorf("worktree path %q escaped root %q", lease.WorktreePath, worktreeRoot))
	}
	runner.report.Workspace = workspaceReport{
		Branch:              lease.BranchName,
		WorktreePath:        lease.WorktreePath,
		InsideWorkspaceRoot: isInside(worktreeRoot, lease.WorktreePath),
	}
	runner.report.Delivery.RunID = dispatch.Run.ID
	runner.passStage("dispatch_to_isolated_worktree", fmt.Sprintf("branch=%s worktree=%s", lease.BranchName, lease.WorktreePath))

	executed, err := jobService.ExecuteDispatchedRun(ctx, task.ID)
	if err != nil {
		return runner.failStage("execute_deterministic_stub", err)
	}
	if !executed.Executed || executed.Run == nil || executed.Task.Status != "completed" || executed.Run.Status != "completed" {
		return runner.failStage("execute_deterministic_stub", fmt.Errorf("execute = %+v, want completed deterministic run", executed))
	}
	artifacts, err := runner.store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: executed.Run.ID, ArtifactType: "executor_evidence"})
	if err != nil {
		return runner.failStage("execute_deterministic_stub", err)
	}
	testsRecorded, reviewArtifact := deliveryEvidenceFlags(artifacts)
	executeStep := runner.step("execute_deterministic_stub")
	if executeStep.Expect.TestsRecorded && !testsRecorded {
		return runner.failStage("execute_deterministic_stub", errors.New("test evidence artifact was not recorded"))
	}
	if executeStep.Expect.ReviewArtifact && !reviewArtifact {
		return runner.failStage("execute_deterministic_stub", errors.New("review artifact evidence was not recorded"))
	}
	runner.report.Delivery.TestsRecorded = testsRecorded
	runner.report.Delivery.ReviewArtifact = reviewArtifact
	runner.passStage("execute_deterministic_stub", fmt.Sprintf("tests_recorded=%t review_artifact=%t", testsRecorded, reviewArtifact))

	runID := executed.Run.ID
	approval, err := runner.store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      executed.Task.ID,
		RunID:       &runID,
		Status:      "pending",
		RequestedBy: "pr_creation_dry_run",
	})
	if err != nil {
		return runner.failStage("require_pr_approval", err)
	}
	if approval.Status != "pending" {
		return runner.failStage("require_pr_approval", fmt.Errorf("approval status = %q, want pending", approval.Status))
	}
	if runner.fixture.mutationCalls != runner.step("require_pr_approval").Expect.GitHubWrites {
		runner.report.GitHub.Mutated = true
		return runner.failStage("require_pr_approval", fmt.Errorf("github writes = %d, want %d", runner.fixture.mutationCalls, runner.step("require_pr_approval").Expect.GitHubWrites))
	}
	runner.report.Delivery.PRReadyBranch = lease.BranchName
	runner.report.Delivery.PRApprovalRequired = true
	runner.passStage("require_pr_approval", fmt.Sprintf("approval=%d status=pending github_writes=%d", approval.ID, runner.fixture.mutationCalls))
	return nil
}

func (runner *runner) runTrackerDryRunLifecycle(ctx context.Context) error {
	doer := &countingDoer{}
	client := trackergithub.NewClientWithConfigAndDoer(trackergithub.Config{
		BaseURL: "https://fixture.invalid",
		Owner:   "acme",
		Repo:    "alpha",
		DryRun:  true,
	}, doer)
	id := tracker.IssueID{Provider: "github", Repo: runner.scenario.Project.GitHubRepo, Number: 101}

	for _, step := range runner.scenario.Steps {
		before := doer.requests
		var err error
		switch step.Name {
		case "mark_running":
			err = client.MarkInProgress(ctx, id)
		case "mark_human_review":
			err = client.MarkReadyForReview(ctx, id)
		case "add_comment":
			err = client.AddComment(ctx, id, "Fixture dry-run comment.")
		default:
			continue
		}
		denied := errors.Is(err, tracker.ErrMutationUnsupported)
		if err != nil && !denied {
			return runner.failStage(step.Name, err)
		}
		writes := doer.requests - before
		if writes != step.Expect.GitHubWrites {
			runner.report.GitHub.Mutated = writes > 0
			return runner.failStage(step.Name, fmt.Errorf("github_writes = %d, want %d", writes, step.Expect.GitHubWrites))
		}
		runner.passStage(step.Name, fmt.Sprintf("dry_run=%t mutation_denied=%t github_writes=%d", step.DryRun, denied, writes))
	}
	return nil
}

func (runner *runner) runWorkspaceSafeCreation(ctx context.Context) error {
	project, task, run, err := runner.createRuntimeWork(ctx, runner.step("create_workspace").IssueTitle)
	if err != nil {
		return runner.failStage("create_workspace", err)
	}
	root := filepath.Join(runner.odinRoot, "worktrees")
	git := &fixtureGit{}
	assignment, err := leases.Manager{
		Store:        runner.store,
		Git:          git,
		WorktreeRoot: root,
	}.Prepare(ctx, leases.Request{
		Mutating:      true,
		ProjectID:     project.ID,
		ProjectKey:    project.Key,
		TaskID:        task.ID,
		RunID:         run.ID,
		RepoRoot:      project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		Try:           1,
	})
	if err != nil {
		return runner.failStage("create_workspace", err)
	}
	createStep := runner.step("create_workspace")
	if !strings.HasPrefix(assignment.BranchName, createStep.Expect.BranchPrefix) {
		return runner.failStage("create_workspace", fmt.Errorf("branch %q does not start with %q", assignment.BranchName, createStep.Expect.BranchPrefix))
	}
	if createStep.Expect.InsideWorkspaceRoot && !isInside(root, assignment.WorktreePath) {
		return runner.failStage("create_workspace", fmt.Errorf("worktree path %q escaped root %q", assignment.WorktreePath, root))
	}
	runner.report.Workspace = workspaceReport{
		Branch:              assignment.BranchName,
		WorktreePath:        assignment.WorktreePath,
		InsideWorkspaceRoot: isInside(root, assignment.WorktreePath),
	}
	runner.passStage("create_workspace", "branch_prefix=odin/ inside_workspace_root=true")

	rejectStep := runner.step("reject_path_traversal")
	rejected := containsPathTraversal(rejectStep.IssueTitle)
	if rejected != rejectStep.Expect.Rejected {
		return runner.failStage("reject_path_traversal", fmt.Errorf("rejected = %t, want %t", rejected, rejectStep.Expect.Rejected))
	}
	runner.passStage("reject_path_traversal", "rejected=true")
	return nil
}

func (runner *runner) runPromptRenderingBrownfield(ctx context.Context) error {
	step := runner.step("render_go_orchestrator_prompt")
	title, criteria, err := runner.loadIssueMarkdown(step.IssueFixture)
	if err != nil {
		return runner.failStage(step.Name, err)
	}
	rendered, err := prompts.FileRenderer{Root: filepath.Join(runner.repoRoot, "prompts", "workers")}.Render(ctx, "go-orchestrator", prompts.TemplateData{
		WorkItemID:         "fixture-brownfield-refactor",
		Role:               "go-orchestrator",
		Title:              title,
		AcceptanceCriteria: criteria,
		Metadata: map[string]string{
			"scenario": runner.scenario.Name,
		},
	})
	if err != nil {
		return runner.failStage(step.Name, err)
	}
	for _, want := range step.ExpectContains {
		if !strings.Contains(rendered, want) {
			return runner.failStage(step.Name, fmt.Errorf("rendered prompt missing %q", want))
		}
	}
	runner.report.Prompt = promptReport{
		Template:  "go-orchestrator",
		SizeBytes: prompts.PromptSizeBytes(rendered),
	}
	runner.passStage(step.Name, fmt.Sprintf("template=go-orchestrator size_bytes=%d", runner.report.Prompt.SizeBytes))
	return nil
}

func (runner *runner) runFailureAnalysis(ctx context.Context) error {
	step := runner.step("classify_missing_acceptance_criteria")
	input, err := runner.loadFailureFixture(step.Input)
	if err != nil {
		return runner.failStage(step.Name, err)
	}
	category := classifyFailure(input)
	if category != step.Expect.Category {
		return runner.failStage(step.Name, fmt.Errorf("category = %q, want %q", category, step.Expect.Category))
	}

	doer := &countingDoer{}
	client := trackergithub.NewClientWithConfigAndDoer(trackergithub.Config{
		BaseURL: "https://fixture.invalid",
		Owner:   "acme",
		Repo:    "alpha",
		DryRun:  true,
	}, doer)
	followUp, err := client.CreateFollowUpIssue(ctx, tracker.FollowUpIssue{
		Repo:   runner.scenario.Project.GitHubRepo,
		Title:  "Follow up: " + input.Title,
		Body:   input.Summary,
		Labels: []string{tracker.LabelHumanReview},
	})
	denied := errors.Is(err, tracker.ErrMutationUnsupported)
	if err != nil && !denied {
		return runner.failStage(step.Name, err)
	}
	created := !denied && followUp.State == "dry-run" && followUp.Title != ""
	if created != step.Expect.CreatesFollowUp {
		return runner.failStage(step.Name, fmt.Errorf("creates_follow_up = %t, want %t", created, step.Expect.CreatesFollowUp))
	}
	if doer.requests != 0 {
		runner.report.GitHub.Mutated = true
		return runner.failStage(step.Name, fmt.Errorf("github writes = %d, want 0", doer.requests))
	}
	runner.report.Failure = failureReport{Category: category, CreatesFollowUp: created}
	runner.passStage(step.Name, fmt.Sprintf("category=%q creates_follow_up=%t mutation_denied=%t", category, created, denied))
	return nil
}

func (runner *runner) createRuntimeWork(ctx context.Context, title string) (sqlite.Project, sqlite.Task, sqlite.Run, error) {
	project, err := runner.store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           runner.scenario.Project.Key,
		Name:          runner.scenario.Project.Name,
		Scope:         "project",
		GitRoot:       filepath.Join(runner.odinRoot, "workspace", runner.scenario.Project.Key),
		DefaultBranch: "main",
		GitHubRepo:    runner.scenario.Project.GitHubRepo,
		ManifestPath:  filepath.Join(runner.odinRoot, "config", "projects.yaml"),
	})
	if err != nil {
		existing, getErr := runner.store.GetProjectByKey(ctx, runner.scenario.Project.Key)
		if getErr != nil {
			return sqlite.Project{}, sqlite.Task{}, sqlite.Run{}, err
		}
		project = existing
	}
	task, err := runner.store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "WI-42",
		Title:       title,
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "e2e",
	})
	if err != nil {
		return sqlite.Project{}, sqlite.Task{}, sqlite.Run{}, err
	}
	run, err := runner.store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "go-orchestrator",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		return sqlite.Project{}, sqlite.Task{}, sqlite.Run{}, err
	}
	return project, task, run, nil
}

func (runner *runner) loadTrackerIssues(path string) ([]tracker.Issue, error) {
	var raw []scenarioIssue
	if err := runner.readJSONFixture(path, &raw); err != nil {
		return nil, err
	}
	issues := make([]tracker.Issue, 0, len(raw))
	for _, issue := range raw {
		issues = append(issues, tracker.Issue{
			Provider: "github",
			Repo:     runner.scenario.Project.GitHubRepo,
			Number:   issue.Number,
			Title:    issue.Title,
			Body:     issue.Body,
			URL:      issue.URL,
			State:    issue.State,
			Labels:   append([]string(nil), issue.Labels...),
		})
	}
	return issues, nil
}

func (runner *runner) loadIssueMarkdown(path string) (string, []string, error) {
	content, err := os.ReadFile(runner.fixturePath(path))
	if err != nil {
		return "", nil, err
	}
	title := "Brownfield refactor"
	var criteria []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "# "):
			title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
		case strings.HasPrefix(line, "- "):
			criteria = append(criteria, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
		}
	}
	if len(criteria) == 0 {
		return "", nil, fmt.Errorf("issue fixture %s has no acceptance criteria", path)
	}
	return title, criteria, nil
}

func (runner *runner) loadFailureFixture(path string) (failureFixture, error) {
	var input failureFixture
	if err := runner.readJSONFixture(path, &input); err != nil {
		return failureFixture{}, err
	}
	return input, nil
}

func (runner *runner) readJSONFixture(path string, target any) error {
	content, err := os.ReadFile(runner.fixturePath(path))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(content, target); err != nil {
		return fmt.Errorf("decode fixture %s: %w", path, err)
	}
	return nil
}

func (runner *runner) fixturePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(runner.repoRoot, path)
}

func (runner *runner) step(name string) scenarioStep {
	for _, step := range runner.scenario.Steps {
		if step.Name == name {
			return step
		}
	}
	return scenarioStep{Name: name}
}

func filterEligibleIssues(issues []tracker.Issue, requiredLabel string, excludedLabels []string) []tracker.Issue {
	if requiredLabel == "" {
		requiredLabel = tracker.LabelReady
	}
	var eligible []tracker.Issue
	for _, issue := range issues {
		if issue.State != "" && issue.State != "open" {
			continue
		}
		if !hasLabel(issue.Labels, requiredLabel) {
			continue
		}
		if hasAnyLabel(issue.Labels, excludedLabels) {
			continue
		}
		eligible = append(eligible, issue)
	}
	return eligible
}

func hasAnyLabel(labels []string, blocked []string) bool {
	for _, label := range blocked {
		if hasLabel(labels, label) {
			return true
		}
	}
	return false
}

func hasLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func isInside(root, path string) bool {
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func containsPathTraversal(value string) bool {
	cleaned := filepath.Clean(value)
	return strings.Contains(cleaned, "..") || strings.Contains(value, "/") || strings.Contains(value, "\\")
}

func classifyFailure(input failureFixture) string {
	text := strings.ToLower(input.Category + " " + input.Summary + " " + strings.Join(input.Signals, " "))
	if strings.Contains(text, "acceptance criteria") {
		return "missing acceptance criteria"
	}
	return "uncategorized"
}

func (runner *runner) passStage(name, detail string) {
	runner.report.Stages = append(runner.report.Stages, stageReport{
		Name:   name,
		Status: "passed",
		Detail: detail,
	})
}

func (runner *runner) failStage(name string, err error) error {
	runner.report.Status = "failed"
	runner.report.Stages = append(runner.report.Stages, stageReport{
		Name:   name,
		Status: "failed",
		Detail: err.Error(),
	})
	return err
}

func writeReport(stdout io.Writer, jsonOutput bool, report report) error {
	if jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	if report.Status == "failed" {
		last := report.Stages[len(report.Stages)-1]
		_, err := fmt.Fprintf(stdout, "status=failed scenario=%s stage=%s error=%q\n", report.Scenario, last.Name, last.Detail)
		return err
	}
	_, err := fmt.Fprintf(stdout, "status=passed scenario=%s odin_root=%s stages=%d github_mode=%s github_mutated=%t codex_mode=%s codex_invoked=%t intake_fetched=%d intake_persisted=%d\n",
		report.Scenario,
		report.OdinRoot,
		len(report.Stages),
		report.GitHub.Mode,
		report.GitHub.Mutated,
		report.Codex.Mode,
		report.Codex.Invoked,
		report.Intake.Fetched,
		report.Intake.Persisted,
	)
	return err
}

type scenario struct {
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	Project     scenarioProject `yaml:"project"`
	GitHub      scenarioGitHub  `yaml:"github"`
	Codex       scenarioCodex   `yaml:"codex"`
	Steps       []scenarioStep  `yaml:"steps"`
}

type scenarioProject struct {
	Key        string `yaml:"key"`
	Name       string `yaml:"name"`
	GitHubRepo string `yaml:"github_repo"`
}

func (project scenarioProject) withDefaults() scenarioProject {
	if strings.TrimSpace(project.Key) == "" {
		project.Key = defaultProjectKey
	}
	if strings.TrimSpace(project.Name) == "" {
		project.Name = defaultProjectName
	}
	if strings.TrimSpace(project.GitHubRepo) == "" {
		project.GitHubRepo = defaultGitHubRepo
	}
	return project
}

type scenarioGitHub struct {
	Mode string `yaml:"mode"`
}

type scenarioStep struct {
	Name           string     `yaml:"name"`
	Fixture        string     `yaml:"fixture"`
	Input          string     `yaml:"input"`
	IssueFixture   string     `yaml:"issue_fixture"`
	IssueNumber    int        `yaml:"issue_number"`
	IssueTitle     string     `yaml:"issue_title"`
	AgentRole      string     `yaml:"agent_role"`
	DryRun         bool       `yaml:"dry_run"`
	Expect         stepExpect `yaml:"-"`
	ExpectContains []string   `yaml:"expect_contains"`
}

func (step *scenarioStep) UnmarshalYAML(value *yaml.Node) error {
	var raw struct {
		Name           string   `yaml:"name"`
		Fixture        string   `yaml:"fixture"`
		Input          string   `yaml:"input"`
		IssueFixture   string   `yaml:"issue_fixture"`
		IssueNumber    int      `yaml:"issue_number"`
		IssueTitle     string   `yaml:"issue_title"`
		AgentRole      string   `yaml:"agent_role"`
		DryRun         bool     `yaml:"dry_run"`
		Expect         any      `yaml:"expect"`
		ExpectContains []string `yaml:"expect_contains"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*step = scenarioStep{
		Name:           raw.Name,
		Fixture:        raw.Fixture,
		Input:          raw.Input,
		IssueFixture:   raw.IssueFixture,
		IssueNumber:    raw.IssueNumber,
		IssueTitle:     raw.IssueTitle,
		AgentRole:      raw.AgentRole,
		DryRun:         raw.DryRun,
		ExpectContains: raw.ExpectContains,
	}
	if mapped, ok := raw.Expect.(map[string]any); ok {
		encoded, err := yaml.Marshal(mapped)
		if err != nil {
			return err
		}
		if err := yaml.Unmarshal(encoded, &step.Expect); err != nil {
			return err
		}
	}
	return nil
}

type stepExpect struct {
	EligibleCount       int      `yaml:"eligible_count"`
	Created             int      `yaml:"created"`
	Linked              int      `yaml:"linked"`
	RequiredLabel       string   `yaml:"required_label"`
	ExcludedLabels      []string `yaml:"excluded_labels"`
	IDempotent          bool     `yaml:"idempotent"`
	Writes              int      `yaml:"writes"`
	GitHubWrites        int      `yaml:"github_writes"`
	BranchPrefix        string   `yaml:"branch_prefix"`
	InsideWorkspaceRoot bool     `yaml:"inside_workspace_root"`
	Rejected            bool     `yaml:"rejected"`
	Category            string   `yaml:"category"`
	CreatesFollowUp     bool     `yaml:"creates_follow_up"`
	TestsRecorded       bool     `yaml:"tests_recorded"`
	ReviewArtifact      bool     `yaml:"review_artifact"`
	ApprovalRequired    bool     `yaml:"approval_required"`
}

type scenarioIssue struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	URL    string   `json:"url"`
	State  string   `json:"state"`
	Labels []string `json:"labels"`
}

type scenarioCodex struct {
	Mode string `yaml:"mode"`
}

type failureFixture struct {
	Title    string   `json:"title"`
	Summary  string   `json:"summary"`
	Category string   `json:"category"`
	Signals  []string `json:"signals"`
}

type report struct {
	Status    string          `json:"status"`
	Scenario  string          `json:"scenario"`
	OdinRoot  string          `json:"odin_root"`
	Stages    []stageReport   `json:"stages"`
	GitHub    githubReport    `json:"github"`
	Codex     codexReport     `json:"codex"`
	Intake    intakeReport    `json:"intake"`
	Workspace workspaceReport `json:"workspace,omitempty"`
	Delivery  deliveryReport  `json:"delivery,omitempty"`
	Prompt    promptReport    `json:"prompt,omitempty"`
	Failure   failureReport   `json:"failure,omitempty"`
}

type stageReport struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type githubReport struct {
	Mode    string `json:"mode"`
	Mutated bool   `json:"mutated"`
}

type codexReport struct {
	Mode    string `json:"mode"`
	Invoked bool   `json:"invoked"`
}

type intakeReport struct {
	Project   string `json:"project"`
	Repo      string `json:"repo"`
	Fetched   int    `json:"fetched"`
	Persisted int    `json:"persisted"`
	Stored    int    `json:"stored"`
}

type workspaceReport struct {
	Branch              string `json:"branch,omitempty"`
	WorktreePath        string `json:"worktree_path,omitempty"`
	InsideWorkspaceRoot bool   `json:"inside_workspace_root,omitempty"`
}

type deliveryReport struct {
	WorkItemKey        string `json:"work_item_key,omitempty"`
	RunID              int64  `json:"run_id,omitempty"`
	PRReadyBranch      string `json:"pr_ready_branch,omitempty"`
	PRApprovalRequired bool   `json:"pr_approval_required,omitempty"`
	TestsRecorded      bool   `json:"tests_recorded,omitempty"`
	ReviewArtifact     bool   `json:"review_artifact,omitempty"`
}

type promptReport struct {
	Template  string `json:"template,omitempty"`
	SizeBytes int    `json:"size_bytes,omitempty"`
}

type failureReport struct {
	Category        string `json:"category,omitempty"`
	CreatesFollowUp bool   `json:"creates_follow_up,omitempty"`
}

type fixtureTracker struct {
	issues        []tracker.Issue
	fetchCalls    int
	mutationCalls int
}

func (fixture *fixtureTracker) FetchEligibleIssues(context.Context) ([]tracker.Issue, error) {
	fixture.fetchCalls++
	issues := make([]tracker.Issue, len(fixture.issues))
	copy(issues, fixture.issues)
	return issues, nil
}

func (fixture *fixtureTracker) FetchIssueByID(context.Context, tracker.IssueID) (tracker.Issue, error) {
	fixture.mutationCalls++
	return tracker.Issue{}, errors.New("fixture tracker does not allow lookup in local e2e")
}

func (fixture *fixtureTracker) MarkInProgress(context.Context, tracker.IssueID) error {
	fixture.mutationCalls++
	return errors.New("fixture tracker does not allow mutation in local e2e")
}

func (fixture *fixtureTracker) MarkBlocked(context.Context, tracker.IssueID, string) error {
	fixture.mutationCalls++
	return errors.New("fixture tracker does not allow mutation in local e2e")
}

func (fixture *fixtureTracker) MarkFailed(context.Context, tracker.IssueID, string) error {
	fixture.mutationCalls++
	return errors.New("fixture tracker does not allow mutation in local e2e")
}

func (fixture *fixtureTracker) MarkReadyForReview(context.Context, tracker.IssueID) error {
	fixture.mutationCalls++
	return errors.New("fixture tracker does not allow mutation in local e2e")
}

func (fixture *fixtureTracker) MarkDone(context.Context, tracker.IssueID) error {
	fixture.mutationCalls++
	return errors.New("fixture tracker does not allow mutation in local e2e")
}

func (fixture *fixtureTracker) AddComment(context.Context, tracker.IssueID, string) error {
	fixture.mutationCalls++
	return errors.New("fixture tracker does not allow mutation in local e2e")
}

func (fixture *fixtureTracker) CreateFollowUpIssue(context.Context, tracker.FollowUpIssue) (tracker.Issue, error) {
	fixture.mutationCalls++
	return tracker.Issue{}, errors.New("fixture tracker does not allow mutation in local e2e")
}

type countingDoer struct {
	requests int
}

func (doer *countingDoer) Do(*http.Request) (*http.Response, error) {
	doer.requests++
	return nil, errors.New("fixture E2E must not call GitHub")
}

type fixtureGit struct {
	branches  []string
	worktrees []string
}

func (git *fixtureGit) BranchExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (git *fixtureGit) CreateBranch(_ context.Context, _ string, branch string, _ string) error {
	git.branches = append(git.branches, branch)
	return nil
}

func (git *fixtureGit) AddWorktree(_ context.Context, _ string, path string, _ string) error {
	git.worktrees = append(git.worktrees, path)
	return os.MkdirAll(path, 0o755)
}

func (git *fixtureGit) RemoveWorktree(context.Context, string, string) error {
	return nil
}

func (git *fixtureGit) WorktreeDirty(context.Context, string) (bool, error) {
	return false, nil
}

var _ leases.Git = (*fixtureGit)(nil)

type fixtureDeliveryExecutor struct{}

func (fixtureDeliveryExecutor) Key() string {
	return "fixture_delivery"
}

func (fixtureDeliveryExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (fixtureDeliveryExecutor) Health(context.Context) (contract.HealthReport, error) {
	return contract.HealthReport{
		Status: contract.HealthStatusHealthy,
	}, nil
}

func (fixtureDeliveryExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsHeadlessPlan: true,
		TaskKinds:            []contract.TaskKind{contract.TaskKindGeneral},
		Scopes:               []string{"project"},
	}, nil
}

func (fixtureDeliveryExecutor) RunTask(_ context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	worktreePath := strings.TrimSpace(spec.Metadata["worktree_path"])
	if worktreePath == "" {
		return contract.ExecutionResult{}, errors.New("worktree_path metadata is required")
	}
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		return contract.ExecutionResult{}, err
	}
	reviewPath := filepath.Join(worktreePath, "ODIN_REVIEW.md")
	if err := os.WriteFile(reviewPath, []byte("tests: passed\nreview: pr-ready\n"), 0o644); err != nil {
		return contract.ExecutionResult{}, err
	}
	artifactsJSON := `[{"type":"test","status":"passed","command":"fixture deterministic test"},{"type":"review","status":"ready","target":"pr-ready-branch"}]`
	return contract.ExecutionResult{
		Handle: contract.TaskHandle{
			ExecutorKey: "fixture_delivery",
			ExternalID:  "fixture-delivery-run",
			Status:      "completed",
		},
		Status: "completed",
		Output: "deterministic tests passed; review artifact ready; branch is PR-ready",
		Metadata: map[string]string{
			"operation":       "issue_to_pr_delivery_dry_run",
			"marker_path":     reviewPath,
			"marker_written":  "true",
			"branch_observed": strings.TrimSpace(spec.Metadata["branch_name"]),
			"artifacts_json":  artifactsJSON,
		},
	}, nil
}

func (fixtureDeliveryExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}

func (fixtureDeliveryExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return contract.ErrNotImplemented
}

func (fixtureDeliveryExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, contract.ErrNotImplemented
}

func externalIssueTaskKey(issue tracker.Issue) string {
	provider := strings.TrimSpace(issue.Provider)
	if provider == "" {
		provider = "github"
	}
	return fmt.Sprintf("%s-issue-%d", slugKeyPart(provider), issue.Number)
}

func slugKeyPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func deliveryEvidenceFlags(artifacts []sqlite.RunArtifact) (testsRecorded bool, reviewArtifact bool) {
	for _, artifact := range artifacts {
		var details map[string]string
		if err := json.Unmarshal([]byte(artifact.DetailsJSON), &details); err != nil {
			continue
		}
		artifactsJSON := details["artifacts_json"]
		if strings.Contains(artifactsJSON, `"type":"test"`) && strings.Contains(artifactsJSON, `"status":"passed"`) {
			testsRecorded = true
		}
		if strings.Contains(artifactsJSON, `"type":"review"`) && strings.Contains(artifactsJSON, `"target":"pr-ready-branch"`) {
			reviewArtifact = true
		}
	}
	return testsRecorded, reviewArtifact
}
