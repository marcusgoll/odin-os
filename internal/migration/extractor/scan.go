package extractor

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var ignoredDirectories = map[string]bool{
	".git":         true,
	".cache":       true,
	".worktrees":   true,
	"node_modules": true,
	"vendor":       true,
	"tmp":          true,
}

func Scan(root string) ([]Candidate, error) {
	var candidates []Candidate

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relativePath = normalizeSlashes(relativePath)

		if entry.IsDir() {
			if relativePath != "." && ignoredDirectories[filepath.Base(relativePath)] {
				return filepath.SkipDir
			}
			return nil
		}

		kind := detectKind(relativePath)
		if kind == KindUnknown {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		candidates = append(candidates, Candidate{
			SourcePath:   path,
			RelativePath: relativePath,
			Kind:         kind,
			Key:          normalizedKeyFromPath(relativePath),
			Title:        extractTitle(relativePath, content),
			ContentHash:  hashContent(content),
			PathSignals:  detectPathSignals(relativePath),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(candidates, func(i int, j int) bool {
		return candidates[i].RelativePath < candidates[j].RelativePath
	})
	return candidates, nil
}

func detectKind(relativePath string) Kind {
	relativePath = normalizeSlashes(relativePath)
	switch {
	case strings.HasPrefix(relativePath, ".claude/skills/") && strings.HasSuffix(relativePath, "/SKILL.md"):
		return KindSkill
	case strings.HasPrefix(relativePath, ".agents/skills/") && strings.HasSuffix(relativePath, "/SKILL.md"):
		return KindSkill
	case strings.HasPrefix(relativePath, "prompts/"):
		return KindPrompt
	case strings.HasPrefix(relativePath, "specs/"):
		return KindWorkflow
	case strings.HasPrefix(relativePath, "docs/adr/"), strings.HasPrefix(relativePath, "docs/adrs/"), strings.HasPrefix(relativePath, "docs/plans/"):
		return KindArchitectureDoc
	case strings.HasPrefix(relativePath, "docs/process/"), strings.HasPrefix(relativePath, "docs/checklists/"), strings.HasPrefix(relativePath, "docs/deployments/"), strings.HasPrefix(relativePath, "ops/"):
		return KindOperationalPlaybook
	default:
		name := strings.ToLower(filepath.Base(relativePath))
		if strings.Contains(name, "prompt") {
			return KindPrompt
		}
		return KindUnknown
	}
}

func extractTitle(relativePath string, content []byte) string {
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			line = strings.TrimLeft(line, "#")
			line = strings.TrimSpace(line)
			if line != "" {
				return line
			}
		}
	}

	key := normalizedKeyFromPath(relativePath)
	parts := strings.Split(key, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func detectPathSignals(relativePath string) []string {
	parts := strings.Split(normalizeSlashes(relativePath), "/")
	signals := make([]string, 0, len(parts))
	for _, part := range parts {
		lower := strings.ToLower(part)
		switch {
		case lower == ".claude":
			signals = append(signals, "claude_root")
		case lower == ".agents":
			signals = append(signals, "agents_root")
		case lower == "docs":
			signals = append(signals, "docs_root")
		case lower == "ops":
			signals = append(signals, "ops_root")
		case lower == "specs":
			signals = append(signals, "specs_root")
		case lower == "prompts":
			signals = append(signals, "prompts_root")
		case strings.Contains(lower, "backup"):
			signals = append(signals, "backup_path")
		case strings.Contains(lower, "worktree"):
			signals = append(signals, "worktree_path")
		}
	}
	if len(signals) == 0 {
		return []string{"legacy_path"}
	}
	return signals
}

func hashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
