# Odin Command Center Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` for task-by-task execution or `superpowers:executing-plans` for inline execution. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a shared Odin operator snapshot and render it as a scannable `odin tui` cockpit plus an expanded clickable PWA dashboard.

**Source of Truth:** `docs/superpowers/specs/2026-05-18-odin-command-center-observability-design.md`

**Architecture:** Add a read-only snapshot composition seam inside the existing Odin runtime/API boundary. The PWA consumes the snapshot from an authenticated mobile endpoint. `odin tui` keeps its current Prometheus/Loki mode and adds an optional Odin snapshot URL for richer terminal rows, preserving fail-closed telemetry behavior when Prometheus is unavailable.

**Tech Stack:** Go standard library HTTP handlers, existing SQLite read-model/projection services, embedded static PWA HTML/CSS/JS, existing `internal/cli/tui` renderer.

**Verification Strategy:** Use focused Go tests for snapshot composition, TUI rendering/client behavior, and PWA static/API wiring; then build `./bin/odin` and run deterministic `odin tui --once` proof plus relevant HTTP/PWA tests.

**Approval Source:** User selected visual direction B, Command Center, on 2026-05-18.

---

## File Map

- Create: `internal/api/http/operator_snapshot.go`
  - Responsibility: compose the read-only operator snapshot from existing status, overview, review queue, approvals, work, runs, blocked/recovery, browser, and activity sources.
  - Used by: `/mobile/operator-snapshot`, future local dashboard consumers, and tests.

- Modify: `internal/api/http/mobile.go`
  - Current responsibility: owns authenticated mobile/PWA API routes.
  - Planned change: register `GET /mobile/operator-snapshot` and return the shared snapshot.

- Test: `internal/api/http/operational_test.go`
  - Covers: authenticated snapshot endpoint exposes action rows, health, live execution, and activity data from seeded runtime state.

- Modify: `internal/cli/tui/model.go`
  - Current responsibility: terminal TUI model for telemetry counts and recent logs.
  - Planned change: add optional snapshot row/detail fields without removing current metric fields.

- Modify: `internal/cli/tui/client.go`
  - Current responsibility: query Prometheus/Loki and run one-shot or continuous terminal rendering.
  - Planned change: add `--odin-url` and optional `--admin-token` flags; when present, query `/mobile/operator-snapshot` after Prometheus succeeds and merge richer rows into the TUI model.

- Modify: `internal/cli/tui/render.go`
  - Current responsibility: render boxed terminal panels.
  - Planned change: render separate `ACTION REQUIRED`, `ODIN HEALTH`, `LIVE EXECUTION`, `ACTIVITY`, and `RECENT LOGS` panels with command hints and rune-safe truncation.

- Test: `internal/cli/tui/client_test.go`
  - Covers: optional Odin snapshot fetch, auth header behavior, and fallback when snapshot is unavailable.

- Test: `internal/cli/tui/render_test.go`
  - Covers: richer boxed panel output and row truncation that preserves action-critical IDs.

- Modify: `internal/api/http/app_static/index.html`
  - Current responsibility: embedded PWA shell markup.
  - Planned change: add dashboard regions for health, live execution, activity, and detail drawer while preserving registration, capture, approvals, and failed-upload controls.

- Modify: `internal/api/http/app_static/app.js`
  - Current responsibility: PWA API calls, dashboard rendering, capture, approval decisions, registration.
  - Planned change: call `/mobile/operator-snapshot`, render snapshot rows, and open row details in a drawer or expandable panel.

- Modify: `internal/api/http/app_static/styles.css`
  - Current responsibility: PWA layout and visual styling.
  - Planned change: add dense neutral command-center layout, single teal accent, responsive two-column desktop/single-column mobile grid, loading/empty/error/detail states.

- Test: `internal/api/http/pwa_test.go`
  - Covers: static shell includes new dashboard/detail regions and JavaScript wiring to `/mobile/operator-snapshot`.

- Modify: `docs/contracts/observability.md`
  - Current responsibility: observability contract and TUI rules.
  - Planned change: document that the Operator Snapshot is a read-only `odin serve` projection consumed by TUI/PWA and not a second runtime authority.

## Scope Check

Proceeding with one plan: the Command Center observability slice. It touches one backend read model and two presentation frontends. It does not add new mutation actions, daemon processes, external integrations, migrations, or deployment rewiring.

### Task 1: Add Operator Snapshot Endpoint

**Purpose:** Give terminal and web frontends one read-only operator snapshot composed from existing Odin runtime truth.

**Files:**
- Create: `internal/api/http/operator_snapshot.go`
- Modify: `internal/api/http/mobile.go`
- Test: `internal/api/http/operational_test.go`
- Modify: `docs/contracts/observability.md`

**Acceptance Criteria:**
- [ ] `GET /mobile/operator-snapshot` requires existing mobile authorization.
- [ ] Snapshot includes sections for action-required, health, live execution, activity, and browser intervention rows.
- [ ] Rows include stable IDs, labels, summaries, severity, detail payloads already available from existing projections, and command/deep-link hints.
- [ ] The endpoint only reads existing runtime state and does not mutate approvals, review queue, triggers, or work.

- [ ] **Step 1: Write the failing endpoint test**

Add `TestMobileOperatorSnapshotExposesCommandCenterRows` in `internal/api/http/operational_test.go`:

```go
func TestMobileOperatorSnapshotExposesCommandCenterRows(t *testing.T) {
    // Seed healthy observability, runtime state, operator read models, and at least one event.
    // GET /mobile/operator-snapshot with X-Odin-Admin-Token.
    // Assert JSON contains "action_required", "odin_health", "live_execution", "activity",
    // a review/approval row, and an inspect command such as "odin review list" or "odin runs show".
}
```

- [ ] **Step 2: Run the narrow test and confirm failure**

Run:

```bash
cd /home/orchestrator/odin-os && go test -count=1 ./internal/api/http -run TestMobileOperatorSnapshotExposesCommandCenterRows
```

Expected:

```text
FAIL because /mobile/operator-snapshot is not registered.
```

- [ ] **Step 3: Implement the minimal endpoint**

Create `internal/api/http/operator_snapshot.go` with:

```go
type operatorSnapshot struct {
    GeneratedAt     string                    `json:"generated_at"`
    ActionRequired  []operatorSnapshotRow     `json:"action_required"`
    OdinHealth      operatorSnapshotHealth    `json:"odin_health"`
    LiveExecution   []operatorSnapshotRow     `json:"live_execution"`
    Activity        []operatorSnapshotRow     `json:"activity"`
    Browser         []operatorSnapshotRow     `json:"browser"`
}
```

Build rows from existing helpers and projections already used by `mobile.go` and `overview.Service`. Register:

```go
mux.HandleFunc("GET /mobile/operator-snapshot", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
    snapshot, err := buildOperatorSnapshot(request.Context(), deps, now)
    if err != nil {
        writeAPIError(writer, http.StatusServiceUnavailable, "operator_snapshot_unavailable", err.Error())
        return
    }
    writeMobileJSON(writer, http.StatusOK, snapshot)
}))
```

Update `docs/contracts/observability.md` to describe the snapshot as a read-only `odin serve` projection.

- [ ] **Step 4: Run the narrow test and confirm pass**

Run:

```bash
cd /home/orchestrator/odin-os && go test -count=1 ./internal/api/http -run TestMobileOperatorSnapshotExposesCommandCenterRows
```

Expected:

```text
PASS
```

- [ ] **Step 5: Run broader relevant checks**

Run:

```bash
cd /home/orchestrator/odin-os && go test -count=1 ./internal/api/http
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
cd /home/orchestrator/odin-os && git status --short
cd /home/orchestrator/odin-os && git add internal/api/http/operator_snapshot.go internal/api/http/mobile.go internal/api/http/operational_test.go docs/contracts/observability.md
cd /home/orchestrator/odin-os && git commit -m "feat: add operator snapshot endpoint"
```

### Task 2: Render Snapshot Rows in `odin tui`

**Purpose:** Make `odin tui` human-readable and scannable while preserving current Prometheus/Loki proof behavior.

**Files:**
- Modify: `internal/cli/tui/model.go`
- Modify: `internal/cli/tui/client.go`
- Modify: `internal/cli/tui/render.go`
- Test: `internal/cli/tui/client_test.go`
- Test: `internal/cli/tui/render_test.go`

**Acceptance Criteria:**
- [ ] Existing `odin tui --once`, `--interval`, and `--no-clear` behavior remains compatible.
- [ ] Prometheus unavailability still returns `ErrUnavailableTelemetry`.
- [ ] When `--odin-url` is supplied, TUI fetches `/mobile/operator-snapshot` and renders row-level action-required, live execution, and activity detail.
- [ ] Snapshot unavailability does not mask telemetry failure; if Prometheus is healthy but snapshot fetch fails, render a controlled `snapshot unavailable` row.
- [ ] Terminal output remains boxed and rune-safe.

- [ ] **Step 1: Write failing TUI tests**

Update `internal/cli/tui/render_test.go` with `TestRenderOverviewShowsCommandCenterPanels`:

```go
func TestRenderOverviewShowsCommandCenterPanels(t *testing.T) {
    output := RenderOverview(Model{
        TelemetryAvailable: true,
        Status: "healthy",
        HealthScore: 92,
        SnapshotRows: []SnapshotRow{{Section: "action_required", Title: "Approval alpha", Command: "odin approvals show 7"}},
        ActivityRows: []SnapshotRow{{Section: "activity", Title: "approval.requested", Command: "odin logs show 12"}},
    })
    // Assert ACTION REQUIRED, ODIN HEALTH, LIVE EXECUTION, ACTIVITY, and command hints render.
}
```

Update `internal/cli/tui/client_test.go` with a test server that returns `/mobile/operator-snapshot` and assert `--odin-url` merges rows.

- [ ] **Step 2: Run narrow tests and confirm failure**

Run:

```bash
cd /home/orchestrator/odin-os && go test -count=1 ./internal/cli/tui -run 'TestRenderOverviewShowsCommandCenterPanels|TestRunMergesOdinSnapshot'
```

Expected:

```text
FAIL because SnapshotRows/ActivityRows and --odin-url behavior do not exist.
```

- [ ] **Step 3: Implement the minimal TUI change**

Add optional snapshot fields to `Model`, add flags in `Run`, implement a small snapshot HTTP client in `client.go`, and update `render.go` to draw:

- `ACTION REQUIRED`
- `ODIN HEALTH`
- `LIVE EXECUTION`
- `ACTIVITY`
- `RECENT LOGS`

Keep metric-only rendering useful when no snapshot URL is supplied.

- [ ] **Step 4: Run narrow tests and confirm pass**

Run:

```bash
cd /home/orchestrator/odin-os && go test -count=1 ./internal/cli/tui
```

Expected:

```text
PASS
```

- [ ] **Step 5: Run broader relevant checks**

Run:

```bash
cd /home/orchestrator/odin-os && go test -count=1 ./internal/cli/tui ./internal/app/lifecycle
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
cd /home/orchestrator/odin-os && git status --short
cd /home/orchestrator/odin-os && git add internal/cli/tui/model.go internal/cli/tui/client.go internal/cli/tui/render.go internal/cli/tui/client_test.go internal/cli/tui/render_test.go internal/app/lifecycle/run_test.go
cd /home/orchestrator/odin-os && git commit -m "feat: render operator snapshot in odin tui"
```

### Task 3: Expand PWA Dashboard With Clickable Details

**Purpose:** Turn `odin.marcusgoll.com/app/` into a first-screen operator command center with clickable details.

**Files:**
- Modify: `internal/api/http/app_static/index.html`
- Modify: `internal/api/http/app_static/app.js`
- Modify: `internal/api/http/app_static/styles.css`
- Test: `internal/api/http/pwa_test.go`
- Test: `internal/api/http/pwa_approvals_test.go`

**Acceptance Criteria:**
- [ ] PWA loads `/mobile/operator-snapshot` with existing mobile auth and CSRF/session behavior unchanged.
- [ ] First viewport shows action-required, Odin health, live execution, and activity timeline.
- [ ] Clicking a row opens a detail drawer/panel with source identifiers, command hints, allowed actions or deep links when present.
- [ ] Loading, empty, auth-required, and error states fit the new layout.
- [ ] Capture and approval decision flows still work.
- [ ] CSS remains responsive, dense, neutral, and avoids decorative gradients, fake data, and overlapping text.

- [ ] **Step 1: Write failing static/PWA tests**

Update `internal/api/http/pwa_test.go`:

```go
// Assert /app/ contains ids:
// command-center-dashboard, odin-health-panel, live-execution-list,
// activity-timeline-list, detail-drawer.
// Assert app.js contains /mobile/operator-snapshot and data-detail-row.
```

- [ ] **Step 2: Run narrow tests and confirm failure**

Run:

```bash
cd /home/orchestrator/odin-os && go test -count=1 ./internal/api/http -run 'TestOperationalHandlerServesInstallablePWAShellAssets|TestPWAStaticShellIncludesApprovalControls'
```

Expected:

```text
FAIL because the static shell and app.js do not include the new command-center regions.
```

- [ ] **Step 3: Implement the PWA dashboard**

Modify the static shell to add command-center sections and a detail drawer. Update `refreshDashboard` to fetch `/mobile/operator-snapshot` alongside existing endpoints, render the snapshot first, and preserve existing fallback renderers for approvals, capture, and browser rows.

Use CSS grid for desktop, single-column mobile collapse, fixed-size row controls, and inline skeleton-style loading rows instead of circular spinners.

- [ ] **Step 4: Run narrow tests and confirm pass**

Run:

```bash
cd /home/orchestrator/odin-os && go test -count=1 ./internal/api/http -run 'TestOperationalHandlerServesInstallablePWAShellAssets|TestPWAStaticShellIncludesApprovalControls'
```

Expected:

```text
PASS
```

- [ ] **Step 5: Run broader relevant checks**

Run:

```bash
cd /home/orchestrator/odin-os && go test -count=1 ./internal/api/http
cd /home/orchestrator/odin-os && node scripts/tests/assert-odin-pwa-static.mjs
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
cd /home/orchestrator/odin-os && git status --short
cd /home/orchestrator/odin-os && git add internal/api/http/app_static/index.html internal/api/http/app_static/app.js internal/api/http/app_static/styles.css internal/api/http/pwa_test.go internal/api/http/pwa_approvals_test.go
cd /home/orchestrator/odin-os && git commit -m "feat: expand odin pwa command center"
```

## Final Verification

Run:

```bash
cd /home/orchestrator/odin-os && go test -count=1 ./internal/api/http ./internal/cli/tui ./internal/app/lifecycle
cd /home/orchestrator/odin-os && node scripts/tests/assert-odin-pwa-static.mjs
cd /home/orchestrator/odin-os && go build -o ./bin/odin ./cmd/odin
cd /home/orchestrator/odin-os && tmp=$(mktemp -d); ODIN_ROOT="$tmp" ./bin/odin tui --once --prometheus-url http://127.0.0.1:1 --loki-url http://127.0.0.1:1 >/tmp/odin-tui-unavailable.out 2>&1; code=$?; rm -rf "$tmp"; test "$code" -eq 0; grep -q "HEALTH        UNKNOWN" /tmp/odin-tui-unavailable.out; grep -q "TELEMETRY     unavailable" /tmp/odin-tui-unavailable.out; ! grep -q "HEALTHY" /tmp/odin-tui-unavailable.out; rm -f /tmp/odin-tui-unavailable.out
```

Expected:

```text
Go tests pass, static PWA assertion passes, build succeeds, and unavailable Prometheus still renders UNKNOWN/unavailable without claiming healthy.
```

When a local `odin serve` plus Prometheus fixture path is available, also run:

```bash
cd /home/orchestrator/odin-os && ./bin/odin tui --once --odin-url http://127.0.0.1:<port> --admin-token <token>
```

Expected:

```text
TUI renders action-required, health, live execution, activity, and recent-log panels from the served snapshot.
```

## Rollout Notes

- No database migration is planned.
- No new daemon is planned.
- Existing PWA auth/session/CSRF behavior must remain unchanged.
- Existing approval decision mutation paths remain the only PWA mutation path touched by this slice.

## Self-Review

- [x] Every requirement maps to at least one task.
- [x] File paths are exact.
- [x] Commands are exact and start with `cd /home/orchestrator/odin-os &&`.
- [x] Tests are behavior-oriented around endpoint, TUI, and PWA seams.
- [x] The plan avoids a second observability authority.
- [x] No implementation work is included in this plan.
