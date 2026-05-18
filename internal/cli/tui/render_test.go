package tui

import (
	"fmt"
	"strings"
	"testing"
)

func TestRenderOverviewShowsUnknownWhenTelemetryIsStale(t *testing.T) {
	t.Parallel()

	output := RenderOverview(Model{
		TelemetryAvailable: true,
		Status:             "healthy",
		HealthScore:        99,
		TelemetryStale:     true,
	})
	if !strings.Contains(output, "│ HEALTH        UNKNOWN") {
		t.Fatalf("output = %q, want UNKNOWN", output)
	}
}

func TestRenderOverviewStableTextOutput(t *testing.T) {
	t.Parallel()

	output := RenderOverview(Model{
		TelemetryAvailable: true,
		Status:             "degraded",
		HealthScore:        87,
		TelemetryStale:     false,
		LifecyclePhase:     "run",
		ActiveRuns:         3,
		Logs: []LogEntry{
			{Timestamp: "1714521600000000000", Line: `{"level":"info","message":"ready"}`},
		},
	})

	for _, want := range []string{
		"┌─ ODIN HEALTH ",
		"│ HEALTH        DEGRADED",
		"│ SCORE         87",
		"│ TELEMETRY     fresh",
		"│ PHASE         run",
		"│ ACTIVE RUNS   3",
		`{"level":"info","message":"ready"}`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want %q", output, want)
		}
	}
}

func TestRenderOverviewShowsActionRequiredPanel(t *testing.T) {
	t.Parallel()

	output := RenderOverview(Model{
		TelemetryAvailable:      true,
		Status:                  "degraded",
		HealthScore:             87,
		LifecyclePhase:          "run",
		ActiveRuns:              3,
		BlockedItems:            2,
		ApprovalsWaiting:        4,
		ReviewQueueItems:        6,
		FailedWorkItems:         1,
		RecoveryRecommendations: 1,
	})

	for _, want := range []string{
		"┌─ ACTION REQUIRED ",
		"│ APPROVALS     4",
		"│ BLOCKED       2",
		"│ REVIEW QUEUE  6",
		"│ FAILED WORK   1",
		"│ RECOVERY      1",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want action-required fragment %q", output, want)
		}
	}
}

func TestRenderOverviewShowsCommandCenterPanels(t *testing.T) {
	t.Parallel()

	output := RenderOverview(Model{
		TelemetryAvailable: true,
		Status:             "healthy",
		HealthScore:        92,
		LifecyclePhase:     "run",
		ActiveRuns:         1,
		ActionRequired: []SnapshotRow{
			{
				ID:       "approval:7",
				Label:    "Approval alpha",
				Summary:  "Approve deployment alpha",
				Severity: "warning",
				Command:  "odin approvals show 7",
			},
		},
		LiveExecution: []SnapshotRow{
			{
				ID:       "run:9",
				Label:    "Run 9",
				Summary:  "work-alpha attempt 1 is running on codex",
				Severity: "info",
				Command:  "odin runs show 9",
			},
		},
		Activity: []SnapshotRow{
			{
				ID:       "event:12",
				Label:    "approval.requested",
				Summary:  "Approval requested for alpha",
				Severity: "info",
				Command:  "odin logs show 12",
			},
		},
	})

	for _, want := range []string{
		"┌─ ACTION REQUIRED ",
		"Approval alpha",
		"odin approvals show 7",
		"┌─ ODIN HEALTH ",
		"┌─ LIVE EXECUTION ",
		"work-alpha attempt 1 is running on codex",
		"odin runs show 9",
		"┌─ ACTIVITY ",
		"approval.requested",
		"odin logs show 12",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want command-center fragment %q", output, want)
		}
	}
}

func TestRenderOverviewPreservesSnapshotIDsWithLongMultiruneLabels(t *testing.T) {
	t.Parallel()

	output := RenderOverview(Model{
		TelemetryAvailable: true,
		Status:             "healthy",
		HealthScore:        92,
		ActionRequired: []SnapshotRow{
			{
				ID:       "approval:123456789",
				Label:    "承認が必要な非常に長いラベル Approval alpha needs a long operator-facing description",
				Summary:  "Approval summary",
				Severity: "warning",
			},
		},
		LiveExecution: []SnapshotRow{
			{
				ID:      "run:987654321",
				Label:   "実行中の非常に長いラベル Run beta has a long operator-facing description",
				Summary: "Run summary",
			},
		},
	})

	for _, want := range []string{
		"id=approval:123456789",
		"id=run:987654321",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want stable ID %q visible at default width", output, want)
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if visibleLen(line) > defaultRenderWidth {
			t.Fatalf("line width = %d, want <= %d: %q", visibleLen(line), defaultRenderWidth, line)
		}
	}
}

func TestRenderOverviewPreservesSnapshotCommandHintsWithLongLabels(t *testing.T) {
	t.Parallel()

	output := RenderOverview(Model{
		TelemetryAvailable: true,
		Status:             "healthy",
		HealthScore:        92,
		ActionRequired: []SnapshotRow{
			{
				ID:       "approval:2468",
				Label:    "Long approval label that would otherwise consume nearly the entire boxed terminal row",
				Summary:  "Approval summary",
				Severity: "warning",
				Command:  "odin approvals show 2468",
			},
		},
		Activity: []SnapshotRow{
			{
				ID:      "event:1357",
				Label:   "Long activity label that would otherwise hide the operator inspection command",
				Summary: "Activity summary",
				Command: "odin logs show 1357",
			},
		},
	})

	for _, want := range []string{
		"inspect=odin approvals show 2468",
		"inspect=odin logs show 1357",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want command hint %q visible at default width", output, want)
		}
	}
}

func TestRenderOverviewUsesBoxedCockpitLayout(t *testing.T) {
	t.Parallel()

	output := RenderOverview(Model{
		TelemetryAvailable: true,
		Status:             "degraded",
		HealthScore:        87,
		TelemetryStale:     false,
		LifecyclePhase:     "run",
		ActiveRuns:         3,
		Logs: []LogEntry{
			{Timestamp: "1714521600000000000", Line: `{"level":"info","message":"ready"}`},
		},
	})

	for _, want := range []string{
		"┌─ ODIN HEALTH ",
		"│ HEALTH        DEGRADED",
		"│ SCORE         87",
		"│ TELEMETRY     fresh",
		"│ PHASE         run",
		"┌─ RECENT LOGS ",
		"│ 1714521600000000000  {\"level\":\"info\",\"message\":\"ready\"}",
		"└",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want boxed cockpit fragment %q", output, want)
		}
	}
}

func TestRenderOverviewShowsUnavailableLogs(t *testing.T) {
	t.Parallel()

	output := RenderOverview(Model{
		TelemetryAvailable: true,
		Status:             "healthy",
		HealthScore:        100,
		LifecyclePhase:     "run",
		LogsUnavailable:    "loki query failed",
	})
	if !strings.Contains(output, "┌─ RECENT LOGS ") ||
		!strings.Contains(output, "│ Loki unavailable - runtime panels continue from store projections") ||
		!strings.Contains(output, "│ unavailable: loki query failed") {
		t.Fatalf("output = %q, want unavailable logs", output)
	}
}

func TestRenderOverviewUsesResponsiveColumnsOnWideTerminals(t *testing.T) {
	t.Parallel()

	output := RenderOverviewForTerminal(Model{
		Name:               "Odin Core",
		TelemetryAvailable: true,
		Status:             "degraded",
		HealthScore:        87,
		LifecyclePhase:     "run",
		BlockedItems:       2,
		Agents: []AgentRow{
			{Name: "codex", Task: "goal-7", Project: "odin-os", Status: "running"},
		},
		Goals: []GoalRow{
			{ID: 7, Title: "Keep overview visible", Status: "running"},
		},
	}, 140, false)

	if !strings.Contains(output, "┐  ┌─ ODIN HEALTH ") {
		t.Fatalf("output = %q, want side-by-side action and health panels", output)
	}
	if !strings.Contains(output, "┐  ┌─ CURRENT GOALS ") {
		t.Fatalf("output = %q, want side-by-side agents and goals panels", output)
	}
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if visibleLen(line) > 140 {
			t.Fatalf("line width = %d, want <= 140: %q", visibleLen(line), line)
		}
	}
}

func TestRenderOverviewAddsColorForTerminalOutput(t *testing.T) {
	t.Parallel()

	output := RenderOverviewForTerminal(Model{
		TelemetryAvailable: true,
		Status:             "healthy",
		HealthScore:        100,
		LifecyclePhase:     "run",
	}, 76, true)

	for _, want := range []string{
		ansiCyan + "ODIN HEALTH" + ansiReset,
		ansiGreen + "HEALTHY" + ansiReset,
		ansiGreen + "100" + ansiReset,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want color fragment %q", output, want)
		}
	}
}

func TestRenderOverviewShowsVisualDeliveryCockpitPanels(t *testing.T) {
	t.Parallel()

	output := RenderOverview(Model{
		Name:               "Odin Core",
		TelemetryAvailable: true,
		Status:             "healthy",
		HealthScore:        100,
		LifecyclePhase:     "run",
		Agents: []AgentRow{
			{Name: "codex", Task: "visual-tui", Project: "odin-os", Status: "running"},
		},
		Flows: []FlowRow{
			{Direction: "IN", Ref: "intake#8", Source: "mobile/share", Status: "received", Subject: "Review captured request"},
			{Direction: "OUT", Ref: "run#12", Source: "codex_headless", Status: "completed", Subject: "Opened PR 42"},
		},
		Goals: []GoalRow{
			{ID: 7, Title: "Ship visual TUI", Status: "running", CurrentRun: "12"},
		},
		Schedules: []ScheduleRoutineRow{
			{
				Source:         "schedule",
				Key:            "daily-proof",
				Project:        "odin-core",
				Status:         "enabled",
				DueStatus:      "waiting",
				NextDueAt:      "2026-05-17T15:00:00Z",
				LastRanAt:      "2026-05-17T00:02:14Z",
				LastWorkItem:   "automation-daily-proof",
				LastWorkStatus: "blocked",
				LastWorkDetail: "previous service instance stopped during execution",
				LastWorkReview: "failed-work:158",
			},
		},
		PullRequests: []PullRequestRow{
			{Project: "odin-os", Repo: "owner/odin-os", Number: 276, Title: "Visual TUI", State: "open", CI: "not_wired"},
		},
		Approvals: []ApprovalRow{
			{ID: 3, Task: "visual-tui", Project: "odin-os", Status: "pending", Resolver: "ok"},
		},
	})

	for _, want := range []string{
		"│ NAME          Odin Core",
		"┌─ INBOX / OUTBOX ",
		"│ IN intake#8 source=mobile/share status=received subject=Review captur...",
		"│ OUT run#12 source=codex_headless status=completed subject=Opened PR 42",
		"┌─ AGENTS RUNNING ",
		"│ codex task=visual-tui project=odin-os status=running",
		"┌─ CURRENT GOALS ",
		"│ goal=7 status=running run=12 title=Ship visual TUI",
		"┌─ SCHEDULES + ROUTINES ",
		"│ schedule=daily-proof",
		"│   project=odin-core status=enabled due=waiting",
		"│   next=2026-05-17T15:00:00Z last_run=2026-05-17T00:02:14Z",
		"│   work_status=blocked detail=previous service instance stopped during...",
		"│   review=odin review show failed-work:158",
		"│   retry=odin review act failed-work:158 retry",
		"┌─ PROJECT PRS + CI ",
		"│ odin-os owner/odin-os#276 state=open ci=not_wired title=Visual TUI",
		"┌─ APPROVALS WAITING ",
		"│ approval=3 task=visual-tui project=odin-os status=pending resolver=ok",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want cockpit fragment %q", output, want)
		}
	}
}

func TestRenderOverviewCapsScheduleRows(t *testing.T) {
	t.Parallel()

	model := Model{TelemetryAvailable: true, Status: "healthy", HealthScore: 100}
	for index := 1; index <= 8; index++ {
		model.Schedules = append(model.Schedules, ScheduleRoutineRow{
			Source:    "schedule",
			Key:       fmt.Sprintf("routine-%d", index),
			Project:   "odin-core",
			Status:    "enabled",
			DueStatus: "waiting",
		})
	}

	output := RenderOverview(model)
	if !strings.Contains(output, "│ schedule=routine-6") {
		t.Fatalf("output = %q, want sixth schedule visible", output)
	}
	if strings.Contains(output, "routine-7") {
		t.Fatalf("output = %q, did not expect seventh schedule detail", output)
	}
	if !strings.Contains(output, "│ ... 2 more") {
		t.Fatalf("output = %q, want remaining schedule count", output)
	}
}
