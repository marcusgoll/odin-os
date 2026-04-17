# Indexer Degradation

## Trigger

- indexer failure rate exceeds threshold
- repeated auth, DNS, timeout, or captcha failures appear
- search quality degrades enough to affect acquisitions materially

## Evidence

- affected indexers
- grouped failure causes
- last successful query time
- downstream acquisition impact

## Safe Actions

- open or update a degradation incident
- group failures by root-cause class
- keep recommendations read-only

## Approval-Required Actions

- changing indexer configuration
- rotating credentials
- altering networking or provider routing

## Rollback Trigger

- degradation starts after an approved indexer or networking change
- post-change validation introduces new widespread indexer failures

## Closeout

- failures return within baseline
- remaining degraded providers are explicitly accepted or disabled by the operator
- any approved change or rollback is documented
