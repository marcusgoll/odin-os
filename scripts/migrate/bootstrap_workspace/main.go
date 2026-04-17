package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"odin-os/internal/app/bootstrap"
	"odin-os/internal/store/sqlite"
)

func main() {
	runtimeRoot := flag.String("runtime-root", defaultRuntimeRoot(), "Odin runtime root containing data/odin.db")
	flag.Parse()

	dbPath := filepath.Join(*runtimeRoot, "data", "odin.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatal(err)
	}

	store, err := sqlite.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	if err := store.Migrate(context.Background()); err != nil {
		log.Fatal(err)
	}

	report, err := bootstrap.BootstrapWorkspaceRuntimeState(context.Background(), store)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("workspace_id: %d\n", report.WorkspaceID)
	fmt.Printf("default_companion_id: %d\n", report.DefaultCompanionID)
	fmt.Printf("projects_reconciled: %d\n", report.ProjectsReconciled)
	fmt.Printf("tasks_bound_to_workspace: %d\n", report.TasksBoundToWorkspace)
	fmt.Printf("tasks_linked_to_initiative: %d\n", report.TasksLinkedToInitiative)
	fmt.Printf("tasks_bound_to_companion: %d\n", report.TasksBoundToCompanion)
	fmt.Printf("tasks_backfilled_work_kind: %d\n", report.TasksBackfilledWorkKind)
}

func defaultRuntimeRoot() string {
	if root := os.Getenv("ODIN_ROOT"); root != "" {
		return root
	}
	return "."
}
