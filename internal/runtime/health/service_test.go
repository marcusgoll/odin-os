package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/store/sqlite"
)

func TestDoctorReportIsHealthyWhenChecksAreFresh(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	store := openStore(t)
	defer store.Close()

	project, err := store.CreateProject(context.Background(), sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(context.Background(), sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "task-1",
		Title:       "Task 1",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if _, err := store.StartRun(context.Background(), sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	}); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	if _, err := store.RecordExecutorHealth(context.Background(), sqlite.RecordExecutorHealthParams{
		Executor:    "codex",
		Status:      "healthy",
		LatencyMS:   42,
		DetailsJSON: `{"mode":"local"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}

	if _, err := store.RecordRegistryVersion(context.Background(), sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "abc123",
		Notes:       "fresh compile",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}

	if _, err := store.RecordProjectionFreshness(context.Background(), sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "healthy",
		DetailsJSON: `{"source":"runtime"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}

	service := Service{
		DB: store.DB(),
		Config: Config{
			QueuePressureThreshold: 5,
			ExecutorFreshnessTTL:   time.Hour,
			SourceFreshnessTTL:     time.Hour,
			ProjectionFreshnessTTL: time.Hour,
		},
		Now: func() time.Time { return now },
	}

	report, err := service.Doctor(context.Background(), true)
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if report.Status != StatusHealthy {
		t.Fatalf("Status = %q, want %q", report.Status, StatusHealthy)
	}
	if len(report.Checks) != 6 {
		t.Fatalf("Checks len = %d, want 6", len(report.Checks))
	}
}

func TestDoctorReportIncludesCodexAndE2EReadinessWhenRepoRootIsConfigured(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	store := openStore(t)
	defer store.Close()
	seedFreshHealthState(t, store, now)

	repoRoot := t.TempDir()
	writeDoctorReadinessFile(t, repoRoot, "AGENTS.md", "## Required verification for Odin-OS changes\nmake odin-e2e-local\n")
	writeDoctorReadinessFile(t, repoRoot, "WORKFLOW.md", "## Verify\nRun make odin-e2e-local for Odin changes.\n")
	writeDoctorReadinessFile(t, repoRoot, "Makefile", "odin-e2e-local:\n\t./scripts/odin-e2e-local.sh\n")
	writeDoctorReadinessFile(t, repoRoot, "scripts/odin-e2e-local.sh", "#!/usr/bin/env bash\n")
	writeDoctorReadinessFile(t, repoRoot, "fixtures/e2e/github-readonly-intake.yaml", "name: github-readonly-intake\n")
	writeDoctorReadinessFile(t, repoRoot, "internal/e2e/run.go", "package e2e\n")

	service := Service{
		DB:       store.DB(),
		RepoRoot: repoRoot,
		Config: Config{
			QueuePressureThreshold: 5,
			ExecutorFreshnessTTL:   time.Hour,
			SourceFreshnessTTL:     time.Hour,
			ProjectionFreshnessTTL: time.Hour,
		},
		Env: map[string]string{
			"ODIN_DRY_RUN":     "true",
			"ODIN_KILL_SWITCH": "false",
		},
		LookPath: func(name string) (string, error) {
			if name != "codex" {
				t.Fatalf("LookPath(%q), want codex", name)
			}
			return "/usr/local/bin/codex", nil
		},
		RunCommand: func(ctx context.Context, name string, args ...string) error {
			if name != "codex" || len(args) != 2 || args[0] != "exec" || args[1] != "--help" {
				t.Fatalf("RunCommand(%q, %v), want codex exec --help", name, args)
			}
			return nil
		},
		Now: func() time.Time { return now },
	}

	report, err := service.Doctor(context.Background(), true)
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	for _, name := range []string{
		"codex_cli",
		"codex_exec",
		"odin_e2e",
		"odin_e2e_command",
		"agents_e2e_rule",
		"workflow_e2e_rule",
		"github_token",
		"dry_run_mode",
		"kill_switch",
	} {
		check := findCheck(t, report, name)
		if check.Status != StatusHealthy {
			t.Fatalf("%s status = %q, want %q: %s", name, check.Status, StatusHealthy, check.Summary)
		}
		if check.Summary == "" {
			t.Fatalf("%s Summary is empty", name)
		}
		if check.Details["status"] == "" && (name == "dry_run_mode" || name == "kill_switch") {
			t.Fatalf("%s details missing status: %+v", name, check.Details)
		}
	}
}

func TestDoctorReportDegradesWhenGitHubTokenIsRequiredAndMissing(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	store := openStore(t)
	defer store.Close()
	seedFreshHealthState(t, store, now)

	service := Service{
		DB:       store.DB(),
		RepoRoot: t.TempDir(),
		Config: Config{
			QueuePressureThreshold: 5,
			ExecutorFreshnessTTL:   time.Hour,
			SourceFreshnessTTL:     time.Hour,
			ProjectionFreshnessTTL: time.Hour,
		},
		Env:      map[string]string{"ODIN_DRY_RUN": "false", "ODIN_PROFILE": "github-readonly"},
		LookPath: func(string) (string, error) { return "/usr/local/bin/codex", nil },
		RunCommand: func(context.Context, string, ...string) error {
			return nil
		},
		Now: func() time.Time { return now },
	}

	report, err := service.Doctor(context.Background(), true)
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	check := findCheck(t, report, "github_token")
	if check.Status != StatusDegraded {
		t.Fatalf("github_token status = %q, want %q", check.Status, StatusDegraded)
	}
	if report.Status != StatusDegraded {
		t.Fatalf("report status = %q, want %q", report.Status, StatusDegraded)
	}
}

func TestDoctorReportIsDegradedWhenQueueAndFreshnessAreStale(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	store := openStore(t)
	defer store.Close()

	project, err := store.CreateProject(context.Background(), sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	for index := 0; index < 4; index++ {
		if _, err := store.CreateTask(context.Background(), sqlite.CreateTaskParams{
			ProjectID:   project.ID,
			Key:         "queued-task-" + string(rune('a'+index)),
			Title:       "Queued task",
			Status:      "queued",
			Scope:       "project",
			RequestedBy: "operator",
		}); err != nil {
			t.Fatalf("CreateTask(%d) error = %v", index, err)
		}
	}

	if _, err := store.RecordExecutorHealth(context.Background(), sqlite.RecordExecutorHealthParams{
		Executor:    "codex",
		Status:      "healthy",
		LatencyMS:   42,
		DetailsJSON: `{"mode":"local"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}
	if _, err := store.DB().ExecContext(context.Background(), `
		UPDATE executor_health
		SET checked_at = ?
	`, now.Add(-2*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatalf("force stale executor health error = %v", err)
	}

	if _, err := store.RecordRegistryVersion(context.Background(), sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "abc123",
		Notes:       "stale compile",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}
	if _, err := store.DB().ExecContext(context.Background(), `
		UPDATE registry_versions
		SET compiled_at = ?
	`, now.Add(-2*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatalf("force stale registry version error = %v", err)
	}

	if _, err := store.RecordProjectionFreshness(context.Background(), sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "healthy",
		DetailsJSON: `{"source":"runtime"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}
	if _, err := store.DB().ExecContext(context.Background(), `
		UPDATE projection_freshness
		SET refreshed_at = ?, updated_at = ?
		WHERE surface = 'doctor'
	`, now.Add(-2*time.Hour).Format(time.RFC3339), now.Add(-2*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatalf("force stale projection freshness error = %v", err)
	}

	service := Service{
		DB: store.DB(),
		Config: Config{
			QueuePressureThreshold: 3,
			ExecutorFreshnessTTL:   30 * time.Minute,
			SourceFreshnessTTL:     30 * time.Minute,
			ProjectionFreshnessTTL: 30 * time.Minute,
		},
		Now: func() time.Time { return now },
	}

	report, err := service.Doctor(context.Background(), false)
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if report.Status != StatusDegraded {
		t.Fatalf("Status = %q, want %q", report.Status, StatusDegraded)
	}
}

func TestDoctorReportIsFailedWhenDatabaseIsUnavailable(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	service := Service{
		DB:     store.DB(),
		Config: DefaultConfig(),
	}

	report, err := service.Doctor(context.Background(), true)
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if report.Status != StatusFailed {
		t.Fatalf("Status = %q, want %q", report.Status, StatusFailed)
	}
}

func seedFreshHealthState(t *testing.T, store *sqlite.Store, now time.Time) {
	t.Helper()

	project, err := store.CreateProject(context.Background(), sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if _, err := store.CreateTask(context.Background(), sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "task-1",
		Title:       "Task 1",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	}); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := store.RecordExecutorHealth(context.Background(), sqlite.RecordExecutorHealthParams{
		Executor:    "codex",
		Status:      "healthy",
		LatencyMS:   42,
		DetailsJSON: `{"mode":"local"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}
	if _, err := store.RecordRegistryVersion(context.Background(), sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "abc123",
		Notes:       "fresh compile",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}
	if _, err := store.RecordProjectionFreshness(context.Background(), sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "healthy",
		DetailsJSON: `{"source":"runtime"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}
}

func writeDoctorReadinessFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func findCheck(t *testing.T, report Report, name string) Check {
	t.Helper()

	for _, check := range report.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("missing check %q in %+v", name, report.Checks)
	return Check{}
}

func openStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}
