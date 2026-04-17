# Skill System CRUD And Dynamic Invocation Design

## Goal

Make skills first-class runtime capabilities in `odin-os` with one authoritative definition format, real CRUD behavior, dynamic discovery, and a standard execution path that both Odin and the active Codex maintenance workflow can use without drift.

## Current State

The repository already has part of a skill system:

- `registry/skills/*.md` is a real authored source for skill metadata and prose guidance.
- `internal/registry/{parser,validator,loader,compiler}` can parse and validate registry markdown into a compiled snapshot.
- `internal/tools/broker` and `internal/workers/planner` can discover registry skills as thin cards and expand them into rich text definitions.

That foundation is incomplete for the stated goal:

- there is no canonical execution contract for skills
- there is no real skill CRUD service or CLI surface
- registry skills do not have versioned runtime metadata, schemas, or handler definitions
- broker/planner can expand skills but cannot execute them
- discovery is snapshot-oriented instead of reload-oriented, so runtime visibility after mutation is not guaranteed
- the active Codex maintenance flow can only change skills by editing files directly, which bypasses lifecycle validation and creates drift risk

## Requirements Driving The Design

The target system must satisfy these constraints:

- skills must stay Git-authored and human-reviewable
- CRUD must update both the persisted definition and the runtime-visible discovery state
- dynamic invocation must use one shared contract instead of per-skill branching
- Odin-internal callers and Codex-maintenance callers must share the same source of truth and the same lifecycle rules
- the initial design should stay small and avoid a heavy plugin framework

## Options Considered

### Option 1: Keep skills as markdown-only guidance

Treat `registry/skills/*.md` as descriptive assets only, add list/get/create/update/delete helpers, and keep execution outside the skill system.

Pros:

- minimal code churn
- low short-term risk

Cons:

- fails the dynamic invocation requirement
- Codex and Odin still need special handling outside the skill system
- CRUD would be only partially real

Rejected because it does not meet the objective.

### Option 2: Introduce a full plugin runtime

Add manifests, plugin packages, sandbox adapters, lifecycle daemons, and cache invalidation across multiple runtime layers.

Pros:

- high long-term flexibility
- room for multiple handler types later

Cons:

- too much framework for the current codebase
- large implementation surface before any reliable result
- would slow delivery and raise maintenance burden

Rejected because it is more system than the repo currently needs.

### Option 3: Registry-first executable skills

Keep markdown registry files as the canonical source of truth, extend the skill contract with execution metadata, add a single `internal/skills` service for CRUD/discovery/invocation, and route Odin plus Codex-facing CLI commands through that service.

Pros:

- one source of truth
- real CRUD
- real runtime invocation
- dynamic reload can be implemented without watchers or daemon-only state
- small enough to fit the current architecture

Cons:

- requires moderate refactoring in broker/planner and the CLI
- initial execution handler support should be intentionally narrow

Chosen because it is the smallest design that fully satisfies the objective.

## Canonical Skill Contract

The canonical source of truth remains `registry/skills/<key>.md`.

Each skill file keeps markdown sections for human guidance, but gains explicit runtime frontmatter fields:

- `kind`
- `key`
- `title`
- `summary`
- `version`
- `status`
- `enabled`
- `tags`
- `owners`
- `strictness`
- `applies_to`
- `scopes`
- `permissions`
- `handler_type`
- `handler_ref`
- `timeout_seconds`
- `input_schema`
- `output_schema`

The required markdown sections remain:

- `## Purpose`
- `## When to Use`
- `## Inputs`
- `## Procedure`
- `## Outputs`
- `## Constraints`
- `## Success Criteria`

### Notes On The Execution Fields

- `handler_type` starts with one supported value: `command`
- `handler_ref` is a repo-relative executable path such as `scripts/skills/triage-skill.sh`
- `timeout_seconds` is bounded and validated
- `input_schema` and `output_schema` use JSON-schema-like maps stored in YAML frontmatter and validated for object shape at the contract level
- `permissions` is a validated declarative list for auditability and future policy enforcement; the first implementation uses it for validation and response metadata, not OS-level sandboxing

This keeps one file authoritative for both guidance and runtime behavior.

## Lifecycle Model

### Create

`internal/skills.Service.Create(...)` accepts a structured spec, validates it, renders the canonical markdown file, writes it to `registry/skills/<key>.md`, then reloads the registry snapshot and returns the persisted item only if the post-write view is valid.

Creation is rejected when:

- the key already exists
- required fields are missing
- schema fields are malformed
- handler paths are invalid
- timeout or permissions are invalid

### Read

Read operations load through the same registry path and return a normalized skill view derived from the compiled snapshot plus the authored markdown sections.

### Update

Updates rewrite the same canonical markdown file through the same renderer and validation path.

Invalid updates are atomic:

- write to a temp file
- validate the updated registry view
- replace the canonical file only after validation succeeds

### Delete

Delete removes the markdown file only after reference checks pass.

The service rejects delete when the skill is still referenced by:

- registry agents through `tools`
- registry workflows through `composes` or other explicit skill references introduced by this change

Forced delete is out of scope for the first cut; explicit safe deletion is preferable.

## Discovery Model

Discovery must use one authoritative source and stay current after CRUD.

The design introduces a registry-backed snapshot loader that can be called on demand. Instead of holding a permanently stale registry snapshot inside the broker, the broker reads through a shared loader function or service. That keeps:

- planner catalog generation
- planner expansion
- CLI skill list/get
- runtime invocation resolution

all sourced from the same fresh registry state.

No database copy, side cache, or duplicate skill index becomes authoritative.

## Dynamic Invocation Model

Invocation uses one request/response envelope owned by `internal/skills`.

### Request

- `skill_key`
- `scope`
- `input` as structured key/value or generic JSON-like map
- `caller`
- `runtime_root`
- optional execution metadata such as correlation id

### Response

- `skill_key`
- `status`
- `summary`
- `output`
- `artifacts`
- `raw_ref`
- `raw_output`
- `duration`
- `permissions`

### Initial Handler Implementation

The first concrete handler type is `command`.

Execution rules:

- resolve `handler_ref` relative to repo root
- reject absolute or escaping paths
- execute directly without shell interpolation
- send JSON request on stdin
- expect JSON response on stdout
- enforce timeout via context
- surface malformed JSON, non-zero exit, and timeout as structured errors

This matches the existing lightweight driver pattern already used in `internal/tools/invocation`, but generalizes it for skills instead of keeping a one-off `project_status` tool path.

## Odin Internal Integration

Odin should use the new skill service in three places:

1. CLI lifecycle commands under `odin skills ...`
2. planner/broker expansion and invocation
3. runtime call sites that need to resolve a skill dynamically by key

Broker behavior changes from:

- skills are discoverable and expandable only

to:

- skills are discoverable
- skills expand to full definitions
- skills can optionally be invoked through the same broker execution path as runtime-backed tools

That requires a standard invoker abstraction in the broker for both built-in tools and registry-backed skills.

## Codex Maintenance Integration

The active Codex session should not manipulate skill files by ad hoc edits when the intent is lifecycle management.

Instead, Codex should use repo-owned entry points:

- `odin skills list`
- `odin skills get <key>`
- `odin skills create ...`
- `odin skills update ...`
- `odin skills delete <key>`
- `odin skills invoke <key> ...`

Those commands call the same `internal/skills.Service` used by Odin itself. That avoids drift because:

- the same validation rules apply
- the same markdown renderer applies
- the same discovery loader applies
- the same runtime invocation path applies

Codex may still edit files manually during development, but the documented maintenance workflow should use the CLI/service path whenever the intent is skill lifecycle management.

## Safety And Simplicity Boundaries

The design intentionally does not add:

- an out-of-process plugin manager
- background watchers
- OS-level permission sandboxing
- multiple handler runtimes in the first cut

The initial system is safe by keeping execution narrow:

- repo-relative handlers only
- direct exec only
- timeout enforced
- structured JSON I/O only
- validated metadata

This is enough to make CRUD and invocation real without adding framework bloat.

## Testing Strategy

### Unit

- registry validation for the expanded skill contract
- skill spec rendering/parsing round trips
- create/update/delete validation and reference checks
- command handler execution success, timeout, malformed output, and failure

### Integration

- CLI skill CRUD updates the registry and fresh discovery sees the change
- planner/broker catalog sees newly created skills without restart
- deleted skills disappear from discovery and invocation
- invalid updates do not corrupt the authored file

### End-To-End

- create a skill through `odin skills create`
- list and inspect it through `odin skills list/get`
- invoke it through `odin skills invoke`
- confirm planner/broker can discover and execute it from the same repo state
- update it and verify changed runtime behavior
- delete it and verify discovery plus invocation fail cleanly

## Documentation Changes

Add one dedicated contract doc for skills, and update existing registry and capability catalog docs to reflect:

- the canonical skill frontmatter
- the CRUD lifecycle
- dynamic invocation contract
- the supported Codex workflow

## Expected Outcome

After this work:

- skills have a real canonical contract
- CRUD is a real runtime-supported lifecycle, not just manual file editing
- Odin can discover and invoke skills dynamically
- Codex maintenance can use the same lifecycle path through repo-owned commands
- regressions in validation, runtime discovery, and skill execution are covered by tests
