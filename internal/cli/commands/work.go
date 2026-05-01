package commands

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	"odin-os/internal/runtime/supervision"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tracker"
	trackergithub "odin-os/internal/tracker/github"
	trackerintake "odin-os/internal/tracker/intake"
	vcsgit "odin-os/internal/vcs/git"
	"odin-os/internal/vcs/worktrees"
)

const stage3LifecycleCommentMarker = "<!-- odin-stage3-lifecycle-proof -->"
const stage6ReviewEvidenceMarker = "<!-- odin-stage6-review-evidence -->"
const stage6HumanReviewHandoffMarker = "<!-- odin-stage6-human-review-handoff -->"

const workUsage = "usage: odin work status|profiles|start --project <key> --title <text>|intake --project <key> [--dry-run]|simulate-lifecycle --issue <number> [--project <key>] [--dry-run] [--json]|apply-lifecycle --issue <number> --approved-target <repo>#<issue> [--project <key>] [--json]|worker-dry-run --issue-fixture <path> [--project <key>] [--keep-worktree] [--json]|pr-dry-run --worktree <path> --base <branch> [--json]|pr-create --issue <number> --approved-target <repo>#<issue> --worktree <path> --base <branch> --wait-ci [--json]|supervise status|start|stop|queue --project <key> [--fixture-issue <number>]|recover|e2e prepare-issue --project <key> --json|e2e run-once --project <key> --issue <number> --json"

const workSuperviseUsage = "usage: odin work supervise status|start|stop|queue --project <key> [--fixture-issue <number>]|recover|e2e prepare-issue --project <key> --json|e2e run-once --project <key> --issue <number> --json"

const workSuperviseFixtureSource = "control_plane_fixture"
const workSuperviseTrackerSource = "issue_intake_source"
const workSuperviseRedactedSensitivePath = "internal/security/redacted-sensitive-path.txt"

var newIntakeTracker = trackerintake.NewGitHubTracker
var runSupervisedE2EWorker = defaultRunSupervisedE2EWorker

const supervisedE2EWorkerTimeout = 30 * time.Minute

var supervisedE2ETokenPattern = regexp.MustCompile(`(?i)(github_pat_[A-Za-z0-9_]{20,}|ghp_[A-Za-z0-9_]{20,}|gho_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_-]{20,}|xox[baprs]-[A-Za-z0-9-]{20,})`)

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
	case "pr-dry-run":
		return runWorkPRDryRun(ctx, args[1:], stdout)
	case "pr-create":
		return runWorkPRCreate(ctx, projectRegistry, args[1:], stdout)
	case "supervise":
		return runWorkSupervise(ctx, store, projectRegistry, args[1:], stdout)
	default:
		_, err := fmt.Fprintf(stdout, "unknown work command: %s\n%s\n", args[0], workUsage)
		return err
	}
}

type workSuperviseReport struct {
	Mode           string                      `json:"mode"`
	Source         string                      `json:"source,omitempty"`
	Enabled        bool                        `json:"enabled"`
	KillSwitch     bool                        `json:"kill_switch"`
	ConfigHash     string                      `json:"config_hash"`
	Queue          []supervision.QueueDecision `json:"queue"`
	Claims         []supervision.PlannedClaim  `json:"claims"`
	Recovery       supervision.RecoveryReport  `json:"recovery"`
	CodexExecution string                      `json:"codex_execution"`
	PRs            string                      `json:"prs"`
	Merge          string                      `json:"merge"`
	Deployment     string                      `json:"deployment"`
}

func runWorkSupervise(ctx context.Context, store *sqlite.Store, projectRegistry projects.Registry, args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		_, err := fmt.Fprintln(stdout, workSuperviseUsage)
		return err
	}

	params := parseWorkStartArgs(args[1:])
	switch args[0] {
	case "status", "start", "stop", "queue", "recover", "e2e":
	default:
		_, err := fmt.Fprintf(stdout, "unknown work supervise command: %s\n%s\n", args[0], workSuperviseUsage)
		return err
	}
	if !parseBoolFlag(params, "json") {
		return fmt.Errorf("--json is required for work supervise in this slice\n%s", workSuperviseUsage)
	}
	if args[0] == "e2e" {
		return runWorkSuperviseE2E(ctx, store, projectRegistry, args[1:], params, stdout)
	}

	service := supervision.NewService(store, supervision.DefaultConfig())
	var report supervision.Report
	var err error
	switch args[0] {
	case "status":
		report, err = service.Status(ctx)
	case "start":
		report, err = service.Start(ctx, "odin-work-supervise")
	case "stop":
		report, err = service.Stop(ctx, "odin-work-supervise")
	case "queue":
		report, err = runWorkSuperviseQueue(ctx, store, projectRegistry, service, params)
	case "recover":
		report, err = service.Recover(ctx)
	}
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(flattenWorkSuperviseReport(report, args[0], params))
}

func runWorkSuperviseE2E(ctx context.Context, store *sqlite.Store, projectRegistry projects.Registry, args []string, params map[string]string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		return fmt.Errorf("missing work supervise e2e command\n%s", workSuperviseUsage)
	}

	projectKey := strings.TrimSpace(params["project"])
	switch args[0] {
	case "prepare-issue":
		if projectKey == "" {
			return fmt.Errorf("missing --project for work supervise e2e prepare-issue")
		}
		manifest, ok := projectRegistry.Lookup(projectKey)
		if !ok {
			return fmt.Errorf("unknown project %q", projectKey)
		}
		return runWorkSuperviseE2EPrepareIssue(ctx, manifest, stdout)
	case "run-once":
		if projectKey == "" {
			return fmt.Errorf("missing --project for work supervise e2e run-once")
		}
		manifest, ok := projectRegistry.Lookup(projectKey)
		if !ok {
			return fmt.Errorf("unknown project %q", projectKey)
		}
		issueText := strings.TrimSpace(params["issue"])
		if issueText == "" {
			return fmt.Errorf("missing --issue for work supervise e2e run-once")
		}
		issueNumber, err := strconv.Atoi(issueText)
		if err != nil || issueNumber <= 0 {
			return fmt.Errorf("invalid --issue %q", issueText)
		}
		return runWorkSuperviseE2ERunOnce(ctx, store, manifest, issueNumber, stdout)
	default:
		return fmt.Errorf("unknown work supervise e2e command: %s\n%s", args[0], workSuperviseUsage)
	}
}

type workSuperviseE2EReport struct {
	Mode               string                       `json:"mode"`
	Phase              string                       `json:"phase"`
	Status             string                       `json:"status"`
	Project            string                       `json:"project"`
	Repo               string                       `json:"repo"`
	RunID              string                       `json:"run_id"`
	Issue              workSuperviseE2EIssueReport  `json:"issue"`
	Queue              []supervision.QueueDecision  `json:"queue,omitempty"`
	Claims             []supervision.PlannedClaim   `json:"claims,omitempty"`
	Worktree           workSuperviseE2EWorktree     `json:"worktree,omitempty"`
	Diff               workSuperviseE2EDiff         `json:"diff,omitempty"`
	CodexExecution     string                       `json:"codex_execution,omitempty"`
	PRs                string                       `json:"prs"`
	Merge              string                       `json:"merge"`
	Deployment         string                       `json:"deployment"`
	HumanMergeRequired bool                         `json:"human_merge_required"`
	Artifacts          workSuperviseE2EArtifactRefs `json:"artifacts,omitempty"`
}

type workSuperviseE2EIssueReport struct {
	Number      int    `json:"number"`
	URL         string `json:"url"`
	PlannedPath string `json:"planned_path"`
}

type workSuperviseE2EArtifactRefs struct {
	PreparedIssue string `json:"prepared_issue,omitempty"`
	QueueReport   string `json:"queue_report,omitempty"`
	FinalReport   string `json:"final_report,omitempty"`
	WorkerPrompt  string `json:"worker_prompt,omitempty"`
	WorkerCommand string `json:"worker_command,omitempty"`
	WorkerOutput  string `json:"worker_output,omitempty"`
	DiffSummary   string `json:"diff_summary,omitempty"`
}

type workSuperviseE2EWorktree struct {
	Root       string `json:"root,omitempty"`
	Path       string `json:"path,omitempty"`
	Branch     string `json:"branch,omitempty"`
	InsideRoot bool   `json:"inside_root,omitempty"`
	Kept       bool   `json:"kept,omitempty"`
}

type workSuperviseE2EDiff struct {
	Files  []string `json:"files,omitempty"`
	SHA256 string   `json:"sha256,omitempty"`
}

type supervisedE2EWorkerRequest struct {
	WorktreePath string
	BranchName   string
	PlannedPath  string
	Prompt       string
	Command      codexexec.Command
	Secrets      []string
}

type supervisedE2EWorkerResult struct {
	Output string
}

type workSuperviseE2EWorkerCommandArtifact struct {
	Executable  string `json:"executable"`
	Redacted    string `json:"redacted"`
	Worktree    string `json:"worktree"`
	SandboxMode string `json:"sandbox_mode"`
	Timeout     string `json:"timeout"`
	Launched    bool   `json:"launched"`
}

type workSuperviseE2EPreparedIssueArtifact struct {
	Project     string   `json:"project"`
	Repo        string   `json:"repo"`
	RunID       string   `json:"run_id"`
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	Labels      []string `json:"labels"`
	Number      int      `json:"number"`
	URL         string   `json:"url"`
	PlannedPath string   `json:"planned_path"`
}

func runWorkSuperviseE2EPrepareIssue(ctx context.Context, manifest projects.Manifest, stdout io.Writer) error {
	repoID := strings.TrimSpace(manifest.GitHub.Repo)
	if repoID == "" {
		return fmt.Errorf("project %q has no GitHub repo", manifest.Key)
	}
	owner, repoName, ok := strings.Cut(repoID, "/")
	if !ok || owner == "" || repoName == "" {
		return fmt.Errorf("invalid GitHub repo %q", repoID)
	}

	now := time.Now().UTC()
	runID := fmt.Sprintf("%d", now.UnixNano())
	plannedPath := fmt.Sprintf("docs/operations/stage-7-supervised-e2e-%s-%s.md", now.Format("2006-01-02"), runID)
	title := fmt.Sprintf("Stage 7 supervised E2E docs proof %s", runID)
	body := strings.Join([]string{
		"Stage 7 supervised E2E docs proof.",
		"",
		"Planned scope: " + plannedPath,
		"",
		"Boundaries: docs-only setup issue; no PR, merge, deployment, worker execution, queue dispatch, claim creation, or scheduler behavior.",
	}, "\n")
	labels := []string{"odin:ready", "safety:low-risk"}

	runDir, preparedIssuePath, finalReportPath, err := prepareSupervisedE2EArtifactTargets(runID)
	if err != nil {
		return redactWorkSuperviseE2EError(err)
	}

	client := trackergithub.NewClientWithConfig(trackergithub.Config{
		BaseURL:  os.Getenv("ODIN_GITHUB_API_BASE_URL"),
		Owner:    owner,
		Repo:     repoName,
		TokenEnv: "GITHUB_TOKEN",
	})
	issue, err := client.CreateFollowUpIssue(ctx, tracker.FollowUpIssue{
		Repo:   repoID,
		Title:  title,
		Body:   body,
		Labels: labels,
	})
	if err != nil {
		_ = os.RemoveAll(runDir)
		return redactWorkSuperviseE2EError(err)
	}

	report := workSuperviseE2EReport{
		Mode:    "supervised_e2e",
		Phase:   "prepared",
		Status:  "prepared",
		Project: manifest.Key,
		Repo:    repoID,
		RunID:   runID,
		Issue: workSuperviseE2EIssueReport{
			Number:      issue.Number,
			URL:         issue.URL,
			PlannedPath: plannedPath,
		},
		PRs:                "not_created",
		Merge:              "not_performed",
		Deployment:         "not_started",
		HumanMergeRequired: true,
	}

	report.Artifacts = workSuperviseE2EArtifactRefs{
		PreparedIssue: preparedIssuePath,
		FinalReport:   finalReportPath,
	}

	preparedIssue := workSuperviseE2EPreparedIssueArtifact{
		Project:     manifest.Key,
		Repo:        repoID,
		RunID:       runID,
		Title:       title,
		Body:        body,
		Labels:      labels,
		Number:      issue.Number,
		URL:         issue.URL,
		PlannedPath: plannedPath,
	}
	if err := writeRedactedJSONArtifact(preparedIssuePath, preparedIssue); err != nil {
		return redactWorkSuperviseE2EError(err)
	}
	if err := writeRedactedJSONArtifact(finalReportPath, report); err != nil {
		return redactWorkSuperviseE2EError(err)
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return redactWorkSuperviseE2EError(err)
	}
	return nil
}

func runWorkSuperviseE2ERunOnce(ctx context.Context, store *sqlite.Store, manifest projects.Manifest, issueNumber int, stdout io.Writer) error {
	repoID := strings.TrimSpace(manifest.GitHub.Repo)
	if repoID == "" {
		return fmt.Errorf("project %q has no GitHub repo", manifest.Key)
	}

	runID := fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	runDir, queueReportPath, finalReportPath, err := prepareSupervisedE2ERunOnceArtifactTargets(runID)
	if err != nil {
		return redactWorkSuperviseE2EError(err)
	}
	artifactRefs := supervisedE2ERunOnceArtifactRefs(runDir, queueReportPath, finalReportPath)

	source, err := newIntakeTracker(manifest, trackerintake.SyncOptions{})
	if err != nil {
		return redactWorkSuperviseE2EError(err)
	}
	fetched, err := source.FetchIssueByID(ctx, tracker.IssueID{Provider: "github", Repo: repoID, Number: issueNumber})
	if err != nil {
		return writeRunOnceFailureArtifactsAndError(queueReportPath, finalReportPath, map[string]any{
			"mode":            "supervised_e2e",
			"phase":           "fetch_issue",
			"status":          "failed_closed",
			"project":         manifest.Key,
			"repo":            repoID,
			"run_id":          runID,
			"requested_issue": issueNumber,
			"error":           err.Error(),
			"codex_execution": supervision.SideEffectNotStarted,
			"prs":             supervision.SideEffectNotCreated,
			"merge":           supervision.SideEffectNotMerged,
			"deployment":      supervision.SideEffectNotStarted,
		}, fmt.Errorf("fetch issue %s#%d: %w", repoID, issueNumber, err))
	}
	if auditor, ok := source.(tracker.RequestAuditor); ok {
		audit := auditor.RequestAudit()
		if audit.Writes > 0 {
			message := "forbidden GitHub write attempted during supervise e2e run-once intake"
			if len(audit.Forbidden) > 0 {
				forbidden := audit.Forbidden[0]
				message = fmt.Sprintf("%s: method=%s path=%s", message, forbidden.Method, forbidden.Path)
			}
			return writeRunOnceFailureArtifactsAndError(queueReportPath, finalReportPath, map[string]any{
				"mode":            "supervised_e2e",
				"phase":           "fetch_issue",
				"status":          "failed_closed",
				"project":         manifest.Key,
				"repo":            repoID,
				"run_id":          runID,
				"requested_issue": issueNumber,
				"error":           message,
				"codex_execution": supervision.SideEffectNotStarted,
				"prs":             supervision.SideEffectNotCreated,
				"merge":           supervision.SideEffectNotMerged,
				"deployment":      supervision.SideEffectNotStarted,
			}, fmt.Errorf("%s", message))
		}
	}
	if fetched.Number != issueNumber {
		return writeRunOnceFailureArtifactsAndError(queueReportPath, finalReportPath, map[string]any{
			"mode":            "supervised_e2e",
			"phase":           "fetch_issue",
			"status":          "failed_closed",
			"project":         manifest.Key,
			"repo":            repoID,
			"run_id":          runID,
			"requested_issue": issueNumber,
			"fetched_issue":   fetched.Number,
			"codex_execution": supervision.SideEffectNotStarted,
			"prs":             supervision.SideEffectNotCreated,
			"merge":           supervision.SideEffectNotMerged,
			"deployment":      supervision.SideEffectNotStarted,
		}, fmt.Errorf("fetched issue number %d differs from requested --issue %d", fetched.Number, issueNumber))
	}
	if err := validateSupervisedE2ERawPlannedScope(fetched.Body); err != nil {
		return writeRunOnceFailureArtifactsAndError(queueReportPath, finalReportPath, map[string]any{
			"mode":            "supervised_e2e",
			"phase":           "validate_issue",
			"status":          "failed_closed",
			"project":         manifest.Key,
			"repo":            repoID,
			"run_id":          runID,
			"requested_issue": issueNumber,
			"codex_execution": supervision.SideEffectNotStarted,
			"prs":             supervision.SideEffectNotCreated,
			"merge":           supervision.SideEffectNotMerged,
			"deployment":      supervision.SideEffectNotStarted,
		}, err)
	}

	issues := supervisionIssuesFromTrackerIssues([]tracker.Issue{fetched}, repoID)
	if len(issues) != 1 {
		return writeRunOnceFailureArtifactsAndError(queueReportPath, finalReportPath, map[string]any{
			"mode":            "supervised_e2e",
			"phase":           "validate_issue",
			"status":          "failed_closed",
			"project":         manifest.Key,
			"repo":            repoID,
			"run_id":          runID,
			"requested_issue": issueNumber,
			"codex_execution": supervision.SideEffectNotStarted,
			"prs":             supervision.SideEffectNotCreated,
			"merge":           supervision.SideEffectNotMerged,
			"deployment":      supervision.SideEffectNotStarted,
		}, fmt.Errorf("expected exactly one fetched issue, got %d", len(issues)))
	}
	issue := issues[0]
	if err := validateSupervisedE2ERunOnceIssue(issue); err != nil {
		return writeRunOnceFailureArtifactsAndError(queueReportPath, finalReportPath, map[string]any{
			"mode":            "supervised_e2e",
			"phase":           "validate_issue",
			"status":          "failed_closed",
			"project":         manifest.Key,
			"repo":            repoID,
			"run_id":          runID,
			"requested_issue": issueNumber,
			"issue": workSuperviseE2EIssueReport{
				Number:      issue.Number,
				URL:         issue.URL,
				PlannedPath: firstString(issue.ChangedPaths),
			},
			"issue_state":     issue.State,
			"pull_request":    issue.PullRequest,
			"labels":          issue.Labels,
			"codex_execution": supervision.SideEffectNotStarted,
			"prs":             supervision.SideEffectNotCreated,
			"merge":           supervision.SideEffectNotMerged,
			"deployment":      supervision.SideEffectNotStarted,
		}, err)
	}
	if err := validateSupervisedE2ERunOnceScope(issue.ChangedPaths); err != nil {
		return writeRunOnceFailureArtifactsAndError(queueReportPath, finalReportPath, map[string]any{
			"mode":            "supervised_e2e",
			"phase":           "validate_issue",
			"status":          "failed_closed",
			"project":         manifest.Key,
			"repo":            repoID,
			"run_id":          runID,
			"requested_issue": issueNumber,
			"issue": workSuperviseE2EIssueReport{
				Number:      issue.Number,
				URL:         issue.URL,
				PlannedPath: firstString(issue.ChangedPaths),
			},
			"changed_paths":   issue.ChangedPaths,
			"codex_execution": supervision.SideEffectNotStarted,
			"prs":             supervision.SideEffectNotCreated,
			"merge":           supervision.SideEffectNotMerged,
			"deployment":      supervision.SideEffectNotStarted,
		}, err)
	}

	project, err := ensureWorkSuperviseProject(ctx, store, manifest)
	if err != nil {
		return redactWorkSuperviseE2EError(err)
	}
	service := supervision.NewService(store, supervision.DefaultConfig())
	queueReport, err := service.Queue(ctx, supervision.Project{ID: project.ID, Key: project.Key, Repo: repoID}, []supervision.Issue{issue})
	if err != nil {
		return redactWorkSuperviseE2EError(err)
	}
	if err := writeRedactedJSONArtifact(queueReportPath, queueReport); err != nil {
		return redactWorkSuperviseE2EError(err)
	}

	status := "claimed"
	if len(queueReport.Decisions) != 1 || queueReport.Decisions[0].Decision != supervision.DecisionEligible || !queueReport.Decisions[0].Eligible {
		status = "refused"
	}
	report := workSuperviseE2EReport{
		Mode:    "supervised_e2e",
		Phase:   "queued",
		Status:  status,
		Project: manifest.Key,
		Repo:    repoID,
		RunID:   runID,
		Issue: workSuperviseE2EIssueReport{
			Number:      issue.Number,
			URL:         issue.URL,
			PlannedPath: issue.ChangedPaths[0],
		},
		Queue:              queueReport.Decisions,
		Claims:             queueReport.Claims,
		CodexExecution:     queueReport.SideEffects.CodexExecution,
		PRs:                queueReport.SideEffects.PRs,
		Merge:              queueReport.SideEffects.Merge,
		Deployment:         queueReport.SideEffects.Deployment,
		HumanMergeRequired: true,
		Artifacts:          artifactRefs,
	}
	if err := writeRedactedJSONArtifact(finalReportPath, report); err != nil {
		return redactWorkSuperviseE2EError(err)
	}
	if status != "claimed" || len(report.Claims) != 1 || report.Claims[0].ClaimKey == "" {
		return redactWorkSuperviseE2EError(fmt.Errorf("work supervise e2e run-once refused before worker behavior: issue=%d status=%s", issueNumber, queueRefusalReason(report.Queue)))
	}
	if !report.Claims[0].NewlyCreated {
		report.Status = "claim_exists"
		report.CodexExecution = supervision.SideEffectNotStarted
		if err := writeRedactedJSONArtifact(finalReportPath, report); err != nil {
			return redactWorkSuperviseE2EError(err)
		}
		return redactWorkSuperviseE2EError(fmt.Errorf("work supervise e2e run-once preserved existing claim before worker behavior: issue=%d claim_key=%s", issueNumber, report.Claims[0].ClaimKey))
	}

	workerReport, err := runSupervisedE2EWorkerAndAudit(ctx, manifest, issue, report, artifactRefs)
	if err != nil {
		return err
	}
	report = workerReport

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return redactWorkSuperviseE2EError(err)
	}
	return nil
}

func prepareSupervisedE2EArtifactTargets(runID string) (string, string, string, error) {
	odinRoot := strings.TrimSpace(os.Getenv("ODIN_ROOT"))
	if odinRoot == "" {
		return "", "", "", fmt.Errorf("ODIN_ROOT is required for work supervise e2e prepare-issue")
	}
	odinRootAbs, err := filepath.Abs(filepath.Clean(expandTilde(odinRoot)))
	if err != nil {
		return "", "", "", fmt.Errorf("preflight supervised e2e artifacts: resolve ODIN_ROOT: %w", err)
	}
	runDir := filepath.Join(odinRootAbs, "runs", "supervised-e2e", runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", "", "", fmt.Errorf("preflight supervised e2e artifacts: create run directory: %w", err)
	}
	preparedIssuePath := filepath.Join(runDir, "prepared-issue.json")
	finalReportPath := filepath.Join(runDir, "final-report.json")
	for _, path := range []string{preparedIssuePath, finalReportPath} {
		if err := preflightArtifactFile(path); err != nil {
			_ = os.RemoveAll(runDir)
			return "", "", "", fmt.Errorf("preflight supervised e2e artifacts: %w", err)
		}
	}
	return runDir, preparedIssuePath, finalReportPath, nil
}

func prepareSupervisedE2ERunOnceArtifactTargets(runID string) (string, string, string, error) {
	odinRoot := strings.TrimSpace(os.Getenv("ODIN_ROOT"))
	if odinRoot == "" {
		return "", "", "", fmt.Errorf("ODIN_ROOT is required for work supervise e2e run-once")
	}
	odinRootAbs, err := filepath.Abs(filepath.Clean(expandTilde(odinRoot)))
	if err != nil {
		return "", "", "", fmt.Errorf("preflight supervised e2e artifacts: resolve ODIN_ROOT: %w", err)
	}
	runDir := filepath.Join(odinRootAbs, "runs", "supervised-e2e", runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", "", "", fmt.Errorf("preflight supervised e2e artifacts: create run directory: %w", err)
	}
	queueReportPath := filepath.Join(runDir, "queue-report.json")
	finalReportPath := filepath.Join(runDir, "final-report.json")
	for _, path := range []string{queueReportPath, finalReportPath} {
		if err := preflightArtifactFile(path); err != nil {
			_ = os.RemoveAll(runDir)
			return "", "", "", fmt.Errorf("preflight supervised e2e artifacts: %w", err)
		}
	}
	return runDir, queueReportPath, finalReportPath, nil
}

func supervisedE2ERunOnceArtifactRefs(runDir string, queueReportPath string, finalReportPath string) workSuperviseE2EArtifactRefs {
	return workSuperviseE2EArtifactRefs{
		QueueReport:   queueReportPath,
		FinalReport:   finalReportPath,
		WorkerPrompt:  filepath.Join(runDir, "worker-prompt.md"),
		WorkerCommand: filepath.Join(runDir, "worker-command.json"),
		WorkerOutput:  filepath.Join(runDir, "worker-output.txt"),
		DiffSummary:   filepath.Join(runDir, "diff-summary.md"),
	}
}

func writeRunOnceFailureArtifactsAndError(queueReportPath string, finalReportPath string, report map[string]any, err error) error {
	if writeErr := writeRedactedJSONArtifact(queueReportPath, report); writeErr != nil {
		return redactWorkSuperviseE2EError(writeErr)
	}
	if writeErr := writeRedactedJSONArtifact(finalReportPath, report); writeErr != nil {
		return redactWorkSuperviseE2EError(writeErr)
	}
	return redactWorkSuperviseE2EError(err)
}

func runSupervisedE2EWorkerAndAudit(ctx context.Context, manifest projects.Manifest, issue supervision.Issue, report workSuperviseE2EReport, artifacts workSuperviseE2EArtifactRefs) (workSuperviseE2EReport, error) {
	plannedPath := issue.ChangedPaths[0]
	if strings.TrimSpace(manifest.GitRoot) == "" {
		return report, supervisedE2EFailWithReport(artifacts.FinalReport, report, "worker_setup", fmt.Errorf("project %q has no git root for supervised e2e worker", manifest.Key))
	}

	worktreeRoot := strings.TrimSpace(os.Getenv("ODIN_WORKTREE_ROOT"))
	if worktreeRoot == "" {
		worktreeRoot = worktrees.DefaultRoot()
	}
	nonce := time.Now().UnixNano()
	branchName := fmt.Sprintf("odin/stage7-supervised-e2e/issue-%d-%d", issue.Number, nonce)
	worktreePath := worktrees.ResolvePath(worktrees.PathParams{
		Root:       worktreeRoot,
		ProjectKey: manifest.Key + "-stage7-supervised-e2e",
		TaskID:     int64(issue.Number),
		RunID:      nonce,
		Try:        1,
	})
	rootAbs, pathAbs, insideRoot, err := provePathInsideRoot(worktreeRoot, worktreePath)
	if err != nil {
		return report, supervisedE2EFailWithReport(artifacts.FinalReport, report, "worker_setup", err)
	}
	if !insideRoot {
		return report, supervisedE2EFailWithReport(artifacts.FinalReport, report, "worker_setup", fmt.Errorf("supervised e2e worktree path %q escaped root %q", worktreePath, worktreeRoot))
	}
	if err := os.MkdirAll(filepath.Dir(pathAbs), 0o755); err != nil {
		return report, supervisedE2EFailWithReport(artifacts.FinalReport, report, "worker_setup", err)
	}

	git := vcsgit.Adapter{}
	baseBranch := strings.TrimSpace(manifest.DefaultBranch)
	if baseBranch == "" {
		baseBranch = "HEAD"
	}
	if err := git.CreateBranch(ctx, manifest.GitRoot, branchName, baseBranch); err != nil {
		return report, supervisedE2EFailWithReport(artifacts.FinalReport, report, "worker_setup", err)
	}
	if err := git.AddWorktree(ctx, manifest.GitRoot, pathAbs, branchName); err != nil {
		_ = deleteGitBranch(context.Background(), manifest.GitRoot, branchName)
		return report, supervisedE2EFailWithReport(artifacts.FinalReport, report, "worker_setup", err)
	}

	report.Worktree = workSuperviseE2EWorktree{
		Root:       rootAbs,
		Path:       pathAbs,
		Branch:     branchName,
		InsideRoot: true,
		Kept:       true,
	}
	report.CodexExecution = "attempted"

	secrets := collectKnownSecretValues()
	prompt := renderSupervisedE2EWorkerPrompt(manifest, issue, plannedPath)
	command, err := codexexec.BuildCommand(codexexec.Config{
		SecretValues: secrets,
		Timeout:      supervisedE2EWorkerTimeout,
	}, runner.Request{
		WorkItemID:  fmt.Sprintf("issue-%d", issue.Number),
		Role:        "builder",
		Worktree:    pathAbs,
		Prompt:      prompt,
		SandboxMode: "workspace-write",
		Timeout:     supervisedE2EWorkerTimeout,
	})
	if err != nil {
		return report, supervisedE2EFailWithReport(artifacts.FinalReport, report, "worker_command", err)
	}

	if err := writeRedactedTextArtifact(artifacts.WorkerPrompt, prompt, secrets); err != nil {
		return report, redactWorkSuperviseE2EError(err)
	}
	commandArtifact := workSuperviseE2EWorkerCommandArtifact{
		Executable:  command.Path,
		Redacted:    codexexec.RedactCommand(command, secrets),
		Worktree:    pathAbs,
		SandboxMode: "workspace-write",
		Timeout:     supervisedE2EWorkerTimeout.String(),
		Launched:    true,
	}
	if err := writeRedactedJSONArtifact(artifacts.WorkerCommand, commandArtifact); err != nil {
		return report, redactWorkSuperviseE2EError(err)
	}

	result, err := runSupervisedE2EWorker(ctx, supervisedE2EWorkerRequest{
		WorktreePath: pathAbs,
		BranchName:   branchName,
		PlannedPath:  plannedPath,
		Prompt:       prompt,
		Command:      command,
		Secrets:      secrets,
	})
	if writeErr := writeRedactedTextArtifact(artifacts.WorkerOutput, result.Output, secrets); writeErr != nil {
		return report, redactWorkSuperviseE2EError(writeErr)
	}
	if err != nil {
		return report, supervisedE2EFailWithReport(artifacts.FinalReport, report, "worker_execution", err)
	}

	if _, err := gitOutput(ctx, pathAbs, "add", "-N", "."); err != nil {
		return report, supervisedE2EFailWithReport(artifacts.FinalReport, report, "diff_audit", err)
	}
	nameStatus, err := gitOutput(ctx, pathAbs, "diff", "--name-status", baseBranch)
	if err != nil {
		return report, supervisedE2EFailWithReport(artifacts.FinalReport, report, "diff_audit", err)
	}
	fullDiff, err := gitOutput(ctx, pathAbs, "diff", baseBranch)
	if err != nil {
		return report, supervisedE2EFailWithReport(artifacts.FinalReport, report, "diff_audit", err)
	}
	diffFiles := filesFromNameStatus(nameStatus)
	sort.Strings(diffFiles)
	diffSHA := sha256String(strings.TrimSpace(nameStatus) + "\n" + fullDiff)
	report.Diff = workSuperviseE2EDiff{Files: diffFiles, SHA256: diffSHA}
	diffSummary := renderSupervisedE2EDiffSummary(baseBranch, branchName, nameStatus, fullDiff, diffSHA)
	if err := writeRedactedTextArtifact(artifacts.DiffSummary, diffSummary, secrets); err != nil {
		return report, redactWorkSuperviseE2EError(err)
	}

	if !supervisedE2EDiffMatchesPlannedPath(diffFiles, plannedPath) {
		return report, supervisedE2EFailWithReport(artifacts.FinalReport, report, "diff_audit", fmt.Errorf("worker diff changed forbidden files: got %s want exactly %s", strings.Join(diffFiles, ", "), plannedPath))
	}

	rawAudit := strings.Join([]string{
		prompt,
		result.Output,
		codexexec.RedactCommand(command, nil),
		fullDiff,
		mustReadString(artifacts.WorkerPrompt),
		mustReadString(artifacts.WorkerCommand),
		mustReadString(artifacts.WorkerOutput),
		mustReadString(artifacts.DiffSummary),
	}, "\n")
	if supervisedE2ETokenPattern.MatchString(rawAudit) {
		return report, supervisedE2EFailWithReport(artifacts.FinalReport, report, "token_audit", fmt.Errorf("token-shaped string detected in supervised e2e worker artifacts or diff"))
	}

	report.Phase = "worker_audited"
	report.Status = "worker_completed"
	report.CodexExecution = "completed"
	report.PRs = supervision.SideEffectNotCreated
	report.Merge = supervision.SideEffectNotMerged
	report.Deployment = supervision.SideEffectNotStarted
	report.HumanMergeRequired = true
	if err := writeRedactedJSONArtifact(artifacts.FinalReport, report); err != nil {
		return report, redactWorkSuperviseE2EError(err)
	}
	return report, nil
}

func defaultRunSupervisedE2EWorker(ctx context.Context, request supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
	adapter := codexexec.NewAgentRunner(codexexec.Config{
		SecretValues: request.Secrets,
		Timeout:      request.Command.Timeout,
	})
	result, err := adapter.Run(ctx, runner.Request{
		WorkItemID:  fmt.Sprintf("supervised-e2e-%s", filepath.Base(request.BranchName)),
		Role:        "builder",
		Worktree:    request.WorktreePath,
		Prompt:      request.Prompt,
		SandboxMode: "workspace-write",
		Timeout:     request.Command.Timeout,
	})
	return supervisedE2EWorkerResult{Output: result.Summary}, err
}

func renderSupervisedE2EWorkerPrompt(manifest projects.Manifest, issue supervision.Issue, plannedPath string) string {
	return strings.Join([]string{
		"Stage 7 supervised E2E worker task.",
		"",
		"Audit the existing repo before editing.",
		"Reuse existing Odin commands, services, contracts, registries, schemas, docs, and tests.",
		"Only edit this exact planned path: " + plannedPath,
		"Do not change runner, security, workspace, token, deploy, CI, scheduler, PR, or merge behavior.",
		"Do not expose tokens or secrets.",
		"Do not create or update a pull request.",
		"",
		fmt.Sprintf("Project: %s", manifest.Key),
		fmt.Sprintf("Repo: %s", manifest.GitHub.Repo),
		fmt.Sprintf("Issue: #%d %s", issue.Number, issue.Title),
		"",
		"Final output must mention make odin-e2e-local.",
	}, "\n")
}

func renderSupervisedE2EDiffSummary(baseBranch string, branchName string, nameStatus string, fullDiff string, sha string) string {
	return strings.Join([]string{
		"# Supervised E2E Diff Summary",
		"",
		"Base: " + baseBranch,
		"Branch: " + branchName,
		"SHA256: " + sha,
		"",
		"## Name Status",
		"",
		"```",
		strings.TrimSpace(nameStatus),
		"```",
		"",
		"## Diff",
		"",
		"```diff",
		strings.TrimSpace(fullDiff),
		"```",
		"",
	}, "\n")
}

func supervisedE2EDiffMatchesPlannedPath(files []string, plannedPath string) bool {
	return len(files) == 1 && files[0] == plannedPath && normalizeSupervisedE2ERunOncePath(files[0]) == plannedPath
}

func supervisedE2EFailWithReport(finalReportPath string, report workSuperviseE2EReport, phase string, err error) error {
	report.Phase = phase
	report.Status = "failed_closed"
	if phase == "worker_setup" || phase == "worker_command" {
		report.CodexExecution = supervision.SideEffectNotStarted
	} else {
		report.CodexExecution = "attempted"
	}
	report.PRs = supervision.SideEffectNotCreated
	report.Merge = supervision.SideEffectNotMerged
	report.Deployment = supervision.SideEffectNotStarted
	if writeErr := writeRedactedJSONArtifact(finalReportPath, report); writeErr != nil {
		return redactWorkSuperviseE2EError(writeErr)
	}
	return redactWorkSuperviseE2EError(err)
}

func writeRedactedTextArtifact(path string, content string, secrets []string) error {
	redacted := codexexec.Redact(content, secrets)
	redacted = supervisedE2ETokenPattern.ReplaceAllString(redacted, "[REDACTED]")
	return os.WriteFile(path, []byte(redacted+"\n"), 0o644)
}

func mustReadString(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}

func sha256String(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validateSupervisedE2ERunOnceIssue(issue supervision.Issue) error {
	if issue.PullRequest {
		return fmt.Errorf("work supervise e2e run-once requires an issue, not a pull request: issue=%d", issue.Number)
	}
	if strings.TrimSpace(issue.State) != "open" {
		return fmt.Errorf("work supervise e2e run-once requires an open issue: issue=%d state=%q", issue.Number, issue.State)
	}
	if hasSupervisedE2ELabel(issue.Labels, tracker.LabelBlocked) {
		return fmt.Errorf("work supervise e2e run-once refuses blocked issue: issue=%d label=%s", issue.Number, tracker.LabelBlocked)
	}
	return nil
}

func validateSupervisedE2ERawPlannedScope(body string) error {
	var plannedFields []string
	for _, line := range strings.Split(body, "\n") {
		markerIndex := strings.Index(strings.ToLower(line), "planned scope:")
		if markerIndex < 0 {
			continue
		}
		rawScope := strings.TrimSpace(line[markerIndex+len("planned scope:"):])
		for _, field := range strings.Fields(rawScope) {
			plannedFields = append(plannedFields, strings.Trim(field, "<>.,!?"))
		}
	}
	if len(plannedFields) != 1 {
		return fmt.Errorf("work supervise e2e run-once requires exactly one raw Planned scope token for exact diff audit: got %d", len(plannedFields))
	}
	return nil
}

func validateSupervisedE2ERunOnceScope(paths []string) error {
	switch len(paths) {
	case 0:
		return fmt.Errorf("work supervise e2e run-once requires exactly one Planned scope under docs/operations/: got zero")
	case 1:
		cleaned := normalizeSupervisedE2ERunOncePath(paths[0])
		if cleaned != paths[0] || !strings.HasPrefix(cleaned, "docs/operations/") {
			return fmt.Errorf("work supervise e2e run-once requires Planned scope under docs/operations/: got %q", paths[0])
		}
		if strings.ContainsAny(cleaned, " \t\r\n") {
			return fmt.Errorf("work supervise e2e run-once requires Planned scope without whitespace for exact diff audit: got %q", paths[0])
		}
		return nil
	default:
		return fmt.Errorf("work supervise e2e run-once requires exactly one Planned scope under docs/operations/: got %d", len(paths))
	}
}

func normalizeSupervisedE2ERunOncePath(value string) string {
	cleaned := filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
	cleaned = strings.TrimPrefix(cleaned, "./")
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	return cleaned
}

func hasSupervisedE2ELabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func queueRefusalReason(decisions []supervision.QueueDecision) string {
	if len(decisions) == 0 {
		return "no_queue_decision"
	}
	if decisions[0].RefusalReason != "" {
		return decisions[0].RefusalReason
	}
	return decisions[0].Decision
}

func preflightArtifactFile(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if _, err := file.WriteString("{}\n"); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

func writeRedactedJSONArtifact(path string, value any) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	redacted := codexexec.Redact(string(content), collectKnownSecretValues())
	redacted = supervisedE2ETokenPattern.ReplaceAllString(redacted, "[REDACTED]")
	return os.WriteFile(path, []byte(redacted+"\n"), 0o644)
}

func redactWorkSuperviseE2EError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", codexexec.Redact(err.Error(), collectKnownSecretValues()))
}

func runWorkSuperviseQueue(ctx context.Context, store *sqlite.Store, projectRegistry projects.Registry, service supervision.Service, params map[string]string) (supervision.Report, error) {
	projectKey := strings.TrimSpace(params["project"])
	if projectKey == "" {
		return supervision.Report{}, fmt.Errorf("missing --project for work supervise queue")
	}
	rawFixtureIssue := strings.TrimSpace(params["fixture-issue"])
	manifest, ok := projectRegistry.Lookup(projectKey)
	if !ok {
		return supervision.Report{}, fmt.Errorf("unknown project %q", projectKey)
	}
	if rawFixtureIssue == "" {
		return runWorkSuperviseTrackerQueue(ctx, store, manifest, service)
	}
	issueNumber, err := strconv.Atoi(rawFixtureIssue)
	if err != nil || issueNumber <= 0 {
		return supervision.Report{}, fmt.Errorf("invalid --fixture-issue %q", rawFixtureIssue)
	}

	report, err := service.Status(ctx)
	if err != nil {
		return supervision.Report{}, err
	}
	issue := supervision.Issue{
		Repo:         manifest.GitHub.Repo,
		Number:       issueNumber,
		Title:        "Stage 7 supervised agency control-plane fixture proof",
		Labels:       []string{"odin:ready", "safety:low-risk"},
		ChangedPaths: []string{"docs/stage-7-supervised-agency.md"},
	}
	eligibility := supervision.EvaluateIssue(supervision.DefaultConfig(), issue)
	decision := supervision.QueueDecision{
		ProjectKey:  manifest.Key,
		Repo:        manifest.GitHub.Repo,
		IssueNumber: issueNumber,
		Eligible:    eligibility.Eligible,
		DecidedAt:   time.Now().UTC(),
	}
	switch {
	case report.Control.KillSwitchActive || report.Control.Status != supervision.ControlStatusEnabled:
		decision.Decision = supervision.DecisionRefused
		decision.Eligible = false
		decision.RefusalReason = supervision.RefusalKillSwitchActive
	case !eligibility.Eligible:
		decision.Decision = supervision.DecisionRefused
		decision.RefusalReason = eligibility.RefusalReason
	default:
		decision.Decision = supervision.DecisionEligible
	}
	report.Decisions = []supervision.QueueDecision{decision}
	report.Claims = []supervision.PlannedClaim{}
	return report, nil
}

func runWorkSuperviseTrackerQueue(ctx context.Context, store *sqlite.Store, manifest projects.Manifest, service supervision.Service) (supervision.Report, error) {
	source, err := newIntakeTracker(manifest, trackerintake.SyncOptions{})
	if err != nil {
		return supervision.Report{}, err
	}
	issues, err := source.FetchEligibleIssues(ctx)
	if err != nil {
		return supervision.Report{}, err
	}
	if auditor, ok := source.(tracker.RequestAuditor); ok {
		audit := auditor.RequestAudit()
		if audit.Writes > 0 {
			if len(audit.Forbidden) > 0 {
				forbidden := audit.Forbidden[0]
				return supervision.Report{}, fmt.Errorf("forbidden GitHub write attempted during supervise queue intake: method=%s path=%s", forbidden.Method, forbidden.Path)
			}
			return supervision.Report{}, fmt.Errorf("forbidden GitHub write attempted during supervise queue intake")
		}
	}

	project, err := ensureWorkSuperviseProject(ctx, store, manifest)
	if err != nil {
		return supervision.Report{}, err
	}
	return service.Queue(ctx, supervision.Project{
		ID:   project.ID,
		Key:  project.Key,
		Repo: manifest.GitHub.Repo,
	}, supervisionIssuesFromTrackerIssues(issues, manifest.GitHub.Repo))
}

func ensureWorkSuperviseProject(ctx context.Context, store *sqlite.Store, manifest projects.Manifest) (sqlite.Project, error) {
	project, err := store.GetProjectByKey(ctx, manifest.Key)
	if err == nil {
		return project, nil
	}
	if err != sql.ErrNoRows {
		return sqlite.Project{}, err
	}

	scopeValue := "project"
	if manifest.SystemProject {
		scopeValue = "odin-core"
	}
	return store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           manifest.Key,
		Name:          manifest.Name,
		Scope:         scopeValue,
		GitRoot:       manifest.GitRoot,
		DefaultBranch: manifest.DefaultBranch,
		GitHubRepo:    manifest.GitHub.Repo,
		ManifestPath:  manifest.SourcePath,
	})
}

func supervisionIssuesFromTrackerIssues(issues []tracker.Issue, fallbackRepo string) []supervision.Issue {
	adapted := make([]supervision.Issue, 0, len(issues))
	for _, issue := range issues {
		repo := strings.TrimSpace(issue.Repo)
		if repo == "" {
			repo = fallbackRepo
		}
		adapted = append(adapted, supervision.Issue{
			Provider:     strings.TrimSpace(issue.Provider),
			Repo:         repo,
			Number:       issue.Number,
			Title:        issue.Title,
			Body:         issue.Body,
			Labels:       append([]string(nil), issue.Labels...),
			URL:          issue.URL,
			State:        issue.State,
			PullRequest:  issue.PullRequest,
			ChangedPaths: extractIssuePathHints(issue.Body),
		})
	}
	return adapted
}

func extractIssuePathHints(text string) []string {
	seen := map[string]bool{}
	var paths []string
	for _, line := range strings.Split(text, "\n") {
		markerIndex := strings.Index(strings.ToLower(line), "planned scope:")
		if markerIndex < 0 {
			continue
		}
		for _, candidate := range issuePathHintFields(line[markerIndex+len("planned scope:"):]) {
			if seen[candidate] {
				continue
			}
			seen[candidate] = true
			paths = append(paths, candidate)
		}
	}
	return paths
}

func issuePathHintFields(text string) []string {
	var paths []string
	for _, field := range strings.FieldsFunc(text, issuePathHintSeparator) {
		candidate := strings.Trim(field, "<>.,!?")
		if !looksLikeRelativePathHint(candidate) {
			continue
		}
		if looksLikeSensitivePathHint(candidate) {
			paths = append(paths, workSuperviseRedactedSensitivePath)
			continue
		}
		paths = append(paths, candidate)
	}
	return paths
}

func issuePathHintSeparator(r rune) bool {
	switch r {
	case ' ', '\r', '\t', ',', ';', '(', ')', '[', ']', '{', '}', '"', '\'', '`':
		return true
	default:
		return false
	}
}

func looksLikeRelativePathHint(candidate string) bool {
	if candidate == "" || strings.Contains(candidate, "://") || strings.HasPrefix(candidate, "/") {
		return false
	}
	if !strings.Contains(candidate, "/") {
		return false
	}
	return strings.Contains(filepath.Base(candidate), ".") || strings.HasSuffix(candidate, "/")
}

func looksLikeSensitivePathHint(candidate string) bool {
	lowered := strings.ToLower(candidate)
	for _, marker := range []string{
		"ghp_",
		"github_pat_",
		"gho_",
		"ghu_",
		"ghs_",
		"ghr_",
		"token",
		"secret",
		"password",
		"credential",
	} {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

func flattenWorkSuperviseReport(report supervision.Report, command string, params map[string]string) workSuperviseReport {
	queue := append([]supervision.QueueDecision(nil), report.Decisions...)
	if queue == nil {
		queue = []supervision.QueueDecision{}
	}
	claims := append([]supervision.PlannedClaim(nil), report.Claims...)
	if claims == nil {
		claims = []supervision.PlannedClaim{}
	}
	return workSuperviseReport{
		Mode:           report.ModeKey,
		Source:         workSuperviseReportSource(command, params),
		Enabled:        report.Control.Status == supervision.ControlStatusEnabled,
		KillSwitch:     report.Control.KillSwitchActive,
		ConfigHash:     report.Control.ConfigHash,
		Queue:          queue,
		Claims:         claims,
		Recovery:       report.Recovery,
		CodexExecution: report.SideEffects.CodexExecution,
		PRs:            report.SideEffects.PRs,
		Merge:          report.SideEffects.Merge,
		Deployment:     report.SideEffects.Deployment,
	}
}

func workSuperviseReportSource(command string, params map[string]string) string {
	if command != "queue" {
		return ""
	}
	if strings.TrimSpace(params["fixture-issue"]) != "" {
		return workSuperviseFixtureSource
	}
	return workSuperviseTrackerSource
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

func runWorkPRDryRun(ctx context.Context, args []string, stdout io.Writer) error {
	params := parseWorkStartArgs(args)
	worktreePath := strings.TrimSpace(params["worktree"])
	baseBranch := strings.TrimSpace(params["base"])
	if worktreePath == "" || baseBranch == "" {
		_, err := fmt.Fprintln(stdout, "usage: odin work pr-dry-run --worktree <path> --base <branch> [--json]")
		return err
	}
	worktreeAbs, err := filepath.Abs(filepath.Clean(expandTilde(worktreePath)))
	if err != nil {
		return err
	}
	currentHead, err := gitOutput(ctx, worktreeAbs, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	diffSummary, err := gitOutput(ctx, worktreeAbs, "diff", "--stat", baseBranch)
	if err != nil {
		return err
	}
	nameStatus, err := gitOutput(ctx, worktreeAbs, "diff", "--name-status", baseBranch)
	if err != nil {
		return err
	}
	if strings.TrimSpace(nameStatus) == "" {
		return fmt.Errorf("no diff between worktree %q and base %q", worktreeAbs, baseBranch)
	}
	files := filesFromNameStatus(nameStatus)

	odinRoot := strings.TrimSpace(os.Getenv("ODIN_ROOT"))
	if odinRoot == "" {
		odinRoot = filepath.Join(worktreeAbs, ".odin")
	}
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	artifactDir := filepath.Join(expandTilde(odinRoot), "runs", "pr-dry-run", runID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return err
	}

	diffPath := filepath.Join(artifactDir, "diff-summary.md")
	diffContent := renderPRDryRunDiffSummary(baseBranch, strings.TrimSpace(currentHead), diffSummary, nameStatus)
	diffSHA, err := writeArtifact(diffPath, diffContent)
	if err != nil {
		return err
	}
	bodyPath := filepath.Join(artifactDir, "pr-body.md")
	bodyContent := renderPRDryRunBody(files)
	bodySHA, err := writeArtifact(bodyPath, bodyContent)
	if err != nil {
		return err
	}
	checklistPath := filepath.Join(artifactDir, "handoff-checklist.md")
	checklistItems := prDryRunChecklistItems()
	checklistSHA, err := writeArtifact(checklistPath, renderChecklist(checklistItems))
	if err != nil {
		return err
	}
	if err := verifyPRTemplate(ctx, worktreeAbs, bodyPath); err != nil {
		return err
	}

	report := workPRDryRunReport{
		Worktree:    worktreeAbs,
		Base:        baseBranch,
		CurrentHead: strings.TrimSpace(currentHead),
		DiffSummary: workPRDryRunDiffSummaryReport{
			Generated: true,
			Files:     files,
			Text:      diffContent,
			Path:      diffPath,
			SHA256:    diffSHA,
		},
		PRBody: workPRDryRunBodyReport{
			Generated:        true,
			Path:             bodyPath,
			SHA256:           bodySHA,
			TemplateVerified: true,
		},
		Checklist: workPRDryRunChecklistReport{
			Path:   checklistPath,
			SHA256: checklistSHA,
			Items:  checklistItems,
		},
		Artifacts: []workPRDryRunArtifactReport{
			{Path: bodyPath, SHA256: bodySHA, Kind: "pr_body", Label: "draft artifact, not durable PR handoff state"},
			{Path: checklistPath, SHA256: checklistSHA, Kind: "human_checklist", Label: "draft artifact, not durable PR handoff state"},
			{Path: diffPath, SHA256: diffSHA, Kind: "diff_summary", Label: "draft artifact, not durable PR handoff state"},
		},
		GitHubWrites: 0,
		Push:         "not_pushed",
		Merge:        "not_merged",
		PRs:          "not_created_or_updated",
		Dispatch:     "not_started",
	}
	if parseBoolFlag(params, "json") {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	_, err = fmt.Fprintf(stdout, "pr_dry_run worktree=%s base=%s body=%s push=not_pushed merge=not_merged prs=not_created_or_updated\n", worktreeAbs, baseBranch, bodyPath)
	return err
}

func runWorkPRCreate(ctx context.Context, projectRegistry projects.Registry, args []string, stdout io.Writer) error {
	params := parseWorkStartArgs(args)
	issueText := strings.TrimSpace(params["issue"])
	approvedTarget := strings.TrimSpace(params["approved-target"])
	worktreePath := strings.TrimSpace(params["worktree"])
	baseBranch := strings.TrimSpace(params["base"])
	if issueText == "" || approvedTarget == "" || worktreePath == "" || baseBranch == "" || !parseBoolFlag(params, "wait-ci") {
		_, err := fmt.Fprintln(stdout, "usage: odin work pr-create --issue <number> --approved-target <repo>#<issue> --worktree <path> --base <branch> --wait-ci [--json]")
		return err
	}
	issueNumber, err := strconv.Atoi(issueText)
	if err != nil || issueNumber <= 0 {
		return fmt.Errorf("invalid issue number %q", issueText)
	}
	project, err := resolveLifecycleProject(projectRegistry, strings.TrimSpace(params["project"]))
	if err != nil {
		return err
	}
	if strings.TrimSpace(project.GitHub.Repo) == "" {
		return fmt.Errorf("project %q has no GitHub repo for PR creation", project.Key)
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
	worktreeAbs, err := filepath.Abs(filepath.Clean(expandTilde(worktreePath)))
	if err != nil {
		return err
	}
	currentHead, err := gitOutput(ctx, worktreeAbs, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	branchName := strings.TrimSpace(currentHead)
	if branchName == "" || branchName == "HEAD" {
		return fmt.Errorf("worktree must be on a named branch")
	}
	if branchName == baseBranch {
		return fmt.Errorf("refusing to create Stage 6 PR from base branch %q", baseBranch)
	}
	diffBase := resolvePRCreateDiffBase(ctx, worktreeAbs, baseBranch)
	stat, err := gitOutput(ctx, worktreeAbs, "diff", "--stat", diffBase)
	if err != nil {
		return err
	}
	nameStatus, err := gitOutput(ctx, worktreeAbs, "diff", "--name-status", diffBase)
	if err != nil {
		return err
	}
	if strings.TrimSpace(nameStatus) == "" {
		return fmt.Errorf("no diff between worktree %q and base %q", worktreeAbs, baseBranch)
	}
	files := filesFromNameStatus(nameStatus)
	if !allDocsOnly(files) {
		return fmt.Errorf("Stage 6 requires a docs-only diff; changed files: %s", strings.Join(files, ", "))
	}
	diffSHA := prCreateDiffFingerprint(strings.TrimSpace(stat), strings.TrimSpace(nameStatus))
	headSHA, err := gitOutput(ctx, worktreeAbs, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	headSHA = strings.TrimSpace(headSHA)

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
	if issue.Number != issueNumber {
		return fmt.Errorf("GitHub returned issue #%d for approved issue #%d", issue.Number, issueNumber)
	}

	branchResult, err := ensureRemoteBranch(ctx, worktreeAbs, branchName, headSHA)
	if err != nil {
		return err
	}

	prs, err := client.ListPullRequests(ctx, branchName, baseBranch)
	if err != nil {
		return err
	}
	prCreated := false
	prReused := false
	var pr trackergithub.PullRequest
	if len(prs) > 0 {
		pr = prs[0]
		if !pr.Draft {
			return fmt.Errorf("existing Stage 6 PR #%d is not a draft PR", pr.Number)
		}
		prReused = true
	} else {
		bodyContent := renderPRCreateBody(issueNumber, files)
		artifactDir, err := ensureWorkArtifactDir(worktreeAbs, "pr-create")
		if err != nil {
			return err
		}
		bodyPath := filepath.Join(artifactDir, "pr-body.md")
		if _, err := writeArtifact(bodyPath, bodyContent); err != nil {
			return err
		}
		if err := verifyPRTemplate(ctx, worktreeAbs, bodyPath); err != nil {
			return err
		}
		pr, err = client.CreatePullRequest(ctx, trackergithub.PullRequestRequest{
			Title: fmt.Sprintf("Stage 6 docs-only proof for #%d", issueNumber),
			Head:  branchName,
			Base:  baseBranch,
			Body:  bodyContent,
			Draft: true,
		})
		if err != nil {
			return err
		}
		prCreated = true
	}

	comments, err := client.FetchIssueComments(ctx, tracker.IssueID{Provider: "github", Repo: project.GitHub.Repo, Number: pr.Number})
	if err != nil {
		return err
	}
	evidenceComments, err := ensureStage6EvidenceComments(ctx, client, project.GitHub.Repo, pr.Number, issueNumber, diffSHA, comments)
	if err != nil {
		return err
	}

	timeout := 10 * time.Minute
	if timeoutText := strings.TrimSpace(params["ci-timeout"]); timeoutText != "" {
		parsed, err := time.ParseDuration(timeoutText)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("invalid ci-timeout %q", timeoutText)
		}
		timeout = parsed
	}
	ciResult, workflowRuns, err := waitForStage6CI(ctx, client, branchName, timeout)
	if err != nil {
		return err
	}
	deploymentAudit := auditStage6Deployment(workflowRuns)
	if !deploymentAudit.NoDeploymentWorkflows {
		return fmt.Errorf("deployment-class workflow ran during Stage 6 proof")
	}

	audit := client.RequestAudit()
	report := workPRCreateReport{
		Project:        project.Key,
		Repo:           project.GitHub.Repo,
		Issue:          issueNumber,
		ApprovedTarget: approvedTarget,
		Worktree:       worktreeAbs,
		Base:           baseBranch,
		Diff: workPRCreateDiffReport{
			DocsOnly: true,
			SHA256:   diffSHA,
			Files:    files,
			Summary:  renderPRDryRunDiffSummary(diffBase, branchName, stat, nameStatus),
		},
		Branch: workPRCreateBranchReport{
			Name:   branchName,
			SHA:    headSHA,
			Pushed: branchResult.Pushed,
			Reused: branchResult.Reused,
		},
		PR: workPRCreatePullRequestReport{
			Number:  pr.Number,
			URL:     pr.URL,
			Draft:   pr.Draft,
			Created: prCreated,
			Reused:  prReused,
		},
		EvidenceComments: evidenceComments,
		CI:               ciResult,
		DeploymentAudit:  deploymentAudit,
		GitHubWrites:     audit.Writes,
		Merge:            "not_merged",
		Deployment:       "not_started",
		Dispatch:         "not_started",
		CodexExecution:   "not_started",
		DurableState:     workPRCreateDurableStateReport{Created: false},
	}
	if parseBoolFlag(params, "json") {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	_, err = fmt.Fprintf(stdout, "pr_create issue=%d branch=%s pr=%s draft=%t ci=%s merge=not_merged deployment=not_started\n", issueNumber, branchName, pr.URL, pr.Draft, ciResult.Conclusion)
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

func gitOutput(ctx context.Context, worktreePath string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", worktreePath}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %v: %w: %s", args, err, string(output))
	}
	return string(output), nil
}

func filesFromNameStatus(nameStatus string) []string {
	seen := map[string]bool{}
	for _, line := range strings.Split(nameStatus, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, file := range fields[1:] {
			if file != "" {
				seen[file] = true
			}
		}
	}
	files := make([]string, 0, len(seen))
	for file := range seen {
		files = append(files, file)
	}
	sort.Strings(files)
	return files
}

func renderPRDryRunDiffSummary(baseBranch string, currentHead string, stat string, nameStatus string) string {
	return strings.Join([]string{
		"# Diff Summary",
		"",
		fmt.Sprintf("- Base: %s", baseBranch),
		fmt.Sprintf("- Current head: %s", currentHead),
		"",
		"## Stat",
		"",
		codeBlock(strings.TrimSpace(stat)),
		"",
		"## Files",
		"",
		codeBlock(strings.TrimSpace(nameStatus)),
		"",
	}, "\n")
}

func renderPRDryRunBody(files []string) string {
	summary := "- Generated Stage 5 dry-run PR handoff draft."
	if len(files) > 0 {
		summary = "- Generated Stage 5 dry-run PR handoff draft for changed paths: " + strings.Join(files, ", ") + "."
	}
	return strings.Join([]string{
		"## Summary",
		summary,
		"",
		"## Proven",
		"- Diff summary generated from the local worktree without pushing.",
		"- PR body template validated by scripts/ci/verify-pr-template.sh.",
		"- Human checklist generated as a local draft artifact.",
		"",
		"## Unproven",
		"- Live GitHub PR creation, update, merge, and push behavior were intentionally not exercised.",
		"",
		"- [x] this PR changes user-visible or orchestration-facing behavior",
		"- [x] if the box above is checked, real `odin` command proof is included below",
		"",
		"## Commands Run",
		"```bash",
		"odin work pr-dry-run --worktree <path> --base <branch> --json",
		"scripts/ci/verify-pr-template.sh <generated-pr-body>",
		"```",
		"",
	}, "\n")
}

func prDryRunChecklistItems() []string {
	return []string{
		"Review diff summary",
		"Confirm tests listed under Commands Run are sufficient",
		"Confirm Unproven items are acceptable",
		"Confirm no push occurred during dry-run",
		"Confirm no live PR was created or updated",
		"Confirm no merge occurred",
	}
}

func renderChecklist(items []string) string {
	var builder strings.Builder
	builder.WriteString("# Human Checklist\n\n")
	for _, item := range items {
		builder.WriteString("- [ ] ")
		builder.WriteString(item)
		builder.WriteString("\n")
	}
	return builder.String()
}

func codeBlock(value string) string {
	if strings.TrimSpace(value) == "" {
		value = "(empty)"
	}
	return "```text\n" + value + "\n```"
}

func writeArtifact(path string, content string) (string, error) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:]), nil
}

func verifyPRTemplate(ctx context.Context, worktreePath string, bodyPath string) error {
	repoRoot, err := gitOutput(ctx, worktreePath, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	scriptPath := filepath.Join(strings.TrimSpace(repoRoot), "scripts", "ci", "verify-pr-template.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		fallback, findErr := findUpward("scripts/ci/verify-pr-template.sh")
		if findErr != nil {
			return findErr
		}
		scriptPath = fallback
	}
	command := exec.CommandContext(ctx, "bash", scriptPath, bodyPath)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify PR template: %w: %s", err, string(output))
	}
	return nil
}

func ensureWorkArtifactDir(worktreeAbs string, kind string) (string, error) {
	odinRoot := strings.TrimSpace(os.Getenv("ODIN_ROOT"))
	if odinRoot == "" {
		odinRoot = filepath.Join(worktreeAbs, ".odin")
	}
	artifactDir := filepath.Join(expandTilde(odinRoot), "runs", kind, fmt.Sprintf("%d", time.Now().UnixNano()))
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return "", err
	}
	return artifactDir, nil
}

func resolvePRCreateDiffBase(ctx context.Context, worktreeAbs string, baseBranch string) string {
	remoteBase := "origin/" + strings.TrimSpace(baseBranch)
	if strings.TrimSpace(baseBranch) == "" {
		return baseBranch
	}
	if _, err := gitOutput(ctx, worktreeAbs, "rev-parse", "--verify", remoteBase); err == nil {
		return remoteBase
	}
	return baseBranch
}

func allDocsOnly(files []string) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		cleaned := filepath.ToSlash(filepath.Clean(file))
		if strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
			return false
		}
		if strings.HasPrefix(cleaned, "docs/") && strings.HasSuffix(strings.ToLower(cleaned), ".md") {
			continue
		}
		if !strings.Contains(cleaned, "/") && strings.HasSuffix(strings.ToLower(cleaned), ".md") {
			continue
		}
		return false
	}
	return true
}

func prCreateDiffFingerprint(stat string, nameStatus string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(stat) + "\n---\n" + strings.TrimSpace(nameStatus)))
	return hex.EncodeToString(sum[:])
}

func ensureRemoteBranch(ctx context.Context, worktreeAbs string, branchName string, headSHA string) (workPRCreateBranchReport, error) {
	remoteRef := "refs/heads/" + branchName
	remoteOutput, err := gitOutput(ctx, worktreeAbs, "ls-remote", "--heads", "origin", remoteRef)
	if err != nil {
		return workPRCreateBranchReport{}, err
	}
	remoteOutput = strings.TrimSpace(remoteOutput)
	if remoteOutput != "" {
		fields := strings.Fields(remoteOutput)
		if len(fields) > 0 && fields[0] == headSHA {
			return workPRCreateBranchReport{Name: branchName, SHA: headSHA, Reused: true}, nil
		}
		return workPRCreateBranchReport{}, fmt.Errorf("remote branch %q already exists at a different commit", branchName)
	}
	command := exec.CommandContext(ctx, "git", "-C", worktreeAbs, "push", "origin", "HEAD:"+remoteRef)
	output, err := command.CombinedOutput()
	if err != nil {
		return workPRCreateBranchReport{}, fmt.Errorf("git push Stage 6 branch: %w: %s", err, string(output))
	}
	return workPRCreateBranchReport{Name: branchName, SHA: headSHA, Pushed: true}, nil
}

func renderPRCreateBody(issueNumber int, files []string) string {
	summary := "- Stage 6 live docs-only PR proof for approved issue #" + strconv.Itoa(issueNumber) + "."
	if len(files) > 0 {
		summary = "- Stage 6 live docs-only PR proof for approved issue #" + strconv.Itoa(issueNumber) + " touching: " + strings.Join(files, ", ") + "."
	}
	return strings.Join([]string{
		"## Summary",
		summary,
		"- This is a draft Human Review Handoff; human merge is required.",
		"- Refs #" + strconv.Itoa(issueNumber) + ".",
		"",
		"## Proven",
		"- Docs-only diff validated before push and PR creation.",
		"- Draft PR creation is gated by exact approved target.",
		"- Odin-authored Stage 6 evidence comments and bounded CI proof are required before completion.",
		"",
		"## Unproven",
		"- Human merge is intentionally unproven and remains required.",
		"- Deployment is intentionally not triggered.",
		"",
		"- [x] this PR changes user-visible or orchestration-facing behavior",
		"- [x] if the box above is checked, real `odin` command proof is included below",
		"",
		"## Commands Run",
		"```bash",
		"odin work pr-create --issue <number> --approved-target <repo>#<issue> --worktree <path> --base <branch> --wait-ci --json",
		"```",
		"",
	}, "\n")
}

func ensureStage6EvidenceComments(ctx context.Context, client *trackergithub.Client, repo string, prNumber int, issueNumber int, diffSHA string, existing []tracker.IssueComment) ([]workPRCreateEvidenceCommentReport, error) {
	specs := []struct {
		marker string
		body   string
	}{
		{
			marker: stage6ReviewEvidenceMarker,
			body: strings.Join([]string{
				stage6ReviewEvidenceMarker,
				"Stage 6 review evidence for human review.",
				"diff_sha256=" + diffSHA,
				"approved_issue=#" + strconv.Itoa(issueNumber),
				"This is not an autonomous review approval and does not authorize merge or deployment.",
			}, "\n"),
		},
		{
			marker: stage6HumanReviewHandoffMarker,
			body: strings.Join([]string{
				stage6HumanReviewHandoffMarker,
				"Stage 6 Human Review Handoff evidence.",
				"diff_sha256=" + diffSHA,
				"human_merge_required=true",
				"No Codex reviewer/QA worker was launched for this stage.",
			}, "\n"),
		},
	}
	reports := make([]workPRCreateEvidenceCommentReport, 0, len(specs))
	for _, spec := range specs {
		found, err := stage6CommentWithMarker(existing, spec.marker, diffSHA)
		if err != nil {
			return nil, err
		}
		report := workPRCreateEvidenceCommentReport{Marker: spec.marker}
		if found != nil {
			report.URL = found.URL
			report.Reused = true
			reports = append(reports, report)
			continue
		}
		comment, err := client.AddCommentWithResult(ctx, tracker.IssueID{Provider: "github", Repo: repo, Number: prNumber}, spec.body)
		if err != nil {
			return nil, err
		}
		report.Created = true
		report.URL = comment.URL
		reports = append(reports, report)
	}
	return reports, nil
}

func stage6CommentWithMarker(comments []tracker.IssueComment, marker string, diffSHA string) (*tracker.IssueComment, error) {
	for index := range comments {
		comment := comments[index]
		if !strings.Contains(comment.Body, marker) {
			continue
		}
		if !strings.Contains(comment.Body, "diff_sha256="+diffSHA) {
			return nil, fmt.Errorf("existing Stage 6 evidence comment %s has a different diff hash", marker)
		}
		return &comment, nil
	}
	return nil, nil
}

func waitForStage6CI(ctx context.Context, client *trackergithub.Client, branchName string, timeout time.Duration) (workPRCreateCIReport, []trackergithub.WorkflowRun, error) {
	deadline := time.Now().Add(timeout)
	for {
		runs, err := client.ListWorkflowRuns(ctx, branchName)
		if err != nil {
			return workPRCreateCIReport{}, nil, err
		}
		if run, ok := findStage6E2ERun(runs); ok {
			report := workPRCreateCIReport{
				Waited:     true,
				URL:        run.URL,
				Status:     run.Status,
				Conclusion: run.Conclusion,
			}
			if strings.EqualFold(run.Status, "completed") {
				if strings.EqualFold(run.Conclusion, "success") {
					return report, runs, nil
				}
				return report, runs, fmt.Errorf("Stage 6 CI concluded %q", run.Conclusion)
			}
		}
		if time.Now().After(deadline) {
			return workPRCreateCIReport{Waited: true, TimedOut: true}, runs, fmt.Errorf("timed out waiting for Stage 6 CI after %s", timeout)
		}
		sleepFor := 2 * time.Second
		if remaining := time.Until(deadline); remaining < sleepFor {
			sleepFor = remaining
		}
		if sleepFor <= 0 {
			sleepFor = 10 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return workPRCreateCIReport{}, runs, ctx.Err()
		case <-time.After(sleepFor):
		}
	}
}

func findStage6E2ERun(runs []trackergithub.WorkflowRun) (trackergithub.WorkflowRun, bool) {
	for _, run := range runs {
		name := strings.ToLower(run.Name)
		path := strings.ToLower(run.Path)
		if strings.Contains(name, "odin e2e") || strings.Contains(path, "odin-e2e") {
			return run, true
		}
	}
	for _, run := range runs {
		name := strings.ToLower(run.Name)
		path := strings.ToLower(run.Path)
		if name == "ci" || strings.HasSuffix(path, "/ci.yml") || strings.HasSuffix(path, "/ci.yaml") {
			return run, true
		}
	}
	return trackergithub.WorkflowRun{}, false
}

func auditStage6Deployment(runs []trackergithub.WorkflowRun) workPRCreateDeploymentAuditReport {
	audit := workPRCreateDeploymentAuditReport{
		NoDeploymentWorkflows: true,
		Dispatches:            0,
		Mutations:             0,
	}
	for _, run := range runs {
		name := strings.ToLower(run.Name)
		path := strings.ToLower(run.Path)
		if strings.Contains(name, "deploy") || strings.Contains(name, "deployment") || strings.Contains(name, "release") || strings.Contains(name, "production") ||
			strings.Contains(path, "deploy") || strings.Contains(path, "deployment") || strings.Contains(path, "release") || strings.Contains(path, "production") {
			audit.NoDeploymentWorkflows = false
			audit.DeploymentRuns = append(audit.DeploymentRuns, run.URL)
		}
	}
	return audit
}

func findUpward(relativePath string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, relativePath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find %s from %s", relativePath, dir)
		}
		dir = parent
	}
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

type workPRDryRunReport struct {
	Worktree     string                        `json:"worktree"`
	Base         string                        `json:"base"`
	CurrentHead  string                        `json:"current_head"`
	DiffSummary  workPRDryRunDiffSummaryReport `json:"diff_summary"`
	PRBody       workPRDryRunBodyReport        `json:"pr_body"`
	Checklist    workPRDryRunChecklistReport   `json:"human_checklist"`
	Artifacts    []workPRDryRunArtifactReport  `json:"artifacts"`
	GitHubWrites int                           `json:"github_writes"`
	Push         string                        `json:"push"`
	Merge        string                        `json:"merge"`
	PRs          string                        `json:"prs"`
	Dispatch     string                        `json:"dispatch"`
}

type workPRCreateReport struct {
	Project          string                              `json:"project"`
	Repo             string                              `json:"repo"`
	Issue            int                                 `json:"issue"`
	ApprovedTarget   string                              `json:"approved_target"`
	Worktree         string                              `json:"worktree"`
	Base             string                              `json:"base"`
	Diff             workPRCreateDiffReport              `json:"diff"`
	Branch           workPRCreateBranchReport            `json:"branch"`
	PR               workPRCreatePullRequestReport       `json:"pr"`
	EvidenceComments []workPRCreateEvidenceCommentReport `json:"evidence_comments"`
	CI               workPRCreateCIReport                `json:"ci"`
	DeploymentAudit  workPRCreateDeploymentAuditReport   `json:"deployment_audit"`
	GitHubWrites     int                                 `json:"github_writes"`
	Merge            string                              `json:"merge"`
	Deployment       string                              `json:"deployment"`
	Dispatch         string                              `json:"dispatch"`
	CodexExecution   string                              `json:"codex_execution"`
	DurableState     workPRCreateDurableStateReport      `json:"durable_state"`
}

type workPRCreateDiffReport struct {
	DocsOnly bool     `json:"docs_only"`
	SHA256   string   `json:"sha256"`
	Files    []string `json:"files"`
	Summary  string   `json:"summary"`
}

type workPRCreateBranchReport struct {
	Name   string `json:"name"`
	SHA    string `json:"sha,omitempty"`
	Pushed bool   `json:"pushed"`
	Reused bool   `json:"reused"`
}

type workPRCreatePullRequestReport struct {
	Number  int    `json:"number"`
	URL     string `json:"url"`
	Draft   bool   `json:"draft"`
	Created bool   `json:"created"`
	Reused  bool   `json:"reused"`
}

type workPRCreateEvidenceCommentReport struct {
	Marker  string `json:"marker"`
	URL     string `json:"url,omitempty"`
	Created bool   `json:"created"`
	Reused  bool   `json:"reused"`
}

type workPRCreateCIReport struct {
	Waited     bool   `json:"waited"`
	TimedOut   bool   `json:"timed_out"`
	URL        string `json:"url,omitempty"`
	Status     string `json:"status,omitempty"`
	Conclusion string `json:"conclusion,omitempty"`
}

type workPRCreateDeploymentAuditReport struct {
	NoDeploymentWorkflows bool     `json:"no_deployment_workflows"`
	Dispatches            int      `json:"dispatches"`
	Mutations             int      `json:"mutations"`
	DeploymentRuns        []string `json:"deployment_runs,omitempty"`
}

type workPRCreateDurableStateReport struct {
	Created bool `json:"created"`
}

type workPRDryRunDiffSummaryReport struct {
	Generated bool     `json:"generated"`
	Files     []string `json:"files"`
	Text      string   `json:"text"`
	Path      string   `json:"path"`
	SHA256    string   `json:"sha256"`
}

type workPRDryRunBodyReport struct {
	Generated        bool   `json:"generated"`
	Path             string `json:"path"`
	SHA256           string `json:"sha256"`
	TemplateVerified bool   `json:"template_verified"`
}

type workPRDryRunChecklistReport struct {
	Path   string   `json:"path"`
	SHA256 string   `json:"sha256"`
	Items  []string `json:"items"`
}

type workPRDryRunArtifactReport struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Kind   string `json:"kind"`
	Label  string `json:"label"`
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
