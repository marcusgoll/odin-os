# Capability Gateway Contract

The capability gateway is Odin OS's provider-neutral discovery and invocation surface.

## HTTP Surface

The current public HTTP contract is:

- `GET /capabilities`
- `GET /capabilities/{id}`
- `POST /capabilities/{id}:invoke`
- `GET /runs/{run_id}`

These routes expose the runtime capability snapshot and invocation envelope without leaking transport-specific executor details.

## Discovery

- `GET /capabilities` returns thin capability cards filtered by the active snapshot and optional scope.
- `GET /capabilities/{id}` returns the fully resolved descriptor for one capability version.

## Invocation

- `POST /capabilities/{id}:invoke` accepts the canonical capability invocation request.
- Inputs are validated against the descriptor schema before dispatch.
- Permissions are enforced centrally before dispatch.
- Results return a canonical run envelope with status, structured output, artifacts, and structured errors.

## Run Lookup

- `GET /runs/{run_id}` returns the persisted run envelope for a previously started invocation.

## Non-Goals

- The gateway does not expose provider-native prompt shaping.
- The gateway does not expose a public capability reload endpoint.
