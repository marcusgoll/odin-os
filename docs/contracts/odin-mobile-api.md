# Odin Mobile API Contract

## Purpose

The Odin Mobile API is the authenticated HTTP surface for the Odin PWA. It is a thin projection and action layer over existing Odin runtime services. It does not own runtime state, scheduling, approvals, intake, browser sessions, work execution, or notification authority.

## Authority boundaries

- Runtime truth remains in SQLite and existing runtime services.
- Read routes call health, overview, projection, approval, intake, and browser-session readbacks.
- Mutating routes require either the configured Odin admin bearer token or a valid registered mobile device session and call a runtime service when one exists.
- Intake writes use the same canonical intake item path used by the Odin CLI because no separate runtime intake service exists yet.
- Approval decisions use `internal/runtime/approvals.Service` so approval resolution and audit events stay canonical.
- Notification subscription requests are accepted as raw intake metadata until Odin has a dedicated notification preference service.
- Responses must not include configured admin tokens, webhook secrets, browser profile paths, handoff URLs, handoff IDs, private viewer URLs, cookies, profile bytes, or credential material.

## Authentication and mutation policy

All `/mobile/*` routes require authentication using one of:

- `Authorization: Bearer <ODIN_ADMIN_TOKEN>`
- `X-Odin-Admin-Token: <ODIN_ADMIN_TOKEN>`
- `odin_mobile_session` HttpOnly cookie from `POST /mobile/devices/register`

The admin token is allowed as an operator/API compatibility path and as the device-registration credential. The PWA must not store it in local storage or other long-lived frontend state.

Cookie-authenticated browser sessions use CSRF protection: mutating routes require `X-Odin-CSRF`, and Odin stores only the CSRF token hash. Mutating routes reject unauthenticated, invalid, revoked, or CSRF-missing requests before parsing or applying a state change.

Forbidden or unauthenticated mutation outcomes:

- missing token: `401 admin_auth_required`
- wrong token: `403 admin_auth_failed`
- disabled token config: `503 admin_disabled`
- invalid or revoked mobile session: `403 mobile_session_invalid`
- missing or invalid CSRF for cookie session: `403 mobile_csrf_required`
- policy or service conflict: `409` with a stable error code

### POST `/mobile/devices/register`

Requires admin bearer auth. Body:

```json
{
  "device_name": "Marcus iPhone"
}
```

Creates a durable mobile device and session, sets the `odin_mobile_session`
HttpOnly, Secure, SameSite=Strict cookie, and returns the one-time CSRF token:

```json
{
  "device_id": "device-public-id",
  "session_id": 1,
  "csrf_token": "one-time-browser-session-csrf-token",
  "expires_at": "2026-06-12T00:00:00Z"
}
```

The response never includes the session token because it is only sent in the
cookie.

### POST `/mobile/devices/{device_id}/revoke`

Requires admin auth or the same device's valid mobile session plus CSRF. Revokes
the device and active sessions. Revoked sessions are denied before future
mobile mutations.

## Endpoints

### GET `/mobile/status`

Returns the same operational status projection used by `/status`: health status, readiness, runtime state, worker dispatch state, and dashboard counts.

### GET `/mobile/overview`

Returns the canonical overview view built by `internal/cli/overview.Service`. The PWA must treat this as a projection only. Counts in `actual_use` must remain consistent with `odin overview --json` for the same runtime root.

### GET `/mobile/work-items`

Returns work item status projections from `internal/runtime/projections.ListTaskStatusViews`.

### GET `/mobile/runs`

Returns run summary projections from `internal/runtime/projections.ListRunSummaryViews`.

### GET `/mobile/review-queue`

Returns a mobile review queue projection derived from current reviewable intake items, pending approvals, and failed work. This is read-only and must not replace `odin review` as the canonical decision queue.

### POST `/mobile/review-queue/{queue_id}/decision`

Applies only the review actions that already have a narrow mobile-safe
continuation:

- `intake-review:<id>` supports `reject`, `clarify`, and `archive`; `accept`
  remains in `odin review` because it may promote work or require policy
  approval.
- `browser-login:<id>` supports `complete` after an operator manually performs
  the attended login handoff. This records an operator attestation and verifies
  the browser session metadata; it does not collect credentials.
- `approval:<id>` decisions must use `/mobile/approvals/{approval_id}/decision`.
- failed work, recovery, and browser evidence rows remain inspect-only until
  their existing runtime services expose a mobile-safe mutation contract.

Body:

```json
{
  "action": "clarify",
  "reason": "Need current screenshot evidence",
  "actor": "odin-pwa"
}
```

The route uses the same mobile authentication and CSRF policy as other mobile
mutations and fails closed with `409 mobile_review_action_unsupported` for
unsupported queue sources or actions.

### GET `/mobile/approvals`

Returns pending approval projections from `internal/runtime/projections.ListPendingApprovalViews` plus resolver support from `internal/runtime/approvals.Service.Detail` when available.

### POST `/mobile/approvals/{approval_id}/decision`

Body:

```json
{
  "action": "approve",
  "reason": "operator approved from mobile",
  "decision_by": "mobile-api"
}
```

Rules:

- `action` is `approve`, `deny`, or `clarify`.
- `reason` is required.
- The route calls `approvals.Service.Resolve`.
- The canonical approval service emits approval audit events through the store.
- Unsupported resolvers fail closed with `409 approval_resolver_unsupported`.

### POST `/mobile/intake/raw`

Body:

```json
{
  "kind": "idea",
  "title": "Short title",
  "content": "Raw mobile text, prompt, or idea",
  "project_key": "optional-project-key",
  "source_app": "optional share source",
  "share_url": "optional source URL",
  "transcript": "optional placeholder"
}
```

Rules:

- JSON `kind` is one of `text`, `note`, `prompt`, `idea`, `task`, `bug`, or `project_note`.
- Content is persisted in intake source facts; responses return metadata only.
- Multipart `POST /mobile/intake/raw` accepts the same fields plus one `attachment` file. Attachment kinds are captured as `photo`/`image` or `voice_note`/`audio`.
- Images must be `image/jpeg`, `image/png`, `image/webp`, or `image/gif` and no larger than 10 MiB.
- Audio must be `audio/webm`, `audio/mpeg`, `audio/mp4`, `audio/wav`, `audio/x-wav`, or `audio/ogg` and no larger than 25 MiB.
- Attachment bytes and metadata are stored under the canonical intake item as raw evidence. The API response returns intake metadata only.
- Validation failures return stable error codes and do not store invalid attachment bytes; the PWA keeps failed captures visible in its retry queue.
- The created item status is `received` and the dedupe recipe is `mobile-api-v1`.
- Raw intake and attachment intake routes are rate-limited per mobile device or admin caller. Current limit: 30 intake mutations per minute.

### POST `/mobile/intake/attachments`

Body:

```json
{
  "filename": "photo.jpg",
  "content_type": "image/jpeg",
  "size_bytes": 12345,
  "digest": "sha256:...",
  "description": "optional operator note"
}
```

The route records attachment metadata only for clients that have already staged bytes elsewhere. PWA image and voice capture must use multipart `POST /mobile/intake/raw` so the raw intake row and attachment evidence are stored together.

### GET `/mobile/notifications/preferences`

Returns the current notification capability projection. Until a dedicated notification preference service exists, the route reports `not_configured` and no durable subscription list.

### POST `/mobile/notifications/subscriptions`

Accepts web-push subscription metadata and records a raw intake item with safe metadata only. It does not echo endpoint URLs, auth keys, or browser keys.

Registered mobile device sessions also record a safe subscription row with the
endpoint hash, host, user agent, platform, and status.

### POST `/mobile/notifications/subscriptions/{subscription_id}/revoke`

Requires a valid mobile device session plus CSRF. Revokes the subscription for
the current device and emits a mobile audit event.

### GET `/mobile/browser/status`

Returns safe browser session, login request, and handoff runner status readbacks from the SQLite browser-session metadata tables. It excludes handoff IDs, handoff URLs, private viewer URLs, bind addresses, profile paths, and credential-bearing material.

## Error envelope

Errors use the existing API envelope:

```json
{
  "error": {
    "code": "stable_code",
    "message": "human readable message"
  }
}
```

## Security review

- Mobile routes require token auth for reads and writes to keep the PWA authenticated by default.
- Mutations reject missing or invalid tokens before executing runtime actions.
- Browser-session mutations reject missing or invalid CSRF before executing runtime actions.
- Device session tokens are stored as HttpOnly, Secure, SameSite=Strict cookies; only token hashes are stored in SQLite.
- Device revoke fails closed by marking the device and active sessions revoked.
- Intake endpoints apply a per-device/admin-caller rate limit.
- Approval decisions reuse the approval service and therefore preserve audit behavior.
- Intake routes persist raw metadata and image/audio evidence through canonical intake items and do not create work items directly.
- Browser status readbacks intentionally omit profile paths, handoff tokens, URLs, process bind data, private viewer URLs, cookies, and credentials.
- Notification subscription requests store only safe metadata and hashes, not push keys or endpoint URLs.
- Mobile-specific login, logout, approval, intake, and push-subscription revoke events are emitted as additional audit evidence.
