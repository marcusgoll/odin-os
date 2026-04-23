# VPN or Downloader Failure

## Trigger

- VPN integrity check fails
- downloader binding or connectivity check fails
- downloader queue stops progressing while the VPN dependency is degraded

## Evidence

- VPN probe result
- downloader API reachability
- external egress or bind evidence
- queue depth and stalled item age

## Safe Actions

- fail closed on mutating media actions
- open or update a `Critical` incident
- keep queue classification read-only

## Approval-Required Actions

- restarting VPN or downloader services
- changing network namespace, ports, or bindings
- replaying or mutating queue state

## Rollback Trigger

- the failure follows an approved networking or service change
- post-change validation shows downloader or VPN integrity regression

## Closeout

- VPN integrity and downloader probes are healthy
- any operator-approved restart or rollback is recorded
- stalled queue impact is summarized for follow-up
