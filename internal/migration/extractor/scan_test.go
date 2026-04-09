package extractor_test

import (
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/migration/extractor"
)

func TestScanDetectsLegacyCandidateKindsAndIgnoresJunk(t *testing.T) {
	root := t.TempDir()

	mustWriteFile(t, filepath.Join(root, ".claude/skills/demo-skill/SKILL.md"), "# Demo Skill\n\nA useful skill.\n")
	mustWriteFile(t, filepath.Join(root, ".agents/skills/demo-skill/SKILL.md"), "# Demo Skill Mirror\n\nA mirrored skill.\n")
	mustWriteFile(t, filepath.Join(root, "docs/adr/2026-03-20-odin-engine-v2.md"), "# Odin Engine V2\n\nArchitecture.\n")
	mustWriteFile(t, filepath.Join(root, "docs/process/CODEX_SESSION_PROTOCOL.md"), "# Codex Session Protocol\n\nOperational process.\n")
	mustWriteFile(t, filepath.Join(root, "specs/ultrathink/odin-self-learning-self-healing.yaml"), "name: self-heal\n")
	mustWriteFile(t, filepath.Join(root, "prompts/system/router.md"), "# Router Prompt\n\nUse this prompt.\n")

	mustWriteFile(t, filepath.Join(root, ".git/config"), "[core]\n")
	mustWriteFile(t, filepath.Join(root, ".cache/tmp.txt"), "ignore me")
	mustWriteFile(t, filepath.Join(root, ".worktrees/wt-1/README.md"), "# backup copy")

	candidates, err := extractor.Scan(root)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if len(candidates) != 6 {
		t.Fatalf("Scan() len = %d, want 6", len(candidates))
	}

	assertCandidateKind(t, candidates, ".claude/skills/demo-skill/SKILL.md", extractor.KindSkill)
	assertCandidateKind(t, candidates, ".agents/skills/demo-skill/SKILL.md", extractor.KindSkill)
	assertCandidateKind(t, candidates, "docs/adr/2026-03-20-odin-engine-v2.md", extractor.KindArchitectureDoc)
	assertCandidateKind(t, candidates, "docs/process/CODEX_SESSION_PROTOCOL.md", extractor.KindOperationalPlaybook)
	assertCandidateKind(t, candidates, "specs/ultrathink/odin-self-learning-self-healing.yaml", extractor.KindWorkflow)
	assertCandidateKind(t, candidates, "prompts/system/router.md", extractor.KindPrompt)
}

func TestScanExtractsStableMetadata(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".claude/skills/brand-ad-generator/SKILL.md")
	mustWriteFile(t, path, "---\nname: brand-ad-generator\ndescription: Create ads\n---\n\n# Brand Ad Generator\n\nBody.\n")

	candidates, err := extractor.Scan(root)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("Scan() len = %d, want 1", len(candidates))
	}

	candidate := candidates[0]
	if candidate.Key != "brand-ad-generator" {
		t.Fatalf("Key = %q, want brand-ad-generator", candidate.Key)
	}
	if candidate.Title != "Brand Ad Generator" {
		t.Fatalf("Title = %q, want Brand Ad Generator", candidate.Title)
	}
	if candidate.ContentHash == "" {
		t.Fatalf("ContentHash = empty, want sha256 hash")
	}
	if len(candidate.PathSignals) == 0 {
		t.Fatalf("PathSignals = empty, want at least one signal")
	}
}

func assertCandidateKind(t *testing.T, candidates []extractor.Candidate, relativePath string, kind extractor.Kind) {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.RelativePath == relativePath {
			if candidate.Kind != kind {
				t.Fatalf("candidate %s kind = %q, want %q", relativePath, candidate.Kind, kind)
			}
			return
		}
	}
	t.Fatalf("missing candidate for %s in %+v", relativePath, candidates)
}

func mustWriteFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}
