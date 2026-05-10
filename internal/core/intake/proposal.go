package intake

import (
	"fmt"
	"strings"
)

const (
	StateReceived             = "received"
	StateProcessing           = "processing"
	StateReviewRequired       = "review_required"
	StateNeedsClarification   = "needs_clarification"
	StateDuplicateLinked      = "duplicate_linked"
	StateArchived             = "archived"
	StateAcceptedForPromotion = "accepted_for_promotion"
	StateErrored              = "errored"
)

const (
	DraftTask              = "draft_task"
	DraftResearch          = "draft_research"
	DraftDocument          = "draft_document"
	DraftAdminTask         = "draft_admin_task"
	DraftIncidentReview    = "draft_incident_review"
	DraftRoutine           = "draft_routine"
	DraftFollowUp          = "draft_follow_up"
	DraftIdea              = "draft_idea"
	DraftPolicyChange      = "draft_policy_change"
	DraftDestructiveAction = "draft_destructive_action"
	ArchiveCandidate       = "archive_candidate"
)

const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

const (
	ApprovalNeedsReview     = "needs_review"
	ApprovalRequired        = "approval_required"
	ApprovalBlocked         = "blocked"
	ApprovalReadyToPromote  = "ready_to_promote"
	ApprovalNoWorkNecessary = "no_work_necessary"
)

type ReviewableProposal struct {
	SourceIntakeKey       string        `json:"source_intake_key"`
	Title                 string        `json:"title"`
	Category              string        `json:"category"`
	Route                 string        `json:"route"`
	Summary               string        `json:"summary"`
	DraftArtifact         DraftArtifact `json:"draft_artifact"`
	AcceptanceCriteria    []string      `json:"acceptance_criteria,omitempty"`
	ClarificationPrompts  []string      `json:"clarification_prompts,omitempty"`
	RiskLevel             string        `json:"risk_level"`
	ApprovalPosture       string        `json:"approval_posture"`
	MissingConstraints    []string      `json:"missing_constraints,omitempty"`
	DedupeResult          string        `json:"dedupe_result"`
	RecommendedNextAction string        `json:"recommended_next_action"`
	OperatorNextAction    string        `json:"operator_next_action"`
}

type DraftArtifact struct {
	Kind                  string `json:"kind"`
	Title                 string `json:"title"`
	ReviewState           string `json:"review_state"`
	ExecutionIntent       string `json:"execution_intent,omitempty"`
	ExecutionIntentSource string `json:"execution_intent_source,omitempty"`
}

func (proposal ReviewableProposal) Validate() error {
	if strings.TrimSpace(proposal.SourceIntakeKey) == "" {
		return fmt.Errorf("source_intake_key is required")
	}
	if strings.TrimSpace(proposal.Title) == "" {
		return fmt.Errorf("title is required")
	}
	if strings.TrimSpace(proposal.Route) == "" {
		return fmt.Errorf("route is required")
	}
	isBlockedClarification := proposal.ApprovalPosture == ApprovalBlocked && len(proposal.ClarificationPrompts) > 0
	if strings.TrimSpace(proposal.DraftArtifact.Kind) == "" && !isBlockedClarification {
		return fmt.Errorf("draft_artifact.kind is required")
	}
	if strings.TrimSpace(proposal.ApprovalPosture) == "" {
		return fmt.Errorf("approval_posture is required")
	}
	if strings.TrimSpace(proposal.OperatorNextAction) == "" {
		return fmt.Errorf("operator_next_action is required")
	}
	return nil
}

func CanonicalState(status string) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch normalized {
	case "", StateReceived:
		return StateReceived
	case StateProcessing, "triaging":
		return StateProcessing
	case StateReviewRequired, "approval_required":
		return StateReviewRequired
	case StateNeedsClarification:
		return StateNeedsClarification
	case StateDuplicateLinked, "duplicate_linked_or_suppressed", "suppressed":
		return StateDuplicateLinked
	case StateArchived, "rejected", "approval_denied":
		return StateArchived
	case StateAcceptedForPromotion, "accepted":
		return StateAcceptedForPromotion
	case StateErrored, "error":
		return StateErrored
	default:
		return normalized
	}
}
