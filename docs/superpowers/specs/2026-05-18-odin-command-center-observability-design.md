# Odin Command Center Observability Design

## Status

Approved direction: **B. Command Center** from the visual companion on 2026-05-18.

## Objective

Make `odin tui` a human-readable, scannable overview of what is happening inside Odin-OS, and expand the `odin.marcusgoll.com` PWA home page into a clickable operator dashboard with deeper runtime observability.

## Existing State

- `docs/contracts/observability.md` already locks the observer boundary inside `odin serve`.
- `docs/contracts/tui-overview.md` already locks the dashboard language around `Workspace`, `Initiative`, `Work Item`, nested `Run Attempts`, `Companions`, `Approvals`, `Review Queue`, `Observability`, `Memory`, `Intake Inbox`, and `Automation Triggers`.
- `odin tui` is implemented under `internal/cli/tui` and currently reads Prometheus and Loki directly. It renders boxed terminal panels for health, action-required counts, and recent logs.
- The PWA is a static app embedded under `internal/api/http/app_static`. It calls `/mobile/status`, `/mobile/overview`, `/mobile/review-queue`, `/mobile/approvals`, `/mobile/browser/status`, and notification endpoints.
- `/mobile/overview` currently builds the same overview view used by the CLI overview service, but the PWA renders mostly shallow cards and does not provide a consolidated clickable details surface.

## Design

Introduce a shared read-only **Operator Snapshot** assembled inside the Odin HTTP/runtime boundary and consumed by both frontends:

- `odin tui` renders the snapshot as dense terminal panels optimized for SSH.
- `odin.marcusgoll.com/app/` renders the snapshot as an expanded dashboard with clickable rows and detail drawers.

The snapshot is not a new authority. It composes existing runtime truth:

- status/readiness from the existing health/status paths
- action-required totals and lanes from the overview service
- review queue rows from existing mobile/review queue composition
- pending approvals from the existing approval service
- active run attempts, blocked work, recovery guidance, and activity events from existing projections
- browser intervention rows from existing browser/mobile status paths

## Dashboard Shape

The first viewport should answer:

1. What needs operator attention?
2. Is Odin healthy enough to trust right now?
3. What is actively running?
4. What changed recently?

Primary sections:

- **Action Required:** review queue, approvals, failed work, blocked work, browser handoffs.
- **Odin Health:** readiness, health status, telemetry freshness, active run count.
- **Live Execution:** active run attempts with work item key, run ID, attempt, executor, status, and next inspection command.
- **Activity Timeline:** recent event rows with event ID, type, scope, project/work/run/approval identifiers, timestamp, and summary.
- **Details Drawer:** click any row to see source identifiers, payload snippets when already exposed, next commands, allowed actions, and deep links.

## TUI Rendering

`odin tui` remains a fast terminal cockpit:

- Keep `--once`, `--interval`, and `--no-clear`.
- Preserve fail-closed behavior when Prometheus telemetry is unavailable.
- Add separate panels for `ACTION REQUIRED`, `ODIN HEALTH`, `LIVE EXECUTION`, `ACTIVITY`, and `RECENT LOGS`.
- Split important fields into multiple rows instead of truncating action-critical data into a single line.
- Use rune-aware padding/truncation for boxed output.
- Show command hints such as `odin review list`, `odin approvals show <id>`, `odin runs show <id>`, and `odin logs show <event-id>`.

## Web Rendering

The PWA home page becomes an operator dashboard rather than a stack of unrelated cards:

- Keep registration and CSRF behavior unchanged.
- Keep capture as an available operator action, but do not let capture dominate the first viewport.
- Render a two-column desktop layout and a single-column mobile layout.
- Use restrained, dense dashboard styling with neutral colors, one teal accent, no Inter dependency change, no decorative gradients, and no fake data.
- Use buttons/links for detail actions, with inline loading, empty, and error states.
- Implement a details drawer or expandable panel that can show row-specific data without navigating away.

## Non-Goals

- Do not add a separate `odin-observer` daemon.
- Do not make Grafana, Prometheus, Loki, or the PWA canonical runtime authorities.
- Do not add a generic `Agents` or `Processes` dashboard.
- Do not evaluate or fire automation triggers from the dashboard.
- Do not add new external mutation actions beyond existing approval decision paths.

## Success Criteria

- `odin tui --once` produces a scannable dashboard with action-required detail beyond aggregate counts.
- The PWA home page shows the same operator concepts with clickable details.
- Both surfaces reuse existing runtime/projection/read-model services.
- Tests prove the snapshot endpoint, TUI rendering, static PWA shell, and JavaScript API wiring.
- Real repo-owned proof runs through `./bin/odin tui --once` and PWA/mobile HTTP tests.
