package extractor_test

import (
	"testing"

	"odin-os/internal/migration/extractor"
)

func TestDuplicateDetectionGroupsMirrorsAndHashMatches(t *testing.T) {
	candidates := []extractor.Candidate{
		{
			RelativePath: ".claude/skills/mcp-builder/SKILL.md",
			Kind:         extractor.KindSkill,
			Key:          "mcp-builder",
			ContentHash:  "same-hash",
			PathSignals:  []string{"claude_root"},
		},
		{
			RelativePath: ".agents/skills/mcp-builder/SKILL.md",
			Kind:         extractor.KindSkill,
			Key:          "mcp-builder",
			ContentHash:  "same-hash",
			PathSignals:  []string{"agents_root"},
		},
		{
			RelativePath: "docs/adr/adr-001.md",
			Kind:         extractor.KindArchitectureDoc,
			Key:          "adr-001",
			ContentHash:  "other-hash",
			PathSignals:  []string{"docs_root"},
		},
	}

	grouped := extractor.DetectDuplicates(candidates)

	first := findCandidate(t, grouped, ".claude/skills/mcp-builder/SKILL.md")
	second := findCandidate(t, grouped, ".agents/skills/mcp-builder/SKILL.md")
	third := findCandidate(t, grouped, "docs/adr/adr-001.md")

	if first.DuplicateGroup == "" || second.DuplicateGroup == "" {
		t.Fatalf("duplicate group ids = %q %q, want shared non-empty id", first.DuplicateGroup, second.DuplicateGroup)
	}
	if first.DuplicateGroup != second.DuplicateGroup {
		t.Fatalf("duplicate groups = %q and %q, want equal", first.DuplicateGroup, second.DuplicateGroup)
	}
	if !first.IsPrimary && second.IsPrimary {
		t.Fatalf("expected deterministic primary selection in grouped duplicates: %+v %+v", first, second)
	}
	if third.DuplicateGroup != "" {
		t.Fatalf("non-duplicate candidate group = %q, want empty", third.DuplicateGroup)
	}
}

func findCandidate(t *testing.T, candidates []extractor.Candidate, relativePath string) extractor.Candidate {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.RelativePath == relativePath {
			return candidate
		}
	}
	t.Fatalf("missing candidate %s in %+v", relativePath, candidates)
	return extractor.Candidate{}
}
