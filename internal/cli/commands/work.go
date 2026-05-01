package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
	"odin-os/internal/runner"
	"odin-os/internal/runner/codexexec"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tracker"
	trackergithub "odin-os/internal/tracker/github"
	trackerintake "odin-os/internal/tracker/intake"
	vcsgit "odin-os/internal/vcs/git"
	"odin-os/internal/vcs/worktrees"
)

const stage3LifecycleCommentMarker = "<!-- odin-stage3-lifecycle-proof -->"

const workUsage = "usage: odin work status|profiles|start --project <key> --title <text>|intake --project <key> [--dry-run]|simulate-lifecycle --issue <number> [--project <key>] [--dry-run] [--json]|apply-lifecycle --issue <number> --approved-target <repo>#<issue> [--project <key>] [--json]|worker-dry-run --issue-fixture <path> [--project <key>] [--keep-worktree] [--json]"

var newIntakeTracker = trackerintake.NewGitHubTracker

func RunWork(ctx context.Context, store *sqlite.Store, projectRegistry projects.Registry, snapshot registry.Snapshot, args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		_, err := fmt.Fprintln(stdout, workUsage)
		return err
	}

	switch args[0] {
	case "status":
		return runWorkStatus(ctx, store, snapshot, stdout)
	case "profiles":
		return runWorkProfiles(snapshot, stdout)
	case "start":
		return runWorkStart(ctx, store, projectRegistry, args[1:], stdout)
	case "intake":
		return runWorkIntake(ctx, store, projectRegistry, args[1:], stdout)
	case "simulate-lifecycle":
		return runWorkSimulateLifecycle(projectRegistry, args[1:], stdout)
	case "apply-lifecycle":
		return runWorkApplyLifecycle(ctx, projectRegistry, args[1:], stdout)
	case "worker-dry-run":
		return runWorkWorkerDryRun(ctx, projectRegistry, args[1:], stdout)
	default:
		_, err := fmt.Fprintf(stdout, "unknown work command: %s\n%s\n", args[0], workUsage)
		return err
	}
}

func runWorkStatus(ctx context.Context, store *sqlite.Store, snapshot registry.Snapshot, stdout io.Writer) error {
	taskViews, err := projections.ListTaskStatusViews(ctx, store.DB())
	if err != nil {
		return err
	}
	runViews, err := projections.ListRunSummaryViews(ctx, store.DB())
	if err != nil {
		return err
	}
	approvalViews, err := projections.ListPendingApprovalViews(ctx, store.DB())
	if err != nil {
		return err
	}

	openWorkItems := 0
	for _, view := range taskViews {
		if isOpenWorkItemStatus(view.Status) {
			openWorkItems++
		}
	}

	activeRunAttempts := 0
	for _, view := range runViews {
		if isActiveRunAttemptStatus(view.Status) {
			activeRunAttempts++
		}
	}

	_, err = fmt.Fprintf(
		stdout,
		"work_items=%d open_work_items=%d active_run_attempts=%d pending_approvals=%d delivery_profiles=%d dispatch=not_implemented intake=manual_read_only\n",
		len(taskViews),
		openWorkItems,
		activeRunAttempts,
		len(approvalViews),
		len(deliveryProfiles(snapshot)),
	)
	return err
}

func runWorkProfiles(snapshot registry.Snapshot, stdout io.Writer) error {
	profiles := deliveryProfiles(snapshot)
	if len(profiles) == 0 {
		_, err := fmt.Fprintln(stdout, "no delivery profiles")
		return err
	}

	for _, profile := range profiles {
		status := profile.Status
		if status == "" {
			status = "unknown"
		}
		if _, err := fmt.Fprintf(stdout, "%s status=%s entrypoint=%s summary=%s\n", profile.Key, status, profile.Entrypoint, profile.Summary); err != nil {
			return err
		}
	}
	return nil
}

func runWorkStart(ctx context.Context, store *sqlite.Store, projectRegistry projects.Registry, args []string, stdout io.Writer) error {
	params := parseWorkStartArgs(args)
	projectKey := strings.TrimSpace(params["project"])
	title := strings.TrimSpace(params["title"])
	if projectKey == "" || title == "" {
		_, err := fmt.Fprintln(stdout, "usage: odin work start --project <key> --title <text>")
		return err
	}

	resolved := scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: projectKey,
	}
	if projectKey == "odin-core" {
		resolved.Kind = scope.ScopeOdinCore
	}

	task, err := jobs.Service{
		Store:    store,
		Registry: projectRegistry,
	}.CreateTaskFromAct(ctx, resolved, title)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(stdout, "work_item_id=%d project=%s key=%s status=%s\n", task.ID, projectKey, task.Key, task.Status)
	return err
}

func runWorkIntake(ctx context.Context, store *sqlite.Store, projectRegistry projects.Registry, args []string, stdout io.Writer) error {
	params := parseWorkStartArgs(args)
	projectKey := strings.TrimSpace(params["project"])
	if projectKey == "" {
		_, err := fmt.Fprintln(stdout, "usage: odin work intake --project <key> [--dry-run] [--json]")
		return err
	}

	service := trackerintake.Service{
		Store:    store,
		Registry: projectRegistry,
		NewTracker: func(project projects.Manifest, options trackerintake.SyncOptions) (tracker.Tracker, error) {
			return newIntakeTracker(project, options)
		},
	}
	options := trackerintake.SyncOptions{
		ProjectKey: projectKey,
		DryRun:     parseBoolFlag(params, "dry-run") || parseEnvBool(os.Getenv("ODIN_DRY_RUN")),
	}

	if parseBoolFlag(params, "json") {
		return runWorkIntakeJSON(ctx, store, service, options, stdout)
	}

	summary, err := service.SyncProject(ctx, options)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(
		stdout,
		"project=%s repo=%s fetched=%d persisted=%d dry_run=%t dispatch=not_started prs=not_created\n",
		summary.ProjectKey,
		summary.Repo,
		summary.Fetched,
		summary.Persisted,
		summary.DryRun,
	)
	return err
}

func runWorkIntakeJSON(ctx context.Context, store *sqlite.Store, service trackerintake.Service, options trackerintake.SyncOptions, stdout io.Writer) error {
	project, ok := service.Registry.Lookup(strings.TrimSpace(options.ProjectKey))
	if !ok {
		return fmt.Errorf("unknown project %q", options.ProjectKey)
	}
	storedBefore, err := countExternalIssues(ctx, store, project.GitHub.Repo)
	if err != nil {
		return err
	}

	first, err := service.SyncProject(ctx, options)
	if err != nil {
		return err
	}
	storedAfterFirst, err := countExternalIssues(ctx, store, first.Repo)
	if err != nil {
		return err
	}

	second, err := service.SyncProject(ctx, options)
	if err != nil {
		return err
	}
	storedAfter, err := countExternalIssues(ctx, store, first.Repo)
	if err != nil {
		return err
	}

	audit := combineRequestAudits(first.Audit, second.Audit)
	if audit.Writes > 0 {
		forbidden := tracker.ForbiddenRequest{}
		if len(audit.Forbidden) > 0 {
			forbidden = audit.Forbidden[0]
		}
		return fmt.Errorf("forbidden GitHub write attempted during Stage 1 intake proof: method=%s path=%s", forbidden.Method, forbidden.Path)
	}
	report := workIntakeJSONReport{
		Project:      first.ProjectKey,
		Repo:         first.Repo,
		StoredBefore: storedBefore,
		StoredAfter:  storedAfter,
		Idempotent:   storedAfterFirst == storedAfter,
		GitHubWrites: audit.Writes,
		FirstPass:    workIntakePassReport{Fetched: first.Fetched, Persisted: first.Persisted},
		SecondPass:   workIntakePassReport{Fetched: second.Fetched, Persisted: second.Persisted},
		MethodAudit: workIntakeAuditReport{
			Reads:     audit.Reads,
			Writes:    audit.Writes,
			Forbidden: audit.Forbidden,
		},
		Dispatch: "not_started",
		PRs:      "not_created",
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func runWorkSimulateLifecycle(projectRegistry projects.Registry, args []string, stdout io.Writer) error {
	params := parseWorkStartArgs(args)
	issueText := strings.TrimSpace(params["issue"])
	if issueText == "" {
		_, err := fmt.Fprintln(stdout, "usage: odin work simulate-lifecycle --issue <number> [--project <key>] [--dry-run] [--json]")
		return err
	}
	issueNumber, err := strconv.Atoi(issueText)
	if err != nil || issueNumber <= 0 {
		return fmt.Errorf("invalid issue number %q", issueText)
	}
	dryRun := parseBoolFlag(params, "dry-run") || parseEnvBool(os.Getenv("ODIN_DRY_RUN"))
	if !dryRun {
		return fmt.Errorf("simulate-lifecycle requires ODIN_DRY_RUN=true or --dry-run")
	}

	project, err := resolveLifecycleProject(projectRegistry, strings.TrimSpace(params["project"]))
	if err != nil {
		return err
	}
	if strings.TrimSpace(project.GitHub.Repo) == "" {
		return fmt.Errorf("project %q has no GitHub repo for lifecycle simulation", project.Key)
	}

	report := buildLifecycleSimulationReport(project, issueNumber, dryRun)
	if parseBoolFlag(params, "json") {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}

	for _, log := range report.Logs {
		if _, err := fmt.Fprintln(stdout, log.Message); err != nil {
			return err
		}
	}
	return nil
}

func runWorkApplyLifecycle(ctx context.Context, projectRegistry projects.Registry, args []string, stdout io.Writer) error {
	params := parseWorkStartArgs(args)
	issueText := strings.TrimSpace(params["issue"])
	approvedTarget := strings.TrimSpace(params["approved-target"])
	if issueText == "" || approvedTarget == "" {
		_, err := fmt.Fprintln(stdout, "usage: odin work apply-lifecycle --issue <number> --approved-target <repo>#<issue> [--project <key>] [--json]")
		return err
	}
	issueNumber, err := strconv.Atoi(issueText)
	if err != nil || issueNumber <= 0 {
		return fmt.Errorf("invalid issue number %q", issueText)
	}
	if parseBoolFlag(params, "dry-run") || parseEnvBool(os.Getenv("ODIN_DRY_RUN")) {
		return fmt.Errorf("apply-lifecycle is live-only; use simulate-lifecycle for dry-run proof")
	}

	project, err := resolveLifecycleProject(projectRegistry, strings.TrimSpace(params["project"]))
	if err != nil {
		return err
	}
	if strings.TrimSpace(project.GitHub.Repo) == "" {
		return fmt.Errorf("project %q has no GitHub repo for lifecycle application", project.Key)
	}
	approvedRepo, approvedIssue, err := parseApprovedTarget(approvedTarget)
	if err != nil {
		return err
	}
	if approvedRepo != project.GitHub.Repo || approvedIssue != issueNumber {
		return fmt.Errorf("approved target %q does not match resolved target %s#%d", approvedTarget, project.GitHub.Repo, issueNumber)
	}
	owner, repoName, ok := strings.Cut(project.GitHub.Repo, "/")
	if !ok || owner == "" || repoName == "" {
		return fmt.Errorf("invalid GitHub repo %q", project.GitHub.Repo)
	}

	client := trackergithub.NewClientWithConfig(trackergithub.Config{
		BaseURL:  os.Getenv("ODIN_GITHUB_API_BASE_URL"),
		Owner:    owner,
		Repo:     repoName,
		TokenEnv: "GITHUB_TOKEN",
	})
	issueID := tracker.IssueID{Provider: "github", Repo: project.GitHub.Repo, Number: issueNumber}
	issue, err := client.FetchIssueByID(ctx, issueID)
	if err != nil {
		return err
	}
	comments, err := client.FetchIssueComments(ctx, issueID)
	if err != nil {
		return err
	}

	report := buildWorkApplyLifecycleReport(project, issueNumber, approvedTarget, issue.Labels, comments)
	projectedLabels := newLabelSet(issue.Labels)
	for _, action := range report.AppliedActions {
		switch action.Action {
		case "add_label":
			if action.Label == tracker.LabelRunning {
				if err := client.MarkInProgress(ctx, issueID); err != nil {
					return err
				}
			} else if action.Label == tracker.LabelHumanReview {
				if err := client.MarkReadyForReview(ctx, issueID); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("unsupported Stage 3 label %q", action.Label)
			}
			projectedLabels[action.Label] = true
		case "remove_label":
			if err := client.RemoveLabel(ctx, issueID, action.Label); err != nil {
				return err
			}
			delete(projectedLabels, action.Label)
		case "add_comment":
			comment, err := client.AddCommentWithResult(ctx, issueID, action.Body)
			if err != nil {
				return err
			}
			report.Comment.Created = true
			report.Comment.URL = comment.URL
		default:
			return fmt.Errorf("unsupported Stage 3 action %q", action.Action)
		}
	}
	report.After.Labels = sortedLabelSet(projectedLabels)
	audit := client.RequestAudit()
	report.MethodAudit = workLifecycleAuditReport{Reads: audit.Reads, Writes: audit.Writes}
	report.GitHubWrites = audit.Writes

	if parseBoolFlag(params, "json") {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	for _, action := range report.AppliedActions {
		if _, err := fmt.Fprintf(stdout, "applied %s %s on %s#%d\n", action.Action, action.Label, project.GitHub.Repo, issueNumber); err != nil {
			return err
		}
	}
	return nil
}

func runWorkWorkerDryRun(ctx context.Context, projectRegistry projects.Registry, args []string, stdout io.Writer) error {
	params := parseWorkStartArgs(args)
	fixturePath := strings.TrimSpace(params["issue-fixture"])
	if fixturePath == "" {
		_, err := fmt.Fprintln(stdout, "usage: odin work worker-dry-run --issue-fixture <path> [--project <key>] [--keep-worktree] [--json]")
		return err
	}
	project, err := resolveLifecycleProject(projectRegistry, strings.TrimSpace(params["project"]))
	if err != nil {
		return err
	}
	if strings.TrimSpace(project.GitRoot) == "" {
		return fmt.Errorf("project %q has no git root for worker dry-run", project.Key)
	}
	issue, err := readWorkerDryRunIssueFixture(fixturePath)
	if err != nil {
		return err
	}

	worktreeRoot := strings.TrimSpace(os.Getenv("ODIN_WORKTREE_ROOT"))
	if worktreeRoot == "" {
		worktreeRoot = worktrees.DefaultRoot()
	}
	nonce := time.Now().UnixNano()
	branchName := fmt.Sprintf("odin/stage4-dry-run/issue-%d-%d", issue.Number, nonce)
	worktreePath := worktrees.ResolvePath(worktrees.PathParams{
		Root:       worktreeRoot,
		ProjectKey: project.Key + "-stage4-dry-run",
		TaskID:     int64(issue.Number),
		RunID:      nonce,
		Try:        1,
	})
	rootAbs, pathAbs, insideRoot, err := provePathInsideRoot(worktreeRoot, worktreePath)
	if err != nil {
		return err
	}
	if !insideRoot {
		return fmt.Errorf("worker dry-run worktree path %q escaped root %q", worktreePath, worktreeRoot)
	}
	if err := os.MkdirAll(filepath.Dir(pathAbs), 0o755); err != nil {
		return err
	}

	git := vcsgit.Adapter{}
	baseBranch := strings.TrimSpace(project.DefaultBranch)
	if baseBranch == "" {
		baseBranch = "HEAD"
	}
	if err := git.CreateBranch(ctx, project.GitRoot, branchName, baseBranch); err != nil {
		return err
	}
	created := false
	cleanedUp := false
	kept := parseBoolFlag(params, "keep-worktree")
	defer func() {
		if !created || kept {
			return
		}
		_ = git.RemoveWorktree(context.Background(), project.GitRoot, pathAbs)
		_ = deleteGitBranch(context.Background(), project.GitRoot, branchName)
	}()
	if err := git.AddWorktree(ctx, project.GitRoot, pathAbs, branchName); err != nil {
		_ = deleteGitBranch(context.Background(), project.GitRoot, branchName)
		return err
	}
	created = true

	prompt := renderWorkerDryRunPrompt(project, issue)
	secrets := collectKnownSecretValues()
	command, err := codexexec.BuildCommand(codexexec.Config{
		SecretValues: secrets,
		Timeout:      30 * time.Minute,
	}, runner.Request{
		WorkItemID:  fmt.Sprintf("issue-%d", issue.Number),
		Role:        "builder",
		Worktree:    pathAbs,
		Prompt:      prompt,
		DryRun:      true,
		SandboxMode: "workspace-write",
		Timeout:     30 * time.Minute,
	})
	if err != nil {
		return err
	}

	if !kept {
		if err := git.RemoveWorktree(ctx, project.GitRoot, pathAbs); err != nil {
			return err
		}
		if err := deleteGitBranch(ctx, project.GitRoot, branchName); err != nil {
			return err
		}
		cleanedUp = true
		created = false
	}

	report := workWorkerDryRunReport{
		Project: project.Key,
		Issue: workWorkerDryRunIssueReport{
			Number: issue.Number,
			Title:  issue.Title,
		},
		Worktree: workWorkerDryRunWorktreeReport{
			Root:        rootAbs,
			Path:        pathAbs,
			Branch:      branchName,
			Created:     true,
			InsideRoot:  insideRoot,
			CleanedUp:   cleanedUp,
			Kept:        kept,
			LeaseStored: false,
		},
		Prompt:     codexexec.Redact(prompt, secrets),
		Guardrails: workerDryRunGuardrails(),
		CodexCommand: workWorkerDryRunCommandReport{
			Executable: command.Path,
			Args:       command.Args,
			Redacted:   codexexec.RedactCommand(command, secrets),
			Launched:   false,
		},
		Environment: workWorkerDryRunEnvironmentReport{
			Excluded:      workerDryRunExcludedEnv(),
			TokenExposure: false,
		},
		Timeout: workWorkerDryRunTimeoutReport{
			Simulated: true,
			Result:    "simulated codex exec timed out after 1ms; process was not launched",
		},
		FinalOutput:    "dry-run worker final output: run make odin-e2e-local before handoff",
		GitHubWrites:   0,
		PRs:            "not_created",
		Dispatch:       "not_started",
		CodexExecution: "not_started",
	}

	if parseBoolFlag(params, "json") {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	_, err = fmt.Fprintf(stdout, "worker_dry_run issue=%d worktree=%s codex_execution=not_started final_output=%q\n", issue.Number, pathAbs, report.FinalOutput)
	return err
}

func resolveLifecycleProject(projectRegistry projects.Registry, projectKey string) (projects.Manifest, error) {
	if projectKey != "" {
		project, ok := projectRegistry.Lookup(projectKey)
		if !ok {
			return projects.Manifest{}, fmt.Errorf("unknown project %q", projectKey)
		}
		return project, nil
	}
	project, ok := projectRegistry.SystemProject()
	if !ok {
		return projects.Manifest{}, fmt.Errorf("no system project registered for lifecycle simulation")
	}
	return project, nil
}

func readWorkerDryRunIssueFixture(path string) (workWorkerDryRunIssueFixture, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return workWorkerDryRunIssueFixture{}, err
	}
	var issue workWorkerDryRunIssueFixture
	if err := json.Unmarshal(content, &issue); err != nil {
		return workWorkerDryRunIssueFixture{}, fmt.Errorf("decode issue fixture: %w", err)
	}
	if issue.Number <= 0 {
		return workWorkerDryRunIssueFixture{}, fmt.Errorf("issue fixture must include positive number")
	}
	if strings.TrimSpace(issue.Title) == "" {
		issue.Title = fmt.Sprintf("Issue %d", issue.Number)
	}
	return issue, nil
}

func renderWorkerDryRunPrompt(project projects.Manifest, issue workWorkerDryRunIssueFixture) string {
	return strings.Join([]string{
		"You are implementing one Odin Work Item in a temporary local dry-run worktree.",
		"",
		fmt.Sprintf("Project: %s", project.Key),
		fmt.Sprintf("Repository: %s", project.GitHub.Repo),
		fmt.Sprintf("Issue: #%d %s", issue.Number, issue.Title),
		"",
		"Brownfield guardrails:",
		"- Audit the existing repo before editing.",
		"- Reuse existing Odin commands, services, contracts, registries, schemas, docs, and tests.",
		"- Do not create parallel command surfaces, registries, or sidecar tools.",
		"- Use odin work ... as the canonical Delivery Workflow operator surface.",
		"- Do not expose tokens or secrets.",
		"- Do not create or update a pull request.",
		"- Final output must include make odin-e2e-local.",
		"",
		"Required final output:",
		"- Changed files",
		"- Verification run, including make odin-e2e-local or a clear reason it was not run",
		"- Remaining risks",
	}, "\n")
}

func workerDryRunGuardrails() map[string]bool {
	return map[string]bool{
		"audit_existing_repo":            true,
		"reuse_existing_odin_primitives": true,
		"no_parallel_surfaces":           true,
		"canonical_odin_work_surface":    true,
		"no_tokens_or_secrets":           true,
		"no_pr_creation":                 true,
		"run_make_odin_e2e_local":        true,
	}
}

func workerDryRunExcludedEnv() []string {
	return []string{"GITHUB_TOKEN", "GH_TOKEN", "API_TOKEN", "ODIN_TRADEBOARD_API_TOKEN"}
}

func collectKnownSecretValues() []string {
	var secrets []string
	for _, key := range workerDryRunExcludedEnv() {
		if value := os.Getenv(key); value != "" {
			secrets = append(secrets, value)
		}
	}
	return secrets
}

func provePathInsideRoot(root string, path string) (string, string, bool, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(expandTilde(root)))
	if err != nil {
		return "", "", false, err
	}
	pathAbs, err := filepath.Abs(filepath.Clean(expandTilde(path)))
	if err != nil {
		return "", "", false, err
	}
	relative, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return "", "", false, err
	}
	inside := relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
	return rootAbs, pathAbs, inside, nil
}

func expandTilde(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func deleteGitBranch(ctx context.Context, repoRoot string, branchName string) error {
	command := exec.CommandContext(ctx, "git", "-C", repoRoot, "branch", "-D", branchName)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git branch -D %s: %w: %s", branchName, err, string(output))
	}
	return nil
}

func buildWorkApplyLifecycleReport(project projects.Manifest, issueNumber int, approvedTarget string, labels []string, comments []tracker.IssueComment) workApplyLifecycleReport {
	labelSet := newLabelSet(labels)
	commentExists := false
	commentURL := ""
	for _, comment := range comments {
		if strings.Contains(comment.Body, stage3LifecycleCommentMarker) {
			commentExists = true
			commentURL = comment.URL
			break
		}
	}

	actions := []workLifecyclePlannedAction{}
	if !labelSet[tracker.LabelHumanReview] {
		if !labelSet[tracker.LabelRunning] {
			actions = append(actions, workLifecyclePlannedAction{Sequence: len(actions) + 1, Action: "add_label", Label: tracker.LabelRunning})
			labelSet[tracker.LabelRunning] = true
		}
		if labelSet[tracker.LabelRunning] {
			actions = append(actions, workLifecyclePlannedAction{Sequence: len(actions) + 1, Action: "remove_label", Label: tracker.LabelRunning})
			delete(labelSet, tracker.LabelRunning)
		}
		actions = append(actions, workLifecyclePlannedAction{Sequence: len(actions) + 1, Action: "add_label", Label: tracker.LabelHumanReview})
		labelSet[tracker.LabelHumanReview] = true
	}
	if !commentExists {
		actions = append(actions, workLifecyclePlannedAction{
			Sequence: len(actions) + 1,
			Action:   "add_comment",
			Body:     stage3LifecycleCommentMarker + "\nStage 3 controlled lifecycle proof.",
		})
	}

	return workApplyLifecycleReport{
		Project: project.Key,
		Repo:    project.GitHub.Repo,
		Issue:   issueNumber,
		DryRun:  false,
		Approval: workLifecycleApprovalReport{
			ApprovedTarget: approvedTarget,
			OperatorSource: "command_flag",
		},
		Before:         workLifecycleLabelsReport{Labels: sortedStrings(labels)},
		After:          workLifecycleLabelsReport{Labels: sortedLabelSet(labelSet)},
		AppliedActions: actions,
		Comment: workLifecycleCommentReport{
			Created: false,
			URL:     commentURL,
			Marker:  stage3LifecycleCommentMarker,
		},
		Dispatch:       "not_started",
		PRs:            "not_created",
		CodexExecution: "not_started",
	}
}

func buildLifecycleSimulationReport(project projects.Manifest, issueNumber int, dryRun bool) workLifecycleSimulationReport {
	reason := "Stage 2 dry-run lifecycle proof: simulated failure path."
	actions := []workLifecyclePlannedAction{
		{Sequence: 1, Action: "add_label", Label: tracker.LabelRunning},
		{Sequence: 2, Action: "add_label", Label: tracker.LabelHumanReview},
		{Sequence: 3, Action: "add_label", Label: tracker.LabelFailed},
		{Sequence: 4, Action: "add_comment", Body: reason},
	}
	logs := make([]workLifecycleLog, 0, len(actions))
	for _, action := range actions {
		switch action.Action {
		case "add_label":
			logs = append(logs, workLifecycleLog{
				Level:   "info",
				Message: fmt.Sprintf("planned add_label %s on %s#%d", action.Label, project.GitHub.Repo, issueNumber),
			})
		case "add_comment":
			logs = append(logs, workLifecycleLog{
				Level:   "info",
				Message: fmt.Sprintf("planned add_comment on %s#%d", project.GitHub.Repo, issueNumber),
			})
		}
	}

	tokenPresent := os.Getenv("GITHUB_TOKEN") != ""
	tokenValue := ""
	if tokenPresent {
		tokenValue = "[REDACTED]"
	}
	return workLifecycleSimulationReport{
		Project:        project.Key,
		Repo:           project.GitHub.Repo,
		Issue:          issueNumber,
		DryRun:         dryRun,
		GitHubWrites:   0,
		PlannedActions: actions,
		Logs:           logs,
		MethodAudit:    workLifecycleAuditReport{Reads: 0, Writes: 0},
		Redaction: workLifecycleRedactionReport{
			TokenEnv:      "GITHUB_TOKEN",
			TokenPresent:  tokenPresent,
			TokenRedacted: true,
			TokenValue:    tokenValue,
		},
		Dispatch:       "not_started",
		PRs:            "not_created",
		CodexExecution: "not_started",
	}
}

type workIntakeJSONReport struct {
	Project      string                `json:"project"`
	Repo         string                `json:"repo"`
	StoredBefore int                   `json:"stored_before"`
	StoredAfter  int                   `json:"stored_after"`
	Idempotent   bool                  `json:"idempotent"`
	GitHubWrites int                   `json:"github_writes"`
	FirstPass    workIntakePassReport  `json:"first_pass"`
	SecondPass   workIntakePassReport  `json:"second_pass"`
	MethodAudit  workIntakeAuditReport `json:"method_audit"`
	Dispatch     string                `json:"dispatch"`
	PRs          string                `json:"prs"`
}

type workIntakePassReport struct {
	Fetched   int `json:"fetched"`
	Persisted int `json:"persisted"`
}

type workIntakeAuditReport struct {
	Reads     int                        `json:"reads"`
	Writes    int                        `json:"writes"`
	Forbidden []tracker.ForbiddenRequest `json:"forbidden,omitempty"`
}

type workLifecycleSimulationReport struct {
	Project        string                       `json:"project"`
	Repo           string                       `json:"repo"`
	Issue          int                          `json:"issue"`
	DryRun         bool                         `json:"dry_run"`
	GitHubWrites   int                          `json:"github_writes"`
	PlannedActions []workLifecyclePlannedAction `json:"planned_actions"`
	Logs           []workLifecycleLog           `json:"logs"`
	MethodAudit    workLifecycleAuditReport     `json:"method_audit"`
	Redaction      workLifecycleRedactionReport `json:"redaction"`
	Dispatch       string                       `json:"dispatch"`
	PRs            string                       `json:"prs"`
	CodexExecution string                       `json:"codex_execution"`
}

type workApplyLifecycleReport struct {
	Project        string                       `json:"project"`
	Repo           string                       `json:"repo"`
	Issue          int                          `json:"issue"`
	DryRun         bool                         `json:"dry_run"`
	GitHubWrites   int                          `json:"github_writes"`
	Approval       workLifecycleApprovalReport  `json:"approval"`
	Before         workLifecycleLabelsReport    `json:"before"`
	After          workLifecycleLabelsReport    `json:"after"`
	AppliedActions []workLifecyclePlannedAction `json:"applied_actions"`
	Comment        workLifecycleCommentReport   `json:"comment"`
	MethodAudit    workLifecycleAuditReport     `json:"method_audit"`
	Dispatch       string                       `json:"dispatch"`
	PRs            string                       `json:"prs"`
	CodexExecution string                       `json:"codex_execution"`
}

type workWorkerDryRunIssueFixture struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

type workWorkerDryRunReport struct {
	Project        string                            `json:"project"`
	Issue          workWorkerDryRunIssueReport       `json:"issue"`
	Worktree       workWorkerDryRunWorktreeReport    `json:"worktree"`
	Prompt         string                            `json:"prompt"`
	Guardrails     map[string]bool                   `json:"guardrails"`
	CodexCommand   workWorkerDryRunCommandReport     `json:"codex_command"`
	Environment    workWorkerDryRunEnvironmentReport `json:"environment"`
	Timeout        workWorkerDryRunTimeoutReport     `json:"timeout"`
	FinalOutput    string                            `json:"final_output"`
	GitHubWrites   int                               `json:"github_writes"`
	PRs            string                            `json:"prs"`
	Dispatch       string                            `json:"dispatch"`
	CodexExecution string                            `json:"codex_execution"`
}

type workWorkerDryRunIssueReport struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

type workWorkerDryRunWorktreeReport struct {
	Root        string `json:"root"`
	Path        string `json:"path"`
	Branch      string `json:"branch"`
	Created     bool   `json:"created"`
	InsideRoot  bool   `json:"inside_root"`
	CleanedUp   bool   `json:"cleaned_up"`
	Kept        bool   `json:"kept"`
	LeaseStored bool   `json:"lease_stored"`
}

type workWorkerDryRunCommandReport struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
	Redacted   string   `json:"redacted"`
	Launched   bool     `json:"launched"`
}

type workWorkerDryRunEnvironmentReport struct {
	Excluded      []string `json:"excluded"`
	TokenExposure bool     `json:"token_exposure"`
}

type workWorkerDryRunTimeoutReport struct {
	Simulated bool   `json:"simulated"`
	Result    string `json:"result"`
}

type workLifecycleApprovalReport struct {
	ApprovedTarget string `json:"approved_target"`
	OperatorSource string `json:"operator_source"`
}

type workLifecycleLabelsReport struct {
	Labels []string `json:"labels"`
}

type workLifecycleCommentReport struct {
	Created bool   `json:"created"`
	URL     string `json:"url,omitempty"`
	Marker  string `json:"marker"`
}

type workLifecyclePlannedAction struct {
	Sequence int    `json:"sequence"`
	Action   string `json:"action"`
	Label    string `json:"label,omitempty"`
	Body     string `json:"body,omitempty"`
}

type workLifecycleLog struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type workLifecycleAuditReport struct {
	Reads  int `json:"reads"`
	Writes int `json:"writes"`
}

type workLifecycleRedactionReport struct {
	TokenEnv      string `json:"token_env"`
	TokenPresent  bool   `json:"token_present"`
	TokenRedacted bool   `json:"token_redacted"`
	TokenValue    string `json:"token_value,omitempty"`
}

func countExternalIssues(ctx context.Context, store *sqlite.Store, repo string) (int, error) {
	issues, err := store.ListExternalIssues(ctx, sqlite.ListExternalIssuesParams{
		Repo:       repo,
		SyncStatus: "eligible",
	})
	if err != nil {
		return 0, err
	}
	return len(issues), nil
}

func combineRequestAudits(audits ...tracker.RequestAudit) tracker.RequestAudit {
	combined := tracker.RequestAudit{}
	for _, audit := range audits {
		combined.Reads += audit.Reads
		combined.Writes += audit.Writes
		combined.Forbidden = append(combined.Forbidden, audit.Forbidden...)
	}
	return combined
}

func parseApprovedTarget(value string) (string, int, error) {
	repo, issueText, ok := strings.Cut(strings.TrimSpace(value), "#")
	if !ok || strings.TrimSpace(repo) == "" || strings.TrimSpace(issueText) == "" {
		return "", 0, fmt.Errorf("approved target must be <repo>#<issue>")
	}
	issue, err := strconv.Atoi(strings.TrimSpace(issueText))
	if err != nil || issue <= 0 {
		return "", 0, fmt.Errorf("invalid approved target issue %q", issueText)
	}
	return strings.TrimSpace(repo), issue, nil
}

func newLabelSet(labels []string) map[string]bool {
	labelSet := make(map[string]bool, len(labels))
	for _, label := range labels {
		if strings.TrimSpace(label) != "" {
			labelSet[label] = true
		}
	}
	return labelSet
}

func sortedLabelSet(labelSet map[string]bool) []string {
	labels := make([]string, 0, len(labelSet))
	for label := range labelSet {
		labels = append(labels, label)
	}
	return sortedStrings(labels)
}

func sortedStrings(values []string) []string {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	return sorted
}

func parseWorkStartArgs(args []string) map[string]string {
	values := make(map[string]string)
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if strings.HasPrefix(arg, "--") {
			key := strings.TrimPrefix(arg, "--")
			if next := index + 1; next >= len(args) || strings.HasPrefix(args[next], "--") {
				values[key] = "true"
				continue
			}
			if next := index + 1; next < len(args) {
				values[key] = args[next]
				index = next
			}
			continue
		}
		if key, value, ok := strings.Cut(arg, "="); ok {
			values[strings.TrimLeft(key, "-")] = value
		}
	}
	return values
}

func parseBoolFlag(values map[string]string, key string) bool {
	value := strings.ToLower(strings.TrimSpace(values[key]))
	return parseEnvBool(value)
}

func parseEnvBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

func deliveryProfiles(snapshot registry.Snapshot) []registry.Item {
	var profiles []registry.Item
	for _, workflow := range snapshot.ByKind[registry.KindWorkflow] {
		for _, tag := range workflow.Tags {
			if strings.EqualFold(tag, "delivery_profile") {
				profiles = append(profiles, workflow)
				break
			}
		}
	}
	sort.Slice(profiles, func(i int, j int) bool {
		return profiles[i].Key < profiles[j].Key
	})
	return profiles
}

func isOpenWorkItemStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "completed", "failed", "cancelled", "canceled":
		return false
	default:
		return true
	}
}

func isActiveRunAttemptStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running", "started":
		return true
	default:
		return false
	}
}
