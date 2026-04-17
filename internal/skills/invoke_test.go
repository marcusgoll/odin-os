package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
	"odin-os/internal/store/sqlite"
)

func TestInvokeExecutesCommandSkillAndReturnsStructuredResponse(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	writeExecutable(t, filepath.Join(service.RepoRoot, "scripts", "skills", "echo-skill.sh"), `#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"ok","summary":"echo complete","output":{"message":"hello"}}'
`)

	spec := minimalSkillSpec("echo-skill")
	spec.HandlerRef = "scripts/skills/echo-skill.sh"
	if _, err := service.Create(context.Background(), spec); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	response, err := service.Invoke(context.Background(), InvokeRequest{
		Key: "echo-skill",
		Input: map[string]any{
			"message": "hello",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.Status != "ok" {
		t.Fatalf("response.Status = %q, want ok", response.Status)
	}
	if response.Summary != "echo complete" {
		t.Fatalf("response.Summary = %q, want %q", response.Summary, "echo complete")
	}
}

func TestInvokeRejectsMalformedJSONResponse(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	writeExecutable(t, filepath.Join(service.RepoRoot, "scripts", "skills", "broken-skill.sh"), `#!/usr/bin/env bash
cat >/dev/null
printf 'not-json\n'
`)

	spec := minimalSkillSpec("broken-skill")
	spec.HandlerRef = "scripts/skills/broken-skill.sh"
	if _, err := service.Create(context.Background(), spec); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err := service.Invoke(context.Background(), InvokeRequest{Key: "broken-skill"})
	if err == nil || !strings.Contains(err.Error(), "decode skill response") {
		t.Fatalf("Invoke() error = %v, want decode failure", err)
	}
}

func TestInvokeRejectsSkillTimeout(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	writeExecutable(t, filepath.Join(service.RepoRoot, "scripts", "skills", "slow-skill.sh"), `#!/usr/bin/env bash
sleep 2
printf '%s\n' '{"status":"ok","summary":"slow"}'
`)

	spec := minimalSkillSpec("slow-skill")
	spec.HandlerRef = "scripts/skills/slow-skill.sh"
	spec.TimeoutSeconds = 1
	if _, err := service.Create(context.Background(), spec); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err := service.Invoke(context.Background(), InvokeRequest{Key: "slow-skill"})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Invoke() error = %v, want timeout", err)
	}
}

func TestInvokeRejectsMutatingPermissionsInGlobalScope(t *testing.T) {
	t.Parallel()

	service := newTestService(t)

	spec := minimalSkillSpec("mutating-skill")
	spec.Permissions = []string{"repo.mutate.full"}
	if _, err := service.Create(context.Background(), spec); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err := service.Invoke(context.Background(), InvokeRequest{Key: "mutating-skill"})
	if err == nil || !strings.Contains(err.Error(), "global scope") {
		t.Fatalf("Invoke() error = %v, want global-scope denial", err)
	}
}

func TestInvokeRejectsMixedPermissionsThatIncludeMutationInGlobalScope(t *testing.T) {
	t.Parallel()

	service := newTestService(t)

	spec := minimalSkillSpec("mixed-skill")
	spec.Permissions = []string{"repo.read", "repo.mutate.full"}
	if _, err := service.Create(context.Background(), spec); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err := service.Invoke(context.Background(), InvokeRequest{Key: "mixed-skill"})
	if err == nil || !strings.Contains(err.Error(), "global scope") {
		t.Fatalf("Invoke() error = %v, want global-scope denial", err)
	}
}

func TestInvokeDeniesIsolatedMutationWhenProjectIsNotInLimitedAction(t *testing.T) {
	t.Parallel()

	env := newInvocationHarness(t, withProjectScope("alpha"))
	env.setTransitionState(projects.TransitionStateInventory, nil)
	env.seedSkill("skill-note", []string{"repo.mutate.isolated:repo_hygiene_note"})

	_, err := env.service.Invoke(context.Background(), env.request("skill-note"))
	if err == nil || !strings.Contains(err.Error(), "transition denied") {
		t.Fatalf("Invoke() error = %v, want transition denial", err)
	}
}

func TestInvokeDeniesIsolatedMutationWhenLimitedActionIsNotAllowlisted(t *testing.T) {
	t.Parallel()

	env := newInvocationHarness(t, withProjectScope("alpha"))
	env.setTransitionState(projects.TransitionStateLimitedAction, []string{"docs_audit_note"})
	env.seedSkill("skill-note", []string{"repo.mutate.isolated:repo_hygiene_note"})

	_, err := env.service.Invoke(context.Background(), env.request("skill-note"))
	if err == nil || !strings.Contains(err.Error(), "transition denied") {
		t.Fatalf("Invoke() error = %v, want allowlist denial", err)
	}
}

func TestInvokeAllowsIsolatedMutationWhenLimitedActionMatches(t *testing.T) {
	t.Parallel()

	env := newInvocationHarness(t, withProjectScope("alpha"))
	env.setTransitionState(projects.TransitionStateLimitedAction, []string{"repo_hygiene_note"})
	env.seedSkill("skill-note", []string{"repo.mutate.isolated:repo_hygiene_note"})

	response, err := env.service.Invoke(context.Background(), env.request("skill-note"))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.Status != "ok" {
		t.Fatalf("response.Status = %q, want ok", response.Status)
	}
}

func TestInvokeDeniesGovernanceMutationWhenApprovalIsRequired(t *testing.T) {
	t.Parallel()

	env := newInvocationHarness(t, withProjectScope("alpha"))
	env.setTransitionState(projects.TransitionStateCutover, nil)
	env.manifest.Policy.ApprovalGates.RequireForGovernanceChanges = boolPtr(true)
	env.seedSkill("skill-governance", []string{"repo.mutate.governance"})

	_, err := env.service.Invoke(context.Background(), env.request("skill-governance"))
	if err == nil || !strings.Contains(err.Error(), "approval") {
		t.Fatalf("Invoke() error = %v, want approval denial", err)
	}
}

func TestInvokeDeniesDestructiveMutationWhenApprovalIsRequired(t *testing.T) {
	t.Parallel()

	env := newInvocationHarness(t, withProjectScope("alpha"))
	env.setTransitionState(projects.TransitionStateCutover, nil)
	env.manifest.Policy.ApprovalGates.RequireForDestructiveOperations = boolPtr(true)
	env.seedSkill("skill-destructive", []string{"repo.mutate.destructive"})

	_, err := env.service.Invoke(context.Background(), env.request("skill-destructive"))
	if err == nil || !strings.Contains(err.Error(), "approval") {
		t.Fatalf("Invoke() error = %v, want approval denial", err)
	}
}

func TestInvokeDeniesOdinCoreMutationWhenSystemProjectApprovalIsRequired(t *testing.T) {
	t.Parallel()

	env := newInvocationHarness(t, withOdinCoreScope("odin-core"))
	env.setTransitionState(projects.TransitionStateCutover, nil)
	env.manifest.SystemProject = true
	env.manifest.Policy.ApprovalGates.RequireForSystemProjectChanges = boolPtr(true)
	env.seedSkill("skill-system", []string{"repo.mutate.full"})

	_, err := env.service.Invoke(context.Background(), env.request("skill-system"))
	if err == nil || !strings.Contains(err.Error(), "approval") {
		t.Fatalf("Invoke() error = %v, want approval denial", err)
	}
}

func TestInvokeDeniesOdinCoreMutationWhenManifestOmitsSystemApprovalGate(t *testing.T) {
	t.Parallel()

	env := newInvocationHarness(t, withOdinCoreScope("odin-core"))
	env.setTransitionState(projects.TransitionStateCutover, nil)
	env.manifest = projects.Manifest{Key: "odin-core"}
	env.seedSkill("skill-system-default", []string{"repo.mutate.full"})

	_, err := env.service.Invoke(context.Background(), env.request("skill-system-default"))
	if err == nil || !strings.Contains(err.Error(), "approval") {
		t.Fatalf("Invoke() error = %v, want system-project approval denial", err)
	}
}

func TestInvokeRejectsRegistryInvalidPermissionsBeforeHandlerRuns(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "side-effect-skill.sh")
	markerPath := filepath.Join(service.RepoRoot, "side-effect.marker")
	writeExecutable(t, handlerPath, "#!/usr/bin/env bash\n: >\""+markerPath+"\"\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\"ran\"}'\n")

	writeFile(t, filepath.Join(service.RepoRoot, "registry", "skills", "side-effect-skill.md"), `---
kind: skill
key: side-effect-skill
title: Side Effect Skill
summary: Should never run.
version: 1.0.0
enabled: true
strictness: rigid
applies_to:
  - testing
scopes:
  - project
permissions:
  - repo.write
handler_type: command
handler_ref: scripts/skills/side-effect-skill.sh
timeout_seconds: 15
input_schema:
  type: object
output_schema:
  type: object
---

## Purpose
Never execute.

## When to Use
Never.

## Inputs
None.

## Procedure
Should be blocked.

## Outputs
None.

## Constraints
Must not run.

## Success Criteria
Handler is not executed.
`)

	_, err := service.Invoke(context.Background(), InvokeRequest{Key: "side-effect-skill"})
	if err == nil || !strings.Contains(err.Error(), "registry has diagnostics") {
		t.Fatalf("Invoke() error = %v, want registry validation denial", err)
	}
	if _, statErr := os.Stat(markerPath); !os.IsNotExist(statErr) {
		t.Fatalf("handler marker = %v, want handler not executed", statErr)
	}
}

func TestInvokeRejectsInjectedInvalidPermissionsBeforeHandlerRuns(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "injected-invalid-skill.sh")
	markerPath := filepath.Join(service.RepoRoot, "injected-invalid.marker")
	writeExecutable(t, handlerPath, "#!/usr/bin/env bash\n: >\""+markerPath+"\"\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\"ran\"}'\n")

	item := registry.Item{
		Kind:           registry.KindSkill,
		Key:            "injected-invalid-skill",
		Title:          "Injected Invalid Skill",
		Summary:        "Bypasses registry validation.",
		Version:        "1.0.0",
		Enabled:        true,
		Strictness:     "rigid",
		AppliesTo:      []string{"testing"},
		Scopes:         []string{"project"},
		Permissions:    []string{"repo.write"},
		HandlerType:    "command",
		HandlerRef:     "scripts/skills/injected-invalid-skill.sh",
		TimeoutSeconds: 15,
		LegacyInputSchema: map[string]any{
			"type": "object",
		},
		LegacyOutputSchema: map[string]any{
			"type": "object",
		},
	}
	service.SnapshotLoader = func() (registry.Snapshot, error) {
		return registry.Snapshot{
			Items: []registry.Item{item},
			ByKey: map[string]registry.Item{
				item.Key: item,
			},
			ByKind: map[registry.Kind][]registry.Item{
				registry.KindSkill: {item},
			},
		}, nil
	}

	_, err := service.Invoke(context.Background(), InvokeRequest{Key: item.Key})
	if err == nil || !strings.Contains(err.Error(), "skill \""+item.Key+"\" denied") {
		t.Fatalf("Invoke() error = %v, want invocation policy denial", err)
	}
	if _, statErr := os.Stat(markerPath); !os.IsNotExist(statErr) {
		t.Fatalf("handler marker = %v, want handler not executed", statErr)
	}
}

func TestInvokeRejectsInjectedEmptyPermissionsBeforeHandlerRuns(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "injected-empty-skill.sh")
	markerPath := filepath.Join(service.RepoRoot, "injected-empty.marker")
	writeExecutable(t, handlerPath, "#!/usr/bin/env bash\n: >\""+markerPath+"\"\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\"ran\"}'\n")

	item := registry.Item{
		Kind:           registry.KindSkill,
		Key:            "injected-empty-skill",
		Title:          "Injected Empty Skill",
		Summary:        "Bypasses registry validation.",
		Version:        "1.0.0",
		Enabled:        true,
		Strictness:     "rigid",
		AppliesTo:      []string{"testing"},
		Scopes:         []string{"project"},
		Permissions:    nil,
		HandlerType:    "command",
		HandlerRef:     "scripts/skills/injected-empty-skill.sh",
		TimeoutSeconds: 15,
		LegacyInputSchema: map[string]any{
			"type": "object",
		},
		LegacyOutputSchema: map[string]any{
			"type": "object",
		},
	}
	service.SnapshotLoader = func() (registry.Snapshot, error) {
		return registry.Snapshot{
			Items: []registry.Item{item},
			ByKey: map[string]registry.Item{
				item.Key: item,
			},
			ByKind: map[registry.Kind][]registry.Item{
				registry.KindSkill: {item},
			},
		}, nil
	}

	_, err := service.Invoke(context.Background(), InvokeRequest{Key: item.Key})
	if err == nil || !strings.Contains(err.Error(), "skill \""+item.Key+"\" denied") {
		t.Fatalf("Invoke() error = %v, want invocation policy denial", err)
	}
	if _, statErr := os.Stat(markerPath); !os.IsNotExist(statErr) {
		t.Fatalf("handler marker = %v, want handler not executed", statErr)
	}
}

func TestCreateRejectsEscapingHandlerPath(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	spec := minimalSkillSpec("unsafe-skill")
	spec.HandlerRef = "../outside.sh"

	_, err := service.Create(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "must stay within the repo") {
		t.Fatalf("Create() error = %v, want path rejection", err)
	}
}

func TestCreateRejectsSymlinkEscapingHandlerPath(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	outsideDir := t.TempDir()
	outsideHandler := filepath.Join(outsideDir, "outside-handler.sh")
	writeExecutable(t, outsideHandler, "#!/usr/bin/env bash\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\"outside\"}'\n")

	symlinkPath := filepath.Join(service.RepoRoot, "scripts", "skills", "symlink-handler.sh")
	if err := os.MkdirAll(filepath.Dir(symlinkPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(symlinkPath), err)
	}
	if err := os.Symlink(outsideHandler, symlinkPath); err != nil {
		t.Fatalf("Symlink(%q, %q) error = %v", outsideHandler, symlinkPath, err)
	}

	spec := minimalSkillSpec("symlink-skill")
	spec.HandlerRef = "scripts/skills/symlink-handler.sh"

	_, err := service.Create(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "must stay within the repo") {
		t.Fatalf("Create() error = %v, want symlink path rejection", err)
	}
}

func TestDeleteRejectsReferencedSkill(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	if _, err := service.Create(context.Background(), minimalSkillSpec("triage-skill")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	writeFile(t, filepath.Join(service.RepoRoot, "registry", "agents", "triage-agent.md"), `---
kind: agent
key: triage-agent
title: Triage Agent
summary: Uses the triage skill.
role: operator
scopes:
  - project
tools:
  - triage-skill
---

## Purpose
Route work.

## When to Use
When work arrives.

## Inputs
Incoming work.

## Procedure
Use the triage skill.

## Outputs
Routed work.

## Constraints
Stay deterministic.

## Success Criteria
Work is routed.
`)

	err := service.Delete(context.Background(), "triage-skill")
	if err == nil || !strings.Contains(err.Error(), "still referenced") {
		t.Fatalf("Delete() error = %v, want reference rejection", err)
	}
}

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

type invocationHarness struct {
	t        *testing.T
	service  Service
	store    *sqlite.Store
	project  sqlite.Project
	manifest projects.Manifest
}

type invocationHarnessOption func(*invocationHarness)

func withProjectScope(projectKey string) invocationHarnessOption {
	return func(env *invocationHarness) {
		env.project = sqlite.Project{
			Key:           projectKey,
			Name:          projectKey,
			Scope:         "project",
			GitRoot:       filepath.Join(env.service.RepoRoot, projectKey),
			DefaultBranch: "main",
			ManifestPath:  "config/projects.yaml",
		}
		env.manifest = projects.Manifest{
			Key:           projectKey,
			Name:          projectKey,
			ProjectClass:  projects.ProjectClassGitHubBacked,
			GitRoot:       env.project.GitRoot,
			DefaultBranch: "main",
			Policy: projects.Policy{
				ApprovalGates: projects.ApprovalGates{},
			},
		}
	}
}

func withOdinCoreScope(projectKey string) invocationHarnessOption {
	return func(env *invocationHarness) {
		env.project = sqlite.Project{
			Key:           projectKey,
			Name:          projectKey,
			Scope:         "odin-core",
			GitRoot:       filepath.Join(env.service.RepoRoot, projectKey),
			DefaultBranch: "main",
			ManifestPath:  "config/projects.yaml",
		}
		env.manifest = projects.Manifest{
			Key:           projectKey,
			Name:          projectKey,
			ProjectClass:  projects.ProjectClassSystem,
			SystemProject: true,
			GitRoot:       env.project.GitRoot,
			DefaultBranch: "main",
			Policy: projects.Policy{
				ApprovalGates: projects.ApprovalGates{},
			},
		}
	}
}

func newInvocationHarness(t *testing.T, opts ...invocationHarnessOption) *invocationHarness {
	t.Helper()

	service := newTestService(t)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	env := &invocationHarness{
		t:       t,
		service: service,
		store:   store,
	}
	for _, opt := range opts {
		opt(env)
	}

	project, err := store.CreateProject(context.Background(), sqlite.CreateProjectParams{
		Key:           env.project.Key,
		Name:          env.project.Name,
		Scope:         env.project.Scope,
		GitRoot:       env.project.GitRoot,
		DefaultBranch: env.project.DefaultBranch,
		ManifestPath:  env.project.ManifestPath,
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	env.project = project

	env.service.TransitionAuthorizer = projects.Service{Store: store}
	return env
}

func (env *invocationHarness) setTransitionState(state projects.TransitionState, allowlist []string) {
	env.t.Helper()

	input := projects.TransitionStateInput{
		ProjectID:      env.project.ID,
		Actor:          projects.TransitionControllerOdinOS,
		TargetState:    state,
		LimitedActions: allowlist,
		ChangedBy:      "operator",
		Notes:          "test transition",
	}
	transitionService := projects.Service{Store: env.store}
	if _, err := transitionService.SetTransitionState(context.Background(), input); err != nil {
		env.t.Fatalf("SetTransitionState(%s) error = %v", state, err)
	}
	env.manifest.Key = env.project.Key
	env.manifest.SystemProject = env.project.Scope == "odin-core"
}

func (env *invocationHarness) seedSkill(key string, permissions []string) {
	env.t.Helper()

	handlerPath := filepath.Join(env.service.RepoRoot, "scripts", "skills", key+".sh")
	writeExecutable(env.t, handlerPath, "#!/usr/bin/env bash\ncat >/dev/null\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\""+key+" complete\"}'\n")

	spec := minimalSkillSpec(key)
	spec.HandlerRef = filepath.ToSlash(filepath.Join("scripts", "skills", key+".sh"))
	spec.Permissions = permissions
	if _, err := env.service.Create(context.Background(), spec); err != nil {
		env.t.Fatalf("Create() error = %v", err)
	}
}

func (env *invocationHarness) request(key string) InvokeRequest {
	env.t.Helper()

	return InvokeRequest{
		Key: key,
		Context: InvocationContext{
			ResolvedScopeKind: env.scopeKind(),
			Project: &InvocationProject{
				ID:            env.project.ID,
				Key:           env.project.Key,
				SystemProject: env.project.Scope == "odin-core",
			},
			Manifest: env.manifest,
		},
	}
}

func (env *invocationHarness) scopeKind() string {
	if env.project.Scope == "odin-core" {
		return "odin-core"
	}
	return "project"
}

func boolPtr(value bool) *bool {
	return &value
}
