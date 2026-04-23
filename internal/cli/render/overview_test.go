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
		"Workspace",
		"Initiatives",
		"Work Items",
		"Run Attempts",
		"initiative=alpha",
		"run=7 executor=codex status=running attempt=1",
		"Companions",
		"Capability Catalog",
		"Approvals",
		"Observability",
		"Memory",
		"Intake Inbox",
		"Automation Triggers",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderOverview() = %q, want substring %q", rendered, want)
		}
	}
	if strings.Contains(rendered, "Processes") {
		t.Fatalf("RenderOverview() = %q, must not introduce Processes lane", rendered)
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
				WorkItemKey:      "alpha-task",
				Title:            "Alpha task",
				Status:           "blocked",
				Scope:            "project",
				CurrentRunID:     &runID,
				CurrentRunStatus: "running",
				RunAttempts: []overview.RunAttemptSummary{
					{
						RunID:       7,
						WorkItemKey: "alpha-task",
						ProjectKey:  "alpha",
						Executor:    "codex",
						Status:      "running",
						Attempt:     1,
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
				ApprovalID:  1,
				WorkItemKey: "alpha-task",
				Status:      "pending",
				RequestedAt: "2026-04-23T00:00:00Z",
			},
		},
		Observability: overview.ObservabilityLane{
			Wiring: overview.WiringLive,
			ActiveRuns: []overview.RunAttemptSummary{
				{
					RunID:       7,
					WorkItemKey: "alpha-task",
					ProjectKey:  "alpha",
					Executor:    "codex",
					Status:      "running",
					Attempt:     1,
				},
			},
			BlockedWork: []overview.BlockedWorkSummary{
				{
					WorkItemKey: "alpha-task",
					ProjectKey:  "alpha",
					Source:      "approval",
					Reason:      "pending",
				},
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
		IntakeInbox: overview.PlaceholderLane{
			Wiring: overview.WiringNotYetWired,
			Status: "unavailable",
			Note:   "intake overview projection not implemented",
		},
		AutomationTriggers: overview.PlaceholderLane{
			Wiring: overview.WiringNotYetWired,
			Status: "unavailable",
			Note:   "automation trigger overview projection not implemented",
		},
	}
}
