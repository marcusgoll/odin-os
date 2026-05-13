# GitHub Tracker Live Smoke

Use this opt-in proof only against a disposable repository or disposable issue.
It verifies the existing GitHub tracker adapter can fetch an issue, add an Odin
lifecycle label, add a comment, and keep dry-run mutation paths off the network.
It is not part of normal CI.

## Scope

This proof covers:

- `FetchIssueByID` against the live GitHub Issues API.
- `MarkInProgress`, which posts the `odin:running` label.
- `AddComment`, which posts a timestamped smoke-test comment.
- Dry-run mutation methods, using an unreachable API base URL to prove the
  dry-run path returns before network access.

This proof does not cover:

- closing an issue with `MarkDone`
- creating real follow-up issues
- pull requests, approvals, merges, deployments, or worker dispatch
- production repositories or production issue state

## Token Scope

Use a token scoped only to the disposable repository. The token must be able to:

- read issues
- write issue labels
- write issue comments

Do not use a broadly scoped operator token unless the operator has explicitly
accepted that risk for this disposable proof. Keep the token in `GITHUB_TOKEN`;
do not commit it, paste it into prompts, or include it in PR bodies.

The target repository must already have the `odin:running` label available, or
the label step may fail with a GitHub validation error.

## Run

From the repo root after confirming the target is disposable:

```bash
export GITHUB_TOKEN=<token with issue read/write scope for the disposable repo>
export ODIN_LIVE_GITHUB_TRACKER_SMOKE=1
export ODIN_LIVE_GITHUB_REPO=<owner>/<repo>
export ODIN_LIVE_GITHUB_ISSUE=<disposable issue number>

go test ./internal/tracker/github -run TestLiveGitHubTrackerSmoke -count=1
```

Expected result:

- the test passes
- the disposable issue has an `odin:running` label
- the disposable issue has one `odin live tracker smoke ...` comment
- no real blocked label, dry-run comment, or follow-up issue is created by the
  dry-run subpath

## Stop Conditions

Stop before running the live smoke when:

- the target is not disposable
- `GITHUB_TOKEN` is missing or broader than the operator has accepted
- `ODIN_LIVE_GITHUB_REPO` does not point at the disposable repository
- `ODIN_LIVE_GITHUB_ISSUE` does not point at the disposable issue
- the target repository is missing the `odin:running` label and the operator
  does not want label setup as part of the proof

## Cleanup

After the proof, leave the disposable issue open unless the operator explicitly
wants it closed. Remove the smoke comment or label only through normal GitHub UI
or CLI cleanup for the disposable target; do not add cleanup automation to this
test because accidental cleanup against the wrong issue would be a live
mutation.
