package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"odin-os/internal/cli/scope"
	coreprojects "odin-os/internal/core/projects"
	"odin-os/internal/runtime/checkpoints"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/store/sqlite"
)

const (
	EnvProjectKey  = "ODIN_WORKSPACE_PROJECT_KEY"
	EnvSessionName = "ODIN_WORKSPACE_SESSION_NAME"
)

type State string

const (
	StateLive    State = "live"
	StateStopped State = "stopped"
)

type FactsSource string

const (
	FactsSourceLive      FactsSource = "live"
	FactsSourceLastKnown FactsSource = "last_known"
)

type RepoStatus struct {
	RepoRoot string
	Branch   string
	Head     string
	Dirty    bool
}

type Status struct {
	ProjectKey           string                    `json:"project_key"`
	ProjectName          string                    `json:"project_name"`
	ProjectClass         coreprojects.ProjectClass `json:"project_class"`
	SessionName          string                    `json:"session_name"`
	GitRoot              string                    `json:"git_root"`
	State                State                     `json:"state"`
	FactsSource          FactsSource               `json:"facts_source"`
	LaunchCwd            string                    `json:"launch_cwd,omitempty"`
	CurrentCwd           string                    `json:"current_cwd,omitempty"`
	Branch               string                    `json:"branch,omitempty"`
	Head                 string                    `json:"head,omitempty"`
	Dirty                bool                      `json:"dirty"`
	AttachedCount        int                       `json:"attached_count"`
	TransitionState      string                    `json:"transition_state"`
	TransitionController string                    `json:"transition_controller"`
	WorkspaceEligible    bool                      `json:"workspace_eligible"`
	WorkspaceReason      string                    `json:"workspace_reason,omitempty"`
	LastKnownRefreshedAt string                    `json:"last_known_refreshed_at,omitempty"`
}

type StartRequest struct {
	SessionName string
	Cwd         string
	Command     []string
	Environment map[string]string
}

type SessionManager interface {
	HasSession(ctx context.Context, sessionName string) (bool, error)
	NewSession(ctx context.Context, request StartRequest) error
	SetEnvironment(ctx context.Context, sessionName string, key string, value string) error
	ShowEnvironment(ctx context.Context, sessionName string, key string) (string, error)
	CurrentPath(ctx context.Context, sessionName string) (string, error)
	AttachedCount(ctx context.Context, sessionName string) (int, error)
	AttachSession(ctx context.Context, sessionName string) error
	KillSession(ctx context.Context, sessionName string) error
}

type RepoInspector interface {
	ResolveGitRoot(ctx context.Context, cwd string) (string, error)
	Inspect(ctx context.Context, cwd string) (RepoStatus, error)
}

type Service struct {
	Store     *sqlite.Store
	Registry  coreprojects.Registry
	Sessions  SessionManager
	Inspector RepoInspector
	CodexBin  string
	Getwd     func() (string, error)
	Getenv    func(string) string
}

type HandoffRequest struct {
	Objective         string
	TaskTarget        string
	LastCompletedStep string
	NextSteps         []string
	Constraints       []string
	Evidence          []checkpoints.Evidence
}

type HandoffResult struct {
	Workspace  Status
	Task       sqlite.Task
	WakePacket sqlite.ContextPacket
}

func SessionName(projectKey string) string {
	sanitized := strings.TrimSpace(strings.ToLower(projectKey))
	if sanitized == "" {
		sanitized = "workspace"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-", "_", "-", ".", "-")
	sanitized = replacer.Replace(sanitized)
	sanitized = strings.Trim(sanitized, "-")
	if sanitized == "" {
		sanitized = "workspace"
	}
	return "odin-workspace-" + sanitized
}

func (service Service) Start(ctx context.Context, explicitProjectKey string) (Status, error) {
	manifest, cwd, resolution, err := service.resolveProject(ctx, explicitProjectKey, true)
	if err != nil {
		return Status{}, err
	}
	sessionName := SessionName(manifest.Key)
	launchCwd := manifest.GitRoot
	if resolution == "cwd" && cwd != "" {
		launchCwd = cwd
	}

	if _, err := service.InspectorOrDefault().Inspect(ctx, manifest.GitRoot); err != nil {
		return Status{}, fmt.Errorf("workspace repo is not ready: %w", err)
	}

	hasSession, err := service.SessionsOrDefault().HasSession(ctx, sessionName)
	if err != nil {
		return Status{}, err
	}
	if hasSession {
		managed, err := service.isManagedSession(ctx, sessionName, manifest.Key)
		if err != nil {
			return Status{}, err
		}
		if !managed {
			if err := service.SessionsOrDefault().KillSession(ctx, sessionName); err != nil {
				return Status{}, err
			}
			hasSession = false
		}
	}

	if !hasSession {
		request := StartRequest{
			SessionName: sessionName,
			Cwd:         launchCwd,
			Command:     []string{service.codexBin()},
			Environment: map[string]string{
				EnvProjectKey:  manifest.Key,
				EnvSessionName: sessionName,
			},
		}
		if err := service.SessionsOrDefault().NewSession(ctx, request); err != nil {
			return Status{}, err
		}
		for key, value := range request.Environment {
			if err := service.SessionsOrDefault().SetEnvironment(ctx, sessionName, key, value); err != nil {
				return Status{}, err
			}
		}
	}

	return service.liveStatus(ctx, manifest, sessionName, launchCwd)
}

func (service Service) Status(ctx context.Context, explicitProjectKey string) (Status, error) {
	manifest, _, _, err := service.resolveProject(ctx, explicitProjectKey, false)
	if err != nil {
		return Status{}, err
	}
	sessionName := SessionName(manifest.Key)

	hasSession, err := service.SessionsOrDefault().HasSession(ctx, sessionName)
	if err != nil {
		return Status{}, err
	}
	if hasSession {
		managed, err := service.isManagedSession(ctx, sessionName, manifest.Key)
		if err != nil {
			return Status{}, err
		}
		if !managed {
			return Status{}, fmt.Errorf("tmux session %q is not an odin-managed workspace", sessionName)
		}
		cached, _, _ := service.loadCachedFacts(ctx, manifest.Key)
		launchCwd := manifest.GitRoot
		if cached.LaunchCwd != "" {
			launchCwd = cached.LaunchCwd
		}
		return service.liveStatus(ctx, manifest, sessionName, launchCwd)
	}

	return service.cachedStatus(ctx, manifest, sessionName)
}

func (service Service) Stop(ctx context.Context, explicitProjectKey string, force bool) (Status, error) {
	manifest, _, _, err := service.resolveProject(ctx, explicitProjectKey, false)
	if err != nil {
		return Status{}, err
	}
	sessionName := SessionName(manifest.Key)

	hasSession, err := service.SessionsOrDefault().HasSession(ctx, sessionName)
	if err != nil {
		return Status{}, err
	}
	if !hasSession {
		return service.cachedStatus(ctx, manifest, sessionName)
	}

	managed, err := service.isManagedSession(ctx, sessionName, manifest.Key)
	if err != nil {
		return Status{}, err
	}
	if !managed {
		return Status{}, fmt.Errorf("tmux session %q is not an odin-managed workspace", sessionName)
	}

	attachedCount, err := service.SessionsOrDefault().AttachedCount(ctx, sessionName)
	if err != nil {
		return Status{}, err
	}
	if attachedCount > 0 && !force {
		return Status{}, fmt.Errorf("workspace session %q is attached; rerun with --force to stop it", sessionName)
	}

	cached, _, _ := service.loadCachedFacts(ctx, manifest.Key)
	currentPath, err := service.SessionsOrDefault().CurrentPath(ctx, sessionName)
	if err != nil {
		return Status{}, err
	}
	launchCwd := cached.LaunchCwd
	if launchCwd == "" {
		launchCwd = currentPath
	}
	if strings.TrimSpace(currentPath) == "" {
		currentPath = firstNonEmptyPath(cached.CurrentCwd, launchCwd, manifest.GitRoot)
	}

	branch := cached.Branch
	head := cached.Head
	dirty := cached.Dirty
	if repo, inspectErr := service.inspectManagedRepo(ctx, manifest, currentPath); inspectErr == nil {
		branch = repo.Branch
		head = repo.Head
		dirty = repo.Dirty
	}

	if err := service.recordFacts(ctx, manifest.Key, FactsSourceLastKnown, cachedWorkspaceFacts{
		SessionName: sessionName,
		LaunchCwd:   launchCwd,
		CurrentCwd:  currentPath,
		Branch:      branch,
		Head:        head,
		Dirty:       dirty,
	}); err != nil {
		return Status{}, err
	}
	if err := service.SessionsOrDefault().KillSession(ctx, sessionName); err != nil {
		return Status{}, err
	}

	return service.cachedStatus(ctx, manifest, sessionName)
}

func (service Service) List(ctx context.Context) ([]Status, error) {
	manifests := service.Registry.Projects()
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].Key < manifests[j].Key
	})

	statuses := make([]Status, 0, len(manifests))
	for _, manifest := range manifests {
		sessionName := SessionName(manifest.Key)
		status, err := service.cachedStatus(ctx, manifest, sessionName)
		if err != nil {
			return nil, err
		}

		hasSession, err := service.SessionsOrDefault().HasSession(ctx, sessionName)
		if err != nil {
			return nil, err
		}
		if hasSession {
			managed, err := service.isManagedSession(ctx, sessionName, manifest.Key)
			if err != nil {
				return nil, err
			}
			if managed {
				status.State = StateLive
				_, cachedSource, refreshedAt := service.loadCachedFacts(ctx, manifest.Key)
				if cachedSource != "" {
					status.FactsSource = cachedSource
				}
				if status.FactsSource == FactsSourceLastKnown {
					status.LastKnownRefreshedAt = refreshedAt
				} else {
					status.LastKnownRefreshedAt = ""
				}
			}
		}

		statuses = append(statuses, status)
	}

	return statuses, nil
}

func (service Service) Handoff(ctx context.Context, explicitProjectKey string, request HandoffRequest) (HandoffResult, error) {
	workspaceStatus, err := service.Status(ctx, explicitProjectKey)
	if err != nil {
		return HandoffResult{}, err
	}
	if strings.TrimSpace(request.Objective) == "" {
		return HandoffResult{}, fmt.Errorf("handoff objective is required")
	}

	manifest, ok := service.Registry.Lookup(workspaceStatus.ProjectKey)
	if !ok {
		return HandoffResult{}, fmt.Errorf("unknown project: %s", workspaceStatus.ProjectKey)
	}

	resolvedScope := scope.Resolution{Kind: scope.ScopeProject, ProjectKey: manifest.Key}
	if manifest.SystemProject {
		resolvedScope.Kind = scope.ScopeOdinCore
	}

	task, err := service.resolveOrCreateHandoffTask(ctx, manifest, resolvedScope, request)
	if err != nil {
		return HandoffResult{}, err
	}

	projectFacts := map[string]string{
		"git_root":        workspaceStatus.GitRoot,
		"branch":          workspaceStatus.Branch,
		"head":            workspaceStatus.Head,
		"current_cwd":     workspaceStatus.CurrentCwd,
		"launch_cwd":      workspaceStatus.LaunchCwd,
		"session_name":    workspaceStatus.SessionName,
		"facts_source":    string(workspaceStatus.FactsSource),
		"dirty":           strconv.FormatBool(workspaceStatus.Dirty),
		"workspace_state": string(workspaceStatus.State),
	}

	compacted, err := (checkpoints.Service{Store: service.Store}).Compact(ctx, checkpoints.CompactParams{
		TaskID:            task.ID,
		Trigger:           checkpoints.TriggerHandoff,
		Objective:         request.Objective,
		TaskStatus:        task.Status,
		LastCompletedStep: request.LastCompletedStep,
		NextSteps:         append([]string(nil), request.NextSteps...),
		Constraints:       append([]string(nil), request.Constraints...),
		Evidence:          append([]checkpoints.Evidence(nil), request.Evidence...),
		ProjectFacts:      projectFacts,
		ManifestSummary:   fmt.Sprintf("project=%s class=%s git_root=%s default_branch=%s", manifest.Key, manifest.ProjectClass, manifest.GitRoot, manifest.DefaultBranch),
	})
	if err != nil {
		return HandoffResult{}, err
	}

	return HandoffResult{
		Workspace:  workspaceStatus,
		Task:       task,
		WakePacket: compacted.WakePacket,
	}, nil
}

func (service Service) Attach(ctx context.Context, explicitProjectKey string) (Status, error) {
	status, err := service.Status(ctx, explicitProjectKey)
	if err != nil {
		return Status{}, err
	}
	if status.State != StateLive {
		return Status{}, fmt.Errorf("workspace session %q is not live; use `odin workspace start` first", status.SessionName)
	}
	if err := service.SessionsOrDefault().AttachSession(ctx, status.SessionName); err != nil {
		return Status{}, err
	}
	return service.Status(ctx, explicitProjectKey)
}

func (service Service) resolveOrCreateHandoffTask(ctx context.Context, manifest coreprojects.Manifest, resolvedScope scope.Resolution, request HandoffRequest) (sqlite.Task, error) {
	if strings.TrimSpace(request.TaskTarget) == "" {
		return (jobs.Service{
			Store:    service.Store,
			Registry: service.Registry,
		}).CreateTaskFromAct(ctx, resolvedScope, request.Objective)
	}

	project, err := service.Store.GetProjectByKey(ctx, manifest.Key)
	if err != nil {
		return sqlite.Task{}, err
	}

	target := strings.TrimSpace(request.TaskTarget)
	if taskID, parseErr := strconv.ParseInt(target, 10, 64); parseErr == nil {
		task, err := service.Store.GetTask(ctx, taskID)
		if err != nil {
			return sqlite.Task{}, err
		}
		if task.ProjectID != project.ID {
			return sqlite.Task{}, fmt.Errorf("task %d does not belong to project %s", task.ID, manifest.Key)
		}
		if task.Status != "queued" {
			return service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
				TaskID: task.ID,
				Status: "queued",
			})
		}
		return task, nil
	}

	task, err := service.Store.GetTaskByProjectAndKey(ctx, project.ID, target)
	if err != nil {
		return sqlite.Task{}, err
	}
	if task.Status != "queued" {
		return service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
			TaskID: task.ID,
			Status: "queued",
		})
	}
	return task, nil
}

func (service Service) codexBin() string {
	if strings.TrimSpace(service.CodexBin) != "" {
		return service.CodexBin
	}
	if value := strings.TrimSpace(service.GetenvOrDefault()(EnvCodexBin)); value != "" {
		return value
	}
	return "codex"
}

const EnvCodexBin = "ODIN_CODEX_BIN"

func (service Service) SessionsOrDefault() SessionManager {
	if service.Sessions != nil {
		return service.Sessions
	}
	return execSessionManager{}
}

func (service Service) InspectorOrDefault() RepoInspector {
	if service.Inspector != nil {
		return service.Inspector
	}
	return gitInspector{}
}

func (service Service) GetwdOrDefault() func() (string, error) {
	if service.Getwd != nil {
		return service.Getwd
	}
	return os.Getwd
}

func (service Service) GetenvOrDefault() func(string) string {
	if service.Getenv != nil {
		return service.Getenv
	}
	return os.Getenv
}

func (service Service) resolveProject(ctx context.Context, explicitProjectKey string, requireEligible bool) (coreprojects.Manifest, string, string, error) {
	explicitProjectKey = strings.TrimSpace(explicitProjectKey)
	if explicitProjectKey != "" {
		manifest, ok := service.Registry.Lookup(explicitProjectKey)
		if !ok {
			return coreprojects.Manifest{}, "", "", fmt.Errorf("unknown project: %s", explicitProjectKey)
		}
		if requireEligible {
			if ok, reason := workspaceEligible(manifest); !ok {
				return coreprojects.Manifest{}, "", "", fmt.Errorf("project %q is not workspace-eligible: %s", manifest.Key, reason)
			}
		}
		return manifest, "", "explicit", nil
	}

	if envProjectKey := strings.TrimSpace(service.GetenvOrDefault()(EnvProjectKey)); envProjectKey != "" {
		manifest, ok := service.Registry.Lookup(envProjectKey)
		if !ok {
			return coreprojects.Manifest{}, "", "", fmt.Errorf("unknown project from workspace environment: %s", envProjectKey)
		}
		if requireEligible {
			if ok, reason := workspaceEligible(manifest); !ok {
				return coreprojects.Manifest{}, "", "", fmt.Errorf("project %q is not workspace-eligible: %s", manifest.Key, reason)
			}
		}
		return manifest, "", "env", nil
	}

	cwd, err := service.GetwdOrDefault()()
	if err != nil {
		return coreprojects.Manifest{}, "", "", err
	}
	gitRoot, err := service.InspectorOrDefault().ResolveGitRoot(ctx, cwd)
	if err != nil {
		return coreprojects.Manifest{}, "", "", fmt.Errorf("workspace target required: pass a project key or run inside an enrolled project repo")
	}
	for _, manifest := range service.Registry.Projects() {
		if samePath(manifest.GitRoot, gitRoot) {
			if requireEligible {
				if ok, reason := workspaceEligible(manifest); !ok {
					return coreprojects.Manifest{}, "", "", fmt.Errorf("project %q is not workspace-eligible: %s", manifest.Key, reason)
				}
			}
			return manifest, cwd, "cwd", nil
		}
	}
	return coreprojects.Manifest{}, "", "", fmt.Errorf("current repo %q is not enrolled; run `odin project enroll` or pass a managed project key", gitRoot)
}

func (service Service) inspectManagedRepo(ctx context.Context, manifest coreprojects.Manifest, currentPath string) (RepoStatus, error) {
	if strings.TrimSpace(currentPath) != "" {
		repo, err := service.InspectorOrDefault().Inspect(ctx, currentPath)
		if err == nil && samePath(repo.RepoRoot, manifest.GitRoot) {
			return repo, nil
		}
	}
	return service.InspectorOrDefault().Inspect(ctx, manifest.GitRoot)
}

func firstNonEmptyPath(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (service Service) liveStatus(ctx context.Context, manifest coreprojects.Manifest, sessionName string, launchCwd string) (Status, error) {
	currentPath, err := service.SessionsOrDefault().CurrentPath(ctx, sessionName)
	if err != nil {
		return Status{}, err
	}
	if strings.TrimSpace(currentPath) == "" {
		currentPath = firstNonEmptyPath(launchCwd, manifest.GitRoot)
	}
	attachedCount, err := service.SessionsOrDefault().AttachedCount(ctx, sessionName)
	if err != nil {
		return Status{}, err
	}

	transitionState, transitionController, err := loadTransition(ctx, service.Store, manifest.Key)
	if err != nil {
		return Status{}, err
	}

	eligible, reason := workspaceEligible(manifest)
	status := Status{
		ProjectKey:           manifest.Key,
		ProjectName:          manifest.Name,
		ProjectClass:         manifest.ProjectClass,
		SessionName:          sessionName,
		GitRoot:              manifest.GitRoot,
		State:                StateLive,
		LaunchCwd:            launchCwd,
		CurrentCwd:           currentPath,
		AttachedCount:        attachedCount,
		TransitionState:      transitionState,
		TransitionController: transitionController,
		WorkspaceEligible:    eligible,
		WorkspaceReason:      reason,
	}

	repo, err := service.inspectManagedRepo(ctx, manifest, currentPath)
	if err == nil {
		status.FactsSource = FactsSourceLive
		status.Branch = repo.Branch
		status.Head = repo.Head
		status.Dirty = repo.Dirty
		if err := service.recordFacts(ctx, manifest.Key, FactsSourceLive, cachedWorkspaceFacts{
			SessionName: sessionName,
			LaunchCwd:   launchCwd,
			CurrentCwd:  currentPath,
			Branch:      repo.Branch,
			Head:        repo.Head,
			Dirty:       repo.Dirty,
		}); err != nil {
			return Status{}, err
		}
		return status, nil
	}

	cached, _, _ := service.loadCachedFacts(ctx, manifest.Key)
	fallbackFacts := cachedWorkspaceFacts{
		SessionName: sessionName,
		LaunchCwd:   firstNonEmptyPath(status.LaunchCwd, cached.LaunchCwd, manifest.GitRoot),
		CurrentCwd:  firstNonEmptyPath(currentPath, cached.CurrentCwd, status.LaunchCwd, manifest.GitRoot),
		Branch:      cached.Branch,
		Head:        cached.Head,
		Dirty:       cached.Dirty,
	}
	if err := service.recordFacts(ctx, manifest.Key, FactsSourceLastKnown, fallbackFacts); err != nil {
		return Status{}, err
	}
	latestFacts, _, refreshedAt := service.loadCachedFacts(ctx, manifest.Key)
	status.FactsSource = FactsSourceLastKnown
	status.LaunchCwd = firstNonEmptyPath(status.LaunchCwd, latestFacts.LaunchCwd, manifest.GitRoot)
	status.CurrentCwd = firstNonEmptyPath(currentPath, latestFacts.CurrentCwd, status.LaunchCwd, manifest.GitRoot)
	status.Branch = latestFacts.Branch
	status.Head = latestFacts.Head
	status.Dirty = latestFacts.Dirty
	status.LastKnownRefreshedAt = refreshedAt
	return status, nil
}

func (service Service) cachedStatus(ctx context.Context, manifest coreprojects.Manifest, sessionName string) (Status, error) {
	transitionState, transitionController, err := loadTransition(ctx, service.Store, manifest.Key)
	if err != nil {
		return Status{}, err
	}
	status := Status{
		ProjectKey:           manifest.Key,
		ProjectName:          manifest.Name,
		ProjectClass:         manifest.ProjectClass,
		SessionName:          sessionName,
		GitRoot:              manifest.GitRoot,
		State:                StateStopped,
		FactsSource:          FactsSourceLastKnown,
		TransitionState:      transitionState,
		TransitionController: transitionController,
	}
	if eligible, reason := workspaceEligible(manifest); !eligible {
		status.WorkspaceEligible = false
		status.WorkspaceReason = reason
	} else {
		status.WorkspaceEligible = true
	}

	cached, _, refreshedAt := service.loadCachedFacts(ctx, manifest.Key)
	status.LaunchCwd = cached.LaunchCwd
	status.CurrentCwd = cached.CurrentCwd
	status.Branch = cached.Branch
	status.Head = cached.Head
	status.Dirty = cached.Dirty
	status.LastKnownRefreshedAt = refreshedAt
	return status, nil
}

func (service Service) isManagedSession(ctx context.Context, sessionName string, projectKey string) (bool, error) {
	envProjectKey, err := service.SessionsOrDefault().ShowEnvironment(ctx, sessionName, EnvProjectKey)
	if err != nil {
		return false, err
	}
	envSessionName, err := service.SessionsOrDefault().ShowEnvironment(ctx, sessionName, EnvSessionName)
	if err != nil {
		return false, err
	}
	return envProjectKey == projectKey && envSessionName == sessionName, nil
}

func (service Service) recordFacts(ctx context.Context, projectKey string, source FactsSource, facts cachedWorkspaceFacts) error {
	if service.Store == nil {
		return fmt.Errorf("workspace store is required")
	}
	payload, err := json.Marshal(facts)
	if err != nil {
		return err
	}
	_, err = service.Store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
		Surface:     "workspace:" + projectKey,
		Status:      string(source),
		DetailsJSON: string(payload),
	})
	return err
}

func (service Service) loadCachedFacts(ctx context.Context, projectKey string) (cachedWorkspaceFacts, FactsSource, string) {
	if service.Store == nil {
		return cachedWorkspaceFacts{}, "", ""
	}
	record, err := service.Store.GetProjectionFreshness(ctx, "workspace:"+projectKey)
	if err != nil {
		return cachedWorkspaceFacts{}, "", ""
	}
	var facts cachedWorkspaceFacts
	if err := json.Unmarshal([]byte(record.DetailsJSON), &facts); err != nil {
		return cachedWorkspaceFacts{}, FactsSource(record.Status), record.RefreshedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return facts, FactsSource(record.Status), record.RefreshedAt.Format("2006-01-02T15:04:05Z07:00")
}

type cachedWorkspaceFacts struct {
	SessionName string `json:"session_name"`
	LaunchCwd   string `json:"launch_cwd"`
	CurrentCwd  string `json:"current_cwd"`
	Branch      string `json:"branch"`
	Head        string `json:"head"`
	Dirty       bool   `json:"dirty"`
}

func workspaceEligible(manifest coreprojects.Manifest) (bool, string) {
	if strings.TrimSpace(manifest.GitRoot) == "" {
		return false, "missing git_root"
	}
	if !coreprojects.IsGitRepository(manifest.GitRoot) {
		return false, "git_root is not a Git repository"
	}
	return true, ""
}

func loadTransition(ctx context.Context, store *sqlite.Store, key string) (string, string, error) {
	if store == nil {
		return string(coreprojects.TransitionStateInventory), string(coreprojects.TransitionControllerLegacyOdin), nil
	}
	project, err := store.GetProjectByKey(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return string(coreprojects.TransitionStateInventory), string(coreprojects.TransitionControllerLegacyOdin), nil
		}
		return "", "", err
	}
	record, err := store.GetProjectTransition(ctx, project.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return string(coreprojects.TransitionStateInventory), string(coreprojects.TransitionControllerLegacyOdin), nil
		}
		return "", "", err
	}
	return record.State, record.Controller, nil
}

func samePath(left string, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}
	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

type gitInspector struct{}

func (gitInspector) ResolveGitRoot(ctx context.Context, cwd string) (string, error) {
	return coreprojects.InferGitRoot(ctx, cwd)
}

func (gitInspector) Inspect(ctx context.Context, cwd string) (RepoStatus, error) {
	repoRoot, err := coreprojects.InferGitRoot(ctx, cwd)
	if err != nil {
		return RepoStatus{}, err
	}
	branch, err := coreprojects.InferCurrentBranch(ctx, repoRoot)
	if err != nil {
		return RepoStatus{}, err
	}
	head, err := gitOutput(ctx, repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return RepoStatus{}, err
	}
	statusOutput, err := gitOutput(ctx, repoRoot, "status", "--porcelain")
	if err != nil {
		return RepoStatus{}, err
	}
	return RepoStatus{
		RepoRoot: repoRoot,
		Branch:   strings.TrimSpace(branch),
		Head:     strings.TrimSpace(head),
		Dirty:    strings.TrimSpace(statusOutput) != "",
	}, nil
}

type execSessionManager struct {
	TMuxBin string
}

func (manager execSessionManager) bin() string {
	if strings.TrimSpace(manager.TMuxBin) != "" {
		return manager.TMuxBin
	}
	return "tmux"
}

func (manager execSessionManager) HasSession(ctx context.Context, sessionName string) (bool, error) {
	cmd := exec.CommandContext(ctx, manager.bin(), "has-session", "-t", sessionName)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (manager execSessionManager) NewSession(ctx context.Context, request StartRequest) error {
	args := []string{"new-session", "-d", "-s", request.SessionName, "-c", request.Cwd}
	command := append([]string{"env"}, envArgs(request.Environment)...)
	command = append(command, request.Command...)
	args = append(args, command...)
	cmd := exec.CommandContext(ctx, manager.bin(), args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux new-session: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (manager execSessionManager) SetEnvironment(ctx context.Context, sessionName string, key string, value string) error {
	return tmuxRun(ctx, manager.bin(), "set-environment", "-t", sessionName, key, value)
}

func (manager execSessionManager) ShowEnvironment(ctx context.Context, sessionName string, key string) (string, error) {
	output, err := tmuxOutput(ctx, manager.bin(), "show-environment", "-t", sessionName, key)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(output)
	line = strings.TrimPrefix(line, key+"=")
	line = strings.TrimPrefix(line, "-"+key)
	return line, nil
}

func (manager execSessionManager) CurrentPath(ctx context.Context, sessionName string) (string, error) {
	output, err := tmuxOutput(ctx, manager.bin(), "display-message", "-p", "-t", sessionName, "#{pane_current_path}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (manager execSessionManager) AttachedCount(ctx context.Context, sessionName string) (int, error) {
	output, err := tmuxOutput(ctx, manager.bin(), "display-message", "-p", "-t", sessionName, "#{session_attached}")
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (manager execSessionManager) KillSession(ctx context.Context, sessionName string) error {
	return tmuxRun(ctx, manager.bin(), "kill-session", "-t", sessionName)
}

func (manager execSessionManager) AttachSession(ctx context.Context, sessionName string) error {
	return tmuxRun(ctx, manager.bin(), "attach-session", "-t", sessionName)
}

func envArgs(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	// tmux command output should be deterministic for tests.
	sortStrings(keys)
	args := make([]string, 0, len(keys))
	for _, key := range keys {
		args = append(args, key+"="+values[key])
	}
	return args
}

func sortStrings(values []string) {
	for index := 0; index < len(values); index++ {
		for inner := index + 1; inner < len(values); inner++ {
			if values[inner] < values[index] {
				values[index], values[inner] = values[inner], values[index]
			}
		}
	}
}

func tmuxRun(ctx context.Context, bin string, args ...string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func tmuxOutput(ctx context.Context, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", commandArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
