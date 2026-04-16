package integration_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type runResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

func TestFreshRuntimeWithoutCodexDriverIsNotReady(t *testing.T) {
	root := t.TempDir()
	result := runOdin(t, root, "healthcheck")
	if result.ExitCode == 0 {
		t.Fatalf("healthcheck exit = %d, want non-zero without driver", result.ExitCode)
	}
}

func TestFreshRuntimeWithCodexDriverCanAnswerAndRun(t *testing.T) {
	root := t.TempDir()
	driver := writeFixtureCodexDriver(t)

	ask := runInteractiveOdin(t, root, map[string]string{
		"ODIN_CODEX_DRIVER": driver,
	}, "what can you do?\n")
	if strings.Contains(ask.Stdout, "codex_headless completed") {
		t.Fatalf("ask output = %q, want real answer instead of stub marker", ask.Stdout)
	}

	act := runInteractiveOdin(t, root, map[string]string{
		"ODIN_CODEX_DRIVER": driver,
	}, "/project odin-core\n/mode act\nprepare a release note\n")
	if !strings.Contains(act.Stdout, "run ") || strings.Contains(act.Stdout, "codex_headless completed") {
		t.Fatalf("act output = %q, want real run output", act.Stdout)
	}
}

func runOdin(t *testing.T, root string, args ...string) runResult {
	t.Helper()

	return runOdinWithEnv(t, root, nil, "", args...)
}

func runInteractiveOdin(t *testing.T, root string, extraEnv map[string]string, stdin string) runResult {
	t.Helper()

	return runOdinWithEnv(t, root, extraEnv, stdin)
}

func runOdinWithEnv(t *testing.T, root string, extraEnv map[string]string, stdin string, args ...string) runResult {
	t.Helper()

	repoRoot := projectRoot(t)
	binaryPath := buildOdinBinary(t, repoRoot)

	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = repoRoot

	env := append([]string{}, os.Environ()...)
	env = append(env, "ODIN_ROOT="+root)
	for key, value := range extraEnv {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	return runResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}
}

func writeFixtureCodexDriver(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "codex-driver.sh")
	script := `#!/usr/bin/env bash
set -euo pipefail
cat >/dev/null
printf '%s\n' "fixture codex driver"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(codex driver) error = %v", err)
	}
	return path
}
