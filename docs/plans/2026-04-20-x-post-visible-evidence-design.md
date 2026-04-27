# X Post Visible Evidence Design

## Problem

The Marcus social workflow in `odin-os` can already produce:

- a 7-day retrospective
- a 4-window comparison
- prompt-only carry-forward guidance

But all of that is still driven by Odin-side social memory, not by browser-captured evidence from a real X page.

The next useful slice is not full platform analytics import. It is a narrow, read-only evidence path that lets Odin use Huginn like a human operator would:

- open an explicit X post URL
- observe the visible page
- capture a screenshot
- extract only what is visibly rendered on the page
- store that evidence in Odin memory
- surface it in the Marcus analytics path

This must stay read-only, explicit-URL-based, and visible-page-only. It must not depend on unofficial API replay, hidden endpoint harvesting, or stealth browser behavior.

## Existing Repo State

### Already real

- `odin-os` already has a generic `/tool run` surface.
- `odin-os` already has a Huginn visual driver path through:
  - `internal/adapters/web/visual_driver.go`
  - `internal/tools/invocation/service.go`
  - `internal/tools/catalog/builtin.go`
  - `scripts/drivers/huginn-visual-audit.sh`
- The browser stack already supports:
  - navigation
  - health checks
  - screenshots
  - visible-page snapshots
  - page-side `evaluate` through the existing Huginn server
- The Marcus social workflow already has workflow-scoped memory and an analytics prompt path.

### Partial

- The generic visual audit tool can capture a screenshot and excerpt, but it has no X-specific extraction contract.
- The current `/tool run` flow can invoke tools, but it does not yet record social evidence from tool results back into memory.
- The Marcus analytics prompt can consume social memory, but it does not yet include browser-captured evidence.

### Missing

- a dedicated X post evidence tool
- a visible-page extraction contract for X post URLs
- a generic tool-to-memory recording hook
- analytics prompt support for recent browser-captured evidence

## Approaches

### Option 1: Overload `huginn_visual_audit` for X extraction

Pros:

- fewer top-level tool keys

Cons:

- muddies the generic visual-audit contract
- mixes screenshot-only review with X-specific semantics

Verdict: reject.

### Option 2: Add a dedicated X post evidence tool on top of the existing Huginn server

Pros:

- clean contract
- reuses existing Huginn/browser infrastructure
- keeps X-specific extraction logic out of the generic visual tool

Cons:

- adds one new driver, adapter, and builtin tool

Verdict: recommended.

### Option 3: Skip tooling and add a social-specific CLI command

Example: `/social capture-x-post ...`

Pros:

- explicit operator UX

Cons:

- duplicates the generic tool surface
- violates the repo’s reuse direction

Verdict: reject.

## Selected Design

### 1. Add one dedicated read-only tool: `huginn_x_post_visible_evidence`

The tool should accept:

- `target_url` required
- optional `label`
- optional `wait_ms`
- optional `headless`
- optional `screenshot_path`

The tool should only accept explicit X post URLs on:

- `x.com`
- `www.x.com`
- `twitter.com`
- `www.twitter.com`

It should reject non-X targets.

### 2. Implement a dedicated Huginn driver for X post evidence

Add a dedicated driver script rather than overloading the visual-audit script.

The driver should:

- start the existing Huginn browser server
- navigate to the explicit X post URL
- wait briefly for the page to render
- capture:
  - final URL
  - title
  - screenshot path
  - visible-page snapshot text
  - visible post text and visible metrics if extractable from rendered DOM

This should use only:

- the existing screenshot path
- `browser_snapshot`
- the existing `/evaluate` endpoint

No network interception or unofficial API replay is allowed.

### 3. Store full visible snapshot text in a file artifact

The first slice should not rely only on a truncated excerpt.

The driver should write the full visible snapshot text to a file under the Odin browser-state directory and return:

- `snapshot_path`
- `snapshot_excerpt`

This makes the evidence auditable and gives the extraction layer a stable local artifact.

### 4. Extract only visible X post evidence

The driver should attempt best-effort extraction of:

- visible post text
- visible author display name
- visible handle
- visible reply count
- visible repost count
- visible like count
- visible bookmark count
- visible view count
- visible post timestamp if extractable

All of these should be optional. Missing values must not fail the tool if the screenshot and page capture succeeded.

### 5. Add generic tool-result memory recording

Do not add a new social CLI command.

Instead, extend the existing tool-result structure so a tool may optionally request memory recording. Then the shell can persist memory after a successful `/tool run`.

For this slice, `huginn_x_post_visible_evidence` should auto-record workflow-scoped memory as:

- memory type: `social_evidence`
- fields including:
  - `channel=x`
  - `evidence_kind=x_post_visible`
  - `target_url`
  - `final_url`
  - visible metrics that were extracted
  - `screenshot_path`
  - `snapshot_path`

### 6. Add a recent evidence section to the Marcus analytics prompt

The first slice should surface recent browser-captured evidence in the analytics prompt without trying to compare it across multiple weeks yet.

Add a new section to the retrospective context:

- `Recent X Visible Evidence:`

This should list recent `social_evidence` entries from the current workflow scope.

### 7. Keep the slice explicitly compliant

This slice must not:

- automate likes, replies, or posting
- use unofficial API calls
- intercept private network traffic
- use stealth automation or evasive browser behavior
- add LinkedIn browser capture

LinkedIn remains manual for now.

## Prompt Shape

The Marcus analytics prompt should gain a new section such as:

- `Recent X Visible Evidence:`
- `- [x x_post_visible] <summary>`

The rest of the retrospective/comparison/carry-forward path remains intact.

## Testing Strategy

Add tests for:

- dedicated X post driver request/response decoding
- invocation service wiring for the new driver
- builtin tool wiring for `huginn_x_post_visible_evidence`
- `/tool run huginn_x_post_visible_evidence ...` recording workflow-scoped `social_evidence`
- analytics prompt inclusion of recent `social_evidence`
- rejection of non-X target URLs

Real `odin` verification should prove:

- the real `odin` shell can run the new tool
- the tool returns screenshot and evidence artifacts
- the tool auto-records workflow-scoped `social_evidence`
- the Marcus analytics prompt includes recent visible evidence

## Non-Goals

- LinkedIn browser capture
- unofficial API call harvesting
- network interception
- automated publishing or engagement
- full analytics import
- long-horizon evidence comparison

## Acceptance Criteria

- `odin-os` provides a dedicated read-only `huginn_x_post_visible_evidence` tool for explicit X post URLs
- the tool stores workflow-scoped `social_evidence`
- the Marcus analytics prompt includes recent visible evidence
- the slice is proven through the real `odin` shell
- the implementation remains read-only and visible-page-only
