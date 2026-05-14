package reviewqueue

import "strings"

type Entry struct {
	ReviewID               string   `json:"review_id,omitempty"`
	QueueID                string   `json:"queue_id"`
	Type                   string   `json:"type,omitempty"`
	SourceType             string   `json:"source_type"`
	SourceID               int64    `json:"source_id,omitempty"`
	ObjectID               int64    `json:"object_id"`
	ObjectKey              string   `json:"object_key"`
	GoalID                 int64    `json:"goal_id,omitempty"`
	Status                 string   `json:"status"`
	Reason                 string   `json:"reason,omitempty"`
	Title                  string   `json:"title,omitempty"`
	CreatedAt              string   `json:"created_at,omitempty"`
	UpdatedAt              string   `json:"updated_at,omitempty"`
	ProjectScope           string   `json:"project_scope,omitempty"`
	Summary                string   `json:"summary,omitempty"`
	TaskID                 int64    `json:"task_id,omitempty"`
	TaskKey                string   `json:"task_key,omitempty"`
	TaskStatus             string   `json:"task_status,omitempty"`
	WorkKind               string   `json:"work_kind,omitempty"`
	Source                 string   `json:"source,omitempty"`
	Risk                   string   `json:"risk,omitempty"`
	Severity               string   `json:"severity,omitempty"`
	Decision               string   `json:"decision,omitempty"`
	RecommendedAction      string   `json:"recommended_action,omitempty"`
	OperatorNextStep       string   `json:"operator_next_step,omitempty"`
	RetryEligible          *bool    `json:"retry_eligible,omitempty"`
	RetryBlockReason       string   `json:"retry_block_reason,omitempty"`
	RecoveryRecommendation string   `json:"recovery_recommendation,omitempty"`
	BrowserEvidenceCount   int      `json:"browser_evidence_count,omitempty"`
	AllowedActions         []string `json:"allowed_actions"`
}

type Projection struct {
	Items               []Entry
	TotalCount          int
	IntakeCount         int
	GoalCount           int
	ApprovalCount       int
	KnowledgeCount      int
	SkillArtifactCount  int
	MemoryProposalCount int
	RecoveryCount       int
	FailedWorkCount     int
}

func Project(entries []Entry) Projection {
	projection := Projection{
		Items:      append([]Entry(nil), entries...),
		TotalCount: len(entries),
	}
	for _, entry := range entries {
		switch sourceType := strings.ToLower(strings.TrimSpace(entry.SourceType)); sourceType {
		case "intake_review", "intake_approval", "intake_goal_conversion":
			projection.IntakeCount++
		case "goal", "goal_blocker":
			projection.GoalCount++
		case "task_approval":
			projection.ApprovalCount++
		case "context_pack":
			projection.KnowledgeCount++
		case "skill_artifact", "design_artifact":
			projection.SkillArtifactCount++
		case "memory_proposal":
			projection.MemoryProposalCount++
		case "recovery":
			projection.RecoveryCount++
		case "failed_work":
			projection.FailedWorkCount++
		}
	}
	return projection
}
