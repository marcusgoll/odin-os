package loader_test

import (
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/registry"
	"odin-os/internal/registry/loader"
)

func TestScanDirInfersKinds(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills", "triage.md"), sampleSkillMarkdown("triage-skill"))
	writeFile(t, filepath.Join(root, "commands", "status.md"), sampleCommandMarkdown("status-command"))

	files, err := loader.ScanDir(root)
	if err != nil {
		t.Fatalf("ScanDir() error = %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("ScanDir() files = %d, want 2", len(files))
	}

	if files[0].ExpectedKind != registry.KindCommand {
		t.Fatalf("files[0].ExpectedKind = %q, want %q", files[0].ExpectedKind, registry.KindCommand)
	}

	if files[1].ExpectedKind != registry.KindSkill {
		t.Fatalf("files[1].ExpectedKind = %q, want %q", files[1].ExpectedKind, registry.KindSkill)
	}
}

func TestLoadDirCompilesValidFilesAndReportsInvalidOnes(t *testing.T) {
	root := t.TempDir()
	registryRoot := filepath.Join(root, "registry")
	writeFile(t, filepath.Join(registryRoot, "skills", "triage.md"), sampleSkillMarkdown("triage-skill"))
	writeFile(t, filepath.Join(registryRoot, "skills", "broken.md"), brokenSkillMarkdown("broken-skill"))
	writeExecutable(t, filepath.Join(root, "scripts", "skills", "triage-skill.sh"), sampleSkillHandlerScript())
	writeExecutable(t, filepath.Join(root, "scripts", "skills", "broken-skill.sh"), sampleSkillHandlerScript())

	snapshot, err := loader.LoadDir(registryRoot)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}

	if len(snapshot.Items) != 1 {
		t.Fatalf("snapshot.Items = %d, want 1", len(snapshot.Items))
	}

	if snapshot.Items[0].Key != "triage-skill" {
		t.Fatalf("snapshot.Items[0].Key = %q, want %q", snapshot.Items[0].Key, "triage-skill")
	}

	if len(snapshot.Diagnostics) == 0 {
		t.Fatal("snapshot.Diagnostics = 0, want at least 1")
	}
}

func TestLoadDirLoadsRepositoryExamples(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "..", "registry"))

	snapshot, err := loader.LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}

	if len(snapshot.Diagnostics) != 0 {
		t.Fatalf("snapshot.Diagnostics = %v, want none", snapshot.Diagnostics)
	}

	if len(snapshot.Items) < 6 {
		t.Fatalf("snapshot.Items = %d, want at least 6 representative examples", len(snapshot.Items))
	}

	byKey := make(map[string]registry.Item, len(snapshot.Items))
	for _, item := range snapshot.Items {
		byKey[item.Key] = item
	}

	for key, wantKind := range map[string]registry.Kind{
		"portal-delivery-agent":        registry.KindAgent,
		"triage-agent":                 registry.KindAgent,
		"status-command":               registry.KindCommand,
		"triage-skill":                 registry.KindSkill,
		"karpathy-guidelines":          registry.KindSkill,
		"project-intake":               registry.KindWorkflow,
		"pixel-perfect-ui-ux-designer": registry.KindSkill,
	} {
		item, ok := byKey[key]
		if !ok {
			t.Fatalf("snapshot missing %q", key)
		}
		if item.Kind != wantKind {
			t.Fatalf("snapshot item %q kind = %q, want %q", key, item.Kind, wantKind)
		}
	}
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}

	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func writeExecutable(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}

	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func sampleSkillMarkdown(key string) string {
	return `---
kind: skill
key: ` + key + `
title: Triage Skill
summary: Helps sort incoming work.
status: active
version: "1.0.0"
enabled: true
strictness: rigid
applies_to:
  - intake
scopes:
  - global
permissions:
  - repo.read
handler_type: command
handler_ref: scripts/skills/` + key + `.sh
timeout_seconds: 15
input_schema:
  type: object
  properties:
    request:
      type: string
output_schema:
  type: object
  properties:
    classification:
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
`
}

func sampleCommandMarkdown(key string) string {
	return `---
kind: command
key: ` + key + `
title: Status Command
summary: Shows current status.
command: status
scopes:
  - global
aliases:
  - stat
---

# Status Command

## Purpose
Show status.

## When to Use
When an operator needs context.

## Inputs
Current scope.

## Procedure
Collect and display status.

## Outputs
Rendered status.

## Constraints
Avoid mutation.

## Success Criteria
The operator understands current state.
`
}

func brokenSkillMarkdown(key string) string {
	return `---
kind: skill
key: ` + key + `
title: Broken Skill
summary: Missing the Procedure section.
status: active
version: "1.0.0"
enabled: true
strictness: rigid
applies_to:
  - intake
scopes:
  - global
permissions:
  - repo.read
handler_type: command
handler_ref: scripts/skills/` + key + `.sh
timeout_seconds: 15
input_schema:
  type: object
  properties:
    request:
      type: string
output_schema:
  type: object
  properties:
    classification:
      type: string
---

# Broken Skill

## Purpose
Sort work.

## When to Use
When intake is noisy.

## Inputs
Work items.

## Outputs
Prioritized list.

## Constraints
Stay deterministic.

## Success Criteria
The queue is sorted.
`
}

func sampleSkillHandlerScript() string {
	return "#!/usr/bin/env bash\nset -euo pipefail\ncat >/dev/null\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\"complete\",\"output\":{\"classification\":\"triage\"}}'\n"
}
