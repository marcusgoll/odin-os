package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
	runtimeevents "odin-os/internal/runtime/events"
)

func TestCreateWaitsForRegistryMutationLock(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	lockFile := holdSkillRegistryLock(t, service.RepoRoot, syscall.LOCK_EX)
	defer releaseSkillRegistryLock(t, lockFile)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := service.Create(ctx, minimalSkillSpec("echo-skill"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Create() error = %v, want context deadline exceeded while lock is held", err)
	}
}

func TestCreateEmitsLifecycleEvent(t *testing.T) {
	t.Parallel()

	observer := &recordingObserver{}
	service := newTestService(t)
	service.Observer = observer

	if _, err := service.Create(context.Background(), minimalSkillSpec("echo-skill")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if len(observer.events) != 1 {
		t.Fatalf("observer events len = %d, want 1", len(observer.events))
	}

	event := observer.events[0]
	if event.Operation != OperationCreate {
		t.Fatalf("event.Operation = %q, want %q", event.Operation, OperationCreate)
	}
	if event.Outcome != OutcomeSuccess {
		t.Fatalf("event.Outcome = %q, want %q", event.Outcome, OutcomeSuccess)
	}
	if event.SkillKey != "echo-skill" {
		t.Fatalf("event.SkillKey = %q, want echo-skill", event.SkillKey)
	}
	if event.Version != "1.0.0" {
		t.Fatalf("event.Version = %q, want 1.0.0", event.Version)
	}
	if event.Duration <= 0 {
		t.Fatalf("event.Duration = %s, want > 0", event.Duration)
	}
}

func TestInvokeEmitsExecutionProfileForAllowedInvoke(t *testing.T) {
	t.Parallel()

	observer := &recordingObserver{}
	service := newTestService(t)
	service.Observer = observer

	if _, err := service.Create(context.Background(), minimalSkillSpec("echo-skill")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, err := service.Invoke(context.Background(), InvokeRequest{Key: "echo-skill"}); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	if len(observer.events) != 2 {
		t.Fatalf("observer events len = %d, want 2", len(observer.events))
	}

	event := observer.events[1]
	if event.Operation != OperationInvoke {
		t.Fatalf("event.Operation = %q, want %q", event.Operation, OperationInvoke)
	}
	if event.Outcome != OutcomeSuccess {
		t.Fatalf("event.Outcome = %q, want %q", event.Outcome, OutcomeSuccess)
	}
	if event.ExecutionProfile != restrictedSkillExecutionProfile {
		t.Fatalf("event.ExecutionProfile = %q, want %q", event.ExecutionProfile, restrictedSkillExecutionProfile)
	}
}

func TestInvokeEmitsRuntimeScopeForProjectInvoke(t *testing.T) {
	t.Parallel()

	observer := &recordingObserver{}
	env := newInvocationHarness(t, withProjectScope("alpha"))
	env.service.Observer = observer
	env.seedSkill("skill-read", []string{"repo.read"})

	if _, err := env.service.Invoke(context.Background(), env.request("skill-read")); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	if len(observer.events) != 2 {
		t.Fatalf("observer events len = %d, want 2", len(observer.events))
	}

	event := observer.events[len(observer.events)-1]
	if event.Scope != "project" {
		t.Fatalf("event.Scope = %q, want project", event.Scope)
	}
	if event.ExecutionProfile != restrictedSkillExecutionProfile {
		t.Fatalf("event.ExecutionProfile = %q, want %q", event.ExecutionProfile, restrictedSkillExecutionProfile)
	}
}

func TestInvokeEmitsFailureLifecycleEvent(t *testing.T) {
	t.Parallel()

	observer := &recordingObserver{}
	service := newTestService(t)
	service.Observer = observer

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
	if err == nil {
		t.Fatal("Invoke() error = nil, want failure")
	}

	if len(observer.events) != 2 {
		t.Fatalf("observer events len = %d, want 2", len(observer.events))
	}

	event := observer.events[1]
	if event.Operation != OperationInvoke {
		t.Fatalf("event.Operation = %q, want %q", event.Operation, OperationInvoke)
	}
	if event.Outcome != OutcomeFailure {
		t.Fatalf("event.Outcome = %q, want %q", event.Outcome, OutcomeFailure)
	}
	if event.SkillKey != "broken-skill" {
		t.Fatalf("event.SkillKey = %q, want broken-skill", event.SkillKey)
	}
	if event.ErrorCode == "" {
		t.Fatal("event.ErrorCode = empty, want classification for invoke failure")
	}
}

func TestInvokeEmitsUnknownPermissionLifecycleEvent(t *testing.T) {
	t.Parallel()

	observer := &recordingObserver{}
	service := newTestService(t)
	service.Observer = observer

	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "unknown-permission-skill.sh")
	writeExecutable(t, handlerPath, "#!/usr/bin/env bash\ncat >/dev/null\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\"should not run\"}'\n")

	item := registry.Item{
		Kind:           registry.KindSkill,
		Key:            "unknown-permission-skill",
		Title:          "Unknown Permission Skill",
		Summary:        "Injects an unknown permission.",
		Version:        "1.0.0",
		Enabled:        true,
		Strictness:     "rigid",
		AppliesTo:      []string{"testing"},
		Scopes:         []string{"project"},
		Permissions:    []string{"repo.write"},
		HandlerType:    "command",
		HandlerRef:     "scripts/skills/unknown-permission-skill.sh",
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
	if err == nil {
		t.Fatal("Invoke() error = nil, want unknown permission denial")
	}

	if len(observer.events) != 1 {
		t.Fatalf("observer events len = %d, want 1", len(observer.events))
	}

	event := observer.events[0]
	if event.ErrorCode != runtimeevents.SkillLifecycleErrorUnknownPermission {
		t.Fatalf("event.ErrorCode = %q, want %q", event.ErrorCode, runtimeevents.SkillLifecycleErrorUnknownPermission)
	}
}

func TestInvokeEmitsMutationRequiresProjectScopeLifecycleEvent(t *testing.T) {
	t.Parallel()

	observer := &recordingObserver{}
	service := newTestService(t)
	service.Observer = observer

	spec := minimalSkillSpec("mutating-skill")
	spec.Permissions = []string{"repo.mutate.full"}
	if _, err := service.Create(context.Background(), spec); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err := service.Invoke(context.Background(), InvokeRequest{Key: "mutating-skill"})
	if err == nil {
		t.Fatal("Invoke() error = nil, want global scope denial")
	}

	event := observer.events[len(observer.events)-1]
	if event.ErrorCode != runtimeevents.SkillLifecycleErrorMutationRequiresProjectScope {
		t.Fatalf("event.ErrorCode = %q, want %q", event.ErrorCode, runtimeevents.SkillLifecycleErrorMutationRequiresProjectScope)
	}
	if event.ExecutionProfile != "" {
		t.Fatalf("event.ExecutionProfile = %q, want empty for pre-exec denial", event.ExecutionProfile)
	}
}

func TestInvokeEmitsTransitionDeniedLifecycleEvent(t *testing.T) {
	t.Parallel()

	observer := &recordingObserver{}
	env := newInvocationHarness(t, withProjectScope("alpha"))
	env.service.Observer = observer
	env.setTransitionState(projects.TransitionStateInventory, nil)
	env.seedSkill("skill-note", []string{"repo.mutate.isolated:repo_hygiene_note"})

	_, err := env.service.Invoke(context.Background(), env.request("skill-note"))
	if err == nil {
		t.Fatal("Invoke() error = nil, want transition denial")
	}

	event := observer.events[len(observer.events)-1]
	if event.ErrorCode != runtimeevents.SkillLifecycleErrorTransitionDenied {
		t.Fatalf("event.ErrorCode = %q, want %q", event.ErrorCode, runtimeevents.SkillLifecycleErrorTransitionDenied)
	}
}

func TestInvokeEmitsApprovalRequiredLifecycleEvent(t *testing.T) {
	t.Parallel()

	observer := &recordingObserver{}
	env := newInvocationHarness(t, withProjectScope("alpha"))
	env.service.Observer = observer
	env.setTransitionState(projects.TransitionStateCutover, nil)
	env.manifest.Policy.ApprovalGates.RequireForGovernanceChanges = boolPtr(true)
	env.seedSkill("skill-governance", []string{"repo.mutate.governance"})

	_, err := env.service.Invoke(context.Background(), env.request("skill-governance"))
	if err == nil {
		t.Fatal("Invoke() error = nil, want approval denial")
	}

	event := observer.events[len(observer.events)-1]
	if event.ErrorCode != runtimeevents.SkillLifecycleErrorApprovalRequired {
		t.Fatalf("event.ErrorCode = %q, want %q", event.ErrorCode, runtimeevents.SkillLifecycleErrorApprovalRequired)
	}
}

func TestCounterObserverCountsOperationOutcomes(t *testing.T) {
	t.Parallel()

	observer := NewCounterObserver()
	observer.RecordSkillEvent(context.Background(), Event{
		Operation: OperationCreate,
		Outcome:   OutcomeSuccess,
		SkillKey:  "echo-skill",
	})
	observer.RecordSkillEvent(context.Background(), Event{
		Operation: OperationInvoke,
		Outcome:   OutcomeFailure,
		SkillKey:  "echo-skill",
		ErrorCode: "decode_response",
	})

	snapshot := observer.Snapshot()
	if snapshot[OperationCreate][OutcomeSuccess] != 1 {
		t.Fatalf("create/success count = %d, want 1", snapshot[OperationCreate][OutcomeSuccess])
	}
	if snapshot[OperationInvoke][OutcomeFailure] != 1 {
		t.Fatalf("invoke/failure count = %d, want 1", snapshot[OperationInvoke][OutcomeFailure])
	}
}

type recordingObserver struct {
	mu     sync.Mutex
	events []Event
}

func (observer *recordingObserver) RecordSkillEvent(_ context.Context, event Event) {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	observer.events = append(observer.events, event)
}

func holdSkillRegistryLock(t *testing.T, repoRoot string, mode int) *os.File {
	t.Helper()

	lockPath := filepath.Join(repoRoot, "registry", ".skill-mutations.lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("OpenFile(%q) error = %v", lockPath, err)
	}
	if err := syscall.Flock(int(file.Fd()), mode|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		t.Fatalf("Flock(%q) error = %v", lockPath, err)
	}
	return file
}

func releaseSkillRegistryLock(t *testing.T, file *os.File) {
	t.Helper()

	if file == nil {
		return
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
		_ = file.Close()
		t.Fatalf("unlock skill registry lock error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
