package factory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	scope "odin-os/internal/cli/scope"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

const WorkKindFactoryLane = "factory_lane"
const ProfileKey = "software-factory-lane-workflow"
const AutonomyMergeWhenGreen = "merge_when_green"

const (
	operatorTrigger     = "operator"
	intakeReviewTrigger = "intake_review"
)

type Phase string

const (
	PhaseAdmitted           Phase = "admitted"
	PhaseSpecification      Phase = "specification"
	PhaseImplementationPlan Phase = "implementation_plan"
	PhaseImplementation     Phase = "implementation"
	PhaseVerification       Phase = "verification"
	PhaseReview             Phase = "review"
	PhasePRHandoff          Phase = "pr_handoff"
	PhaseGreenCheckWait     Phase = "green_check_wait"
	PhaseMerge              Phase = "merge"
	PhaseCloseout           Phase = "closeout"
)

type Service struct {
	Store             *sqlite.Store
	Jobs              jobs.Service
	PullRequestState  PullRequestStateProvider
	PullRequestMerger PullRequestMerger
}

type AdmitOperatorInput struct {
	ProjectKey  string
	Title       string
	RequestedBy string
}

type AdmissionResult struct {
	Task     sqlite.Task
	Created  bool
	Trigger  string
	Autonomy string
	Phase    string
}

type StatusResult struct {
	Task          sqlite.Task
	Trigger       string
	Autonomy      string
	Phase         string
	KnownPhases   []string
	LatestRunID   *int64
	PRHandoffID   string
	BlockedReason string
}

type PhaseEvidenceInput struct {
	TaskID  int64
	RunID   *int64
	Phase   Phase
	Summary string
	Details map[string]string
}

type PullRequestStateProvider interface {
	Read(ctx context.Context, handoff sqlite.PullRequestHandoff) (PullRequestState, error)
}

type PullRequestMerger interface {
	Merge(ctx context.Context, handoff sqlite.PullRequestHandoff, mode string) (MergeResult, error)
}

type PullRequestState struct {
	RequiredChecksGreen       bool
	BranchProtectionSatisfied bool
	Mergeable                 bool
	Stale                     bool
	UnresolvedReviewBlockers  []string
}

type MergeResult struct {
	Merged    bool
	CommitSHA string
	URL       string
}

type MergeGateResult struct {
	Task            sqlite.Task
	Handoff         sqlite.PullRequestHandoff
	State           PullRequestState
	Merged          bool
	CommitSHA       string
	URL             string
	DeployHandoffID string
	Phase           string
	BlockedReason   string
}

type laneArtifact struct {
	Type          string            `json:"type"`
	ProfileKey    string            `json:"profile_key"`
	Trigger       string            `json:"trigger,omitempty"`
	Autonomy      string            `json:"autonomy,omitempty"`
	Phase         string            `json:"phase"`
	Summary       string            `json:"summary,omitempty"`
	Details       map[string]string `json:"details,omitempty"`
	RunID         *int64            `json:"run_id,omitempty"`
	PRHandoffID   string            `json:"pr_handoff_id,omitempty"`
	BlockedReason string            `json:"blocked_reason,omitempty"`
}

func (service Service) AdmitOperatorStart(ctx context.Context, input AdmitOperatorInput) (AdmissionResult, error) {
	projectKey := strings.TrimSpace(input.ProjectKey)
	if projectKey == "" {
		return AdmissionResult{}, fmt.Errorf("factory start requires project key")
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return AdmissionResult{}, fmt.Errorf("factory start requires title")
	}

	jobsService := service.jobsService()
	resolved := scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: projectKey,
	}
	if projectKey == "odin-core" {
		resolved.Kind = scope.ScopeOdinCore
	}

	artifactsJSON, err := factoryArtifactsJSON(operatorTrigger, AutonomyMergeWhenGreen, string(PhaseAdmitted))
	if err != nil {
		return AdmissionResult{}, err
	}
	executionIntent := "mutation"
	executionIntentSource := "factory_lane:operator"
	manifest, ok := jobsService.Registry.Lookup(projectKey)
	if !ok {
		return AdmissionResult{}, fmt.Errorf("unknown project %q", projectKey)
	}
	if jobs.TitleRequiresApproval(manifest, title) {
		executionIntent = ""
		executionIntentSource = ""
	}
	result, err := jobsService.CreateTaskOnce(ctx, jobs.CreateTaskParams{
		Resolved:              resolved,
		Title:                 title,
		RequestedBy:           defaultRequestedBy(input.RequestedBy),
		WorkKind:              WorkKindFactoryLane,
		ArtifactsJSON:         artifactsJSON,
		ExecutionIntent:       executionIntent,
		ExecutionIntentSource: executionIntentSource,
	})
	if err != nil {
		return AdmissionResult{}, err
	}

	return AdmissionResult{
		Task:     result.Task,
		Created:  result.Created,
		Trigger:  operatorTrigger,
		Autonomy: AutonomyMergeWhenGreen,
		Phase:    string(PhaseAdmitted),
	}, nil
}

func (service Service) PromoteAcceptedIntake(ctx context.Context, item sqlite.IntakeItem, title string, acceptance []string) (AdmissionResult, error) {
	projectKey := strings.TrimSpace(item.ScopeKey)
	if item.Scope != "project" || projectKey == "" {
		return AdmissionResult{}, fmt.Errorf("intake intake-%d has no project scope for factory promotion", item.ID)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = strings.TrimSpace(item.Subject)
	}
	if title == "" {
		return AdmissionResult{}, fmt.Errorf("factory intake promotion requires title")
	}

	jobsService := service.jobsService()
	resolved := scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: projectKey,
	}
	if projectKey == "odin-core" {
		resolved.Kind = scope.ScopeOdinCore
	}
	manifest, ok := jobsService.Registry.Lookup(projectKey)
	if !ok {
		return AdmissionResult{}, fmt.Errorf("unknown project %q", projectKey)
	}
	artifactsJSON, err := factoryArtifactsJSON(intakeReviewTrigger, AutonomyMergeWhenGreen, string(PhaseAdmitted))
	if err != nil {
		return AdmissionResult{}, err
	}
	executionIntent := "mutation"
	executionIntentSource := "factory_lane:intake_review"
	if jobs.TitleRequiresApproval(manifest, title) {
		executionIntent = ""
		executionIntentSource = ""
	}
	result, err := jobsService.CreateTaskOnce(ctx, jobs.CreateTaskParams{
		Resolved:              resolved,
		Title:                 title,
		AcceptanceCriteria:    acceptance,
		RequestedBy:           "intake_review:intake-" + strconv.FormatInt(item.ID, 10),
		Key:                   "intake-review-" + strconv.FormatInt(item.ID, 10),
		WorkKind:              WorkKindFactoryLane,
		ArtifactsJSON:         artifactsJSON,
		ExecutionIntent:       executionIntent,
		ExecutionIntentSource: executionIntentSource,
	})
	if err != nil {
		return AdmissionResult{}, err
	}
	if !result.Created && strings.TrimSpace(result.Task.WorkKind) != WorkKindFactoryLane && result.Task.Key == "intake-review-"+strconv.FormatInt(item.ID, 10) {
		upgradeIntent := executionIntent
		upgradeIntentSource := executionIntentSource
		if upgradeIntent == "" {
			upgradeIntent, upgradeIntentSource = jobs.TitleExecutionIntent(manifest, title)
		}
		result.Task, err = service.Store.UpdateTaskFactoryProfile(ctx, sqlite.UpdateTaskFactoryProfileParams{
			TaskID:                result.Task.ID,
			WorkKind:              WorkKindFactoryLane,
			ArtifactsJSON:         artifactsJSON,
			ExecutionIntent:       upgradeIntent,
			ExecutionIntentSource: upgradeIntentSource,
		})
		if err != nil {
			return AdmissionResult{}, err
		}
		if len(acceptance) > 0 {
			result.Task, err = service.Store.UpdateTaskAcceptanceCriteria(ctx, result.Task.ID, acceptance)
			if err != nil {
				return AdmissionResult{}, err
			}
		}
	}
	return AdmissionResult{
		Task:     result.Task,
		Created:  result.Created,
		Trigger:  intakeReviewTrigger,
		Autonomy: AutonomyMergeWhenGreen,
		Phase:    string(PhaseAdmitted),
	}, nil
}

func (service Service) Status(ctx context.Context, taskRef string) (StatusResult, error) {
	if service.Store == nil {
		return StatusResult{}, fmt.Errorf("factory store is required")
	}
	task, err := service.findTask(ctx, taskRef)
	if err != nil {
		return StatusResult{}, err
	}
	if strings.TrimSpace(task.WorkKind) != WorkKindFactoryLane {
		return StatusResult{}, fmt.Errorf("invalid factory task %q: work kind %q is not %q", task.Key, task.WorkKind, WorkKindFactoryLane)
	}
	artifacts, admission, err := factoryArtifactsFromTask(task)
	if err != nil {
		return StatusResult{}, err
	}
	return statusResultFromArtifacts(task, admission, artifacts), nil
}

func (service Service) RecordPhaseEvidence(ctx context.Context, input PhaseEvidenceInput) (StatusResult, error) {
	if service.Store == nil {
		return StatusResult{}, fmt.Errorf("factory store is required")
	}
	if input.TaskID <= 0 {
		return StatusResult{}, fmt.Errorf("factory phase evidence requires task id")
	}
	phase := strings.TrimSpace(string(input.Phase))
	if !validPhase(Phase(phase)) {
		return StatusResult{}, fmt.Errorf("unsupported factory phase %q", input.Phase)
	}

	task, err := service.Store.GetTask(ctx, input.TaskID)
	if err != nil {
		return StatusResult{}, err
	}
	if strings.TrimSpace(task.WorkKind) != WorkKindFactoryLane {
		return StatusResult{}, fmt.Errorf("invalid factory task %q: work kind %q is not %q", task.Key, task.WorkKind, WorkKindFactoryLane)
	}
	artifacts, _, err := factoryArtifactsFromTask(task)
	if err != nil {
		return StatusResult{}, err
	}

	details := cleanDetails(input.Details)
	evidence := laneArtifact{
		Type:          "factory_phase",
		ProfileKey:    ProfileKey,
		Phase:         phase,
		Summary:       strings.TrimSpace(input.Summary),
		Details:       details,
		RunID:         input.RunID,
		PRHandoffID:   strings.TrimSpace(details["pr_handoff_id"]),
		BlockedReason: strings.TrimSpace(details["blocked_reason"]),
	}
	artifacts = append(artifacts, evidence)
	artifactsJSON, err := json.Marshal(artifacts)
	if err != nil {
		return StatusResult{}, err
	}

	if input.RunID != nil {
		detailsJSON, err := json.Marshal(details)
		if err != nil {
			return StatusResult{}, err
		}
		if _, err := service.Store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
			RunID:        *input.RunID,
			ArtifactType: "factory_phase",
			Summary:      evidence.Summary,
			DetailsJSON:  string(detailsJSON),
		}); err != nil {
			return StatusResult{}, err
		}
	}

	updated, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID:         task.ID,
		Status:         task.Status,
		Summary:        task.Summary,
		TerminalReason: task.TerminalReason,
		ArtifactsJSON:  string(artifactsJSON),
	})
	if err != nil {
		return StatusResult{}, err
	}
	artifacts, admission, err := factoryArtifactsFromTask(updated)
	if err != nil {
		return StatusResult{}, err
	}
	return statusResultFromArtifacts(updated, admission, artifacts), nil
}

func (service Service) EvaluateMergeGate(ctx context.Context, taskRef string) (MergeGateResult, error) {
	status, err := service.Status(ctx, taskRef)
	if err != nil {
		return MergeGateResult{}, err
	}
	task := status.Task
	handoffID := strings.TrimSpace(status.PRHandoffID)
	if handoffID == "" {
		return MergeGateResult{}, fmt.Errorf("factory merge-gate requires pull request handoff")
	}
	if approval, err := service.Store.GetLatestTaskApproval(ctx, task.ID); err == nil && approval.Status == "pending" {
		return MergeGateResult{}, fmt.Errorf("factory merge-gate blocked: pending approval %d", approval.ID)
	} else if err != nil && err != sql.ErrNoRows {
		return MergeGateResult{}, err
	}

	handoff, err := service.findPullRequestHandoff(ctx, task, handoffID)
	if err != nil {
		return MergeGateResult{}, err
	}
	stateProvider := service.PullRequestState
	if stateProvider == nil {
		stateProvider = defaultPullRequestStateProvider{}
	}
	state, err := stateProvider.Read(ctx, handoff)
	if err != nil {
		return MergeGateResult{}, err
	}
	if reason := mergeGateBlockedReason(state); reason != "" {
		return MergeGateResult{Task: task, Handoff: handoff, State: state, BlockedReason: reason}, fmt.Errorf("factory merge-gate blocked: %s", reason)
	}
	if len(handoff.Blockers) > 0 {
		reason := "unresolved_pr_blockers"
		return MergeGateResult{Task: task, Handoff: handoff, State: state, BlockedReason: reason}, fmt.Errorf("factory merge-gate blocked: %s", reason)
	}

	merger := service.PullRequestMerger
	if merger == nil {
		merger = defaultPullRequestMerger{}
	}
	mergeResult, err := merger.Merge(ctx, handoff, "squash")
	if err != nil {
		return MergeGateResult{}, err
	}
	if !mergeResult.Merged {
		return MergeGateResult{Task: task, Handoff: handoff, State: state, BlockedReason: "provider_merge_not_confirmed"}, fmt.Errorf("factory merge-gate blocked: provider_merge_not_confirmed")
	}

	if _, err := service.RecordPhaseEvidence(ctx, PhaseEvidenceInput{
		TaskID:  task.ID,
		Phase:   PhaseMerge,
		Summary: "pull request merged when green",
		Details: map[string]string{
			"pr_handoff_id": handoffID,
			"commit_sha":    mergeResult.CommitSHA,
			"merge_url":     mergeResult.URL,
		},
	}); err != nil {
		return MergeGateResult{}, err
	}
	deployHandoffID := fmt.Sprintf("deploy-handoff-%d", task.ID)
	closeout, err := service.RecordPhaseEvidence(ctx, PhaseEvidenceInput{
		TaskID:  task.ID,
		Phase:   PhaseCloseout,
		Summary: "merge complete; deployment handed off for review",
		Details: map[string]string{
			"pr_handoff_id":     handoffID,
			"deploy_handoff_id": deployHandoffID,
			"deploy_action":     "review_required",
		},
	})
	if err != nil {
		return MergeGateResult{}, err
	}
	return MergeGateResult{
		Task:            closeout.Task,
		Handoff:         handoff,
		State:           state,
		Merged:          true,
		CommitSHA:       mergeResult.CommitSHA,
		URL:             mergeResult.URL,
		DeployHandoffID: deployHandoffID,
		Phase:           closeout.Phase,
	}, nil
}

func (service Service) jobsService() jobs.Service {
	jobsService := service.Jobs
	if jobsService.Store == nil {
		jobsService.Store = service.Store
	}
	return jobsService
}

func (service Service) findTask(ctx context.Context, taskRef string) (sqlite.Task, error) {
	ref := strings.TrimSpace(taskRef)
	if ref == "" {
		return sqlite.Task{}, fmt.Errorf("factory status requires task")
	}
	idRef := strings.TrimPrefix(ref, "task-")
	if id, err := strconv.ParseInt(idRef, 10, 64); err == nil && id > 0 {
		return service.Store.GetTask(ctx, id)
	}

	views, err := projections.ListTaskStatusViews(ctx, service.Store.DB())
	if err != nil {
		return sqlite.Task{}, err
	}
	var matched *projections.TaskStatusView
	for _, view := range views {
		if view.TaskKey != ref {
			continue
		}
		if matched != nil {
			return sqlite.Task{}, fmt.Errorf("factory status task key %q is ambiguous", ref)
		}
		candidate := view
		matched = &candidate
	}
	if matched == nil {
		return sqlite.Task{}, sql.ErrNoRows
	}
	return service.Store.GetTask(ctx, matched.TaskID)
}

func (service Service) findPullRequestHandoff(ctx context.Context, task sqlite.Task, handoffID string) (sqlite.PullRequestHandoff, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(handoffID, "pr-handoff-"), 10, 64)
	if err != nil || id <= 0 {
		return sqlite.PullRequestHandoff{}, fmt.Errorf("factory merge-gate pull request handoff %q is not a valid handoff id", handoffID)
	}
	handoffs, err := service.Store.ListPullRequestHandoffs(ctx, sqlite.ListPullRequestHandoffsParams{ProjectID: &task.ProjectID})
	if err != nil {
		return sqlite.PullRequestHandoff{}, err
	}
	for _, handoff := range handoffs {
		if handoff.ID == id {
			return handoff, nil
		}
	}
	return sqlite.PullRequestHandoff{}, fmt.Errorf("factory merge-gate pull request handoff %d not found", id)
}

func factoryArtifactsJSON(trigger, autonomy, phase string) (string, error) {
	payload, err := json.Marshal([]laneArtifact{{
		Type:       WorkKindFactoryLane,
		ProfileKey: ProfileKey,
		Trigger:    trigger,
		Autonomy:   autonomy,
		Phase:      phase,
	}})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func factoryArtifactsFromTask(task sqlite.Task) ([]laneArtifact, laneArtifact, error) {
	var artifacts []laneArtifact
	if err := json.Unmarshal([]byte(task.ArtifactsJSON), &artifacts); err != nil {
		return nil, laneArtifact{}, fmt.Errorf("invalid factory task %q: malformed factory artifacts", task.Key)
	}
	for _, candidate := range artifacts {
		if strings.TrimSpace(candidate.Type) != WorkKindFactoryLane {
			continue
		}
		if strings.TrimSpace(candidate.ProfileKey) != ProfileKey {
			return nil, laneArtifact{}, fmt.Errorf("invalid factory task %q: factory artifact profile %q is not %q", task.Key, candidate.ProfileKey, ProfileKey)
		}
		if strings.TrimSpace(candidate.Trigger) == "" || strings.TrimSpace(candidate.Autonomy) == "" || strings.TrimSpace(candidate.Phase) == "" {
			return nil, laneArtifact{}, fmt.Errorf("invalid factory task %q: incomplete factory artifact", task.Key)
		}
		return artifacts, candidate, nil
	}
	return nil, laneArtifact{}, fmt.Errorf("invalid factory task %q: missing factory lane artifact", task.Key)
}

func statusResultFromArtifacts(task sqlite.Task, admission laneArtifact, artifacts []laneArtifact) StatusResult {
	result := StatusResult{
		Task:        task,
		Trigger:     admission.Trigger,
		Autonomy:    admission.Autonomy,
		Phase:       admission.Phase,
		KnownPhases: []string{admission.Phase},
	}
	seen := map[string]bool{admission.Phase: true}
	for _, artifact := range artifacts {
		if artifact.ProfileKey != ProfileKey {
			continue
		}
		phase := strings.TrimSpace(artifact.Phase)
		switch strings.TrimSpace(artifact.Type) {
		case WorkKindFactoryLane, "factory_phase":
		default:
			continue
		}
		if phase == "" {
			continue
		}
		result.Phase = phase
		if !seen[phase] {
			result.KnownPhases = append(result.KnownPhases, phase)
			seen[phase] = true
		}
		if artifact.RunID != nil {
			runID := *artifact.RunID
			result.LatestRunID = &runID
		}
		if artifact.PRHandoffID != "" {
			result.PRHandoffID = artifact.PRHandoffID
		}
		if artifact.BlockedReason != "" {
			result.BlockedReason = artifact.BlockedReason
		}
		if artifact.Details != nil {
			if value := strings.TrimSpace(artifact.Details["pr_handoff_id"]); value != "" {
				result.PRHandoffID = value
			}
			if value := strings.TrimSpace(artifact.Details["blocked_reason"]); value != "" {
				result.BlockedReason = value
			}
		}
	}
	return result
}

func cleanDetails(details map[string]string) map[string]string {
	if len(details) == 0 {
		return nil
	}
	cleaned := make(map[string]string, len(details))
	for key, value := range details {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		cleaned[key] = value
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

func validPhase(phase Phase) bool {
	switch phase {
	case PhaseAdmitted, PhaseSpecification, PhaseImplementationPlan, PhaseImplementation, PhaseVerification, PhaseReview, PhasePRHandoff, PhaseGreenCheckWait, PhaseMerge, PhaseCloseout:
		return true
	default:
		return false
	}
}

func mergeGateBlockedReason(state PullRequestState) string {
	switch {
	case state.Stale:
		return "stale_pr_state"
	case !state.RequiredChecksGreen:
		return "required_checks_not_green"
	case !state.BranchProtectionSatisfied:
		return "missing_branch_protection"
	case !state.Mergeable:
		return "pr_not_mergeable"
	case len(state.UnresolvedReviewBlockers) > 0:
		return "unresolved_pr_blockers"
	default:
		return ""
	}
}

type defaultPullRequestStateProvider struct{}

func (defaultPullRequestStateProvider) Read(ctx context.Context, handoff sqlite.PullRequestHandoff) (PullRequestState, error) {
	_ = ctx
	state := PullRequestState{Mergeable: true}
	for _, test := range handoff.Tests {
		switch strings.ToLower(strings.TrimSpace(test)) {
		case "checks:green", "required_checks:green":
			state.RequiredChecksGreen = true
		case "branch_protection:satisfied", "branch-protection:satisfied":
			state.BranchProtectionSatisfied = true
		case "mergeable:true":
			state.Mergeable = true
		case "stale:true":
			state.Stale = true
		case "checks:failed", "required_checks:failed":
			state.RequiredChecksGreen = false
		}
	}
	state.UnresolvedReviewBlockers = append([]string(nil), handoff.Blockers...)
	return state, nil
}

type defaultPullRequestMerger struct{}

func (defaultPullRequestMerger) Merge(ctx context.Context, handoff sqlite.PullRequestHandoff, mode string) (MergeResult, error) {
	_ = ctx
	_ = mode
	return MergeResult{
		Merged:    true,
		CommitSHA: fmt.Sprintf("fake-merge-%d", handoff.ID),
		URL:       handoff.URL,
	}, nil
}

func defaultRequestedBy(value string) string {
	if requestedBy := strings.TrimSpace(value); requestedBy != "" {
		return requestedBy
	}
	return "operator"
}
