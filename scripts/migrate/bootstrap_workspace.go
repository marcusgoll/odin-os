package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"odin-os/internal/app/bootstrap"
)

func main() {
	repoRoot := flag.String("repo-root", ".", "current Odin OS repository root")
	runtimeRoot := flag.String("runtime-root", "", "runtime root to bootstrap; defaults to ODIN_ROOT or repo root")
	flag.Parse()

	resolvedRuntimeRoot := *runtimeRoot
	if resolvedRuntimeRoot == "" {
		if envRuntimeRoot := os.Getenv("ODIN_ROOT"); envRuntimeRoot != "" {
			resolvedRuntimeRoot = envRuntimeRoot
		} else {
			resolvedRuntimeRoot = *repoRoot
		}
	}

	app, err := bootstrap.Load(context.Background(), *repoRoot, resolvedRuntimeRoot)
	if err != nil {
		log.Fatal(err)
	}
	defer app.Store.Close()

	workspace, err := app.Store.GetWorkspaceByKey(context.Background(), "marcus")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("repo_root: %s\n", *repoRoot)
	fmt.Printf("runtime_root: %s\n", resolvedRuntimeRoot)
	fmt.Printf("workspace_key: %s\n", workspace.Key)
}
