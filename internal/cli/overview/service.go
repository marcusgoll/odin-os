package overview

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/initiatives"
	coreprojects "odin-os/internal/core/projects"
	"odin-os/internal/core/workspaces"
	knowledgememory "odin-os/internal/memory/knowledge"
	"odin-os/internal/registry"
	approvalsvc "odin-os/internal/runtime/approvals"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
	toolcatalog "odin-os/internal/tools/catalog"
)

type Wiring string

const (
	WiringLive          Wiring = "live"
	WiringCatalogBacked Wiring = "catalog_backed"
	WiringNotYetWired   Wiring = "not_yet_wired"
)

type Service struct {
	Store            *sqlite.Store
	Registry         coreprojects.Registry
	RegistrySnapshot registry.Snapshot
}

type View struct {
	Workspace          WorkspaceLane
	Initiatives        []InitiativeSummary
	WorkItems          []WorkItemSummary
	CompanionSwarms    []CompanionSwarmSummary
	Companions         CompanionLane
	CapabilityCatalog  CapabilityCatalogLane
	Approvals          []ApprovalSummary
	Observability      ObservabilityLane
	Memory             MemoryLane
	IntakeInbox        PlaceholderLane
	AutomationTriggers PlaceholderLane
}

type WorkspaceLane struct {
	Wiring               Wiring
	WorkspaceKey         string
	Name                 string
	Status               string
	OwnerRef             string
	ControlScope         string
	DefaultCompanionKey  string
	InitiativeCount      int
	CompanionCount       int
	OpenWorkItemCount    int
	ActiveRunCount       int
	PendingApprovalCount int
	OpenIncidentCount    int
	BlockedWorkItemCount int
}

type InitiativeSummary struct {
	InitiativeKey        string
	Title                string
	Kind                 string
	Status               string
	Summary              string
	OwnerCompanionKey    *string
	LinkedProjectKey     *string
	OpenWorkItemCount    int
	ActiveRunCount       int
	PendingApprovalCount int
	OpenIncidentCount    int
	BlockedWorkItemCount int
}

type WorkItemSummary struct {
	ProjectKey       string
	InitiativeKey    *string
	CompanionKey     *string
	WorkItemKey      string
	Title            string
	Status           string
	Scope            string
	CurrentRunID     *int64
	CurrentRunStatus string
	RunAttempts      []RunAttemptSummary
}

type CompanionLane struct {
	Wiring Wiring
	Items  []CompanionSummary
}

type CompanionSummary struct {
	CompanionKey         string
	Title                string
	Kind                 string
	Status               string
	OwnedInitiativeCount int
	OpenWorkItemCount    int
	ActiveRunCount       int
	PendingApprovalCount int
	BlockedWorkItemCount int
}

type CapabilityCatalogLane struct {
	Wiring               Wiring
	AgentDefinitionCount int
	SkillCount           int
	WorkflowCount        int
	CommandCount         int
	ToolCount            int
}

type ApprovalSummary struct {
	ApprovalID      int64
	TaskID          int64
	RunID           *int64
	ProjectKey      string
	CompanionKey    *string
	WorkItemKey     string
	Status          string
	RequestedAt     string
	ResolverSupport string
}

type ObservabilityLane struct {
	Wiring      Wiring
	ActiveRuns  []RunAttemptSummary
	BlockedWork []BlockedWorkSummary
	Incidents   []IncidentSummary
	Recoveries  []RecoverySummary
	Freshness   []FreshnessSummary
}

type RunAttemptSummary struct {
	RunID         int64
	TaskID        int64
	WorkItemKey   string
	ProjectKey    string
	InitiativeKey *string
	CompanionKey  *string
	Executor      string
	Status        string
	Attempt       int
	StartedAt     string
}

type BlockedWorkSummary struct {
	TaskID        int64
	WorkItemKey   string
	ProjectKey    string
	WorkspaceKey  string
	InitiativeKey *string
	CompanionKey  *string
	WorkKind      string
	Source        string
	Reason        string
}

type IncidentSummary struct {
	IncidentID   int64
	RunID        int64
	TaskID       int64
	WorkItemKey  string
	ProjectKey   string
	CompanionKey *string
	Severity     string
	Status       string
	Summary      string
	OpenedAt     string
}

type CompanionSwarmSummary struct {
	ParentTaskID             int64
	ParentTaskKey            string
	ProjectKey               string
	WorkspaceKey             string
	InitiativeKey            *string
	CompanionKey             *string
	Title                    string
	Summary                  string
	Status                   string
	BlockedReason            string
	TerminalReason           string
	ConvergenceMode          string
	RequestedBudget          int
	DelegationCount          int
	CompletedDelegationCount int
	ActiveChildRunCount      int
	BacklogCount             int
	BudgetBacklogCount       int
}

type RecoverySummary struct {
	RecoveryID int64
	RunID      int64
	Status     string
	Strategy   string
	StartedAt  string
}

type FreshnessSummary struct {
	Surface     string
	Status      string
	RefreshedAt string
}

type MemoryLane struct {
	Wiring Wiring
	Count  int
	Recent []MemorySummary
}

type MemorySummary struct {
	ID         int64
	MemoryType string
	Scope      string
	ScopeKey   string
	Summary    string
	CreatedAt  string
}

type PlaceholderLane struct {
	Wiring Wiring
	Status string
	Note   string
}

func (service Service) Build(ctx context.Context, resolved scope.Resolution) (View, error) {
	if service.Store == nil {
		return View{}, fmt.Errorf("overview store is required")
	}

	view := View{
		Workspace: WorkspaceLane{
			Wiring:       WiringNotYetWired,
			ControlScope: controlScopeLabel(resolved),
		},
		Companions: CompanionLane{
			Wiring: WiringNotYetWired,
		},
		CapabilityCatalog: CapabilityCatalogLane{
			Wiring:    WiringCatalogBacked,
			ToolCount: len(toolcatalog.BuiltinDefinitions()),
		},
		Observability: ObservabilityLane{
			Wiring: WiringLive,
		},
		Memory: MemoryLane{
			Wiring: WiringLive,
		},
		IntakeInbox: PlaceholderLane{
			Wiring: WiringNotYetWired,
			Status: "unavailable",
			Note:   "intake overview projection not implemented",
		},
		AutomationTriggers: PlaceholderLane{
			Wiring: WiringNotYetWired,
			Status: "unavailable",
			Note:   "automation trigger overview projection not implemented",
		},
	}

	workspaceView, err := projections.GetWorkspaceOverviewView(ctx, service.Store.DB(), workspaces.DefaultWorkspaceKey)
	switch err {
	case nil:
		view.Workspace = WorkspaceLane{
			Wiring:               WiringLive,
			WorkspaceKey:         workspaceView.WorkspaceKey,
			Name:                 workspaceView.Name,
			Status:               workspaceView.Status,
			OwnerRef:             workspaceView.OwnerRef,
			ControlScope:         controlScopeLabel(resolved),
			DefaultCompanionKey:  workspaceView.DefaultCompanionKey,
			InitiativeCount:      workspaceView.ActiveInitiativeCount,
			CompanionCount:       workspaceView.ActiveCompanionCount,
			OpenWorkItemCount:    workspaceView.OpenWorkItemCount,
			ActiveRunCount:       workspaceView.ActiveRunCount,
			PendingApprovalCount: workspaceView.PendingApprovalCount,
			OpenIncidentCount:    workspaceView.OpenIncidentCount,
			BlockedWorkItemCount: workspaceView.BlockedWorkItemCount,
		}
	case sql.ErrNoRows:
	default:
		return View{}, err
	}

	initiativeViews, err := projections.ListInitiativePortfolioViews(ctx, service.Store.DB(), workspaces.DefaultWorkspaceKey)
	if err != nil && err != sql.ErrNoRows {
		return View{}, err
	}
	view.Initiatives = make([]InitiativeSummary, 0, len(initiativeViews))
	for _, initiative := range initiativeViews {
		if !matchesInitiativeScope(initiative, resolved) {
			continue
		}
		view.Initiatives = append(view.Initiatives, InitiativeSummary{
			InitiativeKey:        initiative.InitiativeKey,
			Title:                initiative.Title,
			Kind:                 initiative.Kind,
			Status:               initiative.Status,
			Summary:              initiative.Summary,
			OwnerCompanionKey:    initiative.OwnerCompanionKey,
			LinkedProjectKey:     initiative.LinkedProjectKey,
			OpenWorkItemCount:    initiative.OpenWorkItemCount,
			ActiveRunCount:       initiative.ActiveRunCount,
			PendingApprovalCount: initiative.PendingApprovalCount,
			OpenIncidentCount:    initiative.OpenIncidentCount,
			BlockedWorkItemCount: initiative.BlockedWorkItemCount,
		})
	}
	view.Initiatives = service.mergeRegistryInitiatives(view.Initiatives, resolved, view.Workspace.DefaultCompanionKey)
	if len(view.Initiatives) > view.Workspace.InitiativeCount {
		view.Workspace.InitiativeCount = len(view.Initiatives)
	}

	taskViews, err := projections.ListTaskStatusViews(ctx, service.Store.DB())
	if err != nil {
		return View{}, err
	}
	approvalViews, err := projections.ListPendingApprovalViews(ctx, service.Store.DB())
	if err != nil {
		return View{}, err
	}
	taskProjectIndex := make(map[int64]string, len(taskViews))
	for _, task := range taskViews {
		taskProjectIndex[task.TaskID] = task.ProjectKey
	}
	type taskScopeContext struct {
		projectKey    string
		initiativeKey *string
		companionKey  *string
	}
	taskContextCache := make(map[int64]taskScopeContext, len(taskViews))
	initiativeKeyCache := make(map[int64]string)
	companionKeyCache := make(map[int64]string)
	resolveTaskContext := func(taskID int64) (taskScopeContext, error) {
		if cached, ok := taskContextCache[taskID]; ok {
			return cached, nil
		}

		record, err := service.Store.GetTask(ctx, taskID)
		if err != nil {
			return taskScopeContext{}, err
		}

		resolvedContext := taskScopeContext{
			projectKey: taskProjectIndex[taskID],
		}
		if record.InitiativeID != nil {
			if key, ok := initiativeKeyCache[*record.InitiativeID]; ok {
				resolvedContext.initiativeKey = stringPtr(key)
			} else {
				initiative, err := service.Store.GetInitiativeByID(ctx, *record.InitiativeID)
				if err != nil {
					return taskScopeContext{}, err
				}
				initiativeKeyCache[*record.InitiativeID] = initiative.Key
				resolvedContext.initiativeKey = stringPtr(initiative.Key)
			}
		}
		if record.CompanionID != nil {
			if key, ok := companionKeyCache[*record.CompanionID]; ok {
				resolvedContext.companionKey = stringPtr(key)
			} else {
				companion, err := service.Store.GetCompanionByID(ctx, *record.CompanionID)
				if err != nil {
					return taskScopeContext{}, err
				}
				companionKeyCache[*record.CompanionID] = companion.Key
				resolvedContext.companionKey = stringPtr(companion.Key)
			}
		}

		taskContextCache[taskID] = resolvedContext
		return resolvedContext, nil
	}

	for _, approval := range approvalViews {
		if !matchesProjectScope(taskProjectIndex[approval.TaskID], resolved) {
			continue
		}
		taskContext, err := resolveTaskContext(approval.TaskID)
		if err != nil {
			return View{}, err
		}
		detail, err := approvalsvc.Service{Store: service.Store}.Detail(ctx, approval.ApprovalID)
		if err != nil {
			return View{}, err
		}
		view.Approvals = append(view.Approvals, ApprovalSummary{
			ApprovalID:      approval.ApprovalID,
			TaskID:          approval.TaskID,
			RunID:           detail.Approval.RunID,
			ProjectKey:      taskProjectIndex[approval.TaskID],
			CompanionKey:    taskContext.companionKey,
			WorkItemKey:     approval.TaskKey,
			Status:          approval.Status,
			RequestedAt:     approval.RequestedAt,
			ResolverSupport: string(detail.ResolverSupport),
		})
	}

	activeRunViews, err := projections.ListActiveRunViews(ctx, service.Store.DB())
	if err != nil {
		return View{}, err
	}
	runAttemptsByTaskID := make(map[int64][]RunAttemptSummary)
	for _, run := range activeRunViews {
		if !matchesProjectScope(run.ProjectKey, resolved) {
			continue
		}
		taskContext, err := resolveTaskContext(run.TaskID)
		if err != nil {
			return View{}, err
		}
		summary := RunAttemptSummary{
			RunID:         run.RunID,
			TaskID:        run.TaskID,
			WorkItemKey:   run.TaskKey,
			ProjectKey:    run.ProjectKey,
			InitiativeKey: taskContext.initiativeKey,
			CompanionKey:  taskContext.companionKey,
			Executor:      run.Executor,
			Status:        run.Status,
			Attempt:       run.Attempt,
			StartedAt:     run.StartedAt,
		}
		view.Observability.ActiveRuns = append(view.Observability.ActiveRuns, summary)
	}

	runSummaryViews, err := projections.ListRunSummaryViews(ctx, service.Store.DB())
	if err != nil {
		return View{}, err
	}
	for _, run := range runSummaryViews {
		projectKey := taskProjectIndex[run.TaskID]
		if !matchesProjectScope(projectKey, resolved) {
			continue
		}
		taskContext, err := resolveTaskContext(run.TaskID)
		if err != nil {
			return View{}, err
		}
		runAttemptsByTaskID[run.TaskID] = append(runAttemptsByTaskID[run.TaskID], RunAttemptSummary{
			RunID:         run.RunID,
			TaskID:        run.TaskID,
			WorkItemKey:   run.TaskKey,
			ProjectKey:    projectKey,
			InitiativeKey: taskContext.initiativeKey,
			CompanionKey:  taskContext.companionKey,
			Executor:      run.Executor,
			Status:        run.Status,
			Attempt:       run.Attempt,
			StartedAt:     run.StartedAt,
		})
	}

	visibleCompanionKeys := make(map[string]struct{})
	for _, initiative := range view.Initiatives {
		if initiative.OwnerCompanionKey != nil {
			visibleCompanionKeys[*initiative.OwnerCompanionKey] = struct{}{}
		}
	}
	for _, task := range taskViews {
		if !matchesProjectScope(task.ProjectKey, resolved) || isClosedWorkItemStatus(task.Status) {
			continue
		}
		taskContext, err := resolveTaskContext(task.TaskID)
		if err != nil {
			return View{}, err
		}
		if taskContext.companionKey != nil {
			visibleCompanionKeys[*taskContext.companionKey] = struct{}{}
		}
		view.WorkItems = append(view.WorkItems, WorkItemSummary{
			ProjectKey:       task.ProjectKey,
			InitiativeKey:    taskContext.initiativeKey,
			CompanionKey:     taskContext.companionKey,
			WorkItemKey:      task.TaskKey,
			Title:            task.Title,
			Status:           task.Status,
			Scope:            task.Scope,
			CurrentRunID:     task.CurrentRunID,
			CurrentRunStatus: task.CurrentRunStatus,
			RunAttempts:      append([]RunAttemptSummary(nil), runAttemptsByTaskID[task.TaskID]...),
		})
	}

	blockedViews, err := projections.ListBlockedItemViews(ctx, service.Store.DB())
	if err != nil {
		return View{}, err
	}
	for _, blocked := range blockedViews {
		if !matchesProjectScope(blocked.ProjectKey, resolved) {
			continue
		}
		if blocked.CompanionKey != nil {
			visibleCompanionKeys[*blocked.CompanionKey] = struct{}{}
		}
		view.Observability.BlockedWork = append(view.Observability.BlockedWork, BlockedWorkSummary{
			TaskID:        blocked.TaskID,
			WorkItemKey:   blocked.TaskKey,
			ProjectKey:    blocked.ProjectKey,
			WorkspaceKey:  blocked.WorkspaceKey,
			InitiativeKey: blocked.InitiativeKey,
			CompanionKey:  blocked.CompanionKey,
			WorkKind:      blocked.WorkKind,
			Source:        blocked.Source,
			Reason:        blocked.Reason,
		})
	}

	incidentViews, err := projections.ListIncidentViews(ctx, service.Store.DB())
	if err != nil {
		return View{}, err
	}
	incidentProjectIndex := make(map[int64]string)
	for _, incident := range incidentViews {
		incidentProjectIndex[incident.IncidentID] = incident.ProjectKey
		if !matchesProjectScope(incident.ProjectKey, resolved) {
			continue
		}
		taskContext, err := resolveTaskContext(incident.TaskID)
		if err != nil {
			return View{}, err
		}
		view.Observability.Incidents = append(view.Observability.Incidents, IncidentSummary{
			IncidentID:   incident.IncidentID,
			RunID:        incident.RunID,
			TaskID:       incident.TaskID,
			WorkItemKey:  incident.TaskKey,
			ProjectKey:   incident.ProjectKey,
			CompanionKey: taskContext.companionKey,
			Severity:     incident.Severity,
			Status:       incident.Status,
			Summary:      incident.Summary,
			OpenedAt:     incident.OpenedAt,
		})
	}

	companionSwarmViews, err := projections.ListCompanionSwarmViews(ctx, service.Store.DB(), workspaces.DefaultWorkspaceKey)
	if err != nil && err != sql.ErrNoRows {
		return View{}, err
	}
	for _, swarm := range companionSwarmViews {
		if !matchesProjectScope(swarm.ProjectKey, resolved) {
			continue
		}
		if swarm.CompanionKey != nil {
			visibleCompanionKeys[*swarm.CompanionKey] = struct{}{}
		}
		view.CompanionSwarms = append(view.CompanionSwarms, CompanionSwarmSummary{
			ParentTaskID:             swarm.ParentTaskID,
			ParentTaskKey:            swarm.ParentTaskKey,
			ProjectKey:               swarm.ProjectKey,
			WorkspaceKey:             swarm.WorkspaceKey,
			InitiativeKey:            swarm.InitiativeKey,
			CompanionKey:             swarm.CompanionKey,
			Title:                    swarm.Title,
			Summary:                  swarm.Summary,
			Status:                   swarm.Status,
			BlockedReason:            swarm.BlockedReason,
			TerminalReason:           swarm.TerminalReason,
			ConvergenceMode:          swarm.ConvergenceMode,
			RequestedBudget:          swarm.RequestedBudget,
			DelegationCount:          swarm.DelegationCount,
			CompletedDelegationCount: swarm.CompletedDelegationCount,
			ActiveChildRunCount:      swarm.ActiveChildRunCount,
			BacklogCount:             swarm.BacklogCount,
			BudgetBacklogCount:       swarm.BudgetBacklogCount,
		})
	}

	companionViews, err := projections.ListCompanionAssignmentViews(ctx, service.Store.DB(), workspaces.DefaultWorkspaceKey)
	if err != nil && err != sql.ErrNoRows {
		return View{}, err
	}
	view.Companions.Wiring = WiringLive
	view.Companions.Items = make([]CompanionSummary, 0, len(companionViews))
	for _, companion := range companionViews {
		if !matchesCompanionScope(companion.CompanionKey, resolved, visibleCompanionKeys) {
			continue
		}
		view.Companions.Items = append(view.Companions.Items, CompanionSummary{
			CompanionKey:         companion.CompanionKey,
			Title:                companion.Title,
			Kind:                 companion.Kind,
			Status:               companion.Status,
			OwnedInitiativeCount: companion.OwnedInitiativeCount,
			OpenWorkItemCount:    companion.OpenWorkItemCount,
			ActiveRunCount:       companion.ActiveRunCount,
			PendingApprovalCount: companion.PendingApprovalCount,
			BlockedWorkItemCount: companion.BlockedWorkItemCount,
		})
	}

	recoveryViews, err := projections.ListRecoveryViews(ctx, service.Store.DB())
	if err != nil {
		return View{}, err
	}
	runTaskIDCache := make(map[int64]int64)
	for _, recovery := range recoveryViews {
		recoveryProjectKey := ""
		if recovery.RunID != 0 {
			taskID, ok := runTaskIDCache[recovery.RunID]
			if !ok {
				runRecord, err := service.Store.GetRun(ctx, recovery.RunID)
				if err != nil {
					if err == sql.ErrNoRows {
						continue
					}
					return View{}, err
				}
				taskID = runRecord.TaskID
				runTaskIDCache[recovery.RunID] = taskID
			}
			taskContext, err := resolveTaskContext(taskID)
			if err != nil {
				return View{}, err
			}
			recoveryProjectKey = taskContext.projectKey
		} else {
			recoveryRecord, err := service.Store.GetRecovery(ctx, recovery.RecoveryID)
			if err != nil {
				if err == sql.ErrNoRows {
					continue
				}
				return View{}, err
			}
			if recoveryRecord.IncidentID == nil {
				continue
			}
			recoveryProjectKey = incidentProjectIndex[*recoveryRecord.IncidentID]
		}
		if !matchesProjectScope(recoveryProjectKey, resolved) {
			continue
		}
		view.Observability.Recoveries = append(view.Observability.Recoveries, RecoverySummary{
			RecoveryID: recovery.RecoveryID,
			RunID:      recovery.RunID,
			Status:     recovery.Status,
			Strategy:   recovery.Strategy,
			StartedAt:  recovery.StartedAt,
		})
	}

	freshnessViews, err := projections.ListFreshnessViews(ctx, service.Store.DB())
	if err != nil {
		return View{}, err
	}
	for _, freshness := range freshnessViews {
		view.Observability.Freshness = append(view.Observability.Freshness, FreshnessSummary{
			Surface:     freshness.Surface,
			Status:      freshness.Status,
			RefreshedAt: freshness.RefreshedAt,
		})
	}

	memoryScope, err := service.memoryScope(ctx, resolved)
	if err != nil {
		return View{}, err
	}
	memorySummaries, err := knowledgememory.Service{Store: service.Store}.List(ctx, memoryScope, "")
	if err != nil {
		return View{}, err
	}
	sort.Slice(memorySummaries, func(i int, j int) bool {
		return memorySummaries[i].CreatedAt.After(memorySummaries[j].CreatedAt)
	})
	view.Memory.Count = len(memorySummaries)
	limit := len(memorySummaries)
	if limit > 5 {
		limit = 5
	}
	view.Memory.Recent = make([]MemorySummary, 0, limit)
	for _, summary := range memorySummaries[:limit] {
		view.Memory.Recent = append(view.Memory.Recent, MemorySummary{
			ID:         summary.ID,
			MemoryType: summary.MemoryType,
			Scope:      summary.Scope,
			ScopeKey:   summary.ScopeKey,
			Summary:    summary.Summary,
			CreatedAt:  summary.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	for _, item := range service.RegistrySnapshot.Items {
		switch item.Kind {
		case registry.KindAgent:
			view.CapabilityCatalog.AgentDefinitionCount++
		case registry.KindSkill:
			view.CapabilityCatalog.SkillCount++
		case registry.KindWorkflow:
			view.CapabilityCatalog.WorkflowCount++
		case registry.KindCommand:
			view.CapabilityCatalog.CommandCount++
		}
	}

	return view, nil
}

func (service Service) memoryScope(ctx context.Context, resolved scope.Resolution) (knowledgememory.Scope, error) {
	switch resolved.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		project, err := service.Store.GetProjectByKey(ctx, resolved.ProjectKey)
		if err != nil {
			if err == sql.ErrNoRows {
				return knowledgememory.Scope{Value: "global", Key: "global"}, nil
			}
			return knowledgememory.Scope{}, err
		}
		return knowledgememory.Scope{
			ProjectID: &project.ID,
			Value:     string(resolved.Kind),
			Key:       project.Key,
		}, nil
	case scope.ScopeNewProject:
		return knowledgememory.Scope{Value: "new-project", Key: "new-project"}, nil
	default:
		return knowledgememory.Scope{Value: "global", Key: "global"}, nil
	}
}

func controlScopeLabel(resolved scope.Resolution) string {
	switch resolved.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		if strings.TrimSpace(resolved.ProjectKey) != "" {
			return resolved.ProjectKey
		}
	}
	return string(resolved.Kind)
}

func matchesProjectScope(projectKey string, resolved scope.Resolution) bool {
	switch resolved.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		return projectKey == resolved.ProjectKey
	case scope.ScopeNewProject:
		return false
	default:
		return true
	}
}

func matchesInitiativeScope(initiative projections.InitiativePortfolioView, resolved scope.Resolution) bool {
	switch resolved.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		if initiative.LinkedProjectKey != nil && *initiative.LinkedProjectKey == resolved.ProjectKey {
			return true
		}
		return initiative.InitiativeKey == resolved.ProjectKey
	case scope.ScopeNewProject:
		return false
	default:
		return true
	}
}

func (service Service) mergeRegistryInitiatives(existing []InitiativeSummary, resolved scope.Resolution, defaultCompanionKey string) []InitiativeSummary {
	projects := service.Registry.Projects()
	if len(projects) == 0 {
		return existing
	}

	seen := make(map[string]struct{}, len(existing)*2)
	for _, initiative := range existing {
		seen[initiative.InitiativeKey] = struct{}{}
		if initiative.LinkedProjectKey != nil {
			seen[*initiative.LinkedProjectKey] = struct{}{}
		}
	}

	merged := append([]InitiativeSummary(nil), existing...)
	for _, project := range projects {
		if !matchesRegistryProjectScope(project.Key, resolved) {
			continue
		}
		if _, ok := seen[project.Key]; ok {
			continue
		}

		linkedProjectKey := project.Key
		var ownerCompanionKey *string
		if strings.TrimSpace(defaultCompanionKey) != "" {
			ownerCompanionKey = stringPtr(defaultCompanionKey)
		}
		merged = append(merged, InitiativeSummary{
			InitiativeKey:     project.Key,
			Title:             project.Name,
			Kind:              string(initiatives.KindManagedProject),
			Status:            "active",
			OwnerCompanionKey: ownerCompanionKey,
			LinkedProjectKey:  &linkedProjectKey,
		})
		seen[project.Key] = struct{}{}
	}
	return merged
}

func matchesRegistryProjectScope(projectKey string, resolved scope.Resolution) bool {
	switch resolved.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		return projectKey == resolved.ProjectKey
	case scope.ScopeNewProject:
		return false
	default:
		return true
	}
}

func matchesCompanionScope(companionKey string, resolved scope.Resolution, visible map[string]struct{}) bool {
	switch resolved.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		_, ok := visible[companionKey]
		return ok
	case scope.ScopeNewProject:
		return false
	default:
		return true
	}
}

func stringPtr(value string) *string {
	return &value
}

func isClosedWorkItemStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "completed", "cancelled", "failed":
		return true
	default:
		return false
	}
}
