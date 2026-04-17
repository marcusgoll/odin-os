# Mount Mismatch

## Trigger

- expected mount source is absent
- sentinel file is missing
- observed filesystem does not match configured mount expectations

## Evidence

- expected mount source and target
- observed mount source and target
- sentinel-file state
- affected library or download paths

## Safe Actions

- fail closed immediately
- block imports, cleanup, and approved maintenance until the mount audit passes
- open or update a `Critical` incident with the mismatch details

## Approval-Required Actions

- operator-driven remount or storage repair
- import retries after the mount is restored
- any follow-up restart tied to the repaired mount

## Rollback Trigger

- the mismatch follows an approved infrastructure or path change
- post-change validation detects a new mount-source regression

## Closeout

- expected source and sentinel both match
- previously blocked maintenance remains explicitly reviewed before resuming
- data-safety impact is recorded
