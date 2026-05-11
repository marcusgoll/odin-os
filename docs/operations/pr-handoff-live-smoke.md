# PR Handoff Live Smoke

Use this opt-in proof to verify `odin work pr prepare --live --approval <id>`
can create or update one pull request in GitHub through the real operator
command, with Odin approval and audit evidence.

## Scope

This proof covers:

- a disposable GitHub-backed project supplied through `ODIN_PROJECTS_OVERLAY`
- the real `odin work pr prepare --live` approval request path
- the real `odin approvals resolve` path
- the real `odin work pr prepare --live --approval <id>` handoff mutation path
- `odin work proof` readback of the created or updated PR URL and number
- `odin logs trail` readback of approval and handoff audit events

This proof does not merge pull requests, deploy code, delete branches, mutate
repository settings, create releases, update secrets, or publish follow-up
content beyond the single disposable PR create/update handoff.

## Token Scope

Use a token scoped only to pull request creation/update for the disposable
repository when the provider supports that granularity. Keep the token in
`GITHUB_TOKEN`; do not commit it, paste it into prompts, or include it in PR
bodies.

The disposable repository must already contain the head branch named by
`ODIN_LIVE_PR_HANDOFF_HEAD_BRANCH`. The script does not create branches.

## Run

From the repo root after `make build` and after confirming the target is a
disposable repository:

```bash
export GITHUB_TOKEN=<token with pull request write scope for the disposable repo>
export ODIN_LIVE_PR_HANDOFF_SMOKE=1
export ODIN_LIVE_PR_HANDOFF_REPO=<owner>/<repo>
export ODIN_LIVE_PR_HANDOFF_HEAD_BRANCH=<existing disposable branch>

scripts/ops/pr-handoff-live-smoke.sh
```

Optional overrides:

```bash
export ODIN_LIVE_PR_HANDOFF_PROJECT=live-pr-handoff-smoke
export ODIN_LIVE_PR_HANDOFF_BASE_BRANCH=main
export ODIN_LIVE_PR_HANDOFF_ROOT="$(mktemp -d)"
export ODIN_LIVE_PR_HANDOFF_ODIN="$PWD/bin/odin"
export ODIN_LIVE_PR_HANDOFF_TITLE="Odin live PR handoff smoke"
```

Expected output includes:

```text
project=live-pr-handoff-smoke repo=<owner>/<repo> task=<key> approval=<id> pull_request=<number> url=<url> external_mutated=True pull_request.handoff_prepared=True approval.resolved=True prs=not_merged deploy=not_started
```

## Approval And Mutation Boundary

The proof deliberately performs two PR prepare calls:

1. `odin work pr prepare --live --json` creates an Approval Request and must not
   call GitHub.
2. `odin approvals resolve <id> approve ...` records the operator decision.
3. `odin work pr prepare --live --approval <id> --json` performs the single
   GitHub PR create/update handoff.

The approval authorizes only that one PR handoff. It does not authorize merge,
deploy, branch deletion, release creation, public follow-up comments,
repository settings changes, workflow mutation, or secret mutation.

## Stop Conditions

Do not run the live smoke when:

- the target is not a disposable repository
- the target branch is not disposable
- `GITHUB_TOKEN` is missing or broader than the operator has accepted
- `ODIN_LIVE_PR_HANDOFF_REPO` does not point at the disposable repository
- `ODIN_LIVE_PR_HANDOFF_HEAD_BRANCH` does not already exist in the target repo
- `which odin` does not resolve to the intended installed binary for operator
  proof, or `ODIN_LIVE_PR_HANDOFF_ODIN` does not point at the intended
  repo-built binary for local proof
- the operator has not accepted that this creates or updates one visible PR
  handoff artifact

## Cleanup

The smoke uses a temporary runtime root by default, so Odin runtime cleanup is
normally unnecessary. GitHub cleanup is manual: close the disposable PR and
delete the disposable branch only after confirming no one else is using them.

Normal CI runs only `scripts/tests/pr-handoff-live-smoke-test.sh`, which checks
the opt-in gate and script contract without contacting GitHub.
