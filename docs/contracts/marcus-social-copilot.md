---
title: Marcus Social Copilot Contract
status: active
date: 2026-04-18
phase: "18"
---

# Marcus Social Copilot Contract

## 1. Current-state assumptions

### Existing state found

- `odin-os` already has canonical authored asset locations under `registry/`, `memory/`, and `docs/contracts/`.
- The live registry supports `agent`, `skill`, `workflow`, and `command` assets. There is no first-class runtime `initiative` or `companion` kind yet.
- The live CLI already supports global ask-mode drafting through `/skill use <key>` and operator-visible verification through the real `odin` command path.
- Runtime memory services already exist for users, projects, runs, and knowledge, and authored durable memory docs already have a canonical home under `memory/`.
- Project governance, approval gates, and verification discipline are already part of Odin OS.
- `docs/contracts/workspace-context-map.md` and `docs/plans/2026-04-16-odin-operating-model.md` already define `initiative`, `companion`, `policy`, and `memory` as the target operating vocabulary.
- `docs/migration/legacy-inventory.md` shows legacy social writing skills such as `social-media-writer`, `marketing-copywriter`, `email-marketer`, and `humanized-writer` as rewrite candidates only. They are not live runtime assets in `odin-os`.

### Gaps

- There is no first-class persisted `initiative` or `companion` domain model yet.
- There is no social-specific CLI command or compliant publishing integration yet.
- There is no generalized platform analytics ingestion path yet. The live evidence path is limited to read-only visible-page capture for explicit X post URLs.
- `config/policies.yaml` is still placeholder-only, so shared social policy must currently live in canonical docs, registry assets, and approval gates rather than a separate active policy engine.
- The current live workflow is ask-mode oriented, with workflow-scoped draft queueing and queue resolution available through the generic `/memory` operator surface rather than a first-class native approval object for non-project social work.

### Working assumptions

- Marcus approves every post and reply before publishing.
- The first release is text-only and human-in-the-loop.
- Publishing happens through native X and LinkedIn surfaces, official APIs, or native scheduling tools when available.
- Analytics can begin with manually entered metrics or exported platform data.
- The system must never use stealth automation, deceptive impersonation, or platform-evasive workflows.

## 2. Target operating model inside Odin OS

Odin should treat Marcus's social growth as one governed initiative executed through durable companion role contracts, approval-gated workflows, and explicit memory boundaries.

Because first-class initiative and companion persistence is not live yet, the current implementation maps the model onto existing Odin primitives:

- Initiative definition lives in this contract and durable memory docs.
- Companion definitions live as registry `agent` role contracts.
- Companion working behaviors live as registry `skill` assets that Marcus can select in the real CLI.
- The cross-companion operating sequence lives as a registry `workflow`.
- Approval and compliance rules live in this contract, the skills, the companion constraints, and `AGENTS.md`.

### Production-ready now

- content planning
- X draft generation
- LinkedIn draft generation
- reply suggestion and engagement research
- read-only visible-page evidence capture for explicit X post URLs through Huginn
- manual approval gates
- workflow-scoped approval queue resolution and retrospective guidance

### Future only

- first-class initiative and companion persistence in SQLite
- native social approval queues and publish records
- official API publishing or native scheduling integration
- automated analytics ingestion
- media-rich content production workflows

## 3. Initiative definition

**Initiative key:** `marcus-aviation-authority-growth`

**Title:** Marcus Aviation Authority Growth

**Mission:** Help Marcus consistently publish useful, credible aviation and flight-training content on X and LinkedIn that compounds trust, professional authority, and high-quality audience engagement.

**Primary audience:** pilots in training, flight instructors, pilots pursuing airline progression, and aviation professionals who value practical teaching.

**Channel focus:** X for concise insight and timely engagement, LinkedIn for deeper professional positioning and applied teaching lessons.

**Success definition:** Marcus publishes consistently, approves more good drafts faster, replies with better judgment, sees better engagement from the right audience, and learns what topics and formats actually work without violating platform rules.

**Non-goals:** fake virality, aggressive follower-gaming, autoposting without review, manufactured controversy, or persona inflation.

## 4. Companion definitions

### Content Strategist Advisor

Role: choose weekly topic mix, sharpen hooks, sequence ideas across X and LinkedIn, and keep the calendar aligned to Marcus's real expertise.

Primary outputs: weekly content plan, topic prioritization, draft briefs, and pillar balance adjustments.

Current implementation: `registry/agents/marcus-social-content-strategist-companion.md` and `registry/skills/marcus-social-content-strategist.md`.

### X Drafting Assistant

Role: turn one clear insight into concise, high-signal X drafts that sound like a competent aviation peer rather than a generic AI copywriter.

Primary outputs: text-only posts, thread seeds, concise variants, and approval notes.

Current implementation: `registry/agents/marcus-x-drafting-assistant-companion.md` and `registry/skills/marcus-x-drafting-assistant.md`.

### LinkedIn Drafting Assistant

Role: expand ideas into stronger professional posts with more context, clearer teaching value, and cleaner narrative structure.

Primary outputs: LinkedIn post drafts, article seeds, and audience-fit revisions.

Current implementation: `registry/agents/marcus-linkedin-drafting-assistant-companion.md` and `registry/skills/marcus-linkedin-drafting-assistant.md`.

### Engagement Research Assistant

Role: review recent conversations, identify compliant reply opportunities, and suggest useful responses or no-reply decisions.

Primary outputs: reply suggestions, engagement plans, sensitivity flags, and no-go recommendations.

Current implementation: `registry/agents/marcus-engagement-research-assistant-companion.md` and `registry/skills/marcus-engagement-research-assistant.md`.

### Analytics and Retrospective Advisor

Role: review publishing history and engagement results, isolate what worked, and feed those learnings back into next week's planning.

Primary outputs: weekly retrospective, pillar performance summary, pacing adjustments, and experiment recommendations.

Current implementation: `registry/agents/marcus-social-analytics-advisor-companion.md` and `registry/skills/marcus-social-analytics-advisor.md`.

## 5. Policy definitions

### Approval before publishing

- Every post draft requires Marcus's explicit approval before publishing.
- Every reply draft requires Marcus's explicit approval before posting.
- Odin may prepare, revise, and queue drafts, but it must not silently publish them.

### Approval before sensitive replies

- Sensitive topics require explicit approval and extra caution.
- Sensitive topics include accidents, fatalities, enforcement, medical issues, employer disputes, politics, legal exposure, training failures tied to identifiable people, and emotionally escalated public arguments.
- When a topic is sensitive, the default action is to draft cautiously or recommend no reply.

### Tone and audience boundaries

- Sound professional, clear, practical, and peer-level.
- For X posts and replies, perfect grammar and perfectly complete sentences are optional. Clear meaning, useful substance, and a human voice matter more than polished prose.
- Do not preach, posture, or talk down to students or lower-time pilots.
- Do not use manipulative hooks, fake hustle language, or synthetic controversy.
- Prefer practical teaching insight, operational judgment, career clarity, and honest tradeoffs.

### Source quality and factuality

- Prefer Marcus's lived experience, firsthand teaching observations, and authoritative aviation sources.
- Use official or primary sources when factual claims depend on policy, regulation, training standards, airline process, or platform rules.
- Do not fabricate numbers, results, anecdotes, or credentials.
- If a claim is uncertain, say so or ask Marcus to confirm it before publishing.

### Compliance boundaries

- No stealth automation, saved-session browser puppeteering, fake personas, or engagement manipulation.
- No third-party browser automation for LinkedIn activity.
- No automated likes, follower-growth schemes, spammy duplication, or misleading activity on X.
- Operator-attended native X publishing for approved posts through Odin is allowed when it stays low-volume, explicit, and reviewable.
- Read-only visible-page capture for explicit X post URLs through Huginn is allowed when it behaves like a human operator observing the page.
- Human approval before publishing is mandatory. LinkedIn remains manual unless a future official interface is approved and documented.

### Official interface posture

- X activity must stay inside X's published automation rules: express consent is required for account actions, automated likes are prohibited, and automated replies need opt-in plus extra approval requirements.
- The live Odin path for X profile bio changes is `odin x bio request ...`, `odin approvals resolve ...`, then `odin x bio apply --approval-id ...`. The task must stay browser-executor-owned after approval and can complete only after Odin records public-profile verification evidence.
- The live Odin path for X posts and replies remains one explicitly approved `social_outcome` at a time through `/memory publish <id> via=huginn_x`; `odin x post request ...` and `odin x reply request ...` map operators back to that approved outcome lane until native command ownership is implemented.
- Repost, quote, and share actions are future approval-gated command families only. They require content classification, source URL validation, policy checks, visible pre-action evidence, explicit operator approval, and post-action evidence before any implementation can execute them.
- Likes stay out of execution. Odin may produce read-only like recommendations with rationale, but it must not click or submit likes.
- Do not expand X automation to follows, DMs, bulk posting, engagement farming, or arbitrary browser actions.
- LinkedIn publishing should use official paths such as Share on LinkedIn with `w_member_social` and `POST /v2/ugcPosts` when approved for use; otherwise keep LinkedIn posting manual.
- Do not supplement LinkedIn workflows with scraped website data or unofficial browser tooling.

## 6. Memory design for social workflows

The social copilot should keep only memory that improves future drafting, planning, and compliance.

### Memory categories

- `voice_profile_memory`: phrasing preferences, hook tolerance, sentence length, and words Marcus avoids
- `topic_pillar_memory`: durable topic lanes such as flight instruction, pilot training, airline progression, aviation professionalism, and practical teaching mistakes
- `content_history_memory`: approved post summaries, platform, date, angle, and whether the content felt on-brand
- `performance_learning_memory`: what formats, hooks, and pillars performed well or poorly
- `compliance_boundary_memory`: explicit no-go topics, sensitivity flags, and approval requirements

### Memory rules

- Store only information relevant to Marcus's social workflow.
- Do not leak unrelated sensitive memory into drafting contexts.
- Do not mix social memory with unrelated workspace secrets, finance, family, or health details.
- Treat `odin-os` as the canonical repo for future Odin changes. `odin-orchestrator` is migration context only and is being phased out.

### Recommended capture types

- `social_draft`: draft ideas, proposed posts, or reply candidates worth revisiting
- `social_outcome`: approval decisions, published results, or rejected draft notes. Use explicit outcome fields such as `result`, `channel`, and `content_kind` so approval history stays queryable.
- `social_learning`: durable performance lessons, tone corrections, or audience-fit observations
- `social_research`: reusable findings about engagement opportunities, topic demand, or recurring questions

## 7. End-to-end workflow design

1. Collect ideas from Marcus notes, recurring teaching questions, flight-training observations, recent conversations, and performance gaps.
2. Classify each idea as post, reply, short thread seed, or longer LinkedIn/article seed.
3. Route the idea to the right companion skill.
4. Draft the content with explicit platform fit, hook quality, factual checks, and tone checks.
5. Review for clarity, usefulness, compliance, and whether the draft sounds like Marcus.
6. Queue the draft for Marcus approval with notes on confidence, missing facts, and any sensitivity flags.
7. Publish only through compliant manual or officially supported paths.
8. Record outcomes, summarize learnings, and update future planning memory.

## 8. Weekly cadence

- Monday: generate and rank ideas from current teaching, aviation news relevance, and evergreen topic pillars.
- Tuesday: build the weekly content plan and select which ideas belong on X versus LinkedIn.
- Wednesday: batch X drafts and LinkedIn drafts for Marcus review.
- Thursday: review reply opportunities and prepare compliant response suggestions.
- Friday: capture results, note what was approved or rejected, and write the weekly retrospective.
- Weekend or next-week prep: adjust hooks, topic mix, cadence, and experiments based on the retrospective.

## 9. Analytics and feedback loop

Track these core measures:

- post frequency by platform
- approval rate
- engagement rate
- follower growth
- profile visits
- newsletter or site clicks when available

### Feedback loop

1. collect the week’s approved content and observed results
2. compare performance by topic pillar, hook style, and post structure
3. identify what Marcus liked approving versus what he rejected
4. update the next week’s plan
5. keep only durable learnings, not noisy one-off reactions

## 10. Risks and compliance boundaries

- A good draft can still be wrong if Marcus-specific facts are unverified.
- Reply suggestions carry more reputational risk than original posts because they happen in public context.
- LinkedIn compliance risk is highest when workflow design drifts toward browser automation. Do not do that.
- X compliance risk is highest when drafting turns into high-volume, repetitive, or engagement-bait posting. Avoid that.
- Analytics can be misleading if platform reach is treated as the only success metric.
- The system must never try to conceal when content was AI-assisted.

## 11. Phased implementation roadmap

### Phase 1: Live now in current Odin primitives

- canonical contract doc
- durable memory docs
- companion role contracts as registry `agent` assets
- operator-selectable drafting and planning skills
- one shared workflow definition
- workflow-scoped runtime memory capture for drafts, outcomes, learnings, and research
- filtered recall and exact-id inspection through the generic `/memory` command
- approval queue resolution for `social_draft` entries through `/memory resolve`, with automatic `social_outcome` recording when the draft carries valid channel and content metadata
- manual publish evidence capture for approved `social_outcome` entries through `/memory publish ... url=...`, so approved and actually published content stay distinct when Marcus posts outside Odin and so the same outcome row can be repaired without resubmitting if native X publish already created a live URL but failed to record durable publish state
- operator-attended native X posting for approved `social_outcome` entries through `/memory publish ... via=huginn_x`, including approved X replies when `content_kind=reply` and `in_reply_to_url` are present, with the same memory record updated in place with publish evidence
- explicit approved-versus-rejected content history logging through validated `social_outcome` entries
- repeatable weekly retrospective prompts through the existing workflow + analytics advisor path using the last 7 days of social memory
- compact multi-week comparison across the last 4 rolling weekly windows through the same workflow + analytics advisor path
- prompt-only next-week carry-forward guidance with keep, avoid, test-next, and platform-direction sections through the same analytics advisor path
- read-only visible-page X post evidence capture for explicit post URLs through `/tool run browser_x_post_visible_evidence ...`, with automatic workflow-scoped `social_evidence` recording and retrospective prompt inclusion
- weekly X evidence bundle capture for explicit post URLs through `/tool run browser_x_weekly_evidence_bundle ...`, with one workflow-scoped `social_evidence` memory entry recorded per captured post and reused in the same analytics prompt path
- human approval, operator-attended X native publishing, and compliant manual LinkedIn publishing

### Phase 2: Next practical extension

- longer-horizon trend interpretation and optional persistence for approved experiments or guardrails beyond the prompt-only carry-forward block
- manual LinkedIn evidence intake with the same workflow-scoped `social_evidence` memory model

### Phase 3: When official interfaces are available

- native scheduling or official publishing integration
- official analytics import
- approval queues linked to actual publish records

### Phase 4: Future media-rich expansion

- image, GIF, and video planning
- media briefing and shot-list generation
- compliant asset review workflows

Do not implement Phase 4 until the text-only workflow is stable and compliant.

## 12. CLI and chat examples for how Marcus would use Odin for this workflow

For the first Marcus-live X-post operator path, use [docs/operations/marcus-live-x-post-runbook.md](../operations/marcus-live-x-post-runbook.md).

### Current live CLI flow

```text
odin
/skill use marcus-social-content-strategist
Build a 7-day X and LinkedIn plan for Marcus around flight instruction mistakes, airline career progression, and practical student pilot lessons.

/skill use marcus-x-drafting-assistant
Draft 3 X post options about why students plateau in crosswind landings and how instructors should coach through it.

/skill use marcus-linkedin-drafting-assistant
Turn that idea into one LinkedIn post for flight instructors with a stronger professional takeaway and no fake hype.

/skill use marcus-engagement-research-assistant
Review these 6 public posts and suggest which ones Marcus should reply to, which ones to skip, and draft compliant reply options for the good opportunities.

/skill use marcus-social-analytics-advisor
Summarize this week’s posts, approval decisions, and engagement results, then recommend next week’s adjustments.

/workflow use marcus-social-growth-workflow
/memory remember social_draft channel=x approval=pending -- Draft on why student pilots plateau in crosswind landings and what better coaching looks like.
/memory resolve 12 result=approved
/memory publish 13 via=huginn_x
/memory remember social_outcome result=approved channel=x content_kind=reply in_reply_to_url=https://x.com/example/status/123 -- Short, useful reply text.
/memory publish 14 via=huginn_x
/memory publish 13 url=https://x.com/marcus/status/123456789
/memory remember social_outcome result=approved channel=linkedin content_kind=post -- LinkedIn post about CFI decision-making approved for next review batch.
/memory remember social_outcome result=rejected channel=x content_kind=reply reason=too-defensive -- X reply draft rejected because it sounded too defensive.
/tool run browser_x_post_visible_evidence target_url=https://x.com/marcus/status/123 label=marcus-crosswind
/tool run browser_x_weekly_evidence_bundle target_urls=https://x.com/marcus/status/123,https://x.com/marcus/status/456 label=weekly-review
/memory list type=social_draft field.approval=pending order=desc limit=5
/memory list type=social_outcome field.result=approved
/memory list type=social_outcome field.publish_status=published
/memory list type=social_evidence field.evidence_kind=x_post_visible
/memory list type=social_evidence field.bundle_label=weekly-review
/skill use marcus-social-analytics-advisor
Give me this week’s retrospective, include recent X visible evidence, compare it to the last 4 weekly windows, and give me next-week carry-forward guidance for X and LinkedIn.
/memory show 1
```

### Current live chat-style prompts

```text
Use the Content Strategist Advisor and give me a one-week content calendar focused on CFI decision-making, airline readiness, and practical teaching mistakes.
```

```text
Use the Analytics and Retrospective Advisor and give me a weekly retrospective from the last 7 days, compare recurring patterns across the last 4 weekly windows, then give me keep/avoid/test-next carry-forward guidance with X closer to Marcus's inner thoughts and LinkedIn more professionally framed.
```

```text
Use the X Drafting Assistant and write a text-only post that sounds like a serious flight instructor, not a growth marketer.
```

```text
Use the Engagement Research Assistant and flag anything that needs my explicit approval because it touches a sensitive topic.
```

### Future-only examples

These are not live yet and must stay aspirational until official integrations exist:

- publish an approved post through an official API
- import platform analytics automatically
- schedule approved content without a manual publish step
