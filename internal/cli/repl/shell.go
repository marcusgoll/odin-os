package repl

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"odin-os/internal/cli/commands"
	"odin-os/internal/cli/render"
	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/registry"
	healthsvc "odin-os/internal/runtime/health"
	jobsvc "odin-os/internal/runtime/jobs"
	runsvc "odin-os/internal/runtime/runs"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/leases"
)

type Environment struct {
	Store               *sqlite.Store
	Registry            projects.Registry
	RegistrySnapshot    registry.Snapshot
	RegistryDiagnostics []projects.Diagnostic
	SessionStore        SessionStore
	ExecutorConfig      executorrouter.Config
	Executors           map[string]contract.Executor
	Leases              leases.Manager
}

type Shell struct {
	env         Environment
	state       State
	health      healthsvc.Service
	jobs        jobsvc.Service
	runs        runsvc.Service
	transitions projects.Service
}

const transitionUsage = "/transition [status] | /transition set <state> [allow=<csv>] [confirm] because <reason...>"

func New(env Environment) (*Shell, error) {
	cache, err := env.SessionStore.Load()
	if err != nil {
		return nil, err
	}

	state := ResolveStartupState(cache, env.Registry)
	shell := &Shell{
		env:   env,
		state: state,
		health: healthsvc.Service{
			DB: env.Store.DB(),
		},
		jobs: jobsvc.Service{
			Store:    env.Store,
			Registry: env.Registry,
			Now:      time.Now,
		},
		runs: runsvc.Service{
			DB: env.Store.DB(),
		},
		transitions: projects.Service{
			Store: env.Store,
		},
	}

	if err := shell.persistState(); err != nil {
		return nil, err
	}

	return shell, nil
}

func (shell *Shell) Run(ctx context.Context, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := shell.renderPrompt(ctx, output); err != nil {
			return err
		}

		if !scanner.Scan() {
			return scanner.Err()
		}

		if err := shell.HandleLine(ctx, scanner.Text(), output); err != nil {
			return err
		}
	}
}

func (shell *Shell) HandleLine(ctx context.Context, line string, output io.Writer) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	if command, ok := commands.Parse(line); ok {
		return shell.handleCommand(ctx, command, output)
	}

	if shell.state.Mode == ModeAsk {
		return shell.handleAsk(ctx, line, output)
	}

	task, err := shell.jobs.CreateTaskFromAct(ctx, shell.state.Scope, line)
	if err != nil {
		_, _ = fmt.Fprintf(output, "unable to create task: %v\n", err)
		return nil
	}
	shell.state.ActiveTask = task.Key
	shell.state.ActiveRun = ""
	if err := shell.persistState(); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "created task %s (%s)\n", task.Key, task.Status)
	return err
}

func (shell *Shell) renderPrompt(ctx context.Context, output io.Writer) error {
	pendingApprovals, err := shell.pendingApprovals(ctx)
	if err != nil {
		return err
	}

	healthSummary, err := shell.health.Summary(ctx, len(shell.env.RegistryDiagnostics) == 0)
	if err != nil {
		return err
	}

	header := render.RenderHeader(render.Header{
		Scope:            shell.scopeLabel(),
		Mode:             string(shell.state.Mode),
		Health:           string(healthSummary.Status),
		PendingApprovals: len(pendingApprovals),
		ActiveTask:       shell.state.ActiveTask,
		ActiveRun:        shell.state.ActiveRun,
	})

	if _, err := fmt.Fprintln(output, header); err != nil {
		return err
	}
	_, err = fmt.Fprint(output, "odin> ")
	return err
}

func (shell *Shell) handleCommand(ctx context.Context, command commands.Command, output io.Writer) error {
	switch command.Name {
	case "help":
		if _, err := fmt.Fprintln(output, "/help /mode /scope /project /transition /observe /compare /workflows /tradeboard /overview /jobs /runs /approvals /actions /logs /doctor /self"); err != nil {
			return err
		}
		_, err := fmt.Fprintf(output, "%s\n", transitionUsage)
		return err
	case "mode":
		return shell.handleMode(command.Args, output)
	case "scope":
		return shell.handleScope(command.Args, output)
	case "project":
		return shell.handleProject(command.Args, output)
	case "transition":
		return shell.handleTransition(ctx, command.Args, output)
	case "observe":
		return shell.handleObserve(ctx, command.Args, output)
	case "compare":
		return shell.handleCompare(ctx, command.Args, output)
	case "workflows":
		return shell.handleWorkflows(command.Args, output)
	case "tradeboard":
		return shell.handleTradeboard(ctx, command.Args, output)
	case "overview":
		return shell.handleOverview(ctx, output)
	case "jobs":
		return shell.handleJobs(ctx, output)
	case "runs":
		return shell.handleRuns(ctx, output)
	case "approvals":
		return shell.handleApprovals(ctx, output)
	case "actions":
		return shell.handleActions(ctx, command.Args, output)
	case "logs":
		return shell.handleLogs(ctx, output)
	case "doctor":
		return shell.handleDoctor(ctx, command.Args, output)
	case "self":
		return shell.handleSelf(output)
	case "exit", "quit":
		return io.EOF
	default:
		_, err := fmt.Fprintf(output, "unknown command: /%s\n", command.Name)
		return err
	}
}

func (shell *Shell) handleOverview(ctx context.Context, output io.Writer) error {
	jobs, err := shell.jobs.List(ctx, shell.state.Scope)
	if err != nil {
		return err
	}
	runs, err := shell.runs.List(ctx, shell.state.Scope)
	if err != nil {
		return err
	}
	approvals, err := shell.pendingApprovals(ctx)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintln(output, "Overview"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "Operator Surface: odin work ..."); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(output, "Work Items (%d)\n", len(jobs)); err != nil {
		return err
	}
	if len(jobs) == 0 {
		if _, err := fmt.Fprintln(output, "  none"); err != nil {
			return err
		}
	}
	for _, job := range jobs {
		if _, err := fmt.Fprintf(output, "  %s %s project=%s\n", job.TaskKey, job.Status, job.ProjectKey); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(output, "Run Attempts (%d)\n", len(runs)); err != nil {
		return err
	}
	if len(runs) == 0 {
		if _, err := fmt.Fprintln(output, "  none"); err != nil {
			return err
		}
	}
	for _, run := range runs {
		if _, err := fmt.Fprintf(output, "  %s %s %s attempt=%d\n", run.TaskKey, run.Executor, run.Status, run.Attempt); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "  Active run: %d\n", run.RunID); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(output, "Approvals (%d)\n", len(approvals)); err != nil {
		return err
	}
	if len(approvals) == 0 {
		if _, err := fmt.Fprintln(output, "  none"); err != nil {
			return err
		}
	}
	for _, approval := range approvals {
		if _, err := fmt.Fprintf(output, "  %s %s\n", approval.TaskKey, approval.Status); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "  Pending approval: %d\n", approval.ApprovalID); err != nil {
			return err
		}
	}
	return nil
}

func (shell *Shell) handleAsk(ctx context.Context, line string, output io.Writer) error {
	switch commands.RouteAskIntent(line) {
	case commands.IntentHelp:
		return shell.handleCommand(ctx, commands.Command{Name: "help"}, output)
	case commands.IntentMode:
		return shell.handleCommand(ctx, commands.Command{Name: "mode"}, output)
	case commands.IntentScope:
		return shell.handleCommand(ctx, commands.Command{Name: "scope"}, output)
	case commands.IntentProject:
		return shell.handleCommand(ctx, commands.Command{Name: "project"}, output)
	case commands.IntentJobs:
		return shell.handleCommand(ctx, commands.Command{Name: "jobs"}, output)
	case commands.IntentRuns:
		return shell.handleCommand(ctx, commands.Command{Name: "runs"}, output)
	case commands.IntentApprovals:
		return shell.handleCommand(ctx, commands.Command{Name: "approvals"}, output)
	case commands.IntentLogs:
		return shell.handleCommand(ctx, commands.Command{Name: "logs"}, output)
	case commands.IntentDoctor:
		return shell.handleCommand(ctx, commands.Command{Name: "doctor"}, output)
	default:
		_, err := fmt.Fprintln(output, "local ask is limited in Phase 05. Try /help, /scope, /project, /jobs, /runs, /approvals, /logs, or /doctor.")
		return err
	}
}

func (shell *Shell) handleMode(args []string, output io.Writer) error {
	if len(args) == 0 {
		_, err := fmt.Fprintf(output, "mode=%s\n", shell.state.Mode)
		return err
	}

	requested := Mode(strings.ToLower(args[0]))
	sanitized := sanitizeMode(requested, shell.state.Scope)
	if requested == ModeAct && sanitized != ModeAct {
		shell.state.Mode = ModeAsk
		if err := shell.persistState(); err != nil {
			return err
		}
		_, err := fmt.Fprintln(output, "act mode is not allowed in global scope; remaining in ask mode")
		return err
	}

	shell.state.Mode = sanitized
	if err := shell.persistState(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(output, "mode=%s\n", shell.state.Mode)
	return err
}

func (shell *Shell) handleScope(args []string, output io.Writer) error {
	if len(args) == 0 {
		_, err := fmt.Fprintf(output, "scope=%s\n", shell.scopeLabel())
		return err
	}

	switch strings.ToLower(args[0]) {
	case "global":
		shell.state.Scope = scope.Resolution{Kind: scope.ScopeGlobal}
		shell.state.ActiveTask = ""
		shell.state.ActiveRun = ""
	case "new-project":
		shell.state.Scope = scope.Resolution{Kind: scope.ScopeNewProject}
		shell.state.ActiveTask = ""
		shell.state.ActiveRun = ""
	default:
		_, err := fmt.Fprintf(output, "unsupported scope target: %s\n", args[0])
		return err
	}

	shell.state.Mode = sanitizeMode(shell.state.Mode, shell.state.Scope)
	if err := shell.persistState(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(output, "scope=%s mode=%s\n", shell.scopeLabel(), shell.state.Mode)
	return err
}

func (shell *Shell) handleProject(args []string, output io.Writer) error {
	if len(args) == 0 {
		current := shell.state.Scope.ProjectKey
		if current == "" {
			current = "none"
		}

		projectKeys := make([]string, 0, len(shell.env.Registry.Projects()))
		for _, project := range shell.env.Registry.Projects() {
			projectKeys = append(projectKeys, project.Key)
		}

		_, err := fmt.Fprintf(output, "current=%s projects=%s\n", current, strings.Join(projectKeys, ","))
		return err
	}

	project, ok := shell.env.Registry.Lookup(args[0])
	if !ok {
		_, err := fmt.Fprintf(output, "unknown project: %s\n", args[0])
		return err
	}

	shell.state.Scope = scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey:    project.Key,
			SystemProject: project.SystemProject,
		},
	})
	shell.state.ActiveTask = ""
	shell.state.ActiveRun = ""
	shell.state.Mode = sanitizeMode(shell.state.Mode, shell.state.Scope)
	if err := shell.persistState(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(output, "project=%s scope=%s\n", project.Key, shell.scopeLabel())
	return err
}

func (shell *Shell) handleTransition(ctx context.Context, args []string, output io.Writer) error {
	if len(args) > 0 && strings.EqualFold(args[0], "help") {
		_, err := fmt.Fprintln(output, transitionUsage)
		return err
	}

	manifest, err := shell.scopedManifest()
	if err != nil {
		_, writeErr := fmt.Fprintln(output, err.Error())
		return writeErr
	}

	if len(args) == 0 || strings.EqualFold(args[0], "status") {
		status, err := shell.currentTransitionStatus(ctx, manifest)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, renderTransitionStatus(manifest.Key, status))
		return err
	}

	if !strings.EqualFold(args[0], "set") {
		_, err := fmt.Fprintf(output, "usage: %s\n", transitionUsage)
		return err
	}
	if len(args) < 2 {
		_, err := fmt.Fprintf(output, "usage: %s\n", transitionUsage)
		return err
	}

	request, err := parseTransitionSetRequest(args[1:])
	if err != nil {
		_, writeErr := fmt.Fprintln(output, err.Error())
		return writeErr
	}

	project, err := shell.ensureRuntimeProject(ctx, manifest)
	if err != nil {
		return err
	}
	record, err := shell.transitions.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:      project.ID,
		Actor:          projects.TransitionControllerOdinOS,
		TargetState:    request.State,
		LimitedActions: request.LimitedActions,
		ChangedBy:      "operator",
		Notes:          request.Reason,
	})
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "unable to change transition: %v\n", err)
		return writeErr
	}

	status := transitionStatus{
		State:             projects.TransitionState(record.State),
		Controller:        projects.TransitionController(record.Controller),
		MutationAuthority: projects.TransitionController(record.Controller),
		OdinCanMutate:     record.Controller == string(projects.TransitionControllerOdinOS),
		LimitedActions:    decodeCSVOrJSON(record.LimitedActionsJSON),
		Notes:             record.Notes,
	}
	_, err = fmt.Fprintln(output, renderTransitionStatus(manifest.Key, status))
	return err
}

func (shell *Shell) handleObserve(ctx context.Context, args []string, output io.Writer) error {
	return shell.handleTransitionReport(ctx, args, output, projects.TransitionStateShadow, "shadow_observation")
}

func (shell *Shell) handleCompare(ctx context.Context, args []string, output io.Writer) error {
	return shell.handleTransitionReport(ctx, args, output, projects.TransitionStateCompare, "compare_report")
}

func (shell *Shell) handleTransitionReport(ctx context.Context, args []string, output io.Writer, requiredState projects.TransitionState, reportType string) error {
	manifest, err := shell.scopedManifest()
	if err != nil {
		_, writeErr := fmt.Fprintln(output, err.Error())
		return writeErr
	}
	if len(args) == 0 {
		_, err := fmt.Fprintf(output, "usage: /%s <summary...>\n", commandNameForReport(reportType))
		return err
	}

	project, err := shell.ensureRuntimeProject(ctx, manifest)
	if err != nil {
		return err
	}

	summary := strings.Join(args, " ")
	details, err := json.Marshal(map[string]string{
		"source":  "cli",
		"summary": summary,
	})
	if err != nil {
		return err
	}

	switch requiredState {
	case projects.TransitionStateShadow:
		if _, err := shell.transitions.RecordShadowObservation(ctx, projects.ReportInput{
			ProjectID:   project.ID,
			Actor:       projects.TransitionControllerOdinOS,
			Summary:     summary,
			DetailsJSON: string(details),
		}); err != nil {
			_, writeErr := fmt.Fprintf(output, "unable to record observation: %v\n", err)
			return writeErr
		}
	case projects.TransitionStateCompare:
		if _, err := shell.transitions.RecordCompareReport(ctx, projects.ReportInput{
			ProjectID:   project.ID,
			Actor:       projects.TransitionControllerOdinOS,
			Summary:     summary,
			DetailsJSON: string(details),
		}); err != nil {
			_, writeErr := fmt.Fprintf(output, "unable to record compare report: %v\n", err)
			return writeErr
		}
	}

	_, err = fmt.Fprintf(output, "recorded %s for %s: %s\n", reportType, manifest.Key, summary)
	return err
}

func (shell *Shell) handleJobs(ctx context.Context, output io.Writer) error {
	views, err := shell.jobs.List(ctx, shell.state.Scope)
	if err != nil {
		return err
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(output, "no jobs")
		return err
	}

	for _, view := range views {
		if _, err := fmt.Fprintf(output, "%s %s %s\n", view.ProjectKey, view.TaskKey, view.Status); err != nil {
			return err
		}
	}
	return nil
}

func (shell *Shell) handleRuns(ctx context.Context, output io.Writer) error {
	views, err := shell.runs.List(ctx, shell.state.Scope)
	if err != nil {
		return err
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(output, "no runs")
		return err
	}
	for _, view := range views {
		if _, err := fmt.Fprintf(output, "%s %s %s\n", view.TaskKey, view.Executor, view.Status); err != nil {
			return err
		}
	}
	return nil
}

func (shell *Shell) handleApprovals(ctx context.Context, output io.Writer) error {
	approvals, err := shell.pendingApprovals(ctx)
	if err != nil {
		return err
	}
	if len(approvals) == 0 {
		_, err := fmt.Fprintln(output, "no approvals waiting")
		return err
	}
	for _, approval := range approvals {
		line := fmt.Sprintf("%s %s", approval.TaskKey, approval.Status)
		if approval.ActionID != nil {
			line = fmt.Sprintf("%s action_id=%d payload_hash=%s", line, *approval.ActionID, approval.PayloadHash)
		}
		if _, err := fmt.Fprintln(output, line); err != nil {
			return err
		}
	}
	return nil
}

func (shell *Shell) handleLogs(ctx context.Context, output io.Writer) error {
	params := sqlite.ListEventsParams{}
	if shell.state.Scope.Kind == scope.ScopeProject || shell.state.Scope.Kind == scope.ScopeOdinCore {
		project, err := shell.env.Store.GetProjectByKey(ctx, shell.state.Scope.ProjectKey)
		switch err {
		case nil:
			params.ProjectID = &project.ID
		case sql.ErrNoRows:
			_, writeErr := fmt.Fprintln(output, "no logs")
			return writeErr
		default:
			return err
		}
	}

	records, err := shell.env.Store.ListEvents(ctx, params)
	if err != nil {
		return err
	}

	count := 0
	for _, record := range records {
		if !matchesEventScope(record.Scope, shell.state.Scope) {
			continue
		}
		if _, err := fmt.Fprintf(output, "%d %s %s\n", record.ID, record.Type, record.Scope); err != nil {
			return err
		}
		count++
		if count == 10 {
			break
		}
	}
	if count == 0 {
		_, err := fmt.Fprintln(output, "no logs")
		return err
	}
	return nil
}

func (shell *Shell) handleDoctor(ctx context.Context, args []string, output io.Writer) error {
	report, err := shell.health.Doctor(ctx, len(shell.env.RegistryDiagnostics) == 0)
	if err != nil {
		return err
	}

	if len(args) > 0 && strings.EqualFold(args[0], "json") {
		encoder := json.NewEncoder(output)
		return encoder.Encode(report)
	}

	checks := make(map[string]string, len(report.Checks))
	for _, check := range report.Checks {
		key := check.Name
		if key == "source_freshness" {
			key = "sources"
		}
		checks[key] = string(check.Status)
	}

	_, err = fmt.Fprintf(
		output,
		"status=%s database=%s registry=%s executor=%s queue=%s projections=%s sources=%s\n",
		report.Status,
		checks["database"],
		checks["registry"],
		checks["executor"],
		checks["queue"],
		checks["projections"],
		checks["sources"],
	)
	return err
}

func (shell *Shell) handleSelf(output io.Writer) error {
	project, ok := shell.env.Registry.SystemProject()
	if !ok {
		_, err := fmt.Fprintln(output, "system project is not configured")
		return err
	}

	shell.state.Scope = scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey:    project.Key,
			SystemProject: true,
		},
	})
	shell.state.ActiveTask = ""
	shell.state.ActiveRun = ""
	shell.state.Mode = sanitizeMode(shell.state.Mode, shell.state.Scope)
	if err := shell.persistState(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(output, "project=%s scope=%s\n", project.Key, shell.scopeLabel())
	return err
}

func (shell *Shell) persistState() error {
	cache := Cache{
		Mode: shell.state.Mode,
	}
	if shell.state.Scope.Kind == scope.ScopeProject || shell.state.Scope.Kind == scope.ScopeOdinCore {
		cache.ProjectKey = shell.state.Scope.ProjectKey
	}
	return shell.env.SessionStore.Save(cache)
}

func (shell *Shell) scopeLabel() string {
	switch shell.state.Scope.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		return shell.state.Scope.ProjectKey
	default:
		return string(shell.state.Scope.Kind)
	}
}

func (shell *Shell) pendingApprovals(ctx context.Context) ([]pendingApproval, error) {
	rows, err := shell.env.Store.DB().QueryContext(ctx, `
		SELECT a.id, t.key, a.status, a.action_id, a.payload_hash, t.scope, p.key
		FROM approvals a
		JOIN tasks t ON t.id = a.task_id
		JOIN projects p ON p.id = t.project_id
		WHERE a.status = 'pending'
		ORDER BY a.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var approvals []pendingApproval
	for rows.Next() {
		var approval pendingApproval
		var projectKey string
		var taskScope string
		var actionID sql.NullInt64
		var payloadHash sql.NullString
		if err := rows.Scan(&approval.ApprovalID, &approval.TaskKey, &approval.Status, &actionID, &payloadHash, &taskScope, &projectKey); err != nil {
			return nil, err
		}
		if actionID.Valid {
			id := actionID.Int64
			approval.ActionID = &id
			approval.PayloadHash = payloadHash.String
		}
		if matchesTaskProjectionScope(projectKey, taskScope, shell.state.Scope) {
			approvals = append(approvals, approval)
		}
	}

	return approvals, rows.Err()
}

type pendingApproval struct {
	ApprovalID  int64
	TaskKey     string
	Status      string
	ActionID    *int64
	PayloadHash string
}

func matchesTaskProjectionScope(projectKey, taskScope string, resolved scope.Resolution) bool {
	switch resolved.Kind {
	case scope.ScopeGlobal:
		return true
	case scope.ScopeNewProject:
		return taskScope == string(scope.ScopeNewProject)
	case scope.ScopeProject, scope.ScopeOdinCore:
		return projectKey == resolved.ProjectKey
	default:
		return false
	}
}

func matchesEventScope(eventScope string, resolved scope.Resolution) bool {
	switch resolved.Kind {
	case scope.ScopeGlobal:
		return true
	case scope.ScopeProject:
		return eventScope == string(scope.ScopeProject)
	case scope.ScopeOdinCore:
		return eventScope == string(scope.ScopeOdinCore)
	case scope.ScopeNewProject:
		return eventScope == string(scope.ScopeNewProject)
	default:
		return false
	}
}

type transitionStatus struct {
	State             projects.TransitionState
	Controller        projects.TransitionController
	MutationAuthority projects.TransitionController
	OdinCanMutate     bool
	LimitedActions    []string
	Notes             string
}

type transitionSetRequest struct {
	State          projects.TransitionState
	LimitedActions []string
	Reason         string
	Confirmed      bool
}

func (shell *Shell) scopedManifest() (projects.Manifest, error) {
	switch shell.state.Scope.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		manifest, ok := shell.env.Registry.Lookup(shell.state.Scope.ProjectKey)
		if !ok {
			return projects.Manifest{}, fmt.Errorf("unknown project: %s", shell.state.Scope.ProjectKey)
		}
		return manifest, nil
	default:
		return projects.Manifest{}, fmt.Errorf("transition commands require project scope")
	}
}

func (shell *Shell) currentTransitionStatus(ctx context.Context, manifest projects.Manifest) (transitionStatus, error) {
	project, err := shell.env.Store.GetProjectByKey(ctx, manifest.Key)
	if err != nil {
		if err == sql.ErrNoRows {
			return transitionStatus{
				State:             projects.TransitionStateInventory,
				Controller:        projects.TransitionControllerLegacyOdin,
				MutationAuthority: projects.TransitionControllerLegacyOdin,
				OdinCanMutate:     false,
			}, nil
		}
		return transitionStatus{}, err
	}

	record, err := shell.env.Store.GetProjectTransition(ctx, project.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			return transitionStatus{
				State:             projects.TransitionStateInventory,
				Controller:        projects.TransitionControllerLegacyOdin,
				MutationAuthority: projects.TransitionControllerLegacyOdin,
				OdinCanMutate:     false,
			}, nil
		}
		return transitionStatus{}, err
	}

	controller := projects.TransitionController(record.Controller)
	return transitionStatus{
		State:             projects.TransitionState(record.State),
		Controller:        controller,
		MutationAuthority: controller,
		OdinCanMutate:     controller == projects.TransitionControllerOdinOS,
		LimitedActions:    decodeCSVOrJSON(record.LimitedActionsJSON),
		Notes:             record.Notes,
	}, nil
}

func (shell *Shell) ensureRuntimeProject(ctx context.Context, manifest projects.Manifest) (sqlite.Project, error) {
	project, err := shell.env.Store.GetProjectByKey(ctx, manifest.Key)
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

	return shell.env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           manifest.Key,
		Name:          manifest.Name,
		Scope:         scopeValue,
		GitRoot:       manifest.GitRoot,
		DefaultBranch: manifest.DefaultBranch,
		GitHubRepo:    manifest.GitHub.Repo,
		ManifestPath:  manifest.SourcePath,
	})
}

func parseTransitionSetRequest(args []string) (transitionSetRequest, error) {
	if len(args) == 0 {
		return transitionSetRequest{}, fmt.Errorf("transition target state is required")
	}

	state := projects.TransitionState(strings.ToLower(args[0]))
	validState := map[projects.TransitionState]bool{
		projects.TransitionStateInventory:      true,
		projects.TransitionStateShadow:         true,
		projects.TransitionStateCompare:        true,
		projects.TransitionStateLimitedAction:  true,
		projects.TransitionStateCutover:        true,
		projects.TransitionStateDecommissioned: true,
	}
	if !validState[state] {
		return transitionSetRequest{}, fmt.Errorf("unsupported transition state: %s", args[0])
	}

	becauseIndex := -1
	for index := 1; index < len(args); index++ {
		if strings.EqualFold(args[index], "because") {
			becauseIndex = index
			break
		}
	}
	if becauseIndex == -1 || becauseIndex == len(args)-1 {
		return transitionSetRequest{}, fmt.Errorf("transition changes require a reason: use 'because <reason...>'")
	}

	request := transitionSetRequest{
		State:  state,
		Reason: strings.Join(args[becauseIndex+1:], " "),
	}
	for _, token := range args[1:becauseIndex] {
		switch {
		case strings.EqualFold(token, "confirm"):
			request.Confirmed = true
		case strings.HasPrefix(strings.ToLower(token), "allow="):
			raw := strings.TrimSpace(token[len("allow="):])
			if raw == "" {
				return transitionSetRequest{}, fmt.Errorf("limited_action requires allow=<csv>")
			}
			for _, action := range strings.Split(raw, ",") {
				action = strings.TrimSpace(action)
				if action != "" {
					request.LimitedActions = append(request.LimitedActions, action)
				}
			}
		default:
			return transitionSetRequest{}, fmt.Errorf("unknown transition option: %s", token)
		}
	}

	switch state {
	case projects.TransitionStateLimitedAction:
		if len(request.LimitedActions) == 0 {
			return transitionSetRequest{}, fmt.Errorf("limited_action requires allow=<csv>")
		}
		if !request.Confirmed {
			return transitionSetRequest{}, fmt.Errorf("limited_action requires confirm")
		}
	case projects.TransitionStateCutover, projects.TransitionStateDecommissioned:
		if !request.Confirmed {
			return transitionSetRequest{}, fmt.Errorf("%s requires confirm", state)
		}
	default:
		if len(request.LimitedActions) != 0 {
			return transitionSetRequest{}, fmt.Errorf("allow=<csv> is only valid for limited_action")
		}
	}

	return request, nil
}

func renderTransitionStatus(projectKey string, status transitionStatus) string {
	limitedActions := "none"
	if len(status.LimitedActions) > 0 {
		limitedActions = strings.Join(status.LimitedActions, ",")
	}

	if status.Notes != "" {
		return fmt.Sprintf(
			"project=%s state=%s controller=%s mutation_authority=%s odin_can_mutate=%t limited_actions=%s notes=%s",
			projectKey,
			status.State,
			status.Controller,
			status.MutationAuthority,
			status.OdinCanMutate,
			limitedActions,
			status.Notes,
		)
	}

	return fmt.Sprintf(
		"project=%s state=%s controller=%s mutation_authority=%s odin_can_mutate=%t limited_actions=%s",
		projectKey,
		status.State,
		status.Controller,
		status.MutationAuthority,
		status.OdinCanMutate,
		limitedActions,
	)
}

func commandNameForReport(reportType string) string {
	switch reportType {
	case "shadow_observation":
		return "observe"
	case "compare_report":
		return "compare"
	default:
		return "report"
	}
}

func decodeCSVOrJSON(raw string) []string {
	if raw == "" {
		return nil
	}

	if strings.HasPrefix(strings.TrimSpace(raw), "[") {
		var decoded []string
		if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
			return decoded
		}
	}

	var values []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}
