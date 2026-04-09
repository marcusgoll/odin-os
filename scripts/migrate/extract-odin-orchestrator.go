package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"

	"odin-os/internal/migration/extractor"
)

func main() {
	sourceRoot := flag.String("source", "/home/orchestrator/odin-orchestrator", "legacy source repository root")
	repoRoot := flag.String("repo-root", ".", "current Odin OS repository root")
	emitDrafts := flag.Bool("emit-drafts", false, "emit review-only draft registry files")
	maxDrafts := flag.Int("max-drafts", 0, "maximum number of draft registry files to emit; 0 means unlimited")
	flag.Parse()

	service := extractor.Service{}
	result, err := service.Run(extractor.Options{
		SourceRoot: *sourceRoot,
		DocsRoot:   filepath.Join(*repoRoot, "docs"),
		StateRoot:  filepath.Join(*repoRoot, "state"),
		EmitDrafts: *emitDrafts,
		MaxDrafts:  *maxDrafts,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("inventory: %s\n", result.InventoryPath)
	fmt.Printf("inventory_report: %s\n", result.InventoryReportPath)
	fmt.Printf("duplicate_report: %s\n", result.DuplicateReportPath)
	fmt.Printf("drafts: %d\n", len(result.DraftPaths))
}
