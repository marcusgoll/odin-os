package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLocalInstallScripts(t *testing.T) {
	repoRoot := projectRoot(t)
	homeDir := t.TempDir()
	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "odin-source")

	if err := os.WriteFile(sourcePath, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(sourcePath) error = %v", err)
	}

	install := exec.Command("bash", filepath.Join(repoRoot, "scripts", "dev", "install-local.sh"))
	install.Dir = repoRoot
	install.Env = append(os.Environ(),
		"HOME="+homeDir,
		"ODIN_INSTALL_SOURCE="+sourcePath,
	)
	output, err := install.CombinedOutput()
	if err != nil {
		t.Fatalf("install-local.sh error = %v\n%s", err, string(output))
	}

	targetPath := filepath.Join(homeDir, ".local", "bin", "odin")
	linkTarget, err := os.Readlink(targetPath)
	if err != nil {
		t.Fatalf("Readlink(%s) error = %v", targetPath, err)
	}
	if linkTarget != sourcePath {
		t.Fatalf("link target = %q, want %q", linkTarget, sourcePath)
	}

	uninstall := exec.Command("bash", filepath.Join(repoRoot, "scripts", "dev", "uninstall-local.sh"))
	uninstall.Dir = repoRoot
	uninstall.Env = append(os.Environ(),
		"HOME="+homeDir,
	)
	output, err = uninstall.CombinedOutput()
	if err != nil {
		t.Fatalf("uninstall-local.sh error = %v\n%s", err, string(output))
	}

	if _, err := os.Lstat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("Lstat(%s) error = %v, want not exists", targetPath, err)
	}
}
