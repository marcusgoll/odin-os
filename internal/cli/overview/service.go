package overview

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/initiatives"
	coreprojects "odin-os/internal/core/projects"
	"odin-os/internal/core/workspaces"
	knowledgememory "odin-os/internal/memory/knowledge"
	"odin-os/internal/registry"
	approvalsvc "odin-os/internal/runtime/approvals"
	runtimeevents "odin-os/internal/runtime/events"
	runtimeknowledge "odin-os/internal/runtime/knowledge"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/runtime/recovery"
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
	Now              func() time.Time
}

type View struct {
	Workspace             WorkspaceLane            `json:"workspace"`
	Initiatives           []InitiativeSummary      `json:"initiatives"`
	WorkItems             []WorkItemSummary        `json:"work_items"`
	CompanionSwarms       []CompanionSwarmSummary  `json:"companion_swarms"`
	Companions            CompanionLane            `json:"companions"`
	CapabilityCatalog     CapabilityCatalogLane    `json:"capability_catalog"`
	SkillActivity         SkillActivityLane        `json:"skill_activity"`
	ReviewQueue           ReviewQueueLane          `json:"review_queue"`
	DelegationTruth       DelegationTruthLane      `json:"delegation_truth"`
	Approvals             []ApprovalSummary        `json:"approvals"`
	Observability         ObservabilityLane        `json:"observability"`
	Memory                MemoryLane               `json:"memory"`
	KnowledgeContextPacks KnowledgeContextPackLane `json:"knowledge_context_packs"`
	IntakeInbox           IntakeInboxLane          `json:"intake_inbox"`
	AutomationTriggers    AutomationTriggerLane    `json:"automation_triggers"`
}

type WorkspaceLane struct {
	Wiring               Wiring `json:"wiring"`
	WorkspaceKey         string `json:"workspace_key"`
	Name                 string `json:"name"`
	Status               string `json:"status"`
	OwnerRef             string `json:"owner_ref"`
	ControlScope         string `json:"control_scope"`
	DefaultCompanionKey  string `json:"default_companion_key"`
	InitiativeCount      int    `json:"initiative_count"`
	CompanionCount       int    `json:"companion_count"`
	OpenWorkItemCount    int    `json:"open_work_item_count"`
	ActiveRunCount       int    `json:"active_run_count"`
	PendingApprovalCount int    `json:"pending_approval_count"`
	OpenIncidentCount    int    `json:"open_incident_count"`
	BlockedWorkItemCount int    `json:"blocked_work_item_count"`
}

type InitiativeSummary struct {
	InitiativeKey        string  `json:"initiative_key"`
	Title                string  `json:"title"`
	Kind                 string  `json:"kind"`
	Status               string  `json:"status"`
	Summary              string  `json:"summary"`
	OwnerCompanionKey    *string `json:"owner_companion_key"`
	LinkedProjectKey     *string `json:"linked_project_key"`
	OpenWorkItemCount    int     `json:"open_work_item_count"`
	ActiveRunCount       int     `json:"active_run_count"`
	PendingApprovalCount int     `json:"pending_approval_count"`
	OpenIncidentCount    int     `json:"open_incident_count"`
	BlockedWorkItemCount int     `json:"blocked_work_item_count"`
}

type WorkItemSummary struct {
	ProjectKey            string              `json:"project_key"`
	InitiativeKey         *string             `json:"initiative_key"`
	CompanionKey          *string             `json:"companion_key"`
	WorkItemKey           string              `json:"work_item_key"`
	Title                 string              `json:"title"`
	Status                string              `json:"status"`
	Scope                 string              `json:"scope"`
	BlockedReason         string              `json:"blocked_reason,omitempty"`
	ExecutionIntent       string              `json:"execution_intent,omitempty"`
	ExecutionIntentSource string              `json:"execution_intent_source,omitempty"`
	CurrentRunID          *int64              `json:"current_run_id"`
	CurrentRunStatus      string              `json:"current_run_status"`
	RunAttempts           []RunAttemptSummary `json:"run_attempts"`
}

type CompanionLane struct {
	Wiring Wiring             `json:"wiring"`
	Items  []CompanionSummary `json:"items"`
}

type CompanionSummary struct {
	CompanionKey         string `json:"companion_key"`
	Title                string `json:"title"`
	Kind                 string `json:"kind"`
	Status               string `json:"status"`
	OwnedInitiativeCount int    `json:"owned_initiative_count"`
	OpenWorkItemCount    int    `json:"open_work_item_count"`
	ActiveRunCount       int    `json:"active_run_count"`
	PendingApprovalCount int    `json:"pending_approval_count"`
	BlockedWorkItemCount int    `json:"blocked_work_item_count"`
}

type CapabilityCatalogLane struct {
	Wiring               Wiring `json:"wiring"`
	AgentDefinitionCount int    `json:"agent_definition_count"`
	SkillCount           int    `json:"skill_count"`
	WorkflowCount        int    `json:"workflow_count"`
	CommandCount         int    `json:"command_count"`
	ToolCount            int    `json:"tool_count"`
}

type SkillActivityLane struct {
	Wiring                         Wiring                 `json:"wiring"`
	InvokeSuccessCount             int                    `json:"invoke_success_count"`
	InvokeFailureCount             int                    `json:"invoke_failure_count"`
	StubResultCount                int                    `json:"stub_result_count"`
	CommandOutputOnlyCount         int                    `json:"command_output_only_count"`
	DurableReviewableArtifactCount int                    `json:"durable_reviewable_artifact_count"`
	ReviewRequiredArtifactCount    int                    `json:"review_required_artifact_count"`
	AcceptedArtifactCount          int                    `json:"accepted_artifact_count"`
	RejectedArtifactCount          int                    `json:"rejected_artifact_count"`
	ArchivedArtifactCount          int                    `json:"archived_artifact_count"`
	Recent                         []SkillActivitySummary `json:"recent"`
}

type SkillActivitySummary struct {
	EventID          int64    `json:"event_id"`
	SkillKey         string   `json:"skill_key"`
	Scope            string   `json:"scope"`
	Operation        string   `json:"operation"`
	Outcome          string   `json:"outcome"`
	ExecutionProfile string   `json:"execution_profile"`
	RuntimeEffect    string   `json:"runtime_effect"`
	HandlerRef       string   `json:"handler_ref"`
	Permissions      []string `json:"permissions"`
	ErrorCode        string   `json:"error_code,omitempty"`
	OccurredAt       string   `json:"occurred_at"`
}

type ReviewQueueLane struct {
	Wiring             Wiring `json:"wiring"`
	TotalCount         int    `json:"total_count"`
	IntakeCount        int    `json:"intake_count"`
	ApprovalCount      int    `json:"approval_count"`
	KnowledgeCount     int    `json:"knowledge_count"`
	SkillArtifactCount int    `json:"skill_artifact_count"`
	FailedWorkCount    int    `json:"failed_work_count"`
}

type KnowledgeContextPackLane struct {
	Wiring              Wiring                        `json:"wiring"`
	ReviewRequiredCount int                           `json:"review_required_count"`
	AcceptedCount       int                           `json:"accepted_count"`
	RejectedCount       int                           `json:"rejected_count"`
	ArchivedCount       int                           `json:"archived_count"`
	Recent              []KnowledgeContextPackSummary `json:"recent"`
}

type KnowledgeContextPackSummary struct {
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	ProjectKey string `json:"project_key,omitempty"`
	TaskKey    string `json:"task_key,omitempty"`
	Summary    string `json:"summary"`
	CreatedAt  string `json:"created_at"`
}

type DelegationTruthLane struct {
	Wiring              Wiring `json:"wiring"`
	RuntimeStatus       string `json:"runtime_status"`
	OperatorSurface     string `json:"operator_surface"`
	CompanionWorkPath   string `json:"companion_work_path"`
	CompanionSwarmCount int    `json:"companion_swarm_count"`
	Note                string `json:"note"`
}

type ApprovalSummary struct {
	ApprovalID      int64   `json:"approval_id"`
	TaskID          int64   `json:"task_id"`
	RunID           *int64  `json:"run_id"`
	ProjectKey      string  `json:"project_key"`
	CompanionKey    *string `json:"companion_key"`
	WorkItemKey     string  `json:"work_item_key"`
	Status          string  `json:"status"`
	RequestedAt     string  `json:"requested_at"`
	ResolverSupport string  `json:"resolver_support"`
}

type ObservabilityLane struct {
	Wiring           Wiring                         `json:"wiring"`
	ActivityLog      []ActivityEventSummary         `json:"activity_log"`
	ActiveRuns       []RunAttemptSummary            `json:"active_runs"`
	BlockedWork      []BlockedWorkSummary           `json:"blocked_work"`
	RecoveryGuidance []RetryRecoveryGuidanceSummary `json:"recovery_guidance"`
	Incidents        []IncidentSummary              `json:"incidents"`
	Recoveries       []RecoverySummary              `json:"recoveries"`
	Freshness        []FreshnessSummary             `json:"freshness"`
}

type ActivityEventSummary struct {
	EventID     int64           `json:"event_id"`
	StreamType  string          `json:"stream_type"`
	StreamID    int64           `json:"stream_id"`
	EventType   string          `json:"event_type"`
	Scope       string          `json:"scope"`
	ProjectID   *int64          `json:"project_id,omitempty"`
	ProjectKey  string          `json:"project_key,omitempty"`
	TaskID      *int64          `json:"task_id,omitempty"`
	WorkItemKey string          `json:"work_item_key,omitempty"`
	RunID       *int64          `json:"run_id,omitempty"`
	ApprovalID  *int64          `json:"approval_id,omitempty"`
	OccurredAt  string          `json:"occurred_at"`
	Summary     string          `json:"summary"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

type RunAttemptSummary struct {
	RunID         int64   `json:"run_id"`
	TaskID        int64   `json:"task_id"`
	WorkItemKey   string  `json:"work_item_key"`
	ProjectKey    string  `json:"project_key"`
	InitiativeKey *string `json:"initiative_key"`
	CompanionKey  *string `json:"companion_key"`
	Executor      string  `json:"executor"`
	Status        string  `json:"status"`
	Attempt       int     `json:"attempt"`
	StartedAt     string  `json:"started_at"`
}

type BlockedWorkSummary struct {
	TaskID        int64   `json:"task_id"`
	WorkItemKey   string  `json:"work_item_key"`
	ProjectKey    string  `json:"project_key"`
	WorkspaceKey  string  `json:"workspace_key"`
	InitiativeKey *string `json:"initiative_key"`
	CompanionKey  *string `json:"companion_key"`
	WorkKind      string  `json:"work_kind"`
	Source        string  `json:"source"`
	Reason        string  `json:"reason"`
}

type RetryRecoveryGuidanceSummary struct {
	TaskID                 int64   `json:"task_id"`
	WorkItemKey            string  `json:"work_item_key"`
	ProjectKey             string  `json:"project_key"`
	InitiativeKey          *string `json:"initiative_key"`
	CompanionKey           *string `json:"companion_key"`
	WorkKind               string  `json:"work_kind,omitempty"`
	Source                 string  `json:"source,omitempty"`
	Status                 string  `json:"status"`
	Decision               string  `json:"decision"`
	RetryEligible          bool    `json:"retry_eligible"`
	RetryCount             int     `json:"retry_count"`
	MaxAttempts            int     `json:"max_attempts"`
	LastError              string  `json:"last_error,omitempty"`
	RecoveryRecommendation string  `json:"recovery_recommendation"`
}

type IncidentSummary struct {
	IncidentID   int64   `json:"incident_id"`
	RunID        int64   `json:"run_id"`
	TaskID       int64   `json:"task_id"`
	WorkItemKey  string  `json:"work_item_key"`
	ProjectKey   string  `json:"project_key"`
	CompanionKey *string `json:"companion_key"`
	Severity     string  `json:"severity"`
	Status       string  `json:"status"`
	Summary      string  `json:"summary"`
	FaultKey     string  `json:"fault_key,omitempty"`
	SubjectKey   string  `json:"subject_key,omitempty"`
	DecisionMode string  `json:"decision_mode,omitempty"`
	NextAction   string  `json:"next_action,omitempty"`
	OpenedAt     string  `json:"opened_at"`
}

type CompanionSwarmSummary struct {
	ParentTaskID             int64   `json:"parent_task_id"`
	ParentTaskKey            string  `json:"parent_task_key"`
	ProjectKey               string  `json:"project_key"`
	WorkspaceKey             string  `json:"workspace_key"`
	InitiativeKey            *string `json:"initiative_key"`
	CompanionKey             *string `json:"companion_key"`
	Title                    string  `json:"title"`
	Summary                  string  `json:"summary"`
	Status                   string  `json:"status"`
	BlockedReason            string  `json:"blocked_reason"`
	TerminalReason           string  `json:"terminal_reason"`
	ConvergenceMode          string  `json:"convergence_mode"`
	RequestedBudget          int     `json:"requested_budget"`
	DelegationCount          int     `json:"delegation_count"`
	CompletedDelegationCount int     `json:"completed_delegation_count"`
	ActiveChildRunCount      int     `json:"active_child_run_count"`
	BacklogCount             int     `json:"backlog_count"`
	BudgetBacklogCount       int     `json:"budget_backlog_count"`
}

type RecoverySummary struct {
	RecoveryID   int64  `json:"recovery_id"`
	RunID        int64  `json:"run_id"`
	Status       string `json:"status"`
	Strategy     string `json:"strategy"`
	FaultKey     string `json:"fault_key,omitempty"`
	SubjectKey   string `json:"subject_key,omitempty"`
	DecisionMode string `json:"decision_mode,omitempty"`
	ActionName   string `json:"action_name,omitempty"`
	NextAction   string `json:"next_action,omitempty"`
	StartedAt    string `json:"started_at"`
}

type FreshnessSummary struct {
	Surface     string `json:"surface"`
	Status      string `json:"status"`
	RefreshedAt string `json:"refreshed_at"`
}

type MemoryLane struct {
	Wiring Wiring          `json:"wiring"`
	Count  int             `json:"count"`
	Recent []MemorySummary `json:"recent"`
}

type MemorySummary struct {
	ID         int64  `json:"id"`
	MemoryType string `json:"memory_type"`
	Scope      string `json:"scope"`
	ScopeKey   string `json:"scope_key"`
	Summary    string `json:"summary"`
	CreatedAt  string `json:"created_at"`
}

type PlaceholderLane struct {
	Wiring Wiring `json:"wiring"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

type IntakeInboxLane struct {
	Wiring                      Wiring                  `json:"wiring"`
	Source                      string                  `json:"source"`
	Status                      string                  `json:"status"`
	Note                        string                  `json:"note"`
	RawItemCount                int                     `json:"raw_item_count"`
	RawProcessedCount           int                     `json:"raw_processed_count"`
	ReviewRequiredCount         int                     `json:"review_required_count"`
	NeedsClarificationCount     int                     `json:"needs_clarification_count"`
	DuplicateLinkedCount        int                     `json:"duplicate_linked_or_suppressed_count"`
	ReviewQueueCount            int                     `json:"review_queue_count"`
	IntakeApprovalRequiredCount int                     `json:"intake_approval_required_count"`
	AcceptedCount               int                     `json:"accepted_count"`
	RejectedCount               int                     `json:"rejected_count"`
	ArchivedCount               int                     `json:"archived_count"`
	ApprovalDeniedCount         int                     `json:"approval_denied_count"`
	RawItems                    []RawIntakeSummary      `json:"raw_items"`
	Items                       []IntakeEvidenceSummary `json:"items"`
}

type RawIntakeSummary struct {
	ID          int64  `json:"id"`
	Key         string `json:"key"`
	ProjectKey  string `json:"project_key,omitempty"`
	Source      string `json:"source"`
	IntakeType  string `json:"intake_type"`
	DedupKey    string `json:"dedup_key"`
	RequestedBy string `json:"requested_by,omitempty"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Summary     string `json:"summary,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type IntakeEvidenceSummary struct {
	IntakeID       int64   `json:"intake_id"`
	TaskID         int64   `json:"task_id"`
	WorkspaceKey   string  `json:"workspace_key"`
	ProjectKey     string  `json:"project_key"`
	InitiativeKey  *string `json:"initiative_key"`
	CompanionKey   *string `json:"companion_key"`
	WorkItemKey    string  `json:"work_item_key"`
	WorkItemStatus string  `json:"work_item_status"`
	Source         string  `json:"source"`
	IntakeType     string  `json:"intake_type"`
	DedupKey       string  `json:"dedup_key"`
	RequestedBy    string  `json:"requested_by"`
	CreatedAt      string  `json:"created_at"`
}

type AutomationTriggerLane struct {
	Wiring            Wiring                     `json:"wiring"`
	TriggerCount      int                        `json:"trigger_count"`
	EnabledCount      int                        `json:"enabled_count"`
	MaterializedCount int                        `json:"materialized_count"`
	Items             []AutomationTriggerSummary `json:"items"`
}

type AutomationTriggerSummary struct {
	Source                 string  `json:"source"`
	TriggerID              int64   `json:"trigger_id"`
	Key                    string  `json:"key"`
	WorkspaceKey           string  `json:"workspace_key"`
	InitiativeKey          *string `json:"initiative_key"`
	CompanionKey           *string `json:"companion_key"`
	TargetProjectKey       string  `json:"target_project_key"`
	Title                  string  `json:"title"`
	Kind                   string  `json:"kind"`
	Status                 string  `json:"status"`
	DueStatus              string  `json:"due_status"`
	NextDueAt              string  `json:"next_due_at"`
	LastCompletedAt        *string `json:"last_completed_at"`
	LastMaterializedAt     *string `json:"last_materialized_at,omitempty"`
	LastMaterializationKey string  `json:"last_materialization_key,omitempty"`
	LastWorkItemKey        string  `json:"last_work_item_key,omitempty"`
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
		SkillActivity: SkillActivityLane{
			Wiring: WiringLive,
		},
		ReviewQueue: ReviewQueueLane{
			Wiring: WiringLive,
		},
		DelegationTruth: DelegationTruthLane{
			Wiring:            WiringLive,
			RuntimeStatus:     "not_proven",
			OperatorSurface:   "companion delegate",
			CompanionWorkPath: "governed_work_items",
			Note:              "companion delegate is the operator path; no live delegation evidence is present in this scope yet",
		},
		Observability: ObservabilityLane{
			Wiring: WiringLive,
		},
		Memory: MemoryLane{
			Wiring: WiringLive,
		},
		KnowledgeContextPacks: KnowledgeContextPackLane{
			Wiring: WiringLive,
		},
		IntakeInbox: IntakeInboxLane{
			Wiring: WiringLive,
			Source: "intake_items",
			Status: "empty",
			Note:   "governed intake items and linked task intake evidence are summarized from the runtime store",
		},
		AutomationTriggers: AutomationTriggerLane{
			Wiring: WiringNotYetWired,
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
		if !matchesProjectScope(task.ProjectKey, resolved) {
			continue
		}
		taskContext, err := resolveTaskContext(task.TaskID)
		if err != nil {
			return View{}, err
		}
		if taskContext.companionKey != nil {
			visibleCompanionKeys[*taskContext.companionKey] = struct{}{}
		}
		if strings.EqualFold(strings.TrimSpace(task.Status), "failed") {
			guidance := recovery.RetryGuidanceForTask(recovery.RetryGuidanceInput{
				RetryCount:  task.RetryCount,
				MaxAttempts: task.MaxAttempts,
				WorkKind:    task.WorkKind,
				RequestedBy: task.RequestedBy,
			})
			view.Observability.RecoveryGuidance = append(view.Observability.RecoveryGuidance, RetryRecoveryGuidanceSummary{
				TaskID:                 task.TaskID,
				WorkItemKey:            task.TaskKey,
				ProjectKey:             task.ProjectKey,
				InitiativeKey:          taskContext.initiativeKey,
				CompanionKey:           taskContext.companionKey,
				WorkKind:               task.WorkKind,
				Source:                 guidance.Source,
				Status:                 task.Status,
				Decision:               guidance.Decision,
				RetryEligible:          guidance.RetryEligible,
				RetryCount:             task.RetryCount,
				MaxAttempts:            task.MaxAttempts,
				LastError:              task.LastError,
				RecoveryRecommendation: guidance.RecoveryRecommendation,
			})
		}
		if isClosedWorkItemStatus(task.Status) {
			continue
		}
		view.WorkItems = append(view.WorkItems, WorkItemSummary{
			ProjectKey:            task.ProjectKey,
			InitiativeKey:         taskContext.initiativeKey,
			CompanionKey:          taskContext.companionKey,
			WorkItemKey:           task.TaskKey,
			Title:                 task.Title,
			Status:                task.Status,
			Scope:                 task.Scope,
			BlockedReason:         task.BlockedReason,
			ExecutionIntent:       task.ExecutionIntent,
			ExecutionIntentSource: task.ExecutionIntentSource,
			CurrentRunID:          task.CurrentRunID,
			CurrentRunStatus:      task.CurrentRunStatus,
			RunAttempts:           append([]RunAttemptSummary(nil), runAttemptsByTaskID[task.TaskID]...),
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
		taskContext := taskScopeContext{}
		if incident.TaskID != 0 {
			var err error
			taskContext, err = resolveTaskContext(incident.TaskID)
			if err != nil {
				return View{}, err
			}
		}
		evidence := decodeRecoveryEvidence(incident.DetailsJSON)
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
			FaultKey:     evidence.FaultKey,
			SubjectKey:   evidence.SubjectKey,
			DecisionMode: evidence.DecisionMode,
			NextAction:   evidence.NextAction,
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
	view.DelegationTruth.CompanionSwarmCount = len(view.CompanionSwarms)
	if len(view.CompanionSwarms) > 0 {
		view.DelegationTruth.RuntimeStatus = "delegation_artifacts_visible"
		view.DelegationTruth.OperatorSurface = "companion delegate"
		view.DelegationTruth.Note = "delegation artifacts are visible through companion swarm projections"
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
		evidence := decodeRecoveryEvidence(recovery.DetailsJSON)
		view.Observability.Recoveries = append(view.Observability.Recoveries, RecoverySummary{
			RecoveryID:   recovery.RecoveryID,
			RunID:        recovery.RunID,
			Status:       recovery.Status,
			Strategy:     recovery.Strategy,
			FaultKey:     evidence.FaultKey,
			SubjectKey:   evidence.SubjectKey,
			DecisionMode: evidence.DecisionMode,
			ActionName:   evidence.ActionName,
			NextAction:   evidence.NextAction,
			StartedAt:    recovery.StartedAt,
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

	activityLog, err := service.activityLog(ctx, resolved)
	if err != nil {
		return View{}, err
	}
	view.Observability.ActivityLog = activityLog

	intakeViews, err := projections.ListTaskIntakeEvidenceViews(ctx, service.Store.DB(), workspaces.DefaultWorkspaceKey)
	if err != nil {
		return View{}, err
	}
	view.IntakeInbox.Source = "task_intakes"
	view.IntakeInbox.Items = make([]IntakeEvidenceSummary, 0, len(intakeViews))
	for _, intake := range intakeViews {
		if !matchesIntakeScope(intake, resolved) {
			continue
		}
		view.IntakeInbox.Items = append(view.IntakeInbox.Items, IntakeEvidenceSummary{
			IntakeID:       intake.IntakeID,
			TaskID:         intake.TaskID,
			WorkspaceKey:   intake.WorkspaceKey,
			ProjectKey:     intake.ProjectKey,
			InitiativeKey:  intake.InitiativeKey,
			CompanionKey:   intake.CompanionKey,
			WorkItemKey:    intake.WorkItemKey,
			WorkItemStatus: intake.WorkItemStatus,
			Source:         intake.Source,
			IntakeType:     intake.IntakeType,
			DedupKey:       intake.DedupKey,
			RequestedBy:    intake.RequestedBy,
			CreatedAt:      intake.CreatedAt,
		})
	}
	rawIntakeItems, err := service.Store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
	if err != nil {
		return View{}, err
	}
	view.IntakeInbox.Source = "intake_items"
	view.IntakeInbox.RawItemCount = len(rawIntakeItems)
	for _, item := range rawIntakeItems {
		status := strings.ToLower(strings.TrimSpace(item.Status))
		if status != "received" {
			view.IntakeInbox.RawProcessedCount++
		}
		if isReviewableIntakeStatus(status) {
			view.IntakeInbox.ReviewQueueCount++
		}
		switch status {
		case "review_required":
			view.IntakeInbox.ReviewRequiredCount++
		case "needs_clarification":
			view.IntakeInbox.NeedsClarificationCount++
		case "duplicate_linked_or_suppressed":
			view.IntakeInbox.DuplicateLinkedCount++
		case "approval_required":
			view.IntakeInbox.IntakeApprovalRequiredCount++
		case "accepted":
			view.IntakeInbox.AcceptedCount++
		case "rejected":
			view.IntakeInbox.RejectedCount++
		case "archived":
			view.IntakeInbox.ArchivedCount++
		case "approval_denied":
			view.IntakeInbox.ApprovalDeniedCount++
		}
		view.IntakeInbox.RawItems = append(view.IntakeInbox.RawItems, rawIntakeSummary(item))
	}
	if len(rawIntakeItems) > 0 {
		if len(view.IntakeInbox.Items) > 0 {
			view.IntakeInbox.Source = "intake_items_and_task_intakes"
		}
		view.IntakeInbox.Status = intakeLaneStatus(view.IntakeInbox)
		view.IntakeInbox.Note = "governed intake lifecycle is live; raw, review, approval, and accepted states are counted before task execution"
	} else if len(view.IntakeInbox.Items) > 0 {
		view.IntakeInbox.Source = "task_intakes"
		view.IntakeInbox.Status = "linked_evidence"
		view.IntakeInbox.Note = "task_intakes are linked intake evidence; no raw governed intake items are currently present"
	}

	followUpViews, err := projections.ListFollowUpSummaryViews(ctx, service.Store.DB(), workspaces.DefaultWorkspaceKey, service.now())
	if err != nil {
		return View{}, err
	}
	view.AutomationTriggers.Wiring = WiringLive
	view.AutomationTriggers.Items = make([]AutomationTriggerSummary, 0, len(followUpViews))
	for _, followUp := range followUpViews {
		if !matchesFollowUpScope(followUp, resolved) {
			continue
		}
		var lastCompletedAt *string
		if followUp.LastCompletedAt != nil {
			lastCompletedAt = stringPtr(followUp.LastCompletedAt.UTC().Format(time.RFC3339))
		}
		view.AutomationTriggers.Items = append(view.AutomationTriggers.Items, AutomationTriggerSummary{
			Source:           "follow_up_obligation",
			TriggerID:        followUp.ObligationID,
			WorkspaceKey:     followUp.WorkspaceKey,
			InitiativeKey:    followUp.InitiativeKey,
			CompanionKey:     followUp.CompanionKey,
			TargetProjectKey: followUp.TargetProjectKey,
			Title:            followUp.Title,
			Status:           followUp.Status,
			DueStatus:        followUp.DueStatus,
			NextDueAt:        followUp.NextDueAt.UTC().Format(time.RFC3339),
			LastCompletedAt:  lastCompletedAt,
		})
	}
	automationTriggers, err := service.Store.ListAutomationTriggers(ctx, sqlite.ListAutomationTriggersParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
	if err != nil {
		return View{}, err
	}
	projectKeyCache := make(map[int64]string)
	projectKeyForTrigger := func(trigger sqlite.AutomationTrigger) string {
		if cached, ok := projectKeyCache[trigger.ProjectID]; ok {
			return cached
		}
		project, err := service.Store.GetProject(ctx, trigger.ProjectID)
		if err != nil {
			projectKeyCache[trigger.ProjectID] = trigger.InitiativeKey
			return trigger.InitiativeKey
		}
		projectKeyCache[trigger.ProjectID] = project.Key
		return project.Key
	}
	for _, trigger := range automationTriggers {
		projectKey := projectKeyForTrigger(trigger)
		if !matchesAutomationTriggerScope(trigger, projectKey, resolved) {
			continue
		}
		view.AutomationTriggers.TriggerCount++
		if trigger.Status == "enabled" {
			view.AutomationTriggers.EnabledCount++
		}
		if trigger.LastWorkItemID != nil {
			view.AutomationTriggers.MaterializedCount++
		}
		view.AutomationTriggers.Items = append(view.AutomationTriggers.Items, automationTriggerSummary(trigger, projectKey, service.now()))
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

	skillActivity, err := service.skillActivity(ctx, resolved)
	if err != nil {
		return View{}, err
	}
	view.SkillActivity = skillActivity

	knowledgeContextPacks, err := service.knowledgeContextPacks(ctx, resolved)
	if err != nil {
		return View{}, err
	}
	view.KnowledgeContextPacks = knowledgeContextPacks
	view.ReviewQueue = reviewQueueLane(view)

	return view, nil
}

func reviewQueueLane(view View) ReviewQueueLane {
	lane := ReviewQueueLane{
		Wiring:             WiringLive,
		IntakeCount:        view.IntakeInbox.ReviewQueueCount,
		ApprovalCount:      len(view.Approvals),
		KnowledgeCount:     view.KnowledgeContextPacks.ReviewRequiredCount,
		SkillArtifactCount: view.SkillActivity.ReviewRequiredArtifactCount,
		FailedWorkCount:    len(view.Observability.RecoveryGuidance),
	}
	lane.TotalCount = lane.IntakeCount + lane.ApprovalCount + lane.KnowledgeCount + lane.SkillArtifactCount + lane.FailedWorkCount
	return lane
}

func (service Service) knowledgeContextPacks(ctx context.Context, resolved scope.Resolution) (KnowledgeContextPackLane, error) {
	lane := KnowledgeContextPackLane{Wiring: WiringLive}
	proposals, err := runtimeknowledge.Service{Store: service.Store}.ListContextPackProposals(ctx, "")
	if err != nil {
		return KnowledgeContextPackLane{}, err
	}
	for _, proposal := range proposals {
		if !matchesKnowledgeContextPackScope(proposal, resolved) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(proposal.Packet.Status)) {
		case "review_required":
			lane.ReviewRequiredCount++
		case "active":
			lane.AcceptedCount++
		case "rejected":
			lane.RejectedCount++
		case "archived":
			lane.ArchivedCount++
		}
		lane.Recent = append(lane.Recent, KnowledgeContextPackSummary{
			ID:         proposal.Packet.ID,
			Status:     proposal.Packet.Status,
			ProjectKey: proposal.ContextPack.ProjectKey,
			TaskKey:    proposal.ContextPack.Task.Key,
			Summary:    proposal.Packet.Summary,
			CreatedAt:  proposal.Packet.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	const recentLimit = 5
	if len(lane.Recent) > recentLimit {
		lane.Recent = append([]KnowledgeContextPackSummary(nil), lane.Recent[len(lane.Recent)-recentLimit:]...)
	}
	return lane, nil
}

func (service Service) skillActivity(ctx context.Context, resolved scope.Resolution) (SkillActivityLane, error) {
	lane := SkillActivityLane{
		Wiring: WiringLive,
	}

	var projectID *int64
	if resolved.Kind == scope.ScopeProject || resolved.Kind == scope.ScopeOdinCore {
		project, err := service.Store.GetProjectByKey(ctx, resolved.ProjectKey)
		switch err {
		case nil:
			id := project.ID
			projectID = &id
		case sql.ErrNoRows:
			return lane, nil
		default:
			return SkillActivityLane{}, err
		}
	}

	records, err := service.Store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		return SkillActivityLane{}, err
	}

	for _, record := range records {
		if record.StreamType != runtimeevents.StreamSkill {
			continue
		}
		if !matchesSkillEventScope(record, resolved, projectID) {
			continue
		}

		if record.Type == runtimeevents.EventSkillArtifactRecorded {
			payload, err := runtimeevents.DecodePayload[runtimeevents.SkillArtifactRecordedPayload](record.Payload)
			if err != nil {
				return SkillActivityLane{}, err
			}
			lane.DurableReviewableArtifactCount++
			lane.Recent = append(lane.Recent, SkillActivitySummary{
				EventID:          record.ID,
				SkillKey:         payload.SkillKey,
				Scope:            record.Scope,
				Operation:        "artifact_recorded",
				Outcome:          payload.Status,
				ExecutionProfile: payload.ExecutionProfile,
				RuntimeEffect:    payload.RuntimeEffect,
				HandlerRef:       payload.HandlerRef,
				Permissions:      append([]string(nil), payload.Permissions...),
				OccurredAt:       record.OccurredAt.UTC().Format(time.RFC3339),
			})
			continue
		}
		if record.Type == runtimeevents.EventSkillArtifactReviewed {
			payload, err := runtimeevents.DecodePayload[runtimeevents.SkillArtifactReviewedPayload](record.Payload)
			if err != nil {
				return SkillActivityLane{}, err
			}
			lane.Recent = append(lane.Recent, SkillActivitySummary{
				EventID:       record.ID,
				SkillKey:      payload.SkillKey,
				Scope:         record.Scope,
				Operation:     "artifact_reviewed",
				Outcome:       payload.Decision,
				RuntimeEffect: "review_decision",
				OccurredAt:    record.OccurredAt.UTC().Format(time.RFC3339),
			})
			continue
		}
		if record.Type != runtimeevents.EventSkillLifecycleRecorded {
			continue
		}
		payload, err := runtimeevents.DecodePayload[runtimeevents.SkillLifecycleRecordedPayload](record.Payload)
		if err != nil {
			return SkillActivityLane{}, err
		}
		if payload.Operation == string(skillsOperationInvoke) {
			switch payload.Outcome {
			case "success":
				lane.InvokeSuccessCount++
			case "failure":
				lane.InvokeFailureCount++
			}
			switch payload.RuntimeEffect {
			case "stub_result":
				lane.StubResultCount++
			case "command_output_only":
				lane.CommandOutputOnlyCount++
			}
		}
		lane.Recent = append(lane.Recent, SkillActivitySummary{
			EventID:          record.ID,
			SkillKey:         payload.SkillKey,
			Scope:            record.Scope,
			Operation:        payload.Operation,
			Outcome:          payload.Outcome,
			ExecutionProfile: payload.ExecutionProfile,
			RuntimeEffect:    payload.RuntimeEffect,
			HandlerRef:       payload.HandlerRef,
			Permissions:      append([]string(nil), payload.Permissions...),
			ErrorCode:        payload.ErrorCode,
			OccurredAt:       record.OccurredAt.UTC().Format(time.RFC3339),
		})
	}

	const recentLimit = 5
	if len(lane.Recent) > recentLimit {
		lane.Recent = append([]SkillActivitySummary(nil), lane.Recent[len(lane.Recent)-recentLimit:]...)
	}
	artifacts, err := service.Store.ListSkillArtifacts(ctx, sqlite.ListSkillArtifactsParams{})
	if err != nil {
		return SkillActivityLane{}, err
	}
	for _, artifact := range artifacts {
		if !matchesSkillArtifactScope(artifact, resolved, projectID) {
			continue
		}
		switch artifact.Status {
		case "review_required":
			lane.ReviewRequiredArtifactCount++
		case "accepted":
			lane.AcceptedArtifactCount++
		case "rejected":
			lane.RejectedArtifactCount++
		case "archived":
			lane.ArchivedArtifactCount++
		}
	}
	return lane, nil
}

const activityLogLimit = 12

func (service Service) activityLog(ctx context.Context, resolved scope.Resolution) ([]ActivityEventSummary, error) {
	var projectID *int64
	params := sqlite.ListEventsParams{}
	if resolved.Kind == scope.ScopeProject || resolved.Kind == scope.ScopeOdinCore {
		project, err := service.Store.GetProjectByKey(ctx, resolved.ProjectKey)
		switch err {
		case nil:
			id := project.ID
			projectID = &id
			params.ProjectID = &id
		case sql.ErrNoRows:
			return nil, nil
		default:
			return nil, err
		}
	}

	records, err := service.Store.ListEvents(ctx, params)
	if err != nil {
		return nil, err
	}
	filtered := make([]runtimeevents.Record, 0, len(records))
	for _, record := range records {
		if !matchesActivityEventScope(record, resolved, projectID) {
			continue
		}
		filtered = append(filtered, record)
	}
	if len(filtered) > activityLogLimit {
		filtered = filtered[len(filtered)-activityLogLimit:]
	}
	return BuildActivityEventSummaries(ctx, service.Store, filtered, false)
}

func BuildActivityEventSummaries(ctx context.Context, store *sqlite.Store, records []runtimeevents.Record, includePayload bool) ([]ActivityEventSummary, error) {
	taskCache := make(map[int64]sqlite.Task)
	taskMissing := make(map[int64]struct{})
	projectCache := make(map[int64]string)
	projectMissing := make(map[int64]struct{})

	summaries := make([]ActivityEventSummary, 0, len(records))
	for _, record := range records {
		payload := decodeEventPayloadMap(record.Payload)
		taskID := cloneInt64Ptr(record.TaskID)
		if taskID == nil {
			if id, ok := payloadInt64(payload, "task_id"); ok {
				taskID = &id
			}
		}
		runID := cloneInt64Ptr(record.RunID)
		if runID == nil {
			if id, ok := payloadInt64(payload, "run_id"); ok {
				runID = &id
			}
		}

		var workItemKey string
		projectID := cloneInt64Ptr(record.ProjectID)
		if taskID != nil {
			task, ok, err := cachedActivityTask(ctx, store, taskCache, taskMissing, *taskID)
			if err != nil {
				return nil, err
			}
			if ok {
				workItemKey = task.Key
				if projectID == nil {
					id := task.ProjectID
					projectID = &id
				}
			}
		}

		projectKey := ""
		if projectID != nil {
			key, ok, err := cachedActivityProjectKey(ctx, store, projectCache, projectMissing, *projectID)
			if err != nil {
				return nil, err
			}
			if ok {
				projectKey = key
			}
		}

		item := ActivityEventSummary{
			EventID:     record.ID,
			StreamType:  string(record.StreamType),
			StreamID:    record.StreamID,
			EventType:   string(record.Type),
			Scope:       record.Scope,
			ProjectID:   projectID,
			ProjectKey:  projectKey,
			TaskID:      taskID,
			WorkItemKey: workItemKey,
			RunID:       runID,
			ApprovalID:  activityApprovalID(record),
			OccurredAt:  record.OccurredAt.UTC().Format(time.RFC3339),
			Summary:     summarizeActivityEvent(record, payload, workItemKey, projectKey),
		}
		if includePayload && len(record.Payload) > 0 {
			item.Payload = append(json.RawMessage(nil), record.Payload...)
		}
		summaries = append(summaries, item)
	}
	return summaries, nil
}

func cachedActivityTask(ctx context.Context, store *sqlite.Store, cache map[int64]sqlite.Task, missing map[int64]struct{}, taskID int64) (sqlite.Task, bool, error) {
	if task, ok := cache[taskID]; ok {
		return task, true, nil
	}
	if _, ok := missing[taskID]; ok {
		return sqlite.Task{}, false, nil
	}
	task, err := store.GetTask(ctx, taskID)
	if err != nil {
		if err == sql.ErrNoRows {
			missing[taskID] = struct{}{}
			return sqlite.Task{}, false, nil
		}
		return sqlite.Task{}, false, err
	}
	cache[taskID] = task
	return task, true, nil
}

func cachedActivityProjectKey(ctx context.Context, store *sqlite.Store, cache map[int64]string, missing map[int64]struct{}, projectID int64) (string, bool, error) {
	if key, ok := cache[projectID]; ok {
		return key, true, nil
	}
	if _, ok := missing[projectID]; ok {
		return "", false, nil
	}
	project, err := store.GetProject(ctx, projectID)
	if err != nil {
		if err == sql.ErrNoRows {
			missing[projectID] = struct{}{}
			return "", false, nil
		}
		return "", false, err
	}
	cache[projectID] = project.Key
	return project.Key, true, nil
}

func matchesActivityEventScope(record runtimeevents.Record, resolved scope.Resolution, projectID *int64) bool {
	switch resolved.Kind {
	case scope.ScopeGlobal:
		return true
	case scope.ScopeProject:
		return record.Scope == string(scope.ScopeProject) && sameOptionalID(record.ProjectID, projectID)
	case scope.ScopeOdinCore:
		return (record.Scope == string(scope.ScopeOdinCore) || record.Scope == string(scope.ScopeProject)) && sameOptionalID(record.ProjectID, projectID)
	case scope.ScopeNewProject:
		return record.Scope == string(scope.ScopeNewProject)
	default:
		return false
	}
}

func sameOptionalID(left, right *int64) bool {
	if right == nil {
		return true
	}
	return left != nil && *left == *right
}

func activityApprovalID(record runtimeevents.Record) *int64 {
	if record.StreamType != runtimeevents.StreamApproval || record.StreamID <= 0 {
		return nil
	}
	id := record.StreamID
	return &id
}

func summarizeActivityEvent(record runtimeevents.Record, payload map[string]any, workItemKey, projectKey string) string {
	switch record.Type {
	case runtimeevents.EventTaskCreated:
		parts := []string{"created work item"}
		if workItemKey != "" {
			parts = append(parts, workItemKey)
		}
		appendPayloadPart(&parts, payload, "status", "status")
		appendPayloadPart(&parts, payload, "requested_by", "requested_by")
		appendPayloadPart(&parts, payload, "execution_intent", "intent")
		return strings.Join(parts, " ")
	case runtimeevents.EventTaskDispatchRequested:
		parts := []string{"dispatch requested"}
		appendPayloadPart(&parts, payload, "executor", "executor")
		appendPayloadPart(&parts, payload, "attempt", "attempt")
		appendPayloadPart(&parts, payload, "status", "status")
		return strings.Join(parts, " ")
	case runtimeevents.EventTaskStatusChanged:
		return transitionSummary("work item status", payload, "previous_status", "status")
	case runtimeevents.EventTaskQueueStateChanged:
		parts := []string{transitionSummary("queue state", payload, "previous_status", "status")}
		appendPayloadPart(&parts, payload, "blocked_reason", "blocked_reason")
		appendPayloadPart(&parts, payload, "retry_count", "retry_count")
		return strings.Join(parts, " ")
	case runtimeevents.EventRunStarted:
		parts := []string{"run started"}
		appendPayloadPart(&parts, payload, "executor", "executor")
		appendPayloadPart(&parts, payload, "attempt", "attempt")
		appendPayloadPart(&parts, payload, "status", "status")
		return strings.Join(parts, " ")
	case runtimeevents.EventRunStatusChanged:
		return transitionSummary("run status", payload, "previous_status", "status")
	case runtimeevents.EventRunFinished:
		parts := []string{"run finished"}
		appendPayloadPart(&parts, payload, "status", "status")
		appendPayloadPart(&parts, payload, "terminal_reason", "terminal_reason")
		appendPayloadPart(&parts, payload, "summary", "summary")
		return strings.Join(parts, " ")
	case runtimeevents.EventApprovalRequested:
		parts := []string{"approval requested"}
		appendPayloadPart(&parts, payload, "status", "status")
		appendPayloadPart(&parts, payload, "requested_by", "requested_by")
		return strings.Join(parts, " ")
	case runtimeevents.EventApprovalResolved:
		parts := []string{"approval resolved"}
		appendPayloadPart(&parts, payload, "status", "status")
		appendPayloadPart(&parts, payload, "decision_by", "decision_by")
		appendPayloadPart(&parts, payload, "reason", "reason")
		return strings.Join(parts, " ")
	case runtimeevents.EventContextPacketCreated:
		parts := []string{"context packet created"}
		appendPayloadPart(&parts, payload, "packet_kind", "kind")
		appendPayloadPart(&parts, payload, "trigger", "trigger")
		appendPayloadPart(&parts, payload, "status", "status")
		appendPayloadPart(&parts, payload, "summary", "summary")
		return strings.Join(parts, " ")
	case runtimeevents.EventIntakeItemCreated, runtimeevents.EventIntakeProcessed, runtimeevents.EventIntakeReviewAccepted:
		parts := []string{strings.TrimPrefix(string(record.Type), "intake.")}
		appendPayloadPart(&parts, payload, "status", "status")
		appendPayloadPart(&parts, payload, "dedupe_key", "dedup_key")
		appendPayloadPart(&parts, payload, "requested_by", "requested_by")
		return strings.Join(parts, " ")
	case runtimeevents.EventAutomationTriggerCreated, runtimeevents.EventAutomationTriggerEvaluated, runtimeevents.EventAutomationTriggerMaterialized:
		parts := []string{strings.TrimPrefix(string(record.Type), "automation_trigger.")}
		appendPayloadPart(&parts, payload, "status", "status")
		appendPayloadPart(&parts, payload, "title", "title")
		return strings.Join(parts, " ")
	default:
		parts := []string{fmt.Sprintf("event %s", record.Type)}
		if workItemKey != "" {
			parts = append(parts, "work_item="+workItemKey)
		}
		if projectKey != "" {
			parts = append(parts, "project="+projectKey)
		}
		return strings.Join(parts, " ")
	}
}

func transitionSummary(label string, payload map[string]any, previousKey, statusKey string) string {
	previous := payloadString(payload, previousKey)
	status := payloadString(payload, statusKey)
	if previous != "" && status != "" {
		return fmt.Sprintf("%s %s -> %s", label, previous, status)
	}
	if status != "" {
		return fmt.Sprintf("%s status=%s", label, status)
	}
	return label
}

func appendPayloadPart(parts *[]string, payload map[string]any, key, label string) {
	value := payloadString(payload, key)
	if value == "" {
		return
	}
	*parts = append(*parts, fmt.Sprintf("%s=%s", label, value))
}

func decodeEventPayloadMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func payloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return formatPayloadFloat(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func payloadInt64(payload map[string]any, key string) (int64, bool) {
	value, ok := payload[key]
	if !ok || value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return int64(typed), typed > 0
	case int64:
		return typed, typed > 0
	case int:
		return int64(typed), typed > 0
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, false
		}
		var parsed int64
		if _, err := fmt.Sscan(trimmed, &parsed); err != nil || parsed <= 0 {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func formatPayloadFloat(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprintf("%d", int64(value))
	}
	return fmt.Sprintf("%g", value)
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

type skillsOperation string

const skillsOperationInvoke skillsOperation = "invoke"

func matchesSkillEventScope(record runtimeevents.Record, resolved scope.Resolution, projectID *int64) bool {
	switch resolved.Kind {
	case scope.ScopeGlobal:
		return true
	case scope.ScopeProject, scope.ScopeOdinCore:
		return projectID != nil && record.ProjectID != nil && *record.ProjectID == *projectID && record.Scope == string(resolved.Kind)
	case scope.ScopeNewProject:
		return record.Scope == string(scope.ScopeNewProject)
	default:
		return false
	}
}

func matchesSkillArtifactScope(artifact sqlite.SkillArtifact, resolved scope.Resolution, projectID *int64) bool {
	switch resolved.Kind {
	case scope.ScopeGlobal:
		return true
	case scope.ScopeProject, scope.ScopeOdinCore:
		return projectID != nil && artifact.ProjectID != nil && *artifact.ProjectID == *projectID && artifact.Scope == string(resolved.Kind)
	case scope.ScopeNewProject:
		return artifact.Scope == string(scope.ScopeNewProject)
	default:
		return false
	}
}

func matchesKnowledgeContextPackScope(proposal runtimeknowledge.ContextPackProposal, resolved scope.Resolution) bool {
	switch resolved.Kind {
	case scope.ScopeGlobal:
		return true
	case scope.ScopeProject, scope.ScopeOdinCore:
		return strings.EqualFold(proposal.ContextPack.ProjectKey, resolved.ProjectKey)
	case scope.ScopeNewProject:
		return proposal.ContextPack.ProjectKey == ""
	default:
		return false
	}
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now().UTC()
	}
	return time.Now().UTC()
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

type recoveryEvidenceFields struct {
	FaultKey     string `json:"fault_key"`
	SubjectKey   string `json:"subject_key"`
	DecisionMode string `json:"decision_mode"`
	ActionName   string `json:"action_name"`
	NextAction   string `json:"next_action"`
}

func decodeRecoveryEvidence(detailsJSON string) recoveryEvidenceFields {
	if strings.TrimSpace(detailsJSON) == "" {
		return recoveryEvidenceFields{}
	}
	var evidence recoveryEvidenceFields
	if err := json.Unmarshal([]byte(detailsJSON), &evidence); err != nil {
		return recoveryEvidenceFields{}
	}
	return evidence
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

func matchesFollowUpScope(followUp projections.FollowUpSummaryView, resolved scope.Resolution) bool {
	switch resolved.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		if followUp.TargetProjectKey == resolved.ProjectKey {
			return true
		}
		if followUp.InitiativeKey != nil && *followUp.InitiativeKey == resolved.ProjectKey {
			return true
		}
		return false
	case scope.ScopeNewProject:
		return false
	default:
		return true
	}
}

func matchesAutomationTriggerScope(trigger sqlite.AutomationTrigger, projectKey string, resolved scope.Resolution) bool {
	switch resolved.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		return projectKey == resolved.ProjectKey || trigger.InitiativeKey == resolved.ProjectKey
	case scope.ScopeNewProject:
		return false
	default:
		return true
	}
}

func automationTriggerSummary(trigger sqlite.AutomationTrigger, projectKey string, now time.Time) AutomationTriggerSummary {
	title := trigger.WorkItemTitle
	if title == "" {
		title = trigger.RuleSummary
	}
	if title == "" {
		title = trigger.Key
	}
	var nextDueAt string
	if trigger.NextEligibleAt != nil {
		nextDueAt = trigger.NextEligibleAt.UTC().Format(time.RFC3339)
	}
	var lastMaterializedAt *string
	if trigger.LastMaterializedAt != nil {
		lastMaterializedAt = stringPtr(trigger.LastMaterializedAt.UTC().Format(time.RFC3339))
	}
	return AutomationTriggerSummary{
		Source:                 "automation_trigger",
		TriggerID:              trigger.ID,
		Key:                    trigger.Key,
		WorkspaceKey:           trigger.WorkspaceID,
		InitiativeKey:          stringPtr(trigger.InitiativeKey),
		TargetProjectKey:       projectKey,
		Title:                  title,
		Kind:                   trigger.Kind,
		Status:                 trigger.Status,
		DueStatus:              automationTriggerDueStatus(trigger, now),
		NextDueAt:              nextDueAt,
		LastMaterializedAt:     lastMaterializedAt,
		LastMaterializationKey: trigger.LastMaterializationKey,
		LastWorkItemKey:        trigger.LastWorkItemKey,
	}
}

func automationTriggerDueStatus(trigger sqlite.AutomationTrigger, now time.Time) string {
	if trigger.Status != "enabled" {
		return trigger.Status
	}
	if trigger.NextEligibleAt == nil {
		return "manual"
	}
	if trigger.NextEligibleAt.After(now.UTC()) {
		if trigger.LastEvaluatedAt != nil && (trigger.LastMaterializedAt == nil || trigger.LastEvaluatedAt.After(*trigger.LastMaterializedAt)) {
			return "deferred"
		}
		return "waiting"
	}
	return "due"
}

func matchesIntakeScope(intake projections.TaskIntakeEvidenceView, resolved scope.Resolution) bool {
	switch resolved.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		if intake.ProjectKey == resolved.ProjectKey {
			return true
		}
		if intake.InitiativeKey != nil && *intake.InitiativeKey == resolved.ProjectKey {
			return true
		}
		return false
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

func isReviewableIntakeStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "review_required", "needs_clarification", "duplicate_linked_or_suppressed", "approval_required":
		return true
	default:
		return false
	}
}

func rawIntakeSummary(item sqlite.IntakeItem) RawIntakeSummary {
	return RawIntakeSummary{
		ID:          item.ID,
		Key:         fmt.Sprintf("intake-%d", item.ID),
		ProjectKey:  rawIntakeProjectKey(item),
		Source:      item.SourceFamily,
		IntakeType:  item.EventKind,
		DedupKey:    item.DedupeKey,
		RequestedBy: rawIntakeRequestedBy(item.SourceFactsJSON),
		Title:       item.Subject,
		Status:      item.Status,
		Summary:     item.Summary,
		CreatedAt:   item.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   item.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func rawIntakeProjectKey(item sqlite.IntakeItem) string {
	switch strings.TrimSpace(item.Scope) {
	case "project", "odin-core":
		return strings.TrimSpace(item.ScopeKey)
	default:
		return ""
	}
}

func rawIntakeRequestedBy(sourceFactsJSON string) string {
	var facts struct {
		RequestedBy string `json:"requested_by"`
	}
	if err := json.Unmarshal([]byte(sourceFactsJSON), &facts); err != nil {
		return ""
	}
	return facts.RequestedBy
}

func intakeLaneStatus(lane IntakeInboxLane) string {
	switch {
	case lane.IntakeApprovalRequiredCount > 0:
		return "approval_pending"
	case lane.ReviewQueueCount > 0:
		return "review_pending"
	case lane.RawProcessedCount > 0:
		return "processed"
	default:
		return "received"
	}
}
