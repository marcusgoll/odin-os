# X Weekly Evidence Bundle Design

## Problem

`odin-os` can already capture read-only visible-page evidence for one explicit X post URL through `huginn_x_post_visible_evidence`.

That is enough to prove the model and feed the Marcus analytics prompt, but it is not efficient for weekly use. Marcus still has to run one Odin action per post URL.

The next practical slice is a bounded weekly bundle that:

- accepts several explicit X post URLs
- reuses the existing single-post evidence path
- records one `social_evidence` entry per post
- returns a compact weekly capture summary

This must stay explicit, read-only, and visible-page-only. It must not infer posts automatically yet, and it must not introduce unofficial API harvesting or profile-wide scraping.

## Existing Repo State

### Already real

- `odin-os` already has a dedicated X post evidence tool:
  - `huginn_x_post_visible_evidence`
- The builtin tool already records workflow-scoped `social_evidence`.
- The Marcus analytics prompt already includes recent X visible evidence from `social_evidence`.
- The repo already has a generic `/tool run` surface and generic tool-result memory recording.

### Partial

- The current memory-record hook is shaped around a single logical tool result, even though it can store one memory per invocation.
- The publish-memory path exists, but it is not yet mature enough to drive automatic weekly evidence seeding.

### Missing

- one action that can capture evidence for multiple explicit X post URLs
- a compact weekly batch summary
- a generic multi-memory tool-result contract for batch tools

## Approaches

### Option 1: Add a batch tool that composes the existing single-post X evidence path

Pros:

- preserves the current explicit-URL boundary
- reuses the existing X post driver and memory schema
- gives Marcus one weekly action instead of repeated manual runs

Cons:

- requires a small generic extension so one tool invocation can request multiple memory writes

Verdict: recommended.

### Option 2: Keep batching in the shell by repeating `/tool run huginn_x_post_visible_evidence ...`

Pros:

- smaller initial code change

Cons:

- duplicates orchestration in the CLI
- gives no reusable batch summary contract
- does not help future non-shell callers

Verdict: reject.

### Option 3: Auto-discover weekly X posts from recent published outcomes

Pros:

- less operator input later

Cons:

- couples evidence capture to publish-memory completeness too early
- introduces heuristics before the explicit path is mature

Verdict: future follow-on, not next.

## Selected Design

### 1. Add one new builtin tool: `huginn_x_weekly_evidence_bundle`

The tool should accept:

- `target_urls` required as a comma-separated list of explicit X post URLs
- optional `label`
- optional `wait_ms`
- optional `headless`

The tool should reject:

- empty URL lists
- non-X hosts
- duplicate URLs after normalization

### 2. Reuse the existing single-post evidence path

Do not add a second browser driver.

The bundle tool should compose the existing `huginn_x_post_visible_evidence` path for each explicit URL. That keeps all X DOM extraction and screenshot behavior in one place.

The weekly bundle is orchestration, not a new extraction implementation.

### 3. Extend the generic tool result with multiple memory records

Replace the single optional memory-record shape with a list:

- `MemoryRecords []MemoryRecord`

Single-post tools should emit one-element lists.

The shell should record each requested memory entry in order after a successful `/tool run`.

### 4. Record one `social_evidence` entry per captured post

The weekly bundle should persist one workflow-scoped `social_evidence` record per URL, using the same field model already established for single-post X evidence:

- `channel=x`
- `evidence_kind=x_post_visible`
- `target_url`
- `final_url`
- extracted visible metrics when present
- `screenshot_path`
- `snapshot_path`

Additionally, each batch-created record should include:

- `bundle_label`
- `bundle_position`

These fields help operators inspect the weekly batch later without creating a new memory type.

### 5. Return a compact weekly summary from the batch tool

The batch tool result should include:

- attempted URL count
- recorded evidence count
- failed URL count
- per-post status lines as artifacts

It should not echo every field from every post into the top-level result, because that would make the CLI output noisy. The detailed evidence should live in the recorded `social_evidence` entries.

### 6. Leave the Marcus analytics prompt unchanged

No analytics prompt format change is required for this slice.

Because the existing Marcus analytics path already reads recent `social_evidence`, the weekly bundle should automatically feed that path once it records the per-post entries.

### 7. Future planning after this slice

After the explicit weekly bundle is stable, the next likely follow-ons are:

- optional seeding of `target_urls` from recent published `social_outcome publish_url`
- manual LinkedIn evidence intake using the same `social_evidence` model
- longer-horizon evidence comparison only if it clearly improves operator decisions

## Non-Goals

- LinkedIn browser evidence capture
- automatic inference of X post URLs
- unofficial API replay or hidden network-call harvesting
- profile-wide scraping
- changes to the analytics prompt beyond what already works

## Acceptance Criteria

- `odin-os` provides a `huginn_x_weekly_evidence_bundle` tool for several explicit X post URLs
- the tool reuses the existing single-post X evidence path
- one workflow-scoped `social_evidence` entry is recorded per successful URL
- the CLI returns a compact weekly batch summary
- the existing Marcus analytics prompt sees the new evidence without any additional prompt changes
- the slice is proven through the real `odin` shell
