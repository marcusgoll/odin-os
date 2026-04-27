# Odin OS Agent Rules

## Canonical Repo

Work inside `odin-os`.

`odin-orchestrator` is a migration source only and is being phased out. Do not treat it as the primary implementation target or runtime root unless the user explicitly asks for legacy migration work.

## Reuse and Verification Rules

Before implementing any Odin feature, Codex must first audit the existing repo state.

Required steps:

1. Identify existing commands, services, contracts, registries, schemas, docs, and tests related to the requested feature.
2. Summarize what already exists, what is partial, and what is missing.
3. Prefer extending or repairing existing Odin structures over creating new parallel ones.
4. Do not introduce duplicate abstractions, overlapping command groups, or redundant registries without explicit justification.
5. If a repo-owned `odin` command exists for the target behavior, use it for verification.
6. Verification is incomplete unless the real `odin` command path is exercised where applicable.

Every implementation output must include:

- Existing state found
- Reused components
- New components added
- Why new components were necessary
- Real `odin` command E2E checks performed

## Odin Task Report Format

Every Odin phase or task report must include:

- Current State
- What Already Exists
- Gaps
- Reuse Plan
- New Additions
- Why New Additions Are Necessary
- Real odin E2E Verification
- Remaining Risks
- Best operating rule going forward

## Required Sequence

For Odin tasks, follow this sequence:

1. Audit
2. Verify
3. Reuse
4. Refactor if needed
5. Create only what is missing
6. Prove it through real `odin` commands
