package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/store/sqlite"
)

func TestMediaStackAcceptance(t *testing.T) {
	ctx := context.Background()
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)

	t.Run("top level surfaces expose the media acceptance path", func(t *testing.T) {
		readme := mustReadFile(t, filepath.Join(repoRoot, "README.md"))
		makefile := mustReadFile(t, filepath.Join(repoRoot, "Makefile"))
		cutover := mustReadFile(t, filepath.Join(repoRoot, "docs", "operations", "cutover-readiness.md"))

		if !strings.Contains(readme, "make test-media") {
			t.Fatalf("README.md missing media acceptance command")
		}
		if !strings.Contains(makefile, "test-media:") {
			t.Fatalf("Makefile missing test-media target")
		}
		if !strings.Contains(cutover, "make test-media") {
			t.Fatalf("cutover-readiness.md missing media acceptance verification step")
		}
	})

	t.Run("doctor and healthcheck respect media fixtures", func(t *testing.T) {
		mediaConfig := writeMediaAcceptanceConfig(t)
		healthyRoot := t.TempDir()
		degradedRoot := t.TempDir()

		doctorOutput, err := runOdinCommand(t, repoRoot, odinBinary, healthyRoot, map[string]string{
			"ODIN_HTTP_ADDR":           "127.0.0.1:0",
			"ODIN_MEDIA_CONFIG":        mediaConfig,
			"ODIN_MEDIA_PROBE_COMMAND": filepath.Join(repoRoot, "scripts", "tests", "fixtures", "media-probe-ok.sh"),
		}, "", "doctor", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(doctor --json) error = %v\n%s", err, doctorOutput)
		}
		if !strings.Contains(doctorOutput, "\"media.mounts\"") || !strings.Contains(doctorOutput, "\"media.vpn\"") {
			t.Fatalf("doctor output = %q, want media checks", doctorOutput)
		}

		healthcheckOutput, err := runOdinCommand(t, repoRoot, odinBinary, degradedRoot, map[string]string{
			"ODIN_HTTP_ADDR":           "127.0.0.1:0",
			"ODIN_MEDIA_CONFIG":        mediaConfig,
			"ODIN_MEDIA_PROBE_COMMAND": filepath.Join(repoRoot, "scripts", "tests", "fixtures", "media-probe-mount-mismatch.sh"),
		}, "", "healthcheck")
		if err == nil {
			t.Fatalf("runOdinCommand(healthcheck) error = nil, want fail-closed media readiness\n%s", healthcheckOutput)
		}
		if !strings.Contains(healthcheckOutput, "not ready: failed") {
			t.Fatalf("healthcheck output = %q, want fail-closed readiness message", healthcheckOutput)
		}
	})

	t.Run("serve records incidents without queueing media work", func(t *testing.T) {
		mediaConfig := writeMediaAcceptanceConfig(t)
		runtimeRoot := t.TempDir()
		commandCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		cmd := exec.CommandContext(commandCtx, odinBinary, "serve")
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(),
			"ODIN_ROOT="+runtimeRoot,
			"ODIN_HTTP_ADDR=127.0.0.1:0",
			"ODIN_MEDIA_CONFIG="+mediaConfig,
			"ODIN_MEDIA_PROBE_COMMAND="+filepath.Join(repoRoot, "scripts", "tests", "fixtures", "media-probe-mount-mismatch.sh"),
		)
		output, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("serve output = %q, want bounded timeout exit", string(output))
		}

		store, err := sqlite.Open(filepath.Join(runtimeRoot, "data", "odin.db"))
		if err != nil {
			t.Fatalf("sqlite.Open() error = %v", err)
		}
		defer store.Close()

		var mediaIncidents int
		if err := store.DB().QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM incidents
			WHERE details_json LIKE '%"domain":"media"%'
		`).Scan(&mediaIncidents); err != nil {
			t.Fatalf("count media incidents: %v", err)
		}
		if mediaIncidents != 1 {
			t.Fatalf("media incidents = %d, want 1", mediaIncidents)
		}

		var queuedMediaTasks int
		if err := store.DB().QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM tasks
			WHERE requested_by = 'media-supervisor' AND status = 'queued'
		`).Scan(&queuedMediaTasks); err != nil {
			t.Fatalf("count queued media tasks: %v", err)
		}
		if queuedMediaTasks != 0 {
			t.Fatalf("queued media tasks = %d, want 0", queuedMediaTasks)
		}
	})

	t.Run("backup and verify-backup still work with the media profile enabled", func(t *testing.T) {
		mediaConfig := writeMediaAcceptanceConfig(t)
		runtimeRoot := t.TempDir()
		archivePath := filepath.Join(t.TempDir(), "media-acceptance-backup.tar.gz")

		backupOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, map[string]string{
			"ODIN_MEDIA_CONFIG": mediaConfig,
		}, "", "backup", archivePath)
		if err != nil {
			t.Fatalf("runOdinCommand(backup) error = %v\n%s", err, backupOutput)
		}

		verifyOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, map[string]string{
			"ODIN_MEDIA_CONFIG": mediaConfig,
		}, "", "verify-backup", archivePath)
		if err != nil {
			t.Fatalf("runOdinCommand(verify-backup) error = %v\n%s", err, verifyOutput)
		}
	})
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return string(content)
}

func writeMediaAcceptanceConfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "media-stack.yaml")
	content := `
enabled: true
maintenance_window: "Fri 00:00-23:59"
services:
  - name: plex
    kind: plex
policies:
  notify_only:
    - media_maintenance_candidate
`
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(media-stack.yaml) error = %v", err)
	}
	return path
}
