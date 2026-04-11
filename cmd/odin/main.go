package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"odin-os/internal/app/lifecycle"
)

var runLifecycle = lifecycle.Run

func main() {
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(run(ctx, root, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, root string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if err := runLifecycle(ctx, root, args, stdin, stdout); err != nil {
		if errors.Is(err, context.Canceled) {
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
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
