# Odin OS Capability Platform Design

## Objective

Refactor Odin OS around a first-class capability platform so skills, agents, commands, workflows, and tools can be defined, discovered, versioned, loaded, and invoked dynamically without hardcoding behavior into the CLI, broker, or provider adapters.

The immediate goal is to make the capability model explicit and executable. The longer-term goal is to give Codex, Claude Code, and future external coding agents one stable interoperability surface that does not force provider-specific assumptions into the Odin core.

## Current Context

The April 16, 2026 Odin OS runtime already has several strong foundations:

- SQLite is the canonical runtime authority.
- Runtime mutations are evented and auditable.
- Project governance, task-owned worktrees, and recovery boundaries are intentionally conservative.
- Executors are already abstracted behind a router and contract layer.

The main architectural weakness is the capability plane.

Today:

- `registry/` content is parsed and compiled at bootstrap, but the capability snapshot is not retained as a long-lived runtime service.
- Commands and workflows are documented in registry content, but they are not the live execution surface for CLI or external integrations.
- Tool and executor catalogs still rely heavily on hardcoded registrations.
- Capability metadata is too thin for dynamic interoperability because it lacks typed schemas, versioning, dependencies, lifecycle semantics, and execution policy.
- External agent integration is partial and provider-specific rather than driven by a stable Odin-owned gateway.

That means Odin has good runtime infrastructure, but not yet a complete modular capability architecture.

## Goals

- Make the authored capability set real at runtime through a long-lived capability registry service.
- Define one normalized contract for `tool`, `skill`, `agent`, `command`, and `workflow`.
- Make commands and workflows executable rather than informational metadata.
- Support safe dynamic reload through immutable capability snapshots.
- Add a provider-neutral capability gateway that all transports and agent adapters consume.
- Standardize invocation metadata, validation, permissions, status, errors, artifacts, and lifecycle handling.
- Keep the Odin core independent from Codex- or Claude-specific behavior.

## Non-Goals

- No attempt to make arbitrary untrusted code plugins hot-load directly into the process in this phase.
- No forced unification of all runtime concepts into a single over-abstracted "plugin" type.
- No replacement of SQLite, the event model, or project governance boundaries.
- No provider-specific prompt tuning or UI behavior in the core contract.
- No removal of all legacy hardcoded command/tool paths in one unsafe cutover; short-lived bootstrap fallbacks are acceptable during migration.

## Approaches Considered

### 1. Interop-first adapter layer over the current runtime

Expose the current command/tool surface through MCP or HTTP first, then normalize the capability model later.

Pros:

- fastest visible integration for external agents
- low short-term disruption

Cons:

- codifies current architectural inconsistencies into a public interface
- makes later cleanup more expensive
- keeps commands/workflows partially inert

### 2. Runtime-supervision-first hardening

Prioritize timeouts, retries, resume, and circuit-breaker behavior around the current scheduler and executor flow before changing the capability model.

Pros:

- improves operational safety quickly
- reduces risk around long-running agent execution

Cons:

- does not solve the fragmented discovery and invocation model
- preserves hardcoded coupling between transports and capability implementations

### 3. Capability-platform foundation first

Recommended.

Add a persistent capability registry service, normalize manifest contracts, make commands and workflows executable, and place a provider-neutral gateway in front of the capability plane. Then layer transport, interoperability, and supervision improvements on top.

Pros:

- fixes the root architectural problem instead of its symptoms
- gives all later work one stable contract
- reduces future migration cost for Codex, Claude Code, and additional providers

Cons:

- requires touching bootstrap, registry, transport, and execution code together
- more design-heavy up front than an adapter-only solution

## Recommended Design

Use a three-layer architecture:

1. Stable core
2. Capability plane
3. Edge adapters

### Stable Core

The stable core remains responsible for:

- runtime state and persistence
- eventing and projections
- project governance
- policy enforcement
- orchestration and scheduling
- supervision, checkpoints, and recovery
- VCS mutation safety

The core should not know about Codex or Claude-specific invocation patterns. It should also avoid transport-specific command parsing.

### Capability Plane

Add a new `internal/core/capabilities` package that owns the live capability model.

Responsibilities:

- retain the active compiled capability snapshot
- expose snapshot metadata, digest, and diagnostics
- resolve capability identity and version selection
- validate dependency references
- provide discovery methods for transports and adapters
- mediate capability invocation through a provider-neutral gateway

`internal/registry` remains the authored-manifest ingestion layer:

- scan
- parse
- validate
- compile

It should not own the runtime lifecycle of the active snapshot.

Recommended package split:

- `internal/core/capabilities`
- `internal/core/commands`
- `internal/core/workflows`
- `internal/transports/cli`
- `internal/transports/http`
- future `internal/transports/mcp`

### Normalized Capability Contract

Every capability compiles into one normalized envelope, regardless of whether the source is Markdown, YAML frontmatter, or a generated descriptor.

Required fields:

- `apiVersion`
- `kind`
- `name`
- `version`
- `title`
- `summary`
- `availability`
- `permissions`
- `inputSchema`
- `outputSchema`
- `dependencies`
- `implementation`
- `execution`
- `compatibility`
- `labels`
- `owner`

Semantic split:

- `tool`: atomic action adapter
- `skill`: reusable context/procedure asset
- `agent`: executable worker profile with executor constraints and tool permissions
- `command`: transport-facing entrypoint
- `workflow`: composition over commands, agents, skills, and tools

Markdown remains an implementation asset. It is not the machine interface contract.

### Registry Publication and Dynamic Loading

Dynamic extensibility should use immutable snapshot publication.

Reload flow:

1. scan authored manifests
2. parse and normalize
3. validate schemas, scopes, permissions, and dependency references
4. resolve versions and implementation references
5. preflight executable implementations where possible
6. compile an immutable `CapabilitySnapshot`
7. publish atomically
8. emit diagnostics and publication events

Rules:

- the active snapshot is read-only
- reload never mutates the active snapshot in place
- failed reloads do not corrupt the last good snapshot
- snapshot publication and rejection must be observable in structured events

### Execution Lifecycle

Capability invocation should follow one lifecycle:

- `prepare`
- `invoke`
- `stream`
- `checkpoint`
- `resume`
- `cancel`
- `finalize`

The invocation path must centralize:

- schema validation
- scope resolution
- permissions and approval checks
- idempotency keys
- timeout policy
- retry classification
- artifact recording
- structured status and error emission

Recommended failure classes:

- `validation`
- `policy`
- `dependency`
- `transient`
- `permanent`
- `timeout`
- `cancelled`

Malformed outputs should fail output-schema validation before being treated as success.

### Provider-Neutral Capability Gateway

Expose one Odin-owned gateway:

- `ListCapabilities(filter)`
- `GetCapability(id, version?)`
- `InvokeCapability(request)`
- `GetRun(run_id)`
- `ResumeRun(run_id, input?)`
- `CancelRun(run_id)`
- `SubscribeRunEvents(run_id or filter)`

All transports and external agent adapters should consume this gateway instead of reaching into CLI handlers, Go structs, or raw registry files.

### Codex and Claude Code Interoperability

Codex and Claude Code should integrate through thin adapters over the same gateway contract.

What gets standardized across providers:

- capability identity and versioning
- JSON Schema input/output
- permissions and scope metadata
- request and response envelopes
- error taxonomy
- retry and cancellation semantics
- run status and artifact references
- logging and correlation identifiers

What remains provider-specific:

- prompt/tool description compression
- provider auth and session management
- provider UI conventions
- provider-specific transport constraints

Preferred external surface:

- MCP for interactive coding agents
- HTTP/JSON API as an automation and debugging mirror

### Migration Shape

Phase 1:

- add `internal/core/capabilities`
- retain the active capability snapshot in `App`

Phase 2:

- extend manifests and compiler output to the normalized contract

Phase 3:

- add registry-backed command and workflow execution
- keep hardcoded CLI fallbacks only where needed for bootstrap safety

Phase 4:

- expose the capability gateway through CLI and HTTP

Phase 5:

- add MCP and thin Codex/Claude adapters

Phase 6:

- strengthen supervision, timeout, retry, resume, and cancellation handling around the stabilized invocation path

Phase 7:

- remove legacy hardcoded catalogs and dead config surfaces

## Core vs Extension Boundaries

Belongs in stable core:

- runtime authority and persistence
- event log and projections
- run lifecycle
- policy and approval enforcement
- worktree and branch safety
- checkpoints, recovery, and supervision

Belongs in extensions or edge layers:

- provider-specific adapters
- non-bootstrap tool drivers
- agent/provider prompt shaping
- transport serialization details
- experimental capability kinds

## Operational Requirements

The architecture should treat dynamic capability loading as an operational feature, not just a developer convenience.

Required runtime properties:

- atomic snapshot publication
- structured diagnostics on rejected snapshots
- capability health state and temporary disablement
- per-capability timeout classes
- retry/backoff policy by error type
- concurrency limits for mutation-heavy capabilities
- persisted run records for in-flight and resumed work
- explicit terminal states for timeout and cancellation

## Success Criteria

This refactor is successful when:

- Odin keeps and reports an active capability snapshot with digest and diagnostics
- every invokable capability has explicit versioned metadata and JSON Schema input/output
- commands and workflows execute through one shared engine
- permissions are enforced centrally before invocation
- reload is atomic and failure-tolerant
- resume, cancel, timeout, and retry are part of the invocation lifecycle
- Codex and Claude Code can discover and invoke capabilities through the same provider-neutral contract
- legacy hardcoded capability paths are either removed or explicitly constrained to bootstrap-only use

## Why This Design

This design keeps the parts of Odin that are already strong and replaces the part that is currently too implicit.

It avoids two common failure modes:

- hardcoding more behavior into provider- or transport-specific layers
- over-abstracting too early into a generic plugin system with weak contracts

The result is a capability platform that is explicit, typed, observable, and provider-neutral, while preserving Odin's existing runtime safety model.
