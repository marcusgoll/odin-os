package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"odin-os/internal/registry"
)

func resolveSkillHandlerPath(repoRoot string, handlerRef string) (string, error) {
	trimmed := strings.TrimSpace(handlerRef)
	if trimmed == "" {
		return "", fmt.Errorf("skill handler_ref is required")
	}

	cleaned := filepath.Clean(trimmed)
	if cleaned == "" {
		return "", fmt.Errorf("skill handler_ref is required")
	}
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("skill handler_ref must stay within the repo")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("skill handler_ref must stay within the repo")
	}

	path := filepath.Join(repoRoot, cleaned)
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}

	if err := ensureRepoRelativePath(repoRoot, resolvedPath); err != nil {
		return "", err
	}
	if err := ensureAllowedSkillHandlerRoot(repoRoot, resolvedPath); err != nil {
		return "", err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("skill handler %q is a directory", handlerRef)
	}
	if info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("skill handler %q is not executable", handlerRef)
	}

	return resolvedPath, nil
}

func ensureRepoRelativePath(repoRoot string, resolvedPath string) error {
	relative, err := filepath.Rel(repoRoot, resolvedPath)
	if err != nil {
		return err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("skill handler_ref must stay within the repo")
	}
	return nil
}

func ensureAllowedSkillHandlerRoot(repoRoot string, resolvedPath string) error {
	allowedRoot := filepath.Join(repoRoot, registry.SkillHandlerRoot)
	relative, err := filepath.Rel(allowedRoot, resolvedPath)
	if err != nil {
		return err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("skill handler_ref must resolve under %s", registry.SkillHandlerRoot)
	}
	return nil
}
