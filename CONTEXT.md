# Odin OS

Odin OS is a workspace-governed orchestration system for durable execution, memory, approvals, and delegated work across software and non-project operations. This context file captures the language boundaries for the control plane that owns ongoing responsibilities and routes bounded execution.

## Language

**Workspace**:
The durable top-level operating environment that owns defaults, policy baselines, integrations, schedules, and the inventory of active initiatives.
_Avoid_: account, tenant

**Initiative**:
A durable unit of responsibility inside a Workspace that owns ongoing work independent of any single run.
_Avoid_: project, task

**Scope**:
The active control boundary that determines which workspace, initiative, memory, policy, and runtime surfaces apply to the current request or run.
_Avoid_: context, workspace state

**Managed Project**:
An Initiative kind whose work is Git-governed and executed under project policy, worktrees, and branches.
_Avoid_: repo, workspace

**Managed Domain**:
An Initiative kind for non-Git operational responsibility such as family finance or household administration.
_Avoid_: project, assistant

**Companion**:
A durable AI operating role inside a Workspace such as assistant, advisor, operator, or specialist.
_Avoid_: worker, provider

**Worker**:
An ephemeral execution unit spawned to advance one bounded step of work under Companion, policy, and scope constraints.
_Avoid_: companion, persona

**Intake Inbox**:
The global intake surface where raw signals such as alerts, logs, prompts, and external events arrive before classification.
_Avoid_: queue, backlog

**Intake Item**:
A durable raw intake record stored in the Intake Inbox before triage decides whether to suppress it, enrich it, or link it to one or more Work Items.
_Avoid_: work item, event blob

**Initiative Intake**:
The generic intake-routing layer that classifies an Intake Item into the right Initiative kind, control scope, and next-step handling before any project-specific intake workflow runs.
_Avoid_: project intake, universal triage hack

**Work Queue**:
The scope-owned queue of triaged executable work items that are ready to become Odin tasks and runs.
_Avoid_: inbox, event stream

**Work Item**:
The durable unit of governed work that can be queued, blocked, approved, resumed, and completed inside a Scope.
_Avoid_: run, prompt

**Run Attempt**:
One concrete execution attempt for a Work Item.
_Avoid_: work item, session

**Operator Surface**:
The human-facing control path Odin exposes for governed commands, approvals, status, and state transitions.
_Avoid_: wrapper script, hidden admin flow, playbook

**Observability**:
The read-only operator understanding layer for logs, health, metrics, incidents, recoveries, projection freshness, and cross-scope runtime readbacks.
_Avoid_: control plane, source of truth, execution lane

**Odin Observer Role**:
The Odin-owned observability role inside `odin serve` that exports runtime-derived health, readiness, metrics, and structured logs for external telemetry backends. It is not a separate runtime authority or a second observer service unless a future locked decision explicitly creates one.
_Avoid_: parallel observer daemon, duplicated health derivation, dashboard-owned runtime truth

**Runtime Readiness**:
The machine-oriented safety state that says whether a runtime root is safe to operate, exposed by readiness endpoints and `healthcheck`; `doctor` explains the underlying health evidence.
_Avoid_: dashboard status, work status

**Automation Trigger**:
A schedule-based or event-based rule that creates or updates a governed Work Item instead of launching execution directly.
_Avoid_: cron job, direct worker spawn

**Follow-Up Obligation**:
The v1 schedule-backed Automation Trigger that records a promised next action, reminder, recurring check-in, or recurring obligation and materializes due occurrences into governed Work Items.
_Avoid_: cron entry, reminder task, background job

**Approval Request**:
A durable governance object linked to a Work Item and optionally to the triggering Run Attempt when human review or explicit authorization is required before proceeding.
_Avoid_: approval flag, prompt confirmation

### Browser Control

**Browser Control**:
The Integration-context capability that lets Odin inspect and act through a live browser without turning browser machinery into workflow-owned domain state.
_Avoid_: Huginn feature, browser bot, workflow automation

**Trusted Browser Session**:
A reusable authenticated browser context with persistent profile state that **Browser Control** may attach to, save, and expose for human step-in.
_Avoid_: finance session, social session, task session

**Browser Intervention**:
An explicit human-takeover checkpoint inside **Browser Control** for login, MFA, CAPTCHA, approval, or other live blockers before agent execution may continue.
_Avoid_: manual hack, hidden step, background credential handling

**Browser Intervention Reason**:
A closed coarse token that states why a **Browser Intervention** requires human action across workflows.
_Avoid_: driver error, UI failure, workflow-specific blocker string

**Transfer Intent**:
The operator-approved description of a requested money movement that Odin governs through existing workflow state rather than a dedicated v1 transfer aggregate.
_Avoid_: transfer row, payment object

**Transfer Direction**:
The Robinhood-relative orientation of a Transfer Intent, where `deposit` moves funds into Robinhood and `withdraw` moves funds out of Robinhood.
_Avoid_: generic direction, ledger direction

**Funding Account**:
The non-Robinhood account that provides funds for a deposit or receives funds from a withdrawal.
_Avoid_: source account, destination account

**Robinhood Account**:
The Robinhood-side cash or brokerage account that is the in-platform leg of a Transfer Intent.
_Avoid_: destination account, source account

**Finance Principal**:
The human on whose behalf a governed finance workflow is being performed and whose real-world financial accounts, credentials, and approvals are materially in play.
_Avoid_: operator, account owner, user

**Transfer Status View**:
A derived operator-facing read model that summarizes a Transfer Intent from existing workflow state and may expose a compact derived status without becoming a separate source of truth.
_Avoid_: transfer table, canonical transfer record

**Conversation Transcript**:
A durable scoped record of a direct-answer or companion-conversation exchange that resolves an Intake Item without creating governed work.
_Avoid_: run log, work item transcript

**Dedupe Key**:
A deterministic intake identity derived by Odin from Workspace, source family, and a normalized signal fingerprint, with optional narrowing after Initiative resolution, used to explain duplicate linking and cooldown behavior across Intake Items.
_Avoid_: raw payload hash, connector-only nonce

**Source Facts**:
The normalized ingress facts supplied by adapters and persisted on an Intake Item, such as source family, external object id, event kind, normalized subject identifiers, and observed time, which Odin uses to derive dedupe and routing decisions.
_Avoid_: raw vendor payload, transient webhook blob

**Dedupe Recipe Version**:
The durable version marker for the normalization and fingerprinting recipe Odin used to derive a Dedupe Key from Source Facts for an Intake Item.
_Avoid_: app version, connector release tag

### Marcus Social Operations

**Social Copilot**:
The governed Marcus-specific social operating lane that uses existing workflow, memory, and tool surfaces to plan, approve, publish, and review content.
_Avoid_: social manager, autoposter, growth bot

**Social Draft**:
An approval-pending candidate post or reply captured as a `social_draft` memory record.
_Avoid_: published post, outcome

**Reply Suggestion**:
A proposed response to an external social post that Odin may rank and draft for Marcus but does not publish through the current live surface.
_Avoid_: live reply, autoposted response

**Social Outcome**:
The approved, rejected, or published decision record for social content captured as a `social_outcome` memory record.
_Avoid_: draft, analytics event

**Social Evidence**:
Operator-observed evidence for an explicit published social artifact captured as a `social_evidence` memory record.
_Avoid_: analytics scrape, crawler result

## Relationships

- A **Workspace** owns many **Initiatives**
- A **Workspace** may assign one or more **Companions** across its **Initiatives**
- A **Scope** resolves the active **Workspace** and may narrow to one **Initiative**
- A **Managed Project** is one kind of **Initiative**
- A **Managed Domain** is one kind of **Initiative**
- A **Companion** is a durable role that may supervise or request **Worker** effort
- A **Worker** performs bounded execution and does not own durable responsibility
- An **Operator Surface** is the human-facing entrypoint for governed state transitions over existing runtime authority
- The **Intake Inbox** receives raw signals as durable **Intake Items** that belong to a **Workspace** first, before final **Initiative** or **Scope** resolution
- **Initiative Intake** sits above project-specific intake workflows and decides whether an **Intake Item** belongs in **Managed Project** or **Managed Domain** handling
- An **Intake Item** may be suppressed, enriched, answered directly, re-triaged, or linked to one or more **Work Items**
- A directly answered **Intake Item** should link to a durable **Conversation Transcript** instead of creating a lightweight **Work Item**
- One **Intake Item** may both resolve with a **Conversation Transcript** and create one or more follow-up **Work Items** when the answer itself creates durable obligations
- Follow-up **Work Items** created from a direct-answer path should keep explicit backlinks to the originating **Intake Item** and **Conversation Transcript**
- Repeated equivalent raw signals should still create durable **Intake Items** and then be marked `suppressed` or duplicate-linked to a canonical **Intake Item** rather than being dropped before ingress
- Duplicate detection should be **Workspace**-local only, with optional further narrowing after **Initiative** resolution, and must never collapse intake across different **Workspaces**
- Canonical duplicate linking should be cooldown-bounded so equivalent signals after the dedupe window create a fresh active **Intake Item** instead of linking forever to an old canonical item
- An **Intake Item** should carry an Odin-owned **Dedupe Key** derived from **Workspace**, source family, and normalized signal fingerprint rather than relying on raw payload similarity alone
- Adapters and connectors should provide normalized source facts, while Odin core computes the final **Dedupe Key** centrally so duplicate identity stays consistent across ingress paths
- An **Intake Item** should persist its **Source Facts** alongside the derived **Dedupe Key** so duplicate and routing decisions remain auditable and recomputable
- An **Intake Item** should persist the **Dedupe Recipe Version** used to derive its **Dedupe Key** so later recomputation remains honest when Odin changes normalization or fingerprinting logic
- A **Social Copilot** creates and revises **Social Drafts**
- The **Social Copilot** operator path is a workflow-scoped **Operator Surface** over `/workflow social ...`; it delegates to the Social Copilot runtime service and must not own a separate social queue or runtime state model
- The **Social Copilot** polling loop is owned by one workflow task per environment, with watch scope, checkpoint, cooldown, and `account_actions=none` evidence recorded in existing task, run, context-packet, and memory records
- The engagement lane creates **Reply Suggestions** for candidate conversations before any publish decision exists
- Marcus approval or rejection turns a **Social Draft** into a **Social Outcome**
- A published **Social Outcome** may carry one or more **Social Evidence** records
- **Social Evidence** is tied to explicit published artifacts, not broad profile crawling or generalized analytics ingestion
- In the current live surface, **Reply Suggestions** stay suggestion-only; native publish is limited to top-level X posts
- Duplicate-linked arrivals should remain individual **Intake Items** that reference a canonical **Intake Item** rather than introducing a first-class runtime duplicate-group aggregate
- Duplicate-wave summaries such as duplicate count, latest duplicate arrival, and cooldown state should be derived operator projections rather than mutable fields on the canonical **Intake Item**
- The canonical **Intake Item** for an active dedupe window should remain stable even if a newer duplicate arrives with richer evidence or higher severity; newer arrivals add evidence and operator context without replacing canonical ownership mid-window
- A duplicate arriving inside the active dedupe window may reopen or re-triage the canonical **Intake Item** when it adds materially new evidence or crosses a severity threshold, while keeping the same canonical **Intake Item**
- When material-change re-triage reaches already linked follow-up work, Odin should reuse or requeue the existing **Work Item** when it represents the same durable obligation, and create a new **Work Item** only when the new evidence creates a distinct obligation
- A **Work Queue** belongs to one **Scope** and contains triaged **Work Items**
- A **Work Item** may produce one or more **Run Attempts**
- **Observability** consumes runtime truth and exposes read-only health, metrics, incidents, recoveries, projection freshness, and cross-scope readbacks; it must not become a second authority for work, readiness, or execution state
- **Runtime Readiness** belongs to the runtime/operations boundary and is surfaced through `healthcheck` and readiness endpoints; `/overview` and `doctor` may display readiness evidence but do not own readiness transitions
- An **Automation Trigger** creates or updates **Work Items** before any **Worker** is dispatched
- A **Follow-Up Obligation** is an **Automation Trigger**, not a **Work Item**; its due occurrence may materialize a **Work Item** through the normal governed queue path
- A **Follow-Up Obligation** belongs to one **Workspace** and may narrow to one owning **Initiative** and one responsible **Companion**
- A materialized **Work Item** from a **Follow-Up Obligation** should keep explicit follow-up provenance so `/overview`, agenda, and audit surfaces can distinguish the trigger definition from the executable work it created
- A **Work Item** may create one or more **Approval Requests** across its lifecycle
- An **Approval Request** belongs to one **Work Item** and may reference the **Run Attempt** that produced the blocked state or evidence bundle
- Approval-support filters on an **Operator Surface** may narrow which pending **Approval Requests** are listed by workflow resolver support, but they are derived inspection filters only; they do not authorize batch mutation, bypass workflow-owned resolver support, or change the lifecycle of any **Approval Request**
- **Browser Control** belongs to the **Workspace** integration/tooling layer and may be invoked from an **Operator Surface** or by a **Companion** on behalf of a **Work Item**
- A **Trusted Browser Session** may be reused across multiple **Run Attempts**, but it does not become a domain-owned workflow aggregate
- A **Browser Intervention** pauses **Browser Control** so a human can complete a live blocker in the shared session before execution resumes
- A **Browser Intervention** may hand control back and forth between human and agent within the same live attached **Trusted Browser Session** without minting a new **Run Attempt** while that execution remains active
- If **Browser Control** must survive shell exit, worker handoff, approval wait, crash, or any other durable pause, Odin should resume through a fresh **Run Attempt** from persisted wake or evidence state rather than treating the old live session as the same continuing run
- A **Browser Intervention** should be represented in v1 as **Run Attempt** execution evidence plus optional wake or evidence context, not as a first-class persisted browser aggregate with its own lifecycle
- A **Browser Intervention** does not replace a workflow-owned **Approval Request**; business approval state remains on the owning workflow while browser blockers stay on browser and run evidence
- A **Browser Intervention** should expose exactly one coarse **Browser Intervention Reason** from the closed v1 set `login_required`, `mfa_required`, `captcha_required`, `human_confirmation_required`, `unexpected_live_blocker`
- Workflow-specific browser details such as `google_login_required`, `blocked_on_mfa`, `compose_surface_missing`, and `post_button_not_ready` should remain driver artifacts or **Run Attempt** evidence and should not expand the shared **Browser Intervention Reason** vocabulary automatically
- `unexpected_live_blocker` should be used only when human step-in is required but no narrower shared **Browser Intervention Reason** fits; ordinary browser startup, selector, typing, navigation, or click failures remain driver or execution failures rather than **Browser Interventions**
- In v1, generic **Browser Control** should reuse the existing `/tool` **Operator Surface** and builtin tool catalog rather than introducing a parallel `/browser` command family
- On that `/tool` surface, the canonical future generic **Browser Control** tool family should use `browser_*` catalog keys rather than adapter-branded `huginn_*` keys; adapter-specific names may remain compatibility or implementation language during transition, but they are not the long-term platform vocabulary
- A **Transfer Intent** may be represented in v1 by one governing **Work Item**, its latest **Run Attempt**, the active or terminal **Approval Request** chain, and the linked `approval_wait` wake packet history instead of a dedicated transfer-intent table
- A **Transfer Intent** belongs to one governing **Scope**; identical money-movement facts in different **Initiatives** or **Workspaces** are still distinct **Transfer Intents**
- A **Transfer Intent** belongs to one **Finance Principal** whose real-world financial accounts and credentials are in play for that money movement
- A **Transfer Intent** has one **Transfer Direction** interpreted from Robinhood's point of view, not from the household ledger or whichever account is listed first
- A **Transfer Intent** links one **Funding Account** and one **Robinhood Account**, while transport payloads may still label those legs as `source_account` and `destination_account`
- A **Finance Principal** is distinct from the **Operator** even when the same human fills both roles in one workflow; the **Finance Principal** owns the real-world money movement, while the **Operator** owns the current Odin approval or command action
- A trusted live Robinhood or finance browser session used for transfer execution belongs to one **Finance Principal**; **Companions**, **Work Items**, and **Run Attempts** may reference or request its use but do not own its credentials
- In v1, a trusted live Robinhood or finance browser session should remain execution-context evidence on **Run Attempts** and wake-packet lineage rather than becoming a first-class finance aggregate or separate source of truth
- Durable Robinhood access/bootstrap readiness and headed-browser helper capability belong to the **Workspace** integration/tooling layer; the Family-Ops finance domain may depend on that readiness and consume its evidence, but does not own the integration subsystem itself
- In v1 live Robinhood transfer workflows, any step that opens, authenticates, or reuses the trusted live finance session, including `/transfer prepare`, should also be principal-attended: the acting **Operator** must be the **Finance Principal**
- In v1 live Robinhood transfer workflows, the final approval that authorizes submit should be principal-attended: the approving **Operator** must also be the **Finance Principal** for that transfer
- In v1 live Robinhood transfer workflows, login, MFA, or other auth challenges should be treated as explicit operator checkpoints completed by the **Finance Principal** in the headed session rather than as automated or background credential-handling steps
- In v1 live Robinhood transfer workflows, login, MFA, and similar auth checkpoints should remain **Run Attempt** execution evidence and wake-packet context only; they should not create a second **Approval Request** type or a separate finance auth-governance object
- In the current Family-Ops Robinhood transfer lane, Marcus is the v1 **Finance Principal**
- A **Transfer Status View** is derived from the governing **Work Item**, the latest transfer-relevant **Run Attempt**, the latest transfer-relevant **Approval Request** chain, and the latest relevant wake packet for that same transfer lineage rather than from a separate transfer-owned store
- On **Transfer Status View**, the latest relevant wake packet should mean the wake packet explicitly linked to the selected transfer-relevant **Approval Request** when one exists, and otherwise the latest transfer-relevant wake packet in that same transfer lineage
- A **Transfer Status View** may expose a compact derived status for operator convenience, but that status must be computed from canonical **Work Item**, **Run Attempt**, **Approval Request**, and wake-packet state rather than becoming separate truth
- A **Transfer Status View** should not expose any derived transfer `status` before the first prepare run creates transfer-specific workflow evidence; until then, operators should read the governing **Work Item** state and detail directly rather than inventing a pre-transfer label such as `not_started`
- Before the first prepare run creates transfer-specific workflow evidence, `GET /api/transfers/tasks/{taskKey}` should still return the same **Transfer Status View** shape, but transfer-specific fields such as derived `status` should be omitted rather than replaced with placeholder values or a 404-style absence
- Before the first prepare run creates transfer-specific workflow evidence, transfer-specific identifiers such as `run_id` and `approval_id` should be omitted from **Transfer Status View** rather than returned as `null`, `0`, or placeholder values
- Before the first prepare run creates transfer-specific workflow evidence, transfer-specific evidence fields such as transfer `summary` and transfer `artifacts` should also be omitted from **Transfer Status View** rather than filled with generic work-item prose or empty placeholder objects
- On **Transfer Status View**, `task_key` should remain the v1 transport field naming the governing **Work Item** key; it is a transport alias, not a separate transfer-specific identifier
- On **Transfer Status View**, v1 should not add a parallel `work_item_key` field; `task_key` remains the only transport key for the governing **Work Item**
- V1 should not add a parallel `/api/transfers/work-items/{workItemKey}` read path; `/api/transfers/tasks/{taskKey}` remains the only transfer-status route for the governing **Work Item**
- On the v1 Robinhood transfer-prepare HTTP contract, `initiative_key` should be the canonical request field naming the governing **Initiative**; legacy `project_key` wording should be treated as compatibility-only rather than the canonical field name
- On the v1 Robinhood transfer-prepare HTTP contract, Odin may accept legacy `project_key` as a deprecated compatibility alias for `initiative_key` during transition, but `initiative_key` remains the only canonical field name
- On the v1 Robinhood transfer-prepare HTTP contract, if both `initiative_key` and deprecated `project_key` are supplied, Odin should accept the request only when they match and reject it when they conflict
- On the v1 Robinhood transfer-prepare HTTP contract, once request validation succeeds, Odin should normalize any accepted legacy `project_key` input into canonical `initiative_key` semantics before constructing runtime params, wake packets, transcripts, memory summaries, or other durable transfer evidence
- On the shell operator surface, `/transfer prepare` should stay initiative-context-scoped and should not add an explicit `initiative_key` or `project_key` argument of its own; shell scope selection belongs to the already selected initiative context
- If no initiative is currently selected on the shell operator surface, `/transfer prepare` should fail fast with an explicit "select an initiative first" style error rather than prompting interactively or defaulting to remembered scope
- On the shell operator surface, successful `/transfer prepare` output should expose the pending approval handle as `approval=<id>` rather than `approval_id=<id>`; `approval_id` remains HTTP and JSON transport language
- On the shell operator surface, successful `/transfer prepare` output should also echo the created `task=<key>` and `run=<id>` handles alongside `approval=<id>`, so operators receive the full immediate follow-up handle set on one line
- On the shell operator surface, successful `/transfer prepare` output should also print a concise second-line `summary=<...>` after the handle line so operators get immediate human confirmation that the review is prepared and awaiting approval
- On the shell operator surface, successful `/transfer prepare` output should not dump transfer evidence artifacts such as `review_url` or `screenshot_path`; artifact detail belongs to `/approvals`, `/runs show <run-id>`, and **Transfer Status View**
- On the shell operator surface, successful `/transfer prepare` output should also print a concise `next=<...>` hint that makes the approval-paused follow-up path explicit for the operator
- On the shell operator surface, the `next=<...>` hint printed after successful `/transfer prepare` should use exact shell-command guidance with the concrete `run` and `approval` handles Odin just produced rather than abstract prose about what to do later
- On the shell operator surface, the `next=<...>` hint printed after successful `/transfer prepare` should cover the full governed follow-up path through run evidence review and approval resolution, not only the first inspection commands; once Odin already printed `approval=<id>`, the immediate hint does not need to include `/approvals`
- On the shell operator surface, the run-review command inside the immediate `next=<...>` hint should use `/runs show <run-id>` rather than `/runs show active` because prepare success already produced the concrete `run=<id>` handle
- On the shell operator surface, the approval-resolution command inside the immediate `next=<...>` hint should inline the concrete approval handle Odin just printed, while leaving the operator decision itself open as `approve|deny`
- On the shell operator surface, the approval-resolution command inside the immediate `next=<...>` hint should stay as one compact template such as `/approvals resolve 17 <approve|deny> because <reason...>` rather than expanding into separate approve and deny command variants
- On the shell operator surface, the immediate `next=<...>` hint should also include the final post-resolution verification command so the guided path ends with an explicit check of the resumed run outcome
- On the shell operator surface, that final post-resolution verification command should target the submit continuation run returned by `/approvals resolve ...`, not the original prepare run, because approval resolution creates a fresh **Run Attempt**
- On the shell operator surface, the final verification step named in the prepare-time `next=<...>` hint should explicitly depend on the submit run returned by `/approvals resolve ...`; it should not imply that `/runs show <prepare-run-id>` remains the right post-submit check after approval
- On the shell operator surface, successful `/approvals resolve ...` output should also print `status=resolved` alongside the branch-specific `result=...` field so finance approvals reuse the shell's broader resolve idiom rather than becoming a finance-only format
- On the shell operator surface, successful `/approvals resolve ...` output should also print a concise second-line `summary=<...>` after the token receipt so operators get immediate human-readable confirmation without turning the receipt into a full transcript or evidence view
- On the shell operator surface, successful `/approvals resolve ...` output should remain a receipt-only surface and should not add its own `next=<...>` hint, because `/transfer prepare` already owns the longer guided path and resolve receipts already expose the concrete continuation handle when one exists
- On the shell operator surface, the `summary=<...>` line should stay natural-language and should not simply mirror raw status names such as `reprepare_required`, `submission_unconfirmed`, or `pending_approval`, because the structured token line already carries canonical status words
- On the shell operator surface, the approve-branch `summary=<...>` should say something like `summary=approval granted; submit continuation started` rather than `summary=transfer submitted`, because approval resolution starts submit continuation and returns the submit run but does not itself prove confirmed Robinhood acceptance
- On the shell operator surface, the approve-branch `summary=<...>` should stay short rather than embedding follow-up verification guidance such as `inspect /runs show <submit-run-id>`, because the receipt already prints `run=<submit-run-id>` and follow-up action belongs on handles or dedicated guidance rather than in the summary line
- On the shell operator surface, the deny-branch `summary=<...>` should say something like `summary=approval denied; later retry requires fresh prepare` so the operator sees the current denial outcome and next retry requirement without implying cancellation or permanent impossibility
- On the shell operator surface, the deny-branch `summary=<...>` should stay short rather than restating the deeper rule that the unchanged transfer remains denied until a fresh cycle replaces it, because the token line already says `result=denied` and the fuller identity semantics already live elsewhere in the glossary
- On the shell operator surface, successful `/approvals resolve ... approve ...` output should stay a compact receipt and should not echo the free-form approval reason by default; reason inspection belongs to approval detail, run evidence, or other durable audit surfaces rather than the immediate success line
- On the shell operator surface, successful `/approvals resolve ... approve ...` output should explicitly print `approval=<id> status=resolved result=approved` plus `run=<submit-run-id>` so the operator gets the shared resolved-state token, the approved outcome, and the returned submit continuation run
- On the shell operator surface, finance approval resolution should keep the domain-specific denial word `result=denied` rather than switching to a generic `result=rejected`, because the governing finance glossary already uses `denied` for approval outcomes
- On the shell operator surface, successful `/approvals resolve ... deny ...` output should stay a compact receipt and should not echo the free-form denial reason by default; reason inspection belongs to approval detail, run evidence, or other durable audit surfaces rather than the immediate success line
- On the shell operator surface, successful `/approvals resolve ... deny ...` output should omit `run=` and instead print an explicit denial outcome such as `approval=<id> status=resolved result=denied`, because denial does not create a submit continuation run
- A **Transfer Status View** should not introduce a special `preparing` status in v1; while prepare is actively running and no approval exists yet, operators should read the governing **Work Item** `running` state and latest **Run Attempt** evidence until the transfer reaches `pending_approval`, `reprepare_required`, or a terminal outcome
- A **Transfer Status View** should not introduce a special `failed` status in v1; ordinary system or runtime failure should remain visible through the governing **Work Item** `failed` state and latest **Run Attempt** evidence unless the situation is already captured by a narrower transfer-specific status such as `submission_unconfirmed` or `reprepare_required`
- A **Transfer Status View** should not introduce a special `expired` status in v1; when the current **Approval Request** for an unchanged **Transfer Intent** expires or is superseded, operators should read the approval itself as `expired` and the transfer consequence as `reprepare_required`
- A **Transfer Status View** should not introduce a special `approved` status in v1; once the operator approves, the current plan immediately hands off into submit continuation, so operators should read the **Approval Request** as `approved` and then read the transfer outcome as `submitted`, `submission_unconfirmed`, `reprepare_required`, or ordinary workflow failure
- A **Transfer Status View** should not introduce a special `completed` status in v1; for this workflow, `submitted` is the terminal transfer outcome because Odin currently proves Robinhood request acceptance, not downstream settlement or reconciliation completion
- On **Transfer Status View**, `submitted` should mean Robinhood acceptance is confirmed, not merely that Odin attempted the final submit click
- On **Transfer Status View**, `submission_unconfirmed` should mean Odin attempted submit or reached a post-submit uncertainty path, but Robinhood acceptance could not be confirmed from available evidence
- On **Transfer Status View**, `denied` should mean the latest approval cycle for the current unchanged **Transfer Intent** ended in operator denial and no fresh prepare/approval cycle has replaced it yet
- On **Transfer Status View**, `canceled` should mean the current **Transfer Intent** itself was explicitly canceled by the operator and remains terminated until a brand-new **Transfer Intent** is created
- On **Transfer Status View**, `pending_approval` should mean the current **Transfer Intent** is prepared with fresh review evidence and a currently pending **Approval Request** for final submit
- On **Transfer Status View**, `reprepare_required` should mean the current **Transfer Intent** is unchanged but the latest review/evidence cycle is stale or otherwise unusable, so a fresh prepare is required before approval or submit can continue
- V1 does not require a first-class transfer fingerprint or idempotency key as domain language; transfer identity is inferred from governing **Scope**, core **Transfer Intent** facts, and the approval/evidence chain until duplicate-execution pressure proves otherwise
- A material change to **Transfer Intent** facts such as amount, **Transfer Direction**, **Funding Account**, or **Robinhood Account** creates a new **Transfer Intent** even if Odin reuses the same governing **Work Item** for operator continuity
- A memo or other non-monetary annotation may change on a **Transfer Intent** without creating a new **Transfer Intent** unless the annotation itself changes the real-world money-movement meaning
- A new **Transfer Intent** must produce a fresh review-state evidence bundle, fresh `approval_wait` wake packet, and fresh **Approval Request**; prior review evidence remains historical only and cannot authorize the new intent
- Stale browser session state or stale prepared review context does not by itself create a new **Transfer Intent** when the money-movement facts are unchanged; it invalidates the old execution context and requires a fresh prepare/evidence/approval cycle for that same intent
- Requested execution timing belongs to approval and execution context in v1, not to **Transfer Intent** identity, unless Odin later promotes timing into an explicit money-movement fact
- Browser-driver states such as `review_ready`, `session_expired`, and `resume_verification_failed` belong to **Run Attempt** execution evidence and must not expand **Transfer Intent**, **Work Item**, or **Approval Request** status vocabularies; a transfer **Run Attempt** should expose exactly one primary canonical driver-evidence state rather than multiple simultaneous canonical states, with any extra nuance carried in summary or auxiliary evidence; `session_expired` should mean Odin cannot regain or keep a usable authenticated live session, while `resume_verification_failed` should mean the session exists but Odin cannot prove it returned to the approved expected review state; when both conditions could arguably apply, `session_expired` should take precedence as the primary canonical driver-evidence state only when unusable live-session loss is the earlier terminal blocker, but once a usable authenticated session is reestablished and the run still ends because approved review continuity cannot be re-proven, `resume_verification_failed` should become the primary canonical driver-evidence state instead, while the earlier recovered `session_expired` event should still be preserved as minimal structured secondary evidence rather than dropped, left only in summary prose, or expanded into a general ordered state-history array; that minimal structured secondary evidence should be no richer than a single prior canonical driver-state token, and in transport or artifact payloads that token should use the aligned field name `prior_session_state`, placed beside `session_state` inside `artifacts`; when no such recovered prior state exists, `prior_session_state` should be omitted entirely rather than returned as `null`; in v1, `prior_session_state` should stay restricted to the recovered-`session_expired` case rather than becoming a general prior-driver-state escape hatch, should appear only when the terminal primary canonical driver-evidence state is `resume_verification_failed`, should refer only to an earlier recovered state within the same **Run Attempt**, not a prior run, should be closed to exactly the value `session_expired`, and should stay local to **Run Attempt** transport or artifact evidence rather than being copied onto broader read models such as **Transfer Status View** or into durable transcript or memory summaries as a second structured field, though transcript or memory may still mention the recovered session-loss story in natural-language summary when useful
- Each pending **Approval Request** should explicitly reference the active `approval_wait` wake packet that contains its resume context rather than relying on latest-packet inference
- If material-change re-triage alters the decision basis for a reused **Work Item** that already has a pending **Approval Request**, Odin should mark the old approval `expired` and create a fresh **Approval Request** rather than mutating the pending approval in place
- A replacement **Approval Request** should explicitly reference the expired **Approval Request** it supersedes rather than relying on timestamps or shared work-item history alone
- If material-change re-triage expires a pending **Approval Request**, Odin should supersede the old `approval_wait` wake packet and write a fresh `approval_wait` wake packet for the new approval context rather than mutating the existing packet
- Approving an **Approval Request** should authorize resume from the `approval_wait` wake packet explicitly linked to that approved request, not whichever wake packet happens to be latest active on the **Work Item**
- If principal-attended login, MFA, or similar auth friction appears during post-approval submit continuation, Odin may continue under the same approved **Approval Request** only while it can prove it returned to the same expected review state linked to that approval; otherwise it must fail safe and require a fresh prepare/evidence/approval cycle
- If post-approval auth friction or other submit-continuation failure makes the approved review state unusable, the historical **Approval Request** should remain `approved`; the transfer-facing consequence is `reprepare_required`, and any later retry must proceed through a fresh prepare/evidence/approval cycle rather than mutating the old approval outcome
- If an approved-but-unusable transfer later retries, the new **Approval Request** should explicitly reference the prior approved request it follows rather than relying on timestamps or shared **Work Item** history alone
- If post-approval auth friction or other submit-continuation failure makes the approved review state unusable, the wake packet linked to that approved resume context should be marked `sealed` immediately with a canonical stale reason, becoming terminal history and no longer remaining an active resume candidate
- After post-approval auth friction or other submit-continuation failure seals the old approved resume packet as stale, Odin should not create any new active wake packet until a fresh prepare produces a brand-new `approval_wait` packet for the next approval cycle
- When post-approval auth friction or other submit-continuation failure makes the approved review state unusable, the governing **Work Item** should move to `blocked` rather than `failed`, because the unchanged **Transfer Intent** can still proceed later through a fresh prepare/evidence/approval cycle
- When post-approval auth friction or other submit-continuation failure makes the approved review state unusable, the submit-continuation **Run Attempt** should be recorded as `failed` rather than `interrupted`, because Odin reached a definite non-success execution outcome rather than a restart-recovery interruption
- When a failed post-approval submit-continuation **Run Attempt** cannot prove it returned to the approved expected review state, Odin should record `resume_verification_failed` as the canonical **Run Attempt** execution-evidence term rather than reusing the queue-level `stale_context` token
- If post-approval submit continuation instead ends with `session_expired` because Odin cannot regain or keep a usable authenticated live session, Odin should still take the same downstream finance path as other approved-but-unusable continuity failures: the historical **Approval Request** remains `approved`, the governing **Work Item** becomes `blocked` with `blocked_reason=stale_context`, the old approved resume packet remains sealed as stale terminal history, no replacement active wake packet is created, and the transfer consequence remains `reprepare_required`
- Denying an **Approval Request** should move the **Work Item** to `blocked` with an explicit denial reason and require later re-triage or replanning rather than automatically treating the obligation as failed or canceled
- Operator denial should record both a structured `denial_reason_key` and optional free-text `denial_reason_note` on the **Approval Request** rather than relying on free-text denial alone
- `denial_reason_key` should use a dedicated operator-denial namespace rather than sharing the same vocabulary as system keys like `approval_required`, `policy_denied`, or `transition_denied`
- When a **Work Item** is blocked because of operator denial, its `blocked_reason` should preserve that operator-denial namespace explicitly rather than collapsing into a generic blocked label
- When a **Work Item** is blocked because of operator denial, its canonical queue-level `blocked_reason` should be `operator_denied`, distinct from `approval_required`, while the linked **Approval Request** carries the detailed `denial_reason_key` and `denial_reason_note`
- When post-approval auth friction or other submit-continuation failure makes an approved review state unusable, the governing **Work Item** should use the third coarse queue-level `blocked_reason` `stale_context`, distinct from both `approval_required` and `operator_denied`, rather than reusing either one
- The linked `approval_wait` wake packet should reuse the same canonical machine-readable `blocking_reason` tokens as **Work Item** `blocked_reason`, including `approval_required`, `operator_denied`, and `stale_context`, rather than introducing packet-specific synonyms; human explanation belongs in packet summary or evidence
- All wake packets that carry blocked-state control data should reuse the same canonical machine-readable `blocking_reason` vocabulary as **Work Item** `blocked_reason`, not just `approval_wait` packets
- Wake packets whose task status is `queued` should not carry `blocking_reason`; queued wake context should explain itself through trigger, summary, constraints, and queue timing instead of a blocked-state field
- Queued wake packets should not add a separate machine-readable `queue_reason`; queued explanation should continue to rely on trigger, summary, constraints, and `next_eligible_at`
- Wake-packet `trigger` should remain the packet-creation cause such as `restart`, `approval_wait`, or `handoff`, and should not be overloaded with queue-outcome reasons like `executor_unavailable` or `lease_conflict`
- When queued wake packets need structured "why requeued" detail beyond `trigger` and `next_eligible_at`, Odin should keep that detail in `Evidence.Kind` plus summary or ref rather than adding another top-level field
- Wake-packet `Evidence.Kind` should use a constrained canonical namespaced vocabulary for control-plane packet evidence, such as `runtime.restart`, `runtime.fault`, `tool.snapshot`, and `delegation.created`, rather than bare or free-form text
- V1 `EvidenceKind` values should use exactly a two-segment `<family>.<action>` namespace such as `runtime.restart`, `runtime.fault`, `tool.snapshot`, and `delegation.created`, rather than deeper hierarchies
- The v1 `EvidenceKind` family segment should be a closed documented set, initially just `runtime`, `tool`, and `delegation`
- The v1 `EvidenceKind` action segment should also be a closed family-local set rather than allowing arbitrary new actions within an approved family
- `docs/contracts/context-compaction.md` should document the `EvidenceKind` registry grouped by family rather than as one flat list of full kind strings
- Odin should define a closed documented registry for canonical wake-packet `Evidence.Kind` values rather than allowing arbitrary namespaced strings
- The v1 `Evidence.Kind` registry should start with a small explicit set grounded in live producers, such as `runtime.restart`, `runtime.fault`, `tool.snapshot`, and `delegation.created`, and expand only when new producers actually exist
- The canonical v1 mapping for current live wake-packet evidence producers should be: `restart -> runtime.restart`, `fault -> runtime.fault`, `tool -> tool.snapshot`, and `delegation -> delegation.created`
- The canonical wake-packet `Evidence.Kind` registry should live in `docs/contracts/context-compaction.md` as authored source of truth, with code and tests mirroring that contract rather than replacing it
- Checkpoint compaction should reject unknown wake-packet `Evidence.Kind` values at write time rather than silently persisting them
- Odin should mirror the canonical `Evidence.Kind` registry in Go through named constants, with tests checking that code stays aligned with `docs/contracts/context-compaction.md`, rather than parsing Markdown at runtime
- Odin should represent wake-packet `Evidence.Kind` through a dedicated Go type such as `EvidenceKind` rather than a raw `string` field
- New wake-packet writes should use only canonical namespaced `EvidenceKind` values and should reject legacy bare aliases such as `restart`, `fault`, `tool`, and `delegation`; compatibility belongs in read paths or one-time migration rather than new durable writes
- Historical wake packets that still store bare evidence kinds such as `restart`, `fault`, `tool`, and `delegation` should normalize those values on read into their canonical namespaced `EvidenceKind` forms for operator surfaces and internal read models, while preserving the raw stored payload until a deliberate backfill exists
- Historical `EvidenceKind` alias normalization should live in one shared wake-packet decode path rather than letting each projection or caller normalize bare kinds independently
- Odin should expose an explicit shared wake-packet decode helper in `internal/runtime/checkpoints` and require projections and other consumers to use it rather than reading wake `payload_json` with raw `json.Unmarshal`
- Odin should start with a wake-packet-specific decode helper such as `DecodeTaskWakePacket` and keep `ProjectContext` and `RunContext` on the simple generic unmarshal path until they actually need richer decode semantics
- `DecodeTaskWakePacket` should accept a full `sqlite.ContextPacket` and validate `PacketScope == task_wake_packet` before decoding rather than accepting only raw payload JSON
- `DecodeTaskWakePacket` should also reject packets whose `PacketKind` is not `wake`, rather than treating `PacketScope == task_wake_packet` as sufficient by itself
- `DecodeTaskWakePacket` should return a typed sentinel error such as `ErrInvalidTaskWakePacketEnvelope`, with wrapped detail for scope mismatch, kind mismatch, or malformed payload, so callers can use `errors.Is` rather than string matching
- Projections and other read models should fail fast on `ErrInvalidTaskWakePacketEnvelope` rather than suppressing or skipping invalid wake packets to keep operator surfaces rendering
- `ErrInvalidTaskWakePacketEnvelope` should also open or reuse a runtime **Incident** tied to the affected task so operator projections show explicit human attention required, rather than leaving the failure only as a transient caller error
- `ErrInvalidTaskWakePacketEnvelope` should be converted into an observation that flows through a deterministic diagnosis path, so one shared incident/recovery owner decides whether to open or reuse the task-linked incident rather than each failing wake reader creating incidents directly
- Invalid wake-envelope corruption should get its own explicit recovery `FaultKey`, such as `wake_packet_invalid`, rather than being folded into unrelated fault families like `projection_stale` or `run_failure_repeated`
- `wake_packet_invalid` should use a stable task-scoped `SubjectKey`, such as `task:<task-id>:wake-packet`, rather than a packet-id-scoped subject key; packet ID remains diagnostic detail, not incident identity
- `wake_packet_invalid` observations should explicitly use severity `error` rather than falling back to the recovery default `warning`
- `wake_packet_invalid` should intentionally have no automatic recovery playbook at first; diagnosis should open or reuse the incident and stop at human attention required rather than attempting speculative repair of authoritative wake state
- Recovery `Decision` should gain an explicit outcome mode such as `ignore`, `incident_only`, `playbook`, and `escalate` rather than inferring behavior from whether `Playbook` is empty
- `wake_packet_invalid` should use the explicit diagnosis outcome `incident_only` rather than inventing a no-op playbook or overloading `escalate`
- The `incident_only` diagnosis outcome should open or reuse the incident and then stop without creating a **Recovery** row; `recoveries` remain deterministic recovery attempts, not generic incident annotations
- Recovery executor `Outcome` should explicitly allow "no recovery attempt" for `incident_only`, rather than returning a zero-value **Recovery** record
- `incident_only` should return its own explicit recovery executor `Outcome.Status`, rather than overloading `completed`; `completed` remains reserved for a successful deterministic recovery attempt
- Recovery service logging and audit-style fields should use generic decision language with optional `playbook` detail only for `playbook` outcomes, rather than always saying `self-heal playbook completed` and always emitting a `playbook` field
- Recovery `Decision` should carry an explicit typed mode such as `ignore`, `incident_only`, `playbook`, or `escalate`, with `Playbook` optional and valid only when the mode is `playbook`
- Diagnosis should emit explicit `Decision{mode: ignore}` records for ignored observations rather than dropping them from the decision stream entirely
- `ignore` decisions should stop at diagnosis and be logged there rather than flowing through the executor just to produce an `ignored` outcome
- `CycleResult.Outcomes` should remain executor-only and therefore omit `ignore` decisions; `ignore` stays visible through `Decisions` plus diagnosis logging rather than as an outcome row
- `incident_only` should still appear in `CycleResult.Outcomes` because the executor did open or reuse an incident even though no **Recovery** row exists
- Recovery executor `Outcome.Attempt` should remain a recovery-attempt count and therefore stay unset for `incident_only` rather than using `0` or inventing a non-recovery attempt number
- Recovery executor `Outcome` should drop redundant booleans such as `Suppressed` and `Escalated` and rely on explicit `Status` values alone
- Recovery executor `Outcome.Status` should use its own typed canonical vocabulary such as `incident_only`, `completed`, `failed`, `suppressed`, and `escalated` rather than staying a raw string
- Playbook `ActionResult.Status` should use its own small typed vocabulary, separate from executor `Outcome.Status`, with the executor responsible for mapping action results into executor outcomes
- The v1 playbook `ActionResult.Status` set should be closed to exactly `completed`, `failed`, and `escalated`; executor-only statuses such as `suppressed` and `incident_only` should be forbidden at the playbook action layer
- The recovery executor should reject unknown `ActionResult.Status` values as invalid playbook output rather than coercing them into the generic failure branch
- Invalid playbook action-result statuses should surface through a typed sentinel such as `ErrInvalidActionResultStatus`, so callers can distinguish playbook contract violations from ordinary execution failure
- If invalid `ActionResult.Status` is detected after a recovery attempt has started, Odin should seal that recovery attempt as `failed`, return `ErrInvalidActionResultStatus`, and mark the linked incident `escalated` with explicit contract-violation details rather than leaving the incident merely `open`
- If invalid `ActionResult.Status` is detected after a bounded recovery action has run, Odin should still append `recovery.action_executed` with canonical result `failed` plus explicit contract-violation details rather than persisting the invalid raw status or skipping the event entirely
- If invalid `ActionResult.Status` is detected, Odin should preserve the invalid raw status string only in non-canonical diagnostic details for forensics, while canonical status and event result fields remain normalized to `failed`
- If invalid `ActionResult.Status` is detected, Odin should preserve the raw invalid status in **Recovery** diagnostic details only, while **Incident** details stay summary-level and carry contract-violation category or reason without duplicating the raw invalid status string
- `recovery.action_executed` should gain an optional structured diagnostic-details field so contract-violation metadata can be carried canonically in the event without overloading free-text `Description`
- The new `recovery.action_executed` diagnostic-details field should be a small typed optional event sub-structure rather than a generic `details_json` blob, keeping runtime events aligned with Odin’s typed event-payload style
- `recovery.action_executed.contract_violation` should include a canonical machine-readable violation key, such as `invalid_action_result_status`, plus optional raw offending value such as `raw_status`, rather than carrying only raw malformed output and prose description
- `recovery.action_executed.contract_violation.key` should be a closed typed registry, with v1 starting at exactly `invalid_action_result_status`, rather than an open string convention
- The v1 `contract_violation.key` value should stay the bare snake_case token `invalid_action_result_status`, rather than a namespaced nested key such as `recovery.invalid_action_result_status`
- The v1 `contract_violation` payload should use the explicit field name `raw_status` rather than a generic field such as `raw_value`, because the only live violation key is `invalid_action_result_status` and the malformed source field is `ActionResult.Status`
- V1 `contract_violation` should omit a separate `source_field`; the closed key `invalid_action_result_status` already implies the malformed source is `ActionResult.Status`, so adding `source_field` would duplicate meaning without resolving a real ambiguity
- V1 `contract_violation` should omit an `allowed_statuses` or `expected_statuses` field and instead rely on the already-closed `ActionResult.Status` registry, so the valid set remains canonical in one place rather than being duplicated into each event
- `contract_violation.raw_status` should be required whenever `contract_violation.key == invalid_action_result_status`; the executor already guarantees a concrete `ActionResult.Status` before `recovery.action_executed` is recorded, so making `raw_status` optional would weaken the forensic contract without current benefit
- Denying an **Approval Request** should mark the linked `approval_wait` wake packet `sealed` with a denied or stale reason rather than leaving it `active`
- Denial for timing, comfort, or operator-readiness reasons does not by itself destroy an unchanged **Transfer Intent**; it closes the current approval cycle and requires a fresh prepare/evidence/approval cycle before that same intent can proceed later
- Explicit operator cancellation ends the current **Transfer Intent** itself, not just the approval cycle; any later attempt to perform that money movement starts a new **Transfer Intent**
- If a denied **Work Item** is later re-triaged or replanned and still needs approval, Odin should create a brand-new **Approval Request** and a brand-new `approval_wait` wake packet, leaving the denied approval and sealed packet as terminal history
- A brand-new **Approval Request** created after denial should explicitly reference the denied **Approval Request** it follows so the post-denial governance chain remains queryable
- Resolving an approved **Approval Request** should move the **Work Item** back to `queued` and lead to a fresh **Run Attempt** from persisted wake or resume state rather than continuing the same live session
- A **Managed Project** inherits Git-aware governance and task-owned worktree rules
- A **Managed Domain** supports durable operational work without inheriting Git repository requirements
- Operator-facing navigation should center `Workspace -> Initiative -> Work Item`, with **Run Attempts** nested inside **Work Item** detail instead of acting as the top-level surface
- Default **Work Item** detail should be business-first, showing durable work context before execution telemetry; project and run metadata appear only when relevant to the initiative kind and current execution path

## Status ownership

- V1 **Intake Item** intake status should be one of: `received`, `processing`, `review_required`, `needs_clarification`, `duplicate_linked_or_suppressed`, `approval_required`, `accepted`, `rejected`, `approval_denied`, `archived`, `errored`
- `triaging`, `resolved`, and `suppressed` are compatibility or derived language for V1 intake readbacks; stored intake rows should use the explicit review-oriented statuses above so operator queues can distinguish review, clarification, duplicate, approval, acceptance, denial, and archive outcomes without reinterpreting routing notes
- **Intake Item** should carry outcome references rather than a large branching status enum, including optional links to a **Conversation Transcript**, linked **Work Items**, canonical duplicate **Intake Item** reference, suppression or dedupe reason, routing notes, its explicit **Dedupe Key**, persisted **Source Facts**, and the **Dedupe Recipe Version**
- **Work Item** operator status should be one of: `queued`, `running`, `blocked`, `completed`, `failed`, `canceled`
- Work Item pause/resume is owned by Odin runtime state, not GitHub labels; an operator-paused Work Item should use `status=blocked` with `blocked_reason=operator_paused`, and `odin:paused` should remain a projection label only
- **Run Attempt** execution status should be one of: `running`, `completed`, `failed`, `cancelled`, `interrupted`
- **Approval Request** governance status should be one of: `pending`, `approved`, `denied`, `expired`
- **Follow-Up Obligation** trigger status should be one of: `active`, `paused`, `blocked`, `completed`, `skipped`, `archived`; due or overdue state is a derived schedule view, not the trigger's stored lifecycle status
- **Transfer Status View** may expose a derived summary status for operator readability, but it does not own canonical lifecycle truth
- `initialized` and `ready` should remain system-readiness or dispatch-readiness terms, not the primary operator-visible lifecycle for governed work
- `healthy`, `degraded`, and `failed` belong to health and **Observability** reporting, while `ready` and `not ready` belong to **Runtime Readiness**; `/overview` may summarize those cues but must not reuse them as **Work Item**, **Run Attempt**, **Approval Request**, or **Follow-Up Obligation** lifecycle states

## Example dialogue

> **Dev:** "Should family finance be another project in Odin?"
> **Domain expert:** "No. It belongs in the **Workspace** as a **Managed Domain** initiative. Software delivery stays in a **Managed Project** initiative."
>
> **Dev:** "If the shell still says `/project family-ops`, does that make Family-Ops a managed project?"
> **Domain expert:** "No. That is legacy operator wording for initiative selection. The canonical domain meaning remains **Initiative**, and Family-Ops finance stays a **Managed Domain** until the CLI becomes initiative-aware."
>
> **Dev:** "Do we need a `transfer_intents` table before Odin can govern Robinhood transfers?"
> **Domain expert:** "No. The domain concept is a **Transfer Intent**, but in v1 Odin should represent it through the governing **Work Item**, **Run Attempt**, **Approval Request**, and linked wake-packet history."
>
> **Dev:** "What does `direction=deposit` mean in this workflow?"
> **Domain expert:** "It is a **Transfer Direction** from Robinhood's point of view. `deposit` moves money into Robinhood, and `withdraw` moves money out."
>
> **Dev:** "Are `source_account` and `destination_account` the real finance terms?"
> **Domain expert:** "No. They are transport fields. The domain model should talk about a **Funding Account** and a **Robinhood Account**."
>
> **Dev:** "If I deny a prepared transfer because the amount is wrong and retry with corrected numbers, is that the same intent?"
> **Domain expert:** "No. A material change to amount, direction, funding leg, or Robinhood leg creates a new **Transfer Intent**, even if the same **Work Item** stays open for continuity."
>
> **Dev:** "What if I only fix the memo?"
> **Domain expert:** "That stays the same **Transfer Intent**. Memo is annotation unless it changes the actual business meaning of the money movement."
>
> **Dev:** "If I correct the amount after denial, can Odin reuse the old review screenshot and approval context?"
> **Domain expert:** "No. That is a new **Transfer Intent**, so Odin must require a fresh prepare, fresh review evidence, fresh wake packet, and fresh approval decision."
>
> **Dev:** "What if I deny for timing reasons but later want the exact same transfer?"
> **Domain expert:** "That stays the same **Transfer Intent**, but the denial closes the current approval cycle. Odin must prepare it again and collect fresh evidence and fresh approval before submit."
>
> **Dev:** "What if the Robinhood session goes stale but I still want the exact same transfer?"
> **Domain expert:** "That is still the same **Transfer Intent**. The stale session only invalidates the old execution context, so Odin must prepare and prove it again before approval or submit."
>
> **Dev:** "What if I decide I do not want this transfer anymore?"
> **Domain expert:** "That cancels the **Transfer Intent** itself. If you want to do it later, Odin should treat that as a new **Transfer Intent**, not a continuation of the old one."
>
> **Dev:** "If I want the same transfer tomorrow instead of right now, is that a new intent?"
> **Domain expert:** "Not in v1. Requested timing is approval and execution context, not **Transfer Intent** identity, unless timing later becomes an explicit governed money-movement fact."
>
> **Dev:** "Should `review_ready` become the transfer status?"
> **Domain expert:** "No. That is browser execution evidence on a **Run Attempt**. Operator-facing lifecycle stays on **Work Item** and **Approval Request** statuses."
>
> **Dev:** "What should we call the packet-backed transfer read model operators inspect?"
> **Domain expert:** "Use **Transfer Status View**. It is a derived projection over workflow state, not a second source of truth for the transfer."
>
> **Dev:** "Can that transfer view show `status=pending_approval`?"
> **Domain expert:** "Yes, as a derived shorthand on **Transfer Status View** only. The underlying truth still belongs to the linked **Work Item**, **Approval Request**, **Run Attempt**, and wake-packet state."
>
> **Dev:** "If Odin clicks submit but cannot prove Robinhood accepted it, can the transfer view still say `submitted`?"
> **Domain expert:** "No. `submitted` on **Transfer Status View** requires confirmed acceptance. A click attempt without acceptance proof belongs in **Run Attempt** evidence or a failure/review path."
>
> **Dev:** "So what should the transfer view say when submit was attempted but acceptance is uncertain?"
> **Domain expert:** "Use `submission_unconfirmed` on **Transfer Status View**. It tells the operator the final submit path ran, but acceptance is not proven."
>
> **Dev:** "What should the transfer view show after I deny approval for the same unchanged transfer?"
> **Domain expert:** "Use `denied` on **Transfer Status View** until a fresh prepare and approval cycle starts. That reports the latest approval outcome without pretending the transfer intent was canceled."
>
> **Dev:** "What should the transfer view show after I explicitly cancel the transfer?"
> **Domain expert:** "Use `canceled` on **Transfer Status View**. Cancellation ends that **Transfer Intent** itself, so the view should show it as terminated until a new intent is created."
>
> **Dev:** "What should the transfer view show once prepare reached the review screen and approval is still pending?"
> **Domain expert:** "Use `pending_approval` on **Transfer Status View**. It tells the operator the current intent is freshly prepared and waiting on an active approval decision."
>
> **Dev:** "What should the transfer view show when the transfer facts are unchanged, but the old review state went stale and I need to prepare again?"
> **Domain expert:** "Use `reprepare_required` on **Transfer Status View**. It tells the operator the same transfer intent still exists, but the previous prepare/evidence cycle can no longer be used."
>
> **Dev:** "Should the transfer view also show `preparing` while prepare is still running?"
> **Domain expert:** "No. In v1, keep that on the governing **Work Item** `running` state and latest **Run Attempt** evidence. **Transfer Status View** should stay focused on the operator decision checkpoints, not mirror generic execution progress."
>
> **Dev:** "Should the transfer view also show `failed` for ordinary runtime failure?"
> **Domain expert:** "No. In v1, keep ordinary failure on the governing **Work Item** `failed` state and latest **Run Attempt** evidence. **Transfer Status View** should only add narrower terms when they sharpen transfer meaning beyond generic workflow failure."
>
> **Dev:** "If the approval on the same unchanged transfer expires, should the transfer view show `expired` too?"
> **Domain expert:** "No. Let the **Approval Request** itself be `expired`, and let **Transfer Status View** show `reprepare_required` because the operator now needs a fresh prepare/evidence/approval cycle."
>
> **Dev:** "Should the transfer view show `approved` after I approve and before submit finishes?"
> **Domain expert:** "No. In v1, approval resolution immediately hands into submit continuation. Let the **Approval Request** be `approved`, and let **Transfer Status View** report the resulting transfer outcome instead of pausing at an intermediate `approved` state."
>
> **Dev:** "Should the transfer view show `completed` after Robinhood accepted the transfer?"
> **Domain expert:** "No. In v1, `submitted` is the terminal transfer outcome. `completed` would imply downstream settlement or reconciliation ownership that this workflow does not currently prove."
>
> **Dev:** "If the transfer work item exists but no prepare run has happened yet, should the transfer view say `not_started`?"
> **Domain expert:** "No. Before the first prepare run creates transfer-specific evidence, omit any derived transfer status and let operators read the governing **Work Item** state directly."
>
> **Dev:** "Before the first prepare run, should `GET /api/transfers/tasks/{taskKey}` 404 or return a placeholder transfer status?"
> **Domain expert:** "No. Keep one canonical read surface. Return the same **Transfer Status View**, but omit transfer-specific fields like derived `status` until transfer evidence actually exists."
>
> **Dev:** "Before the first prepare run, should the transfer view still return `run_id` or `approval_id` as `null` or `0`?"
> **Domain expert:** "No. Those are transfer-evidence identifiers. Omit them until a transfer-relevant run or approval actually exists."
>
> **Dev:** "Before the first prepare run, should the transfer view still return transfer `summary` or `artifacts` with generic text or empty objects?"
> **Domain expert:** "No. Those fields describe transfer evidence. Omit them until the first transfer-relevant prepare or follow-on cycle actually creates that evidence."
>
> **Dev:** "Does `task_key` in the transfer view mean there is a separate transfer identifier?"
> **Domain expert:** "No. In v1, `task_key` is just the transport name for the governing **Work Item** key. Do not treat it as a second transfer-specific identifier."
>
> **Dev:** "Should we also add `work_item_key` beside `task_key` for clarity?"
> **Domain expert:** "No. In v1, `task_key` is enough. Adding `work_item_key` would duplicate the same identity without adding new meaning."
>
> **Dev:** "Should the new transfer-prepare HTTP payload keep `project_key` as its canonical field?"
> **Domain expert:** "No. The canonical field should be `initiative_key`, because Family-Ops finance is an **Initiative**. Treat `project_key` as legacy compatibility only if the implementation still needs it during transition."
>
> **Dev:** "During transition, may the prepare API still accept `project_key` at all?"
> **Domain expert:** "Yes, as a deprecated compatibility alias for `initiative_key`. But `initiative_key` remains the only canonical field name in the contract."
>
> **Dev:** "What if a client sends both `initiative_key` and deprecated `project_key`?"
> **Domain expert:** "Accept the request only when they match. If they conflict, reject it. Transfer scope selection is too important for silent precedence."
>
> **Dev:** "If the request came in through deprecated `project_key`, may runtime params or durable evidence keep that project-shaped name?"
> **Domain expert:** "No. Accept the alias only at the boundary. After validation, normalize immediately to canonical `initiative_key` semantics and write downstream transfer state in initiative-shaped language only."
>
> **Dev:** "Should `/transfer prepare` in the shell also take its own `initiative_key` or `project_key` argument?"
> **Domain expert:** "No. On the shell surface, scope should come from the already selected initiative context. `/transfer prepare` should stay focused on transfer facts, not duplicate scope selection."
>
> **Dev:** "What if I run `/transfer prepare` with no initiative selected in the shell?"
> **Domain expert:** "Fail fast and tell the operator to select an initiative first. Do not prompt interactively or guess from remembered scope in a finance workflow."
>
> **Dev:** "When `/transfer prepare` succeeds in the shell, should it print `approval_id=<id>` like HTTP?"
> **Domain expert:** "No. The shell should print `approval=<id>`. Keep `approval_id` for HTTP and JSON transport fields."
>
> **Dev:** "Should shell `/transfer prepare` print only `approval=<id>`?"
> **Domain expert:** "No. It should also echo `task=<key>` and `run=<id>`, so the operator gets the full immediate follow-up handle set without leaving the shell's compact naming style."
>
> **Dev:** "Should shell `/transfer prepare` stop at the handle line, or also print a `summary=` line?"
> **Domain expert:** "It should also print a concise `summary=` line after the handles. The operator should see both the follow-up handles and a short human confirmation that review is prepared and awaiting approval."
>
> **Dev:** "Should shell `/transfer prepare` also print `review_url=` and `screenshot_path=` right away?"
> **Domain expert:** "No. Keep the immediate shell success surface compact. Evidence artifacts belong to `/approvals`, `/runs show <run-id>`, and the structured transfer status view."
>
> **Dev:** "Should shell `/transfer prepare` also print explicit next-step guidance?"
> **Domain expert:** "Yes. This workflow pauses for approval by design, so the shell should print a concise `next=` hint that tells the operator what to do after the transfer is prepared."
>
> **Dev:** "Should that `next=` hint be abstract prose or exact commands?"
> **Domain expert:** "Use exact commands with the concrete handles Odin just printed. The shell already has precedent for command-oriented guidance, and finance operators should not have to translate vague advice into commands."
>
> **Dev:** "Should `next=` stop at the review commands, or also include the approval-resolution command template?"
> **Domain expert:** "Include the full governed path. This workflow intentionally pauses for operator review and then waits for explicit approval resolution, so the shell should show both the review commands and the approval-resolution template."
>
> **Dev:** "Should that `next=` hint still include `/approvals` even though Odin already printed `approval=<id>`?"
> **Domain expert:** "No. Once the shell already printed the approval handle, the sharper v1 hint is `/runs show <run-id>` for evidence review and `/approvals resolve <approval-id> approve|deny because ...` for the governed decision."
>
> **Dev:** "Inside that `next=` hint, should run review use `/runs show active`?"
> **Domain expert:** "No. Use `/runs show <run-id>`. The immediate success output already printed the concrete run handle, so the guidance should stay concrete instead of falling back to an alias."
>
> **Dev:** "Inside that `next=` hint, should approval resolution still say `<approval-id>` or should it inline the printed approval handle?"
> **Domain expert:** "Inline the concrete approval handle. The shell already printed it, so the guided command should be concrete there too while still leaving `approve|deny` as the operator's choice."
>
> **Dev:** "Should that approval-resolution hint stay one compact template, or split into separate approve and deny commands?"
> **Domain expert:** "Keep one compact template. The real operator choice is `approve|deny`, not a different command family, and the immediate shell hint should stay concise."
>
> **Dev:** "Should the `next=` hint stop at `/approvals resolve ...`, or also include the final run check after resolution?"
> **Domain expert:** "Include the final run check too. The governed shell path does not end at issuing the approval decision; it ends when the operator verifies the resumed run outcome."
>
> **Dev:** "Should that final run check point back to the prepare run?"
> **Domain expert:** "No. Approval resolution creates a fresh submit continuation run, so the final verification step must follow the submit run returned by `/approvals resolve ...`, not the original prepare run."
>
> **Dev:** "Should `/approvals resolve ...` itself print that submit run?"
> **Domain expert:** "Yes. It should print `run=<submit-run-id>` on the shell surface, alongside the approval handle, so the operator can immediately use the final verification step."
>
> **Dev:** "On the approve path, should it also print an explicit approved outcome token?"
> **Domain expert:** "Yes. Make it symmetrical with deny: `approval=<id> result=approved run=<submit-run-id>`. The approve path still adds the submit run, but both branches should share the same outcome field."
>
> **Dev:** "Should shell `/approvals resolve ...` also print `status=resolved` alongside `result=approved|denied`?"
> **Domain expert:** "Yes. Reuse the shell's broader resolve idiom. Finance approval resolution should print `status=resolved` on both branches, while `result=` carries `approved` or `denied` and only approve adds `run=<submit-run-id>`."
>
> **Dev:** "Should shell `/approvals resolve ...` also print a `summary=` line?"
> **Domain expert:** "Yes. Add one concise `summary=<...>` line after the token receipt. That keeps the operator surface readable without turning approval resolution into a transcript or evidence dump."
>
> **Dev:** "Should `/approvals resolve ...` also grow its own `next=` hint?"
> **Domain expert:** "No. Leave it receipt-only. `/transfer prepare` already owns the longer guided path through approval and verification, and resolve receipts already expose the concrete `run=` handle when approve starts submit continuation."
>
> **Dev:** "Should that `summary=` line stay natural-language, or can it just mirror raw status names?"
> **Domain expert:** "Keep it natural-language. The token line already carries canonical status words like `status=` and `result=`. The summary should translate the operator meaning, not repeat raw status jargon such as `reprepare_required` or `submission_unconfirmed`."
>
> **Dev:** "What should the approve `summary=` line say?"
> **Domain expert:** "Use wording like `summary=approval granted; submit continuation started`, not `summary=transfer submitted`. Approval resolution starts submit continuation and returns the submit run, but confirmed submission belongs to later transfer evidence."
>
> **Dev:** "Should the approve summary also mention the follow-up verification step with `/runs show <submit-run-id>`?"
> **Domain expert:** "No. Keep the summary short. The receipt already prints `run=<submit-run-id>`, so the next action is discoverable from the handle itself. The summary should explain what happened, not absorb procedural guidance."
>
> **Dev:** "What should the deny `summary=` line say?"
> **Domain expert:** "Use wording like `summary=approval denied; later retry requires fresh prepare`. That reports the denial outcome and the real retry consequence without pretending the transfer was canceled or impossible forever."
>
> **Dev:** "Should the deny summary also restate that the unchanged transfer remains denied until a fresh cycle replaces it?"
> **Domain expert:** "No. Keep the summary short. The receipt already says `result=denied`, and the deeper unchanged-transfer semantics already live in the glossary. The summary only needs the immediate denial outcome and retry consequence."
>
> **Dev:** "Should shell approve output also echo the operator's approval reason?"
> **Domain expert:** "No. Keep the immediate receipt compact on approve too. The operator already supplied the reason at resolution time, and reason inspection belongs on approval detail, run evidence, or other durable audit surfaces rather than the success line."
>
> **Dev:** "Should finance deny output keep `result=denied`, or align to the broader shell's `result=rejected` wording?"
> **Domain expert:** "Keep `result=denied`. Finance approvals already use `denied` as the canonical outcome word, so borrowing `rejected` from a different social-memory surface would blur the glossary for no gain."
>
> **Dev:** "Should shell deny output also echo the operator's denial reason?"
> **Domain expert:** "No. Keep the immediate receipt compact as `approval=<id> status=resolved result=denied`. The operator already supplied the reason at resolution time, and reason inspection belongs on approval detail, run evidence, or other durable audit surfaces."
>
> **Dev:** "What should `/approvals resolve ... deny ...` print if no submit run exists?"
> **Domain expert:** "Do not print a fake `run=`. Print the resolved approval handle plus an explicit denial outcome such as `approval=<id> status=resolved result=denied`, because denial does not create a submit continuation run."
>
> **Dev:** "Should we also add `/api/transfers/work-items/{workItemKey}` beside `/api/transfers/tasks/{taskKey}`?"
> **Domain expert:** "No. In v1, keep `/api/transfers/tasks/{taskKey}` as the only transfer-status route. A second path would duplicate the same governing **Work Item** identity without adding new meaning."
>
> **Dev:** "When the governing work item has multiple runs, should the transfer view always use the newest run by timestamp?"
> **Domain expert:** "No. Use the latest transfer-relevant **Run Attempt** in the transfer lineage. Unrelated or non-transfer runs on the same **Work Item** must not override transfer status."
>
> **Dev:** "If the governing work item has multiple approval cycles, should the transfer view read any active or terminal approval on that work item?"
> **Domain expert:** "No. Use the latest transfer-relevant **Approval Request** chain for that transfer lineage. Unrelated approvals on the same reused **Work Item** must not override transfer status."
>
> **Dev:** "Should the transfer view just use whichever wake packet is newest on the work item?"
> **Domain expert:** "No. If the selected transfer-relevant **Approval Request** links a wake packet, use that packet. Otherwise use the latest transfer-relevant wake packet in the same transfer lineage. Do not fall back to raw newest-packet heuristics."
>
> **Dev:** "If two initiatives request the same money movement, is that one transfer intent?"
> **Domain expert:** "No. **Transfer Intent** identity is local to the governing **Scope**. The same facts in different **Initiatives** or **Workspaces** are distinct intents with separate audit trails."
>
> **Dev:** "Do we need a transfer fingerprint in v1?"
> **Domain expert:** "No. In v1, identity is already clear enough from governing **Scope**, core **Transfer Intent** facts, and the approval/evidence chain. Add a fingerprint later only if real duplicate-execution problems demand it."
>
> **Dev:** "Is the inbox just the run queue?"
> **Domain expert:** "No. The **Intake Inbox** holds raw signals first. Only triaged work moves into a scope-owned **Work Queue**."
>
> **Dev:** "Does every raw alert become a work item immediately?"
> **Domain expert:** "No. Raw intake should land as a durable **Intake Item** first. Triage can suppress it, enrich it, or link it to one or more **Work Items**."
>
> **Dev:** "Should project intake be the universal intake path?"
> **Domain expert:** "No. Use **Initiative Intake** as the generic routing layer, and keep project intake as the project-specific specialization underneath it."
>
> **Dev:** "Does a new intake item already belong to a specific initiative?"
> **Domain expert:** "No. A new **Intake Item** belongs to the **Workspace** first. **Initiative Intake** resolves the narrower **Initiative** and **Scope** when the evidence is good enough."
>
> **Dev:** "Must every triaged intake item create governed work?"
> **Domain expert:** "No. **Initiative Intake** may resolve an **Intake Item** directly when the request only needs an answer, suppression, or lightweight classification. Use a **Work Item** only for durable obligations that need follow-through."
>
> **Dev:** "If intake answers directly, where does that result live?"
> **Domain expert:** "In a durable **Conversation Transcript**, not in a fake lightweight **Work Item**."
>
> **Dev:** "Can one intake item both answer now and create follow-up work?"
> **Domain expert:** "Yes. A direct answer and a durable follow-up are separate outcomes and may both come from the same **Intake Item**."
>
> **Dev:** "If a conversation creates follow-up work, should that work remember where it came from?"
> **Domain expert:** "Yes. The follow-up **Work Item** should keep explicit backlinks to both the originating **Intake Item** and the **Conversation Transcript**."
>
> **Dev:** "Should intake just reuse work statuses?"
> **Domain expert:** "No. **Intake Item** needs its own small intake lifecycle, and the branching detail belongs in outcome references rather than a giant shared status enum."
>
> **Dev:** "Should duplicate raw signals be dropped before they hit the inbox?"
> **Domain expert:** "No. Repeated equivalent signals should still create durable **Intake Items**. Then triage can suppress them or duplicate-link them to a canonical **Intake Item** without erasing arrival history."
>
> **Dev:** "Can duplicate detection collapse similar signals across workspaces?"
> **Domain expert:** "No. Duplicate handling should stay **Workspace**-local. Similar signals in different **Workspaces** are distinct arrivals, even if their content looks equivalent."
>
> **Dev:** "Should a very old canonical intake item keep absorbing new duplicates forever?"
> **Domain expert:** "No. Canonical duplicate linking should be cooldown-bounded. After the dedupe window expires, a new equivalent arrival becomes a fresh active **Intake Item**."
>
> **Dev:** "Should duplicate handling just compare raw payload text?"
> **Domain expert:** "No. Each **Intake Item** should carry an explicit Odin-owned **Dedupe Key** derived from **Workspace**, source family, and a normalized signal fingerprint so duplicate linking stays explainable and testable."
>
> **Dev:** "Should each connector compute its own final dedupe identity?"
> **Domain expert:** "No. Adapters should provide normalized source facts, and Odin core should compute the final **Dedupe Key** so the same business signal dedupes consistently across ingress paths."
>
> **Dev:** "Once Odin computes the dedupe key, can it throw away the inputs?"
> **Domain expert:** "No. The **Intake Item** should persist its normalized **Source Facts** alongside the final **Dedupe Key** so duplicate handling can be explained and recomputed later."
>
> **Dev:** "If the dedupe logic changes later, can Odin just apply the latest recipe silently?"
> **Domain expert:** "No. Each **Intake Item** should persist the **Dedupe Recipe Version** that produced its **Dedupe Key** so recomputation and audits know which logic actually ran."
>
> **Dev:** "Should duplicate waves become their own runtime object?"
> **Domain expert:** "No. Each arrival should stay an individual **Intake Item** and duplicates should reference a canonical **Intake Item**. Grouping is a derived operator view, not a first-class runtime aggregate."
>
> **Dev:** "Should the canonical intake item store mutable duplicate counters and cooldown fields?"
> **Domain expert:** "No. Duplicate-wave data like count, latest arrival, and cooldown state should be derived operator projections, not mutable truth stored on the canonical **Intake Item**."
>
> **Dev:** "If a newer duplicate arrives with better evidence, should it become the new canonical item immediately?"
> **Domain expert:** "No. The canonical **Intake Item** should stay stable for the active dedupe window. Newer duplicates can contribute evidence and urgency, but they should not replace canonical ownership mid-window."
>
> **Dev:** "If a duplicate arrives inside the same window with materially new facts, should Odin still suppress it completely?"
> **Domain expert:** "No. Odin should keep the same canonical **Intake Item**, but materially new evidence may reopen or re-triage that canonical item so handling can change without rotating identity."
>
> **Dev:** "If retriage finds new evidence and there is already follow-up work, should Odin always create a new work item?"
> **Domain expert:** "No. If the obligation is still the same, Odin should reuse or requeue the existing **Work Item**. A new **Work Item** is only needed when the new evidence creates a genuinely distinct obligation."
>
> **Dev:** "If that reused work item already has a pending approval, should Odin just edit the approval in place?"
> **Domain expert:** "No. If the approval basis changed materially, the old **Approval Request** should become `expired` and Odin should create a fresh **Approval Request** with the new evidence and rationale."
>
> **Dev:** "Should a pending approval just assume the latest active packet belongs to it?"
> **Domain expert:** "No. Each pending **Approval Request** should explicitly reference the active `approval_wait` wake packet that contains its resume context."
>
> **Dev:** "When I approve, should Odin resume whatever wake packet is newest on the work item?"
> **Domain expert:** "No. Approval should authorize resume from the `approval_wait` wake packet explicitly linked to that approved **Approval Request**."
>
> **Dev:** "If I deny the approval, does the work just fail?"
> **Domain expert:** "No. Denial should move the **Work Item** to `blocked` with the denial reason and require later re-triage or replanning. It does not mean the underlying obligation is impossible forever."
>
> **Dev:** "Should denial just be one free-text comment?"
> **Domain expert:** "No. Operator denial should record a structured `denial_reason_key` plus an optional `denial_reason_note` on the **Approval Request**."
>
> **Dev:** "Should operator denial just reuse the same keys as policy denial?"
> **Domain expert:** "No. `denial_reason_key` should use a dedicated operator-denial namespace so human governance denial stays distinct from system policy denial."
>
> **Dev:** "Once the work item is blocked, can `blocked_reason` just say `blocked`?"
> **Domain expert:** "No. If operator denial has its own namespace, the **Work Item** `blocked_reason` should preserve it explicitly instead of flattening it into a generic blocked label."
>
> **Dev:** "Should the work item copy the full denial subtype into `blocked_reason`?"
> **Domain expert:** "No. Keep **Work Item** `blocked_reason` coarse at the queue level as `operator_denied`, and let the linked **Approval Request** carry the detailed `denial_reason_key` and `denial_reason_note`."
>
> **Dev:** "After denial, can the work item just go back to `approval_required` because approval is still conceptually involved?"
> **Domain expert:** "No. `approval_required` means waiting on a pending approval. `operator_denied` means the approval cycle ended negatively and the work is now blocked for re-triage or replanning."
>
> **Dev:** "Should the `approval_wait` wake packet keep a human string like `awaiting operator approval` in `blocking_reason`?"
> **Domain expert:** "No. `blocking_reason` should stay machine-readable and reuse the same canonical tokens as **Work Item** `blocked_reason`, such as `approval_required` and `operator_denied`. Human explanation belongs in packet summary or evidence."
>
> **Dev:** "Is that shared token rule just for `approval_wait`, or for all wake packets?"
> **Domain expert:** "For all wake packets that carry blocked-state control data. Restart, approval, and other wake paths should not invent separate prose or packet-specific synonyms for the same control meaning."
>
> **Dev:** "Can a queued restart wake packet still keep `blocking_reason` just to explain why it was requeued?"
> **Domain expert:** "No. If the wake packet says `queued`, explanation belongs in trigger, summary, constraints, and queue timing. Reserve `blocking_reason` for wake packets whose task status is actually `blocked`."
>
> **Dev:** "Should queued wake packets get their own machine-readable `queue_reason` field?"
> **Domain expert:** "No. Odin already has trigger, summary, constraints, and `next_eligible_at`. Do not introduce a parallel queue-reason taxonomy unless those fields prove insufficient."
>
> **Dev:** "If a queued wake packet still needs structured 'why requeued' detail, where should that go?"
> **Domain expert:** "In `Evidence.Kind` plus summary or ref. Reuse the existing evidence model before adding another top-level packet field."
>
> **Dev:** "Can `Evidence.Kind` stay free-form text?"
> **Domain expert:** "No. Once packet control semantics depend on it, `Evidence.Kind` should use a constrained canonical namespaced vocabulary such as `runtime.restart` or `delegation.created` instead of ad hoc strings."
>
> **Dev:** "Can any caller mint a new namespaced `Evidence.Kind` ad hoc?"
> **Domain expert:** "No. Canonical wake-packet `Evidence.Kind` values should come from a closed documented registry, not an open-ended naming convention."
>
> **Dev:** "Should that registry launch broad so future producers already have slots?"
> **Domain expert:** "No. Start with the small live-driven set Odin already produces, such as `runtime.restart`, `runtime.fault`, `tool.snapshot`, and `delegation.created`, and expand only when a new producer actually exists."
>
> **Dev:** "What is the exact v1 mapping from the current bare evidence kinds?"
> **Domain expert:** "Use `restart -> runtime.restart`, `fault -> runtime.fault`, `tool -> tool.snapshot`, and `delegation -> delegation.created`."
>
> **Dev:** "Should code be the primary registry for those evidence kinds?"
> **Domain expert:** "No. Keep the canonical registry in `docs/contracts/context-compaction.md` and make code and tests mirror that authored contract."
>
> **Dev:** "If a caller sends an unknown `Evidence.Kind`, should Odin still write the packet and hope reviews catch it later?"
> **Domain expert:** "No. Checkpoint compaction should reject unknown `Evidence.Kind` values at write time so invalid control-plane evidence never becomes durable state."
>
> **Dev:** "Should runtime code read the Markdown contract directly to know the valid evidence kinds?"
> **Domain expert:** "No. Keep the doc canonical, but mirror the registry in Go through named constants, and use tests to keep code aligned with `docs/contracts/context-compaction.md`."
>
> **Dev:** "Can `Evidence.Kind` stay a raw string field if we have constants?"
> **Domain expert:** "No. Give it a dedicated Go type such as `EvidenceKind`, the same way trigger values use `Trigger`, so the control-plane vocabulary is explicit in code."
>
> **Dev:** "Can a new producer just invent a new family segment for `EvidenceKind`?"
> **Domain expert:** "No. In v1 the family segment is a closed documented set: `runtime`, `tool`, and `delegation`. Expand it only when a real new producer forces the change."
>
> **Dev:** "Can a producer invent a new action inside an approved family without updating the registry?"
> **Domain expert:** "No. In v1 the action set is also closed per family. Do not add a new action under `runtime`, `tool`, or `delegation` unless the canonical registry is deliberately extended."
>
> **Dev:** "Should the contract doc just list all `EvidenceKind` values flat?"
> **Domain expert:** "No. Group the registry by family in `context-compaction.md` so the closed family-local action structure stays visible to future readers."
>
> **Dev:** "Should v1 `EvidenceKind` allow deeper hierarchies like `runtime.recovery.restart`?"
> **Domain expert:** "No. Keep v1 to a strict two-segment `<family>.<action>` shape such as `runtime.restart` or `delegation.created`. That is enough for the live set and avoids premature hierarchy."
>
> **Dev:** "While producers are being updated, should new wake-packet writes still accept old bare kinds like `restart` or `fault` as deprecated aliases?"
> **Domain expert:** "No. New writes should use only the canonical namespaced `EvidenceKind` values. If compatibility is needed, handle it on read or through one-time migration, not by letting deprecated aliases continue into durable state."
>
> **Dev:** "What about old stored wake packets that already contain bare evidence kinds?"
> **Domain expert:** "Normalize those historical values on read into the canonical namespaced `EvidenceKind` forms for operator surfaces and internal read models, but preserve the raw stored payload until a deliberate backfill exists."
>
> **Dev:** "Should every projection or caller do that normalization for itself?"
> **Domain expert:** "No. Put historical `EvidenceKind` alias normalization in one shared wake-packet decode path so resume logic, projections, and later consumers all see the same canonical read behavior."
>
> **Dev:** "Should that shared decode path stay implicit inside one service method?"
> **Domain expert:** "No. Expose an explicit shared wake-packet decode helper in `internal/runtime/checkpoints` and require projections and other consumers to use it instead of raw `json.Unmarshal` on wake packet payloads."
>
> **Dev:** "Should we generalize that into a broader packet-decoder framework immediately?"
> **Domain expert:** "No. Start with a wake-packet-specific decoder such as `DecodeTaskWakePacket`. `ProjectContext` and `RunContext` can stay on the simple generic unmarshal path until they actually need richer decode semantics."
>
> **Dev:** "Should `DecodeTaskWakePacket` accept only raw payload JSON?"
> **Domain expert:** "No. Have it accept the full `ContextPacket` and validate `PacketScope == task_wake_packet` before decoding so wake-specific compatibility rules stay attached to actual wake packets."
>
> **Dev:** "If scope is already `task_wake_packet`, should the decoder ignore a mismatched `packet_kind`?"
> **Domain expert:** "No. Current wake writes already use `packet_kind: wake`, so a packet that claims wake scope but not wake kind should be treated as an invalid envelope rather than silently accepted."
>
> **Dev:** "Should that invalid-envelope failure just come back as a generic decode error?"
> **Domain expert:** "No. Return a typed sentinel such as `ErrInvalidTaskWakePacketEnvelope`, with wrapped detail for scope mismatch, kind mismatch, or malformed payload, so callers can rely on `errors.Is` instead of message text."
>
> **Dev:** "Should invalid wake-envelope corruption stay only as a caller error?"
> **Domain expert:** "No. It should also open or reuse a runtime incident tied to the affected task so operator projections show explicit human attention required."
>
> **Dev:** "Should each failing wake reader open that incident directly?"
> **Domain expert:** "No. Convert `ErrInvalidTaskWakePacketEnvelope` into an observation that flows through the deterministic diagnosis path, so one shared incident owner decides whether to open or reuse the task-linked incident."
>
> **Dev:** "Should that observation just reuse an existing fault family like `projection_stale`?"
> **Domain expert:** "No. Invalid wake-envelope corruption is its own control-plane failure mode and should get its own explicit `FaultKey`, such as `wake_packet_invalid`."
>
> **Dev:** "Should `wake_packet_invalid` key incidents by packet ID?"
> **Domain expert:** "No. Use a stable task-scoped `SubjectKey`, such as `task:<task-id>:wake-packet`, so repeated invalid wake-envelope failures on the same task reuse one incident. Packet ID is diagnostic detail, not incident identity."
>
> **Dev:** "Should `wake_packet_invalid` just inherit the recovery default severity?"
> **Domain expert:** "No. Set its observation severity explicitly to `error`. Invalid wake-envelope corruption is stronger than a stale-data warning because Odin is refusing to trust durable wake state, even if the blast radius is task-scoped."
>
> **Dev:** "Should `wake_packet_invalid` trigger an automatic repair playbook?"
> **Domain expert:** "No. Start with no automatic playbook. Diagnosis should open or reuse the incident and stop at human attention required rather than attempting speculative repair of authoritative wake state."
>
> **Dev:** "Should recovery `Decision` keep inferring behavior only from whether `Playbook` is empty?"
> **Domain expert:** "No. Give `Decision` an explicit deterministic outcome mode such as `ignore`, `incident_only`, `playbook`, and `escalate`. That matches the design doc and cleanly supports incident-worthy faults like `wake_packet_invalid` that intentionally have no automatic playbook."
>
> **Dev:** "For `wake_packet_invalid`, should Odin use `incident_only` rather than a no-op playbook or `escalate`?"
> **Domain expert:** "Yes. `wake_packet_invalid` is incident-worthy and human-attention-required, but it does not currently justify an automatic repair playbook. `incident_only` expresses that directly."
>
> **Dev:** "Should `incident_only` still create a `Recovery` row so the executor shape stays uniform?"
> **Domain expert:** "No. `recoveries` are deterministic recovery attempts. `incident_only` should open or reuse the incident and stop without fabricating a fake recovery attempt."
>
> **Dev:** "Should executor `Outcome` still pretend there is a concrete `Recovery` record even when the decision was `incident_only`?"
> **Domain expert:** "No. `Outcome` should explicitly allow no recovery attempt for `incident_only`, rather than returning a zero-value **Recovery** record."
>
> **Dev:** "Should `incident_only` just reuse executor outcome status `completed`?"
> **Domain expert:** "No. `completed` sounds like a deterministic recovery attempt ran successfully. `incident_only` should return its own explicit executor `Outcome.Status` so the audit trail shows Odin intentionally stopped after incident management."
>
> **Dev:** "Should recovery logs still say `self-heal playbook completed` and always emit a `playbook` field?"
> **Domain expert:** "No. Logging and audit-style fields should use generic decision language and include `playbook` only when the decision outcome is actually `playbook`."
>
> **Dev:** "Should recovery `Decision` itself stay playbook-shaped and rely on special cases for everything else?"
> **Domain expert:** "No. `Decision` should carry an explicit typed mode such as `ignore`, `incident_only`, `playbook`, or `escalate`, with `Playbook` optional and valid only when the mode is `playbook`."
>
> **Dev:** "Should ignored observations still just disappear from the diagnosis result?"
> **Domain expert:** "No. `ignore` is itself a diagnosis outcome, so diagnosis should emit explicit `Decision{mode: ignore}` records rather than forcing readers to infer ignore from absence."
>
> **Dev:** "Should `ignore` still go through the executor just to produce an `ignored` outcome?"
> **Domain expert:** "No. `ignore` should stop at diagnosis and be logged there. There is no bounded runtime work to execute."
>
> **Dev:** "Should `CycleResult.Outcomes` still include `ignore` somehow?"
> **Domain expert:** "No. `Outcomes` should remain executor-only. `ignore` is visible through explicit `Decisions` and diagnosis logging, not as an execution row."
>
> **Dev:** "Should `incident_only` still appear in `CycleResult.Outcomes` even though there is no recovery row?"
> **Domain expert:** "Yes. `incident_only` is still executor-handled side-effecting work because Odin opened or reused an incident. It belongs in `Outcomes`, but without a recovery attempt attached."
>
> **Dev:** "Should `Outcome.Attempt` use `0` for `incident_only`?"
> **Domain expert:** "No. `Attempt` already means recovery-attempt count in the current executor logic. For `incident_only`, it should stay unset because no recovery attempt occurred."
>
> **Dev:** "Should recovery `Outcome` keep booleans like `Suppressed` and `Escalated` alongside `Status`?"
> **Domain expert:** "No. Those booleans duplicate the explicit status vocabulary and create extra drift points. `Outcome` should rely on `Status` alone."
>
> **Dev:** "Should recovery `Outcome.Status` stay a raw string?"
> **Domain expert:** "No. It should use its own typed canonical vocabulary such as `incident_only`, `completed`, `failed`, `suppressed`, and `escalated`, just as `Decision` now has an explicit typed mode."
>
> **Dev:** "Should playbook `ActionResult.Status` share that same executor outcome vocabulary?"
> **Domain expert:** "No. `ActionResult` is playbook-local and should keep its own small typed status layer. The executor should map those action results into canonical executor `Outcome.Status` values."
>
> **Dev:** "Should the v1 playbook `ActionResult.Status` set stay closed to `completed`, `failed`, and `escalated`?"
> **Domain expert:** "Yes. That matches current live usage and keeps executor-only statuses such as `suppressed` and `incident_only` out of the playbook action layer."
>
> **Dev:** "Should the executor coerce unknown `ActionResult.Status` values into the generic failure branch?"
> **Domain expert:** "No. Unknown action-result statuses should be treated as invalid playbook output and rejected as a contract error, not reinterpreted as ordinary fault outcomes."
>
> **Dev:** "Should that contract violation surface through a typed sentinel such as `ErrInvalidActionResultStatus`?"
> **Domain expert:** "Yes. That matches the existing executor error pattern and keeps invalid playbook output explicit and testable instead of collapsing it into a vague generic executor error."
>
> **Dev:** "If invalid `ActionResult.Status` is detected after a recovery attempt has already started, should the incident just stay `open`?"
> **Domain expert:** "No. Seal the recovery attempt as `failed`, return `ErrInvalidActionResultStatus`, and mark the linked incident `escalated` with explicit contract-violation details. An automation contract bug needs stronger operator attention than an ordinary unresolved fault."
>
> **Dev:** "Should Odin skip `recovery.action_executed` if the playbook returned an invalid status?"
> **Domain expert:** "No. A bounded recovery action still ran, so Odin should append `recovery.action_executed` with canonical result `failed` and explicit contract-violation details. The invalid raw status should not become the canonical event result."
>
> **Dev:** "Should Odin preserve the invalid raw status string anywhere?"
> **Domain expert:** "Yes. Keep it only in non-canonical diagnostic details for forensics. Canonical status fields and event result fields should stay normalized to `failed`."
>
> **Dev:** "Should that raw invalid status also be copied into **Incident** details?"
> **Domain expert:** "No. Keep the raw invalid status only in **Recovery** diagnostic details. **Incident** details should stay summary-level and carry contract-violation reason or category without duplicating playbook-specific forensic payload."
>
> **Dev:** "Should `recovery.action_executed` keep that contract-violation detail only in prose `Description`?"
> **Domain expert:** "No. It should gain an optional structured diagnostic-details field so contract-violation metadata can travel canonically in the event without forcing consumers to parse prose."
>
> **Dev:** "Should that new event detail just be a generic `details_json` blob?"
> **Domain expert:** "No. Runtime events already use small typed payloads. `recovery.action_executed` should add a small typed optional sub-structure rather than importing a generic blob into the event contract."
>
> **Dev:** "Should `contract_violation` carry only the raw invalid status string?"
> **Domain expert:** "No. It should include a canonical machine-readable violation key, such as `invalid_action_result_status`, plus the raw offending value. That keeps event consumers queryable while preserving exact malformed output for forensics."
>
> **Dev:** "Should `contract_violation.key` stay an open string?"
> **Domain expert:** "No. It should be a closed typed registry, with v1 starting at exactly `invalid_action_result_status`. That matches Odin’s other small typed key sets and prevents near-duplicate spellings from creeping into the event stream."
>
> **Dev:** "Should that key be namespaced as `recovery.invalid_action_result_status`?"
> **Domain expert:** "No. The event type already carries the namespace. Like other Odin inner key registries, the v1 key should stay the bare snake_case token `invalid_action_result_status`."
>
> **Dev:** "Should the payload use a generic field like `raw_value` instead of `raw_status`?"
> **Domain expert:** "No. The only live violation key is `invalid_action_result_status`, and the malformed source field is literally `ActionResult.Status`. `raw_status` is the clearest v1 shape."
>
> **Dev:** "Should v1 also add `source_field: \"ActionResult.Status\"`?"
> **Domain expert:** "No. The closed v1 key already implies that source. Adding `source_field` now would just duplicate meaning and create another field that could drift."
>
> **Dev:** "Should v1 add `allowed_statuses: [\"completed\", \"failed\", \"escalated\"]` to each contract-violation event?"
> **Domain expert:** "No. That valid set is already canonical in the closed `ActionResult.Status` registry. Repeating it inside each event would duplicate contract truth and create another drift surface."
>
> **Dev:** "Should `raw_status` stay optional even for `invalid_action_result_status`?"
> **Domain expert:** "No. The executor already has a concrete `ActionResult.Status` before it records `recovery.action_executed`, and this branch already chose to preserve that malformed status for forensics. `raw_status` should be required for that key."
>
> **Dev:** "Should projections just skip invalid wake packets so dashboards keep rendering?"
> **Domain expert:** "No. Projections and other read models should fail fast on `ErrInvalidTaskWakePacketEnvelope`. Wake packets are control-plane authority, so hiding envelope corruption would make operator surfaces look healthier than reality."
>
> **Dev:** "Can wake-packet `trigger` carry queue-outcome reasons like `executor_unavailable`?"
> **Domain expert:** "No. `trigger` should stay the packet-creation cause such as `restart`, `approval_wait`, or `handoff`. Queue-outcome meaning belongs elsewhere."
>
> **Dev:** "Should denial leave the approval-wait packet active in case someone changes their mind?"
> **Domain expert:** "No. Denial should mark the linked `approval_wait` wake packet `sealed` with a denied or stale reason. The current approval-based resume path is intentionally closed."
>
> **Dev:** "If we later replan and still need approval, should Odin reopen the denied approval?"
> **Domain expert:** "No. A later alternative path should start a brand-new **Approval Request** and a brand-new `approval_wait` wake packet. The denied approval cycle stays as terminal history."
>
> **Dev:** "Should that later approval cycle stand alone?"
> **Domain expert:** "No. The new **Approval Request** should explicitly reference the denied approval it follows so the governance chain stays queryable after replanning."
>
> **Dev:** "If a new approval replaces the expired one, can Odin just infer the relationship from timestamps?"
> **Domain expert:** "No. The replacement **Approval Request** should explicitly reference the expired approval it supersedes so the governance chain stays queryable and auditable."
>
> **Dev:** "If the old approval expires, should Odin just edit the existing approval-wait packet?"
> **Domain expert:** "No. Odin should supersede the old `approval_wait` wake packet and write a fresh one for the new approval context so the latest active resume state matches the currently pending approval."
>
> **Dev:** "Is the finance advisor the same thing as the worker that executes a task?"
> **Domain expert:** "No. The finance advisor is a **Companion**. The execution unit is a **Worker** spawned for one bounded step."
>
> **Dev:** "If a run crashes, is the work gone?"
> **Domain expert:** "No. The **Work Item** remains durable. The crashed execution is just one **Run Attempt**."
>
> **Dev:** "Does the nightly schedule just spawn a worker?"
> **Domain expert:** "No. The schedule is an **Automation Trigger**. It first creates or updates a **Work Item** and only later dispatches a worker through normal policy checks."
>
> **Dev:** "Can `/overview` show follow-up routines as Automation Triggers?"
> **Domain expert:** "Yes. In v1, a **Follow-Up Obligation** is the schedule-backed Automation Trigger surface. The lane should show the trigger definition and due state separately from any Work Item the trigger has materialized."
>
> **Dev:** "Should the mobile UI open as a run monitor?"
> **Domain expert:** "No. The operator surface should start at **Workspace**, narrow to **Initiative**, and then show **Work Item** detail with **Run Attempts** nested underneath."
>
> **Dev:** "Should every work item show repo, cwd, backend, and sessions by default?"
> **Domain expert:** "No. Default **Work Item** detail should show the durable business context first. Execution metadata is conditional, not universal."
>
> **Dev:** "Is approval just a field on the work item?"
> **Domain expert:** "No. Approval should be a durable **Approval Request** linked to the **Work Item**, and optionally to the triggering **Run Attempt**."
>
> **Dev:** "When approval is granted, does the old run keep going?"
> **Domain expert:** "No. Approval should unblock the **Work Item**, return it to `queued`, and let a fresh **Run Attempt** continue from persisted wake state."
>
> **Dev:** "Should the app show one shared status badge for everything?"
> **Domain expert:** "No. **Work Item**, **Run Attempt**, and **Approval Request** each need their own status vocabulary because they describe different lifecycles."
>
> **Dev:** "Should we add a new social manager object for Marcus?"
> **Domain expert:** "No. The canonical concept is the **Social Copilot**, and today it rides the existing workflow, memory, and tool surfaces."
>
> **Dev:** "When Marcus approves a social draft, is it still a draft?"
> **Domain expert:** "No. The candidate content is a **Social Draft**. The approval decision belongs on a **Social Outcome**."
>
> **Dev:** "Is visible X capture the same as analytics?"
> **Domain expert:** "No. **Social Evidence** is operator-observed evidence for an explicit published artifact, not a crawler or full analytics pipeline."
>
> **Dev:** "If Odin drafts a good X reply, can it publish that reply today?"
> **Domain expert:** "No. That is a **Reply Suggestion** in the current live surface. Native publish is limited to top-level X posts."
>
> **Dev:** "Should we keep calling the shared browser capability Huginn?"
> **Domain expert:** "No. The canonical platform term is **Browser Control**. Huginn is an integration adapter and implementation name."
>
> **Dev:** "When login or CAPTCHA blocks a run, is that a new approval object?"
> **Domain expert:** "No. That is a **Browser Intervention** on a **Trusted Browser Session**. Business workflows keep their own approval and run state."
>
> **Dev:** "If the human fixes login in the shared browser while the run is still open, is that a new run?"
> **Domain expert:** "No. That is still the same live **Run Attempt** inside one attached **Trusted Browser Session**."
>
> **Dev:** "What if the shell exits or the work is resumed later by another worker?"
> **Domain expert:** "Then Odin must resume through a fresh **Run Attempt** from persisted wake or evidence state, even if it reuses the same **Trusted Browser Session**."
>
> **Dev:** "Should Browser Intervention get its own durable object and status flow?"
> **Domain expert:** "No. In v1 it belongs on **Run Attempt** execution evidence and wake context. Workflow approvals and business lifecycle stay where they already live."
>
> **Dev:** "Should `google_login_required` become a shared browser reason?"
> **Domain expert:** "No. Normalize it to **Browser Intervention Reason** `login_required` and keep the Google-specific detail in driver artifacts."
>
> **Dev:** "What about `compose_surface_missing`?"
> **Domain expert:** "That is not a shared **Browser Intervention Reason**. It is a browser or automation failure unless a human really must step in."
>
> **Dev:** "Should generic Browser Control get a new `/browser` shell surface?"
> **Domain expert:** "No. In v1 it should stay on the existing `/tool` operator surface and builtin tool catalog unless real operator use proves that is insufficient."
>
> **Dev:** "Should the tool catalog keep `huginn_*` as the long-term browser key family?"
> **Domain expert:** "No. The long-term generic family should speak platform language such as `browser_*`. Adapter names like `huginn_*` can survive only as transition or implementation language."

## Flagged ambiguities

- "project" was being used to mean both Git-governed delivery work and non-project life or admin operations. Resolved: both are **Initiatives**, but only Git-governed delivery work is a **Managed Project**.
- `/project` was at risk of redefining non-Git initiatives by operator convenience. Resolved: `/project` is legacy CLI wording for initiative selection, and it does not change Family-Ops finance from a **Managed Domain** into a **Managed Project**.
- "Huginn" was at risk of becoming the platform domain term. Resolved: **Browser Control** is the shared capability, while Huginn remains an integration adapter and implementation name.
- "browser session" was at risk of drifting into workflow-owned nouns such as finance session or social session. Resolved: the shared reusable browser context is a **Trusted Browser Session**, which downstream domains may reference as execution evidence but do not own.
- login, MFA, CAPTCHA, and similar live blockers were at risk of being modeled as hidden side effects or as new business approval objects. Resolved: they are **Browser Interventions** within **Browser Control** unless a downstream domain separately requires its own approval decision.
- "resume" after a browser blocker was at risk of sounding like one run stays alive across every pause. Resolved: live human-agent handoff may stay on the same attached **Trusted Browser Session** while execution remains active, but any durable resume after shell exit, worker handoff, approval wait, crash, or similar pause must continue through a fresh **Run Attempt** from persisted wake or evidence state.
- "browser intervention" was at risk of becoming a new persisted workflow object by default. Resolved: in v1 it is represented as **Run Attempt** execution evidence plus optional wake/evidence context, while workflow-owned approvals and business lifecycle remain on their existing objects.
- workflow-specific blocker strings such as `google_login_required` and `blocked_on_mfa` were at risk of becoming shared platform reason terms. Resolved: normalize them into the coarse closed **Browser Intervention Reason** set and keep workflow-specific nuance in driver artifacts or **Run Attempt** evidence.
- browser or automation failures such as `compose_surface_missing`, `compose_type_failed`, and `post_button_not_ready` were at risk of being collapsed into human-step intervention reasons. Resolved: only blockers that truly require human step-in use **Browser Intervention Reason**; ordinary browser execution defects stay driver or **Run Attempt** failures.
- generic browser work was at risk of growing a parallel `/browser` shell family by default. Resolved: v1 generic **Browser Control** stays on the existing `/tool` **Operator Surface** and builtin tool catalog unless real operator use proves that surface is insufficient.
- browser tool catalogs were at risk of keeping adapter-branded `huginn_*` names as the long-term public vocabulary even after the domain model locked generic **Browser Control**. Resolved: the long-term generic catalog family should use `browser_*` keys on `/tool`, while `huginn_*` may remain only as transition or implementation language.
- "transfer intent" was at risk of becoming a premature persistence abstraction. Resolved: **Transfer Intent** is valid domain language, but v1 represents it through existing **Work Item**, **Run Attempt**, **Approval Request**, and wake-packet state rather than a dedicated table.
- "deposit" and "withdraw" were at risk of drifting between broker, bank, and household-ledger viewpoints. Resolved: **Transfer Direction** is always interpreted from Robinhood's point of view.
- "source_account" and "destination_account" were at risk of leaking transport vocabulary into the business model. Resolved: those remain transport fields, while the canonical finance legs are **Funding Account** and **Robinhood Account**.
- transfer retries were at risk of preserving one finance identity across materially different money movements. Resolved: changes to amount, **Transfer Direction**, **Funding Account**, or **Robinhood Account** create a new **Transfer Intent** even when the same **Work Item** continues.
- memo edits were at risk of being treated as new finance identity by default. Resolved: memo remains annotation on the same **Transfer Intent** unless it changes the real-world meaning of the money movement.
- corrected transfers were at risk of inheriting stale approval evidence. Resolved: a new **Transfer Intent** always requires fresh prepare evidence, a fresh `approval_wait` wake packet, and a fresh **Approval Request**.
- operator denial was at risk of being confused with transfer-intent destruction. Resolved: an unchanged **Transfer Intent** may survive denial, but only through a brand-new prepare/evidence/approval cycle.
- stale session or stale review context was at risk of being confused with new transfer identity. Resolved: unchanged money-movement facts keep the same **Transfer Intent**; staleness only forces a fresh prepare/evidence/approval cycle.
- operator cancellation was at risk of collapsing into ordinary denial semantics. Resolved: cancellation terminates the current **Transfer Intent** itself, and any later attempt starts a new **Transfer Intent**.
- transfer read-model cancellation was at risk of being hidden behind raw intent history only. Resolved: `canceled` is the compact derived status when the current **Transfer Intent** itself was explicitly terminated by the operator.
- transfer read-model uncertainty was at risk of collapsing into either false success or generic failure. Resolved: `submission_unconfirmed` is the compact derived status when submit was attempted but Robinhood acceptance is not proven.
- transfer read-model denial was at risk of being mistaken for cancellation or hidden behind raw approval rows only. Resolved: `denied` is the compact derived status when the latest approval cycle on an unchanged **Transfer Intent** ended in operator denial.
- transfer read-model pre-submit readiness was at risk of staying implicit in summaries only. Resolved: `pending_approval` is the compact derived status when the current **Transfer Intent** has fresh review evidence and an active approval decision is still pending.
- transfer read-model stale-cycle invalidation was at risk of staying hidden in raw run evidence only. Resolved: `reprepare_required` is the compact derived status when the current **Transfer Intent** is unchanged but the prior prepare/evidence cycle must be rebuilt.
- transfer read-model progress was at risk of cloning generic workflow execution state. Resolved: v1 does not add `preparing`; operators read **Work Item** `running` plus **Run Attempt** evidence until a transfer-specific decision status is warranted.
- transfer read-model failure was at risk of cloning generic workflow lifecycle. Resolved: v1 does not add `failed`; ordinary failure stays on **Work Item** `failed` plus **Run Attempt** evidence unless a narrower transfer-specific status already applies.
- transfer read-model expiry was at risk of duplicating approval-object lifecycle. Resolved: v1 does not add `expired`; operators read **Approval Request** `expired` and the transfer-facing consequence as `reprepare_required`.
- transfer read-model approval was at risk of duplicating approval-object lifecycle and exposing a transient orchestration handoff as if it were a durable operator state. Resolved: v1 does not add `approved`; operators read **Approval Request** `approved` and then the resulting transfer outcome.
- transfer read-model completion was at risk of overclaiming settlement or reconciliation ownership beyond confirmed request acceptance. Resolved: v1 does not add `completed`; `submitted` is the terminal transfer outcome for this workflow.
- transfer read-model initialization was at risk of inventing pre-workflow labels such as `not_started`. Resolved: before the first prepare run creates transfer-specific evidence, **Transfer Status View** omits derived transfer status and points operators to the governing **Work Item** state instead.
- transfer status endpoint shape was at risk of splitting into placeholder states or 404 behavior before workflow evidence existed. Resolved: keep one canonical **Transfer Status View** endpoint and omit transfer-specific fields until the first prepare run creates evidence.
- transfer status identifiers were at risk of appearing as synthetic `null`, `0`, or placeholder values before transfer workflow evidence existed. Resolved: pre-prepare **Transfer Status View** omits transfer-specific identifiers such as `run_id` and `approval_id` until the relevant run or approval exists.
- transfer status evidence fields were at risk of being backfilled with generic prose or empty objects before transfer workflow evidence existed. Resolved: pre-prepare **Transfer Status View** omits transfer-specific `summary` and `artifacts` until transfer evidence actually exists.
- transfer transport naming was at risk of implying a second identifier beside the governing **Work Item**. Resolved: `task_key` remains the v1 transport alias for the governing **Work Item** key and does not create separate transfer identity.
- transfer payload naming was at risk of duplicating the governing **Work Item** identity under both `task_key` and `work_item_key`. Resolved: v1 keeps `task_key` as the only transport key for that governing object.
- transfer route naming was at risk of duplicating the governing **Work Item** identity under both `/tasks/{taskKey}` and `/work-items/{workItemKey}`. Resolved: v1 keeps `/api/transfers/tasks/{taskKey}` as the only transfer-status route.
- transfer write-contract naming was at risk of hardening legacy project language into a new initiative-owned finance API. Resolved: `initiative_key` is the canonical prepare-request field, while `project_key` is legacy compatibility only if still needed during transition.
- transfer prepare-request compatibility was at risk of being left implicit after the canonical field changed. Resolved: v1 may still accept deprecated `project_key` as an alias for `initiative_key` during transition, but only `initiative_key` is canonical.
- transfer prepare-request scope selection was at risk of silently choosing one field when clients sent both canonical and deprecated names. Resolved: if `initiative_key` and `project_key` are both supplied, Odin accepts only matching values and rejects conflicts.
- transfer prepare-request normalization was at risk of letting deprecated project language leak past the HTTP boundary into runtime params and durable finance state. Resolved: accepted `project_key` input is normalized immediately to canonical initiative-shaped semantics before any downstream transfer state is constructed or persisted.
- transfer shell scope selection was at risk of duplicating initiative context inside `/transfer prepare` arguments. Resolved: on the shell surface, initiative selection stays in shell context and `/transfer prepare` carries transfer facts only.
- transfer shell scope fallback was at risk of prompting interactively or silently defaulting when no initiative context was active. Resolved: `/transfer prepare` fails fast with an explicit initiative-selection error instead of inferring scope in a finance workflow.
- transfer shell approval naming was at risk of drifting into HTTP-shaped `approval_id` wording instead of the REPL's normal compact handle style. Resolved: successful shell `/transfer prepare` output uses `approval=<id>`, while `approval_id` stays on HTTP and JSON transport surfaces.
- transfer shell success output was at risk of exposing only the pending approval handle and forcing operators to fetch task and run handles separately. Resolved: successful shell `/transfer prepare` output includes `task=<key> run=<id> approval=<id>` on the operator surface.
- transfer shell success confirmation was at risk of exposing only raw handles and making operators jump immediately to follow-up inspection commands just to confirm the prepared state. Resolved: successful shell `/transfer prepare` output also prints a concise `summary=` line after the handle line.
- transfer shell success evidence was at risk of turning the immediate success surface into a duplicate evidence UI by dumping artifact fields such as `review_url` and `screenshot_path`. Resolved: artifact detail stays on `/approvals`, `/runs show`, and **Transfer Status View** rather than the immediate `/transfer prepare` success output.
- transfer shell success guidance was at risk of leaving the approval-paused follow-up path implicit even though this finance workflow intentionally stops for operator review. Resolved: immediate shell success output also includes a concise `next=` hint for the operator's follow-up path.
- transfer shell success guidance was at risk of staying vague prose even though the shell and the transfer docs already describe concrete follow-up commands. Resolved: the immediate `next=` hint uses exact shell-command guidance with the concrete `run` and `approval` handles from the prepare result.
- transfer shell success guidance was at risk of stopping at the first review command and leaving the approval-resolution step implicit even though approval resolution is part of the same governed operator path. Resolved: the immediate `next=` hint covers the full governed follow-up path through run review and approval resolution.
- transfer shell success guidance was at risk of redundantly including `/approvals` even after Odin already printed the concrete approval handle. Resolved: the immediate `next=` hint skips `/approvals` in v1 and points operators to `/runs show <run-id>` plus `/approvals resolve <approval-id> ...`.
- transfer shell success guidance was at risk of mixing a concrete printed run handle with the state-dependent alias `/runs show active`. Resolved: the immediate `next=` hint uses `/runs show <run-id>` rather than `/runs show active`.
- transfer shell success guidance was at risk of mixing a concrete printed approval handle with a generic `<approval-id>` placeholder. Resolved: the immediate `next=` hint inlines the concrete approval handle while still leaving the operator decision open as `approve|deny`.
- transfer shell success guidance was at risk of expanding one approval decision into multiple branch-specific command variants and bloating the immediate hint. Resolved: the approval step stays one compact template with a concrete approval handle and an open `approve|deny` slot.
- transfer shell success guidance was at risk of ending at the approval-resolution command even though the documented operator flow still includes post-resolution verification. Resolved: the immediate `next=` hint also includes the final verification command for the resumed run outcome.
- transfer shell success guidance was at risk of pointing the final verification step back at the stale prepare run even though approval resolution records a fresh submit continuation run. Resolved: the final verification step follows the submit run returned by `/approvals resolve ...`, not the original prepare run.
- transfer shell success guidance was at risk of implying that the prepare run ID remained the correct post-submit verification target after approval. Resolved: the prepare-time hint explicitly depends on the submit continuation run returned by `/approvals resolve ...` for the final verification step.
- approval-resolution shell output was at risk of returning submit continuation only implicitly, leaving the operator without a stable handle for the final verification step. Resolved: successful `/approvals resolve ... approve ...` output prints `approval=<id> status=resolved result=approved run=<submit-run-id>` for the returned submit continuation run.
- approval-resolution shell output was at risk of becoming a finance-only exception by omitting the generic `status=resolved` token even though other shell resolve surfaces already print both `status=resolved` and a branch-specific `result=...`. Resolved: finance approval resolution also prints `status=resolved` on both approve and deny branches, while `result=` carries the branch outcome and only approve adds the continuation `run=` handle.
- approval-resolution shell output was at risk of staying token-only and forcing operators to translate bare status fields mentally even though the shell already uses concise `summary=` lines elsewhere. Resolved: successful `/approvals resolve ...` output also prints a compact second-line `summary=<...>` after the token receipt.
- approval-resolution shell output was at risk of growing a second guidance channel with its own `next=` hint even though `/transfer prepare` already owns the guided approval-paused path and resolve receipts already expose `run=` when continuation starts. Resolved: `/approvals resolve ...` remains a receipt-only surface with tokens, optional `run=`, and `summary=`, but no `next=` line.
- approval-resolution shell output was at risk of turning the new `summary=` line into a second copy of raw status jargon even though the receipt already prints structured status tokens on the first line. Resolved: the finance `summary=` line stays natural-language and does not simply mirror raw status names such as `reprepare_required`, `submission_unconfirmed`, or `pending_approval`.
- approval-resolution shell output was at risk of overclaiming confirmed transfer success by using approve-side summary text like `transfer submitted` even though approval resolution only starts submit continuation and returns the submit run. Resolved: the approve summary uses continuation language such as `approval granted; submit continuation started`, leaving confirmed submission to later run and transfer evidence.
- approval-resolution shell output was at risk of overloading the approve summary with follow-up verification guidance even though the receipt already prints `run=<submit-run-id>` and the broader flow already defines the later `/runs show` check. Resolved: the approve summary stays short and leaves next-action guidance to the returned run handle or any dedicated guidance surface.
- approval-resolution shell output was at risk of using deny-side summary text that implied cancellation or permanent impossibility even though denial only closes the current approval cycle on an unchanged transfer. Resolved: the deny summary uses wording such as `approval denied; later retry requires fresh prepare`, keeping the denial outcome and retry requirement explicit without changing transfer identity.
- approval-resolution shell output was at risk of overloading the deny summary with deeper unchanged-transfer identity semantics even though the receipt already prints `result=denied` and the glossary already defines what that means. Resolved: the deny summary stays short and focuses on the denial outcome plus fresh-prepare retry consequence, leaving the fuller unchanged-transfer rule to the glossary.
- approval-resolution shell output was at risk of turning the approve receipt into a second transcript surface by echoing free-form operator rationale on success. Resolved: immediate approve output stays compact and omits the approval reason; reason inspection belongs on approval detail, run evidence, or other durable audit surfaces.
- approval-resolution shell output was at risk of importing the social-memory resolver's `rejected` vocabulary and drifting away from the finance glossary's canonical denial wording. Resolved: finance approval resolution keeps `result=denied`, because finance **Approval Request** and transfer-status language already anchor operator denial on `denied`.
- approval-resolution shell output was at risk of turning the deny receipt into a second transcript surface by echoing free-form operator rationale on success. Resolved: immediate deny output stays compact and omits the denial reason; reason inspection belongs on approval detail, run evidence, or other durable audit surfaces.
- approval-resolution shell output was at risk of implying that deny created the same follow-up run surface as approve. Resolved: deny output omits `run=` and instead prints an explicit denial outcome such as `approval=<id> status=resolved result=denied`.
- transfer read-model run selection was at risk of being clobbered by unrelated or newer non-transfer runs on a reused **Work Item**. Resolved: the view derives from the latest transfer-relevant **Run Attempt**, not merely the newest run by timestamp.
- transfer read-model approval selection was at risk of being clobbered by unrelated or older approval cycles on a reused **Work Item**. Resolved: the view derives from the latest transfer-relevant **Approval Request** chain, not any approval attached to the work item.
- transfer read-model wake-packet selection was at risk of falling back to raw newest-packet heuristics on a reused **Work Item**. Resolved: the view uses the wake packet explicitly linked to the selected transfer-relevant **Approval Request** when available, otherwise the latest transfer-relevant wake packet in the same transfer lineage.
- requested timing was at risk of silently becoming transfer identity. Resolved: in v1, timing belongs to approval and execution context, not **Transfer Intent** identity.
- browser-driver states were at risk of leaking into finance lifecycle status. Resolved: `review_ready`, `session_expired`, and `resume_verification_failed` stay as **Run Attempt** execution evidence rather than canonical **Transfer Intent**, **Work Item**, or **Approval Request** status.
- the packet-backed transfer read model was at risk of staying unnamed or being mistaken for a second source of truth. Resolved: call it **Transfer Status View** and keep it explicitly derived from workflow state.
- transfer read-model status was at risk of silently becoming a fourth source of lifecycle truth. Resolved: **Transfer Status View** may expose a compact derived status, but it is computed shorthand over canonical **Work Item**, **Run Attempt**, **Approval Request**, and wake-packet state.
- transfer read-model status was at risk of overclaiming finality from a browser gesture. Resolved: `submitted` on **Transfer Status View** requires confirmed Robinhood acceptance, not just a submit click attempt.
- transfer identity was at risk of collapsing across governed domains. Resolved: **Transfer Intent** identity is local to the governing **Scope**, so identical facts in different **Initiatives** or **Workspaces** are distinct intents.
- transfer modeling was at risk of adding a fingerprint or idempotency concept before it was needed. Resolved: v1 does not define a first-class transfer fingerprint; identity is inferred from **Scope**, core **Transfer Intent** facts, and approval/evidence lineage.
- "managed-domain" was initially treated like a top-level container. Resolved: it is an **Initiative** kind inside a **Workspace**.
- "assistant", "advisor", and "worker" were being used interchangeably. Resolved: durable roles are **Companions** and bounded execution units are **Workers**.
- "inbox" was being used to mean both raw signal intake and executable queued work. Resolved: raw intake lives in **Intake Inbox** and triaged executable work lives in a **Work Queue**.
- raw alerts and governed work were at risk of collapsing into one object. Resolved: raw intake is a durable **Intake Item** and triaged governed work is a **Work Item**.
- intake was at risk of staying project-first even after introducing managed domains. Resolved: **Initiative Intake** is the generic layer above project-specific intake workflows.
- intake ownership was at risk of being assigned too narrowly at ingest time. Resolved: a new **Intake Item** belongs to the **Workspace** first, with **Initiative** and **Scope** resolved later.
- every intake item was at risk of being forced into governed work. Resolved: **Initiative Intake** may resolve some **Intake Items** directly without creating a **Work Item**.
- direct-answer intake was at risk of lacking a durable audit target. Resolved: directly answered **Intake Items** link to a **Conversation Transcript** rather than a lightweight **Work Item**.
- direct answer and follow-up work were at risk of being treated as mutually exclusive. Resolved: one **Intake Item** may yield both a **Conversation Transcript** and one or more follow-up **Work Items**.
- follow-up work created from conversation was at risk of losing origin provenance. Resolved: those **Work Items** keep explicit backlinks to the source **Intake Item** and **Conversation Transcript**.
- intake was at risk of borrowing execution statuses. Resolved: **Intake Item** owns a small intake lifecycle and uses outcome references for branching results.
- duplicate handling was at risk of erasing raw arrival history. Resolved: repeated signals still create durable **Intake Items**, then use suppression and canonical duplicate linking to control noise.
- duplicate handling was at risk of collapsing distinct ownership domains. Resolved: dedupe is **Workspace**-local and may narrow further after **Initiative** resolution, but never crosses **Workspaces**.
- duplicate handling was at risk of binding new signal waves to stale history forever. Resolved: canonical duplicate linking is cooldown-bounded, and equivalent arrivals after the window create a fresh active **Intake Item**.
- duplicate handling was at risk of depending on opaque payload similarity or connector-local heuristics. Resolved: each **Intake Item** carries an Odin-owned explicit **Dedupe Key** derived from **Workspace**, source family, and normalized signal fingerprint.
- duplicate handling was at risk of drifting by connector or adapter implementation. Resolved: adapters provide normalized source facts, but Odin core computes the final **Dedupe Key** centrally.
- duplicate handling was at risk of losing the evidence needed to explain or recompute identity decisions. Resolved: each **Intake Item** persists its normalized **Source Facts** alongside the derived **Dedupe Key**.
- duplicate handling was at risk of silently changing meaning when normalization or fingerprinting logic evolves. Resolved: each **Intake Item** persists the **Dedupe Recipe Version** that produced its **Dedupe Key**.
- duplicate handling was at risk of sprouting an unnecessary second runtime aggregate. Resolved: duplicate arrivals remain individual **Intake Items** linked to a canonical **Intake Item**, and grouping stays a derived operator view.
- duplicate handling was at risk of creating mutable summary truth that could drift from intake history. Resolved: duplicate-wave summaries remain derived operator projections rather than stored counters on the canonical **Intake Item**.
- duplicate handling was at risk of unstable canonical references inside one active window. Resolved: the canonical **Intake Item** remains stable for the active dedupe window, while newer duplicates contribute evidence without taking over canonical ownership.
- duplicate handling was at risk of suppressing materially new evidence until cooldown expiry. Resolved: duplicates inside the active window may reopen or re-triage the canonical **Intake Item** when the handling decision should change, without changing canonical identity.
- duplicate handling was at risk of creating redundant follow-up work for the same obligation. Resolved: material-change retriage reuses or requeues an existing **Work Item** when the obligation is unchanged, and creates new work only for distinct obligations.
- the current `task_intakes` persistence path was at risk of being treated as full **Intake Item** authority. Resolved: it is a task-linked intake record for the current CLI path; a fully live **Intake Inbox** still requires raw Workspace-first **Intake Item** authority before triage, while `/overview` may only label `task_intakes` as linked or triaged intake evidence.
- follow-up schedules were at risk of staying hidden under agenda or work-item views instead of fulfilling the already named **Automation Trigger** lane. Resolved: v1 should surface **Follow-Up Obligations** as schedule-backed **Automation Triggers** while keeping materialized Work Items in the Work Items lane.
- material-change retriage was at risk of leaving a stale pending approval attached to reused work. Resolved: when the approval basis changes materially, the old **Approval Request** expires and Odin creates a fresh one instead of mutating approval history in place.
- material-change retriage and approval handling were at risk of matching approvals to resume state through latest-packet heuristics. Resolved: each pending **Approval Request** explicitly references its active `approval_wait` wake packet.
- approval resolution was at risk of authorizing a moving resume target. Resolved: approval resumes the `approval_wait` wake packet explicitly linked to the approved **Approval Request**, not whichever packet is latest later.
- operator denial was at risk of being confused with permanent failure or cancellation. Resolved: denying an **Approval Request** blocks the **Work Item** for later re-triage or replanning and records the denial reason.
- operator denial was at risk of being captured only as free text. Resolved: the **Approval Request** records a structured `denial_reason_key` plus optional `denial_reason_note`.
- operator denial was at risk of sharing semantics with machine policy denial. Resolved: `denial_reason_key` uses a dedicated operator-denial namespace separate from system deny/block keys.
- operator denial was at risk of being flattened back into a generic blocked label after approval handling. Resolved: **Work Item** `blocked_reason` preserves the operator-denial namespace explicitly instead of collapsing it downstream.
- operator denial was at risk of bloating queue-state vocabulary by copying full approval subtypes onto the **Work Item**. Resolved: **Work Item** `blocked_reason` stays coarse as `operator_denied`, while the linked **Approval Request** carries detailed denial semantics.
- operator denial was at risk of being collapsed back into `approval_required` even after a negative decision. Resolved: `approval_required` means waiting on a pending approval, while `operator_denied` means the approval cycle ended negatively and the work is blocked for re-triage or replanning.
- approved-but-unusable transfer continuity was at risk of being mislabeled as either pending approval or denial at the queue layer. Resolved: the governing **Work Item** uses the third coarse `blocked_reason` `stale_context`, distinct from both `approval_required` and `operator_denied`.
- approved-but-unusable transfer continuity was at risk of minting a synthetic active wake packet even after the approved resume context had been sealed as stale. Resolved: Odin does not create a replacement active packet at continuity loss; the next active packet appears only when a fresh prepare writes a new `approval_wait`.
- approved-but-unusable transfer continuity was at risk of being treated as a terminal work-item failure even though the same unchanged intent can continue later. Resolved: the governing **Work Item** moves to `blocked`, not `failed`, while a fresh prepare/evidence/approval cycle is awaited.
- approved-but-unusable transfer continuity was at risk of being misclassified as a restart-style execution interruption. Resolved: the submit-continuation **Run Attempt** records canonical status `failed`, not `interrupted`, because Odin reached a definite governed non-success outcome.
- approved-but-unusable transfer continuity was at risk of duplicating coarse queue blockage vocabulary at the run-evidence layer. Resolved: the failed continuation run records `resume_verification_failed` as execution evidence, while `stale_context` stays on the governing **Work Item**.
- browser-driver failure evidence was at risk of letting `session_expired` and `resume_verification_failed` overlap. Resolved: `session_expired` means no usable authenticated live session can be maintained, while `resume_verification_failed` means the session exists but the approved expected review state cannot be re-proven.
- browser-driver failure evidence was at risk of forking finance lifecycle outcomes even when both paths leave the approved execution context unusable. Resolved: `session_expired` and `resume_verification_failed` can differ as **Run Attempt** evidence, but both converge to the same downstream finance consequence for the unchanged **Transfer Intent**.
- browser-driver evidence was at risk of becoming a set of overlapping canonical states for one run. Resolved: a transfer **Run Attempt** exposes exactly one primary canonical driver-evidence state, while extra nuance belongs in summary or auxiliary evidence.
- browser-driver evidence was at risk of leaving primary-state precedence ambiguous when unusable session loss and continuity failure were both in play. Resolved: `session_expired` wins as the primary canonical state only when unusable live-session loss is the earlier terminal blocker; once usable session access is recovered, the continuity-specific state `resume_verification_failed` becomes primary if that is what ultimately ends the run.
- browser-driver evidence was at risk of discarding earlier recovered session-loss history once a later continuity-specific state became primary, preserving it only as prose, overexpanding it into a general history model, naming the minimal token inconsistently with the existing artifact contract, placing it at a mismatched payload level, returning empty placeholder nulls when no prior recovered state exists, letting the field generalize into arbitrary prior driver states, emitting it outside the displaced-blocker scenario it was introduced to explain, letting it reach across run boundaries, or leaking it upward into broader read models or durable summaries. Resolved: after usable session recovery, `resume_verification_failed` becomes primary if it is the terminal blocker, but the earlier recovered `session_expired` event remains preserved as minimal structured secondary evidence no richer than a single prior canonical driver-state token, named `prior_session_state`, carried beside `session_state` inside `artifacts`, omitted entirely when absent, restricted in v1 to the recovered-`session_expired` case, emitted only when the terminal primary state is `resume_verification_failed`, confined to an earlier recovered state within the same **Run Attempt**, closed to exactly the value `session_expired`, and kept local to **Run Attempt** transport or artifact evidence rather than copied onto broader read models such as **Transfer Status View** or into durable transcript or memory summaries as another structured field, though those summaries may still describe the recovered session-loss story in natural-language prose when useful.
- wake-packet blocking state was at risk of drifting into narrative prose or packet-specific synonyms while queue state stayed machine-readable. Resolved: `approval_wait` wake packets reuse the same canonical `blocking_reason` tokens as **Work Item** `blocked_reason`, such as `approval_required`, `operator_denied`, and `stale_context`, while human explanation lives in summary or evidence.
- wake-packet blocking state was at risk of splitting into one structured approval vocabulary and separate prose for restart or recovery flows. Resolved: all wake packets that carry blocked-state control data reuse the same canonical `blocking_reason` vocabulary as **Work Item** `blocked_reason`.
- wake packets were at risk of carrying blocked-state reasons even when their task status was `queued`. Resolved: queued wake packets do not carry `blocking_reason`; queued explanation lives in trigger, summary, constraints, and queue timing instead.
- queued wake packets were at risk of spawning a second machine-readable reason taxonomy beside triggers and blocked reasons. Resolved: Odin does not add `queue_reason`; queued wake explanation continues to rely on trigger, summary, constraints, and `next_eligible_at`.
- queued wake packets were at risk of spawning yet another top-level field for structured "why requeued" detail. Resolved: Odin reuses `Evidence.Kind` plus summary or ref for that detail instead of adding a new top-level field.
- packet evidence was at risk of drifting into ad hoc bare strings after becoming part of queued and blocked control semantics. Resolved: wake-packet `Evidence.Kind` uses a constrained canonical namespaced vocabulary such as `runtime.restart`, `runtime.fault`, `tool.snapshot`, and `delegation.created`.
- packet evidence was at risk of letting new family segments proliferate before the live system needed them. Resolved: the v1 `EvidenceKind` family segment is a closed documented set: `runtime`, `tool`, and `delegation`.
- packet evidence was at risk of letting new actions proliferate inside approved families while pretending the registry was closed. Resolved: the v1 `EvidenceKind` action segment is also a closed family-local set.
- packet evidence was at risk of documenting only the current full strings while hiding the family-local structure that governs expansion. Resolved: the canonical `EvidenceKind` registry is documented grouped by family, not just as a flat list.
- packet evidence was at risk of splintering into arbitrary namespaced strings as more producers appeared. Resolved: canonical wake-packet `Evidence.Kind` values come from a closed documented registry rather than an open-ended naming convention.
- packet evidence was at risk of launching with a speculative registry larger than the live system actually needs. Resolved: the v1 `Evidence.Kind` registry starts from the current live producer set and expands only when a new producer exists.
- packet evidence was at risk of leaving the rename from current bare producer kinds to canonical namespaced kinds implicit. Resolved: the v1 mapping is explicitly `restart -> runtime.restart`, `fault -> runtime.fault`, `tool -> tool.snapshot`, and `delegation -> delegation.created`.
- packet evidence was at risk of splitting source of truth between prose and implementation. Resolved: the canonical `Evidence.Kind` registry lives in `docs/contracts/context-compaction.md`, with code and tests mirroring that contract.
- packet evidence was at risk of being documented as canonical while invalid values still slipped into durable state. Resolved: checkpoint compaction rejects unknown `Evidence.Kind` values at write time.
- packet evidence was at risk of coupling runtime behavior to Markdown parsing or leaving the mirror implicit. Resolved: Odin mirrors the canonical `Evidence.Kind` registry in named Go constants, with tests checking alignment to `docs/contracts/context-compaction.md`.
- packet evidence was at risk of remaining an untyped raw string even after becoming a canonical control-plane vocabulary. Resolved: Odin represents wake-packet evidence kinds through a dedicated Go type such as `EvidenceKind`.
- packet evidence was at risk of sprouting deeper namespaced hierarchies before the live system needed them. Resolved: v1 `EvidenceKind` uses a strict two-segment `<family>.<action>` namespace.
- packet evidence was at risk of keeping deprecated bare aliases alive in new durable state during migration. Resolved: new wake-packet writes accept only canonical namespaced `EvidenceKind` values; legacy bare aliases are compatibility concerns for read paths or one-time migration, not new writes.
- packet evidence was at risk of forcing a storage rewrite before historical wake packets could participate in canonical operator surfaces and read models. Resolved: historical bare evidence kinds normalize to canonical namespaced `EvidenceKind` values on read while raw stored payload remains unchanged until a deliberate backfill exists.
- packet evidence was at risk of scattering historical alias normalization across resume logic, projections, and future callers. Resolved: historical `EvidenceKind` alias normalization lives in one shared wake-packet decode path rather than caller-specific logic.
- packet evidence was at risk of leaving the shared decode rule implicit while projections and tests continued to parse wake payloads directly. Resolved: Odin exposes an explicit shared wake-packet decode helper in `internal/runtime/checkpoints`, and wake-packet consumers should use it instead of raw `json.Unmarshal`.
- packet evidence was at risk of growing into a general packet-decoder framework before any packet type besides wake packets actually needed richer compatibility semantics. Resolved: Odin starts with a wake-specific decoder such as `DecodeTaskWakePacket`, while `ProjectContext` and `RunContext` stay on the simple generic unmarshal path until they need more.
- packet evidence was at risk of trusting callers to pass only wake payloads into wake-specific compatibility logic even though the packet table stores multiple scopes. Resolved: `DecodeTaskWakePacket` accepts a full `ContextPacket` and validates `PacketScope == task_wake_packet` before decoding.
- packet evidence was at risk of treating `packet_kind` as meaningless even though current wake writes consistently emit `packet_kind: wake`. Resolved: `DecodeTaskWakePacket` also rejects packets whose `PacketKind` is not `wake`, treating that mismatch as an invalid envelope rather than redundant metadata.
- packet evidence was at risk of surfacing invalid wake envelopes through opaque generic decode failures even after the envelope rules became part of the contract. Resolved: `DecodeTaskWakePacket` returns a typed sentinel such as `ErrInvalidTaskWakePacketEnvelope`, with wrapped detail for scope mismatch, kind mismatch, or malformed payload, so callers can use `errors.Is`.
- packet evidence was at risk of letting operator surfaces render through invalid wake-envelope corruption as if nothing durable were wrong. Resolved: projections and other read models fail fast on `ErrInvalidTaskWakePacketEnvelope` instead of skipping or suppressing invalid wake packets.
- packet evidence was at risk of existing only as transient caller failure even though invalid wake-envelope corruption means durable control-plane state can no longer be trusted. Resolved: `ErrInvalidTaskWakePacketEnvelope` also opens or reuses a task-linked runtime **Incident** so operators have durable attention state, not just a thrown error.
- packet evidence was at risk of letting every failing wake reader mint incidents independently once invalid envelopes became incident-worthy. Resolved: `ErrInvalidTaskWakePacketEnvelope` is converted into an observation that flows through the deterministic diagnosis path, so one shared incident owner opens or reuses the task-linked incident.
- packet evidence was at risk of being misclassified under an unrelated recovery fault family even though invalid wake-envelope corruption is a distinct control-plane failure mode. Resolved: it gets its own explicit recovery `FaultKey`, such as `wake_packet_invalid`.
- packet evidence was at risk of fragmenting one task-level wake corruption problem into many incidents by keying on transient packet IDs. Resolved: `wake_packet_invalid` uses a stable task-scoped `SubjectKey`, such as `task:<task-id>:wake-packet`; packet ID remains diagnostic detail rather than incident identity.
- packet evidence was at risk of understating control-plane corruption as a routine warning even though Odin is refusing to trust durable wake state. Resolved: `wake_packet_invalid` observations explicitly use severity `error` rather than the recovery default `warning`.
- packet evidence was at risk of encouraging speculative auto-repair for authoritative wake-state corruption before a safe deterministic repair contract exists. Resolved: `wake_packet_invalid` has no automatic playbook at first; diagnosis opens or reuses the incident and stops at human attention required.
- packet evidence was at risk of forcing incident-worthy no-playbook faults back into a playbook-only diagnosis model that could represent only "playbook" or "ignore". Resolved: recovery `Decision` now carries an explicit deterministic outcome mode such as `ignore`, `incident_only`, `playbook`, and `escalate`.
- packet evidence was at risk of expressing `wake_packet_invalid` through a fake no-op playbook or an overloaded `escalate` path even after diagnosis gained explicit outcomes. Resolved: `wake_packet_invalid` uses the explicit diagnosis outcome `incident_only`.
- packet evidence was at risk of polluting the `recoveries` audit trail with fake rows for faults that deliberately stop at human attention required. Resolved: `incident_only` opens or reuses the incident and stops without creating a **Recovery** row.
- packet evidence was at risk of making callers infer "no recovery attempt" from a zero-value **Recovery** record even though `incident_only` is a valid executor result. Resolved: recovery executor `Outcome` explicitly allows no recovery attempt for `incident_only`.
- packet evidence was at risk of making non-playbook incident handling look like successful automated repair by reusing the generic status `completed`. Resolved: `incident_only` gets its own explicit executor `Outcome.Status`, while `completed` remains reserved for successful deterministic recovery attempts.
- packet evidence was at risk of leaving operator-visible recovery logs and audit fields playbook-centric even after diagnosis and execution started supporting non-playbook outcomes. Resolved: recovery service logging now uses generic decision language, and `playbook` becomes optional detail rather than the universal shape.
- packet evidence was at risk of keeping the core diagnosis object playbook-shaped even after the domain model started supporting `ignore`, `incident_only`, `playbook`, and `escalate` as first-class deterministic outcomes. Resolved: recovery `Decision` now carries an explicit typed mode, and `Playbook` is optional detail valid only for `playbook` decisions.
- packet evidence was at risk of making the new `ignore` mode theoretical only, with ignored observations still disappearing from the diagnosis result instead of being recorded as explicit deterministic choices. Resolved: diagnosis emits explicit `Decision{mode: ignore}` records rather than dropping ignored observations from the decision stream.
- packet evidence was at risk of inventing executor-shaped no-op outcomes for `ignore` even though `ignore` performs no bounded runtime work. Resolved: `ignore` stops at diagnosis and is logged there rather than flowing through the executor.
- packet evidence was at risk of turning `CycleResult.Outcomes` into a mixed diagnosis-and-execution bag once `ignore` became explicit. Resolved: `Outcomes` remains executor-only, so `ignore` stays visible through `Decisions` plus diagnosis logging rather than as an outcome row.
- packet evidence was at risk of treating `incident_only` like a diagnosis-only branch even though it still performs executor-owned incident management. Resolved: `incident_only` remains present in `CycleResult.Outcomes`, but without a recovery attempt attached.
- packet evidence was at risk of overloading `Outcome.Attempt` into a generic executor action counter once `incident_only` entered the outcome stream. Resolved: `Attempt` remains a recovery-attempt count and stays unset for `incident_only`.
- packet evidence was at risk of duplicating outcome meaning across `Status` and legacy booleans like `Suppressed` and `Escalated` just as the outcome model was expanding. Resolved: recovery executor `Outcome` relies on explicit `Status` values alone.
- packet evidence was at risk of leaving recovery executor results on unchecked raw string statuses even after the outcome vocabulary became canonical. Resolved: `Outcome.Status` now uses its own typed canonical vocabulary such as `incident_only`, `completed`, `failed`, `suppressed`, and `escalated`.
- packet evidence was at risk of collapsing playbook-local action results and executor-level outcomes into one shared status type even though the two layers now have different semantics. Resolved: `ActionResult.Status` stays its own small typed vocabulary, and the executor maps it into canonical `Outcome.Status`.
- packet evidence was at risk of letting playbook actions emit executor-only statuses once action results became typed. Resolved: the v1 `ActionResult.Status` set is closed to exactly `completed`, `failed`, and `escalated`; statuses such as `suppressed` and `incident_only` remain executor-only.
- packet evidence was at risk of silently treating invalid playbook output as an ordinary failure outcome once the action-result vocabulary became closed. Resolved: the executor rejects unknown `ActionResult.Status` values as contract errors rather than coercing them into the generic failure branch.
- packet evidence was at risk of surfacing invalid playbook output through vague generic executor errors even after the action-result vocabulary became closed. Resolved: invalid action-result statuses surface through a typed sentinel such as `ErrInvalidActionResultStatus`.
- packet evidence was at risk of understating invalid playbook output as an ordinary open incident once a durable recovery attempt had already started. Resolved: contract-invalid action-result status seals the recovery attempt as `failed`, returns `ErrInvalidActionResultStatus`, and escalates the linked incident with explicit contract-violation details.
- packet evidence was at risk of either dropping auditability for invalid playbook output or polluting `recovery.action_executed` with non-canonical result values. Resolved: Odin still appends `recovery.action_executed`, but records canonical result `failed` plus explicit contract-violation details instead of the invalid raw status.
- packet evidence was at risk of either discarding the invalid raw playbook status entirely or promoting it into canonical status fields. Resolved: Odin preserves the raw invalid status only in non-canonical diagnostic details for forensics, while canonical status and event result fields stay normalized to `failed`.
- packet evidence was at risk of duplicating playbook-specific malformed-output forensics into **Incident** details even though incident details currently stay summary-level and partly support incident reuse identity. Resolved: the raw invalid playbook status lives only in **Recovery** diagnostic details, while **Incident** details carry summary-level contract-violation reason or category only.
- packet evidence was at risk of making `recovery.action_executed` carry contract-violation context only as prose even though the recovery event stream is supposed to provide explicit action detail. Resolved: `recovery.action_executed` gains an optional structured diagnostic-details field rather than overloading free-text `Description`.
- packet evidence was at risk of importing generic `details_json` blobs into the runtime event contract just to carry structured recovery-action diagnostics. Resolved: the new `recovery.action_executed` diagnostic-details field stays a small typed optional event sub-structure, consistent with Odin’s typed runtime-event payload style.
- packet evidence was at risk of carrying malformed playbook output in events only as a raw bad value plus prose, which would force downstream consumers back into string matching. Resolved: `recovery.action_executed.contract_violation` carries a canonical machine-readable key plus the raw offending value needed for forensics.
- packet evidence was at risk of leaving the new contract-violation key as an open string even though adjacent Odin domains use small typed registries for the same purpose. Resolved: `recovery.action_executed.contract_violation.key` is a closed typed registry, with v1 starting at exactly `invalid_action_result_status`.
- packet evidence was at risk of mixing namespaced and non-namespaced conventions inside one runtime domain by naming the nested violation key `recovery.invalid_action_result_status` even though adjacent inner registries stay bare snake_case. Resolved: the v1 nested key stays the bare token `invalid_action_result_status`, while namespacing remains at the event-type layer.
- packet evidence was at risk of widening the raw offending-value field into a generic catch-all before any second contract-violation shape existed. Resolved: the v1 payload uses the specific field name `raw_status`, matching the only live violation key and the actual malformed source field `ActionResult.Status`.
- packet evidence was at risk of adding a redundant `source_field` even though the closed v1 violation key already identifies the malformed source unambiguously. Resolved: v1 omits `source_field` and lets `invalid_action_result_status` imply the source is `ActionResult.Status`.
- packet evidence was at risk of duplicating the already-closed `ActionResult.Status` vocabulary into each violation event through `allowed_statuses` or `expected_statuses`. Resolved: v1 omits those fields and relies on the canonical central status registry instead.
- packet evidence was at risk of making malformed-status forensics optional even though the executor already materializes a concrete `ActionResult.Status` before recording `recovery.action_executed`. Resolved: `raw_status` is required whenever the violation key is `invalid_action_result_status`.
- wake packets were at risk of overloading `trigger` with queue-outcome meanings. Resolved: `trigger` remains the packet-creation cause, while queue-outcome semantics stay out of the trigger vocabulary.
- operator denial was at risk of leaving the rejected resume path active. Resolved: denying an **Approval Request** seals the linked `approval_wait` wake packet with a denied or stale reason.
- denial was at risk of being treated as a reversible pause on the same approval cycle. Resolved: later replanning that still needs approval starts a brand-new **Approval Request** and brand-new `approval_wait` wake packet, leaving the denied cycle as terminal history.
- denial was at risk of breaking governance lineage across replanning. Resolved: a post-denial **Approval Request** explicitly references the denied approval it follows.
- material-change retriage was at risk of leaving approval replacement implicit and reconstructable only by timestamps. Resolved: a replacement **Approval Request** explicitly references the expired approval it supersedes.
- material-change retriage was at risk of leaving stale approval resume state active after approval expiry. Resolved: expiring a pending **Approval Request** also supersedes the old `approval_wait` wake packet and writes a fresh packet for the new approval context.
- approval-support filters were at risk of being treated as batch approval controls. Resolved: `supported` and `unsupported` filters are **Operator Surface** inspection filters only; every mutation still targets one explicit **Approval Request** and must pass workflow-owned resolver support.
- "run" was being treated like the main managed object. Resolved: the durable control-plane object is the **Work Item** and execution happens through one or more **Run Attempts**.
- "pause", "resume", "abort", and "reset" were drifting toward process control language. Resolved: operator controls act on **Work Items**, while **Run Attempts** remain execution history and outcomes.
- "cron" or event hooks were drifting toward direct worker launching. Resolved: schedules and event hooks are **Automation Triggers** that materialize governed **Work Items** first.
- the frontend was drifting toward a run-monitor-first shape. Resolved: primary navigation is **Workspace -> Initiative -> Work Item**, with **Run Attempts** shown as nested execution history.
- work-item detail fields were drifting toward software-execution defaults. Resolved: default detail is business-first, with repo/backend/session fields shown only when relevant.
- approval was at risk of being reduced to a status field. Resolved: approval is a first-class **Approval Request** with its own lifecycle, audit trail, and links to **Work Item** and optional **Run Attempt** context.
- "resume" after approval was at risk of sounding like live session continuation. Resolved: approval returns the **Work Item** to `queued` and resumes through a fresh **Run Attempt** built from persisted wake state.
- status labels were at risk of collapsing into one mixed dashboard vocabulary. Resolved: **Work Item**, **Run Attempt**, and **Approval Request** each own separate operator-visible statuses.
- "social media manager" was being used as shorthand for Marcus's governed social lane. Resolved: the canonical term is **Social Copilot**, carried by the existing workflow, memory, and tool surfaces rather than a new first-class manager object.
- The Social Copilot loop was at risk of splitting between the documented `/workflow social` lane and a new top-level social command. Resolved: `/workflow social` is the canonical recovery target because it is the workflow-scoped **Operator Surface** for this loop; a top-level alias may exist later only as a thin adapter over the same runtime service and persisted state.
- The current checkout may advertise workflow, skill, or social shell examples that are not fully wired. Resolved: missing `/workflow social` shell plumbing is implementation drift to repair against the existing Social Copilot runtime service, not evidence for a new manager, queue, registry, or non-shell operator path.
- "reply" was overloaded between research output and live publishing. Resolved: the research output is a **Reply Suggestion**, and the current live publish surface does not publish replies.
- "up and running for Marcus" was ambiguous between proof-safe validation and real account readiness. Resolved: fixture-backed real `odin` proof is required first, but the target state is Marcus-live operator readiness on the actual X account.
- "playbook", "runbook", and "surface" were drifting together. Resolved: the canonical Odin term for the human-facing path is **Operator Surface**; `playbook` stays reserved for recovery semantics, while a runbook is only documentation for using an operator surface.
- Marcus's first guided social **Operator Surface** was ambiguous between a docs-only layer and a new wrapper command. Resolved: start with a runbook over the existing `./bin/odin` shell path and only add a new command surface later if real operator use proves that the existing path is insufficient.
- Marcus's first runbook scope was ambiguous between the full weekly workflow and the smallest live publish loop. Resolved: start with the narrow X-post loop of draft, approve, publish, and evidence over the existing shell path before documenting the broader weekly workflow.
- the first Marcus-live runbook had an unresolved ownership split for approval and publish. Resolved: Marcus himself executes the approval and publish steps inside Odin so approval state, publish action, and evidence remain on one canonical operator surface.
- Marcus's first live X-post runbook had two possible publish-entry paths. Resolved: the canonical path is `social_draft -> /memory resolve -> /memory publish`, while direct `social_outcome` entry remains an exception outside the first live runbook.
- Marcus's first live X-post runbook had two possible evidence bars after publish. Resolved: require the separate visible-page capture so publish proof lands in canonical `social_evidence`; publish-time screenshot fields on `social_outcome` remain supporting artifacts, not the runbook's only evidence step.
- Marcus's first live X-post runbook had two possible draft-entry styles. Resolved: start from `/skill use marcus-x-drafting-assistant` so Odin generates and auto-records the pending `social_draft`; manual `/memory remember social_draft ...` remains an exception outside the first live runbook.
- Marcus's first live X-post runbook had an unresolved drafting breadth choice. Resolved: ask for one primary X draft in the first live runbook, with alternates only when Marcus explicitly requests them.
- Marcus's first live X-post runbook had an unresolved memory-ID safety rule. Resolved: require explicit `/memory list` and `/memory show` confirmation of the exact draft and outcome IDs before `/memory resolve` and `/memory publish`; do not assume the newest records are always correct.
- Marcus's first live X-post runbook had an unresolved preflight readiness rule. Resolved: require split preflight with `odin doctor` for general runtime health plus explicit verification that `ODIN_HUGINN_VISUAL_DRIVER`, `ODIN_HUGINN_X_PUBLISH_DRIVER`, and `ODIN_HUGINN_X_POST_DRIVER` are all configured before entering the live post loop.
- Marcus's first live X-post runbook had an unresolved browser-session timing rule. Resolved: require a safe X compose-surface or browser-session preflight before drafting so session failures surface before Marcus spends time drafting and approving a post.
- Marcus's first live X-post runbook had no dedicated read-only X compose preflight tool. Resolved: reuse the existing generic `browser_visual_audit` operator surface against `https://x.com/compose/post` for the first runbook, while keeping `huginn_visual_audit` accepted only as a hidden compatibility alias during transition and treating any future X-specific compose check as a possible later improvement rather than a blocker now.
- Marcus's first live X-post runbook had an unresolved compose-preflight pass rule. Resolved: run a **headed** `browser_visual_audit` against `https://x.com/compose/post` and require Marcus to manually confirm the returned `final_url` and screenshot before entering the draft loop, because the reused audit tool is generic rather than X-specific.
- Marcus's first live X-post runbook had an unresolved compose-preflight failure rule. Resolved: if the headed compose preflight does not show the correct logged-in X compose page, abort the live loop, repair the X session manually outside the runbook, and restart from preflight rather than pretending the current Odin surface can recover in-place.
- Marcus's first live X-post runbook had an unresolved documentation home. Resolved: keep durable social semantics in `docs/contracts/marcus-social-copilot.md` and place the step-by-step Marcus live operator sequence in a dedicated `docs/operations/` runbook.
- Marcus's first live X-post runbook had an unresolved document shape. Resolved: write it as a strict numbered procedure with exact commands, stop conditions, and restart points rather than as a loose checklist.
- Marcus's first live X-post runbook had an unresolved native-publish failure restart rule. Resolved: if `/memory publish ... via=huginn_x` fails before `publish_status=published`, rerun preflight and resume from the same approved `social_outcome` instead of forcing a new draft or approval cycle.
- Marcus's first live X-post runbook had an unresolved visible-evidence failure restart rule. Resolved: if native publish succeeds but visible-evidence capture fails, resume from the evidence step using the same published `publish_url` rather than rerunning publish.
- Marcus's first live X-post runbook had an unresolved post-evidence completion proof rule. Resolved: successful tool output is not enough; the runbook requires explicit confirmation that the new `social_evidence` memory exists in Odin before the loop is considered complete.
- Marcus's first live X-post runbook had an unresolved final completion rule. Resolved: the loop is complete only when both the `social_outcome` is visibly marked published and the corresponding `social_evidence` memory is visibly recorded in Odin.
- Marcus's first live X-post runbook had an unresolved binary and working-directory entrypoint. Resolved: standardize the live runbook on `cd /home/orchestrator/odin-os && ./bin/odin` rather than assuming a PATH-installed `odin` binary.
- Marcus's first live X-post runbook had an unresolved runtime-root policy. Resolved: require an explicit dedicated `ODIN_ROOT` for the live runbook instead of relying on the repo-default local-development runtime layout.
- Marcus's first live X-post runbook had an unresolved runtime-config loading style. Resolved: require the persistent machine-local env file `~/.config/odin/odin-os.env` to hold the dedicated `ODIN_ROOT` and X driver env vars instead of relying on per-session manual exports.
- Marcus's first live X-post runbook had an unresolved interactive shell env-loading rule. Resolved: require an explicit shell bootstrap step that loads the machine-local `~/.config/odin/odin-os.env` before launching `./bin/odin` rather than assuming Marcus's login shell already exports the required runtime and driver vars.
- Marcus's first live X-post runbook had an unresolved driver-config scope inside the machine-local env file. Resolved: require that `~/.config/odin/odin-os.env` define all three live-loop driver vars `ODIN_HUGINN_VISUAL_DRIVER`, `ODIN_HUGINN_X_PUBLISH_DRIVER`, and `ODIN_HUGINN_X_POST_DRIVER` so compose preflight, native publish, and visible evidence share one canonical config surface.
- Marcus's first live X-post runbook had an unresolved interactive bootstrap command shape. Resolved: keep `~/.config/odin/odin-os.env` as a plain assignment file for systemd compatibility and require an explicit shell bootstrap such as `set -a; . ~/.config/odin/odin-os.env; set +a` before launching `./bin/odin`.
- Marcus's first live X-post runbook had an unresolved post-bootstrap shell-state proof rule. Resolved: require an explicit shell-side confirmation step after bootstrap that checks `ODIN_ROOT`, `ODIN_HUGINN_VISUAL_DRIVER`, `ODIN_HUGINN_X_PUBLISH_DRIVER`, and `ODIN_HUGINN_X_POST_DRIVER` before launching `./bin/odin`, rather than relying on later Odin behavior to reveal misconfiguration.
- Marcus's first live X-post runbook had an unresolved machine-local env-file path convention. Resolved: standardize the runbook on the canonical deployment env path `~/.config/odin/odin-os.env`; the older `~/.config/odin/odin.env` path belongs only to legacy `odin.service` compatibility.
- Marcus's first live X-post runbook had an unresolved driver-path proof rule inside the post-bootstrap shell check. Resolved: require the shell-side confirmation step to verify that `ODIN_HUGINN_VISUAL_DRIVER`, `ODIN_HUGINN_X_PUBLISH_DRIVER`, and `ODIN_HUGINN_X_POST_DRIVER` each point to an existing executable command, not merely that the variables are non-empty.
- Marcus's first live X-post runbook had an unresolved dependency on long-running service mode. Resolved: keep the first live loop on the existing repo-local interactive shell path and do not require `odin serve`; service mode remains a separate runtime surface, while the Marcus social operator examples and verification model already treat scripted or interactive `odin` shell behavior as a complete real command path.
- Marcus's first live X-post runbook had an unresolved location for the `doctor` readiness check. Resolved: require `cd /home/orchestrator/odin-os && ./bin/odin doctor --json` as a top-level preflight command before launching the interactive shell, rather than using `/doctor json` inside the REPL session.
- Marcus's first live X-post runbook had an unresolved machine-oriented readiness gate. Resolved: require both `cd /home/orchestrator/odin-os && ./bin/odin healthcheck` and `cd /home/orchestrator/odin-os && ./bin/odin doctor --json` before launching the interactive shell, so the runbook gets both the binary ready/not-ready gate and the structured readiness detail.
- Marcus's first live X-post runbook had an unresolved authored-asset readiness gate inside the interactive shell. Resolved: require explicit `/workflow validate marcus-social-growth-workflow` and `/skill validate marcus-x-drafting-assistant` before `/workflow use` and `/skill use`, so authored asset failures stop at the narrowest boundary instead of surfacing later in the live loop.
- Marcus's first live X-post runbook had an unresolved ordering between workflow selection and drafting-skill selection. Resolved: require `/workflow use marcus-social-growth-workflow` before `/skill use marcus-x-drafting-assistant`, because the governed draft path is stronger when the workflow is explicitly selected before the drafting skill records workflow-scoped `social_draft` memory.
- Marcus's first live X-post runbook had an unresolved startup rule for persisted workflow and skill selections. Resolved: require `/workflow clear` and `/skill clear` immediately after entering the interactive shell so the first live loop starts from a visibly neutral selection state before Marcus validates and selects the canonical social workflow and drafting skill.
- Marcus's first live X-post runbook had an unresolved proof rule for neutral shell state after clearing persisted selections. Resolved: require explicit `/workflow` and `/skill` inspection after `/workflow clear` and `/skill clear`, so Marcus sees `current=none` on both selection surfaces before re-establishing the canonical social workflow and drafting skill.
- Marcus's first live X-post runbook had an unresolved proof rule for selected workflow and skill state after canonical selection. Resolved: require explicit `/workflow` and `/skill` inspection after `/workflow use marcus-social-growth-workflow` and `/skill use marcus-x-drafting-assistant`, so Marcus sees the current selected workflow and skill on the shell’s readback surfaces rather than relying only on transient `status=selected` output.
- Marcus's first live X-post runbook had an unresolved startup rule for persisted project and mode state. Resolved: require `/scope global` immediately after entering the interactive shell so the first live loop starts from a safe global ask-mode baseline instead of inheriting restored project-scoped or act-mode session state.
- Marcus's first live X-post runbook had an unresolved proof rule for global ask-mode shell state after `/scope global`. Resolved: require explicit `/project` and `/mode` inspection after `/scope global`, so Marcus sees `current=none` for project state and `mode=ask` on the shell’s readback surfaces instead of inferring those facts only from the `/scope global` output.
- Marcus's first live X-post runbook had an unresolved shape for the drafting ask itself. Resolved: require a short drafting prompt template that explicitly includes topic, intended audience, real lesson or opinion, required facts, and style constraints, while still sending that request through the existing natural-language ask surface after `/skill use marcus-x-drafting-assistant`.
- Marcus's first live X-post runbook had an unresolved review surface immediately after drafting. Resolved: require `/memory list type=social_draft field.approval=pending` followed by `/memory show <id>` immediately after the drafting ask, so Marcus reviews the persisted workflow-scoped `social_draft` record rather than relying only on the assistant's transient text output.
- Marcus's first live X-post runbook had an unresolved rejection path when Marcus does not approve the persisted draft. Resolved: require `/memory resolve <id> result=rejected reason=<short-reason>` and then restart from a fresh drafting ask, rather than allowing ad hoc manual editing of the draft before approval.
- Marcus's first live X-post runbook had an unresolved format for draft-rejection reasons. Resolved: require short kebab-case rejection tags such as `too-generic` or `too-defensive` in `reason=<...>`, so rejected-draft history stays compact and queryable on the existing `social_outcome` surface.
- Marcus's first live X-post runbook had an unresolved readback rule after `/memory resolve <draft-id> result=approved`. Resolved: require `/memory list type=social_outcome field.result=approved` followed by `/memory show <outcome-id>` immediately after approval, so Marcus explicitly inspects the durable approved `social_outcome` object that the publish step will act on rather than relying only on transient `outcome_memory=<id>` resolve output.
- Marcus's first live X-post runbook had an unresolved filter shape for the approved-outcome readback step before publish. Resolved: require `/memory list type=social_outcome field.result=approved field.channel=x field.content_kind=post` before `/memory show <outcome-id>`, so the first live X-post loop does not make Marcus sift through unrelated approved outcomes such as LinkedIn posts or non-post content kinds.
- Marcus's first live X-post runbook had an unresolved readback rule after `/memory publish ... via=huginn_x`. Resolved: require `/memory list type=social_outcome field.publish_status=published field.publish_mode=huginn_x field.channel=x field.content_kind=post` followed by `/memory show <outcome-id>` immediately after native publish, so Marcus explicitly inspects the stored publish fields on the durable `social_outcome` before moving to visible evidence capture.
- Marcus's first live X-post runbook had an unresolved source-of-truth rule for the visible-evidence `target_url`. Resolved: require the visible-evidence step to take `target_url` from the `publish_url` field on the just-read published `social_outcome`, rather than allowing an out-of-band pasted URL that could drift from the durable Odin publish record.
- Marcus's first live X-post runbook had an unresolved label rule for visible-evidence capture. Resolved: require `label=social-outcome-<outcome-id>` on `/tool run browser_x_post_visible_evidence ...` so the visible-evidence artifact label matches the deterministic native-publish label shape already used for the same approved `social_outcome`, instead of falling back to generic or freeform labels.
- Marcus's first live X-post runbook had an unresolved filter shape for the `social_evidence` completion-proof readback. Resolved: require `/memory list type=social_evidence field.evidence_kind=x_post_visible field.label=social-outcome-<outcome-id> order=desc limit=1` before `/memory show <evidence-id>`, so Marcus confirms the newest matching visible-evidence memory row for the exact published outcome rather than any older X evidence in the same workflow scope.
- Marcus's first live X-post runbook had an unresolved field-level proof rule on `/memory show <evidence-id>`. Resolved: require the shown `social_evidence` record to confirm `label=social-outcome-<outcome-id>`, `target_url=<publish_url>`, `final_url=<publish_url>`, and non-empty `screenshot_path` plus `snapshot_path`, while leaving author metadata and engagement counts as supporting context rather than completion gates.
- Marcus's first live X-post runbook had an unresolved text-capture proof rule on `/memory show <evidence-id>`. Resolved: require non-empty `snapshot_excerpt` on the shown `social_evidence` record in addition to the stored artifact paths, because `snapshot_path` is assigned up front but `snapshot_excerpt` is the durable sign that readable page text was actually captured.
- Marcus's first live X-post runbook had an unresolved role for DOM-specific extracted `post_text` on `/memory show <evidence-id>`. Resolved: keep `post_text` as supporting context rather than a completion gate, because `snapshot_excerpt` already proves readable content capture while `post_text` depends on a narrower X-specific selector path.
- Marcus's first live X-post runbook had an unresolved time-proof surface after publish and evidence capture. Resolved: rely on `published_at` from the published `social_outcome` as the required time proof, and do not add the driver's X-page `timestamp` to the first live runbook because it is not persisted on `social_evidence` today and would create a second time-proof surface.
- Marcus's first live X-post runbook had an unresolved boundary between Odin-visible proof and shell-side artifact file checks. Resolved: keep completion proof entirely on the canonical Odin operator surface and do not require shell-side `test -f` checks on `screenshot_path` or `snapshot_path` in the first live runbook, because the current shell already records durable `social_outcome` and `social_evidence` state but does not expose an Odin-native artifact-existence command.
- Marcus's first live X-post runbook had an unresolved retry rule for duplicate-label visible-evidence rows. Resolved: keep the deterministic label `social-outcome-<outcome-id>` across evidence retries and require `/memory list type=social_evidence field.evidence_kind=x_post_visible field.label=social-outcome-<outcome-id> order=desc limit=1`, so the newest matching `social_evidence` row is canonical without inventing retry-versioned labels.
- Marcus's first live X-post runbook had an unresolved choice between transient `tool_memory=<id>` output and durable memory readback after visible-evidence capture. Resolved: treat `tool_memory=<id>` as supporting output only and still require the standard `/memory list ... order=desc limit=1` plus `/memory show <evidence-id>` readback, so drafts, outcomes, and evidence all use the same durable Odin proof surface even when tool output prints a recorded memory ID.
- Marcus's first live X-post runbook had an unresolved failure boundary when visible-evidence tool output looks successful but the required durable `social_evidence` readback still does not produce the expected row or fields. Resolved: stop and treat that as an Odin defect on the canonical operator surface, leave the already published `social_outcome` intact, and do not rerun publish or invent a second recovery path inside the first live runbook.
- Marcus's first live X-post runbook had an unresolved shell boundary after one successful post. Resolved: require explicit `/workflow clear`, `/skill clear`, `/scope global`, and `exit` after a successful loop, so the single-post boundary is enforced on the same persisted shell state that the next run would otherwise inherit.
- Marcus's first live X-post runbook had an unresolved shell boundary for branches that restart from top-level preflight. Resolved: require exit and a fresh shell session for any branch that says "restart from preflight," so compose-preflight failures and native-publish failures before durable published state do not continue inside the same persisted REPL context that the restart is meant to invalidate.
- Marcus's first live X-post runbook had an unresolved shell boundary for the visible-evidence retry branch after successful native publish. Resolved: stay in the same REPL and resume the evidence step there using the same published `publish_url`, because that branch is intentionally narrower than a preflight restart and the selected workflow already provides the scope needed to record the replacement `social_evidence` row.
- Marcus's first live X-post runbook had an unresolved URL-identity rule for evidence proof fields. Resolved: require exact raw-string equality when proving `target_url=<publish_url>` and `final_url=<publish_url>` on the shown `social_evidence` record, because this first-live proof path stores raw URLs and does not yet define a broader normalized same-post equivalence rule.
- Marcus's first live X-post runbook had an unresolved field-level proof rule on `/memory show <outcome-id>` after native publish. Resolved: require the shown published `social_outcome` to confirm `publish_status=published`, `publish_mode=huginn_x`, exact `publish_url`, non-empty `published_at`, and non-empty `publish_screenshot_path` before Marcus moves on to visible-evidence capture, because those fields already exist on the canonical published object and the evidence step should not begin until that object itself is explicitly proven.
- Marcus's first live X-post runbook had an unresolved identity rule between the approved-outcome step and the published-outcome step. Resolved: require the same `outcome-id` selected from the approved `social_outcome` readback to remain canonical through `/memory publish`, because native X publish updates that exact durable outcome row in place rather than minting a second published outcome.
- Marcus's first live X-post runbook had an unresolved failure boundary when `/memory publish ... via=huginn_x` appears to succeed but the required durable published-outcome readback on that same `outcome-id` does not prove the expected fields. Resolved: stop and treat that as an Odin defect without rerunning publish, because a second publish attempt could create a duplicate live X post and the current shell does not define retry-safe native-publish semantics after ambiguous success.
- Marcus's first live X-post runbook had an unresolved question about whether the published-outcome `/memory list ...` step still mattered once same-ID continuity through publish was locked. Resolved: keep the `/memory list type=social_outcome field.publish_status=published field.publish_mode=huginn_x field.channel=x field.content_kind=post` step before `/memory show <same outcome-id>`, because it proves discoverability on the same durable filter surface the rest of the runbook uses while `/memory show` proves the field-level contents of that exact row.
- Marcus's first live X-post runbook had an unresolved pass condition for the published-outcome `/memory list ...` step. Resolved: require the filtered published-outcome list output to visibly include the same canonical `outcome-id` before `/memory show <same outcome-id>`, because the list step is meant to prove discoverability of that exact durable row rather than the existence of any published row.
- Marcus's first live X-post runbook had an unresolved choice between a broader visible-evidence list query on the first try and a narrower `order=desc limit=1` query only after known retries. Resolved: always require `/memory list type=social_evidence field.evidence_kind=x_post_visible field.label=social-outcome-<outcome-id> order=desc limit=1` before `/memory show <evidence-id>`, because the shell already supports one query shape that is safe for both the first capture and later duplicate-label retries.
- Marcus's first live X-post runbook had an unresolved source for the `evidence-id` passed to `/memory show <evidence-id>` after the canonical visible-evidence list query. Resolved: require the `memory=<id>` returned by `/memory list type=social_evidence field.evidence_kind=x_post_visible field.label=social-outcome-<outcome-id> order=desc limit=1` to become the canonical `evidence-id` for `/memory show`, because the list step and the show step must chain to the same durable row and `tool_memory=<id>` remains supporting output only.
- Marcus's first live X-post runbook had an unresolved scope contradiction between the locked global social operator surface and the current availability of `browser_visual_audit`. Resolved: extend `browser_visual_audit` to `global` scope so the compose-preflight step stays on the same canonical global/workflow social surface instead of forcing Marcus through a temporary project or `odin-core` scope switch just to run preflight.
