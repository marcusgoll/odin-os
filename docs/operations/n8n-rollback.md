---
title: n8n Rollback Playbook
status: active
date: 2026-04-12
---

# n8n Rollback Playbook

Use this playbook when odin-os intake becomes degraded after an n8n workflow cutover.

## Rollback triggers

Roll back the n8n plane when any of the following are true:

- `odin-os` intake becomes degraded
- Telegram approval callbacks stop resolving live Odin approvals
- pilot workflows start dropping work back to legacy inbox paths
- operator verification cannot explain which workflow export is currently primary

## Rollback sequence

1. disable the dedicated odin-os pilot ingress key on the ingress host or restore its old forced-command entry if one existed
2. restore the old Telegram workflow activation state in n8n by deactivating `Odin OS Telegram Bot` and reactivating `Odin Telegram Bot`
3. restore the old workflow exports for the pilot workflows you just moved from the legacy export snapshot
4. reactivate the legacy approval callback workflow if the Telegram path is no longer trustworthy
5. keep the Odin OS runtime evidence and compare it with the restored legacy flow before attempting another cutover

Because the pilot `pbs` and Telegram replacements reuse the existing webhook paths, do not leave both the legacy and `odin-os` variants active at the same time for the same path.

Rollback must preserve the legacy `/home/node/.ssh/odin_ingress` key path for non-cutover workflows and remove only the dedicated `/home/node/.ssh/odin_os_ingress` pilot path from service.

## Validation after rollback

After rollback:

- confirm the old SSH forced-command entry for the `odin_n8n` key is active again
- confirm `Odin OS Telegram Bot` is inactive and `Odin Telegram Bot` matches the pre-cutover activation state
- confirm the old workflow exports are active for the affected workflows from the legacy snapshot
- confirm no further Odin OS intake traffic is expected until the degraded condition is resolved
