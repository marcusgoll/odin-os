# X Native Post Publish Design

## Current State

`odin-os` already supports the full pre-post Marcus social loop:

- draft capture as `social_draft`
- approval resolution into `social_outcome`
- manual publish evidence via `/memory publish <id> url=...`
- read-only X evidence capture through `huginn_x_post_visible_evidence`
- retrospective prompt enrichment from `social_outcome` and `social_evidence`

The missing capability is the live X post action itself.

## Existing State Found

### Commands and CLI surfaces

- `/memory resolve` updates a pending `social_draft` and auto-records `social_outcome`
- `/memory publish` marks an approved `social_outcome` as published with a URL and timestamp
- `/tool run` invokes builtin tools and auto-records workflow-scoped memory when the tool result includes memory records

### Browser and Huginn surfaces

- `scripts/browser/browser-access.sh` already supports:
  - browser launch with persistent Chromium profile
  - trusted-session attach through Chrome CDP
  - navigation
  - selector typing
  - selector clicking
  - arbitrary DOM evaluation
  - screenshots
- `scripts/browser/odin-huginn-server.js` already exposes the matching server routes:
  - `/launch`
  - `/connect`
  - `/navigate`
  - `/act`
  - `/evaluate`
  - `/screenshot`

### Social tooling

- The only live social Huginn tool today is `huginn_x_post_visible_evidence`
- It is explicitly read-only and visible-page only
- No X or LinkedIn publish tool exists today

### Contracts and policies

- The current contract requires explicit approval before publishing
- The current contract allows read-only X evidence capture through Huginn
- LinkedIn is explicitly manual and should remain manual for this slice

## Problem

Marcus can already draft, approve, and track X posts through Odin, but the actual publish step still happens outside Odin. That leaves the most operationally important step outside the existing workflow, even though the repo already contains enough browser primitives to perform that action.

## Goals

- Add X-only native posting through the existing Odin workflow
- Keep the action explicitly approval-gated
- Reuse the existing `social_outcome` lifecycle
- Keep LinkedIn manual
- Fail closed on uncertain outcomes
- Record enough evidence to support later review and visible-evidence capture

## Non-Goals

- No LinkedIn browser publishing
- No reply automation
- No likes, reposts, follows, DMs, or engagement automation
- No bulk posting or scheduling
- No stealth/background activity designed to conceal automation
- No new social subsystem or second approval queue

## Proposed Design

### Operator surface

Extend the existing command:

```text
/memory publish <id> url=<value> [published_at=<rfc3339>]
/memory publish <id> via=huginn_x
```

Behavior:

- `url=...` keeps the current manual-evidence behavior unchanged
- `via=huginn_x` performs a native X post for an already approved `social_outcome`

This preserves one publish entry point for one publish state transition.

### Eligibility rules

`via=huginn_x` is valid only when all of the following are true:

- memory type is `social_outcome`
- `result=approved`
- `publish_status` is not already `published`
- `channel=x`
- `content_kind=post`

This intentionally excludes replies and threads from the first slice.

### Content source

The outgoing X post body is the `social_outcome` summary text. No second content store is introduced.

### Runtime architecture

Add one new builtin tool and one new driver lane:

- tool key: `huginn_x_post_publish`
- env var: `ODIN_HUGINN_X_PUBLISH_DRIVER`
- adapter/service pair parallel to the current read-only X evidence path
- shell path: `/memory publish ... via=huginn_x` internally calls that tool lane instead of requiring the operator to invoke it manually

The tool result should not auto-record new workflow memory. The source of truth remains the original `social_outcome`, which `/memory publish` updates in place.

### Browser execution model

The publish driver will:

1. start or reuse a headed Huginn browser session
2. navigate to X compose UI
3. verify a compose surface exists
4. type the approved post text
5. click the Post button
6. wait for a success signal
7. extract the resulting post URL when possible
8. capture a screenshot for publish evidence

Preferred session behavior:

- Use existing repo-local persistent browser state
- Favor an operator-visible flow
- Allow trusted-session attach when already configured

### Success criteria for live posting

A native publish is considered successful only if the driver can provide enough evidence to support a real post action. The preferred success bundle is:

- `publish_url`
- `published_at`
- `final_url`
- `screenshot_path`
- `posted_text`

If the driver cannot confirm a publish outcome, it must fail instead of guessing.

### Memory update behavior

On successful native publish, `/memory publish ... via=huginn_x` updates the same `social_outcome` with:

- `publish_status=published`
- `publish_mode=huginn_x`
- `publish_url=<status url>`
- `published_at=<driver timestamp or now>`
- `publish_screenshot_path=<path>`

The existing memory id remains the operator-facing record.

### Failure behavior

The command must fail closed in these cases:

- no compose box found
- no post button found
- X not logged in
- post submission appears blocked
- success cannot be verified
- resulting URL cannot be determined

When it fails, the memory remains approved and unpublished.

## Alternatives Considered

### Option 1: Add a standalone tool and keep `/memory publish` manual only

Rejected.

This would split the publish action from the publish state transition and duplicate operator workflow. The approval lifecycle already lives on `social_outcome`.

### Option 2: Add a new `/social publish` command family

Rejected.

That would create a parallel abstraction instead of extending the existing Odin memory lifecycle.

### Option 3: Allow native posting for both X and LinkedIn together

Rejected.

The current repo policy explicitly treats LinkedIn browser automation as the highest-risk path. This slice should stay X-only.

## Testing Strategy

### Unit and focused shell tests

- parser coverage for `via=huginn_x`
- `/memory publish` validation coverage for incompatible memory types and channels
- `/memory publish` happy-path coverage with a fixture driver

### Driver and tool tests

- adapter/service coverage for the new driver lane
- builtin tool catalog coverage
- live driver script coverage using the existing fixture browser helpers

### Real Odin verification

Use the real compiled binary to prove:

1. draft an X post
2. resolve it to approved
3. publish it via `/memory publish <id> via=huginn_x`
4. confirm the same `social_outcome` is marked published
5. optionally feed the returned URL into `huginn_x_post_visible_evidence`

## Compliance Boundary For This Slice

- X-only
- explicit prior approval required
- one post at a time
- no stealth behavior
- no engagement automation
- no LinkedIn browser posting

## Recommendation

Implement native X posting as a narrow extension of `/memory publish` backed by one new Huginn driver and tool lane. This is the smallest change that closes the real operator gap while preserving the existing Odin workflow and keeping LinkedIn manual.
