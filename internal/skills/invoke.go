package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
)

func (service Service) Invoke(ctx context.Context, request InvokeRequest) (_ InvokeResponse, err error) {
	start := time.Now()
	request.Key = strings.TrimSpace(request.Key)
	invocationContext := service.normalizeInvocationContext(request)
	eventScope := invocationContext.ResolvedScopeKind
	var projectID *int64
	if invocationContext.Project != nil && invocationContext.Project.ID != 0 {
		id := invocationContext.Project.ID
		projectID = &id
	}
	var skill Skill
	executionProfile := ""
	runtimeEffect := "not_invoked"
	defer func() {
		service.recordEvent(ctx, Event{
			Operation:        OperationInvoke,
			Outcome:          outcomeForError(err),
			SkillKey:         request.Key,
			Scope:            eventScope,
			ProjectID:        projectID,
			ExecutionProfile: executionProfile,
			RuntimeEffect:    runtimeEffect,
			Version:          skill.Version,
			HandlerType:      skill.HandlerType,
			HandlerRef:       skill.HandlerRef,
			Permissions:      cloneStrings(skill.Permissions),
			Duration:         time.Since(start),
			ErrorCode:        classifySkillError(err),
			ErrorText:        errorText(err),
		})
	}()

	lock, err := acquireSkillRegistryLock(ctx, service.RepoRoot, registryLockShared)
	if err != nil {
		return InvokeResponse{}, err
	}

	snapshot, err := service.loadSnapshotUnlocked()
	if err != nil {
		_ = lock.Release()
		return InvokeResponse{}, err
	}

	skill, err = service.skillFromSnapshot(snapshot, request.Key)
	if err != nil {
		_ = lock.Release()
		return InvokeResponse{}, err
	}

	policy, err := ResolveInvocationPolicy(InvocationPolicyInput{
		ResolvedScopeKind: invocationContext.ResolvedScopeKind,
		Project:           invocationContext.Project,
		Permissions:       skill.Permissions,
	})
	if err != nil {
		_ = lock.Release()
		return InvokeResponse{}, fmt.Errorf("skill %q denied: %w", request.Key, err)
	}

	if policy.Mutating {
		if service.TransitionAuthorizer == nil {
			_ = lock.Release()
			return InvokeResponse{}, fmt.Errorf("skill %q denied: transition authorizer is required for mutating permissions", request.Key)
		}
		if invocationContext.Project == nil {
			_ = lock.Release()
			return InvokeResponse{}, fmt.Errorf("skill %q denied: project metadata is required for mutating permissions", request.Key)
		}
		manifest := invocationContext.Manifest
		if manifest.Key == "" {
			manifest.Key = invocationContext.Project.Key
		}
		if invocationContext.Project.SystemProject {
			manifest.SystemProject = true
			if manifest.Policy.ApprovalGates.RequireForSystemProjectChanges == nil {
				requireApproval := true
				manifest.Policy.ApprovalGates.RequireForSystemProjectChanges = &requireApproval
			}
		}
		_, err = service.TransitionAuthorizer.AuthorizeMutation(ctx, projects.ActionInput{
			ProjectID:   invocationContext.Project.ID,
			Actor:       projects.TransitionControllerOdinOS,
			ActionClass: policy.ActionClass,
			ActionKey:   policy.LimitedActionKey,
		}, manifest)
		if err != nil {
			_ = lock.Release()
			return InvokeResponse{}, fmt.Errorf("skill %q denied: %w", request.Key, err)
		}
	}

	handlerPath, err := service.resolveHandlerPath(skill.HandlerRef)
	if err != nil {
		_ = lock.Release()
		return InvokeResponse{}, err
	}
	if err := lock.Release(); err != nil {
		return InvokeResponse{}, err
	}

	payload, err := json.Marshal(map[string]any{
		"skill_key": request.Key,
		"input":     cloneAnyMap(request.Input),
		"context":   invocationContext,
		"policy":    policy,
	})
	if err != nil {
		return InvokeResponse{}, err
	}

	timeout := time.Duration(skill.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	invokeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	executionProfile = restrictedSkillExecutionProfile
	stdout, stderr, err := runRestrictedCommand(invokeCtx, service.RepoRoot, restrictedSkillMetadata{
		Key:              request.Key,
		Handler:          skill.HandlerRef,
		ExecutionProfile: restrictedSkillExecutionProfile,
	}, handlerPath, payload)
	if err != nil {
		if invokeCtx.Err() == context.DeadlineExceeded {
			return InvokeResponse{}, fmt.Errorf("skill %q timed out after %s", request.Key, timeout)
		}
		message := strings.TrimSpace(stderr)
		if message == "" {
			message = strings.TrimSpace(stdout)
		}
		if message == "" {
			message = err.Error()
		}
		runtimeEffect = "handler_failed_no_state_change"
		return InvokeResponse{}, fmt.Errorf("skill %q failed: %s", request.Key, message)
	}

	var response InvokeResponse
	if err := json.Unmarshal([]byte(stdout), &response); err != nil {
		return InvokeResponse{}, fmt.Errorf("decode skill response: %w", err)
	}
	if response.SkillKey == "" {
		response.SkillKey = request.Key
	}
	if response.Status == "" {
		response.Status = "ok"
	}
	if response.RawOutput == "" {
		response.RawOutput = strings.TrimSpace(stdout)
	}
	response.Permissions = cloneStrings(skill.Permissions)
	runtimeEffect = classifyInvokeRuntimeEffect(skill.HandlerRef, response)

	return response, nil
}

func classifyInvokeRuntimeEffect(handlerRef string, response InvokeResponse) string {
	switch {
	case strings.Contains(handlerRef, "registry-skill-stub.sh"):
		return "stub_result"
	case len(response.Artifacts) != 0 || strings.TrimSpace(response.RawRef) != "":
		return "command_output_with_artifact_reference"
	default:
		return "command_output_only"
	}
}

func (service Service) resolveHandlerPath(handlerRef string) (string, error) {
	return resolveSkillHandlerPath(service.RepoRoot, handlerRef)
}

func skillReferences(snapshot registry.Snapshot, key string) []string {
	var references []string

	for _, item := range snapshot.Items {
		switch item.Kind {
		case registry.KindAgent:
			for _, tool := range item.Tools {
				if tool == key {
					references = append(references, string(item.Kind)+":"+item.Key)
					break
				}
			}
		case registry.KindWorkflow:
			for _, composed := range item.Composes {
				if composed == key {
					references = append(references, string(item.Kind)+":"+item.Key)
					break
				}
			}
		}
	}

	return references
}
