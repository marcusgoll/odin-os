# Dashboard Admin Hardening

Odin dashboard admin endpoints are operator-only controls. They must not be
exposed raw on an untrusted network.

Current admin mutation endpoints include:

- `POST /kill-switch/on`
- `POST /kill-switch/off`
- `POST /issues/{id}/pause`
- `POST /issues/{id}/resume`

The issue pause/resume routes are authenticated but currently return
`admin_action_not_implemented` from `odin serve`; keep them protected anyway so
future implementation work does not inherit a public mutation route.

When implemented, issue pause/resume must follow
`docs/contracts/pause-resume.md`: dashboard routes are adapters over
Odin-owned SQLite runtime state, GitHub `odin:paused` labels are projection-only,
and generic resume may only clear the `operator_paused` blocked reason.

## Network Exposure

- Bind Odin admin surfaces to localhost by default, for example
  `ODIN_HTTP_ADDR=127.0.0.1:9444`.
- Put any remote access behind SSH tunneling or a reverse proxy with TLS.
- Do not publish admin routes directly through an internet-facing tunnel.
- Keep `/healthz`, `/readyz`, and `/metrics` separate from admin actions when a
  reverse proxy routes public status endpoints.
- Treat any listener on `0.0.0.0`, a LAN address, or a tunnel hostname as an
  exception that needs a reviewed access-control layer before it is used.

## Admin Token

Set the admin token through the configured environment variable, usually
`ODIN_ADMIN_TOKEN` or the value named by `service.admin_token_env`.

Recommended setup:

```bash
install -d -m 700 ~/.config/odin
cp deploy/systemd/odin-os.env.example ~/.config/odin/odin-os.env
chmod 600 ~/.config/odin/odin-os.env
python3 - <<'PY'
import secrets
print("ODIN_ADMIN_TOKEN=" + secrets.token_urlsafe(32))
PY
```

Paste the generated `ODIN_ADMIN_TOKEN=...` line into the machine-local env file
only. If a config file sets `service.admin_token_env` to another environment
variable name, put the token in that named environment variable instead.

Authenticated requests may use either header form:

```bash
curl -fsS -X POST \
  -H "Authorization: Bearer $ODIN_ADMIN_TOKEN" \
  http://127.0.0.1:9444/kill-switch/on

curl -fsS -X POST \
  -H "X-Odin-Admin-Token: $ODIN_ADMIN_TOKEN" \
  http://127.0.0.1:9444/kill-switch/off
```

Operational rules:

- Generate the token outside the repository.
- Store the token in the service environment or secret manager, not in Git.
- Keep local env files owner-readable only, for example `chmod 600`.
- Rotate the token after operator turnover, accidental exposure, or proxy
  configuration changes.
- Restart or reload the service after rotation so the active process observes
  the new value.
- After rotation, verify the old token is rejected and the new token succeeds.

## Access Patterns

Recommended access patterns:

- Local shell on the host running `odin`.
- SSH tunnel from an operator workstation to the localhost-bound service.
- Reverse proxy with TLS, explicit admin route protection, and private upstream
  binding.

SSH tunnel example:

```bash
ssh -N -L 19444:127.0.0.1:9444 odin-host
curl -fsS -X POST \
  -H "Authorization: Bearer $ODIN_ADMIN_TOKEN" \
  http://127.0.0.1:19444/kill-switch/on
```

Reverse proxy expectations:

- Terminate TLS at the proxy.
- Require operator authentication before forwarding admin routes.
- Forward admin routes only to a loopback or private upstream.
- Keep admin tokens in headers; never put tokens in URLs, query strings, access
  logs, issue bodies, or screenshots.
- Prefer separate routing rules for read-only health/metrics and admin mutation
  paths so public status checks cannot reach mutation endpoints.

Avoid:

- Public unauthenticated admin routes.
- Sharing the admin token in chat, issue bodies, PR bodies, logs, or screenshots.
- Reusing production tokens in local test environments.

## Kill Switch And Audit Expectations

Operators should verify kill-switch behavior before enabling any live worker or
mutation path. Admin actions should be visible through Odin logs, runtime events,
or command output sufficient to reconstruct who initiated the action, what was
requested, and whether it succeeded.

Expected kill-switch behavior:

- `POST /kill-switch/on` returns `{"status":"accepted","action":"kill_switch_on"}`
  when authenticated and the admin adapter is available.
- The service writes a readiness flag with reason `dashboard kill switch
  enabled`.
- Runtime state is marked degraded when runtime-state storage is available.
- Readiness should fail closed while the kill switch is active.
- A `dashboard_admin` log event records `kill switch enabled`.
- `POST /kill-switch/off` clears the readiness flag and records `kill switch
  disabled`.

Operational proof checklist:

```bash
curl -fsS http://127.0.0.1:9444/readyz
curl -fsS -X POST \
  -H "Authorization: Bearer $ODIN_ADMIN_TOKEN" \
  http://127.0.0.1:9444/kill-switch/on
curl -fsS http://127.0.0.1:9444/readyz || true
curl -fsS -X POST \
  -H "Authorization: Bearer $ODIN_ADMIN_TOKEN" \
  http://127.0.0.1:9444/kill-switch/off
curl -fsS http://127.0.0.1:9444/readyz
```

If an admin endpoint behaves unexpectedly:

1. Stop exposing the route externally.
2. Rotate `ODIN_ADMIN_TOKEN`.
3. Capture relevant logs and runtime state.
4. Re-enable access only after the route policy and proxy configuration are
   verified.
