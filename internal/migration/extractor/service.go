package extractor

import (
	"fmt"
	"path/filepath"
	"time"
)

type Options struct {
	SourceRoot string
	DocsRoot   string
	StateRoot  string
	EmitDrafts bool
	MaxDrafts  int
	Now        func() time.Time
}

type Result struct {
	InventoryPath       string
	InventoryReportPath string
	DuplicateReportPath string
	DraftPaths          []string
	Inventory           Inventory
}

type Service struct{}

func (Service) Run(options Options) (Result, error) {
	if options.SourceRoot == "" {
		return Result{}, fmt.Errorf("source root is required")
	}

	now := time.Now().UTC()
	if options.Now != nil {
		now = options.Now().UTC()
	}

	candidates, err := Scan(options.SourceRoot)
	if err != nil {
		return Result{}, err
	}
	candidates = DetectDuplicates(candidates)
	candidates = Classify(candidates)

	inventory := Inventory{
		SourceRoot:  options.SourceRoot,
		Candidates:  candidates,
		GeneratedAt: now.Format(time.RFC3339Nano),
	}

	inventoryPath := filepath.Join(options.StateRoot, "migration", "inventory.json")
	if err := writeInventoryJSON(inventoryPath, inventory); err != nil {
		return Result{}, err
	}

	inventoryReportPath := filepath.Join(options.DocsRoot, "migration", "legacy-inventory.md")
	if err := writeMarkdownReport(inventoryReportPath, renderInventoryReport(inventory)); err != nil {
		return Result{}, err
	}

	duplicateReportPath := filepath.Join(options.DocsRoot, "migration", "duplicate-report.md")
	if err := writeMarkdownReport(duplicateReportPath, renderDuplicateReport(inventory)); err != nil {
		return Result{}, err
	}

	var draftPaths []string
	if options.EmitDrafts {
		draftPaths, err = EmitDrafts(candidates, filepath.Join(options.StateRoot, "migration", "drafts"), options.MaxDrafts)
		if err != nil {
			return Result{}, err
		}
	}

	return Result{
		InventoryPath:       inventoryPath,
		InventoryReportPath: inventoryReportPath,
		DuplicateReportPath: duplicateReportPath,
		DraftPaths:          draftPaths,
		Inventory:           inventory,
	}, nil
}
