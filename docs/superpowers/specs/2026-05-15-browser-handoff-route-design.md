# Browser Handoff Route Design

Date: 2026-05-15
Status: ready for implementation handoff

## Audit Summary

- Existing state:
  - `CONTEXT.md` already defines Browser Control, Trusted Browser Session, Browser Intervention, Operator Surface, and Social Copilot language.
  - `docs/contracts/browser-session-handoff.md` already defines metadata-only browser session handoff, login requests, static HTML handoff lookup, admin-token completion, runner metadata, NoVNC planning, and explicit non-capabilities.
  - `internal/api/http/operational.go` already registers `GET /browser/session/handoff` and `POST /browser/session/handoff/complete`.
  - `internal/api/http/mobile.go` already adds attended browser login requests to `/mobile/review-queue` with a deep link to `/browser/session/handoff?handoff_id=<id>`.
  - `internal/runtime/browserhandoff` and lifecycle tests already cover runner metadata, fixture-safe runner start, NoVNC planning, command allowlists, and real-command gates.
  - Live service evidence shows an X session exists as `x-profile-bio-stress` with domain `x.com` and permission tier `authenticated_readonly`.
- Partial or contradictory state:
  - `odin serve` exposes the handoff HTTP route internally, but the live container nginx config only proxies `/app`, `/mobile`, `/healthz`, and `/readyz`.
  - Public/operator access through the live proxy currently returns nginx `404` for `/browser/session/handoff?...`.
  - Existing docs say the handoff URL is not proof that a browser handoff service exists; current HTML also says no browser is launched yet.
  - Host-level tools include display/browser/noVNC candidates, but the live container does not expose those executables to the runner environment.
- Missing pieces:
  - The live operator route for the existing handoff page.
  - A canonical configured handoff base URL for login-request creation.
  - PWA/mobile proof that an attended-login review item opens a reachable handoff page.
  - Host-side supervised browser/noVNC runner wiring, which is deliberately outside this first slice.
  - Browser profile persistence and authenticated session attachment, which remain outside this first slice.
- Reusable pieces:
  - SQLite browser session, login request, handoff lookup, verification, and runner metadata stores.
  - `GET /browser/session/handoff`, `POST /browser/session/handoff/complete`, and existing static escaped HTML.
  - `/mobile/review-queue` browser login review items and PWA review queue UI.
  - Live nginx deployment pattern in `.homelab-runtime/odin-pwa-proxy`.
  - `odin browser session create|login-request|handoff show|runner create|runner show|verify`.
- Relevant docs/ADRs:
  - `CONTEXT.md`
  - `docs/contracts/browser-session-handoff.md`
  - `docs/operations/browser-handoff-runner.md`
  - `docs/briefings/2026-05-14-odin-os-production-readiness-test.md`
  - `docs/contracts/odin-mobile-api.md`
- Relevant tests/commands:
  - `go test ./internal/api/http -run 'TestOperationalHandlerBrowserSessionHandoff|TestMobileBrowserAttendedLogin' -count=1`
  - `go test ./internal/app/lifecycle -run 'TestRunBrowserSession|TestRunBrowserSessionHandoff|TestRunBrowserSessionRunner' -count=1`
  - `go test ./internal/runtime/browserhandoff -count=1`
  - `make odin-pwa-e2e`
  - Live proof through `odin browser session login-request`, `/mobile/review-queue`, and `/browser/session/handoff`.
- Blockers found:
  - No blocker for the first route slice.
  - Real browser launch and X login viewer remain blocked until a separate host-side runner slice wires private viewer supervision and command availability.

## Existing State

The current implementation already has the core Odin metadata path:

1. A browser session record owns the domain, account hint, permission tier, lifecycle status, and profile path metadata.
2. A login request records an opaque `handoff_id`, status, expiration, and optional `handoff_url`.
3. `GET /browser/session/handoff` returns safe JSON by default and static escaped HTML for HTML clients.
4. `POST /browser/session/handoff/complete` is admin-token protected and records operator-attested metadata completion by verifying the session and completing the login request.
5. `/mobile/review-queue` exposes requested login handoffs as `browser_attended_login_required` items with `allowed_actions: ["open-handoff"]`.
6. Runner metadata and NoVNC planning exist, but the current contract is explicit that browser launch, profile persistence, authenticated session attachment, and real login execution are not complete.

The live failure is not that Odin lacks a handoff route. The failure is that the live operator ingress does not route the existing handoff route.

## Reused Components

- Browser session metadata tables and store methods.
- Existing login request and handoff ID generation.
- Existing safe handoff JSON and static HTML rendering.
- Existing mobile review queue item shape and deep-link field.
- Existing admin-token authorization for completion.
- Existing nginx-proxy deploy shape for the live PWA service.
- Existing browser handoff runner metadata as readback only, not as a launch dependency for this slice.

## New Components

This slice should add only route and configuration glue:

- A live proxy allowlist entry for:
  - `GET /browser/session/handoff`
  - `POST /browser/session/handoff/complete`
- A repo-owned or deployment-owned handoff base URL setting used by login-request creation when the operator asks for a live handoff link.
- Test coverage proving public/operator ingress exposes the handoff route without widening unrelated routes.
- PWA/mobile coverage proving browser login review items link to the routed handoff path.
- Operator runbook notes explaining that this route is metadata-only and does not launch a browser.

## Why New Components Are Necessary

The current route exists inside `odin serve`, but the live operator cannot reach it through the deployed nginx path. A new executor, queue, or browser automation lane would be the wrong fix. The missing component is the ingress route that connects the already implemented Operator Surface to the live service boundary.

The configured base URL is necessary because an SSH/homelab operator path must not produce unusable `localhost` links. Login requests should produce a route that the operator can actually open from the approved live/private surface.

## Locked Domain Decisions

- The canonical term is **Browser Handoff Route** for this slice.
- The route is an **Operator Surface** over existing browser session metadata.
- The route is not a **Trusted Browser Session**, not browser profile persistence, and not proof of authenticated browser reuse.
- The route is not an X mutation surface. X profile changes remain an attended, explicit, approval-gated future workflow.
- The route must expose only safe metadata and static escaped HTML.
- The route must not collect credentials, render credential fields, persist cookies, write browser profile bytes, launch a browser, start NoVNC, or imply that login is automated.
- `POST /browser/session/handoff/complete` remains operator-attested metadata completion only.
- Live ingress should remain allowlisted. Adding this route must not expose general operational endpoints such as `/metrics`, `/api/*`, `/admin`, or arbitrary root paths.
- Host-side supervised browser/noVNC remains the preferred future runner boundary so browser tools and credentials stay outside the container image and repo.

## Selected Design

### First Slice: Route Existing Metadata Handoff

The first PR-sized implementation should make the existing metadata handoff reachable from the live operator surface.

Implementation shape:

1. Update the live nginx/proxy config source so `/browser/session/handoff` and `/browser/session/handoff/complete` proxy to `odin serve`.
2. Keep the route exact or narrowly scoped. Do not proxy all `/browser/` paths unless tests prove every exposed path is intended.
3. Introduce a single configured handoff base URL for live login requests. The base should be a routed operator URL, not `localhost`, for example:
   - `https://odin.marcusgoll.com/browser/session/handoff`, if this route is intentionally exposed on the existing private/operator Odin origin.
   - Or a Tailscale/private hostname if the deployment uses one before production rollout.
4. Preserve current safe HTML. The first route slice may improve copy to say "metadata handoff ready" but must not add forms, password fields, scripts, or browser launch claims.
5. Ensure mobile review deep links resolve to the same routed path. If the PWA keeps relative links, the proxy route is sufficient; if login-request JSON includes a full URL, it must use the configured operator base.
6. Add release/deploy proof that a live login request appears in `/mobile/review-queue` and the handoff URL returns HTTP 200 with either JSON or static HTML.

Operator workflow after this slice:

1. Odin or the operator creates or reuses a browser session for `x.com`.
2. Odin creates a login request with a live handoff base URL.
3. The PWA/mobile review queue shows "Attended login required".
4. The operator opens the handoff link.
5. Odin displays safe request metadata and manual-login status.
6. The operator may record metadata-only completion through the protected completion route or CLI verification.

This proves the approval/handoff route exists. It still does not prove a browser viewer exists.

## Rejected Alternatives

### Route All Browser Paths

- Reuses: existing nginx proxy.
- Adds: broad `/browser/` public/operator proxy.
- Tradeoffs: faster but exposes future routes by accident.
- Risks: route expansion could surface endpoints before their security posture is reviewed.
- Test/verification shape: would need broad negative route tests.
- Rollout/migration shape: higher-risk live ingress change.
- Recommendation strength: Weak.

### Build Host-Side Browser Runner First

- Reuses: `internal/runtime/browserhandoff` NoVNC planning and runner metadata.
- Adds: host service, display/browser/noVNC supervision, private viewer, command config, process lifecycle, cleanup.
- Tradeoffs: gets closer to real login, but mixes route and process boundaries.
- Risks: too large for one PR; harder to verify safety; could blur metadata handoff with credential-sensitive browser supervision.
- Test/verification shape: fixture proof, command validation, host process tests, private viewer proof, cleanup proof.
- Rollout/migration shape: separate deploy and host config work.
- Recommendation strength: Medium for the next slice, Weak for this slice.

### Keep Handoff CLI-Only

- Reuses: `odin browser session handoff show`.
- Adds: no route changes.
- Tradeoffs: safe but does not solve the user's missing visible approval problem.
- Risks: PWA/mobile review keeps linking to an unreachable route.
- Test/verification shape: CLI-only proof.
- Rollout/migration shape: no rollout.
- Recommendation strength: Weak.

## Prototype Verdict

No prototype is needed. The design risk is not UI shape or unknown state behavior. Existing code and live evidence already show the route works inside `odin serve` and fails at the nginx ingress allowlist.

## Test And Verification Plan

Local tests:

```bash
go test ./internal/api/http -run 'TestOperationalHandlerBrowserSessionHandoff|TestMobileBrowserAttendedLogin|TestPWAShellContainsMobileApprovalReviewClient' -count=1
go test ./internal/app/lifecycle -run 'TestRunBrowserSession.*Handoff|TestRunBrowserSession.*Runner' -count=1
go test ./internal/runtime/browserhandoff -count=1
make odin-pwa-e2e
```

Route-gate tests should prove:

- `/browser/session/handoff?handoff_id=<id>` is proxied.
- `/browser/session/handoff?format=html&handoff_id=<id>` is proxied.
- `POST /browser/session/handoff/complete` reaches `odin serve` and still requires admin authorization.
- Existing allowed routes still work: `/app/`, `/mobile/*`, `/healthz`, `/readyz`.
- Existing denied routes remain denied: `/metrics`, `/api/*`, `/admin`, arbitrary `/browser/*` paths if not explicitly allowed.

Live proof after deployment:

```bash
which odin
odin browser session list --json
odin browser session login-request --id <x_session_id> --handoff-base-url <live_handoff_base_url> --json
curl -fsS '<live_handoff_url>' | jq '.handoff.status'
curl -fsS -H 'Accept: text/html' '<live_handoff_url>' | grep 'Browser Login Handoff'
curl -fsS <live_base_url>/mobile/review-queue ... # with the existing mobile auth/session path
```

The proof must capture the session ID, login request ID, handoff ID, handoff URL, HTTP status, and the mobile review item. It must also explicitly say no browser was launched.

## Documentation Changes

Update or add:

- `docs/contracts/browser-session-handoff.md`: clarify that the handoff route is now routed on the operator ingress after this slice, but still metadata-only.
- `docs/operations/browser-handoff-runner.md`: add a route proof section before real runner rollout.
- Deployment/runbook docs for the live proxy route allowlist.
- Optional release checklist note for handoff route smoke proof.

No ADR is required. This is not a surprising or hard-to-reverse architecture decision; it is an ingress repair for an already designed Operator Surface.

## Rollout / Migration Notes

- This slice is additive.
- Existing login requests remain valid or expire by their existing status and expiration rules.
- No schema migration is required.
- Deploy requires restarting/recreating the live service container or reloading nginx, depending on the deployed proxy config mechanism.
- Rollback removes the nginx route allowlist and leaves browser session metadata untouched.
- Do not delete browser session rows or encrypted profile artifacts as part of route rollback.

## Open Blockers

No blockers for the route slice.

Known blockers for the next browser-runner slice:

- Decide the private routed viewer origin.
- Wire host-side supervised browser/display/noVNC commands.
- Keep command paths absolute and allowlisted.
- Define cleanup behavior for active/expired/cancelled real runners.
- Decide whether profile capture remains disabled or becomes encrypted-only in that separate slice.

## Planning Handoff

The first implementation goal should only make the existing metadata handoff visible and operable from the live Odin operator surface. It should not start browsers, install browser packages, modify X, persist profiles, or implement authenticated session attachment.

After the route slice is deployed and proven, the next design/implementation slice can handle host-side supervised browser/noVNC launch.

## Codex Goal Handoff

Create Goals

Goal: Implement the browser handoff route slice in `/home/orchestrator/odin-os` using the design at `docs/superpowers/specs/2026-05-15-browser-handoff-route-design.md`.

Outcome:
The existing metadata-only browser handoff route is reachable from the live Odin operator surface. A browser login request can appear in the PWA/mobile review queue, open a routed handoff page, and remain explicit that no browser, credential collection, profile persistence, X login, or X mutation has occurred.

Boundaries:
- Work only inside `odin-os` source/docs/tests and the repo-owned live proxy/deploy config unless evidence shows the current live proxy source lives in the homelab runtime config.
- Reuse existing browser session metadata, login request, handoff lookup, mobile review queue, static handoff HTML, admin-token completion, and nginx/proxy patterns.
- Do not implement real browser/noVNC launch, profile persistence, authenticated browser attachment, automatic login, credential handling, or X profile mutation.
- Keep the route allowlist narrow. Do not expose `/metrics`, `/api/*`, `/admin`, arbitrary `/browser/*`, or unrelated operational endpoints.
- Keep the work PR-sized.

Iteration policy:
- Make atomic commits that each leave the repo coherent.
- After each meaningful change, run the narrowest relevant check.
- If a check fails, diagnose and fix in a follow-up atomic commit.
- Keep a short progress note of what changed, what was verified, and what remains.

Required proof:
- `go test ./internal/api/http -run 'TestOperationalHandlerBrowserSessionHandoff|TestMobileBrowserAttendedLogin|TestPWAShellContainsMobileApprovalReviewClient' -count=1`
- `go test ./internal/app/lifecycle -run 'TestRunBrowserSession.*Handoff|TestRunBrowserSession.*Runner' -count=1`
- `go test ./internal/runtime/browserhandoff -count=1`
- `make odin-pwa-e2e`
- Live post-deploy proof that `odin browser session login-request --id <x_session_id> --handoff-base-url <live_handoff_base_url> --json` creates a handoff URL that returns HTTP 200 through the operator ingress, and that `/mobile/review-queue` contains the browser attended-login item with an openable handoff link.

Delivery:
- Open a PR with `## Summary`, `## Proven`, `## Unproven`, and `## Commands Run`.
- Include a security review section because this touches operator ingress and browser handoff boundaries.
- Monitor remote checks when available.
- Fix check failures in follow-up atomic commits.
- Merge only if checks pass and repo policy permits.
- After merge, deploy to live Odin and capture the live route proof.

Stop condition:
Stop and report blockers if the proxy source of truth is outside repo ownership, mobile auth prevents route proof, implementation would require browser/noVNC launch in the same slice, route exposure cannot remain narrow, or the design would contradict the existing browser-session handoff contract.
