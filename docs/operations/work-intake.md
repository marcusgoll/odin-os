# Work Intake Operations

Use `odin work intake` to sync eligible GitHub issues from a configured
GitHub-backed managed project into Odin's runtime projections.

## Normal Intake

Run intake from an operator shell with the project key:

```bash
odin work intake --project <key>
```

The project must be registered as `project_class: github_backed_project` and
must have a `github.repo` value such as `owner/repo`.

Normal intake:

- fetches open eligible GitHub issues for the project's configured repository
- filters through the existing tracker label rules
- upserts eligible issues into SQLite `external_issues`
- records GitHub as intake/projection state, not runtime authority
- reports `dispatch=not_started` and `prs=not_created`

Normal intake does not:

- create Odin work items
- start scheduler dispatch
- invoke workers
- create branches, commits, PRs, comments, or follow-up issues
- mutate GitHub labels or issue state

## Dry Run

Use dry-run mode before allowing a new project or token environment to persist
anything:

```bash
odin work intake --project <key> --dry-run
```

Dry-run mode still calls the configured tracker and reports the fetched count,
but exits before runtime persistence. A dry-run summary should report
`dry_run=true` and `persisted=0`.

Dry-run mode does not write `external_issues`, create runtime projects, dispatch
workers, create PRs, add comments, or mutate GitHub labels.

## GitHub Token Handling

Live GitHub-backed intake reads the token from `GITHUB_TOKEN` at the tracker
adapter boundary. Keep the token in the operator environment, service
environment, or a machine-local secret manager. Do not place token values in
project manifests, prompts, issue bodies, PR bodies, logs, screenshots, or
worker context.

For read-only intake, use a token with the smallest repository issue-read scope
available for the target repository. The current implementation does not
validate token scopes at startup, so an over-scoped token remains an operator
configuration risk.

The GitHub tracker adapter uses the token only for GitHub API authorization and
redacts token-like values from adapter errors. Worker prompts do not receive the
GitHub token from this path, and `odin work intake` does not start workers.

## Fixture-Backed Verification

The repo-owned fixture proof for the command is:

```bash
go test ./internal/cli/commands -run TestRunWorkIntakeSyncsEligibleIssuesWithoutStartingWork -count=1
```

This test proves that `odin work intake --project <key>` persists an eligible
fixture issue, reports `dispatch=not_started` and `prs=not_created`, and does
not create scheduler tasks.

The service-level dry-run proof is:

```bash
go test ./internal/tracker/intake -run TestServiceDryRunFetchesEligibleIssuesWithoutPersisting -count=1
```

This test proves dry-run fetches eligible fixture issues and leaves
`external_issues` empty.

## Live Read-Only Proof

Live GitHub access is intentionally not part of normal CI. The opt-in live proof
for `odin work intake --project <key>` is tracked by
[issue #60](https://github.com/marcusgoll/odin-os/issues/60). Until that issue
lands, fixture-backed tests are the repo-owned proof and live token scope/API
behavior remains unproven.

Do not use a production repository as the first live proof target. The live
proof should use a disposable issue or repository and verify that only read-only
GitHub requests are made.

## Operator Stop Conditions

Stop before running live intake when:

- `which odin` does not resolve to the intended Odin binary
- the project key is not registered or is not GitHub-backed
- `GITHUB_TOKEN` is absent for live GitHub access
- token scope is broader than the operator has accepted
- the run would rely on production state instead of a disposable proof target
- the operator expects work items, dispatch, workers, or PRs to be created

`odin work intake` is only the read-only issue intake surface. Work creation,
dispatch, worker execution, PR creation, and GitHub mutation require separate
operator surfaces and proof.
