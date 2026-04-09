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
	healthsvc "odin-os/internal/runtime/health"
	jobsvc "odin-os/internal/runtime/jobs"
	runsvc "odin-os/internal/runtime/runs"
	"odin-os/internal/store/sqlite"
)

type Environment struct {
	Store               *sqlite.Store
	Registry            projects.Registry
	RegistryDiagnostics []projects.Diagnostic
	SessionStore        SessionStore
}

type Shell struct {
	env    Environment
	state  State
	health healthsvc.Service
	jobs   jobsvc.Service
	runs   runsvc.Service
}

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
		_, err := fmt.Fprintln(output, "/help /mode /scope /project /jobs /runs /approvals /logs /doctor /self")
		return err
	case "mode":
		return shell.handleMode(command.Args, output)
	case "scope":
		return shell.handleScope(command.Args, output)
	case "project":
		return shell.handleProject(command.Args, output)
	case "jobs":
		return shell.handleJobs(ctx, output)
	case "runs":
		return shell.handleRuns(ctx, output)
	case "approvals":
		return shell.handleApprovals(ctx, output)
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
		if _, err := fmt.Fprintf(output, "%s %s\n", approval.TaskKey, approval.Status); err != nil {
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
		SELECT t.key, a.status, t.scope, p.key
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
		if err := rows.Scan(&approval.TaskKey, &approval.Status, &taskScope, &projectKey); err != nil {
			return nil, err
		}
		if matchesTaskProjectionScope(projectKey, taskScope, shell.state.Scope) {
			approvals = append(approvals, approval)
		}
	}

	return approvals, rows.Err()
}

type pendingApproval struct {
	TaskKey string
	Status  string
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
