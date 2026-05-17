# Email Review Actions Design

## Current State

Odin already has a governed decision spine:

- `odin review list/show/act` is the canonical operator queue for reviewable items.
- `internal/runtime/reviewqueue` defines the shared queue projection shape and `allowed_actions`.
- `internal/runtime/approvals.Service` owns approval detail and resolution.
- `/mobile/approvals/{approval_id}/decision` already resolves approvals through the approval service.
- `/mobile/review-queue/{queue_id}/decision` already supports mobile-safe review mutations for intake review rows and attended browser-login completion.
- `internal/runtime/notifications.Service` routes action-required runtime events into notification rows and emits `notification.created` audit events.

There is no current email sender, email action token, or unauthenticated-by-session HTTP action surface for email links.

## Scope

This slice adds email-action capability without creating a second decision queue. Email content is generated from the same pending approval/review projections used by Odin's operator surfaces. Email actions are limited to actions that are already supported by the existing HTTP/runtime services:

- approvals: `approve`, `deny`, `clarify`
- intake review: `reject`, `clarify`, `archive`
- all other queue entries: `open-review` only

`accept` or other promotion-like review actions remain on `odin review` unless their existing runtime service exposes a mobile-safe HTTP mutation contract.

## Architecture

Add an email action package that owns:

- signed token creation and validation
- HTML/plain-text email rendering
- action-link construction
- current-state checks before any mutation

Tokens include version, recipient, queue ID, source type, object ID, action, reason, issue time, expiration time, and optional policy/runtime snapshot hashes. Tokens are HMAC-signed with an Odin-owned secret and encoded as URL-safe text. Validation fails closed for missing secret, malformed token, bad signature, expiration, unsupported action, stale item state, or changed approval snapshots.

Add Odin HTTP routes:

- `GET /email-actions/{token}` validates the token and applies the encoded action when it is a supported mutation.
- `GET /email-actions/open/{token}` validates the token and redirects to the Odin PWA/review route without mutating state.
- `GET /email-actions/preview` requires admin auth and returns the current email payload for verification and operator inspection.
- `POST /email-actions/send` requires admin auth and submits the same payload through the configured `sendmail` path.

The mutation route uses existing services:

- approvals call `approvals.Service.Resolve`
- intake review decisions call the same store path used by the mobile review endpoint
- unsupported or already-resolved rows return conflict instead of applying a guessed fallback

## Delivery

The first delivery generates email payloads and action links deterministically, and can submit that payload through a configured `sendmail` command. Verification for this slice proves the payload, configured sender boundary, link, HTTP action, and audit path without requiring external email credentials.

The default recipient is `marcusgoll@gmail.com` when the email-action endpoint is configured for this workflow. Production deployment still needs a reachable `base_url` and signing secret.

## Security Review

- Tokens are least privilege: each token authorizes exactly one recipient, one queue item, and one action.
- Tokens expire.
- Tokens carry approval snapshot hashes when available so approval links fail if the approval basis changed.
- Replay fails closed because the handler checks current item state before mutation; a resolved/archived/rejected row is no longer actionable.
- Links never include admin tokens, cookies, browser handoff IDs, secrets, or raw evidence.
- Email links use Odin's HTTP surface, but the token is not a general API credential.
- Actions append the same canonical audit events as their underlying services.

## Verification Contract

Verification must show:

- repo-local `./bin/odin` is built and used for operator command proof
- an isolated `ODIN_ROOT` contains a pending approval or review item
- `odin review list --json` shows the item
- `/email-actions/preview` generates an email payload for `marcusgoll@gmail.com` with action links
- clicking one generated action link resolves only the intended item
- replaying the same link fails closed
- invalid and expired tokens fail closed
- runtime events include the canonical resolution/audit events
