package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type Service struct {
	RepoRoot    string
	RuntimeRoot string
}

func (service Service) CreateArchive(_ context.Context, archivePath string) error {
	if service.RepoRoot == "" {
		return fmt.Errorf("repo root is required")
	}
	if service.RuntimeRoot == "" {
		service.RuntimeRoot = service.RepoRoot
	}

	snapshotPath, err := sqliteSnapshot(filepath.Join(service.RuntimeRoot, "data", "odin.db"))
	if err != nil {
		return err
	}
	defer os.Remove(snapshotPath)

	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		return err
	}

	file, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	if err := addFile(tarWriter, snapshotPath, "data/odin.db"); err != nil {
		return err
	}
	if err := addTree(tarWriter, filepath.Join(service.RepoRoot, "registry"), "registry"); err != nil {
		return err
	}
	if err := addTree(tarWriter, filepath.Join(service.RepoRoot, "memory"), "memory"); err != nil {
		return err
	}
	for _, relativePath := range []string{
		"config/odin.yaml",
		"config/projects.yaml",
		"config/policies.yaml",
		"config/telemetry.yaml",
		"config/executors.yaml",
		"config/models.yaml",
	} {
		if err := addFile(tarWriter, filepath.Join(service.RepoRoot, filepath.FromSlash(relativePath)), relativePath); err != nil {
			return err
		}
	}

	return nil
}

func (service Service) RestoreArchive(_ context.Context, archivePath string, destinationRoot string) error {
	if destinationRoot == "" {
		return fmt.Errorf("destination root is required")
	}

	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		targetPath, err := archiveTargetPath(destinationRoot, header.Name)
		if err != nil {
			return err
		}
		if header.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		targetFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
		if err != nil {
			return err
		}
		if _, err := io.Copy(targetFile, tarReader); err != nil {
			targetFile.Close()
			return err
		}
		if err := targetFile.Close(); err != nil {
			return err
		}
	}
}

func (service Service) VerifyArchive(ctx context.Context, archivePath string) error {
	verifyRoot := filepath.Join(os.TempDir(), "odin-verify")
	if err := os.MkdirAll(verifyRoot, 0o755); err != nil {
		return err
	}
	tempRoot, err := os.MkdirTemp(verifyRoot, "backup-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempRoot)

	if err := service.RestoreArchive(ctx, archivePath, tempRoot); err != nil {
		return err
	}

	dbPath := filepath.Join(tempRoot, "data", "odin.db")
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("backup archive is missing data/odin.db: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	return db.PingContext(ctx)
}

func sqliteSnapshot(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}

	tempFile, err := os.CreateTemp("", "odin-db-*.sqlite")
	if err != nil {
		return "", err
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		return "", err
	}
	if err := os.Remove(tempPath); err != nil {
		return "", err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return "", err
	}
	defer db.Close()

	statement := "VACUUM INTO '" + strings.ReplaceAll(tempPath, "'", "''") + "'"
	if _, err := db.Exec(statement); err != nil {
		return "", err
	}
	return tempPath, nil
}

func addTree(writer *tar.Writer, sourceRoot string, archiveRoot string) error {
	info, err := os.Stat(sourceRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return addFile(writer, sourceRoot, archiveRoot)
	}

	return filepath.Walk(sourceRoot, func(path string, info fs.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == sourceRoot {
			return nil
		}

		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		return addFile(writer, path, filepath.ToSlash(filepath.Join(archiveRoot, relative)))
	})
}

func addFile(writer *tar.Writer, sourcePath string, archivePath string) error {
	info, err := os.Stat(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(archivePath)
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	if info.IsDir() {
		return nil
	}

	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(writer, file)
	return err
}

func archiveTargetPath(destinationRoot string, archiveName string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(archiveName))
	targetPath := filepath.Join(destinationRoot, clean)
	rootClean := filepath.Clean(destinationRoot)
	if targetPath != rootClean && !strings.HasPrefix(targetPath, rootClean+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive path %q escapes restore root", archiveName)
	}
	return targetPath, nil
}
