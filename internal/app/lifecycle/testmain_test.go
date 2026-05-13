package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	driverDir, err := os.MkdirTemp("", "odin-lifecycle-driver-*")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "create lifecycle driver dir: %v\n", err)
		os.Exit(1)
	}

	driverPath := filepath.Join(driverDir, "codex-driver.sh")
	if err := os.WriteFile(driverPath, []byte(`#!/usr/bin/env bash
payload="$(cat)"
PAYLOAD="$payload" python3 - <<'PY'
import json
import os

request = json.loads(os.environ["PAYLOAD"])
action = request.get("action")
if action == "health":
    print(json.dumps({"status":"healthy","details":"lifecycle test driver healthy"}))
else:
    print(json.dumps({"status":"completed","output":"driver test ok","handle":{"external_id":"fixture-driver"}}))
PY
`), 0o755); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "write lifecycle driver: %v\n", err)
		_ = os.RemoveAll(driverDir)
		os.Exit(1)
	}
	if err := os.Chmod(driverPath, 0o755); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "chmod lifecycle driver: %v\n", err)
		_ = os.RemoveAll(driverDir)
		os.Exit(1)
	}
	if err := os.Setenv("ODIN_CODEX_DRIVER", driverPath); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "set ODIN_CODEX_DRIVER: %v\n", err)
		_ = os.RemoveAll(driverDir)
		os.Exit(1)
	}
	if err := os.Setenv("ODIN_ENV_FILE", filepath.Join(driverDir, "missing-odin-os.env")); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "set ODIN_ENV_FILE: %v\n", err)
		_ = os.RemoveAll(driverDir)
		os.Exit(1)
	}
	if os.Getenv("ODIN_SQLITE_FAST_TEST_PRAGMAS") == "" {
		if err := os.Setenv("ODIN_SQLITE_FAST_TEST_PRAGMAS", "1"); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "set ODIN_SQLITE_FAST_TEST_PRAGMAS: %v\n", err)
			_ = os.RemoveAll(driverDir)
			os.Exit(1)
		}
	}

	code := m.Run()
	_ = os.RemoveAll(driverDir)
	os.Exit(code)
}
