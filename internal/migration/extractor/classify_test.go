package extractor_test

import (
	"testing"

	"odin-os/internal/migration/extractor"
)

func TestClassifyAssignsConservativeMigrationActions(t *testing.T) {
	candidates := []extractor.Candidate{
		{RelativePath: ".claude/skills/odin-sentry/SKILL.md", Kind: extractor.KindSkill, Key: "odin-sentry", PathSignals: []string{"claude_root"}},
		{RelativePath: ".claude/skills/agents-skills-backup/odin-sentry/SKILL.md", Kind: extractor.KindSkill, Key: "odin-sentry", PathSignals: []string{"backup_path"}},
		{RelativePath: "docs/adr/2026-03-20-odin-engine-v2.md", Kind: extractor.KindArchitectureDoc, Key: "odin-engine-v2", PathSignals: []string{"docs_root"}},
		{RelativePath: "docs/process/CODEX_SESSION_PROTOCOL.md", Kind: extractor.KindOperationalPlaybook, Key: "codex-session-protocol", PathSignals: []string{"docs_root"}},
		{RelativePath: ".cache/gomod/tmp.txt", Kind: extractor.KindUnknown, Key: "tmp", PathSignals: []string{"cache_path"}},
	}

	classified := extractor.Classify(candidates)

	assertClassification(t, classified, ".claude/skills/odin-sentry/SKILL.md", extractor.ClassificationRewrite)
	assertClassification(t, classified, ".claude/skills/agents-skills-backup/odin-sentry/SKILL.md", extractor.ClassificationArchive)
	assertClassification(t, classified, "docs/adr/2026-03-20-odin-engine-v2.md", extractor.ClassificationReferenceOnly)
	assertClassification(t, classified, "docs/process/CODEX_SESSION_PROTOCOL.md", extractor.ClassificationRewrite)
	assertClassification(t, classified, ".cache/gomod/tmp.txt", extractor.ClassificationDelete)
}

func assertClassification(t *testing.T, candidates []extractor.Candidate, relativePath string, want extractor.Classification) {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.RelativePath == relativePath {
			if candidate.Classification != want {
				t.Fatalf("candidate %s classification = %q, want %q", relativePath, candidate.Classification, want)
			}
			if candidate.Rationale == "" {
				t.Fatalf("candidate %s rationale = empty, want non-empty", relativePath)
			}
			return
		}
	}
	t.Fatalf("missing candidate %s in %+v", relativePath, candidates)
}
