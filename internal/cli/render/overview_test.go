package render

import (
	"strings"
	"testing"

	"odin-os/internal/cli/overview"
)

func TestRenderOverviewUsesCanonicalLanes(t *testing.T) {
	t.Parallel()

	rendered := RenderOverview(sampleOverview())

	for _, want := range []string{
		"Attention",
		"Active Execution",
		"approval=1 work_item=alpha-task",
		"run=7 status=pending resolver=unsupported",
		"incident work_item=alpha-task",
		"Workspace",
		"Initiatives",
		"Work Items",
		"Run Attempts",
		"initiative=alpha",
		"companion=primary",
		"run=7 work_item=alpha-task project=alpha initiative=none companion=primary executor=codex status=running attempt=1",
		"swarm=alpha-task project=alpha companion=primary status=running active_children=1 backlog=0",
		"Companions",
		"Capability Catalog",
		"Approvals",
		"Observability",
		"Memory",
		"Intake Inbox",
		"wiring=not_yet_wired source=task_intakes status=linked_evidence count=1",
		"raw Intake Item authority not implemented",
		"linked_intake=3 source=n8n type=ci_failure dedup_key=ci_failure:alpha:42 requested_by=n8n work_item=alpha-task work_status=blocked initiative=alpha companion=primary project=alpha",
		"Automation Triggers",
		"wiring=live count=1",
		"trigger=42 title=Review automation trigger lane status=active due_status=due initiative=alpha companion=primary target_project=alpha next_due_at=2026-04-25T09:00:00Z",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderOverview() = %q, want substring %q", rendered, want)
		}
	}
	if strings.Contains(rendered, "Processes") {
		t.Fatalf("RenderOverview() = %q, must not introduce Processes lane", rendered)
	}
	approvalRow := "approval=1 work_item=alpha-task project=alpha companion=primary run=7 status=pending resolver=unsupported"
	if got := strings.Count(rendered, approvalRow); got != 2 {
		t.Fatalf("approval row count = %d, want 2 in Attention and Approvals lanes\n%s", got, rendered)
	}
}

func sampleOverview() overview.View {
	runID := int64(7)
	owner := "primary"
	project := "alpha"

	return overview.View{
		Workspace: overview.WorkspaceLane{
			Wiring:               overview.WiringLive,
			WorkspaceKey:         "default",
			Name:                 "Default Workspace",
			Status:               "active",
			ControlScope:         "global",
			DefaultCompanionKey:  "primary",
			InitiativeCount:      1,
			CompanionCount:       1,
			OpenWorkItemCount:    1,
			ActiveRunCount:       1,
			PendingApprovalCount: 1,
			BlockedWorkItemCount: 1,
		},
		Initiatives: []overview.InitiativeSummary{
			{
				InitiativeKey:        "alpha",
				Title:                "Alpha",
				Kind:                 "managed_project",
				Status:               "active",
				OwnerCompanionKey:    &owner,
				LinkedProjectKey:     &project,
				OpenWorkItemCount:    1,
				ActiveRunCount:       1,
				PendingApprovalCount: 1,
				BlockedWorkItemCount: 1,
			},
		},
		WorkItems: []overview.WorkItemSummary{
			{
				ProjectKey:       "alpha",
				InitiativeKey:    &project,
				CompanionKey:     &owner,
				WorkItemKey:      "alpha-task",
				Title:            "Alpha task",
				Status:           "blocked",
				Scope:            "project",
				CurrentRunID:     &runID,
				CurrentRunStatus: "running",
				RunAttempts: []overview.RunAttemptSummary{
					{
						RunID:        7,
						WorkItemKey:  "alpha-task",
						ProjectKey:   "alpha",
						CompanionKey: &owner,
						Executor:     "codex",
						Status:       "running",
						Attempt:      1,
					},
				},
			},
		},
		Companions: overview.CompanionLane{
			Wiring: overview.WiringLive,
			Items: []overview.CompanionSummary{
				{
					CompanionKey:         "primary",
					Title:                "Primary Assistant",
					Kind:                 "assistant",
					Status:               "active",
					OwnedInitiativeCount: 1,
					OpenWorkItemCount:    1,
					ActiveRunCount:       1,
					PendingApprovalCount: 1,
					BlockedWorkItemCount: 1,
				},
			},
		},
		CapabilityCatalog: overview.CapabilityCatalogLane{
			Wiring:               overview.WiringCatalogBacked,
			AgentDefinitionCount: 1,
			SkillCount:           1,
			WorkflowCount:        1,
			CommandCount:         1,
			ToolCount:            4,
		},
		Approvals: []overview.ApprovalSummary{
			{
				ApprovalID:      1,
				RunID:           &runID,
				ProjectKey:      "alpha",
				CompanionKey:    &owner,
				WorkItemKey:     "alpha-task",
				Status:          "pending",
				RequestedAt:     "2026-04-23T00:00:00Z",
				ResolverSupport: "unsupported",
			},
		},
		Observability: overview.ObservabilityLane{
			Wiring: overview.WiringLive,
			ActiveRuns: []overview.RunAttemptSummary{
				{
					RunID:        7,
					WorkItemKey:  "alpha-task",
					ProjectKey:   "alpha",
					CompanionKey: &owner,
					Executor:     "codex",
					Status:       "running",
					Attempt:      1,
				},
			},
			BlockedWork: []overview.BlockedWorkSummary{
				{
					WorkItemKey:  "alpha-task",
					ProjectKey:   "alpha",
					CompanionKey: &owner,
					Source:       "approval",
					Reason:       "pending",
				},
			},
			Incidents: []overview.IncidentSummary{
				{
					IncidentID:   1,
					WorkItemKey:  "alpha-task",
					ProjectKey:   "alpha",
					CompanionKey: &owner,
					Severity:     "warning",
					Status:       "open",
					Summary:      "Browser verification paused",
				},
			},
			Recoveries: []overview.RecoverySummary{
				{
					RecoveryID: 1,
					RunID:      7,
					Status:     "running",
					Strategy:   "self_heal",
				},
			},
		},
		CompanionSwarms: []overview.CompanionSwarmSummary{
			{
				ParentTaskKey:       "alpha-task",
				ProjectKey:          "alpha",
				CompanionKey:        &owner,
				Status:              "running",
				ActiveChildRunCount: 1,
				BacklogCount:        0,
			},
		},
		Memory: overview.MemoryLane{
			Wiring: overview.WiringLive,
			Count:  1,
			Recent: []overview.MemorySummary{
				{
					ID:         1,
					MemoryType: "operator_note",
					Scope:      "global",
					ScopeKey:   "global",
					Summary:    "Remember this overview state",
				},
			},
		},
		IntakeInbox: overview.IntakeInboxLane{
			Wiring: overview.WiringNotYetWired,
			Source: "task_intakes",
			Status: "linked_evidence",
			Note:   "task_intakes are linked intake evidence; raw Intake Item authority not implemented",
			Items: []overview.IntakeEvidenceSummary{
				{
					IntakeID:       3,
					Source:         "n8n",
					IntakeType:     "ci_failure",
					DedupKey:       "ci_failure:alpha:42",
					RequestedBy:    "n8n",
					ProjectKey:     "alpha",
					InitiativeKey:  &project,
					CompanionKey:   &owner,
					WorkItemKey:    "alpha-task",
					WorkItemStatus: "blocked",
				},
			},
		},
		AutomationTriggers: overview.AutomationTriggerLane{
			Wiring: overview.WiringLive,
			Items: []overview.AutomationTriggerSummary{
				{
					TriggerID:        42,
					InitiativeKey:    &project,
					CompanionKey:     &owner,
					TargetProjectKey: "alpha",
					Title:            "Review automation trigger lane",
					Status:           "active",
					DueStatus:        "due",
					NextDueAt:        "2026-04-25T09:00:00Z",
				},
			},
		},
	}
}
