package backup_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/app/backup"
	"odin-os/internal/store/sqlite"
)

func TestServiceBackupRestoreAndVerifyRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repoRoot := filepath.Join(t.TempDir(), "repo")
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	archivePath := filepath.Join(t.TempDir(), "odin-backup.tar.gz")
	restoreRoot := filepath.Join(t.TempDir(), "restore")

	prepareBackupFixture(t, repoRoot, runtimeRoot)

	service := backup.Service{
		RepoRoot:    repoRoot,
		RuntimeRoot: runtimeRoot,
	}

	if err := service.CreateArchive(ctx, archivePath); err != nil {
		t.Fatalf("CreateArchive() error = %v", err)
	}
	if err := service.VerifyArchive(ctx, archivePath); err != nil {
		t.Fatalf("VerifyArchive() error = %v", err)
	}
	if err := service.RestoreArchive(ctx, archivePath, restoreRoot); err != nil {
		t.Fatalf("RestoreArchive() error = %v", err)
	}

	for _, relativePath := range []string{
		"data/odin.db",
		"registry/agents/example.md",
		"memory/projects/example.md",
		"config/odin.yaml",
		"config/projects.yaml",
		"config/policies.yaml",
		"config/telemetry.yaml",
		"config/executors.yaml",
		"config/models.yaml",
	} {
		if _, err := os.Stat(filepath.Join(restoreRoot, relativePath)); err != nil {
			t.Fatalf("restored file %s missing: %v", relativePath, err)
		}
	}

	store, err := sqlite.Open(filepath.Join(restoreRoot, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open(restored) error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(restored) error = %v", err)
	}
}

func TestServiceVerifyArchiveFailsWhenDatabaseMissing(t *testing.T) {
	t.Parallel()

	archivePath := filepath.Join(t.TempDir(), "invalid.tar.gz")
	writeArchive(t, archivePath, map[string]string{
		"registry/agents/example.md": "# missing database\n",
	})

	service := backup.Service{}
	if err := service.VerifyArchive(context.Background(), archivePath); err == nil {
		t.Fatalf("VerifyArchive() error = nil, want failure for missing database")
	}
}

func prepareBackupFixture(t *testing.T, repoRoot string, runtimeRoot string) {
	t.Helper()

	mustWriteFile(t, filepath.Join(repoRoot, "registry", "agents", "example.md"), "# example agent\n")
	mustWriteFile(t, filepath.Join(repoRoot, "memory", "projects", "example.md"), "# example memory\n")
	mustWriteFile(t, filepath.Join(repoRoot, "config", "odin.yaml"), "version: 1\n")
	mustWriteFile(t, filepath.Join(repoRoot, "config", "projects.yaml"), "version: 1\nprojects: []\n")
	mustWriteFile(t, filepath.Join(repoRoot, "config", "policies.yaml"), "version: 1\n")
	mustWriteFile(t, filepath.Join(repoRoot, "config", "telemetry.yaml"), "version: 1\n")
	mustWriteFile(t, filepath.Join(repoRoot, "config", "executors.yaml"), "version: 1\nexecutors: []\nroutes: []\n")
	mustWriteFile(t, filepath.Join(repoRoot, "config", "models.yaml"), "version: 1\nmodels: []\n")

	if err := os.MkdirAll(filepath.Join(runtimeRoot, "data"), 0o755); err != nil {
		t.Fatalf("mkdir runtime data: %v", err)
	}

	store, err := sqlite.Open(filepath.Join(runtimeRoot, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func writeArchive(t *testing.T, path string, files map[string]string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	for name, content := range files {
		header := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("WriteHeader() error = %v", err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
}
