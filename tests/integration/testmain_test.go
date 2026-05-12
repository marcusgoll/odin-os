package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	envDir, err := os.MkdirTemp("", "odin-integration-env-*")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "create integration env dir: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv("ODIN_ENV_FILE", filepath.Join(envDir, "missing-odin-os.env")); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "set ODIN_ENV_FILE: %v\n", err)
		_ = os.RemoveAll(envDir)
		os.Exit(1)
	}
	if os.Getenv("ODIN_SQLITE_FAST_TEST_PRAGMAS") == "" {
		if err := os.Setenv("ODIN_SQLITE_FAST_TEST_PRAGMAS", "1"); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "set ODIN_SQLITE_FAST_TEST_PRAGMAS: %v\n", err)
			_ = os.RemoveAll(envDir)
			os.Exit(1)
		}
	}

	code := m.Run()
	_ = os.RemoveAll(envDir)
	os.Exit(code)
}
