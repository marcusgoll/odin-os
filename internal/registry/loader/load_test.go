package loader_test

import (
	"os"
	"path/filepath"
	"strings"
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
	writeFile(t, filepath.Join(root, "skills", "triage.md"), sampleSkillMarkdown("triage-skill"))
	writeFile(t, filepath.Join(root, "skills", "broken.md"), brokenSkillMarkdown("broken-skill"))

	snapshot, err := loader.LoadDir(root)
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

	wantKeys := []string{
		"flica-annual-vacation",
		"flica-fcfs-bid",
		"flica-schedule",
		"flica-seniority-bid",
		"flica-tradeboard",
		"flica-tradeboard-split-post",
		"project-intake",
	}
	loadedKeys := make(map[string]bool, len(snapshot.Items))
	for _, item := range snapshot.Items {
		loadedKeys[item.Key] = true
	}
	for _, key := range wantKeys {
		if !loadedKeys[key] {
			t.Fatalf("snapshot.Items missing %q", key)
		}
	}
}

func TestLoadDirLoadsUniversalIntakeAgents(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "..", "registry"))

	snapshot, err := loader.LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}

	if len(snapshot.Diagnostics) != 0 {
		t.Fatalf("snapshot.Diagnostics = %v, want none", snapshot.Diagnostics)
	}

	wantAgents := []string{
		"universal-os-orchestrator",
		"capture-agent",
		"classifier-agent",
		"deduper-agent",
		"priority-agent",
		"router-agent",
		"spec-task-builder-agent",
		"review-agent",
		"chief-of-staff-agent",
	}
	for _, key := range wantAgents {
		item, ok := snapshot.ByKey[key]
		if !ok {
			t.Fatalf("snapshot.ByKey missing %q", key)
		}
		if item.Kind != registry.KindAgent {
			t.Fatalf("%s kind = %q, want %q", key, item.Kind, registry.KindAgent)
		}
		if !containsString(item.Tags, "universal-intake") {
			t.Fatalf("%s tags = %v, want universal-intake", key, item.Tags)
		}
	}

	orchestrator := snapshot.ByKey["universal-os-orchestrator"]
	orchestratorContract := strings.Join([]string{
		orchestrator.Sections[registry.SectionPurpose],
		orchestrator.Sections[registry.SectionWhenToUse],
		orchestrator.Sections[registry.SectionInputs],
		orchestrator.Sections[registry.SectionProcedure],
		orchestrator.Sections[registry.SectionOutputs],
		orchestrator.Sections[registry.SectionConstraints],
		orchestrator.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredContract := []string{
		"task",
		"project",
		"idea",
		"bug",
		"feature request",
		"personal admin",
		"calendar item",
		"research request",
		"writing request",
		"coding request",
		"learning goal",
		"health or wellbeing item",
		"finance/admin item",
		"household item",
		"waiting-for item",
		"archive/reference item",
		"unclear",
		"cleaned summary",
		"human approval is required",
		"specialist agent",
		"Never execute high-risk actions directly",
		"Never create implementation tasks from vague ideas",
		"create a clarification task instead of guessing",
	}
	for _, required := range requiredContract {
		if !strings.Contains(orchestratorContract, required) {
			t.Fatalf("universal orchestrator body missing %q", required)
		}
	}

	chiefOfStaff := snapshot.ByKey["chief-of-staff-agent"]
	chiefOfStaffContract := strings.Join([]string{
		chiefOfStaff.Sections[registry.SectionPurpose],
		chiefOfStaff.Sections[registry.SectionWhenToUse],
		chiefOfStaff.Sections[registry.SectionInputs],
		chiefOfStaff.Sections[registry.SectionProcedure],
		chiefOfStaff.Sections[registry.SectionOutputs],
		chiefOfStaff.Sections[registry.SectionConstraints],
		chiefOfStaff.Sections[registry.SectionSuccessCriteria],
	}, "\n")
	requiredBriefContract := []string{
		"active tasks",
		"projects",
		"calendar context",
		"waiting-for items",
		"recent inbox captures",
		"deadlines",
		"top 3 priorities",
		"urgent deadlines",
		"quick wins under 15 minutes",
		"blocked items",
		"waiting-for follow-ups",
		"decisions I need to make",
		"tasks that should be delegated to other agents",
		"tasks that should be deleted or deferred",
		"one recommended focus block",
		"one warning about overcommitment",
		"Do not inflate trivial tasks into strategic initiatives",
	}
	for _, required := range requiredBriefContract {
		if !strings.Contains(chiefOfStaffContract, required) {
			t.Fatalf("chief of staff agent body missing %q", required)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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

func sampleSkillMarkdown(key string) string {
	return `---
kind: skill
key: ` + key + `
title: Triage Skill
summary: Helps sort incoming work.
strictness: rigid
applies_to:
  - intake
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
strictness: rigid
applies_to:
  - intake
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
