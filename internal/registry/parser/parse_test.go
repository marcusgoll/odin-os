package parser_test

import (
	"testing"

	"odin-os/internal/registry"
	"odin-os/internal/registry/parser"
)

func TestParseSourceExtractsFrontmatterAndSections(t *testing.T) {
	source := registry.SourceFile{
		Path:         "/tmp/skills/triage.md",
		RelativePath: "skills/triage.md",
		ExpectedKind: registry.KindSkill,
	}

	content := []byte(`---
kind: skill
key: triage-skill
title: Triage Skill
summary: Helps sort incoming work.
version: 1.0.0
enabled: true
strictness: rigid
applies_to:
  - intake
scopes:
  - project
permissions:
  - repo.read
handler_type: command
handler_ref: scripts/skills/triage.sh
timeout_seconds: 15
input_schema:
  type: object
  properties:
    request:
      type: string
output_schema:
  type: object
  properties:
    summary:
      type: string
---

# Triage Skill

## Purpose
Sort work.

## When to Use
When intake is noisy.

## Inputs
Work items.

## Procedure
Read and categorize.

## Outputs
Prioritized list.

## Constraints
Stay deterministic.

## Success Criteria
The queue is sorted.
`)

	document, diagnostics := parser.ParseSource(source, content)
	if len(diagnostics) != 0 {
		t.Fatalf("ParseSource() diagnostics = %v, want none", diagnostics)
	}

	if document.Frontmatter.Kind != registry.KindSkill {
		t.Fatalf("document kind = %q, want %q", document.Frontmatter.Kind, registry.KindSkill)
	}

	if document.Frontmatter.Key != "triage-skill" {
		t.Fatalf("document key = %q, want %q", document.Frontmatter.Key, "triage-skill")
	}

	if document.Frontmatter.Version != "1.0.0" {
		t.Fatalf("document version = %q, want %q", document.Frontmatter.Version, "1.0.0")
	}

	if document.Frontmatter.Enabled == nil || !*document.Frontmatter.Enabled {
		t.Fatalf("document enabled = %#v, want true", document.Frontmatter.Enabled)
	}

	if document.Frontmatter.HandlerType != "command" {
		t.Fatalf("document handler type = %q, want %q", document.Frontmatter.HandlerType, "command")
	}

	if document.Frontmatter.HandlerRef != "scripts/skills/triage.sh" {
		t.Fatalf("document handler ref = %q, want %q", document.Frontmatter.HandlerRef, "scripts/skills/triage.sh")
	}

	if document.Frontmatter.TimeoutSeconds != 15 {
		t.Fatalf("document timeout = %d, want 15", document.Frontmatter.TimeoutSeconds)
	}

	if got := document.Frontmatter.InputSchema["type"]; got != "object" {
		t.Fatalf("input schema type = %#v, want object", got)
	}

	if got := document.Sections[registry.SectionPurpose]; got != "Sort work." {
		t.Fatalf("Purpose section = %q, want %q", got, "Sort work.")
	}
}

func TestParseSourceRejectsMissingFrontmatter(t *testing.T) {
	source := registry.SourceFile{
		Path:         "/tmp/skills/bad.md",
		RelativePath: "skills/bad.md",
		ExpectedKind: registry.KindSkill,
	}

	document, diagnostics := parser.ParseSource(source, []byte("# Missing Frontmatter"))
	if document.Frontmatter.Key != "" {
		t.Fatalf("document key = %q, want empty", document.Frontmatter.Key)
	}

	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics len = %d, want 1", len(diagnostics))
	}

	if diagnostics[0].Code != "missing_frontmatter" {
		t.Fatalf("diagnostic code = %q, want %q", diagnostics[0].Code, "missing_frontmatter")
	}
}
