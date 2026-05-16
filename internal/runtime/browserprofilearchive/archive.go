package browserprofilearchive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func Pack(sourceDir string) ([]byte, error) {
	sourceDir = filepath.Clean(strings.TrimSpace(sourceDir))
	if sourceDir == "" || !filepath.IsAbs(sourceDir) {
		return nil, fmt.Errorf("browser profile archive source directory must be absolute")
	}
	info, err := os.Stat(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("browser profile archive source stat: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("browser profile archive source must be a directory")
	}

	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	walkErr := filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name, err := normalizeArchiveName(rel)
		if err != nil {
			return err
		}
		if shouldSkipArchiveEntry(entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		header.Mode = int64(clampArchiveMode(info.Mode().Perm(), entry.IsDir()))
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if walkErr != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		return nil, fmt.Errorf("browser profile archive pack: %w", walkErr)
	}
	if err := tarWriter.Close(); err != nil {
		_ = gzipWriter.Close()
		return nil, fmt.Errorf("browser profile archive close tar: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, fmt.Errorf("browser profile archive close gzip: %w", err)
	}
	return buffer.Bytes(), nil
}

func Unpack(payload []byte, targetDir string) error {
	targetDir = filepath.Clean(strings.TrimSpace(targetDir))
	if targetDir == "" || !filepath.IsAbs(targetDir) {
		return fmt.Errorf("browser profile archive target directory must be absolute")
	}
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return fmt.Errorf("browser profile archive create target: %w", err)
	}
	gzipReader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("browser profile archive decode gzip: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("browser profile archive read tar: %w", err)
		}
		name, err := normalizeArchiveName(header.Name)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(targetDir, filepath.FromSlash(name))
		if err := ensureUnderTarget(targetDir, targetPath); err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o700); err != nil {
				return fmt.Errorf("browser profile archive create directory: %w", err)
			}
			if err := os.Chmod(targetPath, fs.FileMode(clampArchiveMode(fs.FileMode(header.Mode), true))); err != nil {
				return fmt.Errorf("browser profile archive chmod directory: %w", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
				return fmt.Errorf("browser profile archive create parent: %w", err)
			}
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fs.FileMode(clampArchiveMode(fs.FileMode(header.Mode), false)))
			if err != nil {
				return fmt.Errorf("browser profile archive create file: %w", err)
			}
			_, copyErr := io.Copy(file, tarReader)
			closeErr := file.Close()
			if copyErr != nil {
				return fmt.Errorf("browser profile archive write file: %w", copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("browser profile archive close file: %w", closeErr)
			}
		default:
			return fmt.Errorf("browser profile archive unsupported entry type %d", header.Typeflag)
		}
	}
	return nil
}

func normalizeArchiveName(name string) (string, error) {
	name = filepath.ToSlash(filepath.Clean(strings.TrimSpace(name)))
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("browser profile archive entry must stay relative")
	}
	return name, nil
}

func ensureUnderTarget(targetDir string, targetPath string) error {
	rel, err := filepath.Rel(targetDir, targetPath)
	if err != nil {
		return fmt.Errorf("browser profile archive target relative path: %w", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("browser profile archive target escapes directory")
	}
	return nil
}

func shouldSkipArchiveEntry(entry fs.DirEntry) bool {
	name := entry.Name()
	if strings.HasPrefix(name, ".org.chromium.Chromium.") {
		return true
	}
	switch name {
	case "SingletonCookie", "SingletonLock", "SingletonSocket":
		return true
	}
	info, err := entry.Info()
	if err != nil {
		return true
	}
	mode := info.Mode()
	if mode&fs.ModeSymlink != 0 || mode&fs.ModeSocket != 0 || mode&fs.ModeDevice != 0 || mode&fs.ModeNamedPipe != 0 || mode&fs.ModeIrregular != 0 {
		return true
	}
	return false
}

func clampArchiveMode(mode fs.FileMode, directory bool) fs.FileMode {
	if directory {
		return 0o700
	}
	return 0o600
}
