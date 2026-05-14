---
title: Odin Mobile Security Model
status: active
date: 2026-05-13
---

# Odin Mobile Security Model

This document is the security contract for using the Odin PWA from a phone.
The mobile surface is a homelab/private-network operator surface, not a public
internet product.

## Homelab Access Assumptions

- Do not expose `odin serve` publicly by default.
- Keep the service listener on loopback or a private interface. The homelab
  default remains `127.0.0.1:9444`.
- Recommended phone access is through Tailscale, another private VPN, or an
  operator-owned reverse proxy reachable only on a trusted private network.
- Browser access assumes HTTPS/TLS at the private ingress layer. Use Tailscale
  HTTPS, a trusted local reverse proxy certificate, or another TLS-terminating
  private ingress before installing the PWA on a phone.
- Do not publish broad Cloudflare Tunnel, router port-forward, or public DNS
  access to Odin unless a separate public-exposure review explicitly approves it.

## Auth And Sessions

- Device registration is `POST /mobile/devices/register`.
- Registration requires the configured Odin admin bearer token. Operators should
  read the current PWA registration credential with `odin mobile token`.
- The admin token is a one-time registration credential for the PWA. It must not
  be stored in frontend state.
- Registration creates a durable `mobile_device` row and a short-lived
  `mobile_session` row.
- The session token is stored only in the `odin_mobile_session` HttpOnly,
  Secure, SameSite=Strict cookie.
- The browser receives a CSRF token once at registration and stores it in
  session storage, not local storage.
- Existing admin-token auth remains available for operator/API compatibility,
  but the PWA capture flow uses the device session cookie and CSRF header.

## CSRF

Mobile browser sessions use a double-submit strategy:

- Mutating mobile requests sent with the session cookie must include
  `X-Odin-CSRF`.
- Odin stores only the CSRF token hash.
- Missing or invalid CSRF headers fail closed before mutation.
- Admin bearer-token requests do not use cookie auth and are not subject to the
  browser CSRF check.

## Device And Push Revoke

- Device revoke is `POST /mobile/devices/{device_id}/revoke`.
- Revoke marks the device revoked and revokes active sessions for that device.
- A revoked device/session is denied before any mobile mutation executes.
- Push subscription requests are recorded as safe metadata and, for registered
  devices, as `mobile_push_subscriptions` rows.
- Push revoke is `POST /mobile/notifications/subscriptions/{subscription_id}/revoke`.

## Intake Gates

- All `/mobile/*` reads and writes require either admin auth or a valid mobile
  device session.
- Intake mutations are rate limited per device/session boundary. Current limit:
  30 intake mutations per minute.
- JSON mobile request bodies are capped at 1 MiB.
- Image uploads are capped at 10 MiB and restricted to JPEG, PNG, WebP, and GIF.
- Audio uploads are capped at 25 MiB and restricted to WebM, MP3, MP4, WAV, and
  Ogg content types.
- Intake writes still use canonical raw intake items and do not create executable
  work directly.

## Browser Storage

- The PWA may store failed-upload retry data in local storage.
- The PWA must not store long-lived admin tokens in local storage, IndexedDB, or
  other frontend state.
- Session tokens are HttpOnly cookies and are not readable by JavaScript.
- CSRF tokens are session-scoped browser state and can be cleared by registering
  the device again.

## Headers And CORS

Odin HTTP responses include conservative browser headers:

- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: no-referrer`
- `Permissions-Policy` limited to same-origin camera and microphone for PWA capture
- `Content-Security-Policy` restricted to same-origin script, style, connect,
  manifest, media, and image sources

CORS is locked down:

- Odin does not emit `Access-Control-Allow-Origin: *`.
- Cross-origin requests receive no CORS allow header by default.
- Same-origin browser requests may use credentials and the `X-Odin-CSRF` header.

## Audit Events

Mobile state changes emit runtime audit events in the `mobile_device` stream:

- `mobile.login` when a device session is registered.
- `mobile.logout` when a device is revoked.
- `mobile.intake_created` when a mobile device session creates raw intake.
- `mobile.approval_resolved` when a mobile device session resolves an approval.
- `mobile.push_subscription_revoked` when a mobile push subscription is revoked.

Canonical approval and intake events still come from the existing runtime/store
services. Mobile events are additional operator evidence, not a second authority.

## Backup And Restore

Mobile devices, sessions, push subscription rows, and mobile audit events live in
the canonical SQLite runtime database. Existing Odin backup, verify-backup, and
restore paths cover this state with the rest of the runtime root. Run the
homelab backup verification gate before any release or rollback.

## Stop Conditions

Stop mobile access work if any condition is true:

- Odin would need to be exposed publicly without a separate exposure review.
- HTTPS/TLS cannot be provided for phone access.
- A mutation endpoint can run without admin auth or a valid mobile session.
- A browser session mutation can run without CSRF validation.
- Device revoke does not deny subsequent mobile mutations.
- The PWA stores the admin token or session token in JavaScript-readable
  persistent storage.
