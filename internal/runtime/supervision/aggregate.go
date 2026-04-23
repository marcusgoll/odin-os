package supervision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"odin-os/internal/store/sqlite"
)

var ErrUnsupportedConvergenceMode = errors.New("unsupported swarm convergence mode")
var ErrVerifierArtifactRequired = errors.New("review_gate convergence requires a verifier artifact")

type AggregationResult struct {
	ParentTask               sqlite.Task
	ConvergenceMode          string
	Status                   string
	Summary                  string
	TerminalReason           string
	Confidence               float64
	EvidenceRefs             []string
	UnresolvedRisks          []string
	ProposedNextActions      []string
	ProposedMemoryCandidates []string
	WinningDelegationID      *int64
	VerifierDelegationID     *int64
}

type delegationResultEnvelope struct {
	Status                   string   `json:"status"`
	Confidence               any      `json:"confidence"`
	EvidenceRefs             []string `json:"evidence_refs"`
	UnresolvedRisks          []string `json:"unresolved_risks"`
	ProposedNextActions      []string `json:"proposed_next_actions"`
	ProposedMemoryCandidates []string `json:"proposed_memory_candidates"`
}

type delegationResult struct {
	Delegation sqlite.Delegation
	Artifact   sqlite.DelegationArtifact
	Envelope   delegationResultEnvelope
	Confidence float64
}

func AggregateConvergence(mode string, artifacts []sqlite.DelegationArtifact) (AggregationResult, error) {
	mode = strings.TrimSpace(mode)
	if !isSupportedConvergenceMode(mode) {
		return AggregationResult{}, fmt.Errorf("%w: %s", ErrUnsupportedConvergenceMode, mode)
	}

	results := make([]delegationResult, 0, len(artifacts))
	for _, artifact := range artifacts {
		if !isAggregationOutcomeArtifactType(artifact.ArtifactType) {
			continue
		}
		envelope, err := decodeDelegationResultEnvelope(artifact.DetailsJSON)
		if err != nil {
			return AggregationResult{}, fmt.Errorf("decode delegation artifact %d: %w", artifact.ID, err)
		}
		results = append(results, delegationResult{
			Artifact:   artifact,
			Envelope:   envelope,
			Confidence: confidenceScore(envelope.Confidence),
		})
	}

	aggregation := AggregationResult{
		ConvergenceMode:          mode,
		Confidence:               bestConfidence(results),
		EvidenceRefs:             collectEvidenceRefs(results),
		UnresolvedRisks:          collectUnresolvedRisks(results),
		ProposedNextActions:      collectNextActions(results),
		ProposedMemoryCandidates: collectMemoryCandidates(results),
	}

	var helper Service
	switch mode {
	case "merge":
		helper.applyMergeConvergence(&aggregation, results)
	case "review_gate":
		verifierIndex := bestVerifierArtifactIndex(results)
		if verifierIndex < 0 {
			return AggregationResult{}, ErrVerifierArtifactRequired
		}
		aggregation.VerifierDelegationID = &results[verifierIndex].Delegation.ID
		producerSummaries := make([]string, 0, len(results))
		for i, outcome := range results {
			if i == verifierIndex {
				continue
			}
			producerSummaries = append(producerSummaries, outcome.Artifact.Summary)
		}
		if len(producerSummaries) == 0 {
			aggregation.Status = "blocked"
			aggregation.TerminalReason = "swarm_results_pending"
			aggregation.Summary = "Awaiting producer result artifacts"
			break
		}
		aggregation.Status = "completed"
		aggregation.Summary = strings.Join(uniqueNonEmptyStrings(producerSummaries), " + ")
		aggregation.Confidence = results[verifierIndex].Confidence
	case "rank":
		helper.applyRankConvergence(&aggregation, results)
	case "quorum":
		helper.applyQuorumConvergence(&aggregation, results, len(results))
	}

	if aggregation.Status == "completed" && len(aggregation.UnresolvedRisks) > 0 {
		aggregation.Status = "blocked"
		aggregation.TerminalReason = "swarm_unresolved_risks"
		aggregation.Summary = fmt.Sprintf("Swarm blocked by unresolved risks: %s", strings.Join(aggregation.UnresolvedRisks, "; "))
	}
	if strings.TrimSpace(aggregation.Status) == "" {
		aggregation.Status = "blocked"
		aggregation.TerminalReason = "swarm_results_pending"
		aggregation.Summary = "Swarm results pending"
	}
	return aggregation, nil
}

func (service Service) AggregateSwarm(ctx context.Context, parentTaskID int64) (AggregationResult, error) {
	if service.Store == nil {
		return AggregationResult{}, fmt.Errorf("supervision store is required")
	}
	if parentTaskID <= 0 {
		return AggregationResult{}, fmt.Errorf("parent task is required")
	}

	parentTask, err := service.Store.GetTask(ctx, parentTaskID)
	if err != nil {
		return AggregationResult{}, err
	}

	delegations, err := service.Store.ListDelegations(ctx, sqlite.ListDelegationsParams{
		ParentTaskID: &parentTaskID,
	})
	if err != nil {
		return AggregationResult{}, err
	}
	if len(delegations) == 0 {
		return AggregationResult{}, fmt.Errorf("swarm parent task %d has no delegations", parentTaskID)
	}

	mode := strings.TrimSpace(delegations[0].ConvergenceMode)
	if !isSupportedConvergenceMode(mode) {
		return AggregationResult{}, fmt.Errorf("%w: %s", ErrUnsupportedConvergenceMode, mode)
	}

	results, err := service.loadDelegationResults(ctx, delegations)
	if err != nil {
		return AggregationResult{}, err
	}
	coverage := delegationCoverageSummary(delegations, results)

	aggregation := AggregationResult{
		ConvergenceMode:          mode,
		Confidence:               bestConfidence(results),
		EvidenceRefs:             collectEvidenceRefs(results),
		UnresolvedRisks:          collectUnresolvedRisks(results),
		ProposedNextActions:      collectNextActions(results),
		ProposedMemoryCandidates: collectMemoryCandidates(results),
	}

	if mode == "merge" || mode == "rank" {
		if coverage.coveredDelegations < coverage.expectedDelegations {
			aggregation.Status = "blocked"
			aggregation.TerminalReason = "swarm_results_pending"
			aggregation.Summary = "Awaiting child result artifacts"
		}
	} else if mode == "review_gate" {
		if coverage.coveredVerifierDelegations == 0 {
			aggregation.Status = "blocked"
			aggregation.TerminalReason = "swarm_review_gate_pending_verifier"
			aggregation.Summary = "Swarm review gate is waiting for verifier output"
		} else if coverage.coveredProducerDelegations < coverage.expectedProducerDelegations {
			aggregation.Status = "blocked"
			aggregation.TerminalReason = "swarm_results_pending"
			aggregation.Summary = "Awaiting child result artifacts"
		}
	}
	if mode == "quorum" {
		switch {
		case coverage.coveredDelegations == 0:
			aggregation.Status = "blocked"
			aggregation.TerminalReason = "swarm_results_pending"
			aggregation.Summary = "Awaiting child result artifacts"
		}
	}
	if strings.TrimSpace(aggregation.Status) == "" {
		switch mode {
		case "merge":
			service.applyMergeConvergence(&aggregation, results)
		case "review_gate":
			service.applyReviewGateConvergence(&aggregation, results)
		case "rank":
			service.applyRankConvergence(&aggregation, results)
		case "quorum":
			service.applyQuorumConvergence(&aggregation, results, coverage.expectedDelegations)
		default:
			return AggregationResult{}, fmt.Errorf("%w: %s", ErrUnsupportedConvergenceMode, mode)
		}
	}

	if aggregation.Status == "completed" && len(aggregation.UnresolvedRisks) > 0 {
		aggregation.Status = "blocked"
		aggregation.TerminalReason = "swarm_unresolved_risks"
		aggregation.Summary = fmt.Sprintf("Swarm blocked by unresolved risks: %s", strings.Join(aggregation.UnresolvedRisks, "; "))
	}
	if strings.TrimSpace(aggregation.Status) == "" {
		aggregation.Status = "blocked"
		aggregation.TerminalReason = "swarm_results_pending"
		aggregation.Summary = "Swarm results pending"
	}

	artifactsJSON, err := marshalAggregationArtifacts(aggregation, results)
	if err != nil {
		return AggregationResult{}, err
	}

	parentTask, err = service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID:                 parentTaskID,
		Status:                 aggregation.Status,
		Summary:                aggregation.Summary,
		TerminalReason:         aggregation.TerminalReason,
		ArtifactsJSON:          artifactsJSON,
		AllowedCurrentStatuses: []string{"queued", "running", "blocked"},
	})
	if err != nil {
		return AggregationResult{}, err
	}

	aggregation.ParentTask = parentTask
	return aggregation, nil
}

func (service Service) loadDelegationResults(ctx context.Context, delegations []sqlite.Delegation) ([]delegationResult, error) {
	results := make([]delegationResult, 0, len(delegations))
	for _, delegation := range delegations {
		artifacts, err := service.Store.ListDelegationArtifacts(ctx, sqlite.ListDelegationArtifactsParams{
			DelegationID: delegation.ID,
			ArtifactType: "result",
		})
		if err != nil {
			return nil, err
		}

		if len(artifacts) == 0 {
			continue
		}

		artifact := artifacts[len(artifacts)-1]
		envelope, err := decodeDelegationResultEnvelope(artifact.DetailsJSON)
		if err != nil {
			return nil, fmt.Errorf("decode delegation artifact %d: %w", artifact.ID, err)
		}
		results = append(results, delegationResult{
			Delegation: delegation,
			Artifact:   artifact,
			Envelope:   envelope,
			Confidence: confidenceScore(envelope.Confidence),
		})
	}
	return results, nil
}

func (service Service) applyMergeConvergence(result *AggregationResult, outcomes []delegationResult) {
	if len(outcomes) == 0 {
		result.Status = "blocked"
		result.TerminalReason = "swarm_results_pending"
		result.Summary = "Awaiting child result artifacts"
		return
	}

	seenTargets := make(map[string]struct{}, len(outcomes))
	summaries := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		target := strings.TrimSpace(outcome.Delegation.ArtifactTarget)
		if target != "" {
			if _, exists := seenTargets[target]; exists {
				result.Status = "blocked"
				result.TerminalReason = "swarm_merge_conflict"
				result.Summary = fmt.Sprintf("Swarm merge blocked by overlapping artifact target %q", target)
				return
			}
			seenTargets[target] = struct{}{}
		}
		summaries = append(summaries, outcome.Artifact.Summary)
	}

	result.Status = "completed"
	result.Summary = strings.Join(uniqueNonEmptyStrings(summaries), " + ")
	result.Confidence = bestConfidence(outcomes)
}

func (service Service) applyReviewGateConvergence(result *AggregationResult, outcomes []delegationResult) {
	if len(outcomes) == 0 {
		result.Status = "blocked"
		result.TerminalReason = "swarm_results_pending"
		result.Summary = "Awaiting child result artifacts"
		return
	}

	var (
		verifier          delegationResult
		hasVerifier       bool
		producerSummaries []string
	)
	for _, outcome := range outcomes {
		if isVerifierDelegation(outcome.Delegation) {
			if !hasVerifier || outcome.Confidence >= verifier.Confidence {
				verifier = outcome
				hasVerifier = true
			}
			continue
		}
		producerSummaries = append(producerSummaries, outcome.Artifact.Summary)
	}

	if !hasVerifier {
		result.Status = "blocked"
		result.TerminalReason = "swarm_review_gate_pending_verifier"
		result.Summary = "Swarm review gate is waiting for verifier output"
		return
	}

	result.VerifierDelegationID = &verifier.Delegation.ID
	if len(producerSummaries) == 0 {
		result.Status = "blocked"
		result.TerminalReason = "swarm_results_pending"
		result.Summary = "Awaiting producer result artifacts"
		return
	}
	result.Status = "completed"
	result.Summary = strings.Join(uniqueNonEmptyStrings(producerSummaries), " + ")
	result.Confidence = verifier.Confidence
}

func (service Service) applyRankConvergence(result *AggregationResult, outcomes []delegationResult) {
	if len(outcomes) == 0 {
		result.Status = "blocked"
		result.TerminalReason = "swarm_results_pending"
		result.Summary = "Awaiting child result artifacts"
		return
	}

	winner := outcomes[0]
	for _, outcome := range outcomes[1:] {
		if outcome.Confidence > winner.Confidence {
			winner = outcome
		}
	}

	result.Status = "completed"
	result.Summary = winner.Artifact.Summary
	result.WinningDelegationID = &winner.Delegation.ID
	result.Confidence = winner.Confidence
}

func (service Service) applyQuorumConvergence(result *AggregationResult, outcomes []delegationResult, expectedDelegations int) {
	if len(outcomes) == 0 {
		result.Status = "blocked"
		result.TerminalReason = "swarm_results_pending"
		result.Summary = "Awaiting child result artifacts"
		return
	}

	threshold := expectedDelegations/2 + 1
	type quorumBucket struct {
		count  int
		result delegationResult
	}
	buckets := make(map[string]quorumBucket, len(outcomes))
	for _, outcome := range outcomes {
		key := strings.ToLower(strings.TrimSpace(outcome.Artifact.Summary))
		bucket := buckets[key]
		bucket.count++
		if bucket.count == 1 || outcome.Confidence > bucket.result.Confidence {
			bucket.result = outcome
		}
		buckets[key] = bucket
	}

	for _, bucket := range buckets {
		if bucket.count >= threshold {
			result.Status = "completed"
			result.Summary = bucket.result.Artifact.Summary
			result.WinningDelegationID = &bucket.result.Delegation.ID
			result.Confidence = bucket.result.Confidence
			return
		}
	}

	result.Status = "blocked"
	result.TerminalReason = "swarm_quorum_not_reached"
	result.Summary = "Swarm quorum not reached"
}

func decodeDelegationResultEnvelope(detailsJSON string) (delegationResultEnvelope, error) {
	trimmed := strings.TrimSpace(detailsJSON)
	if trimmed == "" || trimmed == "{}" {
		return delegationResultEnvelope{}, nil
	}

	var envelope delegationResultEnvelope
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return delegationResultEnvelope{}, err
	}
	return envelope, nil
}

func confidenceScore(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "high":
			return 0.9
		case "medium":
			return 0.6
		case "low":
			return 0.3
		default:
			return 0
		}
	default:
		return 0
	}
}

func isVerifierDelegation(delegation sqlite.Delegation) bool {
	role := strings.ToLower(strings.TrimSpace(delegation.Role))
	actionKey := strings.ToLower(strings.TrimSpace(delegation.ActionKey))
	return strings.Contains(role, "review") || strings.Contains(role, "verif") ||
		strings.Contains(actionKey, "review") || strings.Contains(actionKey, "verif")
}

func collectEvidenceRefs(outcomes []delegationResult) []string {
	collected := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		collected = append(collected, outcome.Envelope.EvidenceRefs...)
	}
	return uniqueNonEmptyStrings(collected)
}

func collectUnresolvedRisks(outcomes []delegationResult) []string {
	collected := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		collected = append(collected, outcome.Envelope.UnresolvedRisks...)
	}
	return uniqueNonEmptyStrings(collected)
}

func collectNextActions(outcomes []delegationResult) []string {
	collected := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		collected = append(collected, outcome.Envelope.ProposedNextActions...)
	}
	return uniqueNonEmptyStrings(collected)
}

func collectMemoryCandidates(outcomes []delegationResult) []string {
	collected := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		collected = append(collected, outcome.Envelope.ProposedMemoryCandidates...)
	}
	return uniqueNonEmptyStrings(collected)
}

type delegationCoverage struct {
	expectedDelegations         int
	coveredDelegations          int
	expectedProducerDelegations int
	coveredProducerDelegations  int
	expectedVerifierDelegations int
	coveredVerifierDelegations  int
}

func delegationCoverageSummary(delegations []sqlite.Delegation, outcomes []delegationResult) delegationCoverage {
	coverage := delegationCoverage{
		expectedDelegations: len(delegations),
	}
	outcomesByDelegation := make(map[int64]delegationResult, len(outcomes))
	for _, outcome := range outcomes {
		outcomesByDelegation[outcome.Delegation.ID] = outcome
	}

	for _, delegation := range delegations {
		if isVerifierDelegation(delegation) {
			coverage.expectedVerifierDelegations++
		} else {
			coverage.expectedProducerDelegations++
		}
		if _, ok := outcomesByDelegation[delegation.ID]; !ok {
			continue
		}
		coverage.coveredDelegations++
		if isVerifierDelegation(delegation) {
			coverage.coveredVerifierDelegations++
		} else {
			coverage.coveredProducerDelegations++
		}
	}

	return coverage
}

func isAggregationOutcomeArtifactType(artifactType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(artifactType))
	if normalized == "" {
		return true
	}
	if strings.Contains(normalized, "plan") || strings.Contains(normalized, "progress") {
		return false
	}
	return true
}

func bestConfidence(outcomes []delegationResult) float64 {
	best := 0.0
	for _, outcome := range outcomes {
		if outcome.Confidence > best {
			best = outcome.Confidence
		}
	}
	return best
}

func bestVerifierArtifactIndex(outcomes []delegationResult) int {
	bestIndex := -1
	bestConfidence := -1.0
	for i, outcome := range outcomes {
		if !isVerifierArtifactType(outcome.Artifact.ArtifactType) {
			continue
		}
		if bestIndex < 0 || outcome.Confidence > bestConfidence {
			bestIndex = i
			bestConfidence = outcome.Confidence
		}
	}
	return bestIndex
}

func isVerifierArtifactType(artifactType string) bool {
	artifactType = strings.ToLower(strings.TrimSpace(artifactType))
	return strings.Contains(artifactType, "verif") || strings.Contains(artifactType, "review")
}

func marshalAggregationArtifacts(result AggregationResult, outcomes []delegationResult) (string, error) {
	artifactIDs := make([]int64, 0, len(outcomes))
	delegationIDs := make([]int64, 0, len(outcomes))
	for _, outcome := range outcomes {
		artifactIDs = append(artifactIDs, outcome.Artifact.ID)
		delegationIDs = append(delegationIDs, outcome.Delegation.ID)
	}

	payload := []map[string]any{
		{
			"type":                       "swarm_aggregation",
			"convergence_mode":           result.ConvergenceMode,
			"status":                     result.Status,
			"summary":                    result.Summary,
			"confidence":                 result.Confidence,
			"artifact_ids":               artifactIDs,
			"delegation_ids":             delegationIDs,
			"evidence_refs":              result.EvidenceRefs,
			"unresolved_risks":           result.UnresolvedRisks,
			"proposed_next_actions":      result.ProposedNextActions,
			"proposed_memory_candidates": result.ProposedMemoryCandidates,
		},
	}
	if result.WinningDelegationID != nil {
		payload[0]["winning_delegation_id"] = *result.WinningDelegationID
	}
	if result.VerifierDelegationID != nil {
		payload[0]["verifier_delegation_id"] = *result.VerifierDelegationID
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
