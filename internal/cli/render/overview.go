package render

import (
	"fmt"
	"strings"

	"odin-os/internal/cli/overview"
)

func RenderOverview(view overview.View) string {
	var lines []string

	lines = append(lines, "Workspace")
	lines = append(lines, fmt.Sprintf(
		"  key=%s name=%s status=%s wiring=%s scope=%s default_companion=%s initiatives=%d companions=%d open_work=%d active_runs=%d approvals=%d incidents=%d blocked=%d",
		valueOrNone(view.Workspace.WorkspaceKey),
		valueOrNone(view.Workspace.Name),
		valueOrNone(view.Workspace.Status),
		valueOrNone(string(view.Workspace.Wiring)),
		valueOrNone(view.Workspace.ControlScope),
		valueOrNone(view.Workspace.DefaultCompanionKey),
		view.Workspace.InitiativeCount,
		view.Workspace.CompanionCount,
		view.Workspace.OpenWorkItemCount,
		view.Workspace.ActiveRunCount,
		view.Workspace.PendingApprovalCount,
		view.Workspace.OpenIncidentCount,
		view.Workspace.BlockedWorkItemCount,
	))

	lines = append(lines, "")
	lines = append(lines, "Attention")
	lines = append(lines, fmt.Sprintf(
		"  approvals=%d incidents=%d blocked_work=%d failed_work=%d recoveries=%d blocked_swarms=%d",
		len(view.Approvals),
		len(view.Observability.Incidents),
		len(view.Observability.BlockedWork),
		len(view.Observability.RecoveryGuidance),
		len(view.Observability.Recoveries),
		countBlockedSwarms(view.CompanionSwarms),
	))
	if len(view.Approvals) == 0 && len(view.Observability.Incidents) == 0 && len(view.Observability.BlockedWork) == 0 && len(view.Observability.RecoveryGuidance) == 0 && len(view.Observability.Recoveries) == 0 && countBlockedSwarms(view.CompanionSwarms) == 0 {
		lines = append(lines, "  none")
	} else {
		for _, approval := range view.Approvals {
			lines = append(lines, fmt.Sprintf(
				"  approval=%d work_item=%s project=%s companion=%s run=%s status=%s resolver=%s requested_at=%s",
				approval.ApprovalID,
				valueOrNone(approval.WorkItemKey),
				valueOrNone(approval.ProjectKey),
				valueOrNone(ptrValue(approval.CompanionKey)),
				nullableInt64(approval.RunID),
				valueOrNone(approval.Status),
				valueOrNone(approval.ResolverSupport),
				valueOrNone(approval.RequestedAt),
			))
		}
		for _, incident := range view.Observability.Incidents {
			lines = append(lines, fmt.Sprintf(
				"  incident work_item=%s project=%s companion=%s severity=%s status=%s summary=%s",
				valueOrNone(incident.WorkItemKey),
				valueOrNone(incident.ProjectKey),
				valueOrNone(ptrValue(incident.CompanionKey)),
				valueOrNone(incident.Severity),
				valueOrNone(incident.Status),
				valueOrNone(incident.Summary),
			))
		}
		for _, blocked := range view.Observability.BlockedWork {
			lines = append(lines, fmt.Sprintf(
				"  blocked work_item=%s project=%s companion=%s source=%s reason=%s",
				valueOrNone(blocked.WorkItemKey),
				valueOrNone(blocked.ProjectKey),
				valueOrNone(ptrValue(blocked.CompanionKey)),
				valueOrNone(blocked.Source),
				valueOrNone(blocked.Reason),
			))
		}
		for _, failed := range view.Observability.RecoveryGuidance {
			lines = append(lines, fmt.Sprintf(
				"  failed_work=%s project=%s companion=%s decision=%s retry_eligible=%t recommendation=%s",
				valueOrNone(failed.WorkItemKey),
				valueOrNone(failed.ProjectKey),
				valueOrNone(ptrValue(failed.CompanionKey)),
				valueOrNone(failed.Decision),
				failed.RetryEligible,
				valueOrNone(failed.RecoveryRecommendation),
			))
		}
		for _, recovery := range view.Observability.Recoveries {
			lines = append(lines, fmt.Sprintf(
				"  recovery run=%d status=%s strategy=%s started_at=%s",
				recovery.RunID,
				valueOrNone(recovery.Status),
				valueOrNone(recovery.Strategy),
				valueOrNone(recovery.StartedAt),
			))
		}
		for _, swarm := range view.CompanionSwarms {
			if strings.TrimSpace(swarm.BlockedReason) == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf(
				"  blocked_swarm=%s project=%s companion=%s reason=%s backlog=%d",
				valueOrNone(swarm.ParentTaskKey),
				valueOrNone(swarm.ProjectKey),
				valueOrNone(ptrValue(swarm.CompanionKey)),
				valueOrNone(swarm.BlockedReason),
				swarm.BacklogCount,
			))
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Review Queue")
	lines = append(lines, fmt.Sprintf(
		"  wiring=%s total=%d intake=%d approvals=%d knowledge=%d skills=%d failed=%d",
		valueOrNone(string(view.ReviewQueue.Wiring)),
		view.ReviewQueue.TotalCount,
		view.ReviewQueue.IntakeCount,
		view.ReviewQueue.ApprovalCount,
		view.ReviewQueue.KnowledgeCount,
		view.ReviewQueue.SkillArtifactCount,
		view.ReviewQueue.FailedWorkCount,
	))

	lines = append(lines, "")
	lines = append(lines, "Active Execution")
	lines = append(lines, fmt.Sprintf(
		"  runs=%d swarms=%d",
		len(view.Observability.ActiveRuns),
		len(view.CompanionSwarms),
	))
	if len(view.Observability.ActiveRuns) == 0 && len(view.CompanionSwarms) == 0 {
		lines = append(lines, "  none")
	} else {
		for _, run := range view.Observability.ActiveRuns {
			lines = append(lines, fmt.Sprintf(
				"  run=%d work_item=%s project=%s initiative=%s companion=%s executor=%s status=%s attempt=%d",
				run.RunID,
				valueOrNone(run.WorkItemKey),
				valueOrNone(run.ProjectKey),
				valueOrNone(ptrValue(run.InitiativeKey)),
				valueOrNone(ptrValue(run.CompanionKey)),
				valueOrNone(run.Executor),
				valueOrNone(run.Status),
				run.Attempt,
			))
		}
		for _, swarm := range view.CompanionSwarms {
			lines = append(lines, fmt.Sprintf(
				"  swarm=%s project=%s companion=%s status=%s active_children=%d backlog=%d blocked_reason=%s",
				valueOrNone(swarm.ParentTaskKey),
				valueOrNone(swarm.ProjectKey),
				valueOrNone(ptrValue(swarm.CompanionKey)),
				valueOrNone(swarm.Status),
				swarm.ActiveChildRunCount,
				swarm.BacklogCount,
				valueOrNone(swarm.BlockedReason),
			))
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Initiatives")
	if len(view.Initiatives) == 0 {
		lines = append(lines, "  none")
	} else {
		for _, initiative := range view.Initiatives {
			lines = append(lines, fmt.Sprintf(
				"  %s title=%s kind=%s status=%s owner=%s project=%s open=%d runs=%d approvals=%d incidents=%d blocked=%d",
				valueOrNone(initiative.InitiativeKey),
				valueOrNone(initiative.Title),
				valueOrNone(initiative.Kind),
				valueOrNone(initiative.Status),
				valueOrNone(ptrValue(initiative.OwnerCompanionKey)),
				valueOrNone(ptrValue(initiative.LinkedProjectKey)),
				initiative.OpenWorkItemCount,
				initiative.ActiveRunCount,
				initiative.PendingApprovalCount,
				initiative.OpenIncidentCount,
				initiative.BlockedWorkItemCount,
			))
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Work Items")
	if len(view.WorkItems) == 0 {
		lines = append(lines, "  none")
	} else {
		for _, item := range view.WorkItems {
			lines = append(lines, fmt.Sprintf(
				"  %s title=%s initiative=%s companion=%s status=%s scope=%s project=%s current_run=%s run_status=%s",
				valueOrNone(item.WorkItemKey),
				valueOrNone(item.Title),
				valueOrNone(ptrValue(item.InitiativeKey)),
				valueOrNone(ptrValue(item.CompanionKey)),
				valueOrNone(item.Status),
				valueOrNone(item.Scope),
				valueOrNone(item.ProjectKey),
				nullableInt64(item.CurrentRunID),
				valueOrNone(item.CurrentRunStatus),
			))
			lines = append(lines, "    Run Attempts")
			if len(item.RunAttempts) == 0 {
				lines = append(lines, "      none")
				continue
			}
			for _, run := range item.RunAttempts {
				lines = append(lines, fmt.Sprintf(
					"      run=%d companion=%s executor=%s status=%s attempt=%d",
					run.RunID,
					valueOrNone(ptrValue(run.CompanionKey)),
					valueOrNone(run.Executor),
					valueOrNone(run.Status),
					run.Attempt,
				))
			}
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Companions")
	lines = append(lines, fmt.Sprintf("  wiring=%s", valueOrNone(string(view.Companions.Wiring))))
	if len(view.Companions.Items) == 0 {
		lines = append(lines, "  none")
	} else {
		for _, companion := range view.Companions.Items {
			lines = append(lines, fmt.Sprintf(
				"  %s title=%s kind=%s status=%s owned_initiatives=%d open=%d runs=%d approvals=%d blocked=%d",
				valueOrNone(companion.CompanionKey),
				valueOrNone(companion.Title),
				valueOrNone(companion.Kind),
				valueOrNone(companion.Status),
				companion.OwnedInitiativeCount,
				companion.OpenWorkItemCount,
				companion.ActiveRunCount,
				companion.PendingApprovalCount,
				companion.BlockedWorkItemCount,
			))
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Capability Catalog")
	lines = append(lines, fmt.Sprintf(
		"  wiring=%s agent_definitions=%d skills=%d workflows=%d commands=%d tools=%d",
		valueOrNone(string(view.CapabilityCatalog.Wiring)),
		view.CapabilityCatalog.AgentDefinitionCount,
		view.CapabilityCatalog.SkillCount,
		view.CapabilityCatalog.WorkflowCount,
		view.CapabilityCatalog.CommandCount,
		view.CapabilityCatalog.ToolCount,
	))

	lines = append(lines, "")
	lines = append(lines, "Capability Truth")
	lines = append(lines, fmt.Sprintf(
		"  wiring=%s authored_assets=%d runtime_proven=%d partial=%d advisory=%d unknown=%d high_risk=%d",
		valueOrNone(string(view.CapabilityTruth.Wiring)),
		view.CapabilityTruth.AuthoredAssetCount,
		view.CapabilityTruth.RuntimeProvenCount,
		view.CapabilityTruth.PartialCount,
		view.CapabilityTruth.AdvisoryCount,
		view.CapabilityTruth.UnknownCount,
		view.CapabilityTruth.HighRiskFamilyCount,
	))
	for _, note := range view.CapabilityTruth.Notes {
		lines = append(lines, fmt.Sprintf("  note=%s", valueOrNone(note)))
	}
	if len(view.CapabilityTruth.Items) == 0 {
		lines = append(lines, "  truth=none")
	} else {
		renderedTruthItems := capabilityTruthItemsForText(view.CapabilityTruth.Items)
		for _, item := range renderedTruthItems {
			lines = append(lines, fmt.Sprintf(
				"  truth kind=%s key=%s level=%s implemented=%t risk=%s proof=%s",
				valueOrNone(item.Kind),
				valueOrNone(item.Key),
				valueOrNone(item.TruthLevel),
				item.CountsAsImplemented,
				valueOrNone(item.RiskLabel),
				valueOrNone(strings.Join(item.Proof, ",")),
			))
		}
		if remaining := len(view.CapabilityTruth.Items) - len(renderedTruthItems); remaining > 0 {
			lines = append(lines, fmt.Sprintf("  truth remaining=%d use_json=true", remaining))
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Skill Activity")
	lines = append(lines, fmt.Sprintf(
		"  wiring=%s invoke_success=%d invoke_failure=%d stub_results=%d command_output_only=%d",
		valueOrNone(string(view.SkillActivity.Wiring)),
		view.SkillActivity.InvokeSuccessCount,
		view.SkillActivity.InvokeFailureCount,
		view.SkillActivity.StubResultCount,
		view.SkillActivity.CommandOutputOnlyCount,
	))
	if len(view.SkillActivity.Recent) == 0 {
		lines = append(lines, "  recent=none")
	} else {
		for _, event := range view.SkillActivity.Recent {
			lines = append(lines, fmt.Sprintf(
				"  skill=%s operation=%s outcome=%s effect=%s handler=%s at=%s",
				valueOrNone(event.SkillKey),
				valueOrNone(event.Operation),
				valueOrNone(event.Outcome),
				valueOrNone(event.RuntimeEffect),
				valueOrNone(event.HandlerRef),
				valueOrNone(event.OccurredAt),
			))
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Delegation Truth")
	lines = append(lines, fmt.Sprintf(
		"  wiring=%s runtime_status=%s operator_surface=%s companion_work_path=%s swarms=%d",
		valueOrNone(string(view.DelegationTruth.Wiring)),
		valueOrNone(view.DelegationTruth.RuntimeStatus),
		valueOrNone(view.DelegationTruth.OperatorSurface),
		valueOrNone(view.DelegationTruth.CompanionWorkPath),
		view.DelegationTruth.CompanionSwarmCount,
	))
	if view.DelegationTruth.Note != "" {
		lines = append(lines, fmt.Sprintf("  note=%s", view.DelegationTruth.Note))
	}

	lines = append(lines, "")
	lines = append(lines, "Approvals")
	if len(view.Approvals) == 0 {
		lines = append(lines, "  none")
	} else {
		for _, approval := range view.Approvals {
			lines = append(lines, fmt.Sprintf(
				"  approval=%d work_item=%s project=%s companion=%s run=%s status=%s resolver=%s requested_at=%s",
				approval.ApprovalID,
				valueOrNone(approval.WorkItemKey),
				valueOrNone(approval.ProjectKey),
				valueOrNone(ptrValue(approval.CompanionKey)),
				nullableInt64(approval.RunID),
				valueOrNone(approval.Status),
				valueOrNone(approval.ResolverSupport),
				valueOrNone(approval.RequestedAt),
			))
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Observability")
	lines = append(lines, fmt.Sprintf(
		"  wiring=%s activity_log=%d active_runs=%d blocked_work=%d failed_work=%d incidents=%d recoveries=%d freshness=%d",
		valueOrNone(string(view.Observability.Wiring)),
		len(view.Observability.ActivityLog),
		len(view.Observability.ActiveRuns),
		len(view.Observability.BlockedWork),
		len(view.Observability.RecoveryGuidance),
		len(view.Observability.Incidents),
		len(view.Observability.Recoveries),
		len(view.Observability.Freshness),
	))
	lines = append(lines, "  Activity Log")
	if len(view.Observability.ActivityLog) == 0 {
		lines = append(lines, "    none")
	}
	for _, event := range view.Observability.ActivityLog {
		lines = append(lines, fmt.Sprintf(
			"    event=%d type=%s scope=%s project=%s work_item=%s run=%s approval=%s summary=%s",
			event.EventID,
			valueOrNone(event.EventType),
			valueOrNone(event.Scope),
			valueOrNone(event.ProjectKey),
			valueOrNone(event.WorkItemKey),
			nullableInt64(event.RunID),
			nullableInt64(event.ApprovalID),
			valueOrNone(event.Summary),
		))
	}
	lines = append(lines, "  Run Attempts")
	if len(view.Observability.ActiveRuns) == 0 {
		lines = append(lines, "    none")
	}
	for _, run := range view.Observability.ActiveRuns {
		lines = append(lines, fmt.Sprintf(
			"    run=%d work_item=%s project=%s initiative=%s companion=%s executor=%s status=%s attempt=%d",
			run.RunID,
			valueOrNone(run.WorkItemKey),
			valueOrNone(run.ProjectKey),
			valueOrNone(ptrValue(run.InitiativeKey)),
			valueOrNone(ptrValue(run.CompanionKey)),
			valueOrNone(run.Executor),
			valueOrNone(run.Status),
			run.Attempt,
		))
	}
	lines = append(lines, "  Blocked Work")
	if len(view.Observability.BlockedWork) == 0 {
		lines = append(lines, "    none")
	}
	for _, blocked := range view.Observability.BlockedWork {
		lines = append(lines, fmt.Sprintf(
			"    blocked=%s project=%s companion=%s source=%s reason=%s",
			valueOrNone(blocked.WorkItemKey),
			valueOrNone(blocked.ProjectKey),
			valueOrNone(ptrValue(blocked.CompanionKey)),
			valueOrNone(blocked.Source),
			valueOrNone(blocked.Reason),
		))
	}
	lines = append(lines, "  Failed Work")
	if len(view.Observability.RecoveryGuidance) == 0 {
		lines = append(lines, "    none")
	}
	for _, failed := range view.Observability.RecoveryGuidance {
		lines = append(lines, fmt.Sprintf(
			"    failed_work=%s project=%s companion=%s status=%s decision=%s retry_eligible=%t retries=%d/%d source=%s last_error=%s recommendation=%s",
			valueOrNone(failed.WorkItemKey),
			valueOrNone(failed.ProjectKey),
			valueOrNone(ptrValue(failed.CompanionKey)),
			valueOrNone(failed.Status),
			valueOrNone(failed.Decision),
			failed.RetryEligible,
			failed.RetryCount,
			failed.MaxAttempts,
			valueOrNone(failed.Source),
			valueOrNone(failed.LastError),
			valueOrNone(failed.RecoveryRecommendation),
		))
	}
	lines = append(lines, "  Incidents")
	if len(view.Observability.Incidents) == 0 {
		lines = append(lines, "    none")
	}
	for _, incident := range view.Observability.Incidents {
		lines = append(lines, fmt.Sprintf(
			"    incident=%d work_item=%s project=%s companion=%s severity=%s status=%s fault_key=%s subject_key=%s decision_mode=%s next_action=%s summary=%s",
			incident.IncidentID,
			valueOrNone(incident.WorkItemKey),
			valueOrNone(incident.ProjectKey),
			valueOrNone(ptrValue(incident.CompanionKey)),
			valueOrNone(incident.Severity),
			valueOrNone(incident.Status),
			valueOrNone(incident.FaultKey),
			valueOrNone(incident.SubjectKey),
			valueOrNone(incident.DecisionMode),
			valueOrNone(incident.NextAction),
			valueOrNone(incident.Summary),
		))
	}
	lines = append(lines, "  Recoveries")
	if len(view.Observability.Recoveries) == 0 {
		lines = append(lines, "    none")
	}
	for _, recovery := range view.Observability.Recoveries {
		lines = append(lines, fmt.Sprintf(
			"    recovery=%d run=%d status=%s strategy=%s fault_key=%s subject_key=%s decision_mode=%s action=%s next_action=%s started_at=%s",
			recovery.RecoveryID,
			recovery.RunID,
			valueOrNone(recovery.Status),
			valueOrNone(recovery.Strategy),
			valueOrNone(recovery.FaultKey),
			valueOrNone(recovery.SubjectKey),
			valueOrNone(recovery.DecisionMode),
			valueOrNone(recovery.ActionName),
			valueOrNone(recovery.NextAction),
			valueOrNone(recovery.StartedAt),
		))
	}

	lines = append(lines, "")
	lines = append(lines, "Memory")
	lines = append(lines, fmt.Sprintf("  wiring=%s count=%d", valueOrNone(string(view.Memory.Wiring)), view.Memory.Count))
	if len(view.Memory.Recent) == 0 {
		lines = append(lines, "  none")
	} else {
		for _, memory := range view.Memory.Recent {
			lines = append(lines, fmt.Sprintf(
				"  memory=%d type=%s scope=%s/%s summary=%s",
				memory.ID,
				valueOrNone(memory.MemoryType),
				valueOrNone(memory.Scope),
				valueOrNone(memory.ScopeKey),
				valueOrNone(memory.Summary),
			))
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Intake Inbox")
	lines = append(lines, fmt.Sprintf(
		"  wiring=%s source=%s status=%s count=%d raw_items=%d raw_processed=%d review_queue=%d approval_required=%d note=%s",
		valueOrNone(string(view.IntakeInbox.Wiring)),
		valueOrNone(view.IntakeInbox.Source),
		valueOrNone(view.IntakeInbox.Status),
		len(view.IntakeInbox.Items),
		view.IntakeInbox.RawItemCount,
		view.IntakeInbox.RawProcessedCount,
		view.IntakeInbox.ReviewQueueCount,
		view.IntakeInbox.IntakeApprovalRequiredCount,
		valueOrNone(view.IntakeInbox.Note),
	))
	if len(view.IntakeInbox.Items) == 0 {
		lines = append(lines, "  none")
	}
	for _, intake := range view.IntakeInbox.Items {
		lines = append(lines, fmt.Sprintf(
			"  linked_intake=%d source=%s type=%s dedup_key=%s requested_by=%s work_item=%s work_status=%s initiative=%s companion=%s project=%s",
			intake.IntakeID,
			valueOrNone(intake.Source),
			valueOrNone(intake.IntakeType),
			valueOrNone(intake.DedupKey),
			valueOrNone(intake.RequestedBy),
			valueOrNone(intake.WorkItemKey),
			valueOrNone(intake.WorkItemStatus),
			valueOrNone(ptrValue(intake.InitiativeKey)),
			valueOrNone(ptrValue(intake.CompanionKey)),
			valueOrNone(intake.ProjectKey),
		))
	}

	lines = append(lines, "")
	lines = append(lines, "Automation Triggers")
	lines = append(lines, fmt.Sprintf(
		"  wiring=%s count=%d",
		valueOrNone(string(view.AutomationTriggers.Wiring)),
		len(view.AutomationTriggers.Items),
	))
	if len(view.AutomationTriggers.Items) == 0 {
		lines = append(lines, "  none")
	}
	for _, trigger := range view.AutomationTriggers.Items {
		lines = append(lines, fmt.Sprintf(
			"  trigger=%d title=%s status=%s due_status=%s initiative=%s companion=%s target_project=%s next_due_at=%s",
			trigger.TriggerID,
			valueOrNone(trigger.Title),
			valueOrNone(trigger.Status),
			valueOrNone(trigger.DueStatus),
			valueOrNone(ptrValue(trigger.InitiativeKey)),
			valueOrNone(ptrValue(trigger.CompanionKey)),
			valueOrNone(trigger.TargetProjectKey),
			valueOrNone(trigger.NextDueAt),
		))
	}

	lines = append(lines, "")
	lines = append(lines, "Compatibility")
	lines = append(lines, "  project -> initiative")
	lines = append(lines, "  task -> work item")
	lines = append(lines, "  run -> run attempt")
	lines = append(lines, "  agent -> agent definition or worker alias")

	return strings.Join(lines, "\n")
}

func countBlockedSwarms(swarms []overview.CompanionSwarmSummary) int {
	count := 0
	for _, swarm := range swarms {
		if strings.TrimSpace(swarm.BlockedReason) != "" {
			count++
		}
	}
	return count
}

func capabilityTruthItemsForText(items []overview.CapabilityTruthSummary) []overview.CapabilityTruthSummary {
	const limit = 20
	if len(items) <= limit {
		return items
	}

	selected := make([]overview.CapabilityTruthSummary, 0, limit)
	seen := make(map[string]struct{}, limit)
	appendIf := func(item overview.CapabilityTruthSummary, include bool) {
		if !include || len(selected) >= limit {
			return
		}
		key := item.Kind + "\x00" + item.Key
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		selected = append(selected, item)
	}

	for _, item := range items {
		appendIf(item, item.CountsAsImplemented)
	}
	for _, item := range items {
		appendIf(item, item.HighRisk)
	}
	for _, item := range items {
		appendIf(item, true)
	}

	return selected
}

func ptrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableInt64(value *int64) string {
	if value == nil {
		return "none"
	}
	return fmt.Sprintf("%d", *value)
}

func valueOrNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "none"
	}
	return value
}
