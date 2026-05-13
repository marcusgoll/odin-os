# Capability Gateway Contract

The capability gateway is Odin OS's provider-neutral discovery and invocation surface.

## Operator Surface

The top-level read-only CLI surface is:

- `odin capabilities list [--kind agent|skill|workflow|command|tool] [--scope <scope>] [--json]`
- `odin capabilities show <id> [--version <version>] [--json]`

JSON responses include:

- `source: capability_gateway`
- `plugin_model: plugins_are_packages_not_runtime_kind`

That plugin model marker is intentional. In v1, `agent`, `skill`, `workflow`,
`command`, and `tool` are runtime capability kinds. A plugin package is a
distribution or install container only; it is not a scheduler kind, approval
kind, executor kind, policy kind, or parallel runtime.

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
- Invocation requires admin authorization through the same admin-token contract
  used by operational mutation routes.
- Invocation requests must include a non-empty caller kind. Empty caller
  identity is default-denied before dispatch, even when the descriptor does not
  require specific permissions.
- Inputs are validated against the descriptor schema before dispatch.
- Permissions are enforced centrally before dispatch.
- Approval-required builtin tools are rejected through the canonical approval
  policy decision before the tool handler runs.
- Results return a canonical run envelope with status, structured output, artifacts, and structured errors.

Read-only discovery routes remain unauthenticated:

- `GET /capabilities`
- `GET /capabilities/{id}`
- `GET /runs/{run_id}`

## Run Lookup

- `GET /runs/{run_id}` returns the persisted run envelope for a previously started invocation.

## Non-Goals

- The gateway does not expose provider-native prompt shaping.
- The gateway does not expose a public capability reload endpoint.
