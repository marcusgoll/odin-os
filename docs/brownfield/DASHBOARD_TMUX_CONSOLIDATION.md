# Dashboard and tmux Consolidation

## Current seams

- Canonical dashboard/status HTTP surface: `internal/api/http.NewOperationalHandler`.
- `odin serve` wiring: `internal/app/lifecycle/run.go`.
- Runtime state source: SQLite via `internal/store/sqlite` and read models in `internal/runtime/projections`.
- Existing health endpoints remain supported as `/healthz`, `/readyz`, and `/metrics`.
- `/health` is an alias for `/healthz` for operator/dashboard compatibility.

## Added endpoints

- `GET /health`
- `GET /status`
- `GET /issues`
- `GET /runs`
- `GET /runs/{id}`
- `POST /kill-switch/on`
- `POST /kill-switch/off`
- `POST /issues/{id}/pause`
- `POST /issues/{id}/resume`

Admin endpoints require `Authorization: Bearer <token>` or `X-Odin-Admin-Token`.
The token is loaded from the environment named by `service.admin_token_env`, defaulting to `ODIN_ADMIN_TOKEN`.

## tmux decision

tmux remains an optional visibility/control surface only. Dashboard state must come from runtime state and projections, not terminal scraping.

If no tmux status provider is configured, `/status` returns:

```json
{"tmux":{"available":false,"source":"not_configured"}}
```

`odin serve` configures the optional workspace tmux status provider. It reports only Odin workspace sessions that the existing workspace service recognizes as live, using the session metadata already bound to managed/adopted workspaces. The provider source is `workspace_sessions`; it reports live and attached session counts plus per-session project key, session name, state, facts source, and attach count.

tmux absence must not prevent `odin serve` from starting. If the provider cannot read tmux state, `/status` still returns 200 and reports the provider error under `tmux.error`. Existing tmux/operator scripts remain compatible because `/status` consumes the current workspace service and does not replace or scrape script output as durable authority.

## Remaining placeholders

- `POST /issues/{id}/pause` and `POST /issues/{id}/resume` are authenticated HTTP hooks, but the `odin serve` admin adapter currently returns `admin_action_not_implemented`. Keep this behavior until issue pause/resume semantics are modeled in runtime state.
- Existing `/healthz` and `/readyz` paths are kept for compatibility. Do not remove them without a migration ticket.
