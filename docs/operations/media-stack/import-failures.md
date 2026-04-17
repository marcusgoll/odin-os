# Import Failures

## Trigger

- Arr import failures appear
- completed-download to import lag exceeds threshold

## Evidence

- failing Arr history items
- source and destination paths
- permissions or mount findings
- downloader completion timing

## Safe Actions

- classify failures by path, permission, mount, or category mismatch
- open or update an import-failure incident
- block retries when mount validation is not green

## Approval-Required Actions

- import retries that move files
- path remapping or root-folder changes
- downloader queue mutation tied to failed imports

## Rollback Trigger

- import failures begin after an approved path or service change
- post-change validation shows fresh import regressions

## Closeout

- the failing import cause is documented
- mount and path prerequisites are green
- any approved retry or rollback is linked to the incident
