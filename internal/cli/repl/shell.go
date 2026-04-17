package repl

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"odin-os/internal/cli/commands"
	"odin-os/internal/cli/render"
	"odin-os/internal/cli/scope"
	clistate "odin-os/internal/cli/state"
	"odin-os/internal/core/capabilities"
	corecommands "odin-os/internal/core/commands"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/registry"
	convsvc "odin-os/internal/runtime/conversation"
	healthsvc "odin-os/internal/runtime/health"
	jobsvc "odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/projections"
	runsvc "odin-os/internal/runtime/runs"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/leases"
	"odin-os/internal/vcs/worktrees"
)

type Environment struct {
	Store               *sqlite.Store
	Registry            projects.Registry
	RegistryDiagnostics []projects.Diagnostic
	SessionStore        SessionStore
	CapabilityGateway   capabilityGateway
	CapabilityService   *capabilities.Service
	CommandService      CommandExecutor
	ExecutorConfig      executorrouter.Config
	Executors           map[string]contract.Executor
	Leases              leases.Manager
}

type CommandExecutor interface {
	Execute(context.Context, capabilities.InvokeRequest) (capabilities.InvokeResponse, error)
}

type Shell struct {
	env            Environment
	state          State
	capabilities   capabilityGateway
	commandService CommandExecutor
	health         healthsvc.Service
	jobs           jobsvc.Service
	runs           runsvc.Service
	transitions    projects.Service
	conversation   convsvc.Service
	worktrees      worktrees.Manager
}

const transitionUsage = "/transition [status] | /transition set <state> [allow=<csv>] [confirm] because <reason...>"
const leaseUsage = "/leases [active|released|all] | /leases inspect <lease-id> | /leases cleanup confirm"

func New(env Environment) (*Shell, error) {
	cache, err := env.SessionStore.Load()
	if err != nil {
		return nil, err
	}

	state := ResolveStartupState(cache, env.Registry)
	leaseManager := env.Leases
	if leaseManager.Store == nil {
		leaseManager.Store = env.Store
	}
	worktreeManager := worktrees.Manager{
		Store: leaseManager.Store,
		Git:   leaseManager.Git,
	}
	shell := &Shell{
		env:   env,
		state: state,
		health: healthsvc.Service{
			DB: env.Store.DB(),
		},
		jobs: jobsvc.Service{
			Store:          env.Store,
			Registry:       env.Registry,
			Executors:      env.Executors,
			ExecutorConfig: env.ExecutorConfig,
			Transitions:    projects.Service{Store: env.Store},
			Leases:         leaseManager,
			Now:            time.Now,
		},
		runs: runsvc.Service{
			DB:    env.Store.DB(),
			Store: env.Store,
		},
		transitions: projects.Service{
			Store: env.Store,
		},
		conversation: convsvc.Service{
			Store:               env.Store,
			Registry:            env.Registry,
			RegistryDiagnostics: env.RegistryDiagnostics,
			ExecutorConfig:      env.ExecutorConfig,
			Executors:           env.Executors,
		},
		worktrees: worktreeManager,
	}
	if env.CommandService != nil {
		shell.commandService = env.CommandService
	}
	if env.CapabilityGateway != nil {
		shell.capabilities = env.CapabilityGateway
	} else if env.CapabilityService != nil {
		shell.capabilities = capabilities.NewGateway(env.CapabilityService, shell.invokeCapability, shell.runs)
	}
	if shell.commandService == nil && shell.capabilities != nil {
		shell.commandService = corecommands.NewService(shell.capabilities)
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
	if _, err := fmt.Fprintf(output, "created task %s (%s)\n", task.Key, task.Status); err != nil {
		return err
	}

	outcome, runErr := shell.jobs.ExecuteTask(ctx, task.ID)
	if outcome.Task.ID != 0 {
		shell.state.ActiveTask = outcome.Task.Key
	}
	if outcome.Run != nil {
		shell.state.ActiveRun = strconv.FormatInt(outcome.Run.ID, 10)
	}
	if err := shell.persistState(); err != nil {
		return err
	}
	return shell.renderActOutcome(output, outcome, runErr)
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
	case "status", "stat":
		return shell.handleRegistryCommand(ctx, command, output)
	case "capabilities":
		return shell.handleCapabilities(command.Args, output)
	case "help":
		if _, err := fmt.Fprintln(output, "prefer explicit cli commands outside the repl: odin help | odin status --json | odin task run --project <key> --title <title> | odin repl"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(output, "repl compatibility commands: /help /mode /scope /workspace /initiatives /project /transition /observe /compare /status /stat /capabilities /leases /jobs [/initiative <key>] /runs /approvals /logs /doctor /self /quit"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "%s\n", transitionUsage); err != nil {
			return err
		}
		_, err := fmt.Fprintf(output, "%s\n", leaseUsage)
		return err
	case "mode":
		return shell.handleMode(command.Args, output)
	case "scope":
		return shell.handleScope(command.Args, output)
	case "workspace":
		return shell.handleWorkspace(ctx, output)
	case "initiatives":
		return shell.handleInitiatives(ctx, output)
	case "project":
		return shell.handleProject(command.Args, output)
	case "transition":
		return shell.handleTransition(ctx, command.Args, output)
	case "observe":
		return shell.handleObserve(ctx, command.Args, output)
	case "compare":
		return shell.handleCompare(ctx, command.Args, output)
	case "leases":
		return shell.handleLeases(ctx, command.Args, output)
	case "jobs":
		return shell.handleJobs(ctx, command.Args, output)
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

func (shell *Shell) handleRegistryCommand(ctx context.Context, command commands.Command, output io.Writer) error {
	resolved, ok := commands.ResolveRegistryCommand(command)
	if !ok {
		_, err := fmt.Fprintf(output, "unknown command: /%s\n", command.Name)
		return err
	}
	if len(command.Args) != 0 {
		_, err := fmt.Fprintf(output, "usage: /%s\n", command.Name)
		return err
	}
	if shell.commandService == nil {
		return fmt.Errorf("command gateway unavailable")
	}

	response, err := shell.commandService.Execute(ctx, capabilities.InvokeRequest{
		CapabilityID:      resolved.CapabilityID,
		CapabilityVersion: resolved.CapabilityVersion,
		Scope: capabilities.ScopeRef{
			Kind:       string(shell.state.Scope.Kind),
			ProjectKey: shell.state.Scope.ProjectKey,
		},
		Caller: capabilities.CallerRef{
			Kind: "cli",
			ID:   "shell",
		},
		Input:     json.RawMessage(`{}`),
		Execution: capabilities.ExecutionRequest{Mode: "local"},
	})
	if err != nil {
		return err
	}
	if len(response.Output) > 0 {
		_, err = fmt.Fprint(output, string(response.Output))
		return err
	}
	if response.Status != "" {
		_, err = fmt.Fprintln(output, response.Status)
		return err
	}
	return nil
}

func (shell *Shell) handleCapabilities(args []string, output io.Writer) error {
	if shell.capabilities == nil {
		_, err := fmt.Fprintln(output, "no capabilities")
		return err
	}

	scopeFilter := string(shell.state.Scope.Kind)
	if len(args) > 0 {
		scopeFilter = strings.ToLower(strings.TrimSpace(args[0]))
	}

	cards := shell.capabilities.ListCapabilities(registry.KindUnknown, scopeFilter)
	if len(cards) == 0 {
		_, err := fmt.Fprintln(output, "no capabilities")
		return err
	}

	for _, card := range cards {
		if _, err := fmt.Fprintf(output, "%s %s %s %s\n", card.ID, card.Version, card.Scope, card.Kind); err != nil {
			return err
		}
	}
	return nil
}

func (shell *Shell) handleAsk(ctx context.Context, line string, output io.Writer) error {
	switch commands.RouteAskIntent(line) {
	case commands.IntentWorkspace:
		return shell.handleCommand(ctx, commands.Command{Name: "workspace"}, output)
	case commands.IntentInitiatives:
		return shell.handleCommand(ctx, commands.Command{Name: "initiatives"}, output)
	}

	result, err := shell.conversation.Respond(ctx, convsvc.Request{
		Scope:  shell.state.Scope,
		Mode:   string(shell.state.Mode),
		Prompt: line,
	})
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "ask failed: %v\n", err)
		return writeErr
	}
	_, err = fmt.Fprintln(output, result.Answer)
	return err
}

func (shell *Shell) invokeCapability(ctx context.Context, request capabilities.InvokeRequest, descriptor capabilities.Descriptor) (capabilities.InvokeResponse, error) {
	switch descriptor.Key {
	case "project.status":
		return shell.executeProjectStatus(ctx, request)
	default:
		return capabilities.InvokeResponse{}, fmt.Errorf("unsupported registry command: %s", descriptor.Key)
	}
}

func (shell *Shell) executeProjectStatus(ctx context.Context, request capabilities.InvokeRequest) (capabilities.InvokeResponse, error) {
	if shell.state.Scope.Kind == scope.ScopeProject || shell.state.Scope.Kind == scope.ScopeOdinCore {
		manifest, err := shell.scopedManifest()
		if err == nil {
			status, err := shell.currentTransitionStatus(ctx, manifest)
			if err == nil {
				return capabilities.InvokeResponse{
					Status: "ok",
					Output: json.RawMessage(renderTransitionStatus(manifest.Key, status)),
				}, nil
			}
		}
	}

	mode := strings.TrimSpace(request.Execution.Mode)
	if mode == "" {
		mode = "local"
	}
	return capabilities.InvokeResponse{
		Status: "ok",
		Output: json.RawMessage(fmt.Sprintf("scope=%s mode=%s\n", shell.scopeLabel(), mode)),
	}, nil
}

func (shell *Shell) renderActOutcome(output io.Writer, outcome jobsvc.ExecutionOutcome, runErr error) error {
	if outcome.Run != nil {
		summary := strings.TrimSpace(outcome.Run.Summary)
		if summary == "" {
			summary = "no summary"
		}
		_, err := fmt.Fprintf(output, "run %d %s %s: %s\n", outcome.Run.ID, outcome.Run.Executor, outcome.Run.Status, summary)
		return err
	}
	if runErr != nil {
		_, err := fmt.Fprintf(output, "run failed before start: %v\n", runErr)
		return err
	}
	return nil
}

func (shell *Shell) handleMode(args []string, output io.Writer) error {
	if len(args) == 0 {
		_, err := fmt.Fprintf(output, "mode=%s\n", shell.state.Mode)
		return err
	}

	requested := Mode(strings.ToLower(args[0]))
	sanitized := clistate.SanitizeMode(requested, shell.state.Scope)
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

	shell.state.Mode = clistate.SanitizeMode(shell.state.Mode, shell.state.Scope)
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
	shell.state.Mode = clistate.SanitizeMode(shell.state.Mode, shell.state.Scope)
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

func (shell *Shell) handleWorkspace(ctx context.Context, output io.Writer) error {
	workspaceKey, err := shell.currentWorkspaceKey(ctx)
	if err != nil {
		_, writeErr := fmt.Fprintln(output, err.Error())
		return writeErr
	}

	homes, err := projections.ListWorkspaceHomeViews(ctx, shell.env.Store.DB())
	if err != nil {
		return err
	}
	for _, home := range homes {
		if home.WorkspaceKey == workspaceKey {
			if _, err := fmt.Fprintf(
				output,
				"workspace=%s initiatives=%d companions=%d approvals=%d blocked=%d\n",
				home.WorkspaceKey,
				home.InitiativeCount,
				home.CompanionCount,
				home.PendingApprovalCount,
				home.BlockedItemCount,
			); err != nil {
				return err
			}

			blocked, err := projections.ListWorkspaceBlockedItemViews(ctx, shell.env.Store.DB(), workspaceKey)
			if err != nil {
				return err
			}
			for _, item := range blocked {
				if _, err := fmt.Fprintf(output, "blocked %s/%s reason=%s next=%s\n", valueOrDefault(item.ProjectKey, "unknown"), item.TaskKey, valueOrDefault(item.Reason, "unknown"), valueOrDefault(item.NextStep, "none")); err != nil {
					return err
				}
			}

			approvals, err := projections.ListWorkspacePendingApprovalViews(ctx, shell.env.Store.DB(), workspaceKey)
			if err != nil {
				return err
			}
			for _, approval := range approvals {
				if _, err := fmt.Fprintf(output, "approval %s/%s %s\n", valueOrDefault(approval.ProjectKey, "unknown"), approval.TaskKey, approval.Status); err != nil {
					return err
				}
			}
			return nil
		}
	}

	_, err = fmt.Fprintf(output, "workspace=%s not found\n", workspaceKey)
	return err
}

func (shell *Shell) handleInitiatives(ctx context.Context, output io.Writer) error {
	workspaceKey, err := shell.currentWorkspaceKey(ctx)
	if err != nil {
		_, writeErr := fmt.Fprintln(output, err.Error())
		return writeErr
	}

	views, err := projections.ListInitiativePortfolioViews(ctx, shell.env.Store.DB(), workspaceKey)
	if err != nil {
		return err
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(output, "no initiatives")
		return err
	}
	for _, view := range views {
		_, err := fmt.Fprintf(output, "%s owner=%s status=%s work_items=%d\n", view.InitiativeKey, valueOrDefault(view.OwnerCompanionKey, "unassigned"), view.Status, view.OpenWorkItemCount)
		if err != nil {
			return err
		}
	}
	return nil
}

func (shell *Shell) handleLeases(ctx context.Context, args []string, output io.Writer) error {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "inspect":
			return shell.handleLeaseInspect(ctx, args[1:], output)
		case "cleanup":
			return shell.handleLeaseCleanup(ctx, args[1:], output)
		}
	}

	filter := "active"
	if len(args) > 0 {
		filter = strings.ToLower(args[0])
	}
	switch filter {
	case "active", "released", "all":
	default:
		_, err := fmt.Fprintln(output, "usage: "+leaseUsage)
		return err
	}

	leasesList, err := shell.env.Store.ListWorktreeLeases(ctx)
	if err != nil {
		return err
	}

	projectKeyByID := map[int64]string{}
	count := 0
	for _, lease := range leasesList {
		projectKey, err := shell.projectKeyForID(ctx, projectKeyByID, lease.ProjectID)
		if err != nil {
			return err
		}
		if !shell.projectInScope(projectKey) {
			continue
		}
		if filter != "all" && lease.State != filter {
			continue
		}

		cleanup := "pending"
		if lease.CleanedUpAt != nil || lease.State == "cleaned" {
			cleanup = "complete"
		}
		if _, err := fmt.Fprintf(output, "project=%s state=%s cleanup=%s task=%d run=%d branch=%s worktree=%s\n", projectKey, lease.State, cleanup, lease.TaskID, lease.RunID, lease.BranchName, lease.WorktreePath); err != nil {
			return err
		}
		count++
	}

	if count == 0 {
		_, err := fmt.Fprintln(output, "no leases")
		return err
	}
	return nil
}

func (shell *Shell) handleJobs(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 2 && strings.EqualFold(args[0], "initiative") {
		workspaceKey, err := shell.currentWorkspaceKey(ctx)
		if err != nil {
			_, writeErr := fmt.Fprintln(output, err.Error())
			return writeErr
		}
		views, err := projections.ListInitiativeWorkItemViews(ctx, shell.env.Store.DB(), workspaceKey, args[1])
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

func (shell *Shell) handleLeaseInspect(ctx context.Context, args []string, output io.Writer) error {
	if len(args) != 1 {
		_, err := fmt.Fprintln(output, "usage: /leases inspect <lease-id>")
		return err
	}

	leaseID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		_, err = fmt.Fprintln(output, "lease id must be numeric")
		return err
	}

	lease, err := shell.env.Store.GetWorktreeLease(ctx, leaseID)
	if err != nil {
		if err == sql.ErrNoRows {
			_, err = fmt.Fprintln(output, "lease not found")
			return err
		}
		return err
	}

	project, err := shell.env.Store.GetProject(ctx, lease.ProjectID)
	if err != nil {
		return err
	}
	if !shell.projectInScope(project.Key) {
		_, err := fmt.Fprintln(output, "lease is outside the current scope")
		return err
	}

	cleanup := "pending"
	if lease.CleanedUpAt != nil || lease.State == "cleaned" {
		cleanup = "complete"
	}
	_, err = fmt.Fprintf(output, "lease_id=%d project=%s task=%d run=%d branch=%s worktree=%s repo_root=%s state=%s cleanup=%s\n", lease.ID, project.Key, lease.TaskID, lease.RunID, lease.BranchName, lease.WorktreePath, lease.RepoRoot, lease.State, cleanup)
	return err
}

func (shell *Shell) handleLeaseCleanup(ctx context.Context, args []string, output io.Writer) error {
	if len(args) != 1 || strings.ToLower(args[0]) != "confirm" {
		_, err := fmt.Fprintln(output, "usage: /leases cleanup confirm")
		return err
	}

	leasesList, err := shell.env.Store.ListWorktreeLeases(ctx)
	if err != nil {
		return err
	}

	projectKeyByID := map[int64]string{}
	cleanupEligible := make([]sqlite.WorktreeLease, 0)
	for _, lease := range leasesList {
		projectKey, err := shell.projectKeyForID(ctx, projectKeyByID, lease.ProjectID)
		if err != nil {
			return err
		}
		if !shell.projectInScope(projectKey) {
			continue
		}
		if lease.State != "released" || lease.CleanedUpAt != nil {
			continue
		}
		cleanupEligible = append(cleanupEligible, lease)
	}

	if len(cleanupEligible) == 0 {
		_, err := fmt.Fprintln(output, "no cleanup-eligible leases")
		return err
	}

	result, err := shell.worktrees.CleanupLeases(ctx, cleanupEligible)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "cleaned %d lease(s)\n", len(result.Removed))
	return err
}

func (shell *Shell) projectKeyForID(ctx context.Context, cache map[int64]string, projectID int64) (string, error) {
	if projectKey := cache[projectID]; projectKey != "" {
		return projectKey, nil
	}
	project, err := shell.env.Store.GetProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	cache[projectID] = project.Key
	return project.Key, nil
}

func (shell *Shell) projectInScope(projectKey string) bool {
	switch shell.state.Scope.Kind {
	case scope.ScopeGlobal:
		return true
	case scope.ScopeProject, scope.ScopeOdinCore:
		return projectKey == shell.state.Scope.ProjectKey
	default:
		return false
	}
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
	shell.state.Mode = clistate.SanitizeMode(shell.state.Mode, shell.state.Scope)
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

func (shell *Shell) currentWorkspaceKey(ctx context.Context) (string, error) {
	if shell.state.Scope.Kind == scope.ScopeProject || shell.state.Scope.Kind == scope.ScopeOdinCore {
		project, err := shell.env.Store.GetProjectByKey(ctx, shell.state.Scope.ProjectKey)
		if err == nil {
			initiative, err := shell.env.Store.GetInitiativeByProjectID(ctx, project.ID)
			if err == nil {
				workspace, err := shell.env.Store.GetWorkspace(ctx, initiative.WorkspaceID)
				if err == nil {
					return workspace.Key, nil
				}
			}
		}
	}

	homes, err := projections.ListWorkspaceHomeViews(ctx, shell.env.Store.DB())
	if err != nil {
		return "", err
	}
	if len(homes) == 0 {
		return "", fmt.Errorf("no workspace found")
	}
	return homes[0].WorkspaceKey, nil
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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
	case scope.ScopeProject, scope.ScopeOdinCore:
		return true
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
