---
title: Media Stack Operations Contract
status: proposed
date: 2026-04-17
phase: "18"
---

# Media Stack Operations Contract

This document defines the bounded media supervision profile Odin OS may operate on top of the existing homelab substrate.

The media profile is optional. It extends the existing homelab substrate exposed through `odin serve`, `odin doctor --json`, `odin healthcheck`, `/healthz`, `/readyz`, `/metrics`, incidents, recoveries, approvals, and backup verification. It must not introduce a second runtime authority or a second automation stack.

## Scope

The media profile covers supervision for services such as:

- Plex
- Radarr
- Sonarr
- Prowlarr
- downloader services
- VPN dependencies
- optional seedbox, Usenet, or sync helpers

Odin supervises those services as operator-owned infrastructure. Odin does not become the source of truth for their application data.

## Contract Goals

- bounded media supervision with inspectable operator state
- explicit safe automatic actions versus approval-required actions
- mount and path safety that fail closed instead of guessing
- predictable incident classification and maintenance candidate creation
- reuse of existing homelab, incident, recovery, approval, and backup surfaces

## Health Model

The media profile may add read-only probes for:

- service reachability
- mount presence and expected mount source
- disk and inode pressure
- queue backlog and stalled downloads
- failed imports and completed-download lag
- VPN integrity and downloader binding
- indexer degradation
- optional seedbox or sync backlog
- backup freshness for approval-gated changes

Media probes must degrade or fail closed when telemetry is stale. Missing or wrong mounts are `Critical`.

## Automation Classes

### Safe automatic actions

Safe automatic actions are read-only or tightly allowlisted operations such as:

- health probes
- queue and import classification
- incident creation or resolution
- maintenance candidate generation
- backup freshness checks
- temp, log, or transcode cleanup on a strict allowlist only

Safe automatic actions must never rewrite configuration, delete media, or change network behavior.

### Approval-required actions

The following actions are approval-required:

- restarting Plex, Arr services, downloader, VPN, or sync helpers
- import retries that move files
- downloader queue mutation
- image pulls, service recreates, or config changes
- cleanup outside explicit temp, log, or transcode allowlists
- rollback execution after a failed change

Approval-required actions must carry preflight evidence, backup freshness, and rollback criteria.

### Forbidden actions

The following actions are forbidden without a future explicit contract change:

- automatic media deletion
- deleting downloader payloads or persistent volumes
- remapping library roots or root folders
- rotating credentials
- exposing or changing ports
- changing VPN or downloader network behavior
- silent restore from backup

## Safety Rules

- Mount validation must fail closed before any import retry, cleanup, or approved maintenance step.
- Odin must not treat a merely mounted path as safe; expected source and sentinel checks are required.
- Approval-required maintenance must stop when backup freshness is stale or missing.
- A failed post-change check must generate a rollback recommendation instead of continued blind automation.

## Incident Expectations

Every media incident playbook must define:

- trigger
- evidence
- safe automatic actions
- approval-required actions
- rollback trigger
- closeout

The playbooks live under `docs/operations/media-stack/`.
