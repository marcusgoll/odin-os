package overview

import "encoding/json"

type Wiring string

const (
	WiringLive          Wiring = "live"
	WiringCatalogBacked Wiring = "catalog_backed"
	WiringNotYetWired   Wiring = "not_yet_wired"
)

type ReadinessLane struct {
	Wiring       Wiring `json:"wiring"`
	Status       string `json:"status"`
	HealthStatus string `json:"health_status"`
	Ready        bool   `json:"ready"`
	Note         string `json:"note,omitempty"`
}

type ActualUseLane struct {
	Wiring                        Wiring `json:"wiring"`
	Status                        string `json:"status"`
	ActionRequiredCount           int    `json:"action_required_count"`
	WorkItemCount                 int    `json:"work_item_count"`
	OpenWorkItemCount             int    `json:"open_work_item_count"`
	ActiveRunCount                int    `json:"active_run_count"`
	PendingApprovalCount          int    `json:"pending_approval_count"`
	ReviewQueueCount              int    `json:"review_queue_count"`
	BlockedWorkItemCount          int    `json:"blocked_work_item_count"`
	FailedWorkItemCount           int    `json:"failed_work_item_count"`
	RecoveryRecommendationCount   int    `json:"recovery_recommendation_count"`
	IntakeReviewCount             int    `json:"intake_review_count"`
	AutomationTriggerCount        int    `json:"automation_trigger_count"`
	EnabledAutomationTriggerCount int    `json:"enabled_automation_trigger_count"`
	FollowUpObligationCount       int    `json:"follow_up_obligation_count"`
	DueFollowUpObligationCount    int    `json:"due_follow_up_obligation_count"`
	DeliveryProfileCount          int    `json:"delivery_profile_count"`
	ExplicitIntentWorkItemCount   int    `json:"explicit_intent_work_item_count"`
	FallbackIntentWorkItemCount   int    `json:"fallback_intent_work_item_count"`
}

type DeliveryProfileLane struct {
	Wiring       Wiring   `json:"wiring"`
	ProfileCount int      `json:"profile_count"`
	Keys         []string `json:"keys"`
}

type ExecutionIntentLane struct {
	Wiring                Wiring `json:"wiring"`
	ExplicitWorkItemCount int    `json:"explicit_work_item_count"`
	FallbackWorkItemCount int    `json:"fallback_work_item_count"`
}

type BinarySourceLane struct {
	Wiring     Wiring `json:"wiring"`
	Status     string `json:"status"`
	BinaryPath string `json:"binary_path,omitempty"`
	SourceRoot string `json:"source_root,omitempty"`
	Note       string `json:"note,omitempty"`
}

type CapabilityCatalogLane struct {
	Wiring               Wiring `json:"wiring"`
	AgentDefinitionCount int    `json:"agent_definition_count"`
	SkillCount           int    `json:"skill_count"`
	WorkflowCount        int    `json:"workflow_count"`
	CommandCount         int    `json:"command_count"`
	ToolCount            int    `json:"tool_count"`
}

type CapabilityTruthLane struct {
	Wiring              Wiring                   `json:"wiring"`
	AuthoredInventory   CapabilityCatalogLane    `json:"authored_inventory"`
	AuthoredAssetCount  int                      `json:"authored_asset_count"`
	RuntimeProvenCount  int                      `json:"runtime_proven_count"`
	PartialCount        int                      `json:"partial_count"`
	AdvisoryCount       int                      `json:"advisory_count"`
	UnknownCount        int                      `json:"unknown_count"`
	HighRiskFamilyCount int                      `json:"high_risk_family_count"`
	Notes               []string                 `json:"notes,omitempty"`
	Items               []CapabilityTruthSummary `json:"items"`
}

type CapabilityTruthSummary struct {
	Kind                string   `json:"kind"`
	Key                 string   `json:"key"`
	Title               string   `json:"title,omitempty"`
	TruthLevel          string   `json:"truth_level"`
	CountsAsImplemented bool     `json:"counts_as_implemented"`
	HighRisk            bool     `json:"high_risk,omitempty"`
	RiskLabel           string   `json:"risk_label,omitempty"`
	Proof               []string `json:"proof,omitempty"`
}

type ReviewQueueLane struct {
	Wiring              Wiring `json:"wiring"`
	TotalCount          int    `json:"total_count"`
	IntakeCount         int    `json:"intake_count"`
	GoalCount           int    `json:"goal_count"`
	ApprovalCount       int    `json:"approval_count"`
	KnowledgeCount      int    `json:"knowledge_count"`
	SkillArtifactCount  int    `json:"skill_artifact_count"`
	MemoryProposalCount int    `json:"memory_proposal_count"`
	RecoveryCount       int    `json:"recovery_count"`
	FailedWorkCount     int    `json:"failed_work_count"`
}

type BrowserEvidenceSummary struct {
	ArtifactID    int64                 `json:"artifact_id"`
	RunID         int64                 `json:"run_id"`
	TaskID        int64                 `json:"task_id"`
	WorkItemKey   string                `json:"work_item_key"`
	ProjectKey    string                `json:"project_key"`
	Status        string                `json:"status,omitempty"`
	EvidenceType  string                `json:"evidence_type,omitempty"`
	AdapterStatus string                `json:"adapter_status,omitempty"`
	InitiativeKey *string               `json:"initiative_key,omitempty"`
	CompanionKey  *string               `json:"companion_key,omitempty"`
	RunStatus     string                `json:"run_status,omitempty"`
	RunAttempt    int                   `json:"run_attempt,omitempty"`
	Executor      string                `json:"executor,omitempty"`
	Summary       string                `json:"summary"`
	URI           string                `json:"uri,omitempty"`
	PageTitle     string                `json:"page_title,omitempty"`
	URL           string                `json:"url,omitempty"`
	SelectedLinks []BrowserEvidenceLink `json:"selected_links,omitempty"`
	Confidence    string                `json:"confidence,omitempty"`
	Limitations   []string              `json:"limitations,omitempty"`
	CreatedAt     string                `json:"created_at"`
}

type BrowserEvidenceLink struct {
	Text   string `json:"text,omitempty"`
	URL    string `json:"url"`
	Reason string `json:"reason,omitempty"`
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
	Summary     string          `json:"summary"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	OccurredAt  string          `json:"occurred_at"`
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
