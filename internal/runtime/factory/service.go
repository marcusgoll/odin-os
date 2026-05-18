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
	admittedPhase       = "admitted"
)

type Service struct {
	Store *sqlite.Store
	Jobs  jobs.Service
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
	Task     sqlite.Task
	Trigger  string
	Autonomy string
	Phase    string
}

type laneArtifact struct {
	Type       string `json:"type"`
	ProfileKey string `json:"profile_key"`
	Trigger    string `json:"trigger"`
	Autonomy   string `json:"autonomy"`
	Phase      string `json:"phase"`
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

	artifactsJSON, err := factoryArtifactsJSON(operatorTrigger, AutonomyMergeWhenGreen, admittedPhase)
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
		Phase:    admittedPhase,
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
	artifactsJSON, err := factoryArtifactsJSON(intakeReviewTrigger, AutonomyMergeWhenGreen, admittedPhase)
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
	return AdmissionResult{
		Task:     result.Task,
		Created:  result.Created,
		Trigger:  intakeReviewTrigger,
		Autonomy: AutonomyMergeWhenGreen,
		Phase:    admittedPhase,
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
	artifact, err := factoryArtifactFromTask(task)
	if err != nil {
		return StatusResult{}, err
	}
	return StatusResult{
		Task:     task,
		Trigger:  artifact.Trigger,
		Autonomy: artifact.Autonomy,
		Phase:    artifact.Phase,
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

func factoryArtifactFromTask(task sqlite.Task) (laneArtifact, error) {
	var artifacts []laneArtifact
	if err := json.Unmarshal([]byte(task.ArtifactsJSON), &artifacts); err != nil {
		return laneArtifact{}, fmt.Errorf("invalid factory task %q: malformed factory artifacts", task.Key)
	}
	for _, candidate := range artifacts {
		if strings.TrimSpace(candidate.Type) != WorkKindFactoryLane {
			continue
		}
		if strings.TrimSpace(candidate.ProfileKey) != ProfileKey {
			return laneArtifact{}, fmt.Errorf("invalid factory task %q: factory artifact profile %q is not %q", task.Key, candidate.ProfileKey, ProfileKey)
		}
		if strings.TrimSpace(candidate.Trigger) == "" || strings.TrimSpace(candidate.Autonomy) == "" || strings.TrimSpace(candidate.Phase) == "" {
			return laneArtifact{}, fmt.Errorf("invalid factory task %q: incomplete factory artifact", task.Key)
		}
		return candidate, nil
	}
	return laneArtifact{}, fmt.Errorf("invalid factory task %q: missing factory lane artifact", task.Key)
}

func defaultRequestedBy(value string) string {
	if requestedBy := strings.TrimSpace(value); requestedBy != "" {
		return requestedBy
	}
	return "operator"
}
