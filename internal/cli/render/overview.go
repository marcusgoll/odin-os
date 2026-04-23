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
				"  %s title=%s project=%s status=%s scope=%s current_run=%s run_status=%s",
				valueOrNone(item.WorkItemKey),
				valueOrNone(item.Title),
				valueOrNone(item.ProjectKey),
				valueOrNone(item.Status),
				valueOrNone(item.Scope),
				nullableInt64(item.CurrentRunID),
				valueOrNone(item.CurrentRunStatus),
			))
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
				"  approval=%d work_item=%s status=%s requested_at=%s",
				approval.ApprovalID,
				valueOrNone(approval.WorkItemKey),
				valueOrNone(approval.Status),
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
			"    run=%d work_item=%s project=%s executor=%s status=%s attempt=%d",
			run.RunID,
			valueOrNone(run.WorkItemKey),
			valueOrNone(run.ProjectKey),
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
			"    blocked=%s project=%s source=%s reason=%s",
			valueOrNone(blocked.WorkItemKey),
			valueOrNone(blocked.ProjectKey),
			valueOrNone(blocked.Source),
			valueOrNone(blocked.Reason),
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
