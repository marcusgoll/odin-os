package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
	registryloader "odin-os/internal/registry/loader"
	"odin-os/internal/registry/parser"
	"odin-os/internal/registry/validator"
	runtimeevents "odin-os/internal/runtime/events"
)

var (
	ErrSkillNotFound = errors.New("skill not found")
	ErrSkillExists   = errors.New("skill already exists")
)

type Service struct {
	RepoRoot             string
	Observer             Observer
	TransitionAuthorizer TransitionAuthorizer
	SnapshotLoader       func() (registry.Snapshot, error)
}

type TransitionAuthorizer interface {
	AuthorizeMutation(context.Context, projects.ActionInput, projects.Manifest) (projects.TransitionDecision, error)
}

func (service Service) List(ctx context.Context) (_ []Skill, err error) {
	start := time.Now()
	defer func() {
		service.recordEvent(ctx, Event{
			Operation: OperationList,
			Outcome:   outcomeForError(err),
			Scope:     "repo",
			Duration:  time.Since(start),
			ErrorCode: classifySkillError(err),
			ErrorText: errorText(err),
		})
	}()

	lock, err := acquireSkillRegistryLock(ctx, service.RepoRoot, registryLockShared)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	snapshot, err := service.loadSnapshotUnlocked()
	if err != nil {
		return nil, err
	}

	skills := make([]Skill, 0, len(snapshot.ByKind[registry.KindSkill]))
	for _, item := range snapshot.ByKind[registry.KindSkill] {
		skills = append(skills, fromRegistryItem(item))
	}

	sort.Slice(skills, func(i int, j int) bool {
		return skills[i].Key < skills[j].Key
	})

	return skills, nil
}

func (service Service) Get(ctx context.Context, key string) (_ Skill, err error) {
	start := time.Now()
	key = strings.TrimSpace(key)
	defer func() {
		service.recordEvent(ctx, Event{
			Operation: OperationGet,
			Outcome:   outcomeForError(err),
			SkillKey:  key,
			Scope:     "repo",
			Duration:  time.Since(start),
			ErrorCode: classifySkillError(err),
			ErrorText: errorText(err),
		})
	}()

	lock, err := acquireSkillRegistryLock(ctx, service.RepoRoot, registryLockShared)
	if err != nil {
		return Skill{}, err
	}
	defer lock.Release()

	snapshot, err := service.loadSnapshotUnlocked()
	if err != nil {
		return Skill{}, err
	}

	return service.skillFromSnapshot(snapshot, key)
}

func (service Service) Create(ctx context.Context, spec SkillSpec) (_ Skill, err error) {
	start := time.Now()
	defer func() {
		service.recordEvent(ctx, service.eventFromSpec(OperationCreate, time.Since(start), spec, err))
	}()

	lock, err := acquireSkillRegistryLock(ctx, service.RepoRoot, registryLockExclusive)
	if err != nil {
		return Skill{}, err
	}
	defer lock.Release()

	snapshot, err := service.loadSnapshotUnlocked()
	if err != nil {
		return Skill{}, err
	}

	content, err := service.validateSpec(spec)
	if err != nil {
		return Skill{}, err
	}
	if _, exists := snapshot.ByKey[spec.Key]; exists {
		return Skill{}, ErrSkillExists
	}

	source, err := service.skillSource(spec.Key)
	if err != nil {
		return Skill{}, err
	}
	path := source.Path
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Skill{}, err
	}
	if _, err := os.Stat(path); err == nil {
		return Skill{}, ErrSkillExists
	} else if !os.IsNotExist(err) {
		return Skill{}, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Skill{}, err
	}

	snapshot, err = service.loadSnapshotUnlocked()
	if err != nil {
		return Skill{}, err
	}
	return service.skillFromSnapshot(snapshot, spec.Key)
}

func (service Service) Update(ctx context.Context, key string, spec SkillSpec) (_ Skill, err error) {
	start := time.Now()
	key = strings.TrimSpace(key)
	defer func() {
		service.recordEvent(ctx, service.eventFromSpec(OperationUpdate, time.Since(start), spec, err))
	}()

	if key == "" {
		return Skill{}, ErrSkillNotFound
	}
	if spec.Key != key {
		return Skill{}, fmt.Errorf("skill key %q does not match target %q", spec.Key, key)
	}

	lock, err := acquireSkillRegistryLock(ctx, service.RepoRoot, registryLockExclusive)
	if err != nil {
		return Skill{}, err
	}
	defer lock.Release()

	snapshot, err := service.loadSnapshotUnlocked()
	if err != nil {
		return Skill{}, err
	}
	if _, err := service.skillFromSnapshot(snapshot, key); err != nil {
		return Skill{}, err
	}

	content, err := service.validateSpec(spec)
	if err != nil {
		return Skill{}, err
	}

	source, err := service.skillSource(key)
	if err != nil {
		return Skill{}, err
	}
	path := source.Path
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, []byte(content), 0o644); err != nil {
		return Skill{}, err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return Skill{}, err
	}

	snapshot, err = service.loadSnapshotUnlocked()
	if err != nil {
		return Skill{}, err
	}
	return service.skillFromSnapshot(snapshot, key)
}

func (service Service) Delete(ctx context.Context, key string) (err error) {
	start := time.Now()
	key = strings.TrimSpace(key)
	defer func() {
		service.recordEvent(ctx, Event{
			Operation: OperationDelete,
			Outcome:   outcomeForError(err),
			SkillKey:  key,
			Scope:     "repo",
			Duration:  time.Since(start),
			ErrorCode: classifySkillError(err),
			ErrorText: errorText(err),
		})
	}()

	lock, err := acquireSkillRegistryLock(ctx, service.RepoRoot, registryLockExclusive)
	if err != nil {
		return err
	}
	defer lock.Release()

	snapshot, err := service.loadSnapshotUnlocked()
	if err != nil {
		return err
	}

	item, ok := snapshot.ByKey[key]
	if !ok || item.Kind != registry.KindSkill {
		return ErrSkillNotFound
	}

	references := skillReferences(snapshot, key)
	if len(references) != 0 {
		return fmt.Errorf("skill %q is still referenced by %s", key, strings.Join(references, ", "))
	}

	if err := os.Remove(item.Source.Path); err != nil {
		if os.IsNotExist(err) {
			return ErrSkillNotFound
		}
		return err
	}
	return nil
}

func (service Service) validateSpec(spec SkillSpec) (string, error) {
	content, err := Render(spec)
	if err != nil {
		return "", err
	}

	source, err := service.skillSource(spec.Key)
	if err != nil {
		return "", err
	}
	document, parseDiagnostics := parser.ParseSource(source, []byte(content))
	diagnostics := append([]registry.Diagnostic{}, parseDiagnostics...)
	diagnostics = append(diagnostics, validator.ValidateDocuments([]registry.ParsedDocument{document})...)
	if len(diagnostics) != 0 {
		return "", fmt.Errorf("invalid skill spec: %s", diagnostics[0].Message)
	}
	if _, err := service.resolveHandlerPath(spec.HandlerRef); err != nil {
		return "", fmt.Errorf("invalid skill spec: %w", err)
	}

	return content, nil
}

func (service Service) normalizeInvocationContext(request InvokeRequest) InvocationContext {
	context := request.Context

	if context.Project != nil {
		project := *context.Project
		project.Key = strings.TrimSpace(project.Key)
		if project.Key == "" && !project.SystemProject {
			context.Project = nil
		} else {
			context.Project = &project
		}
	}

	switch strings.TrimSpace(context.ResolvedScopeKind) {
	case "":
		switch {
		case context.Project == nil:
			context.ResolvedScopeKind = normalizeInvocationScopeKind("")
		case context.Project.SystemProject:
			context.ResolvedScopeKind = normalizeInvocationScopeKind("odin-core")
		default:
			context.ResolvedScopeKind = normalizeInvocationScopeKind("project")
		}
	default:
		context.ResolvedScopeKind = normalizeInvocationScopeKind(context.ResolvedScopeKind)
	}

	return context
}

func (service Service) loadSnapshotUnlocked() (registry.Snapshot, error) {
	if service.SnapshotLoader != nil {
		return service.SnapshotLoader()
	}

	if strings.TrimSpace(service.RepoRoot) == "" {
		return registry.Snapshot{}, fmt.Errorf("repo root is required")
	}

	snapshot, err := registryloader.LoadDir(filepath.Join(service.RepoRoot, "registry"))
	if err != nil {
		return registry.Snapshot{}, err
	}
	if len(snapshot.Diagnostics) != 0 {
		return registry.Snapshot{}, fmt.Errorf("registry has diagnostics: %s", snapshot.Diagnostics[0].Message)
	}
	return snapshot, nil
}

func (service Service) skillPath(key string) string {
	source, err := service.skillSource(key)
	if err != nil {
		return ""
	}
	return source.Path
}

func (service Service) skillSource(key string) (registry.SourceFile, error) {
	trimmed := strings.TrimSpace(key)
	if err := registry.ValidateKey(trimmed); err != nil {
		return registry.SourceFile{}, err
	}
	return registry.SourceFile{
		Path:         filepath.Join(service.RepoRoot, "registry", "skills", trimmed+".md"),
		RelativePath: filepath.ToSlash(filepath.Join("skills", trimmed+".md")),
		ExpectedKind: registry.KindSkill,
	}, nil
}

func (service Service) skillFromSnapshot(snapshot registry.Snapshot, key string) (Skill, error) {
	item, ok := snapshot.ByKey[strings.TrimSpace(key)]
	if !ok || item.Kind != registry.KindSkill {
		return Skill{}, ErrSkillNotFound
	}
	return fromRegistryItem(item), nil
}

func (service Service) eventFromSpec(operation Operation, duration time.Duration, spec SkillSpec, err error) Event {
	return Event{
		Operation:   operation,
		Outcome:     outcomeForError(err),
		SkillKey:    spec.Key,
		Scope:       "repo",
		Version:     spec.Version,
		HandlerType: spec.HandlerType,
		HandlerRef:  spec.HandlerRef,
		Permissions: cloneStrings(spec.Permissions),
		Duration:    duration,
		ErrorCode:   classifySkillError(err),
		ErrorText:   errorText(err),
	}
}

func (service Service) recordEvent(ctx context.Context, event Event) {
	if service.Observer == nil {
		return
	}
	service.Observer.RecordSkillEvent(ctx, event)
}

func outcomeForError(err error) Outcome {
	if err != nil {
		return OutcomeFailure
	}
	return OutcomeSuccess
}

func classifySkillError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrSkillNotFound):
		return "not_found"
	case errors.Is(err, ErrSkillExists):
		return "already_exists"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, projects.ErrTransitionDenied):
		if skillErrorRequiresApproval(err) {
			return runtimeevents.SkillLifecycleErrorApprovalRequired
		}
		return runtimeevents.SkillLifecycleErrorTransitionDenied
	}

	message := err.Error()
	switch {
	case skillErrorIsUnknownPermission(message):
		return runtimeevents.SkillLifecycleErrorUnknownPermission
	case skillErrorRequiresProjectScope(message):
		return runtimeevents.SkillLifecycleErrorMutationRequiresProjectScope
	case strings.Contains(message, "invalid skill spec"):
		return "validation"
	case strings.Contains(message, "still referenced"):
		return "in_use"
	case strings.Contains(message, "decode skill response"):
		return "decode_response"
	case strings.Contains(message, "must stay within the repo"):
		return "path_outside_repo"
	case strings.Contains(message, "not executable"):
		return "not_executable"
	case strings.Contains(message, "timed out"):
		return "timeout"
	case strings.Contains(message, "registry has diagnostics"):
		return "registry_invalid"
	default:
		return "failure"
	}
}

func skillErrorIsUnknownPermission(message string) bool {
	return strings.Contains(message, "unknown permission") ||
		strings.Contains(message, "unsupported permission kind") ||
		strings.Contains(message, "permission set is required")
}

func skillErrorRequiresProjectScope(message string) bool {
	return strings.Contains(message, "mutating permissions are not allowed in global scope") ||
		strings.Contains(message, "mutating permissions are not allowed in new-project scope") ||
		strings.Contains(message, "mutating permissions require project metadata in ") ||
		strings.Contains(message, "mutating permissions are not allowed in unknown scope")
}

func skillErrorRequiresApproval(err error) bool {
	message := err.Error()
	return strings.Contains(message, "requires approval") ||
		strings.Contains(message, "requires explicit approval")
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
