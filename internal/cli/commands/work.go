package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tracker"
	trackerintake "odin-os/internal/tracker/intake"
)

const workUsage = "usage: odin work status|profiles|start --project <key> --title <text>|intake --project <key> [--dry-run]|simulate-lifecycle --issue <number> [--project <key>] [--dry-run] [--json]"

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
