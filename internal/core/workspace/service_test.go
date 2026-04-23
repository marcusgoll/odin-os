package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	coreprojects "odin-os/internal/core/projects"
	"odin-os/internal/store/sqlite"
)

func TestStartCreatesManagedSessionAndCachesFacts(t *testing.T) {
	ctx := context.Background()
	store := openWorkspaceTestStore(t)
	defer store.Close()

	repoRoot := createGitRepo(t, "main")
	subdir := filepath.Join(repoRoot, "docs")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(subdir) error = %v", err)
	}

	registry := writeWorkspaceRegistry(t, map[string]string{"alpha": repoRoot})
	sessions := newFakeSessionManager()
	service := Service{
		Store:    store,
		Registry: registry,
		Sessions: sessions,
		Getwd: func() (string, error) {
			return subdir, nil
		},
		Getenv: func(string) string { return "" },
	}

	status, err := service.Start(ctx, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if status.ProjectKey != "alpha" {
		t.Fatalf("ProjectKey = %q, want alpha", status.ProjectKey)
	}
	if status.State != StateLive {
		t.Fatalf("State = %q, want %q", status.State, StateLive)
	}
	if status.SessionName != SessionName("alpha") {
		t.Fatalf("SessionName = %q, want %q", status.SessionName, SessionName("alpha"))
	}
	if status.LaunchCwd != subdir {
		t.Fatalf("LaunchCwd = %q, want %q", status.LaunchCwd, subdir)
	}
	if status.CurrentCwd != subdir {
		t.Fatalf("CurrentCwd = %q, want %q", status.CurrentCwd, subdir)
	}
	if status.Branch != "main" {
		t.Fatalf("Branch = %q, want main", status.Branch)
	}
	if status.Head == "" {
		t.Fatalf("Head is empty, want commit sha")
	}
	if status.FactsSource != FactsSourceLive {
		t.Fatalf("FactsSource = %q, want %q", status.FactsSource, FactsSourceLive)
	}
	if status.TransitionState != string(coreprojects.TransitionStateInventory) {
		t.Fatalf("TransitionState = %q, want inventory", status.TransitionState)
	}

	session := sessions.mustSession(t, status.SessionName)
	if session.env[EnvProjectKey] != "alpha" {
		t.Fatalf("session env project key = %q, want alpha", session.env[EnvProjectKey])
	}
	if session.env[EnvSessionName] != status.SessionName {
		t.Fatalf("session env session name = %q, want %q", session.env[EnvSessionName], status.SessionName)
	}

	record, err := store.GetProjectionFreshness(ctx, "workspace:alpha")
	if err != nil {
		t.Fatalf("GetProjectionFreshness(workspace:alpha) error = %v", err)
	}
	if record.Status != string(FactsSourceLive) {
		t.Fatalf("freshness status = %q, want %q", record.Status, FactsSourceLive)
	}
	for _, want := range []string{subdir, status.SessionName, `"branch":"main"`} {
		if !strings.Contains(record.DetailsJSON, want) {
			t.Fatalf("freshness details = %s, want substring %q", record.DetailsJSON, want)
		}
	}
}

func TestStatusUsesLastKnownFactsWhenWorkspaceIsStopped(t *testing.T) {
	ctx := context.Background()
	store := openWorkspaceTestStore(t)
	defer store.Close()

	repoRoot := createGitRepo(t, "main")
	subdir := filepath.Join(repoRoot, "docs")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(subdir) error = %v", err)
	}

	registry := writeWorkspaceRegistry(t, map[string]string{"alpha": repoRoot})
	sessions := newFakeSessionManager()
	service := Service{
		Store:    store,
		Registry: registry,
		Sessions: sessions,
		Getwd: func() (string, error) {
			return subdir, nil
		},
		Getenv: func(string) string { return "" },
	}

	started, err := service.Start(ctx, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := sessions.KillSession(ctx, started.SessionName); err != nil {
		t.Fatalf("KillSession() error = %v", err)
	}

	status, err := service.Status(ctx, "")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if status.State != StateStopped {
		t.Fatalf("State = %q, want %q", status.State, StateStopped)
	}
	if status.FactsSource != FactsSourceLastKnown {
		t.Fatalf("FactsSource = %q, want %q", status.FactsSource, FactsSourceLastKnown)
	}
	if status.Branch != "main" {
		t.Fatalf("Branch = %q, want main", status.Branch)
	}
	if status.LaunchCwd != subdir {
		t.Fatalf("LaunchCwd = %q, want %q", status.LaunchCwd, subdir)
	}
}

func TestStopRequiresForceWhenSessionIsAttached(t *testing.T) {
	ctx := context.Background()
	store := openWorkspaceTestStore(t)
	defer store.Close()

	repoRoot := createGitRepo(t, "main")
	registry := writeWorkspaceRegistry(t, map[string]string{"alpha": repoRoot})
	sessions := newFakeSessionManager()
	service := Service{
		Store:    store,
		Registry: registry,
		Sessions: sessions,
		Getwd: func() (string, error) {
			return repoRoot, nil
		},
		Getenv: func(string) string { return "" },
	}

	started, err := service.Start(ctx, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	sessions.mustSession(t, started.SessionName).attached = 1

	if _, err := service.Stop(ctx, "", false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("Stop(force=false) error = %v, want --force guidance", err)
	}

	status, err := service.Stop(ctx, "", true)
	if err != nil {
		t.Fatalf("Stop(force=true) error = %v", err)
	}
	if status.State != StateStopped {
		t.Fatalf("State = %q, want %q", status.State, StateStopped)
	}
	if _, ok := sessions.sessions[started.SessionName]; ok {
		t.Fatalf("expected session %q to be removed", started.SessionName)
	}
}

func TestStatusAndStopDegradeWhenProjectGitRootBecomesInvalid(t *testing.T) {
	ctx := context.Background()
	store := openWorkspaceTestStore(t)
	defer store.Close()

	repoRoot := createGitRepo(t, "main")
	subdir := filepath.Join(repoRoot, "docs")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(subdir) error = %v", err)
	}

	registry := writeWorkspaceRegistry(t, map[string]string{"alpha": repoRoot})
	sessions := newFakeSessionManager()
	service := Service{
		Store:    store,
		Registry: registry,
		Sessions: sessions,
		Getwd: func() (string, error) {
			return subdir, nil
		},
		Getenv: func(string) string { return "" },
	}

	started, err := service.Start(ctx, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if started.FactsSource != FactsSourceLive {
		t.Fatalf("FactsSource = %q, want %q", started.FactsSource, FactsSourceLive)
	}

	if err := os.RemoveAll(filepath.Join(repoRoot, ".git")); err != nil {
		t.Fatalf("RemoveAll(.git) error = %v", err)
	}

	status, err := service.Status(ctx, "alpha")
	if err != nil {
		t.Fatalf("Status(alpha) error = %v", err)
	}
	if status.State != StateLive {
		t.Fatalf("State = %q, want %q", status.State, StateLive)
	}
	if status.FactsSource != FactsSourceLastKnown {
		t.Fatalf("FactsSource = %q, want %q", status.FactsSource, FactsSourceLastKnown)
	}
	if status.Branch != "main" {
		t.Fatalf("Branch = %q, want main", status.Branch)
	}
	if status.CurrentCwd != subdir {
		t.Fatalf("CurrentCwd = %q, want %q", status.CurrentCwd, subdir)
	}
	if status.WorkspaceEligible {
		t.Fatalf("WorkspaceEligible = %t, want false", status.WorkspaceEligible)
	}
	if status.WorkspaceReason != "git_root is not a Git repository" {
		t.Fatalf("WorkspaceReason = %q, want git_root error", status.WorkspaceReason)
	}
	record, err := store.GetProjectionFreshness(ctx, "workspace:alpha")
	if err != nil {
		t.Fatalf("GetProjectionFreshness(workspace:alpha) after degraded status error = %v", err)
	}
	if record.Status != string(FactsSourceLastKnown) {
		t.Fatalf("freshness status after degraded status = %q, want %q", record.Status, FactsSourceLastKnown)
	}

	listed, err := service.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("List() length = %d, want 1", len(listed))
	}
	if listed[0].State != StateLive {
		t.Fatalf("List()[0].State = %q, want %q", listed[0].State, StateLive)
	}
	if listed[0].FactsSource != FactsSourceLastKnown {
		t.Fatalf("List()[0].FactsSource = %q, want %q", listed[0].FactsSource, FactsSourceLastKnown)
	}
	if listed[0].WorkspaceEligible {
		t.Fatalf("List()[0].WorkspaceEligible = %t, want false", listed[0].WorkspaceEligible)
	}
	if listed[0].WorkspaceReason != "git_root is not a Git repository" {
		t.Fatalf("List()[0].WorkspaceReason = %q, want git_root error", listed[0].WorkspaceReason)
	}

	stopped, err := service.Stop(ctx, "alpha", true)
	if err != nil {
		t.Fatalf("Stop(alpha, force=true) error = %v", err)
	}
	if stopped.State != StateStopped {
		t.Fatalf("State = %q, want %q", stopped.State, StateStopped)
	}
	if stopped.FactsSource != FactsSourceLastKnown {
		t.Fatalf("FactsSource = %q, want %q", stopped.FactsSource, FactsSourceLastKnown)
	}
	if stopped.Branch != "main" {
		t.Fatalf("Branch = %q, want main", stopped.Branch)
	}
	if stopped.CurrentCwd != subdir {
		t.Fatalf("CurrentCwd = %q, want %q", stopped.CurrentCwd, subdir)
	}
	if stopped.WorkspaceEligible {
		t.Fatalf("WorkspaceEligible = %t, want false", stopped.WorkspaceEligible)
	}
	if stopped.WorkspaceReason != "git_root is not a Git repository" {
		t.Fatalf("WorkspaceReason = %q, want git_root error", stopped.WorkspaceReason)
	}
}

func openWorkspaceTestStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func createGitRepo(t *testing.T, branch string) string {
	t.Helper()

	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(root) error = %v", err)
	}
	runGit(t, root, "init", "-b", branch)
	runGit(t, root, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "init")
	return root
}

func writeWorkspaceRegistry(t *testing.T, projects map[string]string) coreprojects.Registry {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "projects.yaml")
	builder := &strings.Builder{}
	builder.WriteString("version: 1\nprojects:\n")
	for key, gitRoot := range projects {
		builder.WriteString("  - key: " + key + "\n")
		builder.WriteString("    name: " + key + "\n")
		builder.WriteString("    project_class: local_git_project\n")
		builder.WriteString("    git_root: " + gitRoot + "\n")
		builder.WriteString("    default_branch: main\n")
		builder.WriteString("    policy:\n")
		builder.WriteString("      allowed_commands: [status]\n")
		builder.WriteString("      branch_rules:\n")
		builder.WriteString("        protected_branches: [main]\n")
		builder.WriteString("        require_worktree: true\n")
		builder.WriteString("        require_task_branch: true\n")
		builder.WriteString("        allow_default_branch_mutation: false\n")
		builder.WriteString("      approval_gates:\n")
		builder.WriteString("        require_for_governance_changes: true\n")
		builder.WriteString("        require_for_destructive_operations: true\n")
		builder.WriteString("        require_for_system_project_changes: false\n")
		builder.WriteString("      merge_policy:\n")
		builder.WriteString("        mode: squash\n")
		builder.WriteString("        allow_direct_to_default_branch: false\n")
		builder.WriteString("      destructive_operations:\n")
		builder.WriteString("        allow_reset: false\n")
		builder.WriteString("        allow_clean: false\n")
		builder.WriteString("        allow_force_push: false\n")
		builder.WriteString("        require_explicit_approval: true\n")
	}
	if err := os.WriteFile(configPath, []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("WriteFile(projects.yaml) error = %v", err)
	}

	registry, diagnostics, err := coreprojects.Register(configPath)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Register() diagnostics = %#v", diagnostics)
	}
	return registry
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	commandArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", commandArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v error = %v\n%s", args, err, output)
	}
}

type fakeSessionManager struct {
	sessions map[string]*fakeSession
}

type fakeSession struct {
	name       string
	cwd        string
	command    []string
	env        map[string]string
	attached   int
	currentCwd string
}

func newFakeSessionManager() *fakeSessionManager {
	return &fakeSessionManager{sessions: make(map[string]*fakeSession)}
}

func (manager *fakeSessionManager) HasSession(_ context.Context, name string) (bool, error) {
	_, ok := manager.sessions[name]
	return ok, nil
}

func (manager *fakeSessionManager) NewSession(_ context.Context, request StartRequest) error {
	manager.sessions[request.SessionName] = &fakeSession{
		name:       request.SessionName,
		cwd:        request.Cwd,
		command:    append([]string(nil), request.Command...),
		env:        make(map[string]string),
		currentCwd: request.Cwd,
	}
	return nil
}

func (manager *fakeSessionManager) SetEnvironment(_ context.Context, sessionName string, key string, value string) error {
	session, ok := manager.sessions[sessionName]
	if !ok {
		return os.ErrNotExist
	}
	session.env[key] = value
	return nil
}

func (manager *fakeSessionManager) ShowEnvironment(_ context.Context, sessionName string, key string) (string, error) {
	session, ok := manager.sessions[sessionName]
	if !ok {
		return "", os.ErrNotExist
	}
	return session.env[key], nil
}

func (manager *fakeSessionManager) CurrentPath(_ context.Context, sessionName string) (string, error) {
	session, ok := manager.sessions[sessionName]
	if !ok {
		return "", os.ErrNotExist
	}
	return session.currentCwd, nil
}

func (manager *fakeSessionManager) AttachedCount(_ context.Context, sessionName string) (int, error) {
	session, ok := manager.sessions[sessionName]
	if !ok {
		return 0, os.ErrNotExist
	}
	return session.attached, nil
}

func (manager *fakeSessionManager) KillSession(_ context.Context, sessionName string) error {
	delete(manager.sessions, sessionName)
	return nil
}

func (manager *fakeSessionManager) AttachSession(_ context.Context, sessionName string) error {
	session, ok := manager.sessions[sessionName]
	if !ok {
		return os.ErrNotExist
	}
	session.attached++
	return nil
}

func (manager *fakeSessionManager) mustSession(t *testing.T, sessionName string) *fakeSession {
	t.Helper()

	session, ok := manager.sessions[sessionName]
	if !ok {
		t.Fatalf("expected session %q", sessionName)
	}
	return session
}
