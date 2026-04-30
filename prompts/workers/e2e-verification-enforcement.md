---
role: e2e-verification-enforcement
status: scaffold
prompt_kind: implementation
requires_acceptance_criteria: true
---

You are the Odin-OS E2E verification enforcement agent.

This is a brownfield Odin-OS repository.

Goal:
Make Odin-OS E2E verification mandatory and easy for Codex, humans, and CI.

Brownfield rules:
- Inspect existing implementation before editing.
- Reuse existing doctor, test, CLI, scripts, and CI code.
- Do not create duplicate E2E systems.
- Do not remove existing checks.
- Preserve existing behavior unless explicitly documented.
- Keep the diff small.
- Human approval is required before merge.

Read:
- AGENTS.md
- WORKFLOW.md
- Makefile
- Existing scripts/
- Existing cmd/odin-os CLI code
- Existing internal doctor/diagnostics code
- Existing e2e/test/fixture code
- Existing GitHub Actions workflows
- Existing docs

Implement:
1. A canonical local E2E command:
   - make odin-e2e-local
2. A script:
   - scripts/odin-e2e-local.sh
3. E2E artifact output:
   - .odin/e2e/latest.log
   - .odin/e2e/latest.json where applicable
   - .odin/e2e/run-metadata.json
4. AGENTS.md update requiring Codex to run make odin-e2e-local.
5. WORKFLOW.md update making make odin-e2e-local part of definition of done.
6. CI workflow or CI update that runs the same command.
7. If odin e2e command does not exist:
   - add a minimal fixture-backed odin e2e command,
   - or document it as a blocker and make the script fail clearly.
8. Update docs explaining how Codex should run Odin E2E checks.

Acceptance criteria:
- `make odin-e2e-local` exists.
- `make odin-e2e-local` runs go fmt, go vet, go test, build, and Odin doctor.
- If `odin e2e` exists, the command runs at least one fixture-backed E2E scenario.
- If `odin e2e` does not exist, the command fails clearly with a follow-up ticket recommendation.
- E2E artifacts are written under `.odin/e2e/`.
- `.odin/e2e/` is gitignored except optional fixture docs.
- AGENTS.md requires Codex to run the E2E command.
- WORKFLOW.md includes the E2E command in definition of done.
- CI runs the same E2E command.
- No live GitHub mutation occurs in local E2E.
- No live Codex worker execution occurs unless explicitly configured.
- Existing tests pass.

Required final output format:

## Current State
Describe what existed before this change.
Reference actual files inspected.

## Existing Code Reused
List exact files, packages, functions, scripts, configs, or docs reused.

## New Code Added
List exact new files added and why.

## Files Changed
List every changed file grouped by purpose.

## Canonical Architecture Decision
State the canonical verification command after this change.
State whether any duplicate verification paths remain.

## Acceptance Criteria Status
Use this table:

| Acceptance Criteria | Status | Evidence |
|---|---|---|
| Criteria text | passed / failed / not verified | File, test, command, or explanation |

## Tests and Verification
Use this table:

| Command | Result | Notes |
|---|---|---|
| go fmt ./... | passed / failed / not run | Explanation |
| go vet ./... | passed / failed / not run | Explanation |
| go test ./... | passed / failed / not run | Explanation |
| go build -o ./bin/odin ./cmd/odin-os | passed / failed / not run | Explanation |
| make odin-e2e-local | passed / failed / not run | Explanation |

## Behavior Changes
State whether runtime behavior changed.

## Security and Safety Notes
State whether this touched secrets, tokens, GitHub writes, process execution, filesystem, Codex sandbox settings, deployment, or CI.

## Remaining Risks
List unresolved risks with mitigation.

## Follow-up Tickets
List concrete follow-up tickets.

## Go/No-Go Recommendation
State GO, NO-GO, or CONDITIONAL GO.

Bottom line:

After the 20 prompts, your next job is not "add more agents."

It is:

- Freeze a baseline.
- Create one canonical Odin E2E command.
- Make Codex run it.
- Make CI enforce it.
- Run staged operational proof.
- Only then allow 24/7 supervised operation.

The most important command in the repo should become:

```bash
make odin-e2e-local
```

Once Codex, humans, and CI all run that same command, rely on repeatable verification over narrative summaries. The machine checks the machine.

Required verification:
- Run go fmt ./...
- Run go vet ./...
- Run go test ./...
- Run go build -o ./bin/odin ./cmd/odin-os
- Run make odin-e2e-local

If make odin-e2e-local cannot run, explain:
1. why it could not run,
2. whether the failure is caused by this change,
3. whether the PR is safe to merge without it,
4. the exact follow-up ticket required.

Do not claim completion without reporting the result.

E2E report:
.odin/e2e/run-metadata.json
.odin/e2e/latest.json
.odin/e2e/latest.log
