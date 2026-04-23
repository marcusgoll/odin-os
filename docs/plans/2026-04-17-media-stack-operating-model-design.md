---
title: Media Stack Operating Model
status: proposed
date: 2026-04-17
---

# Media Stack Operating Model

## Existing State Fit

`odin-os` already has the generic homelab substrate this design should extend:

- `odin serve`, `odin healthcheck`, and `odin doctor --json`
- `/healthz`, `/readyz`, and `/metrics`
- bounded startup recovery and generic incidents/recoveries
- backup, verify-backup, and restore flows
- a cutover checklist and homelab acceptance path

What is still missing is the media domain itself. There is no current Plex, Arr, downloader, VPN, or seedbox model in `odin-os`, no media-specific config surface, and no media-specific approval or playbook layer. This design therefore adds a bounded operator domain on top of the existing substrate rather than porting orchestrator-era jobs into a parallel stack.

## Design Principles

- Reuse the current `serve`/doctor/metrics/incidents/approvals/backup substrate.
- Keep media automation bounded, inspectable, and policy-first.
- Default to read-only observation until a specific action class is explicitly allowlisted.
- Treat media supervision as one managed operating profile, not as a second runtime authority.
- Keep the solo-operator complexity budget intact: one config surface, one supervisor loop, one incident model, one approval model.

## 1. Odin Role Definition

- `what Odin owns`: continuous observation of the configured media stack; classification of health, degradation, incidents, and maintenance candidates; approval packet creation; pre-change and post-change validation; auditable incident and recovery state through existing `odin-os` health, incident, recovery, and task surfaces.
- `what Odin supports`: operator-assisted restarts, queue triage, import-failure diagnosis, mount validation, disk-pressure triage, indexer and downloader reachability checks, weekly maintenance candidate generation, and rollback-aware validation after approved changes.
- `what Odin must not do alone`: delete media, delete torrents or NZBs, rewrite compose or service configs, rotate credentials, change VPN or downloader networking, remap library roots, expose ports, bulk restart stateful services, or perform restore actions without explicit approval and a recovery path.

## 2. Monitoring Spec

| Signal | Source | Why It Matters | Check Frequency | Severity if Failing | Odin Action |
| --- | --- | --- | --- | --- | --- |
| Container/process liveness for Plex, Radarr, Sonarr, Prowlarr, downloader, VPN, sync helpers | Docker CLI, service manager, shell probe | Detects hard outages and restart loops | 1 min | `Critical` for Plex, downloader, VPN; `High` otherwise | Open or update incident, attach last-known state, suppress unsafe automation |
| Service API reachability | HTTP probe to Plex and Arr/downloader APIs | A running container can still be unusable | 5 min | `Critical` for Plex/downloader; `High` for Arr/Prowlarr | Mark service degraded, capture failing endpoint, recommend restart only through approval |
| Mount presence and expected source | `findmnt`, `stat`, configured sentinel files | Wrong mounts create the highest data-safety risk | 1 min and before any mutation | `Critical` | Fail closed, block imports and changes, escalate immediately |
| Disk free space and inode pressure | `df`, `du`, transcode and download path probe | Prevents stack-wide cascade failures | 5 min | `High` at 80 percent, `Critical` at 90 percent | Open disk-pressure incident, allow temp-only cleanup if explicitly allowlisted |
| Downloader queue growth and stalled age | Downloader API | Detects silent backlog and stuck items | 5 min | `High` | Create queue-hygiene candidate, keep deletions approval-only |
| Failed imports and completed-download lag | Radarr/Sonarr history and queue APIs | Primary signal for path, permission, and mount drift | 5 min | `High`; `Critical` if paired with mount failure | Correlate cause, block retries until prerequisites pass |
| VPN namespace and egress integrity | Shell probe, external IP probe, downloader bind state | Prevents leak-risk and explains stalled acquisition | 2 min | `Critical` | Freeze all mutating media jobs and escalate |
| Indexer success and error rate | Prowlarr API and history | Search quality degrades before total outage | 30 min | `Medium` or `High` | Group failures by cause and recommend operator action |
| Seedbox or sync backlog | Sync logs, queue depth, remote age | Backlog can create storage pressure or missing imports | 15 min | `High`; `Critical` if local disk is threatened | Open sync incident, allow one approved stateless sync retry only if policy allows |
| Plex library freshness and active sessions | Plex API | Avoids disruptive actions during active use | 10 min | `Medium` or `High` | Suppress restart recommendations while streams are active |
| Backup freshness | Backup archive timestamps and verify-backup results | Risky changes need recent recoverability evidence | Daily and before approved changes | `High`; `Critical` before updates | Block maintenance approvals when backup freshness is stale |
| Telemetry freshness for the media domain | Last successful media probe cycle | Stale observation must degrade automation trust | 5 min | `High` | Downgrade media automation to recommend-only and alert |

## 3. Maintenance Job Catalog

| Job | Trigger | Safe Automation Level | Preconditions | Validation | Rollback or Recovery Step |
| --- | --- | --- | --- | --- | --- |
| `media_probe_cycle` | Every 5 minutes in `serve` mode | Read-only auto | Media config exists and profile is enabled | Probe snapshot written, doctor view updated | None |
| `media_mount_audit` | Every minute and before mutating maintenance | Read-only auto | Configured mount roots and sentinels present | Expected source and sentinel both match | Block media actions and open `Critical` incident |
| `media_queue_audit` | Queue age or backlog threshold exceeded | Notify-only | Downloader reachable, mount audit not failing | Candidate report created with stalled-item classes | Operator decides retry, pause, or purge |
| `media_import_audit` | Any failed import or import-lag threshold breach | Notify-only | Arr reachable | Failure set grouped by path, permission, and mount causes | No automatic moves or retries; require approval |
| `media_disk_guard` | Disk threshold crossed | Allowlist auto for temp paths only | Cleanup targets are strictly temp, log, or transcode paths | Space recovers without touching library or downloads | If still pressured, keep incident open and require approval |
| `media_connectivity_audit` | VPN, downloader, or indexer degradation | Read-only auto | Relevant endpoint configured | Root cause classified as auth, DNS, timeout, or network | Freeze risky automation if VPN or downloader integrity fails |
| `media_backup_gate` | Before approved restart/update/change | Read-only auto | Backup archive exists | `verify-backup` passes and archive freshness is within policy | Block change approval until backup is green |
| `media_maintenance_candidate` | Weekly maintenance window | Notify-only | No open `Critical` incidents and backup freshness is green | Candidate task created with preflight evidence | Defer safely; no mutation occurs |
| `media_change_preflight` | Operator requests approved maintenance | Approval-required | Backup gate green, mount audit green, no active Plex streams if disruptive | Explicit preflight packet recorded | Abort change before side effects if any prerequisite fails |
| `media_change_postflight` | Immediately after approved change | Read-only auto | Preflight packet exists | Core probes and smoke checks return healthy or known-degraded | Trigger rollback recommendation if new `Critical` failure appears |

## 4. Incident Playbooks

- `Plex down`: open a `Critical` incident after two failed checks; gather container/process state, API failures, active sessions, mount status, and transcode-path usage; recommend one restart only through approval when no streams are active.
- `disk pressure`: at `High`, attach the largest contributors and temp-path usage; at `Critical`, open an operator-facing incident and allow only strict temp/log/transcode cleanup; never delete library or downloader content automatically.
- `VPN/downloader issues`: if VPN integrity or downloader bind fails, freeze mutating media actions, capture egress evidence, and escalate; do not restart or reconfigure networking automatically.
- `failed imports`: correlate Arr errors with download completion, permissions, and mount state; if mount mismatch is present, upgrade to `Critical` and block retries; otherwise create an approval packet for targeted retry only.
- `mount mismatch`: fail closed immediately; block imports, maintenance, and any path-touching actions until the operator resolves the mount issue and a fresh audit passes.
- `seedbox or sync failure`: classify remote reachability, auth, queue age, and local staging impact; allow one retry only for explicitly stateless sync helpers; keep remote cleanup and replay approval-only.
- `indexer degradation`: batch failures by auth, DNS, timeout, provider, or captcha cause; raise `Medium` unless acquisition is materially blocked; keep credential and provider edits approval-only.

## 5. Approval Policy

- `auto-allowed`: read-only probes, mount audits, queue and import classification, media backup gate checks, preflight evidence gathering, postflight smoke validation, and temp/log/transcode cleanup only on a strict allowlist.
- `notify-only`: weekly maintenance candidate generation, queue-hygiene recommendations, indexer degradation summaries, library audit recommendations, and seedbox backlog summaries.
- `approval-required`: restarting Plex, Arr, downloader, VPN, or sync helpers; changing any config or compose-managed setting; retrying imports that move files; queue mutation; image pulls or service recreates; rollback execution; cleanup outside strict temp/log/transcode paths.
- `forbidden`: automatic media deletion, deleting downloader payloads or volumes, remapping root folders, rotating credentials, exposing or changing ports, changing VPN or downloader network behavior, or silently restoring from backup.

## 6. Recommended Odin Module Layout

- `skills/modules/jobs`: keep one bounded media domain under `internal/core/media` for policy and job classification, `internal/runtime/media` for supervisor cycles and maintenance candidate generation, `internal/runtime/health` for doctor integration, and `internal/telemetry/metrics` for media counters. Reuse existing generic incidents, recoveries, tasks, approvals, and `serve` loop wiring.
- `config boundaries`: add one optional `config/media-stack.yaml` plus typed loading in `internal/app/config`. This file owns service inventory, endpoints, safe maintenance allowlists, mount sentinels, thresholds, maintenance windows, and optional integrations like seedbox or Usenet.
- `secrets handling`: keep API tokens and secrets outside repo-managed config where possible; `config/media-stack.yaml` should reference environment variables or secret handles rather than embedding live credentials.
- `reusable command structure`: do not add a second CLI. Extend existing `odin doctor --json`, `odin healthcheck`, `/healthz`, `/readyz`, `/metrics`, and `serve` behavior so media supervision is visible through the current operator surfaces. Approved maintenance should travel through the existing task and approval model, not a parallel script runner.
- `runbook structure`: place media playbooks under `docs/operations/media-stack/` using one shared template: trigger, evidence, safe automatic actions, approval-required actions, rollback trigger, and closeout criteria.

## 7. 30-Day Rollout Plan

- `week 1`: freeze the media operating contract in docs, add runbooks, and define one typed `config/media-stack.yaml` with explicit safe-vs-unsafe policy boundaries.
- `week 2`: implement read-only probes for mounts, disk, downloader, Plex, Arr, indexers, VPN integrity, and optional seedbox or sync backlog; surface them through doctor and metrics without mutating behavior.
- `week 3`: add a bounded media supervisor loop in `serve` mode that opens and resolves generic incidents, creates weekly maintenance-candidate tasks, and enforces backup freshness gates for operator-approved changes.
- `week 4`: add approval-aware preflight and postflight checks, extend cutover acceptance with real `odin` command verification using fixture-driven media probes, and keep destructive operations forbidden until live evidence justifies any further automation.
