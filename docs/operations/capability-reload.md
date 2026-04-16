# Capability Reload Operations

Capability reload is an internal snapshot publication flow. Operators should treat it as an atomic swap of the active capability snapshot, not as in-place mutation of the live runtime registry.

## Reload Lifecycle

1. Load and validate the candidate snapshot.
2. Compare it against the active digest.
3. Publish the new snapshot atomically if the active digest is still current.
4. Keep serving the previous snapshot when validation or publication fails.

## Structured Events

- Successful publication emits `capability.snapshot_published`.
- Rejected publication emits `capability.snapshot_rejected`.

These events are the operator-facing source of truth for reload success or failure.

## HTTP and Command Surface

- There is no public CLI or REPL reload command.
- There is no public HTTP reload route.
- Runtime operations remain bounded to the existing machine-facing commands: `odin serve`, `odin healthcheck`, and `odin doctor --json`.
- Capability discovery remains available through `GET /capabilities` and `GET /capabilities/{id}` during reload.
- Invocation remains available through `POST /capabilities/{id}:invoke`.
- Run inspection remains available through `GET /runs/{run_id}`.

## Operator Expectations

- A rejected snapshot does not partially replace the previous snapshot.
- Rejected publications should be investigated via the rejection reason and the associated snapshot diagnostics.
- Bootstrap-only tool and executor catalogs remain fixed runtime inventory until they are replaced by manifest-backed implementations.
