---
title: Odin OS User-Adaptive Workspace Design
status: proposed
date: 2026-04-16
---

# Odin OS User-Adaptive Workspace Design

## Goal

Refactor `odin-os` from a project-centric orchestration runtime into a user-adaptive operating system for Marcus.

The system should be able to:

- manage managed software projects
- manage non-project initiatives such as life admin, research, operations, routines, and follow-ups
- host durable AI companions such as assistants, advisors, operators, and specialists
- apply user-specific memory, policy, and automation preferences without coupling the core runtime to any single provider or prompt style

The objective is not to turn Odin into a bag of personas. The objective is to create one governed runtime that can shape itself around Marcus's real operating environment.

## Problem

The current `odin-os` architecture already has several strong primitives:

- explicit runtime authority in SQLite
- managed project manifests and transition states
- executor routing behind a shared contract
- durable tasks, runs, approvals, incidents, and memory summaries
- authored registry assets for skills, agents, workflows, and commands

Those primitives are valuable, but the semantic center of the system is still too narrow and too technical:

- project governance is first-class, but user workspace modeling is not
- `task`, `scope`, `agent`, `skill`, and `workflow` are still overloaded
- assistants and advisors would currently have to masquerade as projects, tasks, or prompt bundles
- orchestration logic is concentrated in services that mix governance, routing, execution, worktree control, and memory writes

If Odin is meant to automate Marcus's life, work, and operating rhythms, the core model must expand from "managed project controller" to "user-adaptive workspace operating system."

## Recommended Shape

Use a `Workspace`-centered architecture.

`Workspace` is the first-class home for Marcus's long-lived operating environment.

Inside the workspace, Odin manages:

- `Initiatives`
- `Companions`
- `Policies`
- `Memories`
- `Capabilities`
- `Work Items`

This is the recommended semantic model:

### Workspace

The durable operating context for a user or household. For now Odin can assume one primary workspace for Marcus, but the model should not hard-code "single user forever."

Workspace owns:

- default policies
- preference baselines
- long-lived routines and commitments
- default memory rules
- allowed integration boundaries
- the set of initiatives and companions active in that workspace

### Initiative

A durable unit of ongoing responsibility inside the workspace.

Initiative examples:

- a managed Git project
- tax preparation
- health improvement
- inbox cleanup
- a product launch
- a travel plan
- a recurring life-admin process

Initiatives replace the idea that every important thing must be a software project.

Recommended initiative kinds:

- `managed_project`
- `goal`
- `case`
- `routine`
- `campaign`
- `personal_admin`

### Companion

A durable AI operating role configured for a purpose inside the workspace.

Companion examples:

- Daily Operating Assistant
- Finance Advisor
- Health Advisor
- Chief of Staff
- Project Operator
- Research Analyst

Companions are first-class user-facing concepts, but they are built on shared planning, memory, policy, and execution primitives. They should not each invent their own orchestration stack.

Each companion should define:

- role and charter
- initiative assignments
- allowed tools and integrations
- escalation and approval policy
- preferred planning style
- memory policy
- tone and interaction rules

### Work Item

A durable unit of governed work created within the workspace, optionally attached to:

- an initiative
- a companion
- a managed project

The current `task` concept should evolve into the `Work Item` domain model. The physical `tasks` table can remain temporarily during migration, but the domain language should move immediately.

## Domain Boundaries

### Core domain

The core domain is `Governed Workspace Orchestration`.

That includes:

- deciding what work Odin may do
- under which workspace or initiative context
- through which companion or operator path
- with which approvals and policies
- through which safe execution lane
- with which durable operational trace

### Supporting domains

- `Project Governance`
- `Initiative Management`
- `Companion Management`
- `Capability Catalog`
- `Planning and Context Assembly`
- `Execution Routing`
- `Memory and Knowledge`
- `Runtime Resilience`
- `Learning and Promotion`

### Generic subdomains

- SQLite persistence
- CLI, HTTP, and TUI delivery
- telemetry and observability
- provider adapters
- tool and integration adapters
- Git and filesystem plumbing

## Proposed Bounded Contexts

### 1. Workspace Context

Owns the workspace itself, workspace policy defaults, user preferences, identity anchors, and the top-level inventory of initiatives and companions.

This context answers:

- who is Odin operating for
- what is the default operating posture
- which policies apply workspace-wide

### 2. Initiative Context

Owns initiative lifecycle, classification, status, ownership, and relationships to projects, routines, or cases.

This context answers:

- what ongoing responsibility does this work belong to
- which initiative state is active
- which companion is responsible or assigned

### 3. Companion Context

Owns assistants, advisors, operators, and specialist companion definitions.

This context answers:

- what role this companion plays
- which tools and policies it may use
- which memory and planning rules apply

### 4. Project Governance Context

Owns managed Git projects, transition states, mutation authority, approval rules, branch rules, and system-project constraints.

This context remains essential, but it becomes one specialized governance context inside the wider workspace model rather than the whole system's semantic center.

### 5. Capability Catalog Context

Owns authored reusable assets:

- skills
- workflows
- agent role definitions
- operator commands

Companions may reference these assets, but the catalog does not own runtime companion state.

### 6. Planning Context

Owns plan construction, context assembly, tool selection, budget limits, and compaction.

This context consumes workspace policy, initiative context, companion policy, capability catalog, and memory.

### 7. Work Execution Context

Owns work item lifecycle, run attempts, approval requests, blocked states, completion state, and execution outcomes.

This context should become the main application-level coordinator and replace the current cross-cutting runtime execution service shape.

### 8. Memory Context

Owns transcripts, episodic memory, initiative memory, companion memory, workspace memory, and user preference memory.

Different memory scopes must be explicit. "Memory" cannot remain a generic bag of summaries.

### 9. Integration Context

Owns external tools and external provider boundaries:

- model providers
- GitHub
- Gmail
- Google Calendar
- web automation
- shell and filesystem

This context exposes explicit anti-corruption contracts to the rest of Odin.

## Ubiquitous Language

Adopt these terms consistently:

- `Workspace`: the durable operating environment for Marcus
- `Initiative`: a durable responsibility or objective in the workspace
- `Companion`: a durable AI role such as assistant, advisor, operator, or specialist
- `Managed Project`: a Git-governed initiative with mutation controls
- `Work Item`: a durable unit of governed work
- `Run Attempt`: one execution attempt for a work item
- `Control Scope`: the current operating subject and boundary
- `Capability Definition`: an authored reusable asset such as a workflow or skill
- `Execution Lane`: Odin's provider-neutral execution channel
- `Provider Adapter`: infrastructure bridge to Codex, Claude, OpenAI, or another model system

Terms to phase out or narrow:

- `task`: use `work item` in domain and interface language
- `agent`: reserve for `agent role` in the catalog or `subagent instance` at runtime
- `command`: use `operator command` for catalog and interface definitions
- `scope`: use `control scope` when referring to governed operating boundaries

## Control Scope Model

The current scope model is too flat for a user-adaptive system.

Replace a single `kind` enum with a richer `ControlScope` value object:

- `workspace`
- `system_project`
- `managed_project`
- `initiative`
- `setup`

Each scope should carry:

- subject type
- subject key
- optional parent workspace key
- optional initiative key
- optional managed project key
- optional companion key

This allows Odin to operate in ways that fit real usage:

- Marcus talking to his workspace assistant
- a finance advisor operating inside a finance initiative
- a project operator working on a managed project
- a system-maintenance companion acting on `odin-core`

## Execution Model

Execution should pivot from provider-first orchestration to workspace-governed orchestration.

Recommended model:

1. a request enters the workspace
2. intake classifies the request
3. Odin resolves initiative, companion, and control scope
4. planning builds a bounded execution plan
5. work execution creates or resumes a work item
6. project governance is consulted only when the work touches a managed project
7. provider routing selects an execution lane
8. integration gateways and tools are invoked through explicit contracts
9. transcripts and memory are written under explicit scopes

This keeps project safety strong without forcing every life automation through a project-shaped workflow.

## Memory Model

Introduce explicit memory scopes:

- `workspace_memory`
- `initiative_memory`
- `companion_memory`
- `project_memory`
- `run_memory`
- `user_preference_memory`

Each memory write should declare:

- source scope
- visibility scope
- retention intent
- summary type

This prevents finance, health, project, and life-admin context from collapsing into one global memory pool.

## Provider and Integration Isolation

Codex, Claude Code, OpenAI API, Anthropic API, Gemini, and future providers remain infrastructure.

The core runtime should reason about:

- capabilities
- budgets
- latency and health
- approval and risk posture

It should not reason about vendor-specific prompt formats or auth models.

Likewise, GitHub, Gmail, Calendar, and shell access should be exposed as integration contracts, not leaked into core orchestration language.

## Migration Principles

Use a modular-monolith migration, not a service split.

Key migration rules:

- keep SQLite as the runtime authority
- keep the current repo as the runtime root
- introduce new domain terms before renaming physical tables
- map existing `tasks` and `runs` to `Work Item` and `Run Attempt` in domain code first
- preserve current project transition safety while widening the system around workspace and initiatives
- do not make assistants and advisors separate orchestration engines

## Recommended Rollout

### Phase 1: Language and contracts

- publish ubiquitous language
- define workspace, initiative, and companion contracts
- define control scope v2

### Phase 2: Workspace and initiative foundations

- add workspace and initiative persistence
- attach current managed projects to initiatives
- add initiative-aware work item metadata

### Phase 3: Companion foundations

- add runtime companions
- add companion policy and assignment rules
- let work items reference a companion

### Phase 4: Execution refactor

- split work execution from project governance
- move work item orchestration behind domain repositories and services
- leave provider logic behind execution lane contracts

### Phase 5: Memory and operator surfaces

- add typed memory scopes
- expose workspace, initiative, and companion views in CLI and API

### Phase 6: Capability and personalization expansion

- let companions compose skills, workflows, and agent roles safely
- add workspace-tailored defaults and adaptive policy overlays

## Non-Goals

This design does not require:

- one orchestration engine per assistant
- immediate table renames for every legacy term
- microservice decomposition
- full autonomous destructive authority
- provider-specific domain modeling

## Success Criteria

The refactor is successful when:

- Odin can represent Marcus's workspace explicitly
- Odin can manage both managed projects and non-project initiatives cleanly
- assistants and advisors exist as durable companions without duplicating orchestration logic
- work items can be attached to initiative, project, and companion context
- memory and policy are scoped explicitly
- provider and tool details stay outside the core domain
- a small team can still understand and evolve the system
