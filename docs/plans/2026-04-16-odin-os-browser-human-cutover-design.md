# Odin OS Browser-Human Cutover Design

## Objective

Add an `odin-os`-owned browser/Huginn capability that can drive real browser workflows like a human would, without relying on `odin-orchestrator-main` shell libraries at runtime. The first concrete workflow on top of that capability is the Plaid Transfer application flow for `family-ops`.

## Current Context

The live state on April 16, 2026 is split across three lines:

- `odin-os.service` is the active runtime on the host and should remain the canonical controller.
- `odin-os` currently exposes only one Huginn/browser capability, `huginn_pbs_session`, and it is PBS-specific.
- The current driver contract still points back to legacy browser code. In the newer `odin-os` line, `scripts/drivers/huginn-pbs-session.sh` sources `scripts/odin/lib/browser-access.sh` from `odin-orchestrator`.

That means using "Huginn like a human would" today would still route through legacy browser code even if the surrounding control plane is `odin-os`.

Separately, the live Plaid probe showed:

- both Robinhood Items have `auth` consent
- Luke and Claire checking/savings accounts return ACH details from `/auth/get`
- Plaid Transfer is not enabled for the current Plaid account yet

So the browser problem and the Plaid product-access problem are related, but different. We need the `odin-os` browser lane first, then the Plaid application workflow on top of it.

## Goals

- Make browser/Huginn execution an `odin-os`-owned capability rather than a legacy shell dependency.
- Keep the capability auditable through `odin-os` tool invocation, runtime events, and structured artifacts.
- Support "browser like a human" workflows where the browser session can:
  - launch Chromium
  - navigate
  - snapshot
  - click
  - type
  - save/load session cookies
  - capture screenshots
- Add a first real workflow, `plaid_transfer_application`, that can open Plaid Dashboard, detect state, and advance the Transfer application flow until a real human approval/MFA/review wall is hit.

## Non-Goals

- No hidden fallback to `odin-orchestrator-main` browser scripts after this cutover.
- No Firefox or WebKit support in this phase.
- No automatic approval of external financial transfers in this phase.
- No attempt to force Plaid underwriting approval after the application is submitted.
- No generalized high-level autonomous web loops beyond bounded browser workflows.

## Approaches Considered

### 1. Thin wrapper over the legacy browser stack

Add new `odin-os` tool cards, but keep sourcing `odin-orchestrator-main/scripts/odin/lib/browser-access.sh`.

Pros:

- fast
- minimal code movement

Cons:

- violates the requirement that `odin-os` must own the browser lane
- keeps runtime behavior coupled to a legacy repo
- makes future `odin-os` browser workflows harder to reason about and test

### 2. Odin-os-native browser runtime with bounded workflow drivers

Recommended.

Port the required browser/Huginn runtime into `odin-os`, expose it through `odin-os` driver contracts and tool definitions, and layer specific browser workflows like Plaid Transfer setup on top.

Pros:

- honest `odin-os` ownership
- auditable through the existing tool invocation pattern
- reusable for future browser workflows
- supports human-like interaction without exposing raw ad hoc browser mutation directly to every task

Cons:

- more initial work than a thin wrapper
- requires explicit runtime migration and smoke coverage

### 3. Plaid-only one-off script

Add only a Plaid Transfer browser script with no generic browser capability.

Pros:

- smallest surface for the immediate goal

Cons:

- throws away reuse
- reintroduces one-off workflow scripting
- does not actually solve the broader `odin-os` browser ownership problem

## Recommended Design

Use an `odin-os`-native browser runtime plus bounded workflow drivers.

### Repo Ownership

Move the browser runtime assets that are actually needed into `odin-os`, under repo-local paths such as:

- `scripts/browser/browser-access.sh`
- `scripts/browser/odin-huginn-server.js`
- `scripts/browser/huginn-captcha.js`

The important rule is: after this phase, no `odin-os` browser workflow should source or import code from `odin-orchestrator-main` at runtime.

This is a copy-and-own design, not a live shared-library design.

### Capability Model

Add two new `odin-os` browser tool lanes:

1. `huginn_browser_session`
   Used for bounded session checks, preflight, and generic browser state inspection.

2. `plaid_transfer_application`
   Used for the specific Plaid Dashboard workflow needed to enable Transfer for `family-ops`.

These tools should use the existing JSON-over-stdin/stdout driver contract pattern already used for live drivers, rather than inventing a new RPC layer.

### Driver Boundary

Add a generic browser driver adapter under `odin-os`, separate from the current PBS-only web adapter shape. It should:

- accept a generic tool key plus structured input
- invoke a configured repo-local driver command
- decode one structured JSON result
- fail closed if the driver is not configured, returns mismatched keys, or returns incomplete status

The runtime should depend on this contract, not on shell function names directly.

### Runtime State

Browser runtime state must live under `odin-os`, not under legacy `/var/odin` assumptions.

That includes:

- auth token files
- browser port files
- screenshot artifacts
- session cookie vaults
- structured logs

The phase should standardize Chromium as the only supported engine. The live host already proved Chromium works while Firefox does not.

### Plaid Transfer Workflow

`plaid_transfer_application` should be a bounded browser workflow, not a freeform macro recorder.

Expected states:

- `ready_for_login`
- `ready_for_application`
- `blocked_on_mfa`
- `blocked_on_captcha`
- `blocked_on_missing_business_info`
- `submitted_for_review`
- `already_enabled`
- `failed`

Workflow behavior:

- launch Chromium
- load saved Plaid session if present
- navigate to Plaid Dashboard / Transfer application
- inspect page state
- click/type through known steps when the state is unambiguous
- emit screenshots and structured artifacts at each meaningful stage
- stop and return a bounded blocked state if human help is required

This gives us "browser like a human would" behavior without pretending the browser can force Plaid approval.

### Safety Rules

- Browser workflows remain bounded tools, not unconstrained autonomous browsing.
- Unknown tool keys fail closed.
- Browser workflows must not mutate git worktrees or task branches.
- Browser artifacts must be recorded in `odin-os` structured output.
- Screenshots and snapshots should be available for operator review.
- The Plaid workflow should not submit sensitive values from ambient env unless those values are explicitly mapped in the workflow input or a repo-local config contract.

### Operator Contract

Operators should be able to:

- run a browser preflight
- inspect current session health
- run the Plaid Transfer application workflow
- see whether the workflow is blocked on login/MFA/review
- resume later from the last saved session

### Why This Solves The Current Problem

This design separates the real blockers correctly:

- `odin-os` browser ownership becomes a real engineering cutover, not an illusion
- Plaid Transfer enablement becomes a bounded workflow that can help with setup
- Plaid underwriting or product review remains an external dependency and is surfaced honestly as such

## Testing Strategy

### Unit Tests

- generic browser driver request/response validation
- tool catalog registration for `huginn_browser_session` and `plaid_transfer_application`
- fail-closed behavior when the driver command is missing or mismatched

### Integration Tests

- fake driver invocation for the generic browser tool
- fake driver invocation for Plaid workflow states
- proof that driver scripts resolve only repo-local browser libraries, not legacy repo paths

### Host Smoke

- Chromium-only preflight on the live host
- one real browser session smoke against `https://example.com`
- one bounded Plaid dashboard smoke that proves session launch, navigation, screenshot, and state capture

## Success Criteria

This phase is complete when:

- `odin-os` can run a browser workflow without importing runtime code from `odin-orchestrator-main`
- `odin-os` has a Chromium-only browser preflight and session tool
- `odin-os` has a `plaid_transfer_application` workflow that can advance through the Plaid dashboard until blocked by real external review/MFA
- all results are emitted as structured `odin-os` tool artifacts and logs
- legacy browser code is no longer on the runtime path for `odin-os` browser execution
