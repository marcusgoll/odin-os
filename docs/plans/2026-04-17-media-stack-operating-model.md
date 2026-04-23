# Media Stack Operating Model Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a bounded media-stack domain to `odin-os` that extends the existing homelab substrate with read-only media observability, generic incidents, approval-aware maintenance candidates, and rollback-safe change validation.

**Architecture:** Build on `odin serve`, `odin doctor --json`, `odin healthcheck`, `/healthz`, `/readyz`, `/metrics`, generic incidents, generic recoveries, tasks, approvals, and the existing backup/restore flows. Add one media config surface, one domain package, read-only probe adapters, and one bounded media supervisor loop instead of introducing a second scheduler or a separate automation stack.

**Tech Stack:** Go, SQLite, YAML config, standard library HTTP and JSON, existing runtime health and metrics services, existing incidents and task tables, existing integration tests, shell fixtures for deterministic probe simulation

---

## Prerequisite

The current branch tip used for this planning work does not build cleanly. `go test ./...` fails before any media changes because `internal/store/sqlite/store.go` references missing runtime event symbols such as `runtimeevents.StreamConversation` and `runtimeevents.MemorySummaryRecordedPayload`.

Execute this plan only after either:

- rebasing onto a passing branch, or
- fixing that unrelated baseline breakage first

All commands below assume a buildable baseline.

### Task 1: Freeze the media operating contract and playbooks

**Files:**
- Create: `docs/contracts/media-stack-operations.md`
- Create: `docs/operations/media-stack/README.md`
- Create: `docs/operations/media-stack/plex-down.md`
- Create: `docs/operations/media-stack/disk-pressure.md`
- Create: `docs/operations/media-stack/vpn-downloader.md`
- Create: `docs/operations/media-stack/import-failures.md`
- Create: `docs/operations/media-stack/mount-mismatch.md`
- Create: `docs/operations/media-stack/seedbox-sync.md`
- Create: `docs/operations/media-stack/indexer-degradation.md`
- Create: `scripts/tests/media-stack-docs-test.sh`
- Modify: `docs/contracts/homelab-operations.md`
- Modify: `docs/operations/cutover-readiness.md`

**Step 1: Write the failing docs contract test**

Add `scripts/tests/media-stack-docs-test.sh` that asserts:

- `docs/contracts/media-stack-operations.md` exists
- each incident playbook file exists under `docs/operations/media-stack/`
- `docs/contracts/homelab-operations.md` mentions media supervision as an optional profile
- `docs/operations/cutover-readiness.md` includes media-specific cutover checks

**Step 2: Run the docs test to verify it fails**

Run: `bash scripts/tests/media-stack-docs-test.sh`

Expected: FAIL because the contract and playbook files do not exist yet.

**Step 3: Write the contract and playbooks**

Document:

- bounded media supervision on top of the existing homelab substrate
- explicit safe vs unsafe automation rules
- mount-safety fail-closed semantics
- approval-required maintenance categories
- incident playbook sections: trigger, evidence, safe actions, approval-required actions, rollback trigger, closeout

**Step 4: Run the docs test again**

Run: `bash scripts/tests/media-stack-docs-test.sh`

Expected: PASS

**Step 5: Commit**

```bash
git add docs/contracts/media-stack-operations.md docs/operations/media-stack docs/contracts/homelab-operations.md docs/operations/cutover-readiness.md scripts/tests/media-stack-docs-test.sh
git commit -m "docs: add media stack operating contract"
```

### Task 2: Add typed media-stack config and policy classification

**Files:**
- Create: `config/media-stack.yaml`
- Create: `internal/app/config/media.go`
- Create: `internal/app/config/media_test.go`
- Create: `internal/core/media/types.go`
- Create: `internal/core/media/service.go`
- Create: `internal/core/media/service_test.go`
- Modify: `internal/app/config/config.go`
- Modify: `deploy/systemd/odin.env.example`

**Step 1: Write the failing config and policy tests**

Add tests that cover:

- loading an optional `config/media-stack.yaml`
- environment override for the media config path
- validation failure when a service entry is missing required identifiers
- policy classification for `auto_allowed`, `notify_only`, `approval_required`, and `forbidden` maintenance actions

**Step 2: Run the targeted tests to verify they fail**

Run: `go test ./internal/app/config ./internal/core/media -run 'TestMedia' -v`

Expected: FAIL because the media config loader and policy layer do not exist yet.

**Step 3: Write the minimal config and policy types**

Create a single typed media config with:

- service inventory for Plex, Arr apps, downloader, VPN, and optional seedbox or Usenet integrations
- mount roots, sentinels, and expected mount sources
- threshold settings for disk, queue age, import lag, and telemetry freshness
- maintenance window configuration
- action policies for safe, approval-required, and forbidden classes

Keep secrets as environment references or handles, not inline values.

**Step 4: Run the config and policy tests again**

Run: `go test ./internal/app/config ./internal/core/media -run 'TestMedia' -v`

Expected: PASS

**Step 5: Commit**

```bash
git add config/media-stack.yaml internal/app/config/media.go internal/app/config/media_test.go internal/core/media/types.go internal/core/media/service.go internal/core/media/service_test.go internal/app/config/config.go deploy/systemd/odin.env.example
git commit -m "feat(media): add typed media stack config"
```

### Task 3: Implement read-only probe adapters and doctor integration

**Files:**
- Create: `internal/adapters/shell/media_probe.go`
- Create: `internal/adapters/shell/media_probe_test.go`
- Create: `internal/adapters/web/media_probe.go`
- Create: `internal/adapters/web/media_probe_test.go`
- Create: `internal/runtime/health/media.go`
- Create: `internal/runtime/health/media_test.go`
- Create: `scripts/tests/fixtures/media-probe-ok.sh`
- Create: `scripts/tests/fixtures/media-probe-mount-mismatch.sh`
- Modify: `internal/runtime/health/service.go`
- Modify: `internal/api/http/operational.go`
- Modify: `internal/api/http/operational_test.go`
- Modify: `internal/app/lifecycle/run.go`

**Step 1: Write the failing probe and health tests**

Add tests for:

- mount mismatch becoming `failed`
- Plex unreachable becoming `degraded`
- VPN integrity failure becoming `failed`
- queue backlog becoming `degraded`
- media checks being omitted cleanly when no media config is present

**Step 2: Run the targeted tests to verify they fail**

Run: `go test ./internal/adapters/shell ./internal/adapters/web ./internal/runtime/health ./internal/api/http -run 'Test(Media|Operational)' -v`

Expected: FAIL because media probes and health composition do not exist yet.

**Step 3: Write the minimal read-only probe layer**

Implement:

- shell-based probes for `findmnt`, `df`, and optional container or service-manager state
- HTTP-based probes for Plex, Arr apps, downloader, and optional indexer or sync endpoints
- a media health composer that converts probe results into existing `health.Check` items

Do not add mutation logic in this task.

**Step 4: Run the targeted tests again**

Run: `go test ./internal/adapters/shell ./internal/adapters/web ./internal/runtime/health ./internal/api/http -run 'Test(Media|Operational)' -v`

Expected: PASS

**Step 5: Run real `odin` doctor proof with fixture probes**

Build and run against a temp runtime root using fixture-driven probe commands so the command path is deterministic:

```bash
go build -o bin/odin ./cmd/odin
ODIN_ROOT="$(mktemp -d)" ODIN_MEDIA_PROBE_COMMAND="$PWD/scripts/tests/fixtures/media-probe-ok.sh" ./bin/odin doctor --json
ODIN_ROOT="$(mktemp -d)" ODIN_MEDIA_PROBE_COMMAND="$PWD/scripts/tests/fixtures/media-probe-mount-mismatch.sh" ./bin/odin doctor --json
```

Expected:

- first command returns structured media checks with healthy or omitted status
- second command returns a degraded or failed media check for mount mismatch

**Step 6: Commit**

```bash
git add internal/adapters/shell/media_probe.go internal/adapters/shell/media_probe_test.go internal/adapters/web/media_probe.go internal/adapters/web/media_probe_test.go internal/runtime/health/media.go internal/runtime/health/media_test.go internal/runtime/health/service.go internal/api/http/operational.go internal/api/http/operational_test.go internal/app/lifecycle/run.go scripts/tests/fixtures/media-probe-ok.sh scripts/tests/fixtures/media-probe-mount-mismatch.sh
git commit -m "feat(media): add read-only probe health integration"
```

### Task 4: Add media metrics and bounded supervisor cycles in `serve`

**Files:**
- Create: `internal/runtime/media/service.go`
- Create: `internal/runtime/media/service_test.go`
- Create: `internal/runtime/media/types.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/telemetry/metrics/service.go`
- Modify: `internal/telemetry/metrics/service_test.go`
- Modify: `internal/runtime/health/service.go`

**Step 1: Write the failing supervisor and metrics tests**

Add tests for:

- opening a generic incident when a `Critical` media signal appears
- resolving or downgrading that incident when probes recover
- recording weekly maintenance candidates without executing them
- surfacing media counters in metrics output

**Step 2: Run the targeted tests to verify they fail**

Run: `go test ./internal/runtime/media ./internal/telemetry/metrics ./internal/runtime/health -run 'TestMedia' -v`

Expected: FAIL because the supervisor and media metrics do not exist yet.

**Step 3: Write the bounded supervisor**

Implement one `serve`-loop media cycle that:

- runs the read-only media probe set
- converts failures into existing incident records
- generates notify-only maintenance candidates as existing tasks or auditable records
- never performs destructive or network-changing actions

Reuse generic incident and task tables instead of creating a second operations store.

**Step 4: Run the targeted tests again**

Run: `go test ./internal/runtime/media ./internal/telemetry/metrics ./internal/runtime/health -run 'TestMedia' -v`

Expected: PASS

**Step 5: Run real `odin serve` proof**

Run fixture-driven `serve` with a short cancellation window and inspect the resulting incident or candidate side effects:

```bash
go build -o bin/odin ./cmd/odin
ODIN_ROOT="$(mktemp -d)" ODIN_MEDIA_PROBE_COMMAND="$PWD/scripts/tests/fixtures/media-probe-mount-mismatch.sh" timeout 3s ./bin/odin serve
ODIN_ROOT="$(mktemp -d)" ODIN_MEDIA_PROBE_COMMAND="$PWD/scripts/tests/fixtures/media-probe-ok.sh" timeout 3s ./bin/odin serve
```

Expected:

- the failing probe path records a media-related incident
- the healthy probe path does not create a false incident

**Step 6: Commit**

```bash
git add internal/runtime/media/service.go internal/runtime/media/service_test.go internal/runtime/media/types.go internal/app/lifecycle/run.go internal/telemetry/metrics/service.go internal/telemetry/metrics/service_test.go internal/runtime/health/service.go
git commit -m "feat(media): add bounded serve-time supervision"
```

### Task 5: Add approval-aware maintenance and backup-gated change validation

**Files:**
- Create: `internal/core/approvals/media.go`
- Create: `internal/core/approvals/media_test.go`
- Create: `internal/runtime/media/maintenance.go`
- Create: `internal/runtime/media/maintenance_test.go`
- Modify: `internal/runtime/media/service.go`
- Modify: `internal/app/backup/service.go`
- Modify: `tests/integration/alpha_acceptance_test.go`

**Step 1: Write the failing approval and maintenance tests**

Add tests for:

- classifying restart or import-retry requests as approval-required
- rejecting forbidden actions such as media deletion or downloader network mutation
- blocking change preflight when backup verification is stale or missing
- emitting postflight rollback recommendations when a new `Critical` media failure appears

**Step 2: Run the targeted tests to verify they fail**

Run: `go test ./internal/core/approvals ./internal/runtime/media ./internal/app/backup ./tests/integration -run 'Test(Media|AlphaAcceptance)' -v`

Expected: FAIL because media approval classification and backup-gated maintenance do not exist yet.

**Step 3: Write the minimal approval-aware maintenance layer**

Implement:

- action classification for `auto_allowed`, `notify_only`, `approval_required`, and `forbidden`
- backup freshness checks that reuse the existing backup service rather than inventing a new archive model
- preflight evidence packets and postflight smoke validation

Keep all risky media actions behind explicit approval.

**Step 4: Run the targeted tests again**

Run: `go test ./internal/core/approvals ./internal/runtime/media ./internal/app/backup ./tests/integration -run 'Test(Media|AlphaAcceptance)' -v`

Expected: PASS

**Step 5: Run real `odin` command proof for backup-gated maintenance**

Run:

```bash
go build -o bin/odin ./cmd/odin
ODIN_ROOT="$(mktemp -d)" ./bin/odin backup /tmp/odin-media-test.tar.gz
ODIN_ROOT="$(mktemp -d)" ./bin/odin verify-backup /tmp/odin-media-test.tar.gz
```

Expected:

- backup archive creation still works
- verify-backup still succeeds
- media maintenance preflight can depend on this proof instead of bypassing it

**Step 6: Commit**

```bash
git add internal/core/approvals/media.go internal/core/approvals/media_test.go internal/runtime/media/maintenance.go internal/runtime/media/maintenance_test.go internal/runtime/media/service.go internal/app/backup/service.go tests/integration/alpha_acceptance_test.go
git commit -m "feat(media): add approval-aware maintenance gates"
```

### Task 6: Extend acceptance and cutover verification for the media profile

**Files:**
- Create: `tests/integration/media_stack_acceptance_test.go`
- Modify: `docs/operations/cutover-readiness.md`
- Modify: `README.md`
- Modify: `Makefile`

**Step 1: Write the failing media acceptance test**

Cover:

- doctor output for healthy and degraded media fixtures
- healthcheck fail-closed behavior when media policy says readiness should block
- `serve` creating incidents but not mutating media state without approval
- backup and verify-backup still proving the homelab substrate underneath the media profile

**Step 2: Run the failing acceptance test**

Run: `go test ./tests/integration -run 'TestMediaStackAcceptance' -count=1 -v`

Expected: FAIL because the end-to-end media path is not yet wired.

**Step 3: Implement the minimum changes to make the acceptance test pass**

Update documentation, top-level build targets, and fixture setup only as needed for the acceptance suite. Do not add a second CLI or a second supervisor loop.

**Step 4: Run the acceptance and repo verification commands**

Run:

```bash
go test ./tests/integration -run 'TestMediaStackAcceptance' -count=1 -v
go test ./tests/integration -run 'TestAlphaAcceptance' -count=1 -v
make build
```

Expected: PASS

**Step 5: Run the final real `odin` commands**

Run:

```bash
go build -o bin/odin ./cmd/odin
ODIN_ROOT="$(mktemp -d)" ODIN_MEDIA_PROBE_COMMAND="$PWD/scripts/tests/fixtures/media-probe-ok.sh" ./bin/odin doctor --json
ODIN_ROOT="$(mktemp -d)" ODIN_MEDIA_PROBE_COMMAND="$PWD/scripts/tests/fixtures/media-probe-mount-mismatch.sh" ./bin/odin healthcheck
```

Expected:

- `doctor --json` exposes media checks
- `healthcheck` fails closed when the configured media policy treats the probe result as not-ready

**Step 6: Commit**

```bash
git add tests/integration/media_stack_acceptance_test.go docs/operations/cutover-readiness.md README.md Makefile
git commit -m "test(media): add end-to-end media acceptance coverage"
```
