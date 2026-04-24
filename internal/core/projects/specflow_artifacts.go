package projects

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

type SpecFlowArtifact struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

func ListSpecFlowArtifacts(gitRoot string) ([]SpecFlowArtifact, error) {
	root := strings.TrimSpace(gitRoot)
	if root == "" {
		return nil, nil
	}

	var artifacts []SpecFlowArtifact
	for _, scanRoot := range []string{"specs", "epics"} {
		base := filepath.Join(root, scanRoot)
		if err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if path == base {
					return nil
				}
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}

			kind, ok := specFlowArtifactKind(entry.Name())
			if !ok {
				return nil
			}
			relativePath, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			artifacts = append(artifacts, SpecFlowArtifact{
				Path: filepath.ToSlash(relativePath),
				Kind: kind,
			})
			return nil
		}); err != nil {
			return nil, err
		}
	}

	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].Path < artifacts[j].Path
	})
	return artifacts, nil
}

func specFlowArtifactKind(name string) (string, bool) {
	switch name {
	case "spec.md":
		return "feature_spec", true
	case "plan.md":
		return "feature_plan", true
	case "tasks.md":
		return "feature_tasks", true
	case "state.yaml":
		return "state", true
	default:
		return "", false
	}
}
