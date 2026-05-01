# Dashboard Admin Hardening

Odin dashboard admin endpoints are operator-only controls. They must not be
exposed raw on an untrusted network.

## Network Exposure

- Bind Odin admin surfaces to localhost or a private management interface.
- Put any remote access behind SSH tunneling or a reverse proxy with TLS.
- Do not publish admin routes directly through an internet-facing tunnel.
- Keep `/healthz`, `/readyz`, and `/metrics` separate from admin actions when a
  reverse proxy routes public status endpoints.

## Admin Token

Set the admin token through the configured environment variable, usually
`ODIN_ADMIN_TOKEN` or the value named by `service.admin_token_env`.

Operational rules:

- Generate the token outside the repository.
- Store the token in the service environment or secret manager, not in Git.
- Rotate the token after operator turnover, accidental exposure, or proxy
  configuration changes.
- Restart or reload the service after rotation so the active process observes
  the new value.

## Access Patterns

Recommended access patterns:

- Local shell on the host running `odin`.
- SSH tunnel from an operator workstation to the localhost-bound service.
- Reverse proxy with TLS, explicit admin route protection, and private upstream
  binding.

Avoid:

- Public unauthenticated admin routes.
- Sharing the admin token in chat, issue bodies, PR bodies, logs, or screenshots.
- Reusing production tokens in local test environments.

## Kill Switch And Audit Expectations

Operators should verify kill-switch behavior before enabling any live worker or
mutation path. Admin actions should be visible through Odin logs, runtime events,
or command output sufficient to reconstruct who initiated the action, what was
requested, and whether it succeeded.

If an admin endpoint behaves unexpectedly:

1. Stop exposing the route externally.
2. Rotate `ODIN_ADMIN_TOKEN`.
3. Capture relevant logs and runtime state.
4. Re-enable access only after the route policy and proxy configuration are
   verified.
