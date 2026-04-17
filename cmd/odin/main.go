package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"odin-os/internal/app/lifecycle"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	root, err := resolveRepoRoot(cwd, executable)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	err = lifecycle.Run(ctx, root, os.Args[1:], os.Stdin, os.Stdout)
	if errors.Is(err, context.Canceled) && len(os.Args) > 1 && os.Args[1] == "serve" {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func resolveRepoRoot(cwd, executable string) (string, error) {
	candidates := []string{cwd}

	resolvedExecutable := executable
	if executable != "" {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			resolvedExecutable = resolved
		}
		dir := filepath.Dir(resolvedExecutable)
		for {
			candidates = append(candidates, dir)
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		candidate = filepath.Clean(candidate)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		if isRepoRoot(candidate) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("unable to locate odin repo root from cwd %s and executable %s", cwd, executable)
}

func isRepoRoot(root string) bool {
	info, err := os.Stat(filepath.Join(root, "config", "odin.yaml"))
	if err != nil || info.IsDir() {
		return false
	}
	return true
}
