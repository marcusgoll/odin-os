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

## Reconcile Persisted Issues

Use reconciliation after intake has persisted eligible issues and an operator is
ready to materialize those persisted records into Odin Work Items:

```bash
odin work reconcile --project <key>
```

Reconciliation:

- reads eligible rows from SQLite `external_issues`
- creates or reuses deterministic Work Items for those persisted issues
- links the Work Item back to `task_intakes` evidence
- marks reconciled external issue rows with `sync_status=reconciled`
- reports `intake=not_started`, `reconciliation=completed`,
  `dispatch=not_started`, and `prs=not_created`

Reconciliation does not fetch live GitHub data, dispatch workers, create runs,
create branches, create PRs, add comments, or mutate GitHub labels.

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

The reconciliation proof is:

```bash
go test ./internal/cli/commands -run TestRunWorkReconcileCreatesWorkItemsFromPersistedIssuesWithoutDispatch -count=1
go test ./internal/tracker/intake -run TestServiceReconcilesPersistedExternalIssuesIntoWorkItemsIdempotently -count=1
```

These tests prove reconciliation reads persisted issue state, creates one
idempotent Work Item, links intake evidence, and does not create runs or PRs.

## Live Read-Only Proof

Live GitHub access is intentionally not part of normal CI. Use the opt-in
[work-intake-live-smoke.md](work-intake-live-smoke.md) runbook to prove
`odin work intake --project <key> --dry-run` against a disposable GitHub-backed
project.

Do not use a production repository as the first live proof target. The live
proof should use a disposable issue or repository and verify that only read-only
GitHub requests are made.

For tracker-level fetch, label, comment, and dry-run behavior, use the
disposable-target runbook in
[github-tracker-live-smoke.md](github-tracker-live-smoke.md). That proof is
opt-in, mutates only the named disposable issue, and is not part of normal CI.

## Operator Stop Conditions

Stop before running live intake when:

- `which odin` does not resolve to the intended Odin binary
- the project key is not registered or is not GitHub-backed
- `GITHUB_TOKEN` is absent for live GitHub access
- token scope is broader than the operator has accepted
- the run would rely on production state instead of a disposable proof target
- the operator expects intake to directly dispatch workers or create PRs

`odin work intake` is only the read-only issue intake surface. Work Item
creation happens through `odin work reconcile`, while dispatch, worker
execution, PR creation, and GitHub mutation require separate operator surfaces
and proof.
