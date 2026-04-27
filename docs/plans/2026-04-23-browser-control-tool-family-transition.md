# Browser Control Tool Family Transition Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `browser_*` the canonical Browser Control tool family on Odin's existing `/tool` surface while preserving `huginn_*` compatibility aliases and reusing the current driver/browser runtime.

**Domain Source of Truth:** `CONTEXT.md`, `docs/contracts/workspace-context-map.md`, `docs/contracts/live-driver-tools.md`, `docs/contracts/marcus-social-copilot.md`

**Context:** Workspace Integration / Browser Control

**Owns / Does Not Own:** This slice owns generic browser tool naming, catalog visibility, driver request-response normalization, and current operator documentation for `/tool`. It does not own a new browser aggregate, a `/browser` command family, workflow approval state, or social/finance business lifecycle terms.

**Invariants:**
- `/tool` remains the canonical Browser Control operator surface.
- `browser_*` is the canonical platform vocabulary; `huginn_*` survives only as compatibility language during transition.
- `Browser Intervention` remains `Run Attempt` evidence plus wake/evidence context, not a new aggregate.
- Ordinary browser or selector failures remain driver/runtime failures unless a workflow explicitly classifies them as a coarse `Browser Intervention Reason`.
- Existing browser helper scripts and `ODIN_HUGINN_*` env vars are reused in v1; this slice does not add a second driver registry.
- Workflow-owned social publish state such as `via=huginn_x` and `publish_mode=huginn_x` remains unchanged in this slice.

**Architecture:** Introduce canonical `browser_*` builtin tool definitions and hidden `huginn_*` aliases at the catalog layer, then teach the broker to hide aliases from `/tool list` while still accepting them for `/tool show` and `/tool run`. Reuse the existing web adapter and shell driver pipeline by changing default tool keys and user-facing summaries to `browser_*`, while keeping current driver file names and env vars as the adapter translation layer. Update current docs, shell callers, and tests so the live operator vocabulary becomes `browser_*` without changing workflow-owned business state.

**Tech Stack:** Go, Odin CLI REPL, builtin tool broker/catalog, shell driver scripts, existing browser-access shell library, Go unit tests, Go integration tests

---

## Context Mapping

- `Context:` Workspace Integration / Browser Control
- `Owns:` builtin browser tool definitions, compatibility alias behavior, canonical `/tool` browser examples, driver request-response tool-key normalization
- `Depends on:` existing `/tool` shell surface, builtin tool broker, web adapter driver env vars, browser shell library, workflow-owned `social_outcome` publish flow
- `Does not own:` `/memory publish` business semantics, `Approval Request`, `Run Attempt` lifecycle, finance browser governance, social strategy contracts
- `Boundary crossings:` `/tool` shell -> broker -> builtin catalog -> invocation service -> web adapters -> shell drivers; `/memory publish via=huginn_x` -> internal browser publish call; operator docs/runbooks -> canonical `/tool run browser_*` examples

## Current State

- `CONTEXT.md` now locks `Browser Control`, `Trusted Browser Session`, `Browser Intervention`, the closed intervention-reason vocabulary, `/tool` as the operator surface, and `browser_*` as the future canonical tool family.
- `internal/tools/catalog/builtin.go` still exposes browser tools as `huginn_pbs_session`, `huginn_visual_audit`, `huginn_x_post_visible_evidence`, `huginn_x_post_publish`, and `huginn_x_weekly_evidence_bundle`.
- `internal/tools/broker/broker.go` lists every builtin definition directly, so naive alias duplication would pollute `/tool list`.
- `internal/adapters/web/*.go` still default request tool keys to `huginn_*`.
- `scripts/drivers/huginn-*.sh` echo the inbound `tool_key`, but their default fallback keys and several invalid-request paths still hardcode `huginn_*`.
- `internal/cli/repl/shell.go` still calls `huginn_x_post_publish` internally from `/memory publish via=huginn_x`.
- Current contracts, runbooks, shell tests, catalog tests, adapter tests, and integration tests still use `huginn_*` as the visible tool vocabulary.

## What Already Exists

- Generic browser substrate in `scripts/browser/browser-access.sh`
- Existing `/tool` operator surface in `internal/cli/repl/shell.go` and `internal/cli/commands/help.go`
- Shared builtin tool definitions and structured results in `internal/tools/catalog`
- Shared tool lookup and invocation in `internal/tools/broker`
- Existing web adapter seam in `internal/adapters/web`
- Existing shell driver contract in `docs/contracts/live-driver-tools.md`
- Current real-tool tests in:
  - `internal/tools/catalog/builtin_test.go`
  - `internal/tools/broker/broker_test.go`
  - `internal/tools/invocation/service_test.go`
  - `internal/adapters/web/*_test.go`
  - `internal/cli/repl/shell_test.go`
  - `tests/integration/live_driver_scripts_test.go`
  - `tests/integration/social_workflow_test.go`

## Gaps

- No canonical `browser_*` keys exist yet on `/tool`.
- No hidden-alias behavior exists today, so `/tool list` would duplicate tools if aliases were added naively.
- Several direct callers, driver defaults, summaries, and tests still assert `huginn_*` as the user-visible tool family.
- Current docs and runbooks still teach `huginn_*` as the browser operator vocabulary.

## Reuse Plan

- Reuse the current `/tool` surface instead of introducing `/browser`.
- Reuse the current driver env vars and shell scripts instead of creating a second browser driver layer.
- Reuse the current browser helper library and existing structured-result model.
- Reuse the current workflow-owned `via=huginn_x` publish seam and keep it adapter-branded for now.
- Introduce only one new structural concept in code: alias-aware builtin tool metadata sufficient to keep `browser_*` canonical and `huginn_*` compatible.

## New Additions

- Alias-aware builtin tool metadata, likely as small fields on `catalog.ToolDefinition`
- Canonical `browser_*` builtin tool entries for the current browser tools
- Hidden `huginn_*` alias entries or alias resolution built from the canonical definitions
- Updated current-doc examples and tests that prove canonical `browser_*` behavior

## Why New Additions Are Necessary

- Without alias-aware catalog behavior, Odin cannot make `browser_*` canonical without either breaking existing callers or showing duplicate browser families in `/tool list`.
- Without driver/tool-key normalization, direct service and driver tests will fail closed when canonical `browser_*` keys reach scripts that still default to `huginn_*`.
- Without doc and shell-test updates, the repo will keep teaching the old implementation vocabulary after the domain contract was explicitly changed.

## Real odin E2E Verification

- Safe readiness baseline already exists and should be rerun after implementation:
  - `./bin/odin healthcheck`
  - `./bin/odin doctor --json`
- Canonical operator-surface proof after implementation must use `browser_*` through the real shell:
  - `/tool show browser_visual_audit`
  - `/tool show huginn_visual_audit`
  - `/tool run browser_visual_audit ...`
  - `/tool run huginn_visual_audit ...`
- The alias proof is only complete if the legacy command still succeeds while rendering canonical `browser_*` output.

## Remaining Risks

- Historic plan documents will still mention `huginn_*`; this plan updates current source-of-truth docs and current operator runbooks only.
- `via=huginn_x` remains intentionally unchanged in this slice, so the repo will still contain some adapter-branded workflow language by design.
- Driver env vars remain `ODIN_HUGINN_*` in v1, which is acceptable only if the docs clearly describe them as implementation wiring rather than platform vocabulary.

## Best operating rule going forward

Make the `/tool` catalog speak the generic Browser Control language first, keep adapter-specific names behind compatibility layers, and refuse any design that adds a second command family or a second browser lifecycle store.

## Implementation Tasks

### Task 1: Add Canonical `browser_*` Tool Definitions And Hidden `huginn_*` Aliases

**Domain Goal:** Make `browser_*` the canonical Browser Control vocabulary on `/tool` without breaking existing `huginn_*` callers.

**Domain Rules Enforced:**
- `/tool` remains the only Browser Control operator surface.
- `browser_*` is canonical platform vocabulary; `huginn_*` is compatibility-only transition language.

**Why this matters:**
- The domain model is already locked. The catalog must stop teaching implementation names as if they were the platform.

**Files:**
- Modify: `internal/tools/catalog/types.go`
- Modify: `internal/tools/catalog/builtin.go`
- Test: `internal/tools/catalog/types_test.go`
- Test: `internal/tools/catalog/builtin_test.go`

**Step 1: Write the failing test**

```go
func TestBuiltinDefinitionsExposeCanonicalBrowserToolsAndHiddenHuginnAliases(t *testing.T) {
	definitions := BuiltinDefinitions()

	visual, ok := definitions["browser_visual_audit"]
	if !ok {
		t.Fatal("missing browser_visual_audit definition")
	}
	if visual.Hidden {
		t.Fatal("browser_visual_audit should be visible")
	}
	if visual.CanonicalKey != "browser_visual_audit" {
		t.Fatalf("CanonicalKey = %q, want browser_visual_audit", visual.CanonicalKey)
	}

	alias, ok := definitions["huginn_visual_audit"]
	if !ok {
		t.Fatal("missing huginn_visual_audit alias")
	}
	if !alias.Hidden {
		t.Fatal("huginn_visual_audit alias should be hidden from catalog listings")
	}
	if alias.CanonicalKey != "browser_visual_audit" {
		t.Fatalf("CanonicalKey = %q, want browser_visual_audit", alias.CanonicalKey)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/catalog -run 'TestBuiltinDefinitionsExposeCanonicalBrowserToolsAndHiddenHuginnAliases' -count=1`

Expected: FAIL because the catalog has no alias-aware metadata and no `browser_*` definitions yet.

**Step 3: Write minimal implementation**

```go
type ToolDefinition struct {
	Key          string
	CanonicalKey string
	Aliases      []string
	Hidden       bool
	// existing fields...
}

func BuiltinDefinitions() map[string]ToolDefinition {
	definitions := []ToolDefinition{
		{
			Key:          "browser_visual_audit",
			CanonicalKey: "browser_visual_audit",
			Aliases:      []string{"huginn_visual_audit"},
			// existing metadata...
		},
	}

	index := make(map[string]ToolDefinition, len(definitions)*2)
	for _, definition := range definitions {
		index[definition.Key] = definition
		for _, alias := range definition.Aliases {
			aliasDef := definition
			aliasDef.Key = alias
			aliasDef.Hidden = true
			index[alias] = aliasDef
		}
	}
	return index
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/catalog -run 'TestBuiltinDefinitionsExposeCanonicalBrowserToolsAndHiddenHuginnAliases|TestToolDefinitionCardIsThin' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/catalog/types.go internal/tools/catalog/builtin.go internal/tools/catalog/types_test.go internal/tools/catalog/builtin_test.go
git commit -m "feat: add canonical browser tool definitions"
```

### Task 2: Keep `/tool list`, `/tool show`, And `/tool run` Canonical While Accepting Legacy Aliases

**Domain Goal:** Accept old `huginn_*` commands without letting them remain visible as parallel platform vocabulary.

**Domain Rules Enforced:**
- `/tool list` must show one canonical Browser Control family, not duplicate alias families.
- Alias invocation must resolve to canonical `browser_*` output.

**Why this matters:**
- Compatibility without canonical readback still leaves the platform speaking the wrong language.

**Files:**
- Modify: `internal/tools/broker/broker.go`
- Test: `internal/tools/broker/broker_test.go`
- Test: `internal/cli/repl/shell_test.go`

**Step 1: Write the failing test**

```go
func TestCatalogSkipsHiddenAliasTools(t *testing.T) {
	broker := New(testSnapshot(), catalog.BuiltinDefinitions(), testLimits())

	cards := broker.Catalog("global")
	for _, card := range cards {
		if card.Key == "huginn_visual_audit" {
			t.Fatalf("Catalog() exposed hidden alias card: %+v", card)
		}
	}
}

func TestShellToolRunLegacyBrowserAliasRendersCanonicalKey(t *testing.T) {
	// Arrange a fixture ODIN_HUGINN_VISUAL_DRIVER that returns browser_visual_audit.
	// Run: /tool run huginn_visual_audit target_url=https://example.com/dashboard label=cfipros-dashboard
	// Expect output to contain "tool=browser_visual_audit".
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/broker ./internal/cli/repl -run 'TestCatalogSkipsHiddenAliasTools|TestShellToolRunLegacyBrowserAliasRendersCanonicalKey' -count=1`

Expected: FAIL because the broker currently lists every builtin definition and does not canonicalize alias expansion or invocation output.

**Step 3: Write minimal implementation**

```go
func (broker *Broker) Catalog(scope string) []catalog.Card {
	for _, definition := range broker.builtins {
		if definition.Hidden {
			continue
		}
		if catalog.MatchesScope(definition.Scopes, scope) {
			cards = append(cards, definition.Card())
		}
	}
	return cards
}

func (broker *Broker) canonicalDefinition(key string) (catalog.ToolDefinition, bool) {
	definition, ok := broker.builtins[key]
	if !ok {
		return catalog.ToolDefinition{}, false
	}
	if definition.CanonicalKey != "" && definition.CanonicalKey != definition.Key {
		if canonical, ok := broker.builtins[definition.CanonicalKey]; ok {
			return canonical, true
		}
	}
	return definition, true
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/broker ./internal/cli/repl -run 'TestCatalogSkipsHiddenAliasTools|TestShellToolRunLegacyBrowserAliasRendersCanonicalKey|TestShellToolRunInvokesLiveVisualAuditTool|TestShellToolRunInvokesLiveVisualAuditToolInGlobalScope' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/broker/broker.go internal/tools/broker/broker_test.go internal/cli/repl/shell_test.go
git commit -m "feat: canonicalize browser tool aliases in broker"
```

### Task 3: Normalize Browser Tool Keys Through Adapters And Driver Scripts

**Domain Goal:** Make the actual driver contract speak `browser_*` by default while keeping the existing driver files and env vars as the reused adapter substrate.

**Domain Rules Enforced:**
- Existing shell driver files and `ODIN_HUGINN_*` env vars remain the v1 implementation layer.
- Canonical Browser Control vocabulary must appear in request defaults, result tool keys, and user-facing summaries.

**Why this matters:**
- The catalog change alone is not enough if direct callers, driver defaults, or script fallbacks still emit `huginn_*`.

**Files:**
- Modify: `internal/adapters/web/huginn_driver.go`
- Modify: `internal/adapters/web/visual_driver.go`
- Modify: `internal/adapters/web/x_post_driver.go`
- Modify: `internal/adapters/web/x_publish_driver.go`
- Modify: `scripts/drivers/huginn-pbs-session.sh`
- Modify: `scripts/drivers/huginn-visual-audit.sh`
- Modify: `scripts/drivers/huginn-x-post-evidence.sh`
- Modify: `scripts/drivers/huginn-x-post-publish.sh`
- Test: `internal/adapters/web/huginn_driver_test.go`
- Test: `internal/adapters/web/visual_driver_test.go`
- Test: `internal/adapters/web/x_post_driver_test.go`
- Test: `internal/adapters/web/x_publish_driver_test.go`
- Test: `internal/tools/invocation/service_test.go`
- Test: `tests/integration/live_driver_scripts_test.go`

**Step 1: Write the failing test**

```go
func TestVisualDriverDefaultsToCanonicalBrowserToolKey(t *testing.T) {
	driver := NewVisualDriver()
	response, err := driver.Invoke(context.Background(), VisualRequest{
		Input: VisualInput{TargetURL: "https://example.com/dashboard"},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.ToolKey != "browser_visual_audit" {
		t.Fatalf("ToolKey = %q, want browser_visual_audit", response.ToolKey)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/adapters/web ./internal/tools/invocation ./tests/integration -run 'TestVisualDriverDefaultsToCanonicalBrowserToolKey|TestServiceExposesRealHuginnVisualAuditResults|TestHuginnVisualAuditDriverScriptPrefersTrustedSessionWhenHeaded' -count=1`

Expected: FAIL because the adapters and scripts still default to `huginn_*` and fixture responses still assert the old keys and summaries.

**Step 3: Write minimal implementation**

```go
const visualToolKey = "browser_visual_audit"
const xPostToolKey = "browser_x_post_visible_evidence"
const xPublishToolKey = "browser_x_post_publish"
const defaultToolKey = "browser_pbs_session"
```

```bash
tool_key = str(request.get("tool_key") or "browser_visual_audit").strip() or "browser_visual_audit"
```

Update the current driver summaries from `Huginn ...` to `Browser ...` or `Trusted browser session ...` so the real operator readback matches the locked domain vocabulary.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/adapters/web ./internal/tools/invocation ./tests/integration -run 'TestDriverInvokesConfiguredCommandAndDecodesStructuredJSON|TestVisualDriverDefaultsToCanonicalBrowserToolKey|TestServiceExposesRealHuginnVisualAuditResults|TestHuginnPBSSessionDriverScriptValidatesReadySession|TestHuginnVisualAuditDriverScriptPrefersTrustedSessionWhenHeaded|TestHuginnXPostPublishDriverScriptPublishesApprovedPost|TestHuginnXPostEvidenceDriverScriptPrefersTrustedSessionWhenHeaded' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/adapters/web/huginn_driver.go internal/adapters/web/visual_driver.go internal/adapters/web/x_post_driver.go internal/adapters/web/x_publish_driver.go internal/adapters/web/huginn_driver_test.go internal/adapters/web/visual_driver_test.go internal/adapters/web/x_post_driver_test.go internal/adapters/web/x_publish_driver_test.go internal/tools/invocation/service_test.go scripts/drivers/huginn-pbs-session.sh scripts/drivers/huginn-visual-audit.sh scripts/drivers/huginn-x-post-evidence.sh scripts/drivers/huginn-x-post-publish.sh tests/integration/live_driver_scripts_test.go
git commit -m "refactor: canonicalize browser driver tool keys"
```

### Task 4: Update Workflow-Owned Callers And Current Docs To Use Canonical `browser_*`

**Domain Goal:** Switch current operator-facing examples and current workflow-owned browser calls to canonical `browser_*` language while preserving workflow-owned adapter fields like `via=huginn_x`.

**Domain Rules Enforced:**
- Generic Browser Control tool keys become the operator vocabulary.
- Workflow-owned `via=huginn_x` stays unchanged in this slice.
- No `/browser` command family is introduced.

**Why this matters:**
- If current docs and workflow callers keep using `huginn_*`, the codebase will continue teaching the wrong platform contract.

**Files:**
- Modify: `internal/cli/repl/shell.go`
- Test: `internal/cli/repl/shell_test.go`
- Modify: `docs/contracts/live-driver-tools.md`
- Modify: `docs/contracts/marcus-social-copilot.md`
- Modify: `docs/operations/marcus-live-x-post-runbook.md`
- Modify: `CONTEXT.md`

**Step 1: Write the failing test**

```go
func TestShellMemoryPublishViaHuginnXMarksApprovedOutcomePublished(t *testing.T) {
	// Arrange a fixture ODIN_HUGINN_X_PUBLISH_DRIVER that records the request body.
	// After /memory publish <id> via=huginn_x, assert the stored request JSON contains:
	//   "tool_key":"browser_x_post_publish"
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/repl -run 'TestShellMemoryPublishViaHuginnXMarksApprovedOutcomePublished|TestPublishApprovedXOutcomeWithHuginnStripsApprovalChecklistFromPostText' -count=1`

Expected: FAIL because `publishApprovedXOutcomeWithHuginn` still sends `huginn_x_post_publish`.

**Step 3: Write minimal implementation**

```go
result, err := invocation.Service{}.HuginnXPostPublish(ctx, webdriver.XPublishRequest{
	ToolKey: "browser_x_post_publish",
	Input: webdriver.XPublishInput{
		PostText: approvedOutcomePublishText(summary.Summary),
		Label:    fmt.Sprintf("social-outcome-%d", summary.ID),
		WaitMS:   "4000",
		Headless: "false",
	},
})
```

Update current docs and current `CONTEXT.md` examples from:

```text
/tool run huginn_visual_audit ...
/tool run huginn_x_post_visible_evidence ...
```

to:

```text
/tool run browser_visual_audit ...
/tool run browser_x_post_visible_evidence ...
```

and explicitly note that `huginn_*` remains accepted only as a compatibility alias during transition.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/repl -run 'TestShellMemoryPublishViaHuginnXMarksApprovedOutcomePublished|TestPublishApprovedXOutcomeWithHuginnStripsApprovalChecklistFromPostText|TestShellToolRunRecordsWorkflowScopedXVisibleEvidence|TestShellToolRunRecordsWorkflowScopedXWeeklyEvidenceBundle' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/repl/shell.go internal/cli/repl/shell_test.go docs/contracts/live-driver-tools.md docs/contracts/marcus-social-copilot.md docs/operations/marcus-live-x-post-runbook.md CONTEXT.md
git commit -m "docs: switch browser tool examples to canonical keys"
```

### Task 5: Convert Current End-To-End Tests And Prove The Real Odin `/tool` Path

**Domain Goal:** Prove the real operator shell now speaks canonical `browser_*` while legacy aliases still work.

**Domain Rules Enforced:**
- Real proof must go through `./bin/odin` and `/tool`, not just unit tests.
- Alias compatibility is only valid if canonical readback still wins.

**Why this matters:**
- This is the final check that the repo-wide vocabulary changed without breaking current operator flows.

**Files:**
- Modify: `tests/integration/social_workflow_test.go`
- Test: `internal/cli/repl/shell_test.go`
- Test: `tests/integration/social_workflow_test.go`

**Step 1: Write the failing test**

```go
func TestMarcusSocialXVisibleEvidenceCLIIntegration(t *testing.T) {
	// Switch the command under test from:
	//   /tool run huginn_x_post_visible_evidence ...
	// to:
	//   /tool run browser_x_post_visible_evidence ...
	// and expect the output to contain:
	//   tool=browser_x_post_visible_evidence
}
```

Add one compatibility test in `internal/cli/repl/shell_test.go` that still runs:

```text
/tool run huginn_x_post_visible_evidence ...
```

but expects canonical readback:

```text
tool=browser_x_post_visible_evidence
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/repl ./tests/integration -run 'TestMarcusSocialNativeXPublishCLIIntegration|TestMarcusSocialXVisibleEvidenceCLIIntegration|TestMarcusSocialAnalyticsIncludesXVisibleEvidenceCLIIntegration|TestShellToolRunLegacyBrowserAliasRendersCanonicalKey' -count=1`

Expected: FAIL until the shell tests, integration fixtures, and browser tool results all line up on `browser_*`.

**Step 3: Write minimal implementation**

```go
if err := shell.HandleLine(ctx, "/tool run browser_x_post_visible_evidence target_url=https://x.com/marcus/status/123 label=marcus-crosswind", &output); err != nil {
	t.Fatalf("HandleLine(/tool run browser_x_post_visible_evidence) error = %v", err)
}
if !strings.Contains(output.String(), "tool=browser_x_post_visible_evidence") {
	t.Fatalf("output = %q, want canonical browser tool readback", output.String())
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/catalog ./internal/tools/broker ./internal/adapters/web ./internal/tools/invocation ./internal/cli/repl ./tests/integration -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/repl/shell_test.go tests/integration/social_workflow_test.go
git commit -m "test: prove canonical browser tool surface"
```

## Real Odin Verification Commands

Run these after Task 5 from `/home/orchestrator/odin-os`:

1. Build the binary:

```bash
go build -o ./bin/odin ./cmd/odin
```

Expected: build succeeds.

2. Prove runtime readiness:

```bash
./bin/odin healthcheck
./bin/odin doctor --json
```

Expected: healthcheck exits successfully and `doctor --json` reports a ready runtime.

3. Prove canonical browser tool discovery through the real shell:

```bash
printf '/scope global\n/tool show browser_visual_audit\n/tool show huginn_visual_audit\nexit\n' | ./bin/odin
```

Expected:
- both commands succeed
- the rendered tool detail reports `tool=browser_visual_audit`
- no `/browser` command family appears

4. Prove canonical `/tool run browser_*` with a deterministic fixture driver:

```bash
tmpdir="$(mktemp -d)"
cat >"${tmpdir}/browser-visual-driver.sh" <<'EOF'
#!/usr/bin/env bash
cat >/dev/null
printf '{"status":"completed","tool_key":"browser_visual_audit","summary":"Captured browser visual audit evidence for browser-audit-smoke.","artifacts":{"target_url":"https://example.com/dashboard","final_url":"https://example.com/dashboard","title":"Dashboard","label":"browser-audit-smoke","screenshot_path":"/tmp/browser-audit-smoke.png","snapshot_excerpt":"Revenue MRR Pipeline","wait_ms":"2000","launch_mode":"--headless"}}'
EOF
chmod +x "${tmpdir}/browser-visual-driver.sh"
ODIN_HUGINN_VISUAL_DRIVER="${tmpdir}/browser-visual-driver.sh" \
printf '/scope global\n/tool run browser_visual_audit target_url=https://example.com/dashboard label=browser-audit-smoke\nexit\n' | ./bin/odin
```

Expected:
- the shell accepts `browser_visual_audit`
- output contains `tool=browser_visual_audit`
- output contains `artifact screenshot_path=/tmp/browser-audit-smoke.png`

5. Prove legacy alias compatibility still returns canonical output:

```bash
ODIN_HUGINN_VISUAL_DRIVER="${tmpdir}/browser-visual-driver.sh" \
printf '/scope global\n/tool run huginn_visual_audit target_url=https://example.com/dashboard label=browser-audit-alias\nexit\n' | ./bin/odin
```

Expected:
- the alias command still succeeds
- output still reports `tool=browser_visual_audit`

## Domain Invariant Coverage

- `browser_*` is canonical on `/tool`
  - Covered by: `internal/tools/catalog/builtin_test.go`, `internal/tools/broker/broker_test.go`, `internal/cli/repl/shell_test.go`
- `huginn_*` is compatibility-only and hidden from list output
  - Covered by: `internal/tools/broker/broker_test.go`
- Existing driver/env-var substrate is reused instead of creating a second browser path
  - Covered by: `internal/adapters/web/*_test.go`, `tests/integration/live_driver_scripts_test.go`
- Workflow-owned `via=huginn_x` remains intact while the underlying browser tool key becomes canonical
  - Covered by: `internal/cli/repl/shell_test.go`, `tests/integration/social_workflow_test.go`
- `/tool`, not `/browser`, remains the canonical operator surface
  - Covered by: real `./bin/odin` verification above and current-doc updates

## Review Checklist

- domain naming matches `CONTEXT.md`
- invariant coverage exists and is called out by test path
- ADR constraints are honored
- boundary crossings are explicit and justified
- reused repo structures are named
- any unresolved domain gaps are listed as blockers, not hidden in implementation tasks
