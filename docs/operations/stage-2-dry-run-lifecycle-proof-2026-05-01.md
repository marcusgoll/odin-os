# Stage 2 Dry-Run Lifecycle Proof - 2026-05-01

## Scope

Stage 2 proves Odin can plan GitHub issue lifecycle mutations without touching GitHub.

## Command

```bash
ODIN_ROOT=<isolated-temp-runtime> \
ODIN_DRY_RUN=true \
GITHUB_TOKEN=<redaction-proof-token> \
./bin/odin work simulate-lifecycle --issue 123 --json
```

## Result

- Project: `odin-core`
- Repo: `marcusgoll/odin-os`
- Issue: `123`
- Dry run: `true`
- GitHub HTTP reads: `0`
- GitHub HTTP writes: `0`
- Dispatch: `not_started`
- PRs: `not_created`
- Codex execution: `not_started`
- Token value in report: `[REDACTED]`

## Planned Actions

1. Add label `odin:running`.
2. Add label `odin:human-review`.
3. Add label `odin:failed`.
4. Add issue comment body: `Stage 2 dry-run lifecycle proof: simulated failure path.`

## Planned Action Logs

- `planned add_label odin:running on marcusgoll/odin-os#123`
- `planned add_label odin:human-review on marcusgoll/odin-os#123`
- `planned add_label odin:failed on marcusgoll/odin-os#123`
- `planned add_comment on marcusgoll/odin-os#123`

## Verification

- The report contains a `method_audit` object with `reads=0` and `writes=0`.
- The report contains `github_writes=0`.
- The report contains four planned lifecycle actions and no blocked, done, close, follow-up issue, PR, scheduler, or Codex execution action.
- The command was run with a token-shaped `GITHUB_TOKEN`; the emitted report contains `[REDACTED]` and does not expose the token value.
