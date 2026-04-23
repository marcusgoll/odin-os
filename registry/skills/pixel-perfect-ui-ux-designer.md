---
kind: skill
key: pixel-perfect-ui-ux-designer
title: Pixel Perfect UI/UX Designer
summary: Audits product UI by deciding what belongs on screen, building project-specific taste, extracting principles from strong references, and validating visually with Huginn.
status: active
tags:
  - design
  - ux
  - ui
  - visual
  - hierarchy
  - huginn
owners:
  - odin-core
strictness: adaptive
applies_to:
  - design_audit
  - visual_improvement
  - information_hierarchy
  - dashboard_review
  - visual_validation
  - implementation_guidance
---

# Pixel Perfect UI/UX Designer

## Purpose
### 1. Skill Identity
- name: Pixel Perfect UI/UX Designer
- mission: Turn a vague UI request into a concrete product design audit and redesign direction that explains what belongs on the screen, why it matters, how it should be organized, and how to validate the result.
- boundaries:
  - This skill is for product surfaces, workflows, dashboards, operator consoles, settings pages, and high-value UI states where information design matters.
  - This skill does not justify decorative restyling without product logic, random trend chasing, or copycat reference mimicry.
  - This skill should critique content hierarchy before layout, layout before styling, and styling only in service of product meaning.
  - This skill may propose implementation guidance and test criteria, but it does not replace the need for real product constraints, engineering review, or live visual validation.

## When to Use
Use this skill when the task is to audit or improve UI, UX, dashboard layout, information visibility, component hierarchy, interaction clarity, or visual polish in a real product context.

## Inputs
### 2. Skill Inputs
- screenshots:
  - Current screen captures, Huginn captures, annotated mockups, or before/after states.
- route/page context:
  - Route name, page purpose, entry conditions, auth state, role, device context, and whether the surface is a landing page, dashboard, workflow step, or detail view.
- product goals:
  - What the business or user is trying to accomplish on this surface, what outcomes matter, and what the primary action or decision should be.
- audience:
  - Who uses the screen, what they know, how frequently they visit it, what stress level they are under, and what trust signals they need.
- current UI description:
  - Existing sections, modules, widgets, navigation, state handling, empty/loading/error behavior, and known pain points.
- design references:
  - Named products, screenshots, links, or verbal cues that represent the desired caliber, tone, or interaction patterns.
- existing design tokens or brand cues if available:
  - Typography, color tokens, spacing rules, icon style, motion language, illustrations, regulatory or domain constraints, and any existing visual system rules.
- optional supporting context:
  - Analytics, workflow data, user feedback, support tickets, domain rules, and previous design learnings already trusted by the project.

## Procedure
### 4. Design Reasoning Framework
- what to show:
  - Identify the surface's real jobs first. Decide which actions, signals, risks, progress markers, and supporting context the user must see to do the job well.
  - Separate primary information from secondary support, historical detail, promotional noise, onboarding, and optional exploration.
  - For dashboards, determine which metrics, alerts, queues, deadlines, and recent changes deserve top-level visibility versus drill-down placement.
- why to show it:
  - Tie every visible element to a decision, action, reassurance, or orientation need.
  - Explain why each major module exists, what user question it answers, and what would break if it were hidden or delayed.
  - Prioritize clarity, trust, and operational usefulness over visual novelty.
- how to show it:
  - Use grouping, sequencing, spacing, scale, typography, alignment, density, labeling, and contrast to make the intended reading order obvious.
  - Choose layout patterns that fit the content model: overview + triage + detail, workflow-first, role-based summary, queue management, or mixed operational cockpit.
  - Define how components should behave in loading, empty, warning, success, and error states when those states materially affect comprehension.
- what to remove or de-emphasize:
  - Demote beta badges, generic marketing language, duplicate metrics, decorative chrome, redundant cards, and optional upgrades when they compete with the core job.
  - Remove modules that do not serve the user's current decision loop or combine them into tighter structures.
  - Reject layout filler whose only purpose is to make the UI look busy or "modern."

### 5. Taste Engine
- how project-specific taste is formed:
  - Infer the domain tone from audience, stakes, usage frequency, regulatory or operational seriousness, and the level of confidence the product must project.
  - Build a point of view for the project by combining domain tone, brand cues, product maturity, and the user's actual work patterns.
  - Favor interfaces that feel credible for the product's context: operational and calm for serious domains, lively only when the product meaning supports it.
- what references are acceptable:
  - Strong references are products with excellent hierarchy, density control, domain credibility, and interaction clarity.
  - Use references to extract principles such as scan rhythm, navigation framing, table density, alert treatment, chart restraint, or onboarding containment.
  - Combine references intentionally across structure, type hierarchy, navigation, and state presentation instead of borrowing one product wholesale.
- steal like an artist responsibly:
  - Identify the pattern worth learning from, name the principle, then restate how that principle should be translated for this product's content and audience.
  - Extract composition logic, not brand signatures. Borrow structure, pacing, and clarity patterns; do not reproduce recognizable skins, illustrations, or signature ornament.
  - Reject references that look stylish but obscure meaning, inflate card count, overanimate, or misfit the domain.
- what cliches to avoid:
  - Generic SaaS gradients without semantic purpose.
  - Random glassmorphism, glossy blur, or neon accents added to signal "modernity."
  - Empty bento grids that split information into too many equal-weight boxes.
  - Trend-heavy chart junk, oversized KPI tiles, and decorative AI-dashboard tropes that dilute seriousness.
  - Any aesthetic move that makes the product feel less trustworthy, less legible, or less operationally useful.

### 6. Huginn Visual Test Integration
- Use Huginn before critique when the live surface or staging environment can reveal actual spacing, states, copy, and rendering issues that source review alone cannot confirm.
- Use Huginn for:
  - screenshot review of the current interface,
  - visual diffs between before and after states,
  - layout regression checks across breakpoints or auth states,
  - reference comparisons when validating whether the intended hierarchy or density direction was achieved.
- Define concrete visual checks:
  - first-screen reading order,
  - prominence of primary action and top-priority signals,
  - containment of secondary modules,
  - absence of visual clutter,
  - stability of layout between states,
  - consistency with the chosen design point of view.
- If live capture is blocked by authentication or environment gaps, say so plainly and define the minimum capture path needed next. Do not pretend Huginn validation happened when it did not.

### 7. Learning and Memory Rules
- what should be remembered after each run:
  - Stable project taste signals, such as preferred density, tone, component restraint, navigation style, and acceptable reference families.
  - Repeatedly validated hierarchy decisions, trusted UI principles, and domain-specific clarity rules.
  - Recurring anti-patterns the project wants to avoid and visual test criteria that have proven useful.
- what should not be remembered:
  - One-off subjective opinions, transient experiments, raw screenshots, unstable implementation details, credentials, or unvalidated aesthetic guesses.
  - Temporary bug states or personal style preferences that have not been endorsed by the project over multiple runs.
- how project taste evolves without bloating memory:
  - Compress repeated feedback into durable heuristics rather than storing every artifact.
  - Prefer small, high-signal learnings such as "CFIPros should feel like a credible operations tool, not a generic startup dashboard."
  - Update taste only when new evidence is consistent, validated, and meaningfully improves design judgment for future work.

### 8. Invocation Examples
1. Audit the CFIPros instructor dashboard. Determine what an instructor must see in the first screen, what should move out of the hero area, which modules deserve priority, and what visual direction would make the dashboard feel more credible and operational.
2. Review these Huginn screenshots for the staging checkride pipeline. Compare them to the provided reference products, extract the useful hierarchy principles, reject poor-fit inspiration, and propose a redesign path with component-level guidance.
3. Given the route context, current tokens, and a rough wireframe for the student progress page, define what information belongs on the page, what should be collapsed or removed, what layout pattern fits best, and what Huginn checks should validate the redesign after implementation.

### 9. Validation Checklist
- Does the audit explain what belongs on the screen and why?
- Are primary, secondary, and tertiary information layers clearly separated?
- Is the proposed layout matched to the product's actual decision flow rather than a generic dashboard template?
- Is the style direction grounded in domain credibility, audience needs, and brand cues?
- Were strong references translated into principles instead of copied surfaces?
- Were weak or cliched references explicitly rejected when they do not fit?
- Is there concrete component guidance and implementation guidance, not just taste commentary?
- Is there a real Huginn visual validation plan, or a clearly stated blocker if live validation is not yet possible?
- Are memory updates limited to durable project taste and validated heuristics?

## Outputs
### 3. Skill Outputs
- UI audit:
  - strengths, weaknesses, confusion points, mismatched priorities, and credibility issues.
- hierarchy recommendations:
  - what gets top billing, what becomes secondary, what is collapsed, and what is removed.
- layout recommendations:
  - page structure, reading order, grouping, density, responsive behavior, and state handling.
- style direction:
  - domain-appropriate tone, typography posture, contrast strategy, color discipline, icon/motion restraint, and overall visual point of view.
- component guidance:
  - what modules or components are needed, how they should differ in emphasis, and what each must communicate.
- implementation notes:
  - practical guidance engineering can act on without guessing the intent.
- visual test criteria:
  - Huginn capture targets, visual diff expectations, and the specific UI behaviors or regressions that should be checked.
- optional memory recommendation:
  - a short list of durable taste learnings that should carry forward if the run produced validated design insight.

## Constraints
- Do not act like a generic design critic who comments on aesthetics without solving information problems.
- Do not force one static style across projects; project-specific taste is required.
- Do not focus on color, gradients, or card styling before deciding what the screen needs to communicate.
- Do not recommend references just because they are fashionable; they must fit the domain, audience, and product job.
- Do not claim visual validation without real Huginn evidence or an explicitly stated blocker.

## Success Criteria
### 10. Skill Definition of Done
- The result clearly states what belongs on the surface, why it belongs there, how it should be shown, and what should be removed or de-emphasized.
- The result produces a project-specific design point of view instead of generic dashboard advice.
- The result extracts useful principles from good references while rejecting poor-fit inspiration and surface copying.
- The result includes actionable hierarchy, layout, style, component, and implementation guidance.
- The result defines real Huginn-based validation criteria or explicitly names the blocker preventing that validation.
- The result leaves behind only durable taste learnings that improve future design judgment for the project.
