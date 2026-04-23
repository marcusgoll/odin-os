---
title: n8n Cutover Playbook
status: active
date: 2026-04-12
---

# n8n Cutover Playbook

This playbook covers the first n8n intake cutover onto `odin-os`. The initial pilot project is `pbs`.

## Goal

Move the `pbs` n8n workflows onto the Odin OS intake path so queued work lands in `odin intake enqueue`, executes under `odin serve`, and no longer relies on legacy `/var/odin/inbox` drops.

## Operator steps

1. import `odin-os` workflow exports into n8n

Use the canonical exports under `ops/n8n/workflows/`:

- `odin-os-dispatch.json`
- `odin-os-sentry-alert.json`
- `odin-os-pbs-ci-alert.json`
- `odin-os-pbs-github-alert.json`
- `odin-os-telegram-bot.json` only if Telegram callback coverage is part of the smoke plan

Import the shared dispatch workflow first, note its workflow ID, then import the dependent workflows with `scripts/ops/import-n8n-workflow.sh --dispatch-id <workflow-id>`.

The shared `Odin OS Dispatch` workflow must use a dedicated odin-os pilot ingress key at `/home/node/.ssh/odin_os_ingress`. Do not repoint the shared legacy `/home/node/.ssh/odin_ingress` key while non-cutover workflows still depend on it.

The pilot workflow exports intentionally keep the existing live webhook paths for `pbs` and Telegram so upstream senders do not need a second endpoint migration:

- `Odin OS PBS CI Alert` reuses `pbs-ci-alert`
- `Odin OS PBS GitHub Alert` reuses `pbs-github-alert`
- `Odin OS Telegram Bot` reuses `odin-telegram-bot`

Telegram still has an activation prerequisite: `Odin OS Telegram Bot` requires `TELEGRAM_WEBHOOK_SECRET`, and the Telegram bot webhook must be configured to send the matching secret token before the `odin-os` workflow is activated.

2. activate only `pbs` workflows first

Keep the pilot narrow:

- deactivate the legacy `PBS CI Alert` and `PBS GitHub Alert` workflows, then activate the matching `Odin OS PBS ...` replacements on the same webhook paths
- leave non-`pbs` legacy workflows active until their own project overlays are cut over
- keep the legacy controller available as rollback only
- do not activate `Odin OS Telegram Bot` until outstanding legacy approval buttons have either been resolved or deliberately abandoned, because the new callback contract accepts `approval-resolve:` only and does not honor legacy `nonce-update` payloads
- keep all non-cutover workflows on the legacy `odin_ingress` key until they have been explicitly migrated to the odin-os pilot key

3. install a dedicated odin-os pilot ingress key on the host

Add a new authorized key entry for the odin-os pilot key and point only that key at `scripts/ops/odin-n8n-ssh-dispatch.sh` so empty `SSH_ORIGINAL_COMMAND` routes normalized stdin envelopes into Odin OS and `approval-resolve` remains available for callback paths.

Keep the legacy `/home/node/.ssh/odin_ingress` key and its forced command untouched until every workflow that still depends on the legacy payload shape has been migrated.

4. trigger manual smoke events

Run at least one manual `pbs` smoke event through each active pilot workflow:

- a `pbs` CI failure webhook
- a `pbs` GitHub alert webhook
- any explicit approval callback path included in the pilot
- confirm each sender is still using the existing webhook URL path and did not require an upstream endpoint change during the pilot
- if Telegram is in scope, confirm the webhook secret token matches `TELEGRAM_WEBHOOK_SECRET` and that no outstanding legacy approval buttons remain
- confirm the active `pbs` pilot workflows are using `/home/node/.ssh/odin_os_ingress` and that non-cutover workflows still use the legacy `/home/node/.ssh/odin_ingress`

5. verify `odin status --json`, `odin jobs --json`, `odin runs --json`

Confirm that:

- the new `pbs` intake creates queued tasks under `odin-os`
- the queue drains under `odin serve`
- the run completes under Odin OS ownership
- no approval backlog or stalled-run drift appears during the smoke window

6. verify no new `/var/odin/inbox/*.json` files appear for `pbs`

The `pbs` pilot is only successful if the workflow path stays off the legacy inbox transport. During smoke verification:

- check the legacy inbox location for new `pbs` task drops
- confirm the Odin OS runtime root does not create a legacy-style `inbox/` handoff directory
- keep rollback ready if intake falls back to legacy transport
