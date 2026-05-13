# Work Intake Live Smoke

Use this opt-in proof to verify `odin work intake --project <key>` can fetch
eligible issues from GitHub through the real operator command without mutating
GitHub or starting work.

## Scope

This proof covers:

- a disposable GitHub-backed project supplied through `ODIN_PROJECTS_OVERLAY`
- the real `odin work intake --project <key> --dry-run` command
- live GitHub issue fetch through the existing tracker adapter
- a dry-run summary with `persisted=0`, `dispatch=not_started`, and
  `prs=not_created`

This proof does not cover:

- labels, comments, or other GitHub mutation paths
- live persistence into a long-lived Odin runtime
- work item creation, scheduler dispatch, workers, branches, PRs, merges, or
  deployments

For tracker-level label/comment mutation proof, use
[github-tracker-live-smoke.md](github-tracker-live-smoke.md) against a
disposable issue.

## Token Scope

Use a token scoped only to issue reads for the disposable repository when the
provider supports that granularity. Keep the token in `GITHUB_TOKEN`; do not
commit it, paste it into prompts, or include it in PR bodies.

The disposable repository must contain at least one open issue labeled
`odin:ready`. Issues labeled `odin:blocked` are filtered out by the tracker
adapter.

## Run

From the repo root after `make build` and after confirming the target is
disposable:

```bash
export GITHUB_TOKEN=<token with issue read scope for the disposable repo>
export ODIN_LIVE_WORK_INTAKE_SMOKE=1
export ODIN_LIVE_WORK_INTAKE_REPO=<owner>/<repo>

scripts/ops/work-intake-live-smoke.sh
```

Optional overrides:

```bash
export ODIN_LIVE_WORK_INTAKE_PROJECT=live-intake-smoke
export ODIN_LIVE_WORK_INTAKE_ROOT="$(mktemp -d)"
export ODIN_LIVE_WORK_INTAKE_ODIN="$PWD/bin/odin"
```

Expected output includes:

```text
project=live-intake-smoke repo=<owner>/<repo> fetched=<n> persisted=0 dry_run=true dispatch=not_started prs=not_created
```

`fetched` must be at least `1`; otherwise create a disposable open issue labeled
`odin:ready` and rerun.

## Read-Only Check

The proof stays read-only by construction:

- the command uses `--dry-run`
- the overlay project is temporary and does not edit `config/projects.yaml`
- the command path is `odin work intake`, which calls `FetchEligibleIssues`
- the script asserts `persisted=0`, `dispatch=not_started`, and
  `prs=not_created`

Normal CI runs only `scripts/tests/work-intake-live-smoke-test.sh`, which checks
the opt-in gate and script contract without contacting GitHub.

## Stop Conditions

Stop before running the live smoke when:

- the target is not disposable
- `GITHUB_TOKEN` is missing or broader than the operator has accepted
- `ODIN_LIVE_WORK_INTAKE_REPO` does not point at the disposable repository
- the disposable repository has no open `odin:ready` issue
- `which odin` does not resolve to the intended installed binary for operator
  proof, or `ODIN_LIVE_WORK_INTAKE_ODIN` does not point at the intended
  repo-built binary for local proof

## Cleanup

The smoke uses a temporary runtime root by default and performs no GitHub writes,
so there is no GitHub cleanup. If `ODIN_LIVE_WORK_INTAKE_ROOT` points at a
non-temporary runtime, inspect it before deleting anything.
