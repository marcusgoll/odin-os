package overview

import (
	"strings"
	"testing"
	"time"

	"odin-os/internal/registry"
	"odin-os/internal/runtime/reviewqueue"
	"odin-os/internal/store/sqlite"
	toolcatalog "odin-os/internal/tools/catalog"
)

func TestReadinessLaneTreatsMissingInputsAsUnknown(t *testing.T) {
	lane := BuildReadiness(ReadinessInput{})

	if lane.Wiring != WiringLive {
		t.Fatalf("Wiring = %q, want live", lane.Wiring)
	}
	if lane.Status != "unknown" || lane.HealthStatus != "unknown" || lane.Ready {
		t.Fatalf("Readiness = %+v, want unknown/unknown and not ready", lane)
	}
	if !strings.Contains(lane.Note, "unknown is not treated as healthy") {
		t.Fatalf("Readiness note = %q, want unknown-not-healthy warning", lane.Note)
	}
}

func TestCapabilityTruthSeparatesAuthoredInventoryFromRuntimeProof(t *testing.T) {
	snapshot := registry.Snapshot{Items: []registry.Item{
		{
			Kind:  registry.KindAgent,
			Key:   "delegate-agent",
			Title: "Delegate Agent",
			Delegation: registry.DelegationProfile{
				Enabled:         true,
				OperatorSurface: "companion delegate",
			},
		},
		{
			Kind:  registry.KindWorkflow,
			Key:   "publish-workflow",
			Title: "Publish Workflow",
			Tags:  []string{"publish"},
		},
	}}
	tools := map[string]toolcatalog.ToolDefinition{
		"browser-visible-evidence": {
			Key:   "browser-visible-evidence",
			Title: "Browser Visible Evidence",
			Tags:  []string{"browser", "visible_evidence"},
		},
	}

	lane := BuildCapabilityTruth(CapabilityCatalogLane{
		AgentDefinitionCount: 1,
		WorkflowCount:        1,
		ToolCount:            1,
	}, snapshot, tools)

	if lane.AuthoredAssetCount != 3 {
		t.Fatalf("AuthoredAssetCount = %d, want 3", lane.AuthoredAssetCount)
	}
	if lane.RuntimeProvenCount != 1 {
		t.Fatalf("RuntimeProvenCount = %d, want delegate agent proven", lane.RuntimeProvenCount)
	}
	if lane.HighRiskFamilyCount != 2 {
		t.Fatalf("HighRiskFamilyCount = %d, want workflow and browser tool marked", lane.HighRiskFamilyCount)
	}
	if lane.Items[0].Key != "delegate-agent" || !lane.Items[0].CountsAsImplemented {
		t.Fatalf("first truth item = %+v, want delegate-agent counted as implemented", lane.Items[0])
	}
}

func TestReviewQueueLaneUsesSharedProjectionCounts(t *testing.T) {
	projection := reviewqueue.Project([]reviewqueue.Entry{
		{SourceType: "intake_review"},
		{SourceType: "goal"},
		{SourceType: "task_approval"},
		{SourceType: "context_pack"},
		{SourceType: "skill_artifact"},
		{SourceType: "memory_proposal"},
		{SourceType: "recovery"},
		{SourceType: "failed_work"},
	})

	lane := BuildReviewQueue(projection)

	if lane.TotalCount != 8 || lane.IntakeCount != 1 || lane.GoalCount != 1 || lane.MemoryProposalCount != 1 || lane.RecoveryCount != 1 {
		t.Fatalf("ReviewQueue lane = %+v, want shared projection counts", lane)
	}
}

func TestRawIntakeSummaryKeepsRawIntakeBoundedToInboxProjection(t *testing.T) {
	item := sqlite.IntakeItem{
		ID:              7,
		SourceFamily:    "operator",
		EventKind:       "request",
		DedupeKey:       "dedupe-7",
		SourceFactsJSON: `{"requested_by":"operator"}`,
		Subject:         "Review intake",
		Status:          "approval_required",
		Summary:         "Review intake summary",
		Scope:           "project",
		ScopeKey:        "alpha",
		CreatedAt:       time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 5, 14, 12, 1, 0, 0, time.UTC),
	}

	summary := RawIntakeSummaryFromItem(item)

	if summary.Key != "intake-7" || summary.ProjectKey != "alpha" || summary.RequestedBy != "operator" {
		t.Fatalf("RawIntakeSummary = %+v, want stable intake key, project, and requested_by", summary)
	}
	if !IsReviewableIntakeStatus(item.Status) {
		t.Fatalf("IsReviewableIntakeStatus(%q) = false, want true", item.Status)
	}
	if IntakeLaneStatus(IntakeStatusInput{IntakeApprovalRequiredCount: 1, ReviewQueueCount: 1}) != "approval_pending" {
		t.Fatalf("IntakeLaneStatus approval branch did not win")
	}
}
