# Huginn DOM Fast Lanes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` for task-by-task execution or `superpowers:executing-plans` for inline execution. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build one fixture-backed, read-only Huginn DOM Fast Lane through the existing Odin tool and browser-driver surfaces.

**Source of Truth:** `docs/superpowers/specs/2026-05-18-huginn-dom-fast-lanes-design.md`

**Architecture:** Add a named `browser_dom_fast_lane` builtin tool that calls a new web adapter and repo-local Huginn driver script. Keep Odin as control plane, keep Huginn as the browser/evidence lane, and support only domain-scoped read-only extraction with blocked/intervention responses for drift or bot/auth barriers.

**Tech Stack:** Go builtin tool catalog, Go web adapter, JSON-over-stdin/stdout shell driver, existing `scripts/browser/browser-access.sh` helpers, existing REPL `/tool run` operator surface, Go tests, shell-driver integration tests.

**Verification Strategy:** Use TDD for adapter/catalog/invocation behavior, deterministic shell-driver fixture tests, and final repo-local `./bin/odin` proof through `/tool run`.

**Approval Source:** Active Codex Goal and committed brainstorming spec.

---

## Scope Check

Proceeding with one plan: fixture-backed read-only DOM fast lane.

Deferred from this slice:

- recurring webhook endpoint
- live non-fixture site recipe
- hidden/private API replay
- mutation continuation
- authenticated portal automation

## File Map

- Modify: `docs/contracts/live-driver-tools.md`
  - Current responsibility: Documents live external driver wiring and browser/social boundaries.
  - Planned change: Add the DOM fast-lane driver env var, request/response contract, blocked statuses, and no-hidden-API boundary.

- Create: `internal/adapters/web/dom_fast_lane_driver.go`
  - Responsibility: Define request/input structs and invoke the configured DOM fast-lane driver.
  - Used by: `internal/tools/invocation.Service.HuginnDOMFastLane`.

- Create: `internal/adapters/web/dom_fast_lane_driver_test.go`
  - Covers: Driver env config, request JSON, completed response, blocked response, missing config, mismatched tool key, and invalid status.

- Modify: `internal/adapters/web/driver_common.go`
  - Current responsibility: Invokes configured web drivers and only accepts `completed`.
  - Planned change: Add an allow-status helper so DOM fast lanes can return `completed` or `blocked` without weakening existing drivers.

- Modify: `internal/tools/invocation/service.go`
  - Current responsibility: Wraps configured live drivers and normalizes results.
  - Planned change: Add `HuginnDOMFastLane`.

- Modify: `internal/tools/invocation/service_test.go`
  - Covers: Structured artifact preservation for completed and blocked DOM fast-lane results.

- Modify: `internal/tools/catalog/builtin.go`
  - Current responsibility: Defines builtin tool cards, schemas, invokes, and structured result mapping.
  - Planned change: Add `browser_dom_fast_lane` as read-only, non-approval-required builtin tool with recipe/url/label/headless/wait inputs and structured artifacts.

- Modify: `internal/tools/catalog/builtin_test.go`
  - Covers: Definition availability, schema, invoke handler, key facts, completed mapping, and blocked mapping.

- Create: `scripts/drivers/huginn-dom-fast-lane.sh`
  - Responsibility: Deterministic read-only browser recipe runner for `fixture_status`.
  - Used by: `ODIN_HUGINN_DOM_FAST_LANE_DRIVER`.

- Modify: `tests/integration/live_driver_scripts_test.go`
  - Current responsibility: Tests live driver scripts with fixture browser libraries.
  - Planned change: Add completed, selector drift, mutation-shaped request, and bot/auth challenge tests for the new script.

- Modify: `internal/cli/repl/shell_test.go`
  - Current responsibility: Verifies `/tool` operator surface behavior.
  - Planned change: Add `/tool run browser_dom_fast_lane ...` readback test with fixture driver.

## Task 1: Contract And Web Adapter

**Purpose:** Define the DOM fast-lane driver contract in docs and Go, including `blocked` as a valid non-successful-but-safe outcome.

**Files:**
- Modify: `docs/contracts/live-driver-tools.md`
- Create: `internal/adapters/web/dom_fast_lane_driver.go`
- Create: `internal/adapters/web/dom_fast_lane_driver_test.go`
- Modify: `internal/adapters/web/driver_common.go`

**Acceptance Criteria:**
- [ ] `ODIN_HUGINN_DOM_FAST_LANE_DRIVER` is documented.
- [ ] `browser_dom_fast_lane` requests include `recipe_key`, `target_url`, `label`, `wait_ms`, `headless`, and optional `allowed_domain`.
- [ ] Driver accepts `completed` and `blocked`; other statuses fail.
- [ ] Existing web drivers still require `completed`.

- [ ] **Step 1: Write the failing tests**

Add `internal/adapters/web/dom_fast_lane_driver_test.go` with tests:

```go
func TestDOMFastLaneDriverInvokesConfiguredDriver(t *testing.T)
func TestDOMFastLaneDriverAllowsBlockedStatus(t *testing.T)
func TestDOMFastLaneDriverRejectsMissingDriver(t *testing.T)
func TestDOMFastLaneDriverRejectsUnexpectedToolKey(t *testing.T)
func TestDOMFastLaneDriverRejectsUnexpectedStatus(t *testing.T)
```

Expected assertions:

- configured script receives JSON containing `"tool_key":"browser_dom_fast_lane"`
- completed response preserves `recipe_key`, `final_url`, `selector_version`, and `snapshot_excerpt`
- blocked response preserves `intervention_reason:"selector_drift"`
- missing env returns `driver command not configured`
- wrong tool key returns `does not match request`
- status `mutated` returns invalid status error

- [ ] **Step 2: Run the narrow test and confirm failure**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/adapters/web -run 'TestDOMFastLaneDriver' -count=1
```

Expected:

```text
FAIL because DOMFastLaneDriver types and NewDOMFastLaneDriver do not exist.
```

- [ ] **Step 3: Implement the minimal change**

Modify `internal/adapters/web/driver_common.go` by adding an allow-list helper without changing existing callers:

```go
func invokeDriverCommandAllowStatuses(ctx context.Context, envVar string, requestBytes []byte, requestedToolKey string, allowedStatuses map[string]struct{}) (Response, error)
```

Keep `invokeDriverCommand` as a wrapper that passes only `completed`.

Create `internal/adapters/web/dom_fast_lane_driver.go` with:

```go
const domFastLaneDriverEnvVar = "ODIN_HUGINN_DOM_FAST_LANE_DRIVER"
const domFastLaneToolKey = "browser_dom_fast_lane"

type DOMFastLaneInput struct {
    RecipeKey     string `json:"recipe_key"`
    TargetURL     string `json:"target_url"`
    Label         string `json:"label,omitempty"`
    WaitMS        string `json:"wait_ms,omitempty"`
    Headless      string `json:"headless,omitempty"`
    AllowedDomain string `json:"allowed_domain,omitempty"`
}

type DOMFastLaneRequest struct {
    ToolKey string           `json:"tool_key"`
    Input   DOMFastLaneInput `json:"input"`
}

type DOMFastLaneDriver struct {
    EnvVar string
}
```

`Invoke` should default `ToolKey` to `browser_dom_fast_lane` and call the allow-status helper with `completed` and `blocked`.

Document the env var, request, response, and blocked reasons in `docs/contracts/live-driver-tools.md`.

- [ ] **Step 4: Run the narrow test and confirm pass**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/adapters/web -run 'TestDOMFastLaneDriver|TestVisualDriver|TestXPostDriver' -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 5: Run broader relevant checks**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/adapters/web -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
cd /home/orchestrator/odin-os && git status --short
cd /home/orchestrator/odin-os && git add docs/contracts/live-driver-tools.md internal/adapters/web/driver_common.go internal/adapters/web/dom_fast_lane_driver.go internal/adapters/web/dom_fast_lane_driver_test.go
cd /home/orchestrator/odin-os && git commit -m "feat: add dom fast lane web driver"
```

## Task 2: Invocation And Builtin Tool

**Purpose:** Expose the DOM fast lane through the existing builtin tool catalog and invocation service without creating a new command family.

**Files:**
- Modify: `internal/tools/invocation/service.go`
- Modify: `internal/tools/invocation/service_test.go`
- Modify: `internal/tools/catalog/builtin.go`
- Modify: `internal/tools/catalog/builtin_test.go`

**Acceptance Criteria:**
- [ ] `invocation.Service.HuginnDOMFastLane` returns structured completed and blocked results.
- [ ] `BuiltinDefinitions()` exposes `browser_dom_fast_lane`.
- [ ] Tool schema requires `recipe_key` and `target_url`.
- [ ] Tool output includes `recipe_key`, `source_url`, `final_url`, `status`, `intervention_reason`, `screenshot_path`, `snapshot_excerpt`, and `selector_version` when available.
- [ ] Tool is read-only and does not set `RequiresApproval`.

- [ ] **Step 1: Write the failing tests**

In `internal/tools/invocation/service_test.go`, add:

```go
func TestServiceExposesDOMFastLaneResults(t *testing.T)
func TestServicePreservesBlockedDOMFastLaneResult(t *testing.T)
```

In `internal/tools/catalog/builtin_test.go`, add or extend:

```go
func TestBuiltinDefinitionsExposeDOMFastLaneTool(t *testing.T)
func TestBuiltinDefinitionsInvokeDOMFastLaneTool(t *testing.T)
func TestBuiltinDefinitionsPreserveBlockedDOMFastLaneResult(t *testing.T)
```

- [ ] **Step 2: Run the narrow test and confirm failure**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/tools/invocation ./internal/tools/catalog -run 'DOMFastLane|BuiltinDefinitionsExposeDOMFastLaneTool|BuiltinDefinitionsInvokeDOMFastLaneTool' -count=1
```

Expected:

```text
FAIL because HuginnDOMFastLane and browser_dom_fast_lane are not wired.
```

- [ ] **Step 3: Implement the minimal change**

Add to `internal/tools/invocation/service.go`:

```go
func (service Service) HuginnDOMFastLane(ctx context.Context, request webdriver.DOMFastLaneRequest) (BrowserResult, error) {
    response, err := webdriver.NewDOMFastLaneDriver().Invoke(ctx, request)
    if err != nil {
        return BrowserResult{}, err
    }
    return browserResultFromResponse(response.ToolKey, response.Summary, response.Artifacts, response)
}
```

Add a builtin definition in `internal/tools/catalog/builtin.go` near other browser tools:

- `Key: "browser_dom_fast_lane"`
- `CanonicalKey: "browser_dom_fast_lane"`
- `Title: "Browser DOM Fast Lane"`
- `Summary: "Runs a named read-only Browser Control recipe and returns typed DOM evidence."`
- `Scopes: []string{"global", "project", "odin-core"}`
- `Tags: []string{"browser", "dom", "evidence", "live"}`
- `CostHint: CostHintMedium`
- schema required fields `recipe_key`, `target_url`
- invoke via `invocation.Service{}.HuginnDOMFastLane(...)`

Map artifacts into `StructuredResult.Artifacts` and `KeyFacts` with explicit string conversions.

- [ ] **Step 4: Run the narrow test and confirm pass**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/tools/invocation ./internal/tools/catalog -run 'DOMFastLane|BuiltinDefinitionsExposeDOMFastLaneTool|BuiltinDefinitionsInvokeDOMFastLaneTool' -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 5: Run broader relevant checks**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/tools/invocation ./internal/tools/catalog ./internal/tools/broker -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
cd /home/orchestrator/odin-os && git status --short
cd /home/orchestrator/odin-os && git add internal/tools/invocation/service.go internal/tools/invocation/service_test.go internal/tools/catalog/builtin.go internal/tools/catalog/builtin_test.go
cd /home/orchestrator/odin-os && git commit -m "feat: expose dom fast lane tool"
```

## Task 3: Fixture Driver Script

**Purpose:** Provide a deterministic repo-local Huginn driver script that proves read-only DOM extraction, selector drift fallback, and mutation refusal before any live site.

**Files:**
- Create: `scripts/drivers/huginn-dom-fast-lane.sh`
- Modify: `tests/integration/live_driver_scripts_test.go`

**Acceptance Criteria:**
- [ ] `fixture_status` recipe navigates to a provided URL and extracts status rows through read-only `browser_evaluate`.
- [ ] Driver emits `completed` with typed `data`, snapshot, screenshot, final URL, and selector version.
- [ ] Selector drift emits `blocked` with `intervention_reason=selector_drift`.
- [ ] Login/bot text emits `blocked` with `intervention_reason=captcha_or_bot_check` or `login_required`.
- [ ] Mutation-shaped recipe or input is rejected before browser action.
- [ ] Driver never accepts arbitrary JavaScript from operator input.

- [ ] **Step 1: Write the failing tests**

Add tests to `tests/integration/live_driver_scripts_test.go`:

```go
func TestHuginnDOMFastLaneDriverScriptExtractsFixtureStatus(t *testing.T)
func TestHuginnDOMFastLaneDriverScriptReportsSelectorDrift(t *testing.T)
func TestHuginnDOMFastLaneDriverScriptBlocksBotOrLoginChallenge(t *testing.T)
func TestHuginnDOMFastLaneDriverScriptRejectsMutationRecipe(t *testing.T)
```

Use a temp `ODIN_BROWSER_ACCESS_LIB_PATH` fixture that records calls and implements:

```bash
browser_server_start
browser_server_health
browser_navigate
browser_snapshot
browser_evaluate
browser_bc_screenshot
browser_server_stop
```

The fixture `browser_evaluate` should return JSON shaped like:

```json
{
  "source_url": "http://127.0.0.1:18080/status-fixture",
  "final_url": "http://127.0.0.1:18080/status-fixture",
  "page_status": "Ready",
  "rows": [{"name": "alpha", "state": "green"}],
  "selector_version": "fixture-v1"
}
```

- [ ] **Step 2: Run the narrow test and confirm failure**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./tests/integration -run 'TestHuginnDOMFastLaneDriverScript' -count=1
```

Expected:

```text
FAIL because scripts/drivers/huginn-dom-fast-lane.sh does not exist.
```

- [ ] **Step 3: Implement the minimal change**

Create `scripts/drivers/huginn-dom-fast-lane.sh` following the existing driver style:

- parse stdin JSON with Python
- require `target_url`
- default `recipe_key` to empty and reject unless exactly `fixture_status`
- reject recipe keys containing `submit`, `post`, `publish`, `delete`, `buy`, `sell`, `transfer`, `like`, `follow`, or `message`
- source `ODIN_BROWSER_ACCESS_LIB_PATH` or `scripts/browser/browser-access.sh`
- use `browser_request_domain_access` when available
- start headed/headless browser based on input
- navigate to target URL
- capture snapshot
- detect login/bot challenge from snapshot text
- call one hard-coded read-only `browser_evaluate` function owned inside the script
- validate expected fields
- capture screenshot
- emit one JSON response

Set executable bit:

```bash
cd /home/orchestrator/odin-os && chmod +x scripts/drivers/huginn-dom-fast-lane.sh
```

- [ ] **Step 4: Run the narrow test and confirm pass**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./tests/integration -run 'TestHuginnDOMFastLaneDriverScript' -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 5: Run broader relevant checks**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./tests/integration -run 'Test.*DriverScript' -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
cd /home/orchestrator/odin-os && git status --short
cd /home/orchestrator/odin-os && git add scripts/drivers/huginn-dom-fast-lane.sh tests/integration/live_driver_scripts_test.go
cd /home/orchestrator/odin-os && git commit -m "feat: add fixture dom fast lane driver"
```

## Task 4: Operator Surface Proof

**Purpose:** Prove the new fast lane through the existing `/tool run` operator surface and make the output useful to operators without adding a parallel command.

**Files:**
- Modify: `internal/cli/repl/shell_test.go`

**Acceptance Criteria:**
- [ ] `/tool run browser_dom_fast_lane ...` invokes the configured driver.
- [ ] Output includes `browser_dom_fast_lane`, `fixture_status`, `Ready`, and `selector_version`.
- [ ] Blocked output includes `intervention_reason=selector_drift`.
- [ ] The tool remains available through the canonical key only; no hidden `huginn_*` alias is required in v1.

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/repl/shell_test.go`:

```go
func TestShellToolRunBrowserDOMFastLane(t *testing.T)
func TestShellToolRunBrowserDOMFastLaneBlockedResult(t *testing.T)
```

Use a fixture script in `t.TempDir()` and `t.Setenv("ODIN_HUGINN_DOM_FAST_LANE_DRIVER", script)`.

The completed script output should include:

```json
{
  "status": "completed",
  "tool_key": "browser_dom_fast_lane",
  "summary": "Extracted fixture status table.",
  "artifacts": {
    "recipe_key": "fixture_status",
    "source_url": "http://127.0.0.1:18080/status-fixture",
    "final_url": "http://127.0.0.1:18080/status-fixture",
    "page_status": "Ready",
    "selector_version": "fixture-v1",
    "snapshot_excerpt": "Ready alpha green"
  }
}
```

- [ ] **Step 2: Run the narrow test and confirm failure**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/cli/repl -run 'TestShellToolRunBrowserDOMFastLane' -count=1
```

Expected:

```text
FAIL because browser_dom_fast_lane is not available through /tool run.
```

- [ ] **Step 3: Implement the minimal change**

This task should need no production code if Tasks 1 and 2 are correct. If the test fails because `renderToolResult` hides needed artifacts, update only the rendering path to include existing `StructuredResult.Artifacts` values; do not add a new browser command.

- [ ] **Step 4: Run the narrow test and confirm pass**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/cli/repl -run 'TestShellToolRunBrowserDOMFastLane' -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 5: Run broader relevant checks**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/cli/repl ./internal/tools/catalog ./internal/tools/invocation ./internal/adapters/web -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
cd /home/orchestrator/odin-os && git status --short
cd /home/orchestrator/odin-os && git add internal/cli/repl/shell_test.go
cd /home/orchestrator/odin-os && git commit -m "test: prove dom fast lane tool surface"
```

## Task 5: Real Odin Fixture E2E

**Purpose:** Verify the complete slice with the repo-local binary and document the proven/unproven boundary.

**Files:**
- Modify: `docs/contracts/live-driver-tools.md`

**Acceptance Criteria:**
- [ ] `go test` focused packages pass.
- [ ] `go build -o ./bin/odin ./cmd/odin` passes.
- [ ] Repo-local `./bin/odin` can run `/tool run browser_dom_fast_lane ...` against a fixture driver.
- [ ] Contract states webhooks are deferred until recipe contract proof.
- [ ] Contract states hidden/private API replay remains rejected by default.

- [ ] **Step 1: Write or update contract proof notes**

Update `docs/contracts/live-driver-tools.md` with a short "DOM fast lane proof" subsection that names the fixture recipe, expected status values, and deferred webhook boundary.

- [ ] **Step 2: Run the focused test set**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/adapters/web ./internal/tools/invocation ./internal/tools/catalog ./internal/cli/repl ./tests/integration -run 'DOMFastLane|TestHuginnDOMFastLaneDriverScript|TestShellToolRunBrowserDOMFastLane' -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 3: Build Odin**

Run:

```bash
cd /home/orchestrator/odin-os && go build -o ./bin/odin ./cmd/odin
```

Expected:

```text
PASS with no output.
```

- [ ] **Step 4: Run real operator proof**

Run:

```bash
cd /home/orchestrator/odin-os && bash <<'BASH'
tmp="$(mktemp -d)"
driver="$(mktemp)"
cat >"$driver" <<'SH'
#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"completed","tool_key":"browser_dom_fast_lane","summary":"Extracted fixture status table.","artifacts":{"recipe_key":"fixture_status","source_url":"http://127.0.0.1:18080/status-fixture","final_url":"http://127.0.0.1:18080/status-fixture","page_status":"Ready","selector_version":"fixture-v1","snapshot_excerpt":"Ready alpha green","screenshot_path":"/tmp/fixture-status.png"}}'
SH
chmod +x "$driver"
ODIN_ROOT="$tmp" ODIN_HUGINN_DOM_FAST_LANE_DRIVER="$driver" ./bin/odin <<'EOF'
/tool run browser_dom_fast_lane recipe_key=fixture_status target_url=http://127.0.0.1:18080/status-fixture label=fixture-status wait_ms=0 headless=true
/quit
EOF
BASH
```

Expected:

```text
Output contains browser_dom_fast_lane, fixture_status, Ready, selector_version, and /tmp/fixture-status.png.
```

- [ ] **Step 5: Run broader relevant checks**

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/adapters/web ./internal/tools/invocation ./internal/tools/catalog ./internal/tools/broker ./internal/cli/repl ./tests/integration -count=1
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```bash
cd /home/orchestrator/odin-os && git status --short
cd /home/orchestrator/odin-os && git add docs/contracts/live-driver-tools.md
cd /home/orchestrator/odin-os && git commit -m "docs: record dom fast lane proof boundary"
```

## Final Verification

Run:

```bash
cd /home/orchestrator/odin-os && go test ./internal/adapters/web ./internal/tools/invocation ./internal/tools/catalog ./internal/tools/broker ./internal/cli/repl ./tests/integration -count=1
cd /home/orchestrator/odin-os && go build -o ./bin/odin ./cmd/odin
cd /home/orchestrator/odin-os && bash <<'BASH'
tmp="$(mktemp -d)"
driver="$(mktemp)"
cat >"$driver" <<'SH'
#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"completed","tool_key":"browser_dom_fast_lane","summary":"Extracted fixture status table.","artifacts":{"recipe_key":"fixture_status","source_url":"http://127.0.0.1:18080/status-fixture","final_url":"http://127.0.0.1:18080/status-fixture","page_status":"Ready","selector_version":"fixture-v1","snapshot_excerpt":"Ready alpha green","screenshot_path":"/tmp/fixture-status.png"}}'
SH
chmod +x "$driver"
ODIN_ROOT="$tmp" ODIN_HUGINN_DOM_FAST_LANE_DRIVER="$driver" ./bin/odin <<'EOF'
/tool run browser_dom_fast_lane recipe_key=fixture_status target_url=http://127.0.0.1:18080/status-fixture label=fixture-status wait_ms=0 headless=true
/quit
EOF
BASH
```

Expected:

```text
All tests pass, build succeeds, and real ./bin/odin output contains browser_dom_fast_lane, fixture_status, Ready, selector_version, and /tmp/fixture-status.png.
```

## Rollout Notes

- Add `ODIN_HUGINN_DOM_FAST_LANE_DRIVER=/home/orchestrator/odin-os/scripts/drivers/huginn-dom-fast-lane.sh` only after fixture proof passes.
- Do not expose a public webhook in this slice.
- Do not point the driver at finance, social, or authenticated portal pages in this slice.
- Treat `blocked` as safe fallback, not runtime failure.

## Self-Review

- Every spec requirement maps to a task: contract, read-only recipe, domain/safety boundaries, blocked fallback, tool exposure, fixture proof, real Odin command proof.
- No webhook implementation is included because the spec defers webhooks until one recipe is proven.
- No mutation continuation is included because the spec routes mutation to a separate design.
- File paths and commands are exact.
- Tasks are independently reviewable and end in commits.
- No production implementation is included in this plan.

## Execution Handoff

Execution mode considered: How should this plan be implemented?

Recommended mode: Subagent-Driven.

Assumed mode: Subagent-Driven under noninteractive goal mode.

Rationale: The plan has five commit-sized tasks with disjoint enough scopes for task-by-task execution and review, while preserving the existing dirty worktree by requiring focused adds.

Do not start implementation until the execution skill confirms an isolated worktree or a safe dirty-worktree strategy.
