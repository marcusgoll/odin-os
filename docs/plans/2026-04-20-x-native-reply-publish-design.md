# X Native Reply Publish Design

## Current State

`odin-os` already has the right foundations for a true X social E2E loop:

- `social_draft` capture
- approval resolution into `social_outcome`
- manual publish evidence through `/memory publish <id> url=...`
- read-only visible-page X evidence capture through `huginn_x_post_visible_evidence`
- a documented but not-yet-finished native X publish lane through `/memory publish <id> via=huginn_x`

The next gap is not a new social subsystem. It is extending the same native publish path so Marcus can publish one real approved X post and one real approved X reply inside the existing Odin lifecycle.

## Existing State Found

### Commands and lifecycle

- `/memory resolve` already moves approved draft intent into `social_outcome`
- `/memory publish` already updates the approved `social_outcome` in place
- `social_outcome` already carries `channel` and `content_kind`
- `content_kind=reply` is already a valid social outcome shape

### Social workflow

- The Marcus workflow already treats replies as approval-gated social work
- The engagement assistant already produces reply suggestions, but not live reply publishing
- Analytics already reasons over `social_outcome` and `social_evidence`

### Browser and driver surfaces

- The repo already has the browser primitives needed for a native X action
- The post-only native publish design already proposes `ODIN_HUGINN_X_PUBLISH_DRIVER`
- No reply-target metadata or reply-specific native publish contract is documented yet

## Problem

Marcus wants one true post and one true engagement action for a full real-world test.

The post half should use the existing approved-post lifecycle.

The engagement half should be a real X reply on someone else’s post, not just observed engagement telemetry. That means Odin needs an explicit, reviewable way to carry the target post URL from approval through live publish without creating a second command family.

## Goals

- Keep one publish entry point: `/memory publish`
- Reuse the same `social_outcome` lifecycle for posts and replies
- Support one approved X post and one approved X reply as a real E2E workflow
- Require an explicit parent post URL for any live reply action
- Keep the whole path human-approved, operator-visible, and fail-closed
- Preserve compatibility with the existing visible-evidence and analytics path

## Non-Goals

- No LinkedIn native publishing
- No automated likes, reposts, follows, DMs, or bulk engagement
- No autonomous target discovery for replies
- No hidden API replay or stealth browser behavior
- No second publish queue or separate engagement-publish subsystem

## Proposed Design

### 1. Keep one operator surface

Do not add `/social publish` or `/engagement publish`.

Keep the existing publish command:

```text
/memory publish <id> url=<value> [published_at=<rfc3339>]
/memory publish <id> via=huginn_x
```

Behavior:

- `url=...` remains the manual evidence path
- `via=huginn_x` becomes the single native X publish path for both:
  - `content_kind=post`
  - `content_kind=reply`

### 2. Add explicit reply target metadata to the existing outcome

Replies need one required targeting field on the same `social_outcome`:

- `in_reply_to_url=<explicit X post URL>`

Optional but useful metadata may also be carried on the same outcome:

- `in_reply_to_author_handle`
- `reply_context_label`

No new table, memory type, or approval object should be introduced. The reply target lives in `details.fields` on the same `social_outcome`.

### 3. Eligibility rules for native X publish

`/memory publish <id> via=huginn_x` should remain X-only and approval-gated.

It is valid only when all of the following are true:

- memory type is `social_outcome`
- `result=approved`
- `publish_status` is not already `published`
- `channel=x`

Then split by `content_kind`:

For `content_kind=post`:

- existing post-only rules apply

For `content_kind=reply`:

- `in_reply_to_url` is required
- `in_reply_to_url` must be a valid X status URL

This still excludes thread publishing, DMs, and all non-X native actions.

### 4. Reuse the same native publish lane

Do not add a second driver or tool for replies.

Reuse the planned native publish lane:

- env var: `ODIN_HUGINN_X_PUBLISH_DRIVER`
- builtin tool contract: the same native X publish tool should accept `content_kind`
- shell path: `/memory publish ... via=huginn_x`

The publish tool input should include:

- `post_text`
- `content_kind`
- `in_reply_to_url` when replying

This keeps one operator mental model and one state transition.

### 5. Browser execution model

For `content_kind=post`:

- keep the post-only compose flow already documented in the existing native publish design

For `content_kind=reply`:

1. start or reuse the headed Huginn browser session
2. navigate to `in_reply_to_url`
3. verify the reply composer is present
4. type the approved reply text
5. click the Reply button
6. wait for a confirmed success state
7. extract the resulting reply URL when possible
8. capture screenshot evidence

If the driver cannot confirm the reply target, the compose surface, or the published reply URL, it must fail closed.

### 6. Memory update behavior

On successful native X publish, Odin should still update the same `social_outcome` in place.

For both posts and replies, store:

- `publish_status=published`
- `publish_mode=huginn_x`
- `publish_url=<resulting status URL>`
- `published_at=<driver timestamp or now>`
- `publish_screenshot_path=<path>`

For replies, also preserve:

- `in_reply_to_url=<original target URL>`

No new outcome record should be created during publish.

### 7. Evidence handoff

After native publish succeeds, the next operator step should remain the existing visible-evidence path:

```text
/tool run huginn_x_post_visible_evidence target_url=<publish_url> label=<value>
```

This keeps the analytics and retrospective flow unchanged. Native publish updates the outcome; visible evidence records the observed page state.

## Alternatives Considered

### Option 1: Separate reply publishing into `/engagement publish`

Rejected.

That would duplicate approval checks, publish-state handling, and operator workflow.

### Option 2: Add a dedicated `social_reply_outcome` memory type

Rejected.

`social_outcome` already models approved social actions well enough. Adding a second outcome type would be unnecessary parallel structure.

### Option 3: Infer the reply target from the prompt or recent browsing

Rejected.

A real reply action needs an explicit, reviewable target URL. Guessing is not acceptable for a live action path.

## Testing Strategy

### Shell and parser tests

- reply-aware validation for `/memory publish ... via=huginn_x`
- native X publish rejection when `content_kind=reply` lacks `in_reply_to_url`
- native X publish rejection when `in_reply_to_url` is not an X status URL

### Tool, adapter, and driver tests

- preserve or extend the single native X publish lane rather than creating a second one
- verify the driver contract supports both `post` and `reply`
- verify reply-mode request payloads include `in_reply_to_url`

### CLI integration tests

Use the compiled binary with fixture drivers to prove:

1. an approved X post can be natively published through `/memory publish <id> via=huginn_x`
2. an approved X reply with `in_reply_to_url` can be natively published through the same command
3. both updated outcomes remain queryable through `/memory show`
4. both published URLs can feed the existing visible-evidence path

## Compliance Boundary For This Slice

- X-only
- explicit prior approval required
- one post or one reply at a time
- operator-visible browser behavior only
- no stealth behavior
- no automated engagement beyond the one explicitly approved reply action
- no LinkedIn native posting

## Recommendation

Finish the existing native X publish path and extend it so `/memory publish <id> via=huginn_x` supports both approved posts and approved replies. Use `in_reply_to_url` on the same `social_outcome` as the explicit reply target, then keep visible-evidence capture and analytics reuse exactly where they already live.
