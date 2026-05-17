# Marcus Brand OS Runtime Plan

## Goal

Implement the first Odin-side runtime slice for the Marcus Personal Brand Operating System so Odin can create recurring, throughout-the-day brand work without inventing a second daemon or bypassing approval gates.

## Approval Source

Marcus requested: "Ok lets now implement this into odin-os so that we can have my virtual self run continuously throughout the day."

## Existing State

- `odin serve` is already the always-on control plane.
- `odin scheduler tick` and `odin trigger ...` already materialize scheduled Work Items.
- Trigger rule JSON already supports typed `skill_invocation` bindings.
- Skill invocation Work Items are reviewable and run through `odin skills run <task>`.
- Higher-risk trigger work is blocked by approval policy.
- `registry/workflows/marcus-social-growth-workflow.md` and the Marcus social companion skills already define the social copilot lane.
- `internal/runtime/socialcopilot` already runs bounded no-account-action social wakes when enabled.
- `marcusgoll` now defines the broader Personal Brand Operating System and Marcus Teaching Voice as the brand authority.

## Reuse Plan

- Reuse `triggers.Service`, SQLite automation triggers, scheduler tick, and skill invocation bindings for recurring brand routines.
- Reuse the existing Marcus social workflow and social approval memory model for public social outputs.
- Reuse registry skills as the durable role contracts for missing brand lanes.
- Keep `odin serve` as the only continuous daemon.

## New Additions

1. Add missing brand-lane registry skills:
   - Marcus editorial strategist
   - Marcus writing partner
   - Marcus resource producer
   - Marcus newsletter editor
   - Marcus marketing planner
   - Marcus growth analyst
   - Marcus distribution coordinator
2. Add a `marcus-personal-brand-operating-system` workflow that composes existing social skills plus the new broader brand skills.
3. Add `odin trigger seed marcus-brand-os ...` to install a safe default set of recurring brand trigger bindings.
4. Add tests proving the seed command creates enabled schedule triggers with skill-invocation Work Items and no immediate handler execution.
5. Update operations docs with the exact command path for enabling and proving the routine.

## Runtime Boundaries

- The seed command may create internal scheduled work only.
- Public posts, replies, emails, and publishing remain approval-gated.
- The slice must not automate LinkedIn browser actions, likes, reposts, follows, DMs, or bulk posting.
- The slice must not add a new queue, daemon, external worker, approval table, or scheduler.

## Verification

- `go test ./internal/cli/commands ./internal/runtime/triggers ./internal/app/lifecycle ./internal/registry/...`
- `make build`
- `which odin`
- `ODIN_ROOT=<tmp> ./bin/odin trigger seed marcus-brand-os start=2026-05-18 --json`
- `ODIN_ROOT=<tmp> ./bin/odin trigger test marcus-brand-morning-editorial-scan now=2026-05-18T12:30:00Z --json`
- `ODIN_ROOT=<tmp> ./bin/odin scheduler tick now=2026-05-18T12:30:00Z recovery=false --json`
- `ODIN_ROOT=<tmp> ./bin/odin jobs --json`
