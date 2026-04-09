package extractor_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/migration/extractor"
)

func TestServiceWritesInventoryReportsAndOptionalDrafts(t *testing.T) {
	sourceRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(sourceRoot, ".claude/skills/demo-skill/SKILL.md"), "# Demo Skill\n\nBody.\n")
	mustWriteFile(t, filepath.Join(sourceRoot, ".agents/skills/demo-skill/SKILL.md"), "# Demo Skill Mirror\n\nBody.\n")
	mustWriteFile(t, filepath.Join(sourceRoot, "docs/adr/engine.md"), "# Engine\n\nArchitecture.\n")
	mustWriteFile(t, filepath.Join(sourceRoot, "ops/github-runner/README.md"), "# Runner\n\nRunbook.\n")

	outputRoot := t.TempDir()
	service := extractor.Service{}

	result, err := service.Run(extractor.Options{
		SourceRoot: sourceRoot,
		DocsRoot:   filepath.Join(outputRoot, "docs"),
		StateRoot:  filepath.Join(outputRoot, "state"),
		EmitDrafts: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.InventoryPath == "" || result.InventoryReportPath == "" || result.DuplicateReportPath == "" {
		t.Fatalf("result paths = %+v, want inventory and report outputs", result)
	}

	inventoryBytes, err := os.ReadFile(result.InventoryPath)
	if err != nil {
		t.Fatalf("ReadFile(inventory) error = %v", err)
	}
	var inventory extractor.Inventory
	if err := json.Unmarshal(inventoryBytes, &inventory); err != nil {
		t.Fatalf("inventory json unmarshal error = %v", err)
	}
	if len(inventory.Candidates) != 4 {
		t.Fatalf("inventory candidates = %d, want 4", len(inventory.Candidates))
	}

	legacyReportBytes, err := os.ReadFile(result.InventoryReportPath)
	if err != nil {
		t.Fatalf("ReadFile(legacy report) error = %v", err)
	}
	if !strings.Contains(string(legacyReportBytes), "Legacy Migration Inventory") {
		t.Fatalf("legacy report = %q, want title", string(legacyReportBytes))
	}

	duplicateReportBytes, err := os.ReadFile(result.DuplicateReportPath)
	if err != nil {
		t.Fatalf("ReadFile(duplicate report) error = %v", err)
	}
	if !strings.Contains(string(duplicateReportBytes), "Likely Duplicates") {
		t.Fatalf("duplicate report = %q, want duplicates section", string(duplicateReportBytes))
	}

	if len(result.DraftPaths) == 0 {
		t.Fatalf("DraftPaths = 0, want opt-in drafts to be emitted")
	}
}

func TestServiceSkipsDraftOutputWhenDisabled(t *testing.T) {
	sourceRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(sourceRoot, ".claude/skills/demo-skill/SKILL.md"), "# Demo Skill\n\nBody.\n")

	outputRoot := t.TempDir()
	service := extractor.Service{}

	result, err := service.Run(extractor.Options{
		SourceRoot: sourceRoot,
		DocsRoot:   filepath.Join(outputRoot, "docs"),
		StateRoot:  filepath.Join(outputRoot, "state"),
		EmitDrafts: false,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.DraftPaths) != 0 {
		t.Fatalf("DraftPaths = %+v, want none when drafts disabled", result.DraftPaths)
	}
	if _, err := os.Stat(filepath.Join(outputRoot, "state", "migration", "drafts")); !os.IsNotExist(err) {
		t.Fatalf("draft directory exists when drafts are disabled")
	}
}
