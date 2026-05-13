# Actual-Use Phase 0 Proof

Phase 0 of the actual-use roadmap locks the operator baseline before any runtime feature work:

- prove which installed `odin` binary is live truth
- prove which repo-local `./bin/odin` binary is source-local truth after `make build`
- prove readiness fails closed before `serve`, becomes ready while `serve` owns a controlled runtime root, and fails closed again after shutdown
- report the checkout state before doing implementation work

Use the gated proof script from a clean isolated worktree:

```bash
ODIN_ACTUAL_USE_PHASE0_PROOF=1 ./scripts/ops/actual-use-phase0-proof.sh
```

The script is intentionally opt-in because it starts a short-lived repo-local `./bin/odin serve` process. It uses temporary `ODIN_ROOT` directories and an ephemeral `ODIN_HTTP_ADDR`, so it does not mutate the live runtime root. Installed `odin` commands in the script are isolated proof commands.

## Proof Boundaries

The proof must include:

- `git status --short --branch`
- `git worktree list`
- `which odin`
- `realpath "$(which odin)"`
- installed `odin help`
- `make build`
- repo-local `./bin/odin help`
- controlled-root `./bin/odin healthcheck` before `serve`, expected to fail closed
- controlled-root `./bin/odin serve`
- controlled-root `./bin/odin healthcheck` while `serve` is running, expected to pass
- controlled-root `./bin/odin doctor --json`
- controlled-root `./bin/odin healthcheck` after `serve` stops, expected to fail closed

## Stop Conditions

Stop before claiming Phase 0 proof when:

- `which odin` is missing or does not resolve to the intended installed Odin binary
- repo-local `./bin/odin` was not built from the current checkout
- a fresh controlled runtime root reports ready before `serve`
- readiness does not become ready while `serve` owns the controlled runtime root
- readiness remains ready after `serve` stops
- implementation would edit the dirty primary checkout instead of an isolated worktree
