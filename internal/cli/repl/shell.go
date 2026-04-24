package repl

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	webdriver "odin-os/internal/adapters/web"
	"odin-os/internal/cli/commands"
	clioverview "odin-os/internal/cli/overview"
	"odin-os/internal/cli/render"
	"odin-os/internal/cli/scope"
	clistate "odin-os/internal/cli/state"
	"odin-os/internal/core/capabilities"
	corecommands "odin-os/internal/core/commands"
	"odin-os/internal/core/projects"
	corescope "odin-os/internal/core/scope"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	knowledgememory "odin-os/internal/memory/knowledge"
	"odin-os/internal/registry"
	approvalsvc "odin-os/internal/runtime/approvals"
	checkpointsvc "odin-os/internal/runtime/checkpoints"
	convsvc "odin-os/internal/runtime/conversation"
	healthsvc "odin-os/internal/runtime/health"
	jobsvc "odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/projections"
	runsvc "odin-os/internal/runtime/runs"
	transfersvc "odin-os/internal/runtime/transfers"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tools/broker"
	"odin-os/internal/tools/budgets"
	"odin-os/internal/tools/catalog"
	"odin-os/internal/tools/invocation"
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
	TransferInvocation  invocation.Service
	Leases              leases.Manager
	Now                 func() time.Time
}

type CommandExecutor interface {
	Execute(context.Context, capabilities.InvokeRequest) (capabilities.InvokeResponse, error)
}

type Shell struct {
	env            Environment
	state          State
	capabilities   capabilityGateway
	commandService CommandExecutor
	approvals      approvalsvc.Service
	health         healthsvc.Service
	jobs           jobsvc.Service
	runs           runsvc.Service
	transfers      transfersvc.Service
	transitions    projects.Service
	conversation   convsvc.Service
	worktrees      worktrees.Manager
	now            func() time.Time
}

const transitionUsage = "/transition [status] | /transition set <state> [allow=<csv>] [confirm] because <reason...>"
const leaseUsage = "/leases [active|released|all] | /leases inspect <lease-id> | /leases cleanup confirm"
const toolUsage = "/tool [list|show <tool-key>|run <tool-key> key=value]"
const memoryUsage = "/memory [workspace|initiatives|companions|list|publish <id> [url=<value>|via=huginn_x]]"

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
	now := env.Now
	if now == nil {
		now = func() time.Time {
			return time.Now().UTC()
		}
	}
	shell := &Shell{
		env:   env,
		state: state,
		approvals: approvalsvc.Service{
			Store:      env.Store,
			Invocation: env.TransferInvocation,
		},
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
		transfers: transfersvc.Service{
			Store:       env.Store,
			Registry:    env.Registry,
			Checkpoints: checkpointsvc.Service{Store: env.Store},
			Invocation:  env.TransferInvocation,
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
		now:       now,
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
	return nil
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
		if _, err := fmt.Fprintln(output, "/help /mode /scope /memory /overview /workspace /initiatives /companions /agenda /project /tool /transition /observe /compare /jobs /runs /approvals /transfer /logs /doctor /doctor json /doctor report /self"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(output, "repl compatibility commands: /help /mode /scope /memory /overview /project /tool /transition /observe /compare /status /stat /capabilities /leases /jobs /runs /approvals /agenda /transfer /logs /doctor /doctor json /doctor report /self /quit"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "%s\n", transitionUsage); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "%s\n", toolUsage); err != nil {
			return err
		}
		_, err := fmt.Fprintf(output, "%s\n", leaseUsage)
		return err
	case "mode":
		return shell.handleMode(command.Args, output)
	case "scope":
		return shell.handleScope(command.Args, output)
	case "memory":
		return shell.handleMemory(ctx, command.Args, output)
	case "overview":
		return shell.handleOverview(ctx, output)
	case "workspace":
		return shell.handleWorkspace(ctx, output)
	case "initiatives":
		return shell.handleInitiatives(ctx, output)
	case "companions":
		return shell.handleCompanions(ctx, output)
	case "agenda":
		return shell.handleAgenda(ctx, output)
	case "project":
		return shell.handleProject(command.Args, output)
	case "tool":
		return shell.handleTool(ctx, command.Args, output)
	case "transition":
		return shell.handleTransition(ctx, command.Args, output)
	case "observe":
		return shell.handleObserve(ctx, command.Args, output)
	case "compare":
		return shell.handleCompare(ctx, command.Args, output)
	case "leases":
		return shell.handleLeases(ctx, command.Args, output)
	case "jobs":
		return shell.handleJobs(ctx, output)
	case "runs":
		return shell.handleRuns(ctx, command.Args, output)
	case "approvals":
		return shell.handleApprovals(ctx, command.Args, output)
	case "transfer":
		return shell.handleTransfer(ctx, command.Args, output)
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
	case commands.IntentMemory:
		return shell.handleCommand(ctx, commands.Command{Name: "memory"}, output)
	case commands.IntentOverview:
		return shell.handleCommand(ctx, commands.Command{Name: "overview"}, output)
	case commands.IntentWorkspace:
		return shell.handleCommand(ctx, commands.Command{Name: "workspace"}, output)
	case commands.IntentInitiatives:
		return shell.handleCommand(ctx, commands.Command{Name: "initiatives"}, output)
	case commands.IntentCompanions:
		return shell.handleCommand(ctx, commands.Command{Name: "companions"}, output)
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
		if shell.conversation.Store != nil {
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
		_, err := fmt.Fprintln(output, "local ask is limited in Phase 05. Try /help, /scope, /memory, /overview, /workspace, /initiatives, /companions, /agenda, /project, /jobs, /runs, /approvals, /transfer, /logs, or /doctor.")
		return err
	}
}

func (shell *Shell) handleTool(ctx context.Context, args []string, output io.Writer) error {
	_ = ctx

	if len(args) == 0 {
		_, err := fmt.Fprintf(output, "usage: %s\n", toolUsage)
		return err
	}

	switch strings.ToLower(args[0]) {
	case "list":
		cards := shell.newBroker().Catalog(shell.catalogScope())
		count := 0
		for _, card := range cards {
			if card.Kind != catalog.KindTool {
				continue
			}
			if _, err := fmt.Fprintf(output, "%s %s\n", card.Key, card.Summary); err != nil {
				return err
			}
			count++
		}
		if count == 0 {
			_, err := fmt.Fprintln(output, "no tools")
			return err
		}
		return nil
	case "show":
		if len(args) != 2 {
			_, err := fmt.Fprintf(output, "usage: %s\n", toolUsage)
			return err
		}
		toolBroker := shell.newBroker()
		expansion, err := toolBroker.Expand(args[1])
		if err != nil {
			_, writeErr := fmt.Fprintf(output, "unknown tool: %s\n", args[1])
			return writeErr
		}
		if expansion.Tool == nil {
			_, err := fmt.Fprintf(output, "capability %s is not a tool\n", args[1])
			return err
		}
		if !catalog.MatchesScope(expansion.Card.Scopes, shell.catalogScope()) {
			_, err := fmt.Fprintf(output, "tool %s is not available in %s scope\n", args[1], shell.catalogScope())
			return err
		}
		return renderToolDetail(output, *expansion.Tool)
	case "run":
		if len(args) < 2 {
			_, err := fmt.Fprintf(output, "usage: %s\n", toolUsage)
			return err
		}
		input, err := parseCommandInput(args[2:])
		if err != nil {
			_, writeErr := fmt.Fprintf(output, "%v\nusage: %s\n", err, toolUsage)
			return writeErr
		}

		toolBroker := shell.newBroker()
		expansion, err := toolBroker.Expand(args[1])
		if err != nil {
			_, writeErr := fmt.Fprintf(output, "unknown tool: %s\n", args[1])
			return writeErr
		}
		if expansion.Tool == nil {
			_, err := fmt.Fprintf(output, "capability %s is not a tool\n", args[1])
			return err
		}
		if !catalog.MatchesScope(expansion.Card.Scopes, shell.catalogScope()) {
			_, err := fmt.Fprintf(output, "tool %s is not available in %s scope\n", args[1], shell.catalogScope())
			return err
		}

		result, err := toolBroker.InvokeTool(args[1], input)
		if err != nil {
			_, writeErr := fmt.Fprintf(output, "tool %s failed: %v\n", args[1], err)
			return writeErr
		}
		if err := renderToolResult(output, result); err != nil {
			return err
		}
		return shell.recordToolMemory(ctx, output, result)
	default:
		_, err := fmt.Fprintf(output, "usage: %s\n", toolUsage)
		return err
	}
}

func (shell *Shell) recordToolMemory(ctx context.Context, output io.Writer, result catalog.StructuredResult) error {
	if len(result.MemoryRecords) == 0 {
		return nil
	}

	scope, err := shell.memoryScope(ctx)
	if err != nil {
		return err
	}
	for _, memoryRecord := range result.MemoryRecords {
		details, err := shell.memoryDetailsJSON(scope, memoryRecord.Fields)
		if err != nil {
			return err
		}

		summary := strings.TrimSpace(memoryRecord.Summary)
		if summary == "" {
			summary = strings.TrimSpace(result.Summary)
		}

		recorded, err := knowledgememory.Service{Store: shell.env.Store}.Record(ctx, scope, memoryRecord.MemoryType, summary, details, nil)
		if err != nil {
			return err
		}

		if _, err := fmt.Fprintf(output, "tool_memory=%d type=%s scope=%s/%s status=recorded\nsummary=%s\n", recorded.ID, recorded.MemoryType, recorded.Scope, recorded.ScopeKey, strings.TrimSpace(recorded.Summary)); err != nil {
			return err
		}
		if details, err := parseMemoryDetails(recorded.DetailsJSON); err == nil {
			if renderedFields := formatMemoryFields(details.Fields); renderedFields != "" {
				if _, err := fmt.Fprintf(output, "fields=%s\n", renderedFields); err != nil {
					return err
				}
			}
		}
		if _, err := fmt.Fprintf(output, "details_json=%s\n", strings.TrimSpace(recorded.DetailsJSON)); err != nil {
			return err
		}
	}

	return nil
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
		control := shell.controlScope()
		_, err := fmt.Fprintf(
			output,
			"scope=%s subject=%s workspace=%s initiative=%s project=%s companion=%s\n",
			shell.scopeLabel(),
			control.SubjectType,
			control.WorkspaceKey,
			control.InitiativeKey,
			control.ProjectKey,
			control.CompanionKey,
		)
		return err
	}

	if len(args) == 1 && strings.EqualFold(args[0], "current") {
		return shell.handleScope(nil, output)
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

func (shell *Shell) handleOverview(ctx context.Context, output io.Writer) error {
	view, err := clioverview.Service{
		Store:            shell.env.Store,
		RegistrySnapshot: shell.registrySnapshot(),
	}.Build(ctx, shell.state.Scope)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(output, render.RenderOverview(view))
	return err
}

func (shell *Shell) handleRuns(ctx context.Context, args []string, output io.Writer) error {
	if len(args) > 0 {
		if strings.EqualFold(args[0], "show") {
			return shell.handleRunShow(ctx, args[1:], output)
		}
		_, err := fmt.Fprintln(output, "usage: /runs | /runs show <run-id|active>")
		return err
	}

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

func (shell *Shell) handleRunShow(ctx context.Context, args []string, output io.Writer) error {
	if len(args) != 1 {
		_, err := fmt.Fprintln(output, "usage: /runs show <run-id|active>")
		return err
	}

	var runID int64
	if strings.EqualFold(args[0], "active") {
		if strings.TrimSpace(shell.state.ActiveRun) == "" {
			_, err := fmt.Fprintln(output, "no active run")
			return err
		}
		parsed, err := strconv.ParseInt(shell.state.ActiveRun, 10, 64)
		if err != nil || parsed <= 0 {
			_, writeErr := fmt.Fprintln(output, "active run is invalid")
			return writeErr
		}
		runID = parsed
	} else {
		parsed, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil || parsed <= 0 {
			_, writeErr := fmt.Fprintln(output, "run id must be a positive integer")
			return writeErr
		}
		runID = parsed
	}

	detail, err := shell.runs.Show(ctx, shell.state.Scope, runID)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "unable to show run: %v\n", err)
		return writeErr
	}

	if _, err := fmt.Fprintf(output, "run=%d task=%s status=%s executor=%s\n", detail.RunID, detail.TaskKey, detail.Status, detail.Executor); err != nil {
		return err
	}
	if strings.TrimSpace(detail.Summary) != "" {
		if _, err := fmt.Fprintf(output, "summary=%s\n", detail.Summary); err != nil {
			return err
		}
	}
	for _, artifact := range detail.Artifacts {
		if _, err := fmt.Fprintf(output, "artifact=%s summary=%s\n", artifact.ArtifactType, artifact.Summary); err != nil {
			return err
		}
		if details := strings.TrimSpace(artifact.DetailsJSON); details != "" && details != "{}" {
			if _, err := fmt.Fprintf(output, "details=%s\n", details); err != nil {
				return err
			}
		}
	}
	return nil
}

func (shell *Shell) handleWorkspace(ctx context.Context, output io.Writer) error {
	view, err := projections.GetWorkspaceOverviewView(ctx, shell.env.Store.DB(), workspaces.DefaultWorkspaceKey)
	if err != nil {
		if err == sql.ErrNoRows {
			_, writeErr := fmt.Fprintln(output, "no workspace")
			return writeErr
		}
		return err
	}

	_, err = fmt.Fprintf(
		output,
		"workspace=%s status=%s owner=%s default_companion=%s initiatives=%d companions=%d open_work=%d active_runs=%d approvals=%d incidents=%d blocked=%d\n",
		view.WorkspaceKey,
		view.Status,
		view.OwnerRef,
		view.DefaultCompanionKey,
		view.ActiveInitiativeCount,
		view.ActiveCompanionCount,
		view.OpenWorkItemCount,
		view.ActiveRunCount,
		view.PendingApprovalCount,
		view.OpenIncidentCount,
		view.BlockedWorkItemCount,
	)
	return err
}

func (shell *Shell) handleMemory(ctx context.Context, args []string, output io.Writer) error {
	switch len(args) {
	case 0:
		return shell.handleWorkspaceMemory(ctx, output)
	default:
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "workspace":
			return shell.handleWorkspaceMemory(ctx, output)
		case "initiatives":
			return shell.handleInitiativeMemory(ctx, output)
		case "companions":
			return shell.handleCompanionMemory(ctx, output)
		case "list":
			return shell.handleMemoryList(ctx, output)
		case "publish":
			return shell.handleMemoryPublish(ctx, args[1:], output)
		}
	}
	_, err := fmt.Fprintln(output, "usage: "+memoryUsage)
	return err
}

func (shell *Shell) handleMemoryList(ctx context.Context, output io.Writer) error {
	scope, err := shell.memoryScope(ctx)
	if err != nil {
		return err
	}

	summaries, err := knowledgememory.Service{Store: shell.env.Store}.List(ctx, scope, "")
	if err != nil {
		return err
	}
	if len(summaries) == 0 {
		_, err := fmt.Fprintln(output, "no memory")
		return err
	}
	for _, summary := range summaries {
		if err := renderMemorySummary(output, summary); err != nil {
			return err
		}
	}
	return nil
}

func (shell *Shell) handleWorkspaceMemory(ctx context.Context, output io.Writer) error {
	views, err := projections.ListWorkspaceMemoryViews(ctx, shell.env.Store.DB(), projections.WorkspaceMemoryQuery{
		WorkspaceKey: workspaces.DefaultWorkspaceKey,
		Limit:        1,
	})
	if err != nil {
		return err
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(output, "no workspace memory")
		return err
	}

	view := views[0]
	_, err = fmt.Fprintf(
		output,
		"workspace=%s workspace_entries=%d initiative_entries=%d companion_entries=%d\n",
		view.WorkspaceKey,
		view.WorkspaceEntryCount,
		view.InitiativeEntryCount,
		view.CompanionEntryCount,
	)
	return err
}

func (shell *Shell) handleInitiativeMemory(ctx context.Context, output io.Writer) error {
	query := projections.InitiativeMemoryQuery{
		WorkspaceKey: workspaces.DefaultWorkspaceKey,
		Limit:        20,
	}
	switch shell.state.Scope.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore, scope.ScopeNewProject:
		if initiativeKey := shell.controlScope().InitiativeKey; initiativeKey != "" {
			query.InitiativeKey = initiativeKey
		}
	}

	views, err := projections.ListInitiativeMemoryViews(ctx, shell.env.Store.DB(), query)
	if err != nil {
		return err
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(output, "no initiative memory")
		return err
	}

	for _, view := range views {
		if _, err := fmt.Fprintf(output, "%s entries=%d %s\n", view.InitiativeKey, view.EntryCount, view.LastSummary); err != nil {
			return err
		}
	}
	return nil
}

func (shell *Shell) handleCompanionMemory(ctx context.Context, output io.Writer) error {
	views, err := projections.ListCompanionMemoryViews(ctx, shell.env.Store.DB(), projections.CompanionMemoryQuery{
		WorkspaceKey: workspaces.DefaultWorkspaceKey,
		Limit:        20,
	})
	if err != nil {
		return err
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(output, "no companion memory")
		return err
	}

	for _, view := range views {
		if _, err := fmt.Fprintf(output, "%s entries=%d %s\n", view.CompanionKey, view.EntryCount, view.LastSummary); err != nil {
			return err
		}
	}
	return nil
}

func (shell *Shell) handleMemoryPublish(ctx context.Context, args []string, output io.Writer) error {
	request, err := parseMemoryPublishArgs(args)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "%v\nusage: %s\n", err, memoryUsage)
		return writeErr
	}

	summary, ok, err := shell.visibleMemorySummaryByID(ctx, request.MemoryID)
	if err != nil {
		return err
	}
	if !ok {
		_, err := fmt.Fprintf(output, "unknown memory: %d\n", request.MemoryID)
		return err
	}
	if summary.MemoryType != "social_outcome" {
		_, err := fmt.Fprintf(output, "only social_outcome memories can be published\nusage: %s\n", memoryUsage)
		return err
	}

	details, err := parseMemoryDetails(summary.DetailsJSON)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "memory details are invalid: %v\n", err)
		return writeErr
	}
	details = normalizeMemoryDetailsPayload(summary, details)
	if strings.TrimSpace(details.Fields["result"]) != "approved" {
		_, err := fmt.Fprintf(output, "only approved social_outcome memories can be published\nusage: %s\n", memoryUsage)
		return err
	}
	if strings.TrimSpace(details.Fields["publish_status"]) == "published" {
		_, err := fmt.Fprintf(output, "social_outcome is already marked published\nusage: %s\n", memoryUsage)
		return err
	}

	if request.Via == "huginn_x" {
		contentKind := strings.TrimSpace(details.Fields["content_kind"])
		if strings.TrimSpace(details.Fields["channel"]) != "x" || (contentKind != "post" && contentKind != "reply") {
			_, err := fmt.Fprintf(output, "native X publish requires channel=x and content_kind=post or reply\nusage: %s\n", memoryUsage)
			return err
		}
		if contentKind == "reply" {
			replyTarget := strings.TrimSpace(details.Fields["in_reply_to_url"])
			if replyTarget == "" {
				_, err := fmt.Fprintf(output, "native X reply publish requires in_reply_to_url\nusage: %s\n", memoryUsage)
				return err
			}
			if !isAllowedXStatusURL(replyTarget) {
				_, err := fmt.Fprintf(output, "native X reply publish requires in_reply_to_url to be a valid X status URL\nusage: %s\n", memoryUsage)
				return err
			}
		}

		artifacts, err := shell.publishApprovedXOutcomeWithHuginn(ctx, summary)
		if err != nil {
			_, writeErr := fmt.Fprintf(output, "native X publish failed: %v\nusage: %s\n", err, memoryUsage)
			return writeErr
		}

		publishURL := strings.TrimSpace(stringMapValue(artifacts, "publish_url"))
		if publishURL == "" {
			_, err := fmt.Fprintf(output, "native X publish failed: publish_url missing\nusage: %s\n", memoryUsage)
			return err
		}

		publishedAt := strings.TrimSpace(stringMapValue(artifacts, "published_at"))
		if publishedAt == "" {
			publishedAt = request.PublishedAt.Format(time.RFC3339)
		}

		details.Fields["publish_status"] = "published"
		details.Fields["publish_mode"] = "huginn_x"
		details.Fields["publish_url"] = publishURL
		details.Fields["published_at"] = publishedAt
		if screenshotPath := strings.TrimSpace(stringMapValue(artifacts, "screenshot_path")); screenshotPath != "" {
			details.Fields["publish_screenshot_path"] = screenshotPath
		}
	} else {
		details.Fields["publish_status"] = "published"
		details.Fields["publish_url"] = request.URL
		details.Fields["published_at"] = request.PublishedAt.Format(time.RFC3339)
	}

	updatedDetailsJSON, err := marshalMemoryDetailsPayload(details)
	if err != nil {
		return err
	}
	updatedSummary, err := shell.env.Store.UpdateMemorySummaryDetails(ctx, sqlite.UpdateMemorySummaryDetailsParams{
		MemoryID:    summary.ID,
		DetailsJSON: updatedDetailsJSON,
	})
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(output, "memory=%d type=%s scope=%s/%s status=published\nsummary=%s\n", updatedSummary.ID, updatedSummary.MemoryType, updatedSummary.Scope, updatedSummary.ScopeKey, strings.TrimSpace(updatedSummary.Summary)); err != nil {
		return err
	}
	if details, err := parseMemoryDetails(updatedSummary.DetailsJSON); err == nil {
		if renderedFields := formatMemoryFields(details.Fields); renderedFields != "" {
			if _, err := fmt.Fprintf(output, "fields=%s\n", renderedFields); err != nil {
				return err
			}
		}
	}
	_, err = fmt.Fprintf(output, "details_json=%s\n", strings.TrimSpace(updatedSummary.DetailsJSON))
	return err
}

func (shell *Shell) memoryScope(ctx context.Context) (knowledgememory.Scope, error) {
	switch shell.state.Scope.Kind {
	case scope.ScopeGlobal:
		return knowledgememory.Scope{Value: "global", Key: "global"}, nil
	case scope.ScopeProject, scope.ScopeOdinCore:
		if strings.TrimSpace(shell.state.Scope.ProjectKey) == "" {
			return knowledgememory.Scope{}, fmt.Errorf("memory scope requires a selected project")
		}
		project, err := shell.env.Store.GetProjectByKey(ctx, shell.state.Scope.ProjectKey)
		if err != nil {
			return knowledgememory.Scope{}, err
		}
		return knowledgememory.Scope{
			ProjectID: &project.ID,
			Value:     string(shell.state.Scope.Kind),
			Key:       shell.state.Scope.ProjectKey,
		}, nil
	default:
		if strings.TrimSpace(shell.state.Scope.ProjectKey) == "" {
			return knowledgememory.Scope{}, fmt.Errorf("memory scope requires a selected project")
		}
		return knowledgememory.Scope{
			Value: string(shell.state.Scope.Kind),
			Key:   shell.state.Scope.ProjectKey,
		}, nil
	}
}

func (shell *Shell) visibleMemorySummaryByID(ctx context.Context, memoryID int64) (sqlite.MemorySummary, bool, error) {
	scope, err := shell.memoryScope(ctx)
	if err != nil {
		return sqlite.MemorySummary{}, false, err
	}
	summaries, err := knowledgememory.Service{Store: shell.env.Store}.List(ctx, scope, "")
	if err != nil {
		return sqlite.MemorySummary{}, false, err
	}
	for _, summary := range summaries {
		if summary.ID == memoryID {
			return summary, true, nil
		}
	}
	return sqlite.MemorySummary{}, false, nil
}

func (shell *Shell) publishApprovedXOutcomeWithHuginn(ctx context.Context, summary sqlite.MemorySummary) (map[string]any, error) {
	details, err := parseMemoryDetails(summary.DetailsJSON)
	if err != nil {
		return nil, err
	}
	details = normalizeMemoryDetailsPayload(summary, details)

	result, err := invocation.Service{}.HuginnXPostPublish(ctx, webdriver.XPublishRequest{
		ToolKey: "browser_x_post_publish",
		Input: webdriver.XPublishInput{
			PostText: approvedOutcomePublishText(summary.Summary),
			ContentKind: func() string {
				if value := strings.TrimSpace(details.Fields["content_kind"]); value != "" {
					return value
				}
				return "post"
			}(),
			InReplyToURL: strings.TrimSpace(details.Fields["in_reply_to_url"]),
			Label:        fmt.Sprintf("social-outcome-%d", summary.ID),
			WaitMS:       "4000",
			Headless:     "false",
		},
	})
	if err != nil {
		return nil, err
	}
	return result.Artifacts, nil
}

func (shell *Shell) memoryDetailsJSON(scope knowledgememory.Scope, fields map[string]string) (string, error) {
	_ = scope
	payload := memoryDetailsPayload{Fields: fields}
	return marshalMemoryDetailsPayload(payload)
}

func approvedOutcomePublishText(summary string) string {
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" {
		return ""
	}

	lower := strings.ToLower(trimmed)
	for _, marker := range []string{
		"\n\napproval checklist:",
		"\napproval checklist:",
		"approval checklist:",
	} {
		if idx := strings.Index(lower, marker); idx >= 0 {
			return strings.TrimSpace(trimmed[:idx])
		}
	}

	return trimmed
}

func isAllowedXStatusURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	switch host {
	case "x.com", "www.x.com", "twitter.com", "www.twitter.com":
	default:
		return false
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 3 {
		return false
	}
	if parts[len(parts)-2] != "status" {
		return false
	}
	if parts[len(parts)-3] == "" {
		return false
	}
	if _, err := strconv.ParseInt(parts[len(parts)-1], 10, 64); err != nil {
		return false
	}
	return true
}

func parseMemoryPublishArgs(args []string) (memoryPublishRequest, error) {
	if len(args) == 0 {
		return memoryPublishRequest{}, fmt.Errorf("memory id is required")
	}

	memoryID, err := strconv.ParseInt(strings.TrimSpace(args[0]), 10, 64)
	if err != nil || memoryID <= 0 {
		return memoryPublishRequest{}, fmt.Errorf("memory id must be a positive integer")
	}

	request := memoryPublishRequest{
		MemoryID:    memoryID,
		PublishedAt: time.Now().UTC(),
	}
	for _, token := range args[1:] {
		key, value, ok := strings.Cut(strings.TrimSpace(token), "=")
		if !ok {
			return memoryPublishRequest{}, fmt.Errorf("unknown memory publish option: %s", token)
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "url":
			request.URL = strings.TrimSpace(value)
		case "via":
			request.Via = strings.TrimSpace(strings.ToLower(value))
		case "published_at":
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
			if err != nil {
				return memoryPublishRequest{}, fmt.Errorf("published_at must be RFC3339")
			}
			request.PublishedAt = parsed.UTC()
		default:
			return memoryPublishRequest{}, fmt.Errorf("unknown memory publish option: %s", token)
		}
	}

	if request.Via != "" {
		if request.Via != "huginn_x" {
			return memoryPublishRequest{}, fmt.Errorf("memory publish requires via=huginn_x when native publish is requested")
		}
		if strings.TrimSpace(request.URL) != "" {
			return memoryPublishRequest{}, fmt.Errorf("memory publish accepts either url=<value> or via=huginn_x")
		}
		return request, nil
	}

	if strings.TrimSpace(request.URL) == "" {
		return memoryPublishRequest{}, fmt.Errorf("memory publish requires url=<value>")
	}

	return request, nil
}

func stringMapValue(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok {
		return ""
	}
	if value, ok := raw.(string); ok {
		return value
	}
	return fmt.Sprint(raw)
}

type memoryPublishRequest struct {
	MemoryID    int64
	URL         string
	Via         string
	PublishedAt time.Time
}

type memoryDetailsPayload struct {
	Fields map[string]string `json:"fields,omitempty"`
}

func parseMemoryDetails(detailsJSON string) (memoryDetailsPayload, error) {
	var payload memoryDetailsPayload
	if strings.TrimSpace(detailsJSON) == "" {
		return payload, nil
	}
	if err := json.Unmarshal([]byte(detailsJSON), &payload); err != nil {
		return memoryDetailsPayload{}, err
	}
	if payload.Fields == nil {
		payload.Fields = map[string]string{}
	}
	return payload, nil
}

func marshalMemoryDetailsPayload(payload memoryDetailsPayload) (string, error) {
	if payload.Fields == nil {
		payload.Fields = map[string]string{}
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func normalizeMemoryDetailsPayload(summary sqlite.MemorySummary, payload memoryDetailsPayload) memoryDetailsPayload {
	if payload.Fields == nil {
		payload.Fields = map[string]string{}
	}
	return payload
}

func renderMemorySummary(output io.Writer, summary sqlite.MemorySummary) error {
	if _, err := fmt.Fprintf(output, "memory=%d type=%s scope=%s/%s created_at=%s\nsummary=%s\n", summary.ID, summary.MemoryType, summary.Scope, summary.ScopeKey, summary.CreatedAt.Format(time.RFC3339), strings.TrimSpace(summary.Summary)); err != nil {
		return err
	}
	if details, err := parseMemoryDetails(summary.DetailsJSON); err == nil {
		if renderedFields := formatMemoryFields(details.Fields); renderedFields != "" {
			if _, err := fmt.Fprintf(output, "fields=%s\n", renderedFields); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(output, "details_json=%s\n", strings.TrimSpace(summary.DetailsJSON))
	return err
}

func formatMemoryFields(fields map[string]string) string {
	if len(fields) == 0 {
		return ""
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(fields[key])
		if value == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}
	return strings.Join(parts, " ")
}

func (shell *Shell) handleInitiatives(ctx context.Context, output io.Writer) error {
	views, err := projections.ListInitiativePortfolioViews(ctx, shell.env.Store.DB(), workspaces.DefaultWorkspaceKey)
	if err != nil {
		return err
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(output, "no initiatives")
		return err
	}

	for _, view := range views {
		owner := "none"
		if view.OwnerCompanionKey != nil {
			owner = *view.OwnerCompanionKey
		}
		project := "none"
		if view.LinkedProjectKey != nil {
			project = *view.LinkedProjectKey
		}
		if _, err := fmt.Fprintf(
			output,
			"%s %s %s owner=%s project=%s open=%d runs=%d approvals=%d incidents=%d blocked=%d\n",
			view.InitiativeKey,
			view.Kind,
			view.Status,
			owner,
			project,
			view.OpenWorkItemCount,
			view.ActiveRunCount,
			view.PendingApprovalCount,
			view.OpenIncidentCount,
			view.BlockedWorkItemCount,
		); err != nil {
			return err
		}
	}
	return nil
}

func (shell *Shell) handleCompanions(ctx context.Context, output io.Writer) error {
	views, err := projections.ListCompanionAssignmentViews(ctx, shell.env.Store.DB(), workspaces.DefaultWorkspaceKey)
	if err != nil {
		return err
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(output, "no companions")
		return err
	}

	for _, view := range views {
		if _, err := fmt.Fprintf(
			output,
			"%s %s %s owned_initiatives=%d open=%d runs=%d approvals=%d blocked=%d\n",
			view.CompanionKey,
			view.Kind,
			view.Status,
			view.OwnedInitiativeCount,
			view.OpenWorkItemCount,
			view.ActiveRunCount,
			view.PendingApprovalCount,
			view.BlockedWorkItemCount,
		); err != nil {
			return err
		}
	}
	return nil
}

func (shell *Shell) handleApprovals(ctx context.Context, args []string, output io.Writer) error {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "resolve":
			return shell.handleApprovalResolve(ctx, args[1:], output)
		case "show":
			return shell.handleApprovalShow(ctx, args[1:], output)
		default:
			_, err := fmt.Fprintln(output, "usage: /approvals | /approvals show <approval-id> | /approvals resolve <approval-id> <approve|deny> because <reason...>")
			return err
		}
	}
	approvals, err := shell.pendingApprovals(ctx)
	if err != nil {
		return err
	}
	if len(approvals) == 0 {
		_, err := fmt.Fprintln(output, "no approvals waiting")
		return err
	}
	for _, approval := range approvals {
		detail, err := shell.approvals.Detail(ctx, approval.ApprovalID)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(
			output,
			"approval=%d task=%s run=%s status=%s resolver=%s\n",
			approval.ApprovalID,
			approval.TaskKey,
			formatNullableInt64(detail.Approval.RunID),
			approval.Status,
			detail.ResolverSupport,
		); err != nil {
			return err
		}
	}
	return nil
}

func (shell *Shell) handleAgenda(ctx context.Context, output io.Writer) error {
	view, err := projections.GetAgendaView(ctx, shell.env.Store.DB(), workspaces.DefaultWorkspaceKey, shell.now().UTC())
	if err != nil {
		if err == sql.ErrNoRows {
			_, writeErr := fmt.Fprintln(output, "no agenda items")
			return writeErr
		}
		return err
	}
	return commands.WriteAgendaText(output, view)
}

func (shell *Shell) handleApprovalResolve(ctx context.Context, args []string, output io.Writer) error {
	if len(args) < 4 || !strings.EqualFold(args[2], "because") {
		_, err := fmt.Fprintln(output, "usage: /approvals resolve <approval-id> <approve|deny> because <reason...>")
		return err
	}

	approvalID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || approvalID <= 0 {
		_, writeErr := fmt.Fprintln(output, "approval id must be a positive integer")
		return writeErr
	}

	result, err := shell.approvals.Resolve(ctx, approvalsvc.ResolveParams{
		ApprovalID: approvalID,
		Action:     strings.ToLower(strings.TrimSpace(args[1])),
		DecisionBy: "operator",
		Reason:     strings.Join(args[3:], " "),
	})
	if err != nil {
		if errors.Is(err, approvalsvc.ErrUnsupportedResolver) {
			receipt, receiptErr := approvalsvc.FormatReceipt(result)
			if receiptErr != nil {
				_, writeErr := fmt.Fprintf(output, "unable to render approval receipt: %v\n", receiptErr)
				return writeErr
			}
			if _, writeErr := fmt.Fprintln(output, receipt.Line); writeErr != nil {
				return writeErr
			}
			_, writeErr := fmt.Fprintln(output, receipt.Summary)
			return writeErr
		}
		_, writeErr := fmt.Fprintf(output, "unable to resolve approval: %v\n", err)
		return writeErr
	}
	if result.SubmitRun != nil {
		shell.state.ActiveRun = strconv.FormatInt(result.SubmitRun.ID, 10)
		if err := shell.persistState(); err != nil {
			return err
		}
	}

	receipt, err := approvalsvc.FormatReceipt(result)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "unable to render approval receipt: %v\n", err)
		return writeErr
	}
	if _, err := fmt.Fprintln(output, receipt.Line); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, receipt.Summary)
	return err
}

func (shell *Shell) handleApprovalShow(ctx context.Context, args []string, output io.Writer) error {
	if len(args) != 1 {
		_, err := fmt.Fprintln(output, "usage: /approvals show <approval-id>")
		return err
	}

	approvalID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || approvalID <= 0 {
		_, writeErr := fmt.Fprintln(output, "approval id must be a positive integer")
		return writeErr
	}

	detail, err := shell.approvals.Detail(ctx, approvalID)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "unable to show approval: %v\n", err)
		return writeErr
	}
	project, err := shell.env.Store.GetProject(ctx, detail.Task.ProjectID)
	if err != nil {
		return err
	}
	if !matchesTaskProjectionScope(project.Key, detail.Task.Scope, shell.state.Scope) {
		_, writeErr := fmt.Fprintln(output, "approval not found in current scope")
		return writeErr
	}

	if _, err := fmt.Fprintf(
		output,
		"approval=%d status=%s task=%s run=%s resolver=%s requested_at=%s\n",
		detail.Approval.ID,
		detail.Approval.Status,
		detail.Task.Key,
		formatNullableInt64(detail.Approval.RunID),
		detail.ResolverSupport,
		detail.Approval.RequestedAt.Format(time.RFC3339),
	); err != nil {
		return err
	}
	if detail.Approval.ResolvedAt != nil {
		if _, err := fmt.Fprintf(
			output,
			"resolved_at=%s decision_by=%s reason=%s\n",
			detail.Approval.ResolvedAt.Format(time.RFC3339),
			approvalValueOrNone(detail.Approval.DecisionBy),
			approvalValueOrNone(detail.Approval.Reason),
		); err != nil {
			return err
		}
	}
	if detail.Approval.RunID != nil {
		_, err = fmt.Fprintf(output, "evidence=/runs show %d\n", *detail.Approval.RunID)
		return err
	}
	_, err = fmt.Fprintln(output, "evidence=none")
	return err
}

func approvalValueOrNone(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "none"
	}
	return value
}

func formatNullableInt64(value *int64) string {
	if value == nil {
		return "none"
	}
	return strconv.FormatInt(*value, 10)
}

func (shell *Shell) handleTransfer(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 || !strings.EqualFold(args[0], "prepare") {
		_, err := fmt.Fprintln(output, "usage: /transfer prepare direction=<deposit|withdraw> amount_usd=<amount> source_account=<name> destination_account=<name> [memo=<text>]")
		return err
	}
	if shell.state.Scope.Kind != scope.ScopeProject {
		_, err := fmt.Fprintln(output, "select an initiative first with /project <initiative-key>")
		return err
	}

	assignments, err := parseAssignments(args[1:])
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "unable to parse transfer arguments: %v\n", err)
		return writeErr
	}

	result, err := shell.transfers.Prepare(ctx, transfersvc.PrepareParams{
		ProjectKey:         shell.state.Scope.ProjectKey,
		Direction:          assignments["direction"],
		AmountUSD:          assignments["amount_usd"],
		SourceAccount:      assignments["source_account"],
		DestinationAccount: assignments["destination_account"],
		Memo:               assignments["memo"],
	})
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "unable to prepare transfer: %v\n", err)
		return writeErr
	}
	shell.state.ActiveTask = result.Task.Key
	shell.state.ActiveRun = strconv.FormatInt(result.Run.ID, 10)
	if err := shell.persistState(); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(output, "task=%s run=%d approval=%d\n", result.Task.Key, result.Run.ID, result.Approval.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "summary=%s\n", result.Summary); err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		output,
		"next=/runs show %d; /approvals resolve %d <approve|deny> because <reason...>; then /runs show <submit-run-id from resolve output>\n",
		result.Run.ID,
		result.Approval.ID,
	)
	return err
}

func parseAssignments(args []string) (map[string]string, error) {
	assignments := make(map[string]string, len(args))
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("arguments must be key=value")
		}
		assignments[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return assignments, nil
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

	switch len(args) {
	case 0:
	case 1:
	default:
		_, err := fmt.Fprintln(output, "usage: /doctor [json|report]")
		return err
	}

	switch {
	case len(args) == 1 && strings.EqualFold(args[0], "json"):
		encoder := json.NewEncoder(output)
		return encoder.Encode(report)
	case len(args) == 1 && strings.EqualFold(args[0], "report"):
		_, err = fmt.Fprint(output, healthsvc.RenderMarkdownReport(healthsvc.BuildOperatorReport(report)))
		return err
	case len(args) == 1:
		_, err := fmt.Fprintf(output, "unsupported /doctor mode %q; expected json or report\n", args[0])
		return err
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

func (shell *Shell) registrySnapshot() registry.Snapshot {
	if shell.env.CapabilityService == nil {
		return registry.Snapshot{}
	}

	active := shell.env.CapabilityService.Active()
	snapshot := registry.Snapshot{
		Items:       make([]registry.Item, 0, len(active.Capabilities)),
		ByKey:       make(map[string]registry.Item, len(active.Capabilities)),
		ByKind:      make(map[registry.Kind][]registry.Item),
		Diagnostics: append([]registry.Diagnostic(nil), active.Diagnostics...),
	}
	for _, descriptor := range active.Capabilities {
		item := descriptor
		snapshot.Items = append(snapshot.Items, item)
		snapshot.ByKey[item.Key] = item
		snapshot.ByKind[item.Kind] = append(snapshot.ByKind[item.Kind], item)
	}
	return snapshot
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

func (shell *Shell) pendingApprovals(ctx context.Context) ([]projections.PendingApprovalView, error) {
	views, err := projections.ListPendingApprovalViews(ctx, shell.env.Store.DB())
	if err != nil {
		return nil, err
	}

	approvals := make([]projections.PendingApprovalView, 0, len(views))
	for _, view := range views {
		if matchesTaskProjectionScope(view.ProjectKey, view.TaskScope, shell.state.Scope) {
			approvals = append(approvals, view)
		}
	}
	return approvals, nil
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
	transitions := shell.transitions
	if transitions.Store == nil {
		transitions = projects.Service{Store: shell.env.Store}
	}

	return transitions.RegisterManagedProject(ctx, manifest)
}

func (shell *Shell) controlScope() corescope.ControlScope {
	return shell.state.Scope.ControlScope()
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

func parseCommandInput(args []string) (map[string]string, error) {
	input := make(map[string]string, len(args))
	for _, token := range args {
		key, value, ok := strings.Cut(token, "=")
		if !ok {
			return nil, fmt.Errorf("expected key=value input, got %s", token)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			return nil, fmt.Errorf("input key cannot be empty")
		}
		input[key] = value
	}
	return input, nil
}

func (shell *Shell) newBroker() *broker.Broker {
	return broker.New(registry.Snapshot{}, catalog.BuiltinDefinitions(), budgets.Limits{
		Tool: budgets.Tool{
			MaxSelections:  20,
			MaxInvocations: 20,
			MaxCostUnits:   40,
		},
		Context: budgets.Context{
			MaxExpandedDefinitions: 20,
			MaxCompactedResults:    20,
			MaxCompactedBytes:      32_000,
		},
	})
}

func (shell *Shell) catalogScope() string {
	switch shell.state.Scope.Kind {
	case scope.ScopeProject:
		return "project"
	case scope.ScopeOdinCore:
		return "odin-core"
	case scope.ScopeNewProject:
		return "new-project"
	default:
		return "global"
	}
}

func renderToolDetail(output io.Writer, definition catalog.ToolDefinition) error {
	if _, err := fmt.Fprintf(output, "tool=%s title=%s\nsummary=%s\nsource=%s\n", definition.Key, definition.Title, definition.Summary, definition.SourceRef); err != nil {
		return err
	}
	if len(definition.Scopes) > 0 {
		if _, err := fmt.Fprintf(output, "scopes=%s\n", strings.Join(definition.Scopes, ",")); err != nil {
			return err
		}
	}
	if len(definition.Tags) > 0 {
		if _, err := fmt.Fprintf(output, "tags=%s\n", strings.Join(definition.Tags, ",")); err != nil {
			return err
		}
	}
	if inputs := schemaPropertyKeys(definition.Schema); len(inputs) > 0 {
		if _, err := fmt.Fprintf(output, "inputs=%s\n", strings.Join(inputs, ",")); err != nil {
			return err
		}
	}
	return nil
}

func renderToolResult(output io.Writer, result catalog.StructuredResult) error {
	if _, err := fmt.Fprintf(output, "tool=%s\nsummary=%s\n", result.CapabilityKey, result.Summary); err != nil {
		return err
	}
	if len(result.KeyFacts) > 0 {
		keys := make([]string, 0, len(result.KeyFacts))
		for key := range result.KeyFacts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if _, err := fmt.Fprintf(output, "fact %s=%s\n", key, result.KeyFacts[key]); err != nil {
				return err
			}
		}
	}
	for _, artifact := range result.Artifacts {
		if _, err := fmt.Fprintf(output, "artifact %s\n", artifact); err != nil {
			return err
		}
	}
	if result.RawRef != "" {
		if _, err := fmt.Fprintf(output, "raw_ref=%s\n", result.RawRef); err != nil {
			return err
		}
	}
	return nil
}

func schemaPropertyKeys(schema map[string]any) []string {
	if len(schema) == 0 {
		return nil
	}
	rawProperties, ok := schema["properties"]
	if !ok {
		return nil
	}
	properties, ok := rawProperties.(map[string]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
