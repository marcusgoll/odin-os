# Approval Policy Parity Contract

Odin execution paths must resolve risky action decisions through
`internal/core/policy.Service`.

## Canonical Decision Path

- Permission and scope checks use `Service.AuthorizeInvocation`.
- Approval gates use `Service.DecideApproval` or `Service.AuthorizeApproval`.
- Runtime packages may wrap project-specific requirements, but the final
  allowed, approval-required, or denied decision must come from the policy
  service.
- Docs or YAML policy are not sufficient unless the runtime calls the policy
  service before mutation.

## Required Wrappers

- Work Items, jobs, and trigger-materialized work must evaluate approval before
  dispatch or execution.
- Skills must classify mutating permissions through the same approval posture
  used by jobs.
- Tool broker invocation and Capability Gateway builtin-tool invocation must
  reject approval-required tools before the tool handler runs.
- Low-level external mutation invocations must fail closed unless a source-owned
  path passes an already-approved intent to the invocation service.
- Executors may run only after admission has produced an allowed decision.

## Risk Classes

The high-risk classes include sending messages, changing shared calendars,
purchases, deletion, deployment, production mutation, permission changes,
public posting, financial records, legal records, medical records, GitHub
writes, and external tool mutation.

Read-only inspection is allowed without approval. Governance and destructive
mutation require approval when the active project policy says so. System-project
mutation requires approval when the system-project gate is enabled.

## Audit And Readback

- Approval-required Work Items create an `approval.requested` event and a
  pending Approval Request.
- Denied approvals create an `approval.resolved` event with `status=denied`.
- Skill denials emit skill lifecycle events with stable policy error codes.
- Capability and tool denials return coded `approval_required` or
  `approval_denied` errors before handler execution.
- Operators read pending approval state through `odin approvals` and
  approval-backed review entries through `odin review`.
