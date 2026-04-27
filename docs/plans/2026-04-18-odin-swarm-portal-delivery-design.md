# Odin Swarm Portal Delivery Design

## Problem

`odin-os` can already execute single ask/act tasks, persist transcripts and episode memory, catalog registry skills and agents, and store learning records. What it cannot do yet is expose a truthful operator-visible swarm lane that turns one parent request into multiple auditable child tasks, each with explicit agent ownership, dynamic skill selection, and durable memory/learning output.

The next real validation target is not abstract orchestration. It is delivering two real CFIPros portal tracks through Odin:

- `CFI / School Admin Portal`
- `Student Portal`

This must pressure-test:

- long-running parent/child execution
- dynamic skill usage
- persistent memory usage
- learning creation and promotion
- operator usability through real `odin` commands

without bypassing CFIPros work outside Odin CLI.

## Existing Repo State

### Already real

- Registry-backed skills and agents exist:
  - `registry/skills/*.md`
  - `registry/agents/*.md`
- The broker already catalogs agents as `sub_agent` cards:
  - `docs/contracts/capability-catalog.md`
  - `internal/tools/catalog/types.go`
  - `internal/tools/broker/broker.go`
- Durable tasks and runs already exist:
  - `internal/runtime/jobs/service.go`
  - `internal/runtime/runs/service.go`
- Run transcripts and memory summaries already persist and are visible in `/runs show`:
  - `internal/store/sqlite/migrations/0010_memory_and_conversations.sql`
  - `internal/store/sqlite/store.go`
  - `internal/runtime/runs/service.go`
  - `internal/cli/repl/shell.go`
- Learning services already exist:
  - `internal/learning/proposals`
  - `internal/learning/evaluator`
  - `internal/learning/promotion`
  - `internal/learning/replay`
- Context packets already exist as an append-only runtime envelope for selected capabilities, blocking reasons, and resume state:
  - `internal/store/sqlite/store.go`
  - `internal/runtime/checkpoints/service.go`

### Partial

- Delegation storage exists only at the schema/model layer:
  - `internal/store/sqlite/migrations/0009_delegations.sql`
  - `internal/store/sqlite/models.go`
- There are no store methods over `delegations` or `delegation_artifacts`.
- There is no runtime service that creates child work from parent work.
- There is no operator-visible `/agent` command path.
- `/tool` can expand registry agents through the broker, but it rejects them as non-tools.
- Dynamic skill usage is only session-selected prompt injection today; it is not first-class swarm telemetry.
- Persistent memory exists, but there is no agent-focused monitoring path that ties parent work, child work, effective skills, and learnings together.

### Important current UX evidence

- `odin` help currently exposes `/skill`, `/tool`, `/jobs`, and `/runs`, but not `/agent`.
- Running `odin` from `/home/orchestrator` still hits repo-root discovery failure; running from `/home/orchestrator/odin-os` works. This is a known operator defect and should be preserved as context, not silently ignored.

## Approaches

### Option 1: Deliver the portals sequentially on top of current Odin

Use current ask/act flows only. Treat swarms as future work.

Pros:

- Fastest path to visible portal output
- No new runtime surface area

Cons:

- Does not actually validate long-running swarms
- Dynamic skill usage remains mostly implicit
- Memory/learnings are not tied to sub-agent execution
- Fails the stated validation goal

Verdict: reject.

### Option 2: Build a broad generic swarm framework first

Implement nested swarms, generic planners, broad agent lifecycle management, and generalized observability before touching portal delivery.

Pros:

- Strong end-state platform posture

Cons:

- Too much surface area before pressure-testing on real work
- High risk of inventing abstractions the portals do not need
- Delays the actual delivery-first acceptance workload

Verdict: reject for this pass.

### Option 3: Minimal truthful swarm lane plus immediate portal delivery

Add the smallest real parent/child agent path necessary to deliver the two portals. Reuse existing tasks, runs, transcripts, memory summaries, learning services, context packets, and the existing registry/broker model.

Pros:

- Delivery-first and platform-validating at the same time
- Reuses existing Odin control-plane structures
- Produces concrete evidence for skills, agents, memory, and learnings

Cons:

- Requires careful scope control
- Still leaves broader multi-level orchestration for later

Verdict: recommended.

## Selected Design

### 1. Add one truthful operator-visible agent lane

Introduce `/agent [list|show <key>|run <key> [input=value...]]`.

Why:

- agents already exist in the registry and broker
- `/tool` is the wrong operator surface because it explicitly rejects non-tool capabilities
- `/skill` already has a dedicated user-facing command, so `/agent` is consistent rather than redundant

This command should:

- list available registry agents in the current scope
- show the expanded agent definition
- run a parent agent workflow using the existing jobs/runs lane

No hidden or internal shortcuts should be required.

### 2. Reuse the existing tasks/runs model as the execution authority

Do not create a second execution system for swarms.

The swarm control plane should be:

- parent work = existing `task` + `run`
- child work = existing `task` + `run`
- linkage = existing `delegations` + `delegation_artifacts`
- audit trail = existing events, transcripts, memory summaries, and context packets

The delegation row becomes the connective tissue between parent work and child work. The child task/run remains the real unit of execution and cancellation.

### 3. Add runtime delegation services on top of the existing schema

Add a runtime service that can:

- create a delegation record from a parent task/run
- create a child task in the same project or `odin-core` when needed
- attach the child task/run ids back to the delegation row
- execute the child task through the existing executor path
- update delegation status as the child advances
- attach delegation artifacts that summarize child output, learning ids, screenshot refs, and implementation handoff evidence

This keeps orchestration thin and auditable.

### 4. Track dynamic skill usage without inventing new schema

Dynamic skill usage must be inspectable, but tasks and runs are intentionally thin. Reuse existing extensibility points instead:

- `ExecutionRequest.Metadata` for `requested_skill`, `effective_skill`, `skill_source`, `agent_key`, `delegation_id`, and `portal_track`
- `conversation_transcripts.tool_summary` for structured per-run skill telemetry
- `context_packets.selected_capabilities` for checkpointed capability state
- `delegations.details_json` for requested child-scope configuration
- `delegation_artifacts` for any materialized changes in skill choice during execution

This preserves the current schema and still makes skill selection visible.

### 5. Use existing memory and learning services for persistent output

Persistent memory should happen at two levels:

- child run episode memory:
  - what this worker learned or accomplished
- project memory:
  - durable portal-role heuristics and design/IA truths worth keeping

Learnings should use the existing proposal/evaluation/promotion services. The first pass only needs enough support to:

- create a proposal from swarm output
- approve/promote when the evidence is strong enough
- surface the created proposal/promotion ids in delegation artifacts and run detail

This keeps learning truthful instead of inventing a parallel “swarm memory” subsystem.

### 6. Use context packets for checkpointed swarm state

Context packets already encode wake packets, selected capabilities, blocking reasons, and resume state. Extend their use for swarms instead of introducing a parallel progress log.

For parent and child work, record checkpoint packets that show:

- objective
- current portal track
- selected capabilities
- blocking reason if any
- next steps

This allows long-running swarm work to be resumable and inspectable with the same runtime primitive already used elsewhere.

### 7. Deliver through two portal tracks as the acceptance workload

The proving workload is not synthetic.

The first real swarm should decompose CFIPros dashboard/portal work into bounded child tasks for:

- `admin-cfi` portal
- `student` portal

Each portal track should produce:

- purpose and role assumptions
- information architecture
- dashboard/home layout direction
- implementation guidance
- Huginn verification plan
- durable project learnings when justified

The parent agent can also spawn `odin-core` child work if a genuine platform gap appears, such as:

- refining a skill
- adding a swarm-specific agent definition
- repairing a CLI defect that blocks normal operator flow

That preserves clean workstream boundaries.

### 8. Keep observability on existing operator surfaces

Prefer extending existing operator commands over creating a new reporting UI.

Use:

- `/agent run` to launch parent work
- `/jobs` and `/runs` for queue/run inventory
- `/runs show <id|active>` for detailed evidence

Extend run detail so it can show:

- child delegations
- delegation artifacts
- effective skill telemetry
- linked memory summaries
- linked learning proposal/promotion evidence

This is enough to validate dynamic skill usage and persistent memory without adding a new `/memory` or `/learning` command group in the first pass.

## Portal Delivery Contract

### Parent workflow

The parent agent should accept structured operator input such as:

- `project_key`
- `portal_track=admin-cfi|student`
- `surface=dashboard|home`
- `goal`
- `references`
- `allow_odin_core_repairs=true|false`

### Child work classes

The initial swarm only needs a small set of child work classes:

- `ia_audit`
- `design_direction`
- `implementation_handoff`
- `visual_verification`
- `learning_capture`
- `odin_repair` when a real Odin gap blocks user-style operation

### Skill policy

- `pixel-perfect-ui-ux-designer` should be the default design skill for visual and layout tasks.
- `triage-skill` or no explicit skill should handle decomposition and routing.
- skill overrides must be explicit and recorded.
- no project-specific skill should be created unless an actual skill gap is found during delivery.

## Non-Goals

- multi-level nested swarms beyond one parent to child layer
- generic planner autonomy beyond what the portal workload requires
- a new memory-specific CLI command group
- a second execution queue distinct from tasks/runs
- direct CFIPros file modification outside Odin CLI
- replacing the existing learning subsystem

## Success Criteria

- `/agent` is a real user-facing command path, not an internal shortcut.
- A parent agent run can create and track multiple child tasks/runs through existing Odin execution lanes.
- Every child run shows effective agent and skill telemetry.
- Parent and child work leave auditable memory summaries and delegation artifacts.
- Swarm output can create or promote real learnings using the existing learning services.
- The `admin-cfi` and `student` portal tracks are both executed through Odin workflows, not manual bypasses.
- `/runs show` provides enough evidence to explain:
  - which sub-agent did what
  - which skill was used
  - what memory was written
  - what learnings were proposed or promoted
- The implementation stays DRY/KISS:
  - no duplicate execution system
  - no second registry
  - no synthetic swarm-only memory store
