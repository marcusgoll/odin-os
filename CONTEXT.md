# Odin OS

Odin OS is the governed operator control plane for Marcus's work. It owns operator invocation, approvals, workflow registry entries, run evidence, memory, policy, dispatch decisions, and proof requirements. Domain-specific systems keep ownership of their own business language and external-system behavior unless explicitly promoted into Odin by a locked decision.

## Language

**Operator Workflow Suite**:
A governed family of Odin-authored workflows that coordinate one business capability area through the canonical Odin operator surface.
_Avoid_: Domain platform, side script, hidden automation bundle

**Marcus FLICA Operations Workflow Suite**:
The Odin-owned operator workflow suite for Marcus's FLICA-backed airline operations, including seniority-based bidding, first-come-first-served bidding, TradeBoard, annual vacation, and schedule workflows.
_Avoid_: Odin-owned FLICA domain, duplicate PBS airline model, autonomous FLICA bot

**Seniority Bid Workflow**:
The top-level Odin workflow for operator-attended seniority-based bid and pickup actions where airline rules rank eligible pilots by seniority context.
_Avoid_: Generic bidding workflow, FCFS workflow

**FCFS Bid Workflow**:
The top-level Odin workflow for operator-attended first-come-first-served bid and pickup actions where request timing is the controlling airline mode.
_Avoid_: Seniority workflow, generic open-time workflow

**Bid Action**:
The shared Odin operator action model used by **Seniority Bid Workflow** and **FCFS Bid Workflow** for a prepared airline-facing acquire or bid-related write.
_Avoid_: Separate seniority action type, separate FCFS action type, raw pickup command

**Trade Evaluation Mode**:
The airline-defined evaluation mode applied to a **Bid Action**, such as seniority-ranked or first-come-first-served. Odin records and routes by this mode but PBS owns the airline-domain semantics.
_Avoid_: Odin ranking logic, local priority policy

**TradeBoard Workflow**:
The top-level Odin workflow for operator-attended airline TradeBoard actions, including posting, pickup, drop, exchange, split-trip, and readback-proof procedures.
_Avoid_: Native marketplace, platform exchange

**TradeBoard Action**:
The distinct Odin operator action model used by **TradeBoard Workflow** for post-award airline trade surface writes and readbacks, including post, pickup, drop, exchange, and split-trip actions.
_Avoid_: Bid Action, raw marketplace command, generic schedule edit

**Annual Vacation Workflow**:
The top-level Odin workflow for operator-attended pilot annual vacation actions and related vacation-buffer or vacation-slide checks.
_Avoid_: Generic time-off workflow, personal calendar workflow

**Annual Vacation Action**:
The distinct Odin operator action model used by **Annual Vacation Workflow** for airline annual vacation requests, bids, changes, checks, status readbacks, and vacation-related schedule-impact review.
_Avoid_: Schedule edit, personal calendar event, generic time-off request

**Action Lifecycle**:
The shared progress and proof-state model for airline-facing Odin actions: prepared, preflighted, approved, submitted, internally recorded, externally read back, completed, failed, or abandoned.
_Avoid_: Workflow-specific status ladder, implicit completion, submitted means complete

**Action Record**:
The durable Odin-owned evidence record for an airline-facing action. It links the workflow, action type, **Action Lifecycle** state, **Operator Approval**, **Schedule Snapshot**, downstream PBS/flight-api evidence, and **External Readback** or declared **Substitute Proof**.
_Avoid_: PBS-only action log, browser-only screenshot, unlinked run note

**Action Evidence Event**:
An append-only evidence entry on an **Action Record** that records lifecycle changes, approvals, submissions, downstream evidence, browser proof, readbacks, failures, abandonment, or corrections without overwriting prior evidence.
_Avoid_: Mutable status edit, hidden correction, overwritten proof

**Prepared Action Payload**:
The immutable airline-facing payload snapshot prepared for **Operator Approval**. It includes the target activity, action type, workflow-specific fields, **Schedule Preflight** or **Schedule Snapshot** reference, operator-facing comment when applicable, **Submit Path**, and downstream destination.
_Avoid_: Loose intent, editable approval form, inferred submit details

**Submit Path**:
The explicit route declared for airline-facing submission, such as PBS/flight-api downstream capability or Huginn operator browser. Browser authentication, Duo, live FLICA UI interaction, or browser proof requires a Huginn submit path.
_Avoid_: Implicit route, plain Playwright fallback, undeclared direct script

**Readback Path**:
The explicit route declared for collecting **External Readback**. It may match **Submit Path** or differ from it; live FLICA UI readback, browser authentication, Duo, or browser proof requires a Huginn readback path.
_Avoid_: Internal success as readback, implicit proof route, undeclared browser check

**Schedule Workflow**:
The top-level Odin workflow for operator-attended schedule sync, schedule snapshot inspection, schedule-change proof, and schedule-derived preflight checks for other FLICA workflows.
_Avoid_: Logbook workflow, monthly line workflow

**Schedule Snapshot**:
The canonical schedule state evidence produced or validated by **Schedule Workflow** for Odin workflows. Odin treats it as the shared preflight input while PBS owns airline-domain schedule semantics.
_Avoid_: Per-workflow schedule check, local freshness guess, personal calendar snapshot

**Schedule Preflight**:
The required pre-write check supplied by the **Schedule Workflow** before any other Marcus FLICA workflow performs an airline-facing write.
_Avoid_: Optional freshness check, hidden sync assumption

**Schedule Freshness Requirement**:
The registry-declared recency rule for the **Schedule Snapshot** used by **Schedule Preflight** before approval or submission. It is declared per **Workflow Registry Entry** rather than hardcoded globally.
_Avoid_: Global freshness guess, stale schedule approval, implicit snapshot validity

**Operator Surface**:
The user-facing Odin command, shell, TUI, or workflow entrypoint through which an operator invokes or reviews governed work.
_Avoid_: Direct downstream script, hidden service call

**CEO Briefing Workflow**:
The Odin-owned operator-invoked workflow for producing a portfolio-wide briefing proposal and, after explicit approval, publishing one **Daily Priority Packet** for a business date. Its canonical operator surface is `odin brief ceo`; REPL aliases may assist but must not become proof authority.
_Avoid_: Scheduled CEO sidecar, legacy executive_review authority, direct priorities file write

**Briefing Proposal**:
The draft output of a **CEO Briefing Workflow** before approval. It contains evidence-backed focus recommendations, blockers, queue recommendations, and proposed slot guidance, but it does not mutate queue state or publish priorities.
_Avoid_: Daily Priority Packet, approved priority state, queue mutation

**Daily Priority Packet**:
The approved Odin runtime record for one business date's portfolio priorities. It may include focus weights, blockers, deferred or killed recommendations, and slot recommendations, but it is priority evidence only and does not directly defer, cancel, release, dispatch, or mutate queued work.
_Avoid_: Queue action, scheduler directive, legacy priorities.json as authority

**Priority Packet Supersession**:
The append-only event relationship where a later approved **Daily Priority Packet** for the same business date replaces the prior active packet while preserving the earlier packet in audit history.
_Avoid_: In-place priority edit, multiple active packets for one date, hidden overwrite

**Initiative**:
A durable Odin-owned unit of responsibility. A managed software project is one **Initiative** kind, not the whole Odin product model. Other initiative kinds may include goals, cases, routines, campaigns, and personal administration.
_Avoid_: Treating project as the only durable work container, provider-specific project state

**Work Item**:
The durable Odin-owned operational object that turns intent into governed execution. A feature request, bug, task, investigation, review, or follow-up should map to a **Work Item** or a grouped set of **Work Items**, not to a new parallel top-level entity.
_Avoid_: Feature as a separate root aggregate, loose task note, executor-owned work state

**Run Attempt**:
One execution attempt for a **Work Item** through an executor lane. A **Run Attempt** may use Codex, Claude, another CLI runner, or an API executor, but it does not own durable truth after it completes.
_Avoid_: Work Item, durable feature state, provider-owned task record

**Live Execution Session**:
An ephemeral process or attachment handle for a running **Run Attempt**, such as a Codex tmux session. Odin may record its executor, host, process/session identifier, repo or worktree path, liveness, and handoff evidence, but the session is not durable workspace or project truth.
_Avoid_: Workspace, Project, Work Item, tmux as source of truth, direct Codex session as delivery authority

**Live Execution Session Key**:
The generated stable Odin identifier for a **Live Execution Session**, derived from the owning **Run Attempt** identity. Operators may add aliases for convenience, but aliases must not replace the canonical session key in persisted evidence.
_Avoid_: Provider process id as canonical key, ambiguous alias, human nickname as durable identity

**Adopted Live Execution Session**:
A **Live Execution Session** that was started outside Odin, such as an SSH-launched Codex session on the homelab server, and later bound to one **Run Attempt** by an explicit operator action. Adoption records the session handle and available handoff context; it does not make arbitrary external sessions Odin-governed automatically, and pre-adoption work is not proof until separately verified.
_Avoid_: Auto-discovered worker, retroactive authority, unmanaged SSH session as proof

**Delivery Workflow**:
The Odin-owned workflow for taking a **Work Item** from intent to verified completion. The canonical delivery loop is domain lock, design, implementation plan, execution, verification, branch finish, and learning capture. Its canonical operator surface is the top-level `odin work ...` command family; REPL slash commands may alias that surface but must not become the proof authority.
_Avoid_: Ad hoc skill chain, direct Codex session as authority, unverified completion

**Delivery Gate**:
One ordered checkpoint in a **Delivery Workflow**. V1 gates are domain_locked, design_approved, plan_ready, execution_selected, execution_complete, verified, branch_finished, and learning_reviewed.
_Avoid_: Hidden checklist item, implied progress, executor-local status

**Feedback Loop**:
The repeatable skill-agent-workflow cycle that turns evidence into the next bounded action. A clean feedback loop produces clear state, useful tests or proof, concise output, and an explicit next gate or failure branch.
_Avoid_: Noisy transcript, ambiguous next step, unreviewed agent output

**Delivery Profile**:
An authored registry concept that selects the skills, agents, workflow gates, proof requirements, and failure branches for a **Delivery Workflow** based on **Work Item** kind, risk, scope, and task shape. In v1, a **Delivery Profile** is represented as a specialized `workflow` registry entry tagged `delivery_profile`, not as a separate registry kind.
_Avoid_: One-size-fits-all pipeline, hardcoded feature-only workflow, hidden routing rule

**Agency Orchestrator**:
The Odin-owned, multi-project Delivery Workflow capability that continuously turns eligible intake from enrolled projects into governed **Work Items**, isolated **Run Attempts**, draft pull requests, QA/review evidence, and human review handoff. It is not a separate product, repo, or cfipros-specific control plane.
_Avoid_: cfipros-agency, project-specific agency app, GitHub-owned scheduler, parallel orchestrator

**Issue Intake Source**:
An upstream tracker record, such as a GitHub Issue, that can propose or update a **Work Item** for an enrolled managed project. The intake source is evidence and synchronization input; Odin-owned SQLite state remains the runtime authority.
_Avoid_: GitHub as runtime truth, tracker-owned Work Item state, issue equals run

**Human Review Handoff**:
The Delivery Workflow state where Odin has recorded implementation, QA, review, and PR evidence and now waits for a human to decide merge, rejection, follow-up, or deployment. It is not approval to merge or deploy.
_Avoid_: autonomous merge, auto-deploy, done without human decision

**Feature Work Item**:
A **Work Item** kind or grouping used for product or code feature delivery. It may decompose into smaller **Work Items** and **Run Attempts**, but it is not a separate top-level Odin aggregate unless a future locked decision promotes it.
_Avoid_: Feature aggregate, project-owned feature registry, hidden implementation checklist

**Operator-Invoked**:
The required invocation mode for live Marcus FLICA workflows. An operator must start the **Workflow Run** from an **Operator Surface**; scheduled or autonomous live airline-facing runs are not allowed without a future locked domain decision.
_Avoid_: Scheduled live write, autonomous FLICA run, helper-started live action

**Operator Approval**:
An explicit approval at an Odin **Operator Surface** that authorizes exactly one prepared airline-facing action payload after the workflow has prepared inputs and completed required preflight checks. Material payload changes require a new approval evidence event before submission.
_Avoid_: Helper confidence, implicit approval, background auto-submit

**Workflow Registry Entry**:
The authored Markdown-with-frontmatter workflow definition under `registry/workflows/` that records repeatable operator procedure, inputs, constraints, and success criteria. For live airline-facing use, it must declare the operator surface, inputs, action model, **Helper Permissions**, schedule preflight requirement, **Schedule Freshness Requirement**, approval point, **Submit Path**, proof requirement, **Readback Path** or **Substitute Proof**, and failure or abandonment rules.
_Avoid_: Tribal note, loose prompt, untracked checklist

**Workflow Run**:
The Odin-owned invocation record for a **Workflow Registry Entry**. It records run-level timing, operator surface, inputs, helper activity, and outcome, and may contain zero or more **Action Records**.
_Avoid_: Action Record, loose session note, downstream-only run

**Workflow Run Outcome**:
The invocation-level result for a **Workflow Run**, separate from **Action Lifecycle**. Outcomes include inspected, preflighted, completed with action, completed without action, failed, or abandoned.
_Avoid_: Action Lifecycle, submitted means run complete, completed means airline action completed

**Proof Requirement**:
The workflow-specific evidence that must be observed before Odin treats a workflow as complete, such as a real Odin command result, downstream action log, persisted run evidence, or live external readback.
_Avoid_: Internal-only success, unchecked browser action

**External Readback**:
A live or recently captured airline-owned confirmation surface that shows the intended write was accepted or is visible in FLICA, such as My Requests, request status, schedule state, vacation status, or another workflow-specific confirmation view.
_Avoid_: Local-only log, inferred success, unchecked screenshot

**Substitute Proof**:
A declared proof exception in a **Workflow Registry Entry** used only when FLICA provides no readable confirmation surface for that workflow. It states why **External Readback** is unavailable, the alternate evidence source, and the operator-facing risk.
_Avoid_: Convenience bypass, internal success as proof, undeclared readback skip

**Workflow Helper**:
A skill, subagent, shim, or other bounded assistant used by an Odin workflow to prepare inputs, inspect live state, map external UI details, draft operator-facing text, or verify proof.
_Avoid_: Execution owner, hidden submitter, autonomous external writer

**Helper Permission**:
The explicit capability boundary declared in a **Workflow Registry Entry** for a **Workflow Helper**. It records allowed tasks, forbidden tasks, required stop condition, and whether the helper may touch Huginn, PBS/flight-api, or only local evidence.
_Avoid_: Implicit helper authority, helper approval, helper-owned submission

**Skill**:
A reusable instruction bundle or tool-aware procedure that helps an operator or worker perform a bounded part of a workflow.
_Avoid_: Workflow authority, external write owner

**Subagent/Shim**:
A delegated or adapter-like helper used for a bounded workflow task, such as Huginn page inspection, BCID validation, split-leg mapping, or readback verification.
_Avoid_: Independent FLICA actor, bypass path, second operator surface

**Knowledge Source**:
A governed source document or source document collection that Odin may ingest, index, refresh, and cite within an explicit scope. A **Knowledge Source** may point to Markdown, PDF, or another supported document format, but its extracted or indexed representation is derived runtime state rather than the source of truth.
_Avoid_: RAG file, context dump, memory blob, registry item

**Knowledge Source Manifest**:
The authored Markdown-with-frontmatter record under `memory/knowledge/` that declares a **Knowledge Source** and its scope, source type, location, checksum or external reference, refresh policy, extractor expectations, and citation rules.
_Avoid_: Index row, extracted chunk, binary storage location

**Knowledge Artifact**:
A content-addressed local artifact or external file reference used by a **Knowledge Source Manifest** for source bytes that should not live directly in Git, such as a pilot contract PDF, book PDF, or other large source document.
_Avoid_: Git-tracked book, opaque attachment, runtime cache

**Restricted Knowledge Source**:
A **Knowledge Source** whose source content is sensitive, licensed, private, or otherwise unsuitable for broad export or unbounded executor context injection. Pilot contracts, books, manuals, and other large or licensed PDFs are **Restricted Knowledge Sources** by default unless a manifest explicitly records a less restrictive policy.
_Avoid_: Public reference, shareable corpus, unrestricted context

**Knowledge Operator Surface**:
The canonical top-level Odin command family for **Knowledge Source** work. The command family is `odin knowledge` and owns ingest, list, show, search, refresh, and restricted-source approval flows. REPL slash commands may alias this surface, but they must not become the canonical proof path.
_Avoid_: Direct manifest edit, sidecar RAG script, memory-only command, REPL-only workflow

**Knowledge Source Lifecycle**:
The readiness state for a **Knowledge Source** and its derived retrieval artifacts: declared, artifact_available, extracted, indexed, ready, stale, or failed.
_Avoid_: Approval state, export permission, executor context permission

**Restricted Knowledge Use Approval**:
An explicit single-use operator approval event that authorizes one broader use of a **Restricted Knowledge Source**. V1 use types are bulk_export, broad_extraction, sharing, and executor_context_injection. It is separate from **Knowledge Source Lifecycle**.
_Avoid_: Ready state, standing permission, implicit sharing

**Supported Knowledge Source Class**:
A v1 source format that Odin can extract with reliable provenance: Markdown, plain text, or machine-readable PDF.
_Avoid_: Scanned PDF, OCR-derived source, image-only document

**OCR-Required Knowledge Artifact**:
A **Knowledge Artifact** whose source bytes require optical character recognition before Odin can extract cited text reliably. OCR-required artifacts are out of scope for v1 extraction and must not be treated as ready.
_Avoid_: Machine-readable PDF, weak extraction success, silent OCR fallback

## Ownership Boundaries

Odin owns the **Marcus FLICA Operations Workflow Suite** as an operator workflow suite. It owns when the workflow may run, what approval or confirmation is required, which operator surface is canonical, which helper capabilities may participate, and what proof is required before completion.

PBS owns the airline pilot platform domain for FLICA-backed bidding, schedules, TradeBoard actions, annual vacation support, browser/session behavior, and airline-owned integration details. PBS remains the source of canonical terms such as **Planning Period**, **Bid Category**, **Seniority Standing**, **Airline Trade Surface**, **Acquire**, **Drop**, **Exchange**, and **Schedule Snapshot**.

Huginn is the operator browser capability used when live browser proof or operator-attended authentication is required. Huginn does not own FLICA domain state.

Odin owns **Knowledge Sources** as governed inputs to scoped memory and retrieval. Source documents remain authored or referenced knowledge inputs; extracted text, chunks, embeddings, indexes, summaries, and search projections are derived runtime state and must not outrank the source document plus its recorded provenance.

Odin owns **Knowledge Source Manifests** under `memory/knowledge/` as the canonical authored declarations for document knowledge. **Knowledge Artifacts** may live outside Git or in an Odin-managed artifact store when source bytes are large, sensitive, licensed, or operationally awkward to review in Git.

Odin owns the retrieval policy for **Restricted Knowledge Sources**. It may use restricted sources for scoped personal/operator answers, but it must preserve source limits around quoting, export, sharing, and executor context injection.

Odin treats personal reference documents, including pilot contracts, books, and manuals, as global-scoped **Restricted Knowledge Sources** by default. A **Knowledge Source Manifest** may bind a source to a managed-project scope only when the document is truly project-specific.

Odin owns the **Knowledge Operator Surface** as the canonical operator entrypoint for document knowledge. Missing `odin knowledge` support is an Odin product gap, not authorization to bypass the model with direct file edits, hidden ingestion scripts, or a parallel knowledge tool.

Odin owns **Knowledge Source Lifecycle** as retrieval readiness state. Odin owns **Restricted Knowledge Use Approval** as separate approval evidence for broader use of restricted content; approval state must not be encoded as lifecycle readiness.

Odin owns **Initiatives**, **Work Items**, **Run Attempts**, **Delivery Gates**, **Delivery Profiles**, **Feedback Loops**, and the **Delivery Workflow** as governed work-control concepts. Managed software projects may back an **Initiative**, but project repositories do not own Odin's delivery state, proof requirements, or branch-finish decisions.

Odin owns `odin work ...` as the canonical **Delivery Workflow** operator surface. Missing `odin work` support is an Odin product gap; direct Codex sessions, REPL-only aliases, and sidecar scripts may assist but cannot satisfy canonical operator proof for delivery workflow behavior.

Odin owns the orchestration of skills, agents, and workflows inside **Feedback Loops**. Skills provide reusable procedures, agents perform bounded work or review, and workflow registry entries declare the repeatable sequence and proof contract. None of them may replace Odin-owned gate state or runtime evidence.

Odin owns **Delivery Profiles** as authored routing and proof contracts for flexible delivery. Profiles let different **Work Item** shapes use different gate subsets, skills, agents, proof levels, and failure branches while preserving the same Odin-owned evidence model.

Odin owns v1 **Delivery Profiles** through the existing `workflow` registry kind. A new registry kind for delivery profiles is deferred until profiles need separate validation, UI behavior, or lifecycle semantics that cannot be cleanly represented by workflow entries.

Odin owns the **Agency Orchestrator** as a multi-project extension of the **Delivery Workflow**, not as a new project-specific product. `cfipros` may be an enrolled managed project and an early proving target, but it does not own the orchestrator, runtime state, gate model, or scheduler semantics.

Odin treats GitHub Issues as an **Issue Intake Source** for GitHub-backed managed projects. GitHub labels, comments, issue state, and pull requests may mirror or request Odin state, but they must not outrank Odin-owned **Work Items**, **Run Attempts**, approvals, events, worktree leases, or proof records.

Odin owns v1 extraction only for **Supported Knowledge Source Classes**. OCR for scanned PDFs or image-only documents is a future capability that requires a separate locked decision before implementation.

Odin owns the **CEO Briefing Workflow**, **Briefing Proposals**, **Daily Priority Packets**, and **Priority Packet Supersession** as governed portfolio-priority concepts. Legacy `/var/odin` or `odin-orchestrator` executive review artifacts may inform migration and audit, but they do not own current priority state or operator proof.

## Invariants

- Odin must not duplicate PBS airline domain ownership when a PBS capability already owns the behavior.
- **Initiative**, **Work Item**, and **Run Attempt** are the canonical delivery hierarchy for project, feature, and task work until a future locked decision changes it.
- A feature request must be represented as a **Feature Work Item** or a grouped set of **Work Items**, not as a separate root aggregate.
- A **Run Attempt** must remain execution evidence for a **Work Item**; it must not become the durable source of truth for feature state.
- A **CEO Briefing Workflow** must be operator-invoked through `odin brief ceo`; scheduled or legacy sidecar execution cannot publish an Odin-owned **Daily Priority Packet**.
- A **Briefing Proposal** must not mutate queue state, publish priorities, or change scheduler behavior before explicit operator approval.
- A **Daily Priority Packet** may record focus weights, blockers, deferred or killed recommendations, and slot recommendations, but queue mutations require separate operator actions and evidence.
- For one business date, only one approved **Daily Priority Packet** may be active at a time; approving a later packet must record **Priority Packet Supersession** instead of editing the earlier packet in place.
- **Daily Priority Packets** must live in SQLite runtime state with append-only events and CLI projections; generated files, legacy `/var/odin/state/priorities.json`, or briefing emails are artifacts only.
- A **Live Execution Session** must be attached to one **Run Attempt** when Odin records it; it must not become durable **Workspace**, **Project**, or **Work Item** state.
- A **Live Execution Session** must have a generated stable **Live Execution Session Key** derived from the owning **Run Attempt**; operator aliases are optional conveniences and must resolve back to that canonical key.
- **Live Execution Session** aliases must be unique among active sessions in the current operator workspace and must not be used as the only persisted identifier in evidence, handoff, stop, or proof records.
- A **Run Attempt** may have at most one active **Live Execution Session**; simultaneous Codex, tmux, SSH, or shell sessions for the same **Work Item** must be represented as separate **Run Attempts**.
- tmux and similar process supervisors may be treated as liveness truth for a **Live Execution Session**, but Odin-owned runtime evidence remains the proof authority for **Delivery Workflow** progress.
- An **Adopted Live Execution Session** requires an explicit operator binding action; Odin must not auto-adopt arbitrary SSH, tmux, Codex, or shell sessions.
- Adoption must record at least the **Run Attempt**, host, executor lane, mutation mode, process or session identifier, repo or worktree path, adoption time, and operator-provided handoff evidence.
- A mutating **Live Execution Session** must use a distinct worktree path for its active **Run Attempt**; read-only live sessions may share the repo root when they do not claim mutation authority.
- Parallel mutating **Live Execution Sessions** for the same **Work Item** must not share a worktree path, branch, or active worktree lease.
- Pre-adoption work from an **Adopted Live Execution Session** is imported handoff context, not Delivery Gate proof; gates may advance only after Odin records explicit verification evidence.
- List and overview surfaces for **Live Execution Sessions** should read cached liveness state with freshness metadata; `workspace status` and `workspace attach` style commands are the live tmux/SSH probe points.
- `workspace attach` is bind-only: it attaches the operator to an existing **Live Execution Session** and must not create, adopt, resume, start, or stop a Codex, tmux, SSH, or shell session.
- Live session lifecycle verbs must stay separate: `workspace start` creates an Odin-launched session, `workspace bind` or `workspace adopt` records an external session, `workspace status` probes liveness, `workspace attach` attaches to an existing session, `workspace handoff` records handoff context, and `workspace stop` marks the session stopped in Odin by default.
- `workspace stop` must not terminate the underlying Codex, tmux, SSH, or shell process unless the operator supplies an explicit termination flag such as `--terminate`.
- Terminating a **Live Execution Session** process is a destructive lifecycle action and must preserve or request handoff evidence before Odin treats the **Run Attempt** outcome as complete.
- A mutating **Live Execution Session** requires `workspace handoff` before `workspace stop` or `workspace stop --terminate`, unless the operator explicitly marks the session abandoned.
- `workspace handoff` for a mutating session must capture summary, changed paths or worktree reference, last known status, verification already run, and next recommended action.
- A **Delivery Workflow** is incomplete until its required proof is recorded, including real `odin` command evidence when the operator surface is user-visible.
- Skills, subagents, and executor lanes may help execute a **Delivery Workflow**, but they must not replace Odin's **Work Item**, approval, evidence, verification, or branch-finish authority.
- Real E2E verification for **Delivery Workflow** behavior must exercise `odin work ...` once that command family exists; REPL aliases and direct executor sessions are insufficient proof.
- A **Delivery Workflow** must advance through ordered **Delivery Gates**: domain_locked, design_approved, plan_ready, execution_selected, execution_complete, verified, branch_finished, and learning_reviewed.
- A **Delivery Gate** must not be marked complete without current evidence recorded in Odin-owned state or an explicitly linked artifact.
- `systematic_debugging` is the failure branch for unexpected errors, test failures, build failures, or confusing behavior during the **Delivery Workflow**.
- `writing_skills` is an optional learning action after learning_reviewed, used only when the review identifies a reusable process gap that merits skill work.
- A **Feedback Loop** must produce clean output: current gate, evidence, decision, next action, and remaining risk. Noisy agent output must be summarized into Odin-owned evidence before it advances the workflow.
- Skills, agents, and workflows used in a **Feedback Loop** must be selected by the current **Work Item** kind, risk, scope, and proof needs rather than hardcoded to software feature work only.
- A **Delivery Profile** must declare applicable **Work Item** kinds or shapes, required **Delivery Gates**, required or optional skills, allowed agents, proof requirements, and failure branches.
- A **Delivery Profile** must not remove verification or learning review for delivery work; it may satisfy them with lighter proof only when the profile declares why that proof is sufficient.
- **Delivery Profiles** must remain authored and reviewable; hidden runtime heuristics may recommend a profile but must not silently rewrite profile requirements.
- V1 **Delivery Profiles** must be authored as `workflow` registry entries tagged `delivery_profile`; implementation must not add a fifth registry kind unless a future locked decision proves workflow entries are insufficient.
- The **Agency Orchestrator** must operate across enrolled managed projects; it must not hardcode `cfipros` or any other project as the product boundary.
- An **Issue Intake Source** may create, refresh, or annotate a **Work Item**, but it must not become the canonical runtime state for dispatch, retries, approvals, verification, or branch finish.
- For Stage 1 live GitHub read-only proof, `ODIN_DRY_RUN=true` means external-mutation dry-run, not local-runtime dry-run. Odin may persist fetched eligible issue records into SQLite `external_issues` to prove idempotence, but it must not write GitHub comments, labels, pull requests, or issue state.
- Stage 1 live GitHub read-only proof may persist only **Issue Intake Source** records as SQLite `external_issues`. It must not create **Work Items**, **Run Attempts**, approvals, worktree leases, pull request handoffs, comments, labels, scheduler jobs, or dispatch state.
- Stage 3 live GitHub controlled mutation treats the transition from `odin:running` to `odin:human-review` as a real lifecycle move for the approved test issue: the final issue must retain `odin:human-review` and must not retain `odin:running`.
- Stage 3 live GitHub controlled mutation uses a command-local approval record bound to the exact approved repo, issue, and planned lifecycle payload. It must not create durable `approvals`, **Work Items**, **Run Attempts**, scheduler state, PR handoff state, or dispatch state.
- Stage 3 dry-run/live comparison uses a Stage 3-specific lifecycle plan: add `odin:running`, remove `odin:running`, add `odin:human-review`, and add one Odin comment. The Stage 3 plan must not include `odin:failed`; Stage 2 failed-path dry-run proof remains historical evidence of the broader dry-run planner.
- Stage 3 live GitHub controlled mutation is idempotent only when rerunning the approved live command performs zero additional GitHub writes after the final label state and Odin comment marker already exist. The Stage 3 comment must carry a stable marker so Odin can detect the existing proof comment without duplicate comments.
- Stage 3 live GitHub controlled mutation targets the existing `odin-core` GitHub repo, `marcusgoll/odin-os`, and must not add a separate test project key. The live command must fail before any write unless `--approved-target` exactly matches the resolved repo and issue.
- Stage 3 live GitHub controlled mutation must use a pre-existing manually selected test issue. Odin must not create a GitHub issue as part of Stage 3.
- Stage 4 local Codex worker dry-run must not launch a local Codex process. It constructs a redacted `codex exec` command plan, renders the worker prompt, creates an isolated worktree, simulates bounded timeout behavior, and emits deterministic worker output containing `make odin-e2e-local`.
- Stage 4 local Codex worker dry-run must create a real temporary Git worktree under the Odin worktree root using a dry-run branch name, prove the path is inside the root, and clean it up after the proof unless `--keep-worktree` is set.
- Stage 4 local Codex worker dry-run prompt guardrails must require repo audit before editing, reuse of existing Odin primitives, no parallel command surfaces or registries, `odin work ...` as canonical Delivery Workflow surface, no token or secret exposure, no PR creation or update, and final output that runs or reports `make odin-e2e-local`.
- Stage 4 local Codex worker dry-run must both redact token-like values from JSON, logs, reports, and artifacts and construct a sanitized Codex environment plan that excludes token-bearing environment variables such as `GITHUB_TOKEN`, `GH_TOKEN`, `API_TOKEN`, and `ODIN_TRADEBOARD_API_TOKEN`.
- Stage 4 local Codex worker dry-run is command-local proof: it must not persist **Work Items**, **Run Attempts**, approvals, scheduler jobs, PR handoff state, or durable worktree lease rows. Durable worker/dispatch state is deferred to a later stage.
- Stage 5 PR creation dry-run is a read-only **Human Review Handoff** draft produced by `odin work pr-dry-run --worktree <path> --base <branch> --json`. It generates a diff summary, PR body draft, human checklist, zero-push proof, zero-merge proof, and zero GitHub PR API write proof without pushing, creating or updating a live pull request, merging, or persisting durable PR handoff state.
- Stage 5 PR creation dry-run must generate a PR body draft with the required `## Summary`, `## Proven`, `## Unproven`, and `## Commands Run` headings, validate it with `scripts/ci/verify-pr-template.sh`, and include a human checklist for diff review, command sufficiency, unproven risk acceptance, zero push, zero live PR mutation, and zero merge.
- Stage 5 PR creation dry-run may persist local disposable draft artifacts under the Odin runtime artifact area, including PR body, human checklist, and diff summary files. JSON output must include artifact paths and SHA-256 hashes, and the artifacts must be labeled as draft artifacts, not durable PR handoff state or live pull requests.
- Stage 5 PR creation dry-run consumes an existing local worktree and branch with changes against the requested base branch. It must fail clearly when there is no diff, and it must not synthesize fixture changes as part of the command.
- Stage 6 live docs-only PR proof is a command-local live **Human Review Handoff** proof for one approved low-risk docs-only issue. It may push one task branch, create one pull request, and add Odin-authored review evidence comments for that approved issue, but it must not launch Codex reviewer/QA workers, scheduler dispatch, durable **Run Attempts**, autonomous merge, or deployment.
- Stage 6 "review agents comment" means Odin-authored Stage 6 review evidence comments on the live pull request, not full autonomous reviewer/QA **Run Attempts**. Those comments must be clearly labeled as Stage 6 review evidence for human review and must not imply approval to merge or deploy.
- Stage 6 live docs-only PR proof must consume an existing operator-approved docs-only issue and an existing docs-only worktree or branch diff. Odin must not create the issue, invent the docs change, or synthesize fixture changes as part of the Stage 6 live command.
- Stage 6 live docs-only PR proof must use a separate live command, `odin work pr-create --issue <number> --approved-target <repo>#<issue> --worktree <path> --base <branch> --json`, rather than adding a live mode to `pr-dry-run`. The live command should reuse Stage 5 diff, PR body, checklist, and template-validation internals while keeping push and pull-request creation behind the exact approved target gate.
- Stage 6 live docs-only PR proof is complete only when the PR exists, CI runs `make odin-e2e-local`, the review evidence comments exist, merge remains human-gated, and no deployment happens.
- Stage 1 live GitHub read-only proof targets the existing `odin-core` system project identity. Implementation must not add a second `odin-os` managed project key for the same repository; instead, `odin-core` may carry GitHub intake metadata while preserving its stricter self-governance constraints.
- Every mutating agency worker must map to one **Work Item**, one **Run Attempt**, one task-owned branch, and one active mutable worktree lease.
- The **Agency Orchestrator** must not commit directly to a default branch, merge pull requests, or deploy production without a separate explicit human approval path.
- Agency workers must not receive production secrets or unrestricted host credentials. Project enrollment and executor configuration must define the allowed development credentials and command surface.
- A **Human Review Handoff** is required before merge or production deployment; passing QA and review agents is evidence for the human, not an autonomous approval.
- Agency orchestration must support dry-run dispatch planning and a kill switch before unattended worker launch is considered complete.
- A future Codex app-server runner must remain behind the shared executor contract; app-server thread, turn, and streaming-event details must not leak into the durable **Work Item** model.
- A **Knowledge Source** must declare its scope before ingestion or retrieval.
- Personal reference documents such as pilot contracts, books, and manuals default to global scope and restricted policy unless their **Knowledge Source Manifest** explicitly binds them to a managed-project scope.
- Each **Knowledge Source** must have a **Knowledge Source Manifest** before Odin treats it as an approved retrieval input.
- A **Knowledge Source** must retain provenance that lets an operator trace an answer or retrieved excerpt back to the source document, source location when available, ingest time, and extractor used.
- A **Knowledge Source Manifest** must declare source type, source location, checksum or external reference, scope, refresh policy, extractor expectations, and citation rules.
- Large or licensed PDF sources, including pilot contracts and books, should be referenced as **Knowledge Artifacts** rather than committed directly to Git unless a future locked decision explicitly allows that source class in Git.
- Pilot contracts, books, manuals, and other sensitive or licensed PDFs are **Restricted Knowledge Sources** by default.
- **Restricted Knowledge Sources** must not be bulk-exported, broadly shared, or injected into Codex context without an explicit operator action and manifest-compatible policy.
- Retrieval from a **Restricted Knowledge Source** should prefer narrow cited excerpts, page or location references, and summaries over broad reproduced content.
- **Knowledge Source** ingestion, search, refresh, and restricted-source approvals must be exposed through the **Knowledge Operator Surface** before they are treated as complete operator features.
- Real E2E verification for knowledge behavior must exercise `odin knowledge ...` once that command family exists; REPL aliases and direct file edits are insufficient proof.
- **Knowledge Source Lifecycle** states are limited to declared, artifact_available, extracted, indexed, ready, stale, and failed.
- A **Knowledge Source** may be ready for narrow scoped retrieval while still requiring **Restricted Knowledge Use Approval** for bulk export, broad extraction, sharing, or executor context injection.
- **Restricted Knowledge Use Approval** must be scoped to a specific **Restricted Knowledge Source**, requested use, operator decision, and evidence record; it must not grant standing unrestricted access unless a future locked decision explicitly adds that policy.
- V1 **Restricted Knowledge Use Approval** use types are bulk_export, broad_extraction, sharing, and executor_context_injection.
- **Restricted Knowledge Use Approval** is single-use by default; repeated broader use requires a new approval event unless a future locked decision explicitly adds standing approvals.
- V1 extraction supports only Markdown, plain text, and machine-readable PDFs.
- Scanned PDFs, image-only PDFs, and other **OCR-Required Knowledge Artifacts** must not silently advance to extracted, indexed, or ready.
- When a source requires OCR, Odin should leave the **Knowledge Source Lifecycle** at artifact_available or failed with extractor evidence such as `ocr_required`.
- Any future OCR support must record extractor provenance, page-location confidence, and restricted-source policy before OCR-derived content may be used for retrieval.
- Extracted or indexed knowledge derived from a **Knowledge Source** must be rebuildable from the source document and ingestion metadata; it must not become an untraceable standalone memory blob.
- **Knowledge Sources** must not be loaded into Codex context as unbounded dumps; Odin should retrieve scoped excerpts or summaries through an operator-visible retrieval path.
- **Knowledge Sources** must not duplicate **Workflow Registry Entries**, skills, commands, or policies; registry and policy assets remain governed by their own authored contracts.
- Final airline-facing writes must go through an explicit Odin operator surface when an Odin workflow is the requested path.
- Live Marcus FLICA workflows are **Operator-Invoked** only; scheduled or autonomous live airline-facing runs are not allowed unless a future locked domain decision explicitly changes the invocation mode.
- Every Marcus FLICA airline-facing write requires **Operator Approval**, even when a **Workflow Helper** or PBS recommendation is high confidence.
- FLICA workflows that require live browser proof must use Huginn when available.
- Operator-attended authentication, including Duo, is part of the workflow boundary and must not be treated as autonomous credential failure.
- A workflow is incomplete until its declared **Proof Requirement** is satisfied.
- Completed airline-facing writes require both internal downstream action evidence and **External Readback** when FLICA provides a readable confirmation surface.
- When FLICA does not provide a readable confirmation surface for a workflow, the **Workflow Registry Entry** must declare **Substitute Proof** before the workflow can be treated as complete.
- **Workflow Helpers** may prepare, inspect, map, draft, and verify, but they must not own final airline-facing writes.
- Each **Workflow Helper** used by a live Marcus FLICA workflow must have a **Helper Permission** declared in the relevant **Workflow Registry Entry** before it touches Huginn, PBS/flight-api, external UI state, or workflow evidence.
- **Helper Permissions** must declare allowed tasks, forbidden tasks, stop conditions, and accessible surfaces; they must not authorize helper-owned operator approval or final airline-facing submission.
- **Seniority Bid Workflow**, **FCFS Bid Workflow**, **TradeBoard Workflow**, and **Annual Vacation Workflow** must complete a **Schedule Preflight** before any airline-facing write.
- **Seniority Bid Workflow** and **FCFS Bid Workflow** share the **Bid Action** model; their domain distinction is the **Trade Evaluation Mode**.
- **TradeBoard Workflow** uses the distinct **TradeBoard Action** model for post-award airline trade surface work and must not reuse **Bid Action** for split-trip posts, exchanges, drops, or post-award pickups.
- **Annual Vacation Workflow** uses the distinct **Annual Vacation Action** model and must not treat airline annual vacation writes as generic **Schedule Workflow** updates or personal calendar events.
- **Schedule Workflow** owns the Odin-facing production and validation of the canonical **Schedule Snapshot** used by **Schedule Preflight**; other FLICA workflows must not define separate schedule freshness or readback models.
- **Schedule Preflight** for live airline-facing writes must satisfy the **Schedule Freshness Requirement** declared by the relevant **Workflow Registry Entry** before approval or submission.
- Airline-facing **Bid Actions**, **TradeBoard Actions**, and **Annual Vacation Actions** share the **Action Lifecycle**; action type identifies the workflow domain while lifecycle state identifies progress and proof status.
- An airline-facing action is not **completed** until its declared **Proof Requirement** is satisfied, including **External Readback** when FLICA provides a readable confirmation surface.
- Every airline-facing **Bid Action**, **TradeBoard Action**, and **Annual Vacation Action** must produce or update an Odin-owned **Action Record** before it can be treated as internally recorded, externally read back, completed, failed, or abandoned.
- Each top-level Marcus FLICA workflow must have a **Workflow Registry Entry** before live airline-facing use is allowed.
- **Action Records** are append-only: lifecycle changes, corrections, approvals, submissions, failures, abandonment, and readbacks must be recorded as **Action Evidence Events** rather than overwriting prior evidence.
- **Operator Approval** is append-only and scoped to exactly one prepared action payload; material changes to target activity, action type, split scope, bid mode, vacation period, comment, submit path, or other airline-facing payload fields require a new approval evidence event before submission.
- A **Prepared Action Payload** is immutable once presented for **Operator Approval**; material changes create a new payload and invalidate the prior approval for submission.
- Every **Action Record** belongs to exactly one **Workflow Run**; a **Workflow Run** may complete without an **Action Record** when it only inspects, preflights, verifies, fails, or is abandoned before an airline-facing action is prepared.
- **Workflow Run Outcome** and **Action Lifecycle** are separate; a completed **Workflow Run** must not imply a completed airline-facing action unless the run contains an **Action Record** whose **Action Lifecycle** is completed.
- Every live airline-facing **Prepared Action Payload** and **Workflow Registry Entry** must declare a **Submit Path** before approval or submission.
- A **Submit Path** that requires browser authentication, Duo, live FLICA UI interaction, or browser proof must use Huginn and must not fall back to plain Playwright or undeclared direct scripts.
- Every workflow that requires **External Readback** must declare a **Readback Path** before it can be treated as complete.
- A **Readback Path** that requires live FLICA UI, browser authentication, Duo, or browser proof must use Huginn and must not treat internal downstream success as airline-owned readback.
- **Substitute Proof** is allowed only when no readable FLICA confirmation surface exists for the workflow; it must not be used as a convenience replacement for available **External Readback**.

## Relationships

- The **Marcus FLICA Operations Workflow Suite** contains one or more **Workflow Registry Entries**.
- The **Marcus FLICA Operations Workflow Suite** has five top-level workflows: **Seniority Bid Workflow**, **FCFS Bid Workflow**, **TradeBoard Workflow**, **Annual Vacation Workflow**, and **Schedule Workflow**.
- A **Workflow Registry Entry** names one **Operator Surface** as its entrypoint.
- A live **Workflow Run** in the **Marcus FLICA Operations Workflow Suite** must be **Operator-Invoked** from its declared **Operator Surface**.
- A **Workflow Registry Entry** may produce many **Workflow Runs** over time; each **Workflow Run** belongs to exactly one **Workflow Registry Entry**.
- A **Workflow Run** may contain zero or more **Action Records**; each **Action Record** belongs to exactly one **Workflow Run**.
- A **Workflow Run Outcome** describes invocation result, while **Action Lifecycle** describes proof progress for airline-facing actions created during that run.
- A **Workflow Registry Entry** is the declared safety and proof contract for live airline-facing use of a top-level Marcus FLICA workflow.
- The **Schedule Workflow** produces or validates the canonical **Schedule Snapshot** and provides **Schedule Preflight** according to the **Schedule Freshness Requirement** declared by each other top-level Marcus FLICA workflow.
- **Seniority Bid Workflow** and **FCFS Bid Workflow** produce **Bid Actions** that include target activity, active BCID, **Trade Evaluation Mode**, **Schedule Preflight**, **Operator Approval**, internal action evidence, and **External Readback** or declared **Substitute Proof**.
- **TradeBoard Workflow** produces **TradeBoard Actions** that include post-award target activity, requested trade surface operation, split-trip scope when applicable, **Schedule Preflight**, **Operator Approval**, internal action evidence, and **External Readback** or declared **Substitute Proof**.
- **Annual Vacation Workflow** produces **Annual Vacation Actions** that include vacation target period or status target, requested vacation operation, **Schedule Preflight** when the operation is airline-facing, **Operator Approval** for airline-facing writes, internal action evidence, and **External Readback** or declared **Substitute Proof**.
- The **Action Lifecycle** is shared across airline-facing action models and is interpreted through each action's declared workflow, proof requirement, and downstream capability owner.
- An **Action Record** is the Odin-owned traceability link between operator approval, schedule evidence, downstream PBS/flight-api evidence, Huginn/browser evidence when used, and completion proof.
- An **Action Record** may expose a current lifecycle state, but that state must be derived from or backed by append-only **Action Evidence Events**.
- An **Operator Approval** is recorded as an **Action Evidence Event** on the relevant **Action Record** and must identify the **Prepared Action Payload** it authorizes.
- A **Prepared Action Payload** is created before approval and becomes the exact payload submitted to the downstream capability unless a new payload and approval are recorded.
- A **Submit Path** connects a **Prepared Action Payload** to the declared downstream capability or Huginn browser route used for submission.
- A **Readback Path** connects a **Proof Requirement** to the declared downstream capability or Huginn browser route used to collect **External Readback**.
- **Substitute Proof** connects a **Proof Requirement** to declared alternate evidence when **External Readback** is unavailable.
- A FLICA workflow may call PBS/flight-api as a downstream capability owner.
- A FLICA workflow may require Huginn for live proof, authentication, or readback.
- PBS action records and FLICA readback evidence can satisfy Odin **Proof Requirements** when the workflow declares them.
- Internal downstream action records prove that Odin/PBS attempted or submitted the action; **External Readback** proves FLICA accepted or displays the result.
- A **Workflow Registry Entry** may compose **Workflow Helpers** when their **Helper Permissions**, responsibilities, and stop conditions are explicit.
- An **Initiative** may contain many **Work Items**; each **Work Item** belongs to one owning **Initiative** when initiative context is known.
- A **Feature Work Item** may decompose into smaller **Work Items** for planning, implementation, review, verification, or follow-up.
- A **Work Item** may have many **Run Attempts**; each **Run Attempt** belongs to exactly one **Work Item**.
- A **CEO Briefing Workflow** produces many **Briefing Proposals** over time.
- A **Briefing Proposal** may publish zero or one **Daily Priority Packet** after approval.
- A **Daily Priority Packet** belongs to exactly one business date and may supersede at most one prior active packet for that date.
- A **Run Attempt** may have at most one active **Live Execution Session** handle for attachment and liveness, plus historical handoff or completion evidence after the process exits.
- A **Live Execution Session Key** identifies the session across status, attach, handoff, stop, liveness projections, and evidence records; aliases are lookup aids only.
- Parallel live sessions on the same **Work Item** are sibling **Run Attempts**, not multiple active sessions on one **Run Attempt**.
- Mutating sibling **Run Attempts** require separate worktree paths; read-only sibling **Run Attempts** may share a repo root only while they remain read-only.
- Cached **Live Execution Session** liveness belongs to read projections with freshness timestamps; live process probes update those projections only when an operator requests status or attachment.
- Attachment is an operator presence action, not session creation, adoption, resumption, or proof advancement.
- Stopping a **Live Execution Session** in Odin records lifecycle state; process termination is a separate explicit action.
- Abandoning a mutating **Live Execution Session** is an explicit outcome that records why handoff is unavailable or intentionally skipped.
- An **Adopted Live Execution Session** is the same relationship as a normal **Live Execution Session**, but its start event happened outside Odin and its Odin-governed proof begins only after adoption and explicit verification.
- A **Delivery Workflow** coordinates one or more **Work Items** and records proof through Odin-owned runtime evidence, not through executor-local claims alone.
- A **Delivery Workflow** contains an ordered sequence of **Delivery Gates**; each gate has evidence, status, and a next-action decision.
- A **Feedback Loop** composes one or more skills, agents, and workflow registry entries around the current **Delivery Gate**.
- A **Delivery Workflow** selects one **Delivery Profile** for the current **Work Item** before execution begins.
- A **Delivery Profile** composes skills, agents, gates, proof requirements, and failure branches through a specialized workflow registry entry; it does not own **Work Item** state.
- The **Agency Orchestrator** coordinates many **Work Items** across many managed projects through the existing **Delivery Workflow** model.
- An **Issue Intake Source** may map to zero, one, or many **Work Items** over time when planning decomposes a large issue into smaller governed work.
- A **Human Review Handoff** belongs to one **Work Item** or grouped delivery outcome and may reference one or more pull requests, QA artifacts, reviewer notes, and follow-up **Work Items**.
- `systematic_debugging` may return a **Work Item** to an earlier gate when root-cause evidence changes the domain, design, plan, or implementation assumptions.
- `writing_skills` may create a follow-up **Work Item** when learning_reviewed identifies a reusable skill gap, but it is not required for every completed delivery.
- A **Knowledge Source** belongs to one explicit scope, such as global, odin-core, or a managed project.
- Personal reference **Knowledge Sources** belong to global scope by default; project-specific **Knowledge Sources** require an explicit managed-project scope in the **Knowledge Source Manifest**.
- A **Knowledge Source** is declared by exactly one **Knowledge Source Manifest**.
- A **Knowledge Source Manifest** may reference one or more **Knowledge Artifacts** when the source bytes live outside Git or in an Odin-managed artifact store.
- A **Restricted Knowledge Source** is a policy classification of a **Knowledge Source** and should be declared in its **Knowledge Source Manifest**.
- The **Knowledge Operator Surface** creates, validates, indexes, refreshes, searches, and shows **Knowledge Sources** through their **Knowledge Source Manifests** and **Knowledge Artifacts**.
- A **Knowledge Source** has one current **Knowledge Source Lifecycle** state derived from its manifest, artifact availability, extraction result, index result, refresh policy, and latest failure evidence.
- A **Restricted Knowledge Use Approval** belongs to one **Restricted Knowledge Source** and one requested broader-use action; it does not change the source's **Knowledge Source Lifecycle**.
- A **Restricted Knowledge Use Approval** records one use type, one reason, one operator decision, and one evidence record.
- A **Knowledge Source Manifest** declares the source class and extractor expectations used to decide whether the source is a **Supported Knowledge Source Class** or an **OCR-Required Knowledge Artifact**.
- A **Knowledge Source** may produce many derived retrieval artifacts over time, including extracted text, chunks, embeddings, summaries, and index records.
- Derived retrieval artifacts must reference their **Knowledge Source** and must be replaceable when the source document or extractor changes.

## Open Questions

- Dedicated Odin operator surfaces still need to be implemented or audited for non-TradeBoard workflows, especially Annual Vacation.
- Action Record persistence, append-only Action Evidence Events, Prepared Action Payload identity, and Workflow Run Outcome enforcement still need implementation alignment with the registry contracts.
- The current code still exposes `task` and `run` in several runtime surfaces; implementation planning must decide where to keep those as storage/API compatibility terms and where to render canonical **Work Item** and **Run Attempt** language.
- `odin work ...` is the canonical Delivery Workflow operator command family. Intake for Stage 1 live GitHub read-only proof must use `odin work intake --project odin-core --json`, backed by the existing Issue Intake Source service, rather than a parallel top-level `odin intake github` command. Stage 2 dry-run lifecycle proof must use `odin work simulate-lifecycle --issue <number> --json`, reusing existing tracker lifecycle labels, rather than a parallel top-level `odin tracker ...` operator surface. Stage 3 live GitHub controlled mutation must use a separate `odin work apply-lifecycle --issue <number> --json` command so live writes are not hidden behind a command named `simulate`. Stage 4 local Codex worker dry-run must use `odin work worker-dry-run --issue-fixture <path> --json`, not a parallel top-level `odin worker ...` command. Stage 5 PR creation dry-run must use `odin work pr-dry-run --worktree <path> --base <branch> --json` as a read-only Human Review Handoff draft, not live PR creation. Stage 6 live docs-only PR proof must use `odin work pr-create --issue <number> --approved-target <repo>#<issue> --worktree <path> --base <branch> --json`, not `pr-dry-run --live`.
- Always-on multi-project agency orchestration has no current canonical operator surface beyond the planned **Delivery Workflow** surface. Implementation planning must decide the exact `odin work ...` subcommands for queue, dispatch, review handoff, dry-run, and kill-switch operations without creating a parallel top-level agency authority.
- `odin brief ceo` is not implemented yet in the current binary; implementation work must add that canonical command family and must not rely on legacy `odin-orchestrator` executive_review launchers.
- `odin workspace ...` is not implemented yet in the current binary; implementation work must add a canonical binding, adoption, and attachment surface before **Live Execution Sessions** can be managed as Odin-governed operator state.
- A future ADR may promote **Delivery Profiles** to a separate registry kind only if specialized workflow entries prove insufficient for validation, UI behavior, or lifecycle semantics.
- `odin knowledge` is not implemented yet in the current binary; implementation work must add that canonical command family rather than relying on direct file edits or REPL-only commands.
- The exact `odin knowledge` flag spelling for creating **Restricted Knowledge Use Approval** events is still unresolved, but the single-use and use-type scoped approval model is locked.
- OCR support for scanned PDFs and image-only documents is intentionally unresolved and out of scope for v1.
