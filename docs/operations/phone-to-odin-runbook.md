---
title: Phone-to-Odin Operator Runbook
status: active
date: 2026-05-13
---

# Phone-to-Odin Operator Runbook

This runbook is the release gate for daily phone use of Odin. It proves the
private-network PWA/mobile surface can monitor Odin, handle approvals, capture
inbox items, exercise notification subscription metadata, and show Huginn
browser evidence in mobile review without touching live external systems.

## Release Gate Command

Run the phone release gate from the repo root:

```bash
make odin-phone-release-check
```

The target builds repo-local binaries and runs
`scripts/odin-phone-release-check.sh`. The script intentionally uses
source-local mode: `./bin/odin` is the authoritative binary for the check. If an
installed binary exists, it is not treated as release authority for this gate.

## Safety Boundaries

- Use a temporary `ODIN_ROOT` and temporary `HOME`.
- Keep the service listener on loopback for the check.
- No public exposure.
- No real browser login.
- No live push provider.
- No external mutation.
- Fail closed when admin auth, mobile session auth, CSRF, approval resolver
  support, or attachment policy is bypassed.

Phone access for actual use still follows `docs/operations/odin-mobile-security.md`:
use a private network path such as Tailscale or an operator-owned private
reverse proxy with HTTPS/TLS at the phone-facing ingress. Do not expose Odin on
the public internet without a separate exposure review.

## Proof Matrix

The gate proves these items:

- Source-local binary alignment: `./bin/odin help` runs in explicit
  source-local mode.
- Odin health/readiness: short-lived `odin serve` starts against temp
  `ODIN_ROOT`; `/healthz` and `/readyz` return healthy or degraded JSON.
- Mobile API auth: device registration uses admin bearer auth; subsequent
  mobile mutations use the session cookie plus `X-Odin-CSRF`.
- PWA build: `go test ./internal/api/http -run TestPWA -count=1 -v`.
- PWA installability artifacts: app shell, manifest, service worker, and share
  target are present.
- Overview loads: `/mobile/overview` returns the canonical overview projection.
- Approval handling: a supported pending approval is listed and approved through
  `/mobile/approvals/{approval_id}/decision`.
- Raw text intake: `/mobile/intake/raw` stores a text raw intake item.
- Image attachment intake: multipart image capture stores attachment metadata.
- Audio/voice attachment intake: multipart voice capture stores attachment
  metadata.
- Share target: `/app/share`, manifest `share_target`, service worker pending
  share handling, and `/mobile/intake/share` are tested where possible without a
  real phone browser install.
- Notification subscription fake path: `/mobile/notifications/subscriptions`
  accepts a `push.example.test` endpoint and revoke works without a live push
  provider.
- Huginn browser evidence: a `huginn_browser` run artifact with
  `adapter_kind=stub_local` appears in the mobile review queue as browser
  evidence count.
- No external mutation: the test path uses local HTTP servers, temp SQLite
  state, stub-local browser evidence, and fake push endpoints only.
- Audit events: mobile login, mobile intake, mobile approval resolution, mobile
  push revoke, canonical approval requested, and canonical approval resolved
  events are asserted.

## Stubbed vs Real Behavior

Real in this gate:

- repo-local binary execution
- `odin serve` health/readiness
- PWA static artifact checks
- mobile session auth and CSRF
- mobile overview
- mobile approval resolution
- raw text, image, audio, and share-target intake writes
- mobile and canonical audit events

Stubbed or test-only, explicitly labeled:

- Huginn browser evidence uses `adapter_kind=stub_local`; no live browser login
  or external website mutation occurs.
- Notification subscription uses `https://push.example.test/...`; no live push
  provider is called.
- Share target installation is tested through manifest, service worker, route,
  and intake behavior; no mobile OS share sheet is launched.

## Before Daily Phone Use

After this gate passes, daily phone use still needs operator setup outside the
test harness:

- private HTTPS/TLS ingress for the phone-facing URL
- PWA install from the private URL
- device registration using the admin token once, then removal of the admin
  token from the phone workflow
- a deliberate decision on whether notification delivery remains in-app only or
  gets a real push provider
- a separate public-exposure review before any public DNS, broad Cloudflare
  Tunnel, or router port-forward path is introduced
