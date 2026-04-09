# Local Odin Command Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `odin` a repeatable local command by installing a symlink into `~/.local/bin`.

**Architecture:** Keep the change narrow. Use small shell scripts for install and uninstall, wire them into the existing `Makefile`, and verify behavior with one integration test that runs against a temporary home directory and fake source binary. Avoid adding new runtime abstractions.

**Tech Stack:** Make, Bash, Go integration tests

---

### Task 1: Add the failing integration test

**Files:**
- Create: `tests/integration/local_install_test.go`

**Step 1: Write the failing test**

Write one test that:
- creates a temporary home directory
- creates a temporary fake source binary
- runs `scripts/dev/install-local.sh`
- verifies `$HOME/.local/bin/odin` is a symlink to the fake source
- runs `scripts/dev/uninstall-local.sh`
- verifies the link no longer exists

**Step 2: Run test to verify it fails**

Run: `go test ./tests/integration -run TestLocalInstallScripts -count=1`
Expected: FAIL because the install scripts do not exist yet.

### Task 2: Implement the install scripts

**Files:**
- Create: `scripts/dev/install-local.sh`
- Create: `scripts/dev/uninstall-local.sh`

**Step 1: Write minimal implementation**

Implement:
- repo root discovery
- `ODIN_INSTALL_SOURCE` override
- `ODIN_INSTALL_BIN_DIR` override
- default install directory of `$HOME/.local/bin`
- symlink create and remove behavior

**Step 2: Run the focused test**

Run: `go test ./tests/integration -run TestLocalInstallScripts -count=1`
Expected: PASS

### Task 3: Wire the scripts into the Makefile

**Files:**
- Modify: `Makefile`

**Step 1: Add new targets**

Add:
- `install-local`: depends on `build`, then runs `scripts/dev/install-local.sh`
- `uninstall-local`: runs `scripts/dev/uninstall-local.sh`

**Step 2: Verify command surface**

Run: `make -n install-local`
Expected: shows `build` and the install script invocation

### Task 4: Update usage docs

**Files:**
- Modify: `README.md`

**Step 1: Add a short local usage section**

Document:
- `make build`
- `make install-local`
- `odin`

**Step 2: Verify docs reference the real targets**

Run: `rg -n "install-local|uninstall-local|odin"` README.md Makefile scripts/dev`
Expected: references appear in the expected files

### Task 5: Final verification

**Files:**
- No new files

**Step 1: Run focused verification**

Run:
- `go test ./tests/integration -run TestLocalInstallScripts -count=1`
- `make fmtcheck`

Expected: PASS

**Step 2: Run broader verification**

Run:
- `make test`
- `make build`

Expected: PASS
