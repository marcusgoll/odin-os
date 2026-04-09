# Phase 15 Homelab Deployment And Cutover Readiness Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Harden Odin OS for 24/7 homelab operation with native deployment artifacts, bounded restart recovery, machine-readable health endpoints, verified backup and restore tooling, and an operator cutover readiness checklist.

**Architecture:** Extend the existing CLI-first binary with a small command dispatcher and a headless `serve` mode, backed by a typed runtime config loader. Reuse existing health, metrics, store, and checkpoint services for startup recovery and operational endpoints instead of introducing a new control plane. Keep backups simple with a Go-backed backup service plus shell wrappers, and document the operational flow with systemd artifacts and cutover guidance.

**Tech Stack:** Go, SQLite, embedded SQL migrations, standard library HTTP server, shell scripts, systemd unit files, Go unit tests

---

### Task 1: Add the Phase 15 operational docs and deployment artifacts

**Files:**
- Create: `docs/contracts/homelab-operations.md`
- Create: `docs/operations/cutover-readiness.md`
- Create: `deploy/systemd/odin.service`
- Create: `deploy/systemd/odin.env.example`
- Modify: `README.md`

**Step 1: Write the contract doc**

Document:
- `odin serve`
- `odin healthcheck`
- operational endpoints
- restart recovery expectations
- backup and restore scope

**Step 2: Write the cutover readiness checklist**

Include:
- install and restart drill
- doctor and readiness validation
- backup and restore drill
- transition-state review for managed projects

**Step 3: Add systemd artifacts**

Create:
- a native `odin.service`
- an example env file with `ODIN_ROOT`, bind address, and health timing knobs

**Step 4: Update repo status text**

Record that Phase 15 adds homelab deployment, restart recovery, health endpoints, and verified backups.

**Step 5: Verify docs and service files**

Run: `sed -n '1,220p' docs/contracts/homelab-operations.md && sed -n '1,240p' docs/operations/cutover-readiness.md && sed -n '1,200p' deploy/systemd/odin.service && sed -n '1,160p' deploy/systemd/odin.env.example && sed -n '1,120p' README.md`

Expected: all files describe the same Phase 15 operational model.

### Task 2: Add failing tests for command dispatch and runtime configuration

**Files:**
- Create: `internal/app/config/config_test.go`
- Create: `internal/app/lifecycle/serve_test.go`
- Modify: `internal/app/lifecycle/run_test.go`

**Step 1: Write failing config tests**

Cover:
- loading default runtime config from `config/odin.yaml`
- overriding the runtime root with `ODIN_ROOT`
- parsing serve bind address and health endpoint settings

**Step 2: Write failing lifecycle tests**

Cover:
- `odin` with no args still starts the interactive shell
- `odin serve` selects service mode
- `odin healthcheck` returns success for a healthy runtime
- `odin doctor --json` emits machine-readable JSON instead of starting the REPL

**Step 3: Run the focused lifecycle and config tests**

Run: `go test ./internal/app/config ./internal/app/lifecycle -run 'Test(Load|Run|Serve|Healthcheck|Doctor)'`

Expected: FAIL because the command dispatcher, config loader, and service mode do not exist yet.

### Task 3: Implement runtime config loading and top-level command dispatch

**Files:**
- Create: `internal/app/config/config.go`
- Modify: `config/odin.yaml`
- Modify: `cmd/odin/main.go`
- Modify: `internal/app/bootstrap/bootstrap.go`
- Modify: `internal/app/lifecycle/run.go`

**Step 1: Add a typed Phase 15 runtime config**

Support:
- runtime root
- service bind address
- health endpoint enablement
- optional startup recovery toggle

**Step 2: Teach bootstrap to honor the runtime root**

Load state under the configured root while keeping the repo-root default for local development.

**Step 3: Add command dispatch**

Support:
- interactive shell by default
- `serve`
- `healthcheck`
- `doctor --json`

**Step 4: Run the focused lifecycle and config tests**

Run: `go test ./internal/app/config ./internal/app/lifecycle -run 'Test(Load|Run|Serve|Healthcheck|Doctor)'`

Expected: PASS

### Task 4: Add failing tests for restart recovery and resumable wake packets

**Files:**
- Create: `internal/runtime/recovery/startup_test.go`
- Modify: `internal/store/sqlite/store_test.go`

**Step 1: Write failing restart recovery tests**

Cover:
- a `running` run becomes `interrupted`
- the associated task is moved back to a resumable queued state
- a restart-triggered wake packet is created
- recovery and event records are appended

**Step 2: Run the focused restart recovery tests**

Run: `go test ./internal/runtime/recovery ./internal/store/sqlite -run 'TestStartupRecovery'`

Expected: FAIL because startup recovery behavior and any needed store helpers do not exist yet.

### Task 5: Implement startup recovery

**Files:**
- Create: `internal/runtime/recovery/startup.go`
- Modify: `internal/runtime/checkpoints/service.go`
- Modify: `internal/runtime/events/events.go`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`

**Step 1: Add minimal store helpers**

Implement only what the failing tests need:
- list active runs at startup
- update run status to `interrupted`
- move tasks back to `queued` when recovery says they are resumable

**Step 2: Add a bounded startup recovery service**

For each interrupted runtime:
- mark the prior run interrupted
- requeue the task
- emit restart wake packets using the existing checkpoint service
- record recovery actions in incidents, recoveries, and events

**Step 3: Run the focused restart recovery tests**

Run: `go test ./internal/runtime/recovery ./internal/store/sqlite -run 'TestStartupRecovery'`

Expected: PASS

### Task 6: Add failing tests for the operational HTTP server and healthcheck command

**Files:**
- Create: `internal/api/http/operational_test.go`
- Modify: `internal/app/lifecycle/serve_test.go`
- Modify: `internal/telemetry/metrics/service_test.go`

**Step 1: Write failing HTTP endpoint tests**

Cover:
- `/healthz`
- `/readyz`
- `/metrics`
- degraded readiness when the runtime is unhealthy

**Step 2: Extend lifecycle tests**

Cover:
- `healthcheck` non-zero exit on degraded or failed readiness
- `serve` runs startup recovery before reporting ready

**Step 3: Run the focused operational tests**

Run: `go test ./internal/api/http ./internal/app/lifecycle ./internal/telemetry/metrics -run 'Test(Operational|Serve|Healthcheck|Metrics)'`

Expected: FAIL because the operational server and command wiring do not exist yet.

### Task 7: Implement the operational server and machine-oriented healthcheck surfaces

**Files:**
- Create: `internal/api/http/operational.go`
- Modify: `internal/runtime/health/service.go`
- Modify: `internal/telemetry/metrics/service.go`
- Modify: `internal/app/lifecycle/run.go`

**Step 1: Add a small operational HTTP handler**

Expose:
- `/healthz`
- `/readyz`
- `/metrics`

**Step 2: Add readiness semantics**

Differentiate process-up health from safe-to-operate readiness using the existing doctor report.

**Step 3: Add the healthcheck path**

Return a non-zero result when readiness is degraded or failed.

**Step 4: Run the focused operational tests**

Run: `go test ./internal/api/http ./internal/app/lifecycle ./internal/telemetry/metrics -run 'Test(Operational|Serve|Healthcheck|Metrics)'`

Expected: PASS

### Task 8: Add failing tests for backup, restore, and verification

**Files:**
- Create: `internal/app/backup/service_test.go`
- Create: `scripts/dev/backup-odin.sh`
- Create: `scripts/dev/restore-odin.sh`
- Create: `scripts/dev/verify-backup.sh`

**Step 1: Write failing backup service tests**

Cover:
- archiving `data/odin.db`, `registry/`, `memory/`, and selected config
- restoring into a fresh target root
- verifying the restored archive opens cleanly

**Step 2: Add thin shell wrappers**

Create scripts that call into the compiled `odin` binary or Go package once the service exists.

**Step 3: Run the focused backup tests**

Run: `go test ./internal/app/backup -run 'Test(Backup|Restore|Verify)'`

Expected: FAIL because the backup service does not exist yet.

### Task 9: Implement backup and restore support

**Files:**
- Create: `internal/app/backup/service.go`
- Create: `scripts/dev/install-systemd-service.sh`
- Modify: `internal/app/lifecycle/serve.go` if needed by the command split

**Step 1: Implement the backup service**

Add:
- archive creation
- restore to target root
- verification of archive contents and SQLite openability

**Step 2: Wire the shell scripts**

Ensure the dev scripts are thin wrappers around the real backup logic and fail loudly on misuse.

**Step 3: Add the install helper**

Create a small `systemd` install script for homelab setup.

**Step 4: Run the focused backup tests**

Run: `go test ./internal/app/backup -run 'Test(Backup|Restore|Verify)'`

Expected: PASS

### Task 10: Run repo-wide verification

**Files:**
- Verify only

**Step 1: Run formatting**

Run: `make format && make fmtcheck`

Expected: exit 0

**Step 2: Run lint**

Run: `make lint`

Expected: exit 0

**Step 3: Run tests**

Run: `make test`

Expected: exit 0

**Step 4: Run build**

Run: `make build`

Expected: exit 0

### Task 11: Commit the implementation

**Files:**
- Commit all Phase 15 implementation files

**Step 1: Review the final diff**

Run: `git status --short && git diff --stat`

Expected: only Phase 15 files and generated changes are present.

**Step 2: Commit**

Run:

```bash
git add README.md config/odin.yaml deploy/systemd docs/contracts/homelab-operations.md docs/operations/cutover-readiness.md docs/plans/2026-04-09-phase-15-homelab.md internal/api/http internal/app internal/runtime/checkpoints/service.go internal/runtime/events/events.go internal/runtime/health/service.go internal/runtime/recovery internal/store/sqlite internal/telemetry/metrics scripts/dev
git commit -m "feat: add homelab deployment and cutover readiness for phase 15"
```

Expected: one implementation commit for Phase 15.
