package extractor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func EmitDrafts(candidates []Candidate, outputRoot string, maxDrafts int) ([]string, error) {
	var emitted []string
	for _, candidate := range candidates {
		if maxDrafts > 0 && len(emitted) >= maxDrafts {
			break
		}
		if !supportsDraft(candidate) {
			continue
		}

		directory := draftDirectory(candidate.Kind)
		path := filepath.Join(outputRoot, directory, candidate.Key+".md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(renderDraft(candidate)), 0o644); err != nil {
			return nil, err
		}
		emitted = append(emitted, path)
	}
	return emitted, nil
}

func supportsDraft(candidate Candidate) bool {
	if !candidate.IsPrimary {
		return false
	}
	if candidate.Classification != ClassificationRewrite && candidate.Classification != ClassificationMigrateAsIs {
		return false
	}
	switch candidate.Kind {
	case KindSkill, KindAgent, KindWorkflow:
		return true
	default:
		return false
	}
}

func draftDirectory(kind Kind) string {
	switch kind {
	case KindSkill:
		return "skills"
	case KindAgent:
		return "agents"
	case KindWorkflow:
		return "workflows"
	default:
		return "drafts"
	}
}

func renderDraft(candidate Candidate) string {
	return strings.TrimSpace(fmt.Sprintf(
		"---\n"+
			"kind: %s\n"+
			"key: %s\n"+
			"title: %s\n"+
			"summary: Draft migrated from legacy source.\n"+
			"status: draft\n"+
			"tags:\n"+
			"  - migration-draft\n"+
			"owners:\n"+
			"  - odin\n"+
			"%s"+
			"---\n\n"+
			"## Purpose\n\n"+
			"Draft migration candidate derived from a legacy asset.\n\n"+
			"## When to Use\n\n"+
			"Use during migration review when deciding whether to promote or rewrite this asset.\n\n"+
			"## Inputs\n\n"+
			"- Review the legacy asset content and surrounding references.\n\n"+
			"## Procedure\n\n"+
			"Legacy source: `%s`\n\n"+
			"Review this draft against the canonical Odin OS contract before promotion.\n\n"+
			"## Outputs\n\n"+
			"- A normalized asset ready for maintainers to revise or promote.\n\n"+
			"## Constraints\n\n"+
			"- This file is a generated draft and is not canonical runtime authority.\n"+
			"- Preserve provenance to the legacy source during review.\n\n"+
			"## Success Criteria\n\n"+
			"- The draft conforms to the current registry contract.\n"+
			"- The asset is either promoted deliberately or rejected explicitly.\n",
		candidate.Kind,
		candidate.Key,
		candidate.Title,
		draftKindSpecificFrontmatter(candidate),
		candidate.RelativePath,
	)) + "\n"
}

func draftKindSpecificFrontmatter(candidate Candidate) string {
	switch candidate.Kind {
	case KindSkill:
		return fmt.Sprintf(
			"version: \"0.1.0\"\n"+
				"enabled: false\n"+
				"strictness: review\n"+
				"applies_to:\n"+
				"  - migration\n"+
				"scopes:\n"+
				"  - global\n"+
				"permissions:\n"+
				"  - repo.read\n"+
				"handler_type: command\n"+
				"handler_ref: scripts/skills/%s.sh\n"+
				"timeout_seconds: 15\n"+
				"input_schema:\n"+
				"  type: object\n"+
				"output_schema:\n"+
				"  type: object\n",
			candidate.Key,
		)
	case KindAgent:
		return "role: migration-review\nscopes:\n  - global\ntools:\n  - review\n"
	case KindWorkflow:
		return "entrypoint: migration-review\ncomposes:\n  - review\n"
	default:
		return ""
	}
}
