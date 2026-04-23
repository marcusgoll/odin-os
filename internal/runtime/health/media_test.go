package health

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	coremedia "odin-os/internal/core/media"
	"odin-os/internal/store/sqlite"
)

func TestMediaChecksOmittedWhenConfigIsNil(t *testing.T) {
	t.Parallel()

	checks, err := MediaChecks{Config: nil}.Checks(context.Background(), DefaultConfig(), nowFixture())
	if err != nil {
		t.Fatalf("Checks() error = %v", err)
	}
	if len(checks) != 0 {
		t.Fatalf("Checks() = %+v, want none", checks)
	}
}

func TestMediaChecksFailOnMountMismatch(t *testing.T) {
	t.Parallel()

	checks, err := MediaChecks{
		Config:       enabledMediaConfig(),
		ProbeCommand: fixtureProbePath(t, "media-probe-mount-mismatch.sh"),
	}.Checks(context.Background(), DefaultConfig(), nowFixture())
	if err != nil {
		t.Fatalf("Checks() error = %v", err)
	}

	assertCheckStatus(t, checks, "media.mounts", StatusFailed)
}

func TestMediaChecksFailWhenProbeCommandErrors(t *testing.T) {
	t.Parallel()

	checks, err := MediaChecks{
		Config:       enabledMediaConfig(),
		ProbeCommand: "/definitely/missing/media-probe-command",
	}.Checks(context.Background(), DefaultConfig(), nowFixture())
	if err != nil {
		t.Fatalf("Checks() error = %v", err)
	}

	assertCheckStatus(t, checks, "media.probe", StatusFailed)
}

func TestMediaChecksDegradeWhenPlexIsUnreachable(t *testing.T) {
	t.Parallel()

	config := enabledMediaConfig()
	config.Services = []coremedia.StackService{{
		Name:    "plex",
		Kind:    coremedia.ServiceKindPlex,
		BaseURL: "http://127.0.0.1:1",
	}}

	checks, err := MediaChecks{Config: config}.Checks(context.Background(), DefaultConfig(), nowFixture())
	if err != nil {
		t.Fatalf("Checks() error = %v", err)
	}

	assertCheckStatus(t, checks, "media.plex", StatusDegraded)
}

func TestMediaChecksFailOnVPNIntegritySignal(t *testing.T) {
	t.Parallel()

	script := writeInlineProbeScript(t, `{"signals":[{"name":"media.vpn","status":"failed","summary":"vpn integrity failed"}]}`)
	checks, err := MediaChecks{
		Config:       enabledMediaConfig(),
		ProbeCommand: script,
	}.Checks(context.Background(), DefaultConfig(), nowFixture())
	if err != nil {
		t.Fatalf("Checks() error = %v", err)
	}

	assertCheckStatus(t, checks, "media.vpn", StatusFailed)
}

func TestMediaChecksDegradeOnQueueBacklogSignal(t *testing.T) {
	t.Parallel()

	script := writeInlineProbeScript(t, `{"signals":[{"name":"media.queue","status":"degraded","summary":"queue backlog is growing"}]}`)
	checks, err := MediaChecks{
		Config:       enabledMediaConfig(),
		ProbeCommand: script,
	}.Checks(context.Background(), DefaultConfig(), nowFixture())
	if err != nil {
		t.Fatalf("Checks() error = %v", err)
	}

	assertCheckStatus(t, checks, "media.queue", StatusDegraded)
}

func TestDoctorIncludesMediaChecksInOverallStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openHealthTestStore(t)
	defer store.Close()
	seedHealthyObservability(t, ctx, store)

	report, err := Service{
		DB: store.DB(),
		Media: &MediaChecks{
			Config:       enabledMediaConfig(),
			ProbeCommand: fixtureProbePath(t, "media-probe-mount-mismatch.sh"),
		},
	}.Doctor(ctx, true)
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}

	if report.Status != StatusFailed {
		t.Fatalf("Doctor().Status = %q, want %q", report.Status, StatusFailed)
	}
	assertCheckStatus(t, report.Checks, "media.mounts", StatusFailed)
}

func openHealthTestStore(t *testing.T) *sqlite.Store {
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

func seedHealthyObservability(t *testing.T, ctx context.Context, store *sqlite.Store) {
	t.Helper()

	if _, err := store.RecordRegistryVersion(ctx, sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "phase-18",
		Notes:       "media health test sample",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}
	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "codex_headless",
		Status:      "healthy",
		LatencyMS:   10,
		DetailsJSON: `{"status":"healthy"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}
	if _, err := store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
		Surface:     "active_runs",
		Status:      "current",
		DetailsJSON: `{"source":"media-test"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}
}

func enabledMediaConfig() *coremedia.Config {
	return &coremedia.Config{
		Enabled: true,
		Services: []coremedia.StackService{
			{
				Name:    "plex",
				Kind:    coremedia.ServiceKindPlex,
				BaseURL: "http://127.0.0.1:32400",
			},
		},
	}
}

func nowFixture() time.Time {
	return time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
}

func fixtureProbePath(t *testing.T, name string) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller() failed")
	}
	return filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "scripts", "tests", "fixtures", name)
}

func writeInlineProbeScript(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "probe.sh")
	content := "#!/usr/bin/env bash\ncat <<'EOF'\n" + body + "\nEOF\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func assertCheckStatus(t *testing.T, checks []Check, name string, want Status) {
	t.Helper()

	for _, check := range checks {
		if check.Name == name {
			if check.Status != want {
				t.Fatalf("check %s status = %q, want %q", name, check.Status, want)
			}
			return
		}
	}
	t.Fatalf("check %s not found in %+v", name, checks)
}
