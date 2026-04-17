package extractor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/migration/extractor"
	"odin-os/internal/registry/loader"
)

func TestDraftGenerationEmitsDraftRegistryFilesThatPassValidation(t *testing.T) {
	repoRoot := t.TempDir()
	outputRoot := filepath.Join(repoRoot, "registry")
	candidates := []extractor.Candidate{
		{
			SourcePath:     "/legacy/.claude/skills/demo-skill/SKILL.md",
			RelativePath:   ".claude/skills/demo-skill/SKILL.md",
			Kind:           extractor.KindSkill,
			Key:            "demo-skill",
			Title:          "Demo Skill",
			Classification: extractor.ClassificationRewrite,
			IsPrimary:      true,
		},
		{
			SourcePath:     "/legacy/docs/adr/engine.md",
			RelativePath:   "docs/adr/engine.md",
			Kind:           extractor.KindArchitectureDoc,
			Key:            "engine",
			Title:          "Engine",
			Classification: extractor.ClassificationReferenceOnly,
			IsPrimary:      true,
		},
	}

	paths, err := extractor.EmitDrafts(candidates, outputRoot, 0)
	if err != nil {
		t.Fatalf("EmitDrafts() error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("EmitDrafts() len = %d, want 1 supported draft", len(paths))
	}

	draftBytes, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", paths[0], err)
	}
	draftText := string(draftBytes)
	for _, want := range []string{
		"kind: skill",
		"status: draft",
		"## Purpose",
		"## When to Use",
		"## Inputs",
		"## Procedure",
		"## Outputs",
		"## Constraints",
		"## Success Criteria",
		"Legacy source: `.claude/skills/demo-skill/SKILL.md`",
	} {
		if !strings.Contains(draftText, want) {
			t.Fatalf("draft = %q, want substring %q", draftText, want)
		}
	}

	handlerPath := filepath.Join(repoRoot, "scripts", "skills", "demo-skill.sh")
	if err := os.MkdirAll(filepath.Dir(handlerPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(handlerPath), err)
	}
	if err := os.WriteFile(handlerPath, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", handlerPath, err)
	}

	snapshot, err := loader.LoadDir(outputRoot)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(snapshot.Diagnostics) != 0 {
		t.Fatalf("LoadDir() diagnostics = %+v, want none", snapshot.Diagnostics)
	}
	if _, err := os.Stat(filepath.Join(outputRoot, "architecture_docs", "engine.md")); !os.IsNotExist(err) {
		t.Fatalf("unexpected draft emitted for architecture doc")
	}
}
