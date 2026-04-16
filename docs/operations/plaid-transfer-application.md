# Plaid Transfer Application Operations

Use this runbook for the bounded Plaid transfer application workflow exposed by `scripts/drivers/plaid-transfer-application.sh`.

## Purpose

This driver inspects the Plaid dashboard transfer application page, captures a screenshot when the browser lane can provide one, and returns an operator-facing summary of the current state.

## Input

- `action`: use `inspect` for the bounded workflow
- `application_url`: must stay on `https://dashboard.plaid.com/transfer/application`
- `path`: optional screenshot destination; when omitted, the driver uses the repo-local browser state directory

## Output

The driver returns structured JSON with:

- `session_state`
- `current_url`
- `screenshot_path`
- `evidence`
- `next_action`
- `summary`

The same fields are also mirrored under `artifacts` for the browser driver harness.

## State Buckets

- `ready_for_login` -> login required
- `blocked_on_mfa` -> MFA challenge required
- `submitted_for_review` -> review pending
- `already_enabled` -> no further action needed
- `unclassified` -> the dashboard text did not match a known state

## Operator Flow

1. Run the inspect action against the Plaid transfer application URL.
2. Read `summary` first. It gives the operator-facing interpretation of the state.
3. Use `session_state` to branch the next step: login, MFA, review, or enabled.
4. Use `evidence` as the captured page text when the state needs a manual check.
5. Use `screenshot_path` when present to inspect the visual state or attach it to a handoff.
6. Follow `next_action` and keep the workflow bounded to the Plaid dashboard page.

## Operational Notes

- The driver fails closed on non-Plaid URLs.
- Screenshot capture is best-effort; if the browser lane cannot provide a screenshot path, the driver still reports the detected state and evidence.
- `current_url` should remain on the Plaid dashboard transfer application page during a healthy inspect run.
