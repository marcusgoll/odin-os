# Odin Actual-Use E2E Harness

Run the final MVP proof with:

```sh
make odin-actual-use-e2e
```

The target builds the repo-local `bin/odin` binary and then runs `scripts/odin-actual-use-e2e.sh`.

## Boundary

No live email/calendar/GitHub/production mutation occurs by default. The harness uses:

- A temporary `ODIN_ROOT`.
- A temporary `HOME`.
- A temporary local git project registered through `ODIN_PROJECTS_OVERLAY`.
- The fixture `scripts/drivers/codex-headless.sh` driver through `ODIN_CODEX_DRIVER`.

`ODIN_DRY_RUN` is not set globally because the proof must persist intake, approval, scheduler, work, recovery, and overview state in the temporary store. The mutation boundary is the temp runtime root and temp local git repository.

## Scenarios

1. Binary proof: proves repo-local `bin/odin help`; installed binary is skipped in temp mode with the recorded reason that it may point at a live release.
2. Readiness smoke: runs `doctor --json`, starts `serve`, waits for `healthcheck`, and reads `overview --json`.
3. Raw intake: creates, lists, shows, processes, and reviews a raw intake item.
4. Dedupe: creates a duplicate raw item and proves it is linked to the canonical item while retaining raw payload evidence.
5. Approval gate: starts high-risk Odin work and proves dispatch is blocked on an approval request.
6. Work dispatch: runs safe internal project work through the canonical `codex_headless` executor with the deterministic fixture driver.
7. Delivery loop: runs the `raw-intake-delivery-dry-run` fixture from raw prompt intake through routed Work Item, isolated worktree dispatch, deterministic worker evidence, tmux/session orchestration, specialist reviewer/QA/security handoff, approval resolution, and verified dry-run PR merge.
8. Scheduler: materializes a due trigger once, proves a repeated tick does not duplicate it, and checks trigger audit events.
9. Review queue: proves approvals, failed work/recovery, context-pack artifacts, and intake appear in the single `review list --json` queue.
10. Observability: proves `overview --json` shows intake, triggers, approvals, work, recovery/readiness evidence, and activity from the same temp store.

Artifacts are written under `.odin/actual-use-e2e/`, including `latest.log` and `latest.json`.
