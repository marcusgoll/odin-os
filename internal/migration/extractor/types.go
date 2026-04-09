package extractor

import (
	"path/filepath"
	"strings"
)

type Kind string

const (
	KindUnknown             Kind = ""
	KindSkill               Kind = "skill"
	KindAgent               Kind = "agent"
	KindWorkflow            Kind = "workflow"
	KindPrompt              Kind = "prompt"
	KindArchitectureDoc     Kind = "architecture_doc"
	KindOperationalPlaybook Kind = "operational_playbook"
)

type Classification string

const (
	ClassificationMigrateAsIs   Classification = "migrate_as_is"
	ClassificationRewrite       Classification = "rewrite"
	ClassificationReferenceOnly Classification = "reference_only"
	ClassificationArchive       Classification = "archive"
	ClassificationDelete        Classification = "delete"
)

type Candidate struct {
	SourcePath     string         `json:"source_path"`
	RelativePath   string         `json:"relative_path"`
	Kind           Kind           `json:"kind"`
	Key            string         `json:"key"`
	Title          string         `json:"title"`
	ContentHash    string         `json:"content_hash"`
	PathSignals    []string       `json:"path_signals"`
	Classification Classification `json:"classification,omitempty"`
	Rationale      string         `json:"rationale,omitempty"`
	DuplicateGroup string         `json:"duplicate_group,omitempty"`
	IsPrimary      bool           `json:"is_primary,omitempty"`
}

func normalizeSlashes(path string) string {
	return filepath.ToSlash(path)
}

func normalizedKeyFromPath(relativePath string) string {
	relativePath = normalizeSlashes(relativePath)
	base := strings.TrimSuffix(filepath.Base(relativePath), filepath.Ext(relativePath))
	if base == "SKILL" {
		base = filepath.Base(filepath.Dir(relativePath))
	}
	return strings.ToLower(strings.ReplaceAll(base, " ", "-"))
}
