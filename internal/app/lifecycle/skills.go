package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"odin-os/internal/app/bootstrap"
	"odin-os/internal/cli/commands"
	"odin-os/internal/cli/scope"
	clistate "odin-os/internal/cli/state"
	"odin-os/internal/core/projects"
	"odin-os/internal/core/skillbinding"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/skills"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/telemetry/logs"
)

func runSkills(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(remaining) == 0 {
		return fmt.Errorf("usage: odin skills [list|get|create|update|delete|invoke|run|artifacts|artifact] ... [--json]")
	}

	logger := logs.Logger{Writer: os.Stderr}
	observers := skills.MultiObserver{
		skills.LoggerObserver{
			Logger: logger,
			Scope:  "repo",
		},
	}
	if app.Store != nil {
		observers = append(observers, skills.ObserverFunc(func(ctx context.Context, event skills.Event) {
			if err := app.Store.RecordSkillLifecycleEvent(ctx, sqlite.RecordSkillLifecycleEventParams{
				SkillKey:         event.SkillKey,
				Scope:            event.Scope,
				ProjectID:        event.ProjectID,
				Operation:        string(event.Operation),
				Outcome:          string(event.Outcome),
				ExecutionProfile: event.ExecutionProfile,
				RuntimeEffect:    event.RuntimeEffect,
				Version:          event.Version,
				HandlerType:      event.HandlerType,
				HandlerRef:       event.HandlerRef,
				Permissions:      append([]string(nil), event.Permissions...),
				DurationMS:       event.Duration.Milliseconds(),
				ErrorCode:        event.ErrorCode,
				ErrorText:        event.ErrorText,
			}); err != nil {
				_ = logger.Log(logs.Record{
					Level:     logs.LevelWarn,
					Component: "skills",
					Message:   "skill lifecycle audit append failed",
					Scope: func() string {
						if event.Scope != "" {
							return event.Scope
						}
						return "repo"
					}(),
					Fields: map[string]any{
						"skill_key":         event.SkillKey,
						"operation":         event.Operation,
						"outcome":           event.Outcome,
						"execution_profile": event.ExecutionProfile,
						"error":             err.Error(),
						"error_code":        "skill_audit_append_failed",
					},
				})
			}
		}))
	}

	service := skills.Service{
		RepoRoot:             app.RepoRoot,
		Observer:             observers,
		TransitionAuthorizer: projects.Service{Store: app.Store},
	}
	if app.Store != nil {
		service.ReviewArtifactRecorder = skillReviewArtifactRecorder{Store: app.Store}
	}

	state, err := loadCLIState(app)
	if err != nil {
		return err
	}

	invocationContext := skills.InvocationContext{
		ResolvedScopeKind: string(state.Scope.Kind),
	}

	switch remaining[0] {
	case "list":
		if len(remaining) != 1 {
			return fmt.Errorf("usage: odin skills list [--json]")
		}
		skillList, err := service.List(ctx)
		if err != nil {
			return err
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, commands.SkillsView{Skills: skillList})
		}
		for _, skill := range skillList {
			if _, err := fmt.Fprintf(stdout, "%s %s\n", skill.Key, skill.Version); err != nil {
				return err
			}
		}
		return nil
	case "get":
		if len(remaining) != 2 {
			return fmt.Errorf("usage: odin skills get <key> [--json]")
		}
		skill, err := service.Get(ctx, remaining[1])
		if err != nil {
			return err
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, skill)
		}
		_, err = fmt.Fprintf(stdout, "key=%s version=%s handler=%s\n", skill.Key, skill.Version, skill.HandlerRef)
		return err
	case "create":
		specPath, err := consumeFlagValue(remaining[1:], "--spec")
		if err != nil {
			return err
		}
		spec, err := commands.LoadSkillSpecFile(specPath)
		if err != nil {
			return err
		}
		skill, err := service.Create(ctx, spec)
		if err != nil {
			return err
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, skill)
		}
		_, err = fmt.Fprintf(stdout, "created=%s version=%s\n", skill.Key, skill.Version)
		return err
	case "update":
		if len(remaining) < 2 {
			return fmt.Errorf("usage: odin skills update <key> --spec <path> [--json]")
		}
		specPath, err := consumeFlagValue(remaining[2:], "--spec")
		if err != nil {
			return err
		}
		spec, err := commands.LoadSkillSpecFile(specPath)
		if err != nil {
			return err
		}
		skill, err := service.Update(ctx, remaining[1], spec)
		if err != nil {
			return err
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, skill)
		}
		_, err = fmt.Fprintf(stdout, "updated=%s version=%s\n", skill.Key, skill.Version)
		return err
	case "delete":
		if len(remaining) != 2 {
			return fmt.Errorf("usage: odin skills delete <key> [--json]")
		}
		if err := service.Delete(ctx, remaining[1]); err != nil {
			return err
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, commands.SkillDeleteView{Key: remaining[1], Deleted: true})
		}
		_, err := fmt.Fprintf(stdout, "deleted=%s\n", remaining[1])
		return err
	case "artifacts":
		if len(remaining) != 1 {
			return fmt.Errorf("usage: odin skills artifacts [--json]")
		}
		if app.Store == nil {
			return fmt.Errorf("skill artifacts require runtime store")
		}
		artifacts, err := app.Store.ListSkillArtifacts(ctx, sqlite.ListSkillArtifactsParams{})
		if err != nil {
			return err
		}
		views := make([]skills.ReviewArtifact, 0, len(artifacts))
		for _, artifact := range artifacts {
			views = append(views, renderSkillReviewArtifact(artifact))
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, map[string]any{"artifacts": views})
		}
		for _, artifact := range views {
			if _, err := fmt.Fprintf(stdout, "artifact id=%d skill=%s status=%s type=%s summary=%s\n", artifact.ID, artifact.SkillKey, artifact.Status, artifact.ArtifactType, artifact.Summary); err != nil {
				return err
			}
		}
		return nil
	case "artifact":
		if len(remaining) == 4 && remaining[1] == "review" {
			return runSkillArtifactReview(ctx, app, remaining[2], remaining[3], jsonOutput, stdout)
		}
		if len(remaining) != 3 || remaining[1] != "show" {
			return fmt.Errorf("usage: odin skills artifact show <id> [--json] OR odin skills artifact review <accept|reject|archive> <id> [--json]")
		}
		if app.Store == nil {
			return fmt.Errorf("skill artifact show requires runtime store")
		}
		artifactID, err := strconv.ParseInt(remaining[2], 10, 64)
		if err != nil || artifactID <= 0 {
			return fmt.Errorf("skill artifact id must be a positive integer")
		}
		artifact, err := app.Store.GetSkillArtifact(ctx, artifactID)
		if err != nil {
			return err
		}
		view := renderSkillReviewArtifact(artifact)
		if jsonOutput {
			return commands.WriteJSON(stdout, view)
		}
		_, err = fmt.Fprintf(stdout, "artifact id=%d skill=%s status=%s type=%s summary=%s\n", view.ID, view.SkillKey, view.Status, view.ArtifactType, view.Summary)
		return err
	case "invoke":
		if len(remaining) < 2 {
			return fmt.Errorf("usage: odin skills invoke <key> [--input <json>] [--json]")
		}
		inputValue, err := optionalFlagValue(remaining[2:], "--input")
		if err != nil {
			return err
		}
		input, err := commands.DecodeSkillInput(inputValue)
		if err != nil {
			return err
		}
		if state.Scope.Kind == scope.ScopeProject || state.Scope.Kind == scope.ScopeOdinCore {
			manifest, ok := app.Registry.Lookup(state.Scope.ProjectKey)
			if !ok {
				return fmt.Errorf("unknown project: %s", state.Scope.ProjectKey)
			}

			project, err := projects.Service{Store: app.Store}.RegisterManagedProject(ctx, manifest)
			if err != nil {
				return err
			}

			invocationContext.Project = &skills.InvocationProject{
				ID:            project.ID,
				Key:           project.Key,
				SystemProject: manifest.SystemProject,
			}
			invocationContext.Manifest = manifest
		}
		response, err := service.Invoke(ctx, skills.InvokeRequest{
			Key:     remaining[1],
			Input:   input,
			Context: invocationContext,
		})
		if err != nil {
			return err
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, response)
		}
		_, err = fmt.Fprintf(stdout, "skill=%s status=%s summary=%s\n", response.SkillKey, response.Status, response.Summary)
		return err
	case "run":
		if len(remaining) != 2 {
			return fmt.Errorf("usage: odin skills run <task-id|task-key> [--json]")
		}
		return runSkillInvocationBinding(ctx, app, service, remaining[1], jsonOutput, stdout)
	default:
		return fmt.Errorf("unknown skills subcommand: %s", remaining[0])
	}
}

type skillInvocationRunView struct {
	TaskID        int64                 `json:"task_id"`
	TaskKey       string                `json:"task_key"`
	TaskStatus    string                `json:"task_status"`
	SkillKey      string                `json:"skill_key"`
	Status        string                `json:"status"`
	Summary       string                `json:"summary"`
	RuntimeEffect string                `json:"runtime_effect"`
	ArtifactID    int64                 `json:"artifact_id,omitempty"`
	ReviewID      string                `json:"review_id,omitempty"`
	Response      skills.InvokeResponse `json:"response"`
}

func runSkillInvocationBinding(ctx context.Context, app bootstrap.App, service skills.Service, taskRef string, jsonOutput bool, stdout io.Writer) error {
	if app.Store == nil {
		return fmt.Errorf("skill invocation binding requires runtime store")
	}
	task, err := findSkillInvocationTask(ctx, app.Store, taskRef)
	if err != nil {
		return err
	}
	if task.Status != "queued" {
		return fmt.Errorf("skill invocation task %s must be queued, got %s", task.Key, task.Status)
	}
	binding, ok, err := skillbinding.DecodeArtifacts(task.ArtifactsJSON)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("task %s has no skill invocation binding", task.Key)
	}
	project, err := app.Store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return err
	}
	manifest, ok := app.Registry.Lookup(project.Key)
	if !ok {
		return fmt.Errorf("unknown project %q", project.Key)
	}
	input, err := skillbinding.InputMap(binding)
	if err != nil {
		return err
	}
	response, err := service.Invoke(ctx, skills.InvokeRequest{
		Key:   binding.SkillKey,
		Input: input,
		Context: skills.InvocationContext{
			ResolvedScopeKind: task.Scope,
			Project: &skills.InvocationProject{
				ID:            project.ID,
				Key:           project.Key,
				SystemProject: manifest.SystemProject,
			},
			Manifest: manifest,
		},
	})
	if err != nil {
		return err
	}
	artifactID := int64(0)
	reviewID := ""
	if response.ReviewArtifact != nil {
		artifactID = response.ReviewArtifact.ID
		reviewID = fmt.Sprintf("skill-artifact:%d", artifactID)
	}
	updated, err := app.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID:         task.ID,
		Status:         "completed",
		Summary:        response.Summary,
		TerminalReason: "skill_invocation_completed",
		ArtifactsJSON:  task.ArtifactsJSON,
	})
	if err != nil {
		return err
	}
	view := skillInvocationRunView{
		TaskID:        updated.ID,
		TaskKey:       updated.Key,
		TaskStatus:    updated.Status,
		SkillKey:      response.SkillKey,
		Status:        response.Status,
		Summary:       response.Summary,
		RuntimeEffect: response.RuntimeEffect,
		ArtifactID:    artifactID,
		ReviewID:      reviewID,
		Response:      response,
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout, "task=%s skill=%s status=%s artifact_id=%d\n", view.TaskKey, view.SkillKey, view.Status, view.ArtifactID)
	return err
}

func findSkillInvocationTask(ctx context.Context, store *sqlite.Store, ref string) (sqlite.Task, error) {
	ref = strings.TrimSpace(ref)
	idRef := strings.TrimPrefix(ref, "task-")
	if id, err := strconv.ParseInt(idRef, 10, 64); err == nil && id > 0 {
		return store.GetTask(ctx, id)
	}
	views, err := projections.ListTaskStatusViews(ctx, store.DB())
	if err != nil {
		return sqlite.Task{}, err
	}
	for _, view := range views {
		if view.TaskKey == ref {
			return store.GetTask(ctx, view.TaskID)
		}
	}
	return sqlite.Task{}, fmt.Errorf("task %q not found", ref)
}

type skillArtifactReviewDecisionView struct {
	Artifact    skills.ReviewArtifact      `json:"artifact"`
	Decision    string                     `json:"decision"`
	WorkCreated bool                       `json:"work_created"`
	Repeated    bool                       `json:"repeated"`
	WorkItem    *skillArtifactWorkItemView `json:"work_item,omitempty"`
}

type skillArtifactWorkItemView struct {
	ID          int64  `json:"id"`
	Key         string `json:"key"`
	Status      string `json:"status"`
	RequestedBy string `json:"requested_by"`
}

func runSkillArtifactReview(ctx context.Context, app bootstrap.App, action string, artifactRef string, jsonOutput bool, stdout io.Writer) error {
	if app.Store == nil {
		return fmt.Errorf("skill artifact review requires runtime store")
	}
	artifactID, err := strconv.ParseInt(artifactRef, 10, 64)
	if err != nil || artifactID <= 0 {
		return fmt.Errorf("skill artifact id must be a positive integer")
	}
	artifact, err := app.Store.GetSkillArtifact(ctx, artifactID)
	if err != nil {
		return err
	}

	decision := ""
	status := ""
	reason := ""
	var task *sqlite.Task
	workCreated := false
	repeated := false

	switch action {
	case "accept":
		decision = "accepted"
		status = "accepted"
		reason = "skill artifact accepted by operator"
		if artifact.Status == "accepted" {
			repeated = true
			loaded, created, err := createTaskFromAcceptedSkillArtifact(ctx, app, artifact)
			if err != nil {
				return err
			}
			task = &loaded
			workCreated = created
			break
		}
		if artifact.Status != "review_required" {
			return fmt.Errorf("skill artifact %d cannot be accepted from status %s", artifact.ID, artifact.Status)
		}
		createdTask, created, err := createTaskFromAcceptedSkillArtifact(ctx, app, artifact)
		if err != nil {
			return err
		}
		task = &createdTask
		workCreated = created
	case "reject":
		decision = "rejected"
		status = "rejected"
		reason = "skill artifact rejected by operator"
		if artifact.Status == "rejected" {
			repeated = true
			break
		}
		if artifact.Status != "review_required" {
			return fmt.Errorf("skill artifact %d cannot be rejected from status %s", artifact.ID, artifact.Status)
		}
	case "archive":
		decision = "archived"
		status = "archived"
		reason = "skill artifact archived by operator"
		if artifact.Status == "archived" {
			repeated = true
			break
		}
		if artifact.Status != "review_required" {
			return fmt.Errorf("skill artifact %d cannot be archived from status %s", artifact.ID, artifact.Status)
		}
	default:
		return fmt.Errorf("usage: odin skills artifact review <accept|reject|archive> <id> [--json]")
	}

	var followOnTaskID *int64
	followOnTaskKey := ""
	followOnTaskState := ""
	if task != nil {
		id := task.ID
		followOnTaskID = &id
		followOnTaskKey = task.Key
		followOnTaskState = task.Status
	}
	updated, err := app.Store.ReviewSkillArtifact(ctx, sqlite.ReviewSkillArtifactParams{
		ArtifactID:        artifact.ID,
		Decision:          decision,
		Status:            status,
		ReviewedBy:        "operator",
		Reason:            reason,
		Repeated:          repeated,
		WorkCreated:       workCreated,
		FollowOnTaskID:    followOnTaskID,
		FollowOnTaskKey:   followOnTaskKey,
		FollowOnTaskState: followOnTaskState,
	})
	if err != nil {
		return err
	}

	result := skillArtifactReviewDecisionView{
		Artifact:    renderSkillReviewArtifact(updated),
		Decision:    decision,
		WorkCreated: workCreated,
		Repeated:    repeated,
	}
	if task != nil {
		result.WorkItem = &skillArtifactWorkItemView{
			ID:          task.ID,
			Key:         task.Key,
			Status:      task.Status,
			RequestedBy: task.RequestedBy,
		}
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, result)
	}
	workKey := "none"
	if task != nil {
		workKey = task.Key
	}
	_, err = fmt.Fprintf(stdout, "skill_artifact=%d decision=%s status=%s work_created=%t work_item=%s\n", artifact.ID, decision, updated.Status, workCreated, workKey)
	return err
}

func createTaskFromAcceptedSkillArtifact(ctx context.Context, app bootstrap.App, artifact sqlite.SkillArtifact) (sqlite.Task, bool, error) {
	if artifact.Scope != "project" || artifact.ProjectID == nil {
		return sqlite.Task{}, false, fmt.Errorf("skill artifact %d has no project scope for work promotion", artifact.ID)
	}
	project, err := app.Store.GetProject(ctx, *artifact.ProjectID)
	if err != nil {
		return sqlite.Task{}, false, err
	}
	manifest, ok := app.Registry.Lookup(project.Key)
	if !ok {
		return sqlite.Task{}, false, fmt.Errorf("unknown project %q", project.Key)
	}
	resolved := scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey:    manifest.Key,
			SystemProject: manifest.SystemProject,
		},
	})
	title := strings.TrimSpace(artifact.Summary)
	if title == "" {
		title = fmt.Sprintf("Review skill artifact %d", artifact.ID)
	}
	result, err := jobs.Service{
		Store:       app.Store,
		Registry:    app.Registry,
		Transitions: projects.Service{Store: app.Store},
		Now:         time.Now,
	}.CreateTaskOnce(ctx, jobs.CreateTaskParams{
		Resolved:              resolved,
		Title:                 title,
		RequestedBy:           fmt.Sprintf("skill_artifact_review:%d", artifact.ID),
		Key:                   skillArtifactWorkItemKey(artifact.ID),
		ExecutionIntent:       "read_only",
		ExecutionIntentSource: "skill_artifact",
	})
	return result.Task, result.Created, err
}

func skillArtifactWorkItemKey(id int64) string {
	return fmt.Sprintf("skill-artifact-%d", id)
}

type skillReviewArtifactRecorder struct {
	Store *sqlite.Store
}

func (recorder skillReviewArtifactRecorder) RecordReviewArtifact(ctx context.Context, input skills.RecordReviewArtifactInput) (skills.ReviewArtifact, error) {
	outputJSON := "{}"
	if len(input.Output) != 0 {
		encoded, err := json.Marshal(input.Output)
		if err != nil {
			return skills.ReviewArtifact{}, err
		}
		outputJSON = string(encoded)
	}
	permissionsJSON := "[]"
	if len(input.Permissions) != 0 {
		encoded, err := json.Marshal(input.Permissions)
		if err != nil {
			return skills.ReviewArtifact{}, err
		}
		permissionsJSON = string(encoded)
	}

	artifact, err := recorder.Store.CreateSkillArtifact(ctx, sqlite.CreateSkillArtifactParams{
		SkillKey:         input.SkillKey,
		Scope:            input.Scope,
		ProjectID:        input.ProjectID,
		Status:           "review_required",
		ArtifactType:     "skill_output",
		Summary:          input.Summary,
		OutputJSON:       outputJSON,
		RawOutput:        input.RawOutput,
		HandlerRef:       input.HandlerRef,
		ExecutionProfile: input.ExecutionProfile,
		PermissionsJSON:  permissionsJSON,
	})
	if err != nil {
		return skills.ReviewArtifact{}, err
	}
	return renderSkillReviewArtifact(artifact), nil
}

func renderSkillReviewArtifact(artifact sqlite.SkillArtifact) skills.ReviewArtifact {
	var permissions []string
	_ = json.Unmarshal([]byte(artifact.PermissionsJSON), &permissions)
	return skills.ReviewArtifact{
		ID:               artifact.ID,
		SkillKey:         artifact.SkillKey,
		Scope:            artifact.Scope,
		ProjectID:        artifact.ProjectID,
		Status:           artifact.Status,
		ArtifactType:     artifact.ArtifactType,
		Summary:          artifact.Summary,
		OutputJSON:       artifact.OutputJSON,
		RawOutput:        artifact.RawOutput,
		HandlerRef:       artifact.HandlerRef,
		ExecutionProfile: artifact.ExecutionProfile,
		Permissions:      permissions,
		ReviewDecision:   artifact.ReviewDecision,
		ReviewedAt:       artifact.ReviewedAt,
		ReviewedBy:       artifact.ReviewedBy,
		ReviewReason:     artifact.ReviewReason,
		FollowOnTaskID:   artifact.FollowOnTaskID,
		FollowOnTaskKey:  artifact.FollowOnTaskKey,
		CreatedAt:        artifact.CreatedAt,
		UpdatedAt:        artifact.UpdatedAt,
	}
}

func consumeFlagValue(args []string, flag string) (string, error) {
	value, err := optionalFlagValue(args, flag)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("%s is required", flag)
	}
	return value, nil
}

func optionalFlagValue(args []string, flag string) (string, error) {
	var value string
	for index := 0; index < len(args); index++ {
		if args[index] != flag {
			continue
		}
		if value != "" {
			return "", fmt.Errorf("duplicate %s flag", flag)
		}
		if index+1 >= len(args) {
			return "", fmt.Errorf("%s requires a value", flag)
		}
		value = args[index+1]
		index++
	}
	return value, nil
}

func consumeJSONFlag(args []string) (bool, []string, error) {
	jsonOutput := false
	remaining := make([]string, 0, len(args))
	for _, arg := range args {
		if arg != "--json" {
			remaining = append(remaining, arg)
			continue
		}
		if jsonOutput {
			return false, nil, fmt.Errorf("duplicate --json flag")
		}
		jsonOutput = true
	}
	return jsonOutput, remaining, nil
}

func loadCLIState(app bootstrap.App) (clistate.State, error) {
	cache, err := app.SessionStore.Load()
	if err != nil {
		return clistate.State{}, err
	}
	return clistate.ResolveStartupState(cache, app.Registry), nil
}
