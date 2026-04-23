# Marcus Robinhood Live Transfer Runbook

Use this runbook for the principal-attended Robinhood transfer workflow exposed by `./bin/odin`.

## Purpose

This runbook separates deterministic proof from attended live Robinhood use. The deterministic proof must be green before anyone treats the live Family-Ops transfer path as operator-ready.

## Preconditions

- The workflow is principal-attended. Marcus is the current **Finance Principal** and must also be the acting operator for live session reuse, auth, prepare, and final approval.
- Live auth, login, and MFA remain explicit operator checkpoints in the headed session.
- `docs/operations/browser-human.md` preflight should be green before any attended browser work.
- If Odin prints `unknown project: family-ops`, stop and fix registry alignment before claiming Family-Ops live proof.

## Deterministic Proof

Run the deterministic proof first:

```bash
go test ./tests/integration -run 'TestRobinhoodTransferShellFlowDeterministic|TestRobinhoodTransferFlowScript' -count=1
go build -o ./bin/odin ./cmd/odin
```

The deterministic shell proof exercises the real `./bin/odin repl` command path with a fixture driver and proves:

- `/transfer prepare` persists review-ready evidence
- `/approvals resolve` returns the submit run handle
- `/runs show <run-id>` exposes run-local driver artifacts
- submit continuation lands in either confirmed `submitted` or stale-context failure with the locked continuity semantics

The current deterministic integration fixture still uses the checked-in `pbs` project so portable test repos do not depend on a local `family-ops` checkout, but manual operator proof should now run through `family-ops`.

## Attended Live Commands

Only run these after deterministic proof is green and the project registry resolves `family-ops`:

```bash
./bin/odin repl
/project family-ops
/transfer prepare direction=deposit amount_usd=1.00 source_account=checking destination_account=brokerage memo=attended-smoke
/runs show <prepare-run-id>
/approvals resolve <approval-id> approve because attended live confirmation
/runs show <submit-run-id>
```

## Expected Outcomes

- Prepare should print `task=<key> run=<id> approval=<id>` and a `summary=review prepared and awaiting approval` line.
- `/runs show <prepare-run-id>` should expose `artifact=driver_result` with `session_state=review_ready`.
- Approve should print `approval=<id> status=resolved result=approved run=<submit-run-id>`.
- `/runs show <submit-run-id>` should show either:
  - `session_state=submitted` for confirmed Robinhood acceptance
  - or a failed submit run with `session_state=resume_verification_failed` or `session_state=session_expired`, which means fresh prepare is required

## Operational Notes

- Deterministic proof and attended live proof are separate claims. Passing tests do not prove live Robinhood readiness.
- If live auth or MFA appears, Marcus completes it in the headed session; Odin must not background or auto-complete it.
- If continuity cannot be re-proven after approval, the old approval remains historical `approved`, the task becomes blocked with stale context, and the next attempt starts with a fresh prepare.
