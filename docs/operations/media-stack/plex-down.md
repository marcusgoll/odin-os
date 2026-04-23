# Plex Down

## Trigger

- Plex process or container is down for two consecutive checks
- Plex API is unreachable while the service should be up

## Evidence

- last successful probe time
- process or container state
- Plex API failure details
- active session count
- mount and transcode-path status

## Safe Actions

- open or update a `Critical` incident
- collect health evidence and attach it to the incident
- suppress disruptive maintenance recommendations while active sessions exist

## Approval-Required Actions

- restarting Plex
- changing Plex configuration, mounts, or transcode path
- rollback of a recent approved change

## Rollback Trigger

- Plex became unavailable after an approved maintenance change
- post-change validation introduces a new `Critical` media failure

## Closeout

- Plex probes are healthy again
- operator impact is documented
- any approved change or restart is linked in the incident history
