package intake

import "testing"

func TestReviewableProposalPreservesTypedDraftArtifact(t *testing.T) {
	proposal := ReviewableProposal{
		SourceIntakeKey:       "intake-7",
		Title:                 "Investigate import incident",
		Category:              "bug",
		Route:                 "draft_incident_review",
		Summary:               "Prepare incident review for operator.",
		DraftArtifact:         DraftArtifact{Kind: DraftIncidentReview, Title: "Investigate import incident"},
		RiskLevel:             RiskMedium,
		ApprovalPosture:       ApprovalNeedsReview,
		DedupeResult:          "unique",
		RecommendedNextAction: "review",
		OperatorNextAction:    "odin intake review show intake-7",
	}
	if err := proposal.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if proposal.DraftArtifact.Kind != DraftIncidentReview {
		t.Fatalf("DraftArtifact.Kind = %q, want %q", proposal.DraftArtifact.Kind, DraftIncidentReview)
	}
}

func TestReviewableProposalAllowsBlockedClarificationWithoutDraftArtifact(t *testing.T) {
	proposal := ReviewableProposal{
		SourceIntakeKey:       "intake-8",
		Title:                 "Help with this",
		Category:              "clarification_needed",
		Route:                 StateNeedsClarification,
		Summary:               "Raw intake needs operator clarification before drafting work",
		ClarificationPrompts:  []string{"What outcome should Odin prepare for review?"},
		RiskLevel:             RiskMedium,
		ApprovalPosture:       ApprovalBlocked,
		DedupeResult:          "unique",
		RecommendedNextAction: "clarify",
		OperatorNextAction:    "odin intake review clarify intake-8",
	}
	if err := proposal.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestLifecycleAliasMapsCompatibilityStatuses(t *testing.T) {
	cases := map[string]string{
		"received":                       StateReceived,
		"review_required":                StateReviewRequired,
		"needs_clarification":            StateNeedsClarification,
		"duplicate_linked_or_suppressed": StateDuplicateLinked,
		"approval_required":              StateReviewRequired,
		"accepted":                       StateAcceptedForPromotion,
		"archived":                       StateArchived,
	}
	for input, want := range cases {
		if got := CanonicalState(input); got != want {
			t.Fatalf("CanonicalState(%q) = %q, want %q", input, got, want)
		}
	}
}
