package conversation

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"odin-os/internal/cli/commands"
	"odin-os/internal/cli/scope"
	coreinitiatives "odin-os/internal/core/initiatives"
	"odin-os/internal/core/projects"
	coreworkspaces "odin-os/internal/core/workspaces"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	healthsvc "odin-os/internal/runtime/health"
	jobsvc "odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/projections"
	runsvc "odin-os/internal/runtime/runs"
	"odin-os/internal/store/sqlite"
)

const defaultWorkspaceKey = "marcus"

type Service struct {
	Store               *sqlite.Store
	DB                  *sql.DB
	Registry            projects.Registry
	RegistryDiagnostics []projects.Diagnostic
	ExecutorConfig      executorrouter.Config
	Executors           map[string]contract.Executor
	Now                 func() time.Time
	StalledTimeout      time.Duration
}

type Request struct {
	Scope  scope.Resolution
	Mode   string
	Prompt string
}

type Response struct {
	Answer      string
	Intent      string
	ExecutorKey string
	ScopeLabel  string
	Warning     string
}

type Snapshot struct {
	GeneratedAt                time.Time                  `json:"generated_at"`
	ApprovalsWaiting           []ApprovalWaitingView      `json:"approvals_waiting"`
	StalledRuns                []StalledRunView           `json:"stalled_runs"`
	ActiveRuns                 []ActiveRunView            `json:"active_runs"`
	ProjectTransitions         []ProjectTransitionView    `json:"project_transitions"`
	ProjectTransitionOwnership ProjectTransitionOwnership `json:"project_transition_ownership"`
}

type ApprovalWaitingView struct {
	ApprovalID  int64  `json:"approval_id"`
	TaskID      int64  `json:"task_id"`
	TaskKey     string `json:"task_key"`
	Status      string `json:"status"`
	RequestedAt string `json:"requested_at"`
}

type ActiveRunView struct {
	RunID      int64  `json:"run_id"`
	TaskID     int64  `json:"task_id"`
	TaskKey    string `json:"task_key"`
	ProjectKey string `json:"project_key"`
	Executor   string `json:"executor"`
	Status     string `json:"status"`
	Attempt    int    `json:"attempt"`
	StartedAt  string `json:"started_at"`
}

type StalledRunView struct {
	RunID      int64  `json:"run_id"`
	TaskID     int64  `json:"task_id"`
	TaskKey    string `json:"task_key"`
	ProjectKey string `json:"project_key"`
	Executor   string `json:"executor"`
	Status     string `json:"status"`
	Attempt    int    `json:"attempt"`
	StartedAt  string `json:"started_at"`
}

type ProjectTransitionView struct {
	ProjectID       int64   `json:"project_id"`
	ProjectKey      string  `json:"project_key"`
	Name            string  `json:"name"`
	Scope           string  `json:"scope"`
	TaskCount       int     `json:"task_count"`
	OpenTaskCount   int     `json:"open_task_count"`
	LastEventAt     *string `json:"last_event_at,omitempty"`
	TransitionState string  `json:"transition_state"`
	Controller      string  `json:"controller"`
	LastReportType  string  `json:"last_report_type"`
	LastReportAt    *string `json:"last_report_at,omitempty"`
}

type ProjectTransitionOwnership struct {
	LegacyOdin int `json:"legacy_odin"`
	OdinOS     int `json:"odin_os"`
	Unknown    int `json:"unknown"`
}

func (service Service) Respond(ctx context.Context, request Request) (Response, error) {
	if service.Store == nil {
		return Response{}, fmt.Errorf("conversation store is required")
	}

	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" {
		return Response{}, fmt.Errorf("prompt is required")
	}

	scopeLabel := service.scopeLabel(request.Scope)
	var response Response

	switch commands.RouteAskIntent(prompt) {
	case commands.IntentHelp:
		response = Response{
			Answer:     helpAnswer(),
			Intent:     "help",
			ScopeLabel: scopeLabel,
		}
	case commands.IntentMode:
		response = Response{
			Answer:     fmt.Sprintf("You are currently in %s mode.", service.modeLabel(request.Mode)),
			Intent:     "mode",
			ScopeLabel: scopeLabel,
		}
	case commands.IntentScope:
		response = Response{
			Answer:     service.scopeAnswer(request.Scope),
			Intent:     "scope",
			ScopeLabel: scopeLabel,
		}
	case commands.IntentProject:
		response = Response{
			Answer:     service.projectAnswer(request.Scope),
			Intent:     "project",
			ScopeLabel: scopeLabel,
		}
	case commands.IntentJobs:
		answer, err := service.jobsAnswer(ctx, request.Scope)
		if err != nil {
			return Response{}, err
		}
		response = Response{
			Answer:     answer,
			Intent:     "jobs",
			ScopeLabel: scopeLabel,
		}
	case commands.IntentRuns:
		answer, err := service.runsAnswer(ctx, request.Scope)
		if err != nil {
			return Response{}, err
		}
		response = Response{
			Answer:     answer,
			Intent:     "runs",
			ScopeLabel: scopeLabel,
		}
	case commands.IntentApprovals:
		answer, err := service.approvalsAnswer(ctx, request.Scope)
		if err != nil {
			return Response{}, err
		}
		response = Response{
			Answer:     answer,
			Intent:     "approvals",
			ScopeLabel: scopeLabel,
		}
	case commands.IntentLogs:
		answer, err := service.logsAnswer(ctx, request.Scope)
		if err != nil {
			return Response{}, err
		}
		response = Response{
			Answer:     answer,
			Intent:     "logs",
			ScopeLabel: scopeLabel,
		}
	case commands.IntentDoctor:
		answer, err := service.doctorAnswer(ctx)
		if err != nil {
			return Response{}, err
		}
		response = Response{
			Answer:     answer,
			Intent:     "doctor",
			ScopeLabel: scopeLabel,
		}
	default:
		answer, executorKey, warning, err := service.executorAnswer(ctx, request, prompt, scopeLabel)
		if err != nil {
			return Response{}, err
		}
		response = Response{
			Answer:      answer,
			Intent:      "conversation",
			ExecutorKey: executorKey,
			ScopeLabel:  scopeLabel,
			Warning:     warning,
		}
	}

	if err := service.recordTranscript(ctx, request, response); err != nil {
		return Response{}, err
	}

	return response, nil
}

func (service Service) Snapshot(ctx context.Context) (Snapshot, error) {
	db := service.DB
	if db == nil && service.Store != nil {
		db = service.Store.DB()
	}
	if db == nil {
		return Snapshot{}, fmt.Errorf("status store is required")
	}

	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}
	stalledTimeout := service.StalledTimeout
	if stalledTimeout <= 0 {
		stalledTimeout = 30 * time.Minute
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback()

	approvals, err := projections.ListPendingApprovalViews(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	activeRuns, err := projections.ListActiveRunViews(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	stalledRuns, err := projections.ListStalledRunViews(ctx, tx, now.Add(-stalledTimeout))
	if err != nil {
		return Snapshot{}, err
	}
	transitions, err := projections.ListProjectTransitionViews(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return Snapshot{}, err
	}

	return Snapshot{
		GeneratedAt:                now,
		ApprovalsWaiting:           toApprovalWaitingViews(approvals),
		StalledRuns:                toStalledRunViews(stalledRuns),
		ActiveRuns:                 toActiveRunViews(activeRuns),
		ProjectTransitions:         toProjectTransitionViews(transitions),
		ProjectTransitionOwnership: summarizeProjectTransitions(transitions),
	}, nil
}

func helpAnswer() string {
	return "Available commands: /help /mode /scope /project /transition /observe /compare /jobs /runs /approvals /logs /doctor /self. Switch to /mode act to execute durable work."
}

func (service Service) modeLabel(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return "ask"
	}
	return strings.TrimSpace(mode)
}

func (service Service) scopeLabel(resolved scope.Resolution) string {
	switch resolved.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		if resolved.ProjectKey != "" {
			return resolved.ProjectKey
		}
		return string(resolved.Kind)
	case scope.ScopeNewProject:
		return string(scope.ScopeNewProject)
	default:
		return string(scope.ScopeGlobal)
	}
}

func (service Service) scopeAnswer(resolved scope.Resolution) string {
	switch resolved.Kind {
	case scope.ScopeProject:
		return fmt.Sprintf("You are currently in project scope for %s.", resolved.ProjectKey)
	case scope.ScopeOdinCore:
		return "You are currently in odin-core scope."
	case scope.ScopeNewProject:
		return "You are currently in new-project scope."
	default:
		return "You are currently in global scope."
	}
}

func (service Service) projectAnswer(resolved scope.Resolution) string {
	switch resolved.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		if resolved.ProjectKey != "" {
			return fmt.Sprintf("Current project is %s.", resolved.ProjectKey)
		}
		return "A project scope is selected."
	default:
		return "No project is selected right now."
	}
}

func (service Service) jobsAnswer(ctx context.Context, resolved scope.Resolution) (string, error) {
	views, err := jobsvc.Service{
		Store:    service.Store,
		Registry: service.Registry,
		Now:      time.Now,
	}.List(ctx, resolved)
	if err != nil {
		return "", err
	}
	if len(views) == 0 {
		return "There are no jobs in the current scope.", nil
	}
	latest := views[len(views)-1]
	return fmt.Sprintf("There are %d job(s) in the current scope. Latest task %s is %s.", len(views), latest.TaskKey, latest.Status), nil
}

func (service Service) runsAnswer(ctx context.Context, resolved scope.Resolution) (string, error) {
	if service.Store == nil {
		return "", fmt.Errorf("conversation store is required")
	}
	views, err := runsvc.Service{DB: service.Store.DB()}.List(ctx, resolved)
	if err != nil {
		return "", err
	}
	if len(views) == 0 {
		return "There are no runs in the current scope.", nil
	}
	latest := views[len(views)-1]
	return fmt.Sprintf("There are %d run(s) in the current scope. Latest run for %s is %s via %s.", len(views), latest.TaskKey, latest.Status, latest.Executor), nil
}

func (service Service) approvalsAnswer(ctx context.Context, resolved scope.Resolution) (string, error) {
	views, err := service.pendingApprovals(ctx)
	if err != nil {
		return "", err
	}
	filtered := make([]pendingApprovalView, 0, len(views))
	for _, view := range views {
		if matchesScope(view.ProjectKey, view.TaskScope, resolved) {
			filtered = append(filtered, view)
		}
	}
	if len(filtered) == 0 {
		return "There are no approvals waiting in the current scope.", nil
	}
	latest := filtered[len(filtered)-1]
	return fmt.Sprintf("There are %d approval(s) waiting in the current scope. Latest approval is for %s with status %s.", len(filtered), latest.TaskKey, latest.Status), nil
}

func (service Service) logsAnswer(ctx context.Context, resolved scope.Resolution) (string, error) {
	if service.Store == nil {
		return "", fmt.Errorf("conversation store is required")
	}

	params := sqlite.ListEventsParams{}
	if resolved.Kind == scope.ScopeProject || resolved.Kind == scope.ScopeOdinCore {
		project, err := service.Store.GetProjectByKey(ctx, resolved.ProjectKey)
		switch err {
		case nil:
			params.ProjectID = &project.ID
		case sql.ErrNoRows:
			return "no logs", nil
		default:
			return "", err
		}
	}

	records, err := service.Store.ListEvents(ctx, params)
	if err != nil {
		return "", err
	}

	count := 0
	for _, record := range records {
		if !matchesEventScope(record.Scope, resolved) {
			continue
		}
		count++
		if count == 10 {
			break
		}
	}
	if count == 0 {
		return "no logs", nil
	}
	return fmt.Sprintf("There are %d log event(s) in the current scope.", count), nil
}

func (service Service) doctorAnswer(ctx context.Context) (string, error) {
	if service.Store == nil {
		return "", fmt.Errorf("conversation store is required")
	}
	summary, err := healthsvc.Service{DB: service.Store.DB()}.Summary(ctx, len(service.RegistryDiagnostics) == 0)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Runtime health is %s.", summary.Status), nil
}

func (service Service) executorAnswer(ctx context.Context, request Request, prompt string, scopeLabel string) (string, string, string, error) {
	if len(service.Executors) == 0 || len(service.ExecutorConfig.Executors) == 0 {
		return service.fallbackAnswer(request), "", "", nil
	}

	spec := contract.TaskSpec{
		ID:     fmt.Sprintf("ask-%d", time.Now().UTC().UnixNano()),
		Kind:   contract.TaskKindGeneral,
		Scope:  service.scopeKindLabel(request.Scope),
		Prompt: prompt,
		Requirements: contract.Requirements{
			AllowedClasses: []contract.ExecutorClass{
				contract.ExecutorClassPlanBackedCLI,
				contract.ExecutorClassAPI,
				contract.ExecutorClassBroker,
			},
		},
		Metadata: map[string]string{
			"mode":        service.modeLabel(request.Mode),
			"scope":       service.scopeKindLabel(request.Scope),
			"scope_label": scopeLabel,
		},
	}

	selector := executorrouter.Selector{
		Config:    service.ExecutorConfig,
		Executors: service.Executors,
	}
	decision, err := selector.Select(ctx, spec)
	if err != nil {
		warning := fmt.Sprintf("Executor-backed ask is unavailable (%v).", err)
		return warning + " " + service.fallbackAnswer(request), "", warning, nil
	}

	result, err := service.Executors[decision.ExecutorKey].RunTask(ctx, spec)
	if err != nil {
		warning := fmt.Sprintf("Executor-backed ask is unavailable (%v).", err)
		return warning + " " + service.fallbackAnswer(request), "", warning, nil
	}
	if strings.TrimSpace(result.Output) == "" {
		warning := "Executor-backed ask is unavailable (empty response)."
		return warning + " " + service.fallbackAnswer(request), "", warning, nil
	}
	return strings.TrimSpace(result.Output), decision.ExecutorKey, "", nil
}

func (service Service) fallbackAnswer(request Request) string {
	return fmt.Sprintf("Odin is listening in %s scope. Switch to /mode act if you want this turned into durable work. Prompt: %s", service.scopeLabel(request.Scope), strings.TrimSpace(request.Prompt))
}

func (service Service) recordTranscript(ctx context.Context, request Request, response Response) error {
	projectID, err := service.projectIDForScope(ctx, request.Scope)
	if err != nil {
		return err
	}
	workspaceID, initiativeID, err := service.memoryOwnersForScope(ctx, projectID)
	if err != nil {
		return err
	}

	toolSummary := strings.TrimSpace(response.Intent)
	if response.Warning != "" {
		if toolSummary != "" {
			toolSummary += "; "
		}
		toolSummary += "warning=" + response.Warning
	}

	_, err = service.Store.RecordConversationTranscript(ctx, sqlite.RecordConversationTranscriptParams{
		ProjectID:    projectID,
		WorkspaceID:  workspaceID,
		InitiativeID: initiativeID,
		Scope:        service.scopeKindLabel(request.Scope),
		ScopeKey:     service.scopeLabel(request.Scope),
		Mode:         service.modeLabel(request.Mode),
		Prompt:       strings.TrimSpace(request.Prompt),
		Response:     strings.TrimSpace(response.Answer),
		ToolSummary:  toolSummary,
		Executor:     response.ExecutorKey,
	})
	return err
}

func (service Service) memoryOwnersForScope(ctx context.Context, projectID *int64) (*int64, *int64, error) {
	if service.Store == nil {
		return nil, nil, nil
	}

	workspace, err := coreworkspaces.Service{Store: service.Store}.GetByKey(ctx, defaultWorkspaceKey)
	switch {
	case err == nil:
	case err == sql.ErrNoRows:
		return nil, nil, nil
	default:
		return nil, nil, err
	}

	workspaceID := workspace.ID
	if projectID == nil {
		return &workspaceID, nil, nil
	}

	initiatives, err := coreinitiatives.Service{Store: service.Store}.ListByWorkspace(ctx, workspace.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, initiative := range initiatives {
		if initiative.LinkedProjectID != nil && *initiative.LinkedProjectID == *projectID {
			initiativeID := initiative.ID
			return &workspaceID, &initiativeID, nil
		}
	}
	return &workspaceID, nil, nil
}

func (service Service) projectIDForScope(ctx context.Context, resolved scope.Resolution) (*int64, error) {
	if service.Store == nil {
		return nil, nil
	}
	if strings.TrimSpace(resolved.ProjectKey) == "" {
		return nil, nil
	}
	project, err := service.Store.GetProjectByKey(ctx, resolved.ProjectKey)
	switch err {
	case nil:
		return &project.ID, nil
	case sql.ErrNoRows:
		return nil, nil
	default:
		return nil, err
	}
}

func (service Service) scopeKindLabel(resolved scope.Resolution) string {
	if resolved.Kind == "" {
		return string(scope.ScopeGlobal)
	}
	return string(resolved.Kind)
}

type pendingApprovalView struct {
	TaskKey    string
	Status     string
	TaskScope  string
	ProjectKey string
}

func (service Service) pendingApprovals(ctx context.Context) ([]pendingApprovalView, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("conversation store is required")
	}

	rows, err := service.Store.DB().QueryContext(ctx, `
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

	var approvals []pendingApprovalView
	for rows.Next() {
		var approval pendingApprovalView
		if err := rows.Scan(&approval.TaskKey, &approval.Status, &approval.TaskScope, &approval.ProjectKey); err != nil {
			return nil, err
		}
		approvals = append(approvals, approval)
	}

	return approvals, rows.Err()
}

func matchesScope(projectKey, taskScope string, resolved scope.Resolution) bool {
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

func toApprovalWaitingViews(views []projections.PendingApprovalView) []ApprovalWaitingView {
	result := make([]ApprovalWaitingView, 0, len(views))
	for _, view := range views {
		result = append(result, ApprovalWaitingView{
			ApprovalID:  view.ApprovalID,
			TaskID:      view.TaskID,
			TaskKey:     view.TaskKey,
			Status:      view.Status,
			RequestedAt: view.RequestedAt,
		})
	}
	return result
}

func toActiveRunViews(views []projections.ActiveRunView) []ActiveRunView {
	result := make([]ActiveRunView, 0, len(views))
	for _, view := range views {
		result = append(result, ActiveRunView{
			RunID:      view.RunID,
			TaskID:     view.TaskID,
			TaskKey:    view.TaskKey,
			ProjectKey: view.ProjectKey,
			Executor:   view.Executor,
			Status:     view.Status,
			Attempt:    view.Attempt,
			StartedAt:  view.StartedAt,
		})
	}
	return result
}

func toStalledRunViews(views []projections.StalledRunView) []StalledRunView {
	result := make([]StalledRunView, 0, len(views))
	for _, view := range views {
		result = append(result, StalledRunView{
			RunID:      view.RunID,
			TaskID:     view.TaskID,
			TaskKey:    view.TaskKey,
			ProjectKey: view.ProjectKey,
			Executor:   view.Executor,
			Status:     view.Status,
			Attempt:    view.Attempt,
			StartedAt:  view.StartedAt,
		})
	}
	return result
}

func toProjectTransitionViews(views []projections.ProjectTransitionView) []ProjectTransitionView {
	result := make([]ProjectTransitionView, 0, len(views))
	for _, view := range views {
		result = append(result, ProjectTransitionView{
			ProjectID:       view.ProjectID,
			ProjectKey:      view.ProjectKey,
			Name:            view.Name,
			Scope:           view.Scope,
			TaskCount:       view.TaskCount,
			OpenTaskCount:   view.OpenTaskCount,
			LastEventAt:     view.LastEventAt,
			TransitionState: view.TransitionState,
			Controller:      view.Controller,
			LastReportType:  view.LastReportType,
			LastReportAt:    view.LastReportAt,
		})
	}
	return result
}

func summarizeProjectTransitions(views []projections.ProjectTransitionView) ProjectTransitionOwnership {
	var summary ProjectTransitionOwnership
	for _, view := range views {
		switch view.Controller {
		case "legacy_odin":
			summary.LegacyOdin++
		case "odin_os":
			summary.OdinOS++
		default:
			summary.Unknown++
		}
	}
	return summary
}
