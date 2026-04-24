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
		"  approvals=%d incidents=%d blocked_work=%d recoveries=%d blocked_swarms=%d",
		len(view.Approvals),
		len(view.Observability.Incidents),
		len(view.Observability.BlockedWork),
		len(view.Observability.Recoveries),
		countBlockedSwarms(view.CompanionSwarms),
	))
	if len(view.Approvals) == 0 && len(view.Observability.Incidents) == 0 && len(view.Observability.BlockedWork) == 0 && len(view.Observability.Recoveries) == 0 && countBlockedSwarms(view.CompanionSwarms) == 0 {
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
		"  wiring=%s active_runs=%d blocked_work=%d incidents=%d recoveries=%d freshness=%d",
		valueOrNone(string(view.Observability.Wiring)),
		len(view.Observability.ActiveRuns),
		len(view.Observability.BlockedWork),
		len(view.Observability.Incidents),
		len(view.Observability.Recoveries),
		len(view.Observability.Freshness),
	))
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
	lines = append(lines, "  Incidents")
	if len(view.Observability.Incidents) == 0 {
		lines = append(lines, "    none")
	}
	for _, incident := range view.Observability.Incidents {
		lines = append(lines, fmt.Sprintf(
			"    incident=%d work_item=%s project=%s companion=%s severity=%s status=%s summary=%s",
			incident.IncidentID,
			valueOrNone(incident.WorkItemKey),
			valueOrNone(incident.ProjectKey),
			valueOrNone(ptrValue(incident.CompanionKey)),
			valueOrNone(incident.Severity),
			valueOrNone(incident.Status),
			valueOrNone(incident.Summary),
		))
	}
	lines = append(lines, "  Recoveries")
	if len(view.Observability.Recoveries) == 0 {
		lines = append(lines, "    none")
	}
	for _, recovery := range view.Observability.Recoveries {
		lines = append(lines, fmt.Sprintf(
			"    recovery=%d run=%d status=%s strategy=%s started_at=%s",
			recovery.RecoveryID,
			recovery.RunID,
			valueOrNone(recovery.Status),
			valueOrNone(recovery.Strategy),
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
		"  wiring=%s status=%s note=%s",
		valueOrNone(string(view.IntakeInbox.Wiring)),
		valueOrNone(view.IntakeInbox.Status),
		valueOrNone(view.IntakeInbox.Note),
	))

	lines = append(lines, "")
	lines = append(lines, "Automation Triggers")
	lines = append(lines, fmt.Sprintf(
		"  wiring=%s status=%s note=%s",
		valueOrNone(string(view.AutomationTriggers.Wiring)),
		valueOrNone(view.AutomationTriggers.Status),
		valueOrNone(view.AutomationTriggers.Note),
	))

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
