---
title: Stage 0 Local Fixture Proof
status: passed
date: 2026-04-30
---

# Stage 0 Local Fixture Proof

Stage 0 was run from `/home/orchestrator/odin-os` on branch `codex/serve-lifecycle-cancel-fix`.

## Goal

- No live GitHub.
- No live Codex.
- No PR creation.
- No scheduler dispatch.

## Commands Run

```bash
rm -rf .odin/e2e
rm -f ./bin/odin
make odin-e2e-local
make odin-e2e-contract
make ci
```

Stage 1 and Stage 2 command probes were also run after Stage 0:

```bash
ODIN_DRY_RUN=true ODIN_PROFILE=github-readonly ./bin/odin-os intake github --json
ODIN_DRY_RUN=true ./bin/odin-os tracker simulate-lifecycle --issue 123 --json
go build -o ./bin/odin ./cmd/odin-os
ODIN_DRY_RUN=true ODIN_PROFILE=github-readonly ./bin/odin intake github --json
```

## Results

- `make odin-e2e-local`: passed.
- `make odin-e2e-contract`: passed.
- `make ci`: passed.
- Doctor status: healthy.
- Fixture E2E scenarios: passed.
- E2E artifacts: generated under `.odin/e2e/`.
- GitHub mode: fixture.
- GitHub mutation: false.
- Codex mode: disabled.
- Codex invoked: false.

## Artifacts

- `.odin/e2e/run-metadata.json`
- `.odin/e2e/latest.json`
- `.odin/e2e/latest.log`
- `.odin/e2e/doctor.json`
- `.odin/e2e/failure-analysis.json`
- `.odin/e2e/github-readonly-intake.json`
- `.odin/e2e/prompt-rendering-brownfield.json`
- `.odin/e2e/tracker-dry-run-lifecycle.json`
- `.odin/e2e/workspace-safe-creation.json`

## Exit Criteria

| Criteria | Status | Evidence |
|---|---|---|
| Doctor passes | passed | `.odin/e2e/doctor.json` status `healthy` |
| Fixture E2E passes | passed | Scenario JSON files under `.odin/e2e/` report `status: passed` |
| Artifacts exist | passed | `.odin/e2e/run-metadata.json`, `.odin/e2e/latest.json`, `.odin/e2e/latest.log` |
| No live writes | passed | Scenario reports show `github.mode: fixture` and `github.mutated: false` |
| No live Codex | passed | Scenario reports show `codex.mode: disabled` and `codex.invoked: false` |
| CI passes | passed | `make ci` completed successfully |

## Promotion Status

Stage 0 is complete for this checkout.

Stage 1 is not complete. After Stage 0, the originally probed top-level command
surface was not available:

```text
unknown command: intake
```

Stage 2 is not complete. The requested command surface is not available:

```text
unknown command: tracker
```

Stage 1 has since been mapped to the existing Odin-owned Delivery Workflow
command path: `odin work intake --project odin-core --json`. Do not proceed to
live GitHub proof until that command supports the required Stage 1 JSON output,
external-mutation dry-run semantics with local SQLite persistence, and
zero-write proof artifacts.
