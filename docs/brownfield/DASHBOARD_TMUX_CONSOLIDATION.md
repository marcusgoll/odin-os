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

tmux absence must not prevent `odin serve` from starting.

## Remaining placeholders

- `POST /issues/{id}/pause` and `POST /issues/{id}/resume` are authenticated HTTP hooks, but the `odin serve` admin adapter currently returns `admin_action_not_implemented`. Keep this behavior until issue pause/resume semantics are modeled in runtime state.
- Existing `/healthz` and `/readyz` paths are kept for compatibility. Do not remove them without a migration ticket.
