package repl

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	webdriver "odin-os/internal/adapters/web"
	"odin-os/internal/cli/commands"
	"odin-os/internal/cli/render"
	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	knowledgememory "odin-os/internal/memory/knowledge"
	"odin-os/internal/prompting"
	"odin-os/internal/registry"
	"odin-os/internal/runtime/checkpoints"
	convsvc "odin-os/internal/runtime/conversation"
	delegsvc "odin-os/internal/runtime/delegations"
	healthsvc "odin-os/internal/runtime/health"
	jobsvc "odin-os/internal/runtime/jobs"
	runsvc "odin-os/internal/runtime/runs"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tools/broker"
	"odin-os/internal/tools/budgets"
	"odin-os/internal/tools/catalog"
	"odin-os/internal/tools/invocation"
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
	env          Environment
	state        State
	health       healthsvc.Service
	jobs         jobsvc.Service
	runs         runsvc.Service
	transitions  projects.Service
	conversation convsvc.Service
}

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
	}

	if !shell.skillExists(shell.state.SelectedSkillKey) {
		shell.state.SelectedSkillKey = ""
	}
	if !shell.workflowExists(shell.state.SelectedWorkflowKey) {
		shell.state.SelectedWorkflowKey = ""
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

	executionRequest, err := shell.executionRequestForPromptWithContext(ctx, line)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "unable to prepare task execution: %v\n", err)
		return writeErr
	}

	outcome, runErr := shell.jobs.ExecuteTaskWithRequest(ctx, task.ID, executionRequest)
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
		SelectedSkill:    shell.state.SelectedSkillKey,
		SelectedWorkflow: shell.state.SelectedWorkflowKey,
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
		_, err := fmt.Fprintln(output, commands.InteractiveHelp())
		return err
	case "mode":
		return shell.handleMode(command.Args, output)
	case "scope":
		return shell.handleScope(command.Args, output)
	case "project":
		return shell.handleProject(command.Args, output)
	case "agent":
		return shell.handleAgent(ctx, command.Args, output)
	case "workflow":
		return shell.handleWorkflow(command.Args, output)
	case "memory":
		return shell.handleMemory(ctx, command.Args, output)
	case "skill":
		return shell.handleSkill(command.Args, output)
	case "tool":
		return shell.handleTool(ctx, command.Args, output)
	case "transition":
		return shell.handleTransition(ctx, command.Args, output)
	case "observe":
		return shell.handleObserve(ctx, command.Args, output)
	case "compare":
		return shell.handleCompare(ctx, command.Args, output)
	case "jobs":
		return shell.handleJobs(ctx, command.Args, output)
	case "runs":
		return shell.handleRuns(ctx, command.Args, output)
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
	executionRequest, err := shell.executionRequestForPromptWithContext(ctx, line)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "ask failed: unable to prepare skill context: %v\n", err)
		return writeErr
	}

	prompt := line
	if strings.TrimSpace(executionRequest.PromptOverride) != "" {
		prompt = executionRequest.PromptOverride
	}

	result, err := shell.conversation.Respond(ctx, convsvc.Request{
		Scope:          shell.state.Scope,
		Mode:           string(shell.state.Mode),
		Prompt:         line,
		ExecutorPrompt: prompt,
	})
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "ask failed: %v\n", err)
		return writeErr
	}
	if err := shell.maybeRecordSocialDraftFromAsk(ctx, line, result); err != nil {
		_, writeErr := fmt.Fprintf(output, "ask failed: unable to record social draft: %v\n", err)
		return writeErr
	}
	_, err = fmt.Fprintln(output, result.Answer)
	return err
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
		sort.Strings(projectKeys)

		_, err := fmt.Fprintf(output, "current=%s projects=%s\n", current, strings.Join(projectKeys, ","))
		return err
	}

	if strings.EqualFold(args[0], "add") {
		return shell.handleProjectAdd(args[1:], output)
	}

	project, ok := shell.env.Registry.Lookup(args[0])
	if !ok {
		_, err := fmt.Fprintf(output, "unknown project: %s\n", args[0])
		return err
	}

	return shell.selectProject(project, output)
}

func (shell *Shell) handleProjectAdd(args []string, output io.Writer) error {
	request, err := parseProjectAddArgs(args)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "%v\nusage: %s\n", err, commands.ProjectAddUsage)
		return writeErr
	}

	if request.DefaultBranch == "" {
		request.DefaultBranch, err = projects.InferDefaultBranch(context.Background(), request.GitRoot)
		if err != nil {
			_, writeErr := fmt.Fprintf(output, "unable to infer default branch: %v\nusage: %s\n", err, commands.ProjectAddUsage)
			return writeErr
		}
	}

	registry, diagnostics, err := projects.AppendProject(shell.env.Registry.ConfigPath(), request.manifest())
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "unable to add project: %v\n", err)
		return writeErr
	}
	if len(diagnostics) != 0 {
		_, writeErr := fmt.Fprintf(output, "unable to add project: %s\n", diagnostics[0].Message)
		return writeErr
	}

	shell.syncRegistry(registry)
	project, ok := shell.env.Registry.Lookup(request.Key)
	if !ok {
		_, err := fmt.Fprintf(output, "added project but could not reload %s\n", request.Key)
		return err
	}

	if _, err := fmt.Fprintf(output, "added project %s (%s)\n", project.Key, project.ProjectClass); err != nil {
		return err
	}
	return shell.selectProject(project, output)
}

func (shell *Shell) handleAgent(ctx context.Context, args []string, output io.Writer) error {
	if len(args) > 0 && strings.EqualFold(args[0], "run") {
		return shell.handleAgentRun(ctx, args[1:], output)
	}
	return shell.handleRegistryInspect(args, registry.KindAgent, "agent", "agents", commands.AgentUsage, output)
}

func (shell *Shell) handleAgentRun(ctx context.Context, args []string, output io.Writer) error {
	if len(args) < 1 {
		_, err := fmt.Fprintf(output, "usage: %s\n", commands.AgentUsage)
		return err
	}
	if shell.state.Scope.Kind == scope.ScopeGlobal {
		_, err := fmt.Fprintln(output, "agent run requires a non-global scope")
		return err
	}

	item, ok := shell.lookupRegistryItem(args[0], registry.KindAgent)
	if !ok {
		_, err := fmt.Fprintf(output, "unknown agent: %s\n", args[0])
		return err
	}
	if err := validateRegistryItem(item, registry.KindAgent); err != nil {
		_, writeErr := fmt.Fprintf(output, "agent %s is not ready: %v\n", item.Key, err)
		return writeErr
	}

	input, err := parseCommandInput(args[1:])
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "%v\nusage: %s\n", err, commands.AgentUsage)
		return writeErr
	}

	service := delegsvc.Service{
		Store:            shell.env.Store,
		Jobs:             shell.jobs,
		Checkpoints:      checkpoints.Service{Store: shell.env.Store},
		RegistrySnapshot: shell.env.RegistrySnapshot,
	}
	parentTask, parentRun, _, runErr := service.RunAgent(ctx, delegsvc.RunInput{
		ResolvedScope: shell.state.Scope,
		AgentKey:      item.Key,
		RequestedBy:   "operator",
		Inputs:        input,
	})
	if parentTask.ID != 0 {
		shell.state.ActiveTask = parentTask.Key
	}
	if parentRun != nil {
		shell.state.ActiveRun = strconv.FormatInt(parentRun.ID, 10)
	}
	if err := shell.persistState(); err != nil {
		return err
	}

	if parentTask.ID != 0 {
		if _, err := fmt.Fprintf(output, "created task %s (%s)\n", parentTask.Key, parentTask.Status); err != nil {
			return err
		}
	}
	if parentRun != nil {
		detail, err := shell.runs.Detail(ctx, shell.state.Scope, parentRun.ID)
		if err != nil {
			return err
		}
		if err := renderRunDetail(output, detail); err != nil {
			return err
		}
	}
	if runErr != nil {
		_, err := fmt.Fprintf(output, "\nagent run error: %v\n", runErr)
		return err
	}
	return nil
}

func (shell *Shell) handleWorkflow(args []string, output io.Writer) error {
	if len(args) == 0 {
		current := shell.state.SelectedWorkflowKey
		if current == "" {
			current = "none"
		}
		_, err := fmt.Fprintf(output, "current=%s\nusage: %s\n", current, commands.WorkflowUsage)
		return err
	}

	switch strings.ToLower(args[0]) {
	case "use":
		if len(args) != 2 {
			_, err := fmt.Fprintf(output, "usage: %s\n", commands.WorkflowUsage)
			return err
		}
		item, ok := shell.lookupWorkflow(args[1])
		if !ok {
			_, err := fmt.Fprintf(output, "unknown workflow: %s\n", args[1])
			return err
		}
		if err := validateRegistryItem(item, registry.KindWorkflow); err != nil {
			_, writeErr := fmt.Fprintf(output, "workflow %s is not ready: %v\n", item.Key, err)
			return writeErr
		}
		shell.state.SelectedWorkflowKey = item.Key
		if err := shell.persistState(); err != nil {
			return err
		}
		_, err := fmt.Fprintf(output, "workflow=%s status=selected\n", item.Key)
		return err
	case "clear":
		shell.state.SelectedWorkflowKey = ""
		if err := shell.persistState(); err != nil {
			return err
		}
		_, err := fmt.Fprintln(output, "workflow=none")
		return err
	default:
		return shell.handleRegistryInspect(args, registry.KindWorkflow, "workflow", "workflows", commands.WorkflowUsage, output)
	}
}

func (shell *Shell) handleMemory(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 {
		_, err := fmt.Fprintf(output, "usage: %s\n", commands.MemoryUsage)
		return err
	}

	switch strings.ToLower(args[0]) {
	case "list":
		return shell.handleMemoryList(ctx, args[1:], output)
	case "show":
		return shell.handleMemoryShow(ctx, args[1:], output)
	case "remember":
		return shell.handleMemoryRemember(ctx, args[1:], output)
	case "resolve":
		return shell.handleMemoryResolve(ctx, args[1:], output)
	case "publish":
		return shell.handleMemoryPublish(ctx, args[1:], output)
	default:
		_, err := fmt.Fprintf(output, "usage: %s\n", commands.MemoryUsage)
		return err
	}
}

func (shell *Shell) handleMemoryList(ctx context.Context, args []string, output io.Writer) error {
	request, err := parseMemoryListArgs(args)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "%v\nusage: %s\n", err, commands.MemoryUsage)
		return writeErr
	}

	scope, err := shell.memoryScope(ctx)
	if err != nil {
		return err
	}
	summaries, err := knowledgememory.Service{Store: shell.env.Store}.List(ctx, scope, request.MemoryType)
	if err != nil {
		return err
	}
	summaries = filterMemorySummaries(summaries, request)
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

func (shell *Shell) handleMemoryShow(ctx context.Context, args []string, output io.Writer) error {
	if len(args) != 1 {
		_, err := fmt.Fprintf(output, "usage: %s\n", commands.MemoryUsage)
		return err
	}

	memoryID, err := strconv.ParseInt(strings.TrimSpace(args[0]), 10, 64)
	if err != nil || memoryID <= 0 {
		_, writeErr := fmt.Fprintf(output, "memory id must be a positive integer\nusage: %s\n", commands.MemoryUsage)
		return writeErr
	}

	summary, ok, err := shell.visibleMemorySummaryByID(ctx, memoryID)
	if err != nil {
		return err
	}
	if ok {
		return renderMemorySummary(output, summary)
	}

	_, err = fmt.Fprintf(output, "unknown memory: %d\n", memoryID)
	return err
}

func (shell *Shell) handleMemoryRemember(ctx context.Context, args []string, output io.Writer) error {
	request, err := parseMemoryRememberArgs(args)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "%v\nusage: %s\n", err, commands.MemoryUsage)
		return writeErr
	}
	if err := validateMemoryRememberRequest(request); err != nil {
		_, writeErr := fmt.Fprintf(output, "%v\nusage: %s\n", err, commands.MemoryUsage)
		return writeErr
	}

	scope, err := shell.memoryScope(ctx)
	if err != nil {
		return err
	}

	details, err := shell.memoryDetailsJSON(scope, request.Fields)
	if err != nil {
		return err
	}

	summary, err := knowledgememory.Service{Store: shell.env.Store}.Record(ctx, scope, request.MemoryType, request.Summary, details, nil)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(output, "memory=%d type=%s scope=%s/%s status=recorded\nsummary=%s\ndetails_json=%s\n", summary.ID, summary.MemoryType, summary.Scope, summary.ScopeKey, strings.TrimSpace(summary.Summary), strings.TrimSpace(summary.DetailsJSON))
	return err
}

func (shell *Shell) handleMemoryResolve(ctx context.Context, args []string, output io.Writer) error {
	request, err := parseMemoryResolveArgs(args)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "%v\nusage: %s\n", err, commands.MemoryUsage)
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
	if summary.MemoryType != "social_draft" {
		_, err := fmt.Fprintf(output, "only social_draft memories can be resolved\nusage: %s\n", commands.MemoryUsage)
		return err
	}

	details, err := parseMemoryDetails(summary.DetailsJSON)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "memory details are invalid: %v\n", err)
		return writeErr
	}
	details = normalizeMemoryDetailsPayload(summary, details)
	if strings.TrimSpace(details.Fields["approval"]) != "pending" {
		_, err := fmt.Fprintf(output, "social_draft approval must be pending to resolve\nusage: %s\n", commands.MemoryUsage)
		return err
	}

	details.Fields["approval"] = request.Result
	if request.Reason != "" {
		details.Fields["reason"] = request.Reason
	} else {
		delete(details.Fields, "reason")
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

	var outcomeSummary *sqlite.MemorySummary
	if outcomeFields, ok := socialOutcomeFieldsForResolvedDraft(details.Fields, request.Result, request.Reason); ok {
		scope := knowledgememory.Scope{
			ProjectID: updatedSummary.ProjectID,
			Value:     updatedSummary.Scope,
			Key:       updatedSummary.ScopeKey,
		}
		outcomeDetails := details
		outcomeDetails.Fields = outcomeFields
		outcomeDetailsJSON, err := marshalMemoryDetailsPayload(outcomeDetails)
		if err != nil {
			return err
		}
		recorded, err := knowledgememory.Service{Store: shell.env.Store}.Record(
			ctx,
			scope,
			"social_outcome",
			strings.TrimSpace(updatedSummary.Summary),
			outcomeDetailsJSON,
			nil,
		)
		if err != nil {
			return err
		}
		outcomeSummary = &recorded
	}

	if _, err := fmt.Fprintf(output, "memory=%d type=%s scope=%s/%s status=resolved\nsummary=%s\n", updatedSummary.ID, updatedSummary.MemoryType, updatedSummary.Scope, updatedSummary.ScopeKey, strings.TrimSpace(updatedSummary.Summary)); err != nil {
		return err
	}
	if details, err := parseMemoryDetails(updatedSummary.DetailsJSON); err == nil {
		if renderedFields := formatMemoryFields(details.Fields); renderedFields != "" {
			if _, err := fmt.Fprintf(output, "fields=%s\n", renderedFields); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(output, "details_json=%s\n", strings.TrimSpace(updatedSummary.DetailsJSON)); err != nil {
		return err
	}

	if outcomeSummary == nil {
		_, err := fmt.Fprintln(output, "outcome_memory=skipped reason=draft lacks valid social outcome fields")
		return err
	}

	if _, err := fmt.Fprintf(output, "outcome_memory=%d type=%s scope=%s/%s status=recorded\nsummary=%s\n", outcomeSummary.ID, outcomeSummary.MemoryType, outcomeSummary.Scope, outcomeSummary.ScopeKey, strings.TrimSpace(outcomeSummary.Summary)); err != nil {
		return err
	}
	if details, err := parseMemoryDetails(outcomeSummary.DetailsJSON); err == nil {
		if renderedFields := formatMemoryFields(details.Fields); renderedFields != "" {
			if _, err := fmt.Fprintf(output, "fields=%s\n", renderedFields); err != nil {
				return err
			}
		}
	}
	_, err = fmt.Fprintf(output, "details_json=%s\n", strings.TrimSpace(outcomeSummary.DetailsJSON))
	return err
}

func (shell *Shell) handleMemoryPublish(ctx context.Context, args []string, output io.Writer) error {
	request, err := parseMemoryPublishArgs(args)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "%v\nusage: %s\n", err, commands.MemoryUsage)
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
		_, err := fmt.Fprintf(output, "only social_outcome memories can be published\nusage: %s\n", commands.MemoryUsage)
		return err
	}

	details, err := parseMemoryDetails(summary.DetailsJSON)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "memory details are invalid: %v\n", err)
		return writeErr
	}
	details = normalizeMemoryDetailsPayload(summary, details)
	if strings.TrimSpace(details.Fields["result"]) != "approved" {
		_, err := fmt.Fprintf(output, "only approved social_outcome memories can be published\nusage: %s\n", commands.MemoryUsage)
		return err
	}
	if strings.TrimSpace(details.Fields["publish_status"]) == "published" {
		_, err := fmt.Fprintf(output, "social_outcome is already marked published\nusage: %s\n", commands.MemoryUsage)
		return err
	}

	if request.Via == "huginn_x" {
		contentKind := strings.TrimSpace(details.Fields["content_kind"])
		if strings.TrimSpace(details.Fields["channel"]) != "x" || (contentKind != "post" && contentKind != "reply") {
			_, err := fmt.Fprintf(output, "native X publish requires channel=x and content_kind=post or reply\nusage: %s\n", commands.MemoryUsage)
			return err
		}
		if contentKind == "reply" {
			replyTarget := strings.TrimSpace(details.Fields["in_reply_to_url"])
			if replyTarget == "" {
				_, err := fmt.Fprintf(output, "native X reply publish requires in_reply_to_url\nusage: %s\n", commands.MemoryUsage)
				return err
			}
			if !isAllowedXStatusURL(replyTarget) {
				_, err := fmt.Fprintf(output, "native X reply publish requires in_reply_to_url to be a valid X status URL\nusage: %s\n", commands.MemoryUsage)
				return err
			}
		}

		artifacts, err := shell.publishApprovedXOutcomeWithHuginn(ctx, summary)
		if err != nil {
			_, writeErr := fmt.Fprintf(output, "native X publish failed: %v\nusage: %s\n", err, commands.MemoryUsage)
			return writeErr
		}

		publishURL := strings.TrimSpace(stringMapValue(artifacts, "publish_url"))
		if publishURL == "" {
			_, err := fmt.Fprintf(output, "native X publish failed: publish_url missing\nusage: %s\n", commands.MemoryUsage)
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

func (shell *Shell) handleRegistryInspect(args []string, kind registry.Kind, singular string, plural string, usage string, output io.Writer) error {
	if len(args) == 0 {
		_, err := fmt.Fprintf(output, "usage: %s\n", usage)
		return err
	}

	switch strings.ToLower(args[0]) {
	case "list":
		items := shell.listRegistryItems(kind)
		if len(items) == 0 {
			_, err := fmt.Fprintf(output, "no %s\n", plural)
			return err
		}
		for _, item := range items {
			if _, err := fmt.Fprintf(output, "%s %s\n", item.Key, item.Summary); err != nil {
				return err
			}
		}
		return nil
	case "show":
		if len(args) != 2 {
			_, err := fmt.Fprintf(output, "usage: %s\n", usage)
			return err
		}
		item, ok := shell.lookupRegistryItem(args[1], kind)
		if !ok {
			_, err := fmt.Fprintf(output, "unknown %s: %s\n", singular, args[1])
			return err
		}
		switch kind {
		case registry.KindAgent:
			return renderAgentDetail(output, item)
		case registry.KindWorkflow:
			return renderWorkflowDetail(output, item)
		default:
			_, err := fmt.Fprintf(output, "unsupported registry kind: %s\n", kind)
			return err
		}
	case "validate":
		if len(args) != 2 {
			_, err := fmt.Fprintf(output, "usage: %s\n", usage)
			return err
		}
		item, ok := shell.lookupRegistryItem(args[1], kind)
		if !ok {
			_, err := fmt.Fprintf(output, "unknown %s: %s\n", singular, args[1])
			return err
		}
		if err := validateRegistryItem(item, kind); err != nil {
			_, writeErr := fmt.Fprintf(output, "%s=%s status=invalid error=%v\n", singular, item.Key, err)
			return writeErr
		}
		switch kind {
		case registry.KindAgent:
			_, err := fmt.Fprintf(output, "agent=%s status=ready source=%s role=%s scopes=%s tools=%s\n", item.Key, item.Source.RelativePath, item.Role, strings.Join(item.Scopes, ","), strings.Join(item.Tools, ","))
			return err
		case registry.KindWorkflow:
			_, err := fmt.Fprintf(output, "workflow=%s status=ready source=%s entrypoint=%s composes=%s\n", item.Key, item.Source.RelativePath, item.Entrypoint, strings.Join(item.Composes, ","))
			return err
		default:
			_, err := fmt.Fprintf(output, "%s=%s status=ready source=%s\n", singular, item.Key, item.Source.RelativePath)
			return err
		}
	default:
		_, err := fmt.Fprintf(output, "usage: %s\n", usage)
		return err
	}
}

func (shell *Shell) handleSkill(args []string, output io.Writer) error {
	if len(args) == 0 {
		current := shell.state.SelectedSkillKey
		if current == "" {
			current = "none"
		}
		_, err := fmt.Fprintf(output, "current=%s\nusage: %s\n", current, commands.SkillUsage)
		return err
	}

	switch strings.ToLower(args[0]) {
	case "list":
		cards := shell.newBroker().Catalog(shell.catalogScope())
		count := 0
		for _, card := range cards {
			if card.Kind != catalog.KindSkill {
				continue
			}
			if _, err := fmt.Fprintf(output, "%s %s\n", card.Key, card.Summary); err != nil {
				return err
			}
			count++
		}
		if count == 0 {
			_, err := fmt.Fprintln(output, "no skills")
			return err
		}
		return nil
	case "show":
		if len(args) != 2 {
			_, err := fmt.Fprintf(output, "usage: %s\n", commands.SkillUsage)
			return err
		}
		item, ok := shell.lookupSkill(args[1])
		if !ok {
			_, err := fmt.Fprintf(output, "unknown skill: %s\n", args[1])
			return err
		}
		return renderSkillDetail(output, item)
	case "use":
		if len(args) != 2 {
			_, err := fmt.Fprintf(output, "usage: %s\n", commands.SkillUsage)
			return err
		}
		item, ok := shell.lookupSkill(args[1])
		if !ok {
			_, err := fmt.Fprintf(output, "unknown skill: %s\n", args[1])
			return err
		}
		if !catalog.MatchesScope(item.Scopes, shell.catalogScope()) {
			_, err := fmt.Fprintf(output, "skill %s is not available in %s scope\n", item.Key, shell.catalogScope())
			return err
		}
		if err := validateSkillItem(item); err != nil {
			_, writeErr := fmt.Fprintf(output, "skill %s is not ready: %v\n", item.Key, err)
			return writeErr
		}
		shell.state.SelectedSkillKey = item.Key
		if err := shell.persistState(); err != nil {
			return err
		}
		_, err := fmt.Fprintf(output, "skill=%s status=selected\n", item.Key)
		return err
	case "validate":
		if len(args) != 2 {
			_, err := fmt.Fprintf(output, "usage: %s\n", commands.SkillUsage)
			return err
		}
		item, ok := shell.lookupSkill(args[1])
		if !ok {
			_, err := fmt.Fprintf(output, "unknown skill: %s\n", args[1])
			return err
		}
		if err := validateSkillItem(item); err != nil {
			_, writeErr := fmt.Fprintf(output, "skill=%s status=invalid error=%v\n", item.Key, err)
			return writeErr
		}
		_, err := fmt.Fprintf(output, "skill=%s status=ready source=%s strictness=%s applies_to=%s\n", item.Key, item.Source.RelativePath, item.Strictness, strings.Join(item.AppliesTo, ","))
		return err
	case "clear":
		shell.state.SelectedSkillKey = ""
		if err := shell.persistState(); err != nil {
			return err
		}
		_, err := fmt.Fprintln(output, "skill=none")
		return err
	default:
		_, err := fmt.Fprintf(output, "usage: %s\n", commands.SkillUsage)
		return err
	}
}

func (shell *Shell) handleTool(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 {
		_, err := fmt.Fprintf(output, "usage: %s\n", commands.ToolUsage)
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
			_, err := fmt.Fprintf(output, "usage: %s\n", commands.ToolUsage)
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
			_, err := fmt.Fprintf(output, "usage: %s\n", commands.ToolUsage)
			return err
		}
		input, err := parseCommandInput(args[2:])
		if err != nil {
			_, writeErr := fmt.Fprintf(output, "%v\nusage: %s\n", err, commands.ToolUsage)
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
		_, err := fmt.Fprintf(output, "usage: %s\n", commands.ToolUsage)
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

func (shell *Shell) handleTransition(ctx context.Context, args []string, output io.Writer) error {
	if len(args) > 0 && strings.EqualFold(args[0], "help") {
		_, err := fmt.Fprintln(output, commands.TransitionUsage)
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
		_, err := fmt.Fprintf(output, "usage: %s\n", commands.TransitionUsage)
		return err
	}
	if len(args) < 2 {
		_, err := fmt.Fprintf(output, "usage: %s\n", commands.TransitionUsage)
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

func (shell *Shell) handleJobs(ctx context.Context, args []string, output io.Writer) error {
	if len(args) > 0 {
		if strings.EqualFold(args[0], "cancel") {
			return shell.handleJobsCancel(ctx, args[1:], output)
		}
		_, err := fmt.Fprintf(output, "usage: %s\n", commands.JobsUsage)
		return err
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

func (shell *Shell) handleJobsCancel(ctx context.Context, args []string, output io.Writer) error {
	if len(args) != 1 {
		_, err := fmt.Fprintf(output, "usage: %s\n", commands.JobsUsage)
		return err
	}

	view, err := shell.jobs.CancelTaskByKey(ctx, shell.state.Scope, args[0])
	switch err {
	case nil:
	case sql.ErrNoRows:
		_, writeErr := fmt.Fprintf(output, "unknown task: %s\n", args[0])
		return writeErr
	case jobsvc.ErrTaskRunning:
		runArg := "active"
		if view.CurrentRunID != nil {
			runArg = strconv.FormatInt(*view.CurrentRunID, 10)
		}
		_, writeErr := fmt.Fprintf(output, "task %s is running via run=%s; use /runs cancel %s\n", view.TaskKey, runArg, runArg)
		return writeErr
	default:
		return err
	}

	_, err = fmt.Fprintf(output, "%s %s %s\n", view.ProjectKey, view.TaskKey, view.Status)
	return err
}

func (shell *Shell) handleRuns(ctx context.Context, args []string, output io.Writer) error {
	if len(args) > 0 {
		if strings.EqualFold(args[0], "show") {
			return shell.handleRunsShow(ctx, args[1:], output)
		}
		if strings.EqualFold(args[0], "cancel") {
			return shell.handleRunsCancel(ctx, args[1:], output)
		}
		_, err := fmt.Fprintf(output, "usage: %s\n", commands.RunsUsage)
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
		if _, err := fmt.Fprintf(output, "run=%d task=%s executor=%s status=%s\n", view.RunID, view.TaskKey, view.Executor, view.Status); err != nil {
			return err
		}
	}
	return nil
}

func (shell *Shell) handleRunsShow(ctx context.Context, args []string, output io.Writer) error {
	runID, err := shell.resolveRunID(ctx, args)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "%v\nusage: %s\n", err, commands.RunsUsage)
		return writeErr
	}

	detail, err := shell.runs.Detail(ctx, shell.state.Scope, runID)
	switch err {
	case nil:
	case sql.ErrNoRows:
		_, writeErr := fmt.Fprintf(output, "unknown run: %d\n", runID)
		return writeErr
	default:
		return err
	}

	return renderRunDetail(output, detail)
}

func (shell *Shell) handleRunsCancel(ctx context.Context, args []string, output io.Writer) error {
	runID, err := shell.resolveRunID(ctx, args)
	if err != nil {
		_, writeErr := fmt.Fprintf(output, "%v\nusage: %s\n", err, commands.RunsUsage)
		return writeErr
	}

	detail, err := shell.runs.Cancel(ctx, shell.state.Scope, runID)
	switch err {
	case nil:
	case sql.ErrNoRows:
		_, writeErr := fmt.Fprintf(output, "unknown run: %d\n", runID)
		return writeErr
	default:
		return err
	}

	return renderRunDetail(output, detail)
}

func (shell *Shell) resolveRunID(ctx context.Context, args []string) (int64, error) {
	if len(args) == 0 || strings.EqualFold(args[0], "active") {
		if shell.state.ActiveRun == "" {
			return shell.latestRunningRunID(ctx)
		}
		runID, err := strconv.ParseInt(shell.state.ActiveRun, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid active run id: %w", err)
		}
		return runID, nil
	}
	if len(args) > 1 {
		return 0, fmt.Errorf("too many arguments")
	}
	runID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid run id: %s", args[0])
	}
	return runID, nil
}

func (shell *Shell) latestRunningRunID(ctx context.Context) (int64, error) {
	views, err := shell.runs.List(ctx, shell.state.Scope)
	if err != nil {
		return 0, err
	}
	for index := len(views) - 1; index >= 0; index-- {
		if views[index].Status == "running" {
			return views[index].RunID, nil
		}
	}
	return 0, fmt.Errorf("no active run")
}

func renderRunDetail(output io.Writer, detail runsvc.Detail) error {
	finishedAt := "running"
	if detail.Run.FinishedAt != nil {
		finishedAt = detail.Run.FinishedAt.Format(time.RFC3339)
	}
	if _, err := fmt.Fprintf(
		output,
		"run=%d task=%s project=%s executor=%s status=%s attempt=%d\nstarted_at=%s finished_at=%s\nsummary=%s\n",
		detail.Run.ID,
		detail.Task.Key,
		detail.Project.Key,
		detail.Run.Executor,
		detail.Run.Status,
		detail.Run.Attempt,
		detail.Run.StartedAt.Format(time.RFC3339),
		finishedAt,
		strings.TrimSpace(detail.Run.Summary),
	); err != nil {
		return err
	}

	for _, transcript := range detail.Transcripts {
		if _, err := fmt.Fprintf(
			output,
			"\ntranscript=%d mode=%s executor=%s created_at=%s\nprompt:\n%s\nresponse:\n%s\n",
			transcript.ID,
			transcript.Mode,
			transcript.Executor,
			transcript.CreatedAt.Format(time.RFC3339),
			strings.TrimSpace(transcript.Prompt),
			strings.TrimSpace(transcript.Response),
		); err != nil {
			return err
		}
		if strings.TrimSpace(transcript.ToolSummary) != "" {
			if _, err := fmt.Fprintf(output, "tool_summary=%s\n", strings.TrimSpace(transcript.ToolSummary)); err != nil {
				return err
			}
			if err := renderTelemetryFields(output, strings.TrimSpace(transcript.ToolSummary)); err != nil {
				return err
			}
		}
	}

	for _, summary := range detail.MemorySummaries {
		if _, err := fmt.Fprintf(
			output,
			"\nmemory=%d type=%s created_at=%s\nsummary:\n%s\n",
			summary.ID,
			summary.MemoryType,
			summary.CreatedAt.Format(time.RFC3339),
			strings.TrimSpace(summary.Summary),
		); err != nil {
			return err
		}
		if strings.TrimSpace(summary.DetailsJSON) != "" {
			if _, err := fmt.Fprintf(output, "details_json=%s\n", strings.TrimSpace(summary.DetailsJSON)); err != nil {
				return err
			}
		}
	}

	for _, delegation := range detail.Delegations {
		if _, err := fmt.Fprintf(
			output,
			"\ndelegation=%d relation=%s key=%s role=%s status=%s child_task=%s child_run=%s\n",
			delegation.Delegation.ID,
			delegation.Relation,
			delegation.Delegation.DelegationKey,
			delegation.Delegation.Role,
			delegation.Delegation.Status,
			formatNullableInt64(delegation.Delegation.ChildTaskID),
			formatNullableInt64(delegation.Delegation.ChildRunID),
		); err != nil {
			return err
		}
		if strings.TrimSpace(delegation.Delegation.DetailsJSON) != "" {
			if _, err := fmt.Fprintf(output, "details_json=%s\n", strings.TrimSpace(delegation.Delegation.DetailsJSON)); err != nil {
				return err
			}
		}
		for _, artifact := range delegation.Artifacts {
			if _, err := fmt.Fprintf(
				output,
				"artifact=%d type=%s created_at=%s\nsummary=%s\n",
				artifact.ID,
				artifact.ArtifactType,
				artifact.CreatedAt.Format(time.RFC3339),
				strings.TrimSpace(artifact.Summary),
			); err != nil {
				return err
			}
			if strings.TrimSpace(artifact.DetailsJSON) != "" {
				if _, err := fmt.Fprintf(output, "details_json=%s\n", strings.TrimSpace(artifact.DetailsJSON)); err != nil {
					return err
				}
				if err := renderTelemetryFields(output, strings.TrimSpace(artifact.DetailsJSON)); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func renderTelemetryFields(output io.Writer, raw string) error {
	fields := telemetryFields(raw)
	for _, key := range []string{"agent_key", "requested_skill", "effective_skill", "skill_source", "portal_track", "delegation_id"} {
		if value := strings.TrimSpace(fields[key]); value != "" {
			if _, err := fmt.Fprintf(output, "%s=%s\n", key, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func telemetryFields(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil
	}

	fields := make(map[string]string)
	for key, value := range decoded {
		switch typed := value.(type) {
		case string:
			fields[key] = typed
		case map[string]any:
			for nestedKey, nestedValue := range typed {
				if nestedString, ok := nestedValue.(string); ok {
					fields[nestedKey] = nestedString
				}
			}
		}
	}
	return fields
}

func formatNullableInt64(value *int64) string {
	if value == nil {
		return "none"
	}
	return strconv.FormatInt(*value, 10)
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

func (shell *Shell) selectProject(project projects.Manifest, output io.Writer) error {
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

func (shell *Shell) executionRequestForPrompt(prompt string) (jobsvc.ExecutionRequest, error) {
	return shell.executionRequestForPromptWithContext(context.Background(), prompt)
}

func (shell *Shell) executionRequestForPromptWithContext(ctx context.Context, prompt string) (jobsvc.ExecutionRequest, error) {
	workflow, hasWorkflow := shell.lookupWorkflow(shell.state.SelectedWorkflowKey)
	if shell.state.SelectedWorkflowKey != "" && !hasWorkflow {
		return jobsvc.ExecutionRequest{}, fmt.Errorf("selected workflow %s is not available", shell.state.SelectedWorkflowKey)
	}
	if hasWorkflow {
		if err := validateRegistryItem(workflow, registry.KindWorkflow); err != nil {
			return jobsvc.ExecutionRequest{}, err
		}
	}

	skill, hasSkill := shell.lookupSkill(shell.state.SelectedSkillKey)
	if shell.state.SelectedSkillKey != "" && !hasSkill {
		return jobsvc.ExecutionRequest{}, fmt.Errorf("selected skill %s is not available", shell.state.SelectedSkillKey)
	}
	if !hasWorkflow && !hasSkill {
		return jobsvc.ExecutionRequest{}, nil
	}
	if hasSkill && !catalog.MatchesScope(skill.Scopes, shell.catalogScope()) {
		return jobsvc.ExecutionRequest{}, fmt.Errorf("selected skill %s is not available in %s scope", skill.Key, shell.catalogScope())
	}
	if hasSkill {
		if err := validateSkillItem(skill); err != nil {
			return jobsvc.ExecutionRequest{}, err
		}
	}
	metadata := map[string]string{}
	if hasWorkflow {
		metadata["workflow_key"] = workflow.Key
		metadata["workflow_title"] = workflow.Title
	}
	if hasSkill {
		metadata["skill_key"] = skill.Key
		metadata["skill_title"] = skill.Title
	}

	supplementalContext := ""
	if shell.shouldInjectSocialRetrospectiveContext(workflow, hasWorkflow, skill, hasSkill) {
		retrospectiveContext, err := shell.socialRetrospectivePromptContext(ctx)
		if err != nil {
			return jobsvc.ExecutionRequest{}, err
		}
		supplementalContext = retrospectiveContext
	}
	return jobsvc.ExecutionRequest{
		PromptOverride: prompting.ComposeExecutionPrompt(strings.TrimSpace(prompt), workflow, hasWorkflow, skill, hasSkill, supplementalContext),
		Metadata:       metadata,
	}, nil
}

func (shell *Shell) newBroker() *broker.Broker {
	return broker.New(shell.env.RegistrySnapshot, catalog.BuiltinDefinitions(), budgets.Limits{
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

func (shell *Shell) listRegistryItems(kind registry.Kind) []registry.Item {
	items := make([]registry.Item, 0)
	for _, item := range shell.env.RegistrySnapshot.Items {
		if item.Kind == kind {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i int, j int) bool {
		return items[i].Key < items[j].Key
	})
	return items
}

func (shell *Shell) lookupRegistryItem(key string, kind registry.Kind) (registry.Item, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return registry.Item{}, false
	}
	if item, ok := shell.env.RegistrySnapshot.ByKey[key]; ok && item.Kind == kind {
		return item, true
	}
	for _, item := range shell.env.RegistrySnapshot.Items {
		if item.Key == key && item.Kind == kind {
			return item, true
		}
	}
	return registry.Item{}, false
}

func (shell *Shell) lookupSkill(key string) (registry.Item, bool) {
	return shell.lookupRegistryItem(key, registry.KindSkill)
}

func (shell *Shell) lookupWorkflow(key string) (registry.Item, bool) {
	return shell.lookupRegistryItem(key, registry.KindWorkflow)
}

func (shell *Shell) skillExists(key string) bool {
	_, ok := shell.lookupSkill(key)
	return ok
}

func (shell *Shell) workflowExists(key string) bool {
	_, ok := shell.lookupWorkflow(key)
	return ok
}

func (shell *Shell) syncRegistry(registry projects.Registry) {
	shell.env.Registry = registry
	shell.jobs.Registry = registry
	shell.conversation.Registry = registry
}

func (shell *Shell) persistState() error {
	cache := Cache{
		Mode:                shell.state.Mode,
		SelectedSkillKey:    shell.state.SelectedSkillKey,
		SelectedWorkflowKey: shell.state.SelectedWorkflowKey,
	}
	if shell.state.Scope.Kind == scope.ScopeProject || shell.state.Scope.Kind == scope.ScopeOdinCore {
		cache.ProjectKey = shell.state.Scope.ProjectKey
	}
	return shell.env.SessionStore.Save(cache)
}

func (shell *Shell) memoryScope(ctx context.Context) (knowledgememory.Scope, error) {
	if shell.state.SelectedWorkflowKey != "" {
		if _, ok := shell.lookupWorkflow(shell.state.SelectedWorkflowKey); !ok {
			return knowledgememory.Scope{}, fmt.Errorf("selected workflow %s is not available", shell.state.SelectedWorkflowKey)
		}
		return knowledgememory.Scope{
			Value: "workflow",
			Key:   shell.state.SelectedWorkflowKey,
		}, nil
	}

	switch shell.state.Scope.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		scopeValue := string(shell.state.Scope.Kind)
		result := knowledgememory.Scope{
			Value: scopeValue,
			Key:   shell.state.Scope.ProjectKey,
		}
		project, err := shell.env.Store.GetProjectByKey(ctx, shell.state.Scope.ProjectKey)
		switch err {
		case nil:
			result.ProjectID = &project.ID
		case sql.ErrNoRows:
		default:
			return knowledgememory.Scope{}, err
		}
		return result, nil
	case scope.ScopeNewProject:
		return knowledgememory.Scope{
			Value: "new-project",
			Key:   "new-project",
		}, nil
	default:
		return knowledgememory.Scope{
			Value: "global",
			Key:   "global",
		}, nil
	}
}

func (shell *Shell) memoryDetailsJSON(scope knowledgememory.Scope, fields map[string]string) (string, error) {
	payload := memoryDetailsPayload{
		Source:              "cli",
		SelectedWorkflowKey: shell.state.SelectedWorkflowKey,
		SelectedSkillKey:    shell.state.SelectedSkillKey,
		Scope:               scope.Value,
		ScopeKey:            scope.Key,
		Fields:              fields,
	}
	return marshalMemoryDetailsPayload(payload)
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

func validateSkillItem(item registry.Item) error {
	return validateRegistryItem(item, registry.KindSkill)
}

func validateRegistryItem(item registry.Item, wantKind registry.Kind) error {
	if item.Kind != wantKind {
		return fmt.Errorf("capability %s is not a %s", item.Key, wantKind)
	}
	for _, section := range registry.RequiredSections {
		if strings.TrimSpace(item.Sections[section]) == "" {
			return fmt.Errorf("missing required section %q", section)
		}
	}
	switch wantKind {
	case registry.KindSkill:
		if strings.TrimSpace(item.Strictness) == "" {
			return fmt.Errorf("missing skill strictness")
		}
		if len(item.AppliesTo) == 0 {
			return fmt.Errorf("missing skill applies_to")
		}
	case registry.KindAgent:
		if strings.TrimSpace(item.Role) == "" {
			return fmt.Errorf("missing agent role")
		}
		if len(item.Scopes) == 0 {
			return fmt.Errorf("missing agent scopes")
		}
		if len(item.Tools) == 0 {
			return fmt.Errorf("missing agent tools")
		}
	case registry.KindWorkflow:
		if strings.TrimSpace(item.Entrypoint) == "" {
			return fmt.Errorf("missing workflow entrypoint")
		}
		if len(item.Composes) == 0 {
			return fmt.Errorf("missing workflow composes")
		}
	}
	return nil
}

func composeSkillPrompt(prompt string, item registry.Item) string {
	return prompting.ComposeSkillPrompt(prompt, item)
}

func composeExecutionPrompt(prompt string, workflow registry.Item, hasWorkflow bool, skill registry.Item, hasSkill bool, supplementalContext string) string {
	return prompting.ComposeExecutionPrompt(prompt, workflow, hasWorkflow, skill, hasSkill, supplementalContext)
}

func (shell *Shell) shouldInjectSocialRetrospectiveContext(workflow registry.Item, hasWorkflow bool, skill registry.Item, hasSkill bool) bool {
	return hasWorkflow && workflow.Key == "marcus-social-growth-workflow" && hasSkill && skill.Key == "marcus-social-analytics-advisor"
}

func (shell *Shell) maybeRecordSocialDraftFromAsk(ctx context.Context, prompt string, response convsvc.Response) error {
	if response.Intent != "conversation" {
		return nil
	}
	if strings.TrimSpace(response.Warning) != "" || strings.TrimSpace(response.ExecutorKey) == "" {
		return nil
	}

	fields, ok := socialDraftFieldsForSelectedSkill(shell.state.SelectedWorkflowKey, shell.state.SelectedSkillKey, prompt)
	if !ok {
		return nil
	}

	scope, err := shell.memoryScope(ctx)
	if err != nil {
		return err
	}

	details, err := shell.memoryDetailsJSON(scope, fields)
	if err != nil {
		return err
	}

	_, err = knowledgememory.Service{Store: shell.env.Store}.Record(
		ctx,
		scope,
		"social_draft",
		strings.TrimSpace(response.Answer),
		details,
		nil,
	)
	return err
}

func socialDraftFieldsForSelectedSkill(workflowKey string, skillKey string, prompt string) (map[string]string, bool) {
	if strings.TrimSpace(workflowKey) != "marcus-social-growth-workflow" {
		return nil, false
	}

	fields := map[string]string{
		"approval": "pending",
	}
	lowerPrompt := strings.ToLower(prompt)

	switch strings.TrimSpace(skillKey) {
	case "marcus-social-content-strategist":
		fields["artifact_kind"] = "plan"
		fields["channel"] = "mixed"
	case "marcus-x-drafting-assistant":
		fields["channel"] = "x"
		fields["content_kind"] = "post"
		if promptRequestsThread(lowerPrompt) {
			fields["content_kind"] = "thread"
		}
	case "marcus-linkedin-drafting-assistant":
		fields["channel"] = "linkedin"
		fields["content_kind"] = "post"
		if strings.Contains(lowerPrompt, "article") {
			fields["content_kind"] = "article_seed"
		}
	case "marcus-engagement-research-assistant":
		fields["artifact_kind"] = "reply_suggestion"
		fields["content_kind"] = "reply"
		switch {
		case strings.Contains(lowerPrompt, "linkedin"):
			fields["channel"] = "linkedin"
		case strings.Contains(lowerPrompt, " on x") || strings.Contains(lowerPrompt, "x reply") || strings.Contains(lowerPrompt, "tweet"):
			fields["channel"] = "x"
		}
		if fields["channel"] == "x" {
			if replyTarget := extractAllowedXStatusURL(prompt); replyTarget != "" {
				fields["in_reply_to_url"] = replyTarget
			}
		}
	default:
		return nil, false
	}

	return fields, true
}

func extractAllowedXStatusURL(text string) string {
	for _, token := range strings.Fields(text) {
		candidate := strings.TrimSpace(token)
		candidate = strings.Trim(candidate, "\"'()[]{}<>,.!?;:")
		if candidate == "" {
			continue
		}
		if isAllowedXStatusURL(candidate) {
			return candidate
		}
	}
	return ""
}

func promptRequestsThread(lowerPrompt string) bool {
	if !strings.Contains(lowerPrompt, "thread") {
		return false
	}
	for _, phrase := range []string{
		"not a thread",
		"not thread",
		"one primary x post",
		"one x post",
		"one post",
		"single post",
		"text-only x post",
	} {
		if strings.Contains(lowerPrompt, phrase) {
			return false
		}
	}
	return true
}

func (shell *Shell) socialRetrospectivePromptContext(ctx context.Context) (string, error) {
	scope, err := shell.memoryScope(ctx)
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	since := now.Add(-7 * 24 * time.Hour)
	comparisonSince := now.Add(-28 * 24 * time.Hour)

	outcomes, err := shell.recentSocialMemories(ctx, scope, "social_outcome", since, 6)
	if err != nil {
		return "", err
	}
	learnings, err := shell.recentSocialMemories(ctx, scope, "social_learning", since, 4)
	if err != nil {
		return "", err
	}
	research, err := shell.recentSocialMemories(ctx, scope, "social_research", since, 4)
	if err != nil {
		return "", err
	}
	evidence, err := shell.recentSocialMemories(ctx, scope, "social_evidence", since, 4)
	if err != nil {
		return "", err
	}

	comparisonOutcomes, err := shell.recentSocialMemories(ctx, scope, "social_outcome", comparisonSince, 0)
	if err != nil {
		return "", err
	}
	comparisonLearnings, err := shell.recentSocialMemories(ctx, scope, "social_learning", comparisonSince, 0)
	if err != nil {
		return "", err
	}
	comparisonResearch, err := shell.recentSocialMemories(ctx, scope, "social_research", comparisonSince, 0)
	if err != nil {
		return "", err
	}

	retrospective := summarizeSocialRetrospectiveContext(outcomes, learnings, research, filterRecentXVisibleEvidence(evidence))
	comparison := summarizeSocialMultiWeekComparison(now, comparisonOutcomes, comparisonLearnings, comparisonResearch)
	carryForward := summarizeSocialCarryForward(
		comparison.RecurringApproved,
		comparison.RecurringRejected,
		comparison.RecurringLearnings,
		comparison.RecurringResearch,
		comparison.NewThisWeek,
	)
	return retrospective + "\n" + comparison.Text + "\n" + carryForward, nil
}

func filterRecentXVisibleEvidence(summaries []sqlite.MemorySummary) []sqlite.MemorySummary {
	filtered := make([]sqlite.MemorySummary, 0, len(summaries))
	for _, summary := range summaries {
		details, err := parseMemoryDetails(summary.DetailsJSON)
		if err != nil {
			continue
		}
		if strings.TrimSpace(details.Fields["channel"]) != "x" {
			continue
		}
		if strings.TrimSpace(details.Fields["evidence_kind"]) != "x_post_visible" {
			continue
		}
		filtered = append(filtered, summary)
	}
	return filtered
}

func (shell *Shell) recentSocialMemories(ctx context.Context, scope knowledgememory.Scope, memoryType string, since time.Time, limit int) ([]sqlite.MemorySummary, error) {
	summaries, err := knowledgememory.Service{Store: shell.env.Store}.List(ctx, scope, memoryType)
	if err != nil {
		return nil, err
	}

	filtered := make([]sqlite.MemorySummary, 0, len(summaries))
	for _, summary := range summaries {
		if summary.CreatedAt.Before(since) {
			continue
		}
		filtered = append(filtered, summary)
	}

	sort.Slice(filtered, func(i int, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].ID > filtered[j].ID
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func summarizeSocialRetrospectiveContext(outcomes []sqlite.MemorySummary, learnings []sqlite.MemorySummary, research []sqlite.MemorySummary, evidence []sqlite.MemorySummary) string {
	var approved []string
	var rejected []string
	for _, outcome := range outcomes {
		details, _ := parseMemoryDetails(outcome.DetailsJSON)
		label := formatRetrospectiveMemoryLine(outcome.Summary, details.Fields)
		switch details.Fields["result"] {
		case "approved":
			approved = append(approved, label)
		case "rejected":
			rejected = append(rejected, label)
		}
	}

	learningLines := formatRetrospectiveMemoryLines(learnings)
	researchLines := formatRetrospectiveMemoryLines(research)
	evidenceLines := formatRetrospectiveMemoryLines(evidence)

	var builder strings.Builder
	builder.WriteString("Retrospective Window: last 7 days\n")
	if len(approved) == 0 && len(rejected) == 0 && len(learningLines) == 0 && len(researchLines) == 0 && len(evidenceLines) == 0 {
		builder.WriteString("No recent retrospective memory found in the last 7 days.\n")
	}
	builder.WriteString("Recent Approved Outcomes:\n")
	writeRetrospectiveList(&builder, approved)
	builder.WriteString("Recent Rejected Outcomes:\n")
	writeRetrospectiveList(&builder, rejected)
	builder.WriteString("Recent Learnings:\n")
	writeRetrospectiveList(&builder, learningLines)
	builder.WriteString("Recent Research Signals:\n")
	writeRetrospectiveList(&builder, researchLines)
	if len(evidenceLines) > 0 {
		builder.WriteString("Latest X Visible Evidence Snapshot:\n")
		writeRetrospectiveList(&builder, evidenceLines[:1])
		if len(evidenceLines) > 1 {
			builder.WriteString("Recent X Visible Evidence History:\n")
			writeRetrospectiveList(&builder, evidenceLines[1:])
		}
		builder.WriteString("Use the Latest X Visible Evidence Snapshot entry as the canonical most recent visible evidence. Treat any X visible evidence history lines as older captures.\n")
	}
	builder.WriteString("X Voice Guidance: express Marcus's inner thoughts, perspective, conviction, tension, and concise observations.\n")
	builder.WriteString("LinkedIn Voice Guidance: use more professional framing, practical lessons, clearer structure, and peer-level authority.\n")
	return builder.String()
}

type socialComparisonWindow struct {
	Start            time.Time
	End              time.Time
	ApprovedCount    int
	RejectedCount    int
	LearningCount    int
	ResearchCount    int
	ApprovedPatterns map[string]struct{}
	RejectedPatterns map[string]struct{}
	LearningPatterns map[string]struct{}
	ResearchPatterns map[string]struct{}
}

type socialComparisonSummary struct {
	Text               string
	RecurringApproved  []string
	RecurringRejected  []string
	RecurringLearnings []string
	RecurringResearch  []string
	NewThisWeek        []string
}

func summarizeSocialMultiWeekComparison(now time.Time, outcomes []sqlite.MemorySummary, learnings []sqlite.MemorySummary, research []sqlite.MemorySummary) socialComparisonSummary {
	windows := buildSocialComparisonWindows(now, 4)
	indexOutcomePatterns(windows, outcomes)
	indexLearningPatterns(windows, learnings)
	indexResearchPatterns(windows, research)

	recurringApproved := recurringPatterns(windows, func(window socialComparisonWindow) map[string]struct{} {
		return window.ApprovedPatterns
	})
	recurringRejected := recurringPatterns(windows, func(window socialComparisonWindow) map[string]struct{} {
		return window.RejectedPatterns
	})
	recurringLearnings := recurringPatterns(windows, func(window socialComparisonWindow) map[string]struct{} {
		return window.LearningPatterns
	})
	recurringResearch := recurringPatterns(windows, func(window socialComparisonWindow) map[string]struct{} {
		return window.ResearchPatterns
	})

	seenEarlier := make(map[string]struct{})
	for _, patterns := range []map[string]struct{}{
		unionPatterns(windows[1:], func(window socialComparisonWindow) map[string]struct{} { return window.ApprovedPatterns }),
		unionPatterns(windows[1:], func(window socialComparisonWindow) map[string]struct{} { return window.RejectedPatterns }),
		unionPatterns(windows[1:], func(window socialComparisonWindow) map[string]struct{} { return window.LearningPatterns }),
		unionPatterns(windows[1:], func(window socialComparisonWindow) map[string]struct{} { return window.ResearchPatterns }),
	} {
		for pattern := range patterns {
			seenEarlier[pattern] = struct{}{}
		}
	}

	var newThisWeek []string
	for _, patterns := range []map[string]struct{}{
		windows[0].ApprovedPatterns,
		windows[0].RejectedPatterns,
		windows[0].LearningPatterns,
		windows[0].ResearchPatterns,
	} {
		for _, pattern := range sortedPatternKeys(patterns) {
			if _, ok := seenEarlier[pattern]; ok {
				continue
			}
			newThisWeek = append(newThisWeek, pattern)
		}
	}

	var builder strings.Builder
	builder.WriteString("Comparison Window: last 4 weekly windows\n")
	for index, window := range windows {
		builder.WriteString(fmt.Sprintf(
			"Week %d (%s to %s): approved=%d rejected=%d learnings=%d research=%d\n",
			index+1,
			window.Start.Format("2006-01-02"),
			window.End.Format("2006-01-02"),
			window.ApprovedCount,
			window.RejectedCount,
			window.LearningCount,
			window.ResearchCount,
		))
	}
	builder.WriteString("Recurring Approval Patterns:\n")
	writeRetrospectiveList(&builder, recurringApproved)
	builder.WriteString("Recurring Rejection Patterns:\n")
	writeRetrospectiveList(&builder, recurringRejected)
	builder.WriteString("Recurring Learning Signals:\n")
	writeRetrospectiveList(&builder, recurringLearnings)
	builder.WriteString("Recurring Research Signals:\n")
	writeRetrospectiveList(&builder, recurringResearch)
	builder.WriteString("New This Week:\n")
	writeRetrospectiveList(&builder, newThisWeek)
	return socialComparisonSummary{
		Text:               builder.String(),
		RecurringApproved:  recurringApproved,
		RecurringRejected:  recurringRejected,
		RecurringLearnings: recurringLearnings,
		RecurringResearch:  recurringResearch,
		NewThisWeek:        newThisWeek,
	}
}

func summarizeSocialCarryForward(recurringApproved []string, recurringRejected []string, recurringLearnings []string, recurringResearch []string, newThisWeek []string) string {
	testNext := dedupeStringsPreserveOrder(append(append(append([]string{}, recurringLearnings...), recurringResearch...), newThisWeek...))
	xDirection := platformCarryForwardDirection(
		"Keep X closer to Marcus's inner thoughts, conviction, tension, and concise observations.",
		[]string{"[x]", "x/"},
		recurringApproved,
		recurringRejected,
		testNext,
	)
	linkedinDirection := platformCarryForwardDirection(
		"Keep LinkedIn more professionally framed, practical, structured, and peer-level.",
		[]string{"[linkedin]", "linkedin/"},
		recurringApproved,
		recurringRejected,
		testNext,
	)

	var builder strings.Builder
	builder.WriteString("Next-Week Carry-Forward\n")
	builder.WriteString("Keep:\n")
	writeRetrospectiveList(&builder, recurringApproved)
	builder.WriteString("Avoid:\n")
	writeRetrospectiveList(&builder, recurringRejected)
	builder.WriteString("Test Next:\n")
	writeRetrospectiveList(&builder, testNext)
	builder.WriteString("X Direction:\n")
	writeRetrospectiveList(&builder, xDirection)
	builder.WriteString("LinkedIn Direction:\n")
	writeRetrospectiveList(&builder, linkedinDirection)
	return builder.String()
}

func buildSocialComparisonWindows(now time.Time, count int) []socialComparisonWindow {
	windows := make([]socialComparisonWindow, 0, count)
	end := now.UTC()
	for index := 0; index < count; index++ {
		start := end.Add(-7 * 24 * time.Hour)
		windows = append(windows, socialComparisonWindow{
			Start:            start,
			End:              end,
			ApprovedPatterns: make(map[string]struct{}),
			RejectedPatterns: make(map[string]struct{}),
			LearningPatterns: make(map[string]struct{}),
			ResearchPatterns: make(map[string]struct{}),
		})
		end = start
	}
	return windows
}

func indexOutcomePatterns(windows []socialComparisonWindow, outcomes []sqlite.MemorySummary) {
	for _, outcome := range outcomes {
		windowIndex := socialComparisonWindowIndex(windows, outcome.CreatedAt)
		if windowIndex == -1 {
			continue
		}
		details, _ := parseMemoryDetails(outcome.DetailsJSON)
		label := socialOutcomePatternLabel(details.Fields)
		switch details.Fields["result"] {
		case "approved":
			windows[windowIndex].ApprovedCount++
			if label != "" {
				windows[windowIndex].ApprovedPatterns["- "+label] = struct{}{}
			}
		case "rejected":
			windows[windowIndex].RejectedCount++
			if label != "" {
				windows[windowIndex].RejectedPatterns["- "+label] = struct{}{}
			}
		}
	}
}

func indexLearningPatterns(windows []socialComparisonWindow, learnings []sqlite.MemorySummary) {
	for _, learning := range learnings {
		windowIndex := socialComparisonWindowIndex(windows, learning.CreatedAt)
		if windowIndex == -1 {
			continue
		}
		windows[windowIndex].LearningCount++
		details, _ := parseMemoryDetails(learning.DetailsJSON)
		label := formatRetrospectiveMemoryLine(learning.Summary, details.Fields)
		if label != "" {
			windows[windowIndex].LearningPatterns[label] = struct{}{}
		}
	}
}

func indexResearchPatterns(windows []socialComparisonWindow, research []sqlite.MemorySummary) {
	for _, summary := range research {
		windowIndex := socialComparisonWindowIndex(windows, summary.CreatedAt)
		if windowIndex == -1 {
			continue
		}
		windows[windowIndex].ResearchCount++
		details, _ := parseMemoryDetails(summary.DetailsJSON)
		label := formatRetrospectiveMemoryLine(summary.Summary, details.Fields)
		if label != "" {
			windows[windowIndex].ResearchPatterns[label] = struct{}{}
		}
	}
}

func socialComparisonWindowIndex(windows []socialComparisonWindow, createdAt time.Time) int {
	createdAt = createdAt.UTC()
	for index, window := range windows {
		if (createdAt.Equal(window.Start) || createdAt.After(window.Start)) && createdAt.Before(window.End) {
			return index
		}
		if index == 0 && createdAt.Equal(window.End) {
			return index
		}
	}
	return -1
}

func socialOutcomePatternLabel(fields map[string]string) string {
	result := strings.TrimSpace(fields["result"])
	channel := strings.TrimSpace(fields["channel"])
	contentKind := strings.TrimSpace(fields["content_kind"])
	switch {
	case channel != "" && contentKind != "" && result != "":
		return channel + "/" + contentKind + " " + result
	case channel != "" && result != "":
		return channel + " " + result
	case contentKind != "" && result != "":
		return contentKind + " " + result
	default:
		return strings.TrimSpace(result)
	}
}

func dedupeStringsPreserveOrder(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func platformCarryForwardDirection(baseline string, markers []string, groups ...[]string) []string {
	lines := []string{"- " + baseline}
	for _, entry := range dedupeStringsPreserveOrder(flattenStringGroups(groups...)) {
		if !containsAnyFold(entry, markers...) {
			continue
		}
		lines = append(lines, entry)
	}
	return lines
}

func containsAnyFold(value string, markers ...string) bool {
	value = strings.ToLower(value)
	for _, marker := range markers {
		if strings.Contains(value, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func flattenStringGroups(groups ...[]string) []string {
	var flattened []string
	for _, group := range groups {
		flattened = append(flattened, group...)
	}
	return flattened
}

func recurringPatterns(windows []socialComparisonWindow, selector func(socialComparisonWindow) map[string]struct{}) []string {
	if len(windows) == 0 {
		return nil
	}
	current := selector(windows[0])
	previous := unionPatterns(windows[1:], selector)
	var recurring []string
	for _, pattern := range sortedPatternKeys(current) {
		if _, ok := previous[pattern]; ok {
			recurring = append(recurring, pattern)
		}
	}
	return recurring
}

func unionPatterns(windows []socialComparisonWindow, selector func(socialComparisonWindow) map[string]struct{}) map[string]struct{} {
	union := make(map[string]struct{})
	for _, window := range windows {
		for pattern := range selector(window) {
			union[pattern] = struct{}{}
		}
	}
	return union
}

func sortedPatternKeys(patterns map[string]struct{}) []string {
	keys := make([]string, 0, len(patterns))
	for key := range patterns {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func formatRetrospectiveMemoryLines(summaries []sqlite.MemorySummary) []string {
	lines := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		details, _ := parseMemoryDetails(summary.DetailsJSON)
		lines = append(lines, formatRetrospectiveMemoryLine(summary.Summary, details.Fields))
	}
	return lines
}

func formatRetrospectiveMemoryLine(summary string, fields map[string]string) string {
	if strings.TrimSpace(fields["channel"]) == "x" && strings.TrimSpace(fields["evidence_kind"]) == "x_post_visible" {
		return formatXVisibleEvidenceRetrospectiveLine(summary, fields)
	}

	var prefixes []string
	if channel := strings.TrimSpace(fields["channel"]); channel != "" {
		prefixes = append(prefixes, channel)
	}
	if contentKind := strings.TrimSpace(fields["content_kind"]); contentKind != "" {
		prefixes = append(prefixes, contentKind)
	}
	if len(prefixes) == 0 {
		return "- " + strings.TrimSpace(summary)
	}
	return "- [" + strings.Join(prefixes, " ") + "] " + strings.TrimSpace(summary)
}

func formatXVisibleEvidenceRetrospectiveLine(summary string, fields map[string]string) string {
	parts := []string{"- [x]"}
	if handle := strings.TrimSpace(fields["author_handle"]); handle != "" {
		parts = append(parts, handle)
	}
	for _, metric := range []struct {
		field string
		label string
	}{
		{field: "reply_count", label: "replies"},
		{field: "repost_count", label: "reposts"},
		{field: "like_count", label: "likes"},
		{field: "bookmark_count", label: "bookmarks"},
		{field: "view_count", label: "views"},
	} {
		if value := strings.TrimSpace(fields[metric.field]); value != "" {
			parts = append(parts, metric.label+"="+value)
		}
	}
	parts = append(parts, strings.TrimSpace(summary))
	return strings.Join(parts, " ")
}

func writeRetrospectiveList(builder *strings.Builder, entries []string) {
	if len(entries) == 0 {
		builder.WriteString("- none\n")
		return
	}
	for _, entry := range entries {
		builder.WriteString(entry)
		builder.WriteString("\n")
	}
}

func renderSkillDetail(output io.Writer, item registry.Item) error {
	if _, err := fmt.Fprintf(output, "skill=%s title=%s\nsummary=%s\nsource=%s\nstrictness=%s\n", item.Key, item.Title, item.Summary, item.Source.RelativePath, item.Strictness); err != nil {
		return err
	}
	if len(item.AppliesTo) > 0 {
		if _, err := fmt.Fprintf(output, "applies_to=%s\n", strings.Join(item.AppliesTo, ",")); err != nil {
			return err
		}
	}
	if len(item.Tags) > 0 {
		if _, err := fmt.Fprintf(output, "tags=%s\n", strings.Join(item.Tags, ",")); err != nil {
			return err
		}
	}
	for _, section := range registry.RequiredSections {
		value := strings.TrimSpace(item.Sections[section])
		if value == "" {
			continue
		}
		if _, err := fmt.Fprintf(output, "\n%s:\n%s\n", section, value); err != nil {
			return err
		}
	}
	return nil
}

func renderAgentDetail(output io.Writer, item registry.Item) error {
	if _, err := fmt.Fprintf(output, "agent=%s title=%s\nsummary=%s\nsource=%s\nrole=%s\n", item.Key, item.Title, item.Summary, item.Source.RelativePath, item.Role); err != nil {
		return err
	}
	if len(item.Scopes) > 0 {
		if _, err := fmt.Fprintf(output, "scopes=%s\n", strings.Join(item.Scopes, ",")); err != nil {
			return err
		}
	}
	if len(item.Tools) > 0 {
		if _, err := fmt.Fprintf(output, "tools=%s\n", strings.Join(item.Tools, ",")); err != nil {
			return err
		}
	}
	if len(item.Tags) > 0 {
		if _, err := fmt.Fprintf(output, "tags=%s\n", strings.Join(item.Tags, ",")); err != nil {
			return err
		}
	}
	for _, section := range registry.RequiredSections {
		value := strings.TrimSpace(item.Sections[section])
		if value == "" {
			continue
		}
		if _, err := fmt.Fprintf(output, "\n%s:\n%s\n", section, value); err != nil {
			return err
		}
	}
	return nil
}

func renderWorkflowDetail(output io.Writer, item registry.Item) error {
	if _, err := fmt.Fprintf(output, "workflow=%s title=%s\nsummary=%s\nsource=%s\nentrypoint=%s\n", item.Key, item.Title, item.Summary, item.Source.RelativePath, item.Entrypoint); err != nil {
		return err
	}
	if len(item.Composes) > 0 {
		if _, err := fmt.Fprintf(output, "composes=%s\n", strings.Join(item.Composes, ",")); err != nil {
			return err
		}
	}
	if len(item.Tags) > 0 {
		if _, err := fmt.Fprintf(output, "tags=%s\n", strings.Join(item.Tags, ",")); err != nil {
			return err
		}
	}
	for _, section := range registry.RequiredSections {
		value := strings.TrimSpace(item.Sections[section])
		if value == "" {
			continue
		}
		if _, err := fmt.Fprintf(output, "\n%s:\n%s\n", section, value); err != nil {
			return err
		}
	}
	return nil
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
		_, err := fmt.Fprintf(output, "raw_ref=%s\n", result.RawRef)
		return err
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

type memoryRememberRequest struct {
	MemoryType string
	Summary    string
	Fields     map[string]string
}

type memoryResolveRequest struct {
	MemoryID int64
	Result   string
	Reason   string
}

type memoryPublishRequest struct {
	MemoryID    int64
	URL         string
	PublishedAt time.Time
	Via         string
}

type memoryListRequest struct {
	MemoryType   string
	Contains     string
	FieldFilters map[string]string
	Limit        int
	OrderDesc    bool
}

type memoryDetailsPayload struct {
	Source              string            `json:"source"`
	SelectedWorkflowKey string            `json:"selected_workflow_key,omitempty"`
	SelectedSkillKey    string            `json:"selected_skill_key,omitempty"`
	Scope               string            `json:"scope"`
	ScopeKey            string            `json:"scope_key"`
	Fields              map[string]string `json:"fields,omitempty"`
}

func parseMemoryRememberArgs(args []string) (memoryRememberRequest, error) {
	if len(args) == 0 {
		return memoryRememberRequest{}, fmt.Errorf("memory type is required")
	}

	request := memoryRememberRequest{
		MemoryType: strings.TrimSpace(args[0]),
		Fields:     make(map[string]string),
	}
	if request.MemoryType == "" {
		return memoryRememberRequest{}, fmt.Errorf("memory type is required")
	}

	summaryStart := -1
	for index := 1; index < len(args); index++ {
		token := strings.TrimSpace(args[index])
		if token == "" {
			continue
		}
		if token == "--" {
			summaryStart = index + 1
			break
		}
		if summaryStart == -1 {
			key, value, ok := strings.Cut(token, "=")
			if ok && strings.TrimSpace(key) != "" {
				request.Fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
				continue
			}
			summaryStart = index
			break
		}
	}

	if summaryStart == -1 {
		return memoryRememberRequest{}, fmt.Errorf("memory summary is required")
	}
	request.Summary = strings.TrimSpace(strings.Join(args[summaryStart:], " "))
	if request.Summary == "" {
		return memoryRememberRequest{}, fmt.Errorf("memory summary is required")
	}
	if len(request.Fields) == 0 {
		request.Fields = nil
	}
	return request, nil
}

func parseMemoryResolveArgs(args []string) (memoryResolveRequest, error) {
	if len(args) == 0 {
		return memoryResolveRequest{}, fmt.Errorf("memory id is required")
	}

	memoryID, err := strconv.ParseInt(strings.TrimSpace(args[0]), 10, 64)
	if err != nil || memoryID <= 0 {
		return memoryResolveRequest{}, fmt.Errorf("memory id must be a positive integer")
	}

	request := memoryResolveRequest{MemoryID: memoryID}
	for _, token := range args[1:] {
		key, value, ok := strings.Cut(strings.TrimSpace(token), "=")
		if !ok {
			return memoryResolveRequest{}, fmt.Errorf("unknown memory resolve option: %s", token)
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "result":
			request.Result = strings.TrimSpace(value)
		case "reason":
			request.Reason = strings.TrimSpace(value)
		default:
			return memoryResolveRequest{}, fmt.Errorf("unknown memory resolve option: %s", token)
		}
	}

	if request.Result != "approved" && request.Result != "rejected" {
		return memoryResolveRequest{}, fmt.Errorf("memory resolve requires result=approved|rejected")
	}

	return request, nil
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

func validateMemoryRememberRequest(request memoryRememberRequest) error {
	if request.MemoryType != "social_outcome" {
		return nil
	}
	return validateSocialOutcomeFields(request.Fields)
}

func validateSocialOutcomeFields(fields map[string]string) error {
	if !memoryFieldAllowed(fields, "result", "approved", "rejected") {
		return fmt.Errorf("social_outcome requires result=approved|rejected")
	}
	if !memoryFieldAllowed(fields, "channel", "x", "linkedin") {
		return fmt.Errorf("social_outcome requires channel=x|linkedin")
	}
	if !memoryFieldAllowed(fields, "content_kind", "post", "reply", "thread", "article_seed") {
		return fmt.Errorf("social_outcome requires content_kind=post|reply|thread|article_seed")
	}
	return nil
}

func memoryFieldAllowed(fields map[string]string, key string, allowed ...string) bool {
	if len(fields) == 0 {
		return false
	}
	value, ok := fields[key]
	if !ok {
		return false
	}
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func parseMemoryListArgs(args []string) (memoryListRequest, error) {
	request := memoryListRequest{}

	for _, rawToken := range args {
		token := strings.TrimSpace(rawToken)
		if token == "" {
			continue
		}

		key, value, ok := strings.Cut(token, "=")
		if !ok {
			if request.MemoryType == "" {
				request.MemoryType = token
				continue
			}
			return memoryListRequest{}, fmt.Errorf("unknown memory list option: %s", token)
		}

		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch {
		case key == "type":
			request.MemoryType = value
		case key == "contains":
			request.Contains = value
		case key == "limit":
			limit, err := strconv.Atoi(value)
			if err != nil || limit <= 0 {
				return memoryListRequest{}, fmt.Errorf("limit must be a positive integer")
			}
			request.Limit = limit
		case key == "order":
			switch strings.ToLower(value) {
			case "", "asc":
				request.OrderDesc = false
			case "desc":
				request.OrderDesc = true
			default:
				return memoryListRequest{}, fmt.Errorf("order must be asc or desc")
			}
		case strings.HasPrefix(key, "field."):
			fieldName := strings.TrimSpace(key[len("field."):])
			if fieldName == "" {
				return memoryListRequest{}, fmt.Errorf("field filter name is required")
			}
			if request.FieldFilters == nil {
				request.FieldFilters = make(map[string]string)
			}
			request.FieldFilters[fieldName] = value
		default:
			return memoryListRequest{}, fmt.Errorf("unknown memory list option: %s", token)
		}
	}

	return request, nil
}

func parseMemoryDetails(detailsJSON string) (memoryDetailsPayload, error) {
	detailsJSON = strings.TrimSpace(detailsJSON)
	if detailsJSON == "" {
		return memoryDetailsPayload{}, nil
	}

	var payload memoryDetailsPayload
	if err := json.Unmarshal([]byte(detailsJSON), &payload); err != nil {
		return memoryDetailsPayload{}, err
	}
	if len(payload.Fields) != 0 || payload.Source != "" || payload.Scope != "" || payload.ScopeKey != "" || payload.SelectedWorkflowKey != "" || payload.SelectedSkillKey != "" {
		return payload, nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(detailsJSON), &raw); err != nil {
		return payload, nil
	}

	if source := stringValueFromRawJSON(raw["source"]); source != "" {
		payload.Source = source
	}
	if scopeValue := stringValueFromRawJSON(raw["scope"]); scopeValue != "" {
		payload.Scope = scopeValue
	}
	if scopeKey := stringValueFromRawJSON(raw["scope_key"]); scopeKey != "" {
		payload.ScopeKey = scopeKey
	}
	if selectedWorkflowKey := stringValueFromRawJSON(raw["selected_workflow_key"]); selectedWorkflowKey != "" {
		payload.SelectedWorkflowKey = selectedWorkflowKey
	}
	if selectedSkillKey := stringValueFromRawJSON(raw["selected_skill_key"]); selectedSkillKey != "" {
		payload.SelectedSkillKey = selectedSkillKey
	}
	if fields := legacyMemoryFieldsFromRaw(raw); len(fields) != 0 {
		payload.Fields = fields
	}
	return payload, nil
}

func legacyMemoryFieldsFromRaw(raw map[string]json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}

	fields := make(map[string]string)
	for key, value := range raw {
		switch key {
		case "source", "scope", "scope_key", "selected_workflow_key", "selected_skill_key", "fields", "execution_metadata", "prompt":
			continue
		}
		if scalar := scalarStringFromRawJSON(value); scalar != "" {
			fields[key] = scalar
		}
	}

	var executionMetadata map[string]string
	if len(raw["execution_metadata"]) != 0 {
		if err := json.Unmarshal(raw["execution_metadata"], &executionMetadata); err == nil {
			for key, value := range executionMetadata {
				key = strings.TrimSpace(key)
				value = strings.TrimSpace(value)
				if key == "" || value == "" {
					continue
				}
				fields[key] = value
			}
		}
	}

	if len(fields) == 0 {
		return nil
	}
	return fields
}

func stringValueFromRawJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func scalarStringFromRawJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func marshalMemoryDetailsPayload(payload memoryDetailsPayload) (string, error) {
	if strings.TrimSpace(payload.Source) == "" {
		payload.Source = "cli"
	}
	if len(payload.Fields) == 0 {
		payload.Fields = nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func normalizeMemoryDetailsPayload(summary sqlite.MemorySummary, payload memoryDetailsPayload) memoryDetailsPayload {
	if strings.TrimSpace(payload.Source) == "" {
		payload.Source = "cli"
	}
	if strings.TrimSpace(payload.Scope) == "" {
		payload.Scope = summary.Scope
	}
	if strings.TrimSpace(payload.ScopeKey) == "" {
		payload.ScopeKey = summary.ScopeKey
	}
	if payload.Fields == nil {
		payload.Fields = make(map[string]string)
	}
	return payload
}

func socialOutcomeFieldsForResolvedDraft(draftFields map[string]string, result string, reason string) (map[string]string, bool) {
	outcomeFields := make(map[string]string, 4)
	if channel := strings.TrimSpace(draftFields["channel"]); channel != "" {
		outcomeFields["channel"] = channel
	}
	if contentKind := strings.TrimSpace(draftFields["content_kind"]); contentKind != "" {
		outcomeFields["content_kind"] = contentKind
	}
	if replyTarget := strings.TrimSpace(draftFields["in_reply_to_url"]); replyTarget != "" {
		outcomeFields["in_reply_to_url"] = replyTarget
	}
	outcomeFields["result"] = strings.TrimSpace(result)
	if strings.TrimSpace(reason) != "" {
		outcomeFields["reason"] = strings.TrimSpace(reason)
	}
	if err := validateSocialOutcomeFields(outcomeFields); err != nil {
		return nil, false
	}
	return outcomeFields, true
}

func filterMemorySummaries(summaries []sqlite.MemorySummary, request memoryListRequest) []sqlite.MemorySummary {
	filtered := make([]sqlite.MemorySummary, 0, len(summaries))
	containsNeedle := strings.ToLower(strings.TrimSpace(request.Contains))

	for _, summary := range summaries {
		if containsNeedle != "" && !strings.Contains(strings.ToLower(summary.Summary), containsNeedle) {
			continue
		}
		if len(request.FieldFilters) > 0 {
			details, err := parseMemoryDetails(summary.DetailsJSON)
			if err != nil || !memoryFieldsMatch(details.Fields, request.FieldFilters) {
				continue
			}
		}
		filtered = append(filtered, summary)
	}

	if request.OrderDesc {
		for left, right := 0, len(filtered)-1; left < right; left, right = left+1, right-1 {
			filtered[left], filtered[right] = filtered[right], filtered[left]
		}
	}
	if request.Limit > 0 && len(filtered) > request.Limit {
		filtered = filtered[:request.Limit]
	}
	return filtered
}

func memoryFieldsMatch(fields map[string]string, required map[string]string) bool {
	if len(required) == 0 {
		return true
	}
	if len(fields) == 0 {
		return false
	}
	for key, want := range required {
		got, ok := fields[key]
		if !ok || got != want {
			return false
		}
	}
	return true
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
	if strings.TrimSpace(summary.DetailsJSON) != "" {
		if _, err := fmt.Fprintf(output, "details_json=%s\n", strings.TrimSpace(summary.DetailsJSON)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(output)
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
		parts = append(parts, key+"="+fields[key])
	}
	return strings.Join(parts, ",")
}

type projectAddRequest struct {
	Key           string
	GitRoot       string
	Name          string
	ProjectClass  projects.ProjectClass
	DefaultBranch string
	GitHubRepo    string
}

func parseProjectAddArgs(args []string) (projectAddRequest, error) {
	if len(args) < 2 {
		return projectAddRequest{}, fmt.Errorf("project key and git root are required")
	}

	request := projectAddRequest{
		Key:          strings.TrimSpace(args[0]),
		GitRoot:      strings.TrimSpace(args[1]),
		Name:         strings.TrimSpace(args[0]),
		ProjectClass: projects.ProjectClassLocalGit,
	}

	for _, token := range args[2:] {
		key, value, ok := strings.Cut(token, "=")
		if !ok {
			return projectAddRequest{}, fmt.Errorf("unknown project option: %s", token)
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "name":
			request.Name = value
		case "class":
			request.ProjectClass = projects.ProjectClass(value)
		case "default_branch":
			request.DefaultBranch = value
		case "github_repo":
			request.GitHubRepo = value
		default:
			return projectAddRequest{}, fmt.Errorf("unknown project option: %s", token)
		}
	}

	if request.Key == "" {
		return projectAddRequest{}, fmt.Errorf("project key is required")
	}
	if request.GitRoot == "" {
		return projectAddRequest{}, fmt.Errorf("git root is required")
	}
	if request.Name == "" {
		request.Name = request.Key
	}
	if request.GitHubRepo != "" && request.ProjectClass == projects.ProjectClassLocalGit {
		request.ProjectClass = projects.ProjectClassGitHubBacked
	}

	return request, nil
}

func (request projectAddRequest) manifest() projects.Manifest {
	return projects.Manifest{
		Key:           request.Key,
		Name:          request.Name,
		ProjectClass:  request.ProjectClass,
		GitRoot:       request.GitRoot,
		DefaultBranch: request.DefaultBranch,
		GitHub: projects.GitHub{
			Repo: request.GitHubRepo,
		},
		Policy: projects.DefaultManagedProjectPolicy(),
	}
}

func defaultManagedProjectPolicy() projects.Policy {
	trueValue := true
	falseValue := false

	return projects.Policy{
		AllowedCommands: []string{"status", "test"},
		BranchRules: projects.BranchRules{
			ProtectedBranches:          []string{"main"},
			RequireWorktree:            &trueValue,
			RequireTaskBranch:          &trueValue,
			AllowDefaultBranchMutation: &falseValue,
		},
		ApprovalGates: projects.ApprovalGates{
			RequireForGovernanceChanges:     &trueValue,
			RequireForDestructiveOperations: &trueValue,
			RequireForSystemProjectChanges:  &falseValue,
		},
		MergePolicy: projects.MergePolicy{
			Mode:                       "squash",
			AllowDirectToDefaultBranch: &falseValue,
		},
		DestructiveOperations: projects.DestructiveOperations{
			AllowReset:              &falseValue,
			AllowClean:              &falseValue,
			AllowForcePush:          &falseValue,
			RequireExplicitApproval: &trueValue,
		},
	}
}

func inferCurrentBranch(ctx context.Context, gitRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", gitRoot, "branch", "--show-current")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git branch --show-current: %w: %s", err, strings.TrimSpace(string(output)))
	}
	branch := strings.TrimSpace(string(output))
	if branch == "" {
		return "", fmt.Errorf("empty branch name")
	}
	return branch, nil
}
