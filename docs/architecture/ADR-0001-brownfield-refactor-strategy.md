---
title: ADR-0001 Brownfield Refactor Strategy
status: accepted
date: 2026-04-30
---

# ADR-0001: Brownfield Refactor Strategy

## Context

Odin OS is already a functioning Go-based orchestration system. It has a real `odin` binary, lifecycle composition, SQLite runtime state, registry assets, executor contracts, worktree leases, recovery, health checks, tests, systemd deployment files, scripts, and migration documentation.

The repository also contains partial or uncommitted scaffold work that can conflict with the established architecture, including duplicate runner/config seams and an accidental TypeScript scaffold. Future work must preserve working behavior while migrating toward the target architecture.

## Decision

Odin OS will use a brownfield refactor strategy:

1. Explore existing behavior before editing.
2. Prefer modifying existing modules over creating duplicate modules.
3. Use `cmd/odin`, `internal/app/lifecycle`, `internal/store/sqlite`, `internal/executors`, `internal/vcs`, and `registry` as the default architectural center.
4. Do not create new shims unless no existing integration point works.
5. Do not rename, move, or delete files without a migration reason.
6. Do not delete existing skills, agents, shims, scripts, or registry assets without inventory and approval.
7. Add characterization tests before refactoring risky behavior.
8. Keep refactors small and reviewable.
9. Maintain backward compatibility unless a ticket explicitly removes it.
10. Record future architectural decisions in `docs/architecture/ADR-*.md`.
11. Require security review for changes to runners, shims, filesystem operations, process execution, GitHub tokens, or secrets.

## Keep / Refactor / Replace / Remove Policy

- **Keep** working modules and extend them through existing interfaces.
- **Refactor** useful but messy modules while preserving behavior.
- **Replace** only when refactoring cannot satisfy the target architecture safely.
- **Remove** only after inventory, approval, and proof that useful behavior or knowledge is preserved elsewhere.

The initial classification source is `docs/brownfield/COMPONENT_INVENTORY.md`.

## Security Review Triggers

Security review is required for changes touching:

- runner or executor launch behavior
- Codex CLI or app-server integration
- shell scripts, subprocesses, shims, or process management
- filesystem mutation, worktree cleanup, backup, or restore
- GitHub tokens, API calls, issue mutation, comments, labels, or pull requests
- secrets, credentials, environment files, or deployment files
- worker sandboxing, approval policy, or command allowlists

## Verification

Go changes must run the relevant targeted tests and, when feasible:

```bash
go fmt ./...
go vet ./...
go test ./...
go build ./cmd/odin-os
```

User-visible and orchestration-facing changes require real `odin` command proof against a controlled `ODIN_ROOT`.

## Consequences

Positive:

- Reduces duplicate abstractions and accidental greenfield rewrites.
- Preserves working runtime behavior while improving locality.
- Makes removal and replacement decisions auditable.
- Keeps security-sensitive changes visible.

Negative:

- Migration will be slower than a rewrite.
- Some compatibility naming, such as `tasks` / `runs` versus Work Items / Run Attempts, will persist until explicitly migrated.
- Workers must do more up-front audit work before making changes.

## Rejected Alternatives

### Greenfield Rewrite

Rejected because Odin OS already has valuable working runtime behavior, tests, and operator surfaces.

### Permanent Compatibility Shims

Rejected because shims are useful only as time-bounded migration aids. Permanent shims create unclear authority and duplicate behavior.

### GitHub Or Runner As Runtime Authority

Rejected because accepted Odin guidance keeps SQLite as runtime authority, GitHub as intake/projection, and executors as lanes rather than owners of durable state.
