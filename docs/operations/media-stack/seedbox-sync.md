# Seedbox or Sync Failure

## Trigger

- remote sync backlog exceeds threshold
- remote or local sync probe fails
- local staging pressure grows because remote sync is stalled

## Evidence

- last successful sync time
- backlog age or queue depth
- remote reachability or auth failure details
- local staging and disk-pressure context

## Safe Actions

- open or update a sync incident
- classify the failure by auth, network, or backlog growth
- keep retry suggestions read-only unless the helper is explicitly allowlisted and stateless

## Approval-Required Actions

- restarting sync helpers
- changing remote paths, credentials, or sync configuration
- replaying or purging queued sync work

## Rollback Trigger

- sync failure begins immediately after an approved helper or path change
- post-change validation introduces new sync backlog or reachability regression

## Closeout

- backlog returns within threshold
- remote reachability or auth is restored
- any approved retry or rollback is captured in the incident
