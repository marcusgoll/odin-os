# Huginn DOM Fast Lanes Design

Date: 2026-05-18

## Objective

Decide whether Huginn should create custom DOM-backed APIs, webhooks, or
adapter recipes for repetitive browser-viewing tasks so Odin can get data,
evidence, or task state faster than repeated manual browser inspection.

## Current State

Odin already owns the governed control plane. Huginn is the browser/evidence
lane, wired through JSON-over-stdin/stdout drivers and repo-local browser
helpers.

Existing reusable pieces:

- `docs/contracts/live-driver-tools.md` defines Huginn driver wiring.
- `scripts/browser/browser-access.sh` exposes browser start, navigation,
  snapshot, evaluate, selector typing, selector clicking, and screenshot
  helpers.
- `odin browser run` can produce public read-only live browser evidence when
  the live adapter is configured.
- High-risk browser mutation is approval-gated or unsupported unless a narrow
  explicit driver exists.
- Social evidence contracts already reject broad scraping, hidden network-call
  harvesting, and unofficial API replay.

The missing piece is a governed pattern for repeated read-only page work where
the browser has already loaded the page and the useful data can be extracted
from visible DOM, stable page state, or explicitly observed client-side data.

## Question Considered

Question: Which first adapter class should this design target?

Recommended answer: read-only extraction.

Assumed answer: read-only extraction under the active brainstorming goal.

Rationale: Read-only DOM extraction gives the speed benefit without crossing
into posting, submitting, buying, deleting, or account mutation. Mutation
continuation belongs in the separate Browser Mutation Continuation design.

## Approaches

### Approach A: Read-Only DOM Fast Lanes

Create named site/task recipes that navigate through the normal browser lane,
extract structured fields from the DOM or visible page state, record evidence,
and return typed JSON to Odin.

Pros:

- Fastest safe win for repeated browser-viewing tasks.
- Reuses current browser helpers and evidence storage.
- Avoids official API signup, external app review, and rate-key monitoring.
- Keeps Odin/Huginn roles intact.

Cons:

- Selectors can drift.
- Pages with heavy bot detection still need attended fallback.
- Each recipe needs maintenance.

Recommendation: use this as v1.

### Approach B: Browser-Derived Webhooks

Create repo-owned webhook endpoints that accept scheduled or operator-triggered
requests, run a named Huginn recipe, and post normalized results back into Odin
intake, review, or run artifacts.

Pros:

- Good for recurring checks and external triggers.
- Turns repetitive page checks into one operator-facing endpoint.
- Can integrate with existing n8n or Odin intake paths.

Cons:

- Adds exposed surface and auth requirements.
- Needs replay protection and request auditing.
- Should not be built before recipe contracts are stable.

Recommendation: defer until at least one DOM fast lane is proven.

### Approach C: Hidden Unofficial API Replay

Inspect browser network calls, replay private endpoints directly, and treat
them as custom APIs.

Pros:

- Potentially fastest path.
- Can skip rendering and UI waits when stable.

Cons:

- Highest policy, fragility, and detection risk.
- Can look like bot evasion or unauthorized API use.
- Often bypasses visible-page evidence and human inspection.

Recommendation: reject as default. Allow only case-by-case read-only research
when documented as an observed page dependency, not as a stealth API.

## Product Decision

Adopt **Huginn DOM Fast Lanes** as the product term.

Definition: a Huginn DOM Fast Lane is a named, read-only browser recipe that
loads an allowed page through the normal browser session, extracts typed data
from visible DOM or clearly observed page state, records evidence, and returns
structured JSON to Odin.

Fast lanes are allowed because they make repeated viewing faster. They are not
allowed to bypass approval, mutate external state, solve anti-bot challenges,
or replay private APIs as a stealth integration.

Approval source: active brainstorming goal.

Assumed approval: approved for design documentation and planning handoff.

## Boundaries

Allowed:

- Read visible text, links, tables, counters, metadata, form labels, and page
  status from allowed domains.
- Use DOM selectors, accessible names, ARIA roles, stable data attributes, and
  visible text matching.
- Use browser snapshot and screenshots as evidence.
- Use browser `evaluate` for bounded read-only extraction functions owned by
  the recipe.
- Use observed network responses only to explain what the visible page is
  showing or why extraction failed.
- Trigger Odin intake, review, or run-artifact creation with normalized
  read-only results.

Forbidden by default:

- Credential entry, CAPTCHA solving, MFA bypass, bot-detection bypass, or
  stealth evasion.
- Posting, submitting forms, buying, selling, deleting, liking, following,
  reposting, messaging, or changing account state.
- Broad profile scraping, crawling, or engagement farming.
- Replaying hidden/private API calls as the primary integration path.
- Storing cookies, tokens, passwords, or full sensitive page payloads in
  recipes, logs, or artifacts.

If a task needs mutation, it must go through approval-gated browser mutation
continuation or an existing narrow attended driver.

## Architecture

### Odin

Odin remains the source of truth for:

- task and goal state
- approval policy
- allowed tool catalog
- evidence and artifact persistence
- intake/review routing
- operator-facing status

Odin should expose fast lanes as existing `browser_*` or `/tool run` entries
before adding a new command family.

### Huginn Recipe

A recipe is a small site/task adapter with:

- `recipe_key`
- purpose and owning workflow
- allowed domains
- start URL shape
- required auth/session state
- extraction schema
- selector strategy
- evidence requirements
- drift detection rules
- fallback behavior

Recipes are code-owned, not free-form prompts. Operator input can provide URLs,
labels, date ranges, or IDs, but not arbitrary JavaScript.

### Browser Driver

The browser driver executes the recipe through current browser helpers:

1. Start or connect to the browser server.
2. Navigate to the allowed start URL.
3. Wait for visible readiness.
4. Capture pre-extraction snapshot and optional screenshot.
5. Run bounded read-only extraction.
6. Validate result schema and source URL.
7. Capture post-extraction evidence.
8. Return one JSON response on stdout.

### Output Contract

Each result should include:

```json
{
  "status": "completed",
  "tool_key": "browser_dom_fast_lane",
  "recipe_key": "example_status_table",
  "adapter_kind": "huginn_live",
  "source_url": "https://example.com/status",
  "final_url": "https://example.com/status",
  "extracted_at": "2026-05-18T00:00:00Z",
  "schema_version": 1,
  "data": {},
  "evidence": {
    "snapshot_excerpt": "...",
    "screenshot_path": "/path/to/browser.png",
    "selector_version": "2026-05-18"
  },
  "limitations": []
}
```

Blocked results should include `status=blocked` and an
`intervention_reason`, such as `login_required`, `mfa_required`,
`captcha_or_bot_check`, `selector_drift`, `domain_changed`,
`ambiguous_result`, or `unsupported_mutation`.

## Fallback Policy

Use the fast lane when:

- domain is allowed
- purpose is read-only
- session state is already valid or public
- extraction matches the expected schema
- evidence can be recorded

Fall back to slow careful browser operation when:

- login, MFA, CAPTCHA, or bot challenge appears
- page structure differs from the recipe
- extracted data is ambiguous
- the page asks for confirmation or mutation
- rate limiting or automation warning appears
- the task carries financial, legal, medical, employment, or account-risk
  consequences

The fallback is not a failure. It is the safe result for unstable pages.

## First Slice

Build one fixture-backed read-only DOM fast lane before any live site:

- local fixture page with table, status label, detail link, and changing text
- recipe extracts typed rows and page status
- driver records snapshot and screenshot evidence
- Odin stores result as a run artifact
- drift fixture proves selector failure becomes `blocked=selector_drift`

After fixture proof, choose one real low-risk repetitive viewing task, such as
public page status evidence or non-sensitive dashboard readback. Avoid finance,
social posting, and authenticated portal tasks for the first real proof.

## Safety Review

Any implementation must review:

- command allowlist changes
- domain allowlist enforcement
- arbitrary JavaScript prevention
- secret redaction
- screenshot/snapshot retention
- auth/session handling
- bot-detection and CAPTCHA fallback
- rate limiting and retry behavior
- evidence visibility in Odin overview/review/logs

## Verification

Minimum proof path:

```text
go test ./internal/tools ./internal/executors/drivers ./internal/app/lifecycle -run 'Browser|Driver|Tool' -count=1
go build -o ./bin/odin ./cmd/odin
tmp="$(mktemp -d)"
fixture_url="http://127.0.0.1:18080/status-fixture"
ODIN_ROOT="$tmp" ./bin/odin tool run browser_dom_fast_lane recipe_key=fixture_status url="$fixture_url" --json
ODIN_ROOT="$tmp" ./bin/odin logs --json
```

Expected proof:

- fixture fast lane returns typed JSON
- evidence artifact is persisted
- drift fixture returns `selector_drift`
- disallowed domain fails closed before navigation
- mutation-shaped recipe is rejected before driver execution

## Out Of Scope

- generic custom APIs for arbitrary sites
- hidden/private API replay as a default strategy
- CAPTCHA or bot-detection bypass
- autonomous posting or form submission
- LinkedIn automation
- broad X scraping or engagement automation
- storing credentials or session tokens
- replacing Odin as control plane

## Implementation Handoff

Create a PR-sized brownfield slice in `odin-os`.

Reuse:

- `docs/contracts/live-driver-tools.md`
- `scripts/browser/browser-access.sh`
- existing driver invocation patterns
- existing tool catalog and `/tool run` surface
- SQLite run artifacts and logs

Add only what is missing:

- one contract section for DOM fast lanes
- one fixture recipe and deterministic driver
- typed result schema
- schema/drift/domain tests
- one Odin operator proof command

Best operating rule: lean toward faster DOM-backed read-only recipes when they
are visible, typed, domain-scoped, evidenced, and easy to fall back from. Use
slow attended browser operation for anything unstable, auth-sensitive,
mutation-shaped, or blocked by anti-bot controls.
