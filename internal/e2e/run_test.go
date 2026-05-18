package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestFixtureSetRunsLocallyWithoutLiveSystems(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	fixtures := []string{
		"github-readonly-intake.yaml",
		"github-issue-delivery-dry-run.yaml",
		"raw-intake-delivery-dry-run.yaml",
		"tracker-dry-run-lifecycle.yaml",
		"workspace-safe-creation.yaml",
		"prompt-rendering-brownfield.yaml",
		"failure-analysis.yaml",
		"software-factory-lane.yaml",
	}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer
			err := Run(context.Background(), repoRoot, []string{
				"--scenario", filepath.Join("fixtures", "e2e", fixture),
				"--json",
			}, &stdout)
			if err != nil {
				t.Fatalf("Run(%s) error = %v\noutput:\n%s", fixture, err, stdout.String())
			}

			var report struct {
				Status   string `json:"status"`
				Scenario string `json:"scenario"`
				OdinRoot string `json:"odin_root"`
				Stages   []struct {
					Name   string `json:"name"`
					Status string `json:"status"`
				} `json:"stages"`
				GitHub struct {
					Mode    string `json:"mode"`
					Mutated bool   `json:"mutated"`
				} `json:"github"`
				Codex struct {
					Mode    string `json:"mode"`
					Invoked bool   `json:"invoked"`
				} `json:"codex"`
				Intake struct {
					RawIntakeKey      string `json:"raw_intake_key"`
					RawStatus         string `json:"raw_status"`
					RawSource         string `json:"raw_source"`
					RawIntakeType     string `json:"raw_intake_type"`
					RoutedWorkItemKey string `json:"routed_work_item_key"`
				} `json:"intake"`
				Workspace struct {
					SessionName       string `json:"session_name"`
					WorkspaceState    string `json:"workspace_state"`
					WorkspaceAttached int    `json:"workspace_attached"`
				} `json:"workspace"`
				Delivery struct {
					WorkItemKey                        string   `json:"work_item_key"`
					HandoffID                          int64    `json:"handoff_id"`
					HandoffReviewState                 string   `json:"handoff_review_state"`
					HandoffReviewRoles                 []string `json:"handoff_review_roles"`
					HandoffReviewResults               []string `json:"handoff_review_results"`
					ApprovalID                         int64    `json:"approval_id"`
					ApprovalStatus                     string   `json:"approval_status"`
					ApprovalResolverSupport            string   `json:"approval_resolver_support"`
					ApprovalTaskStatusBeforeResolution string   `json:"approval_task_status_before_resolution"`
					ApprovalTaskStatusAfterResolution  string   `json:"approval_task_status_after_resolution"`
					ApprovalTaskBlockedReason          string   `json:"approval_task_blocked_reason"`
					MergeVerified                      bool     `json:"merge_verified"`
					MergeTaskStatus                    string   `json:"merge_task_status"`
				} `json:"delivery"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
				t.Fatalf("json.Unmarshal(%s) error = %v\noutput:\n%s", fixture, err, stdout.String())
			}
			if report.Status != "passed" {
				t.Fatalf("report.Status = %q, want passed\noutput:\n%s", report.Status, stdout.String())
			}
			if report.OdinRoot == "" || filepath.Clean(report.OdinRoot) == repoRoot {
				t.Fatalf("odin_root = %q, want temp root distinct from repo root %q", report.OdinRoot, repoRoot)
			}
			if report.GitHub.Mode != "fixture" || report.GitHub.Mutated {
				t.Fatalf("github guard = %+v, want fixture and not mutated", report.GitHub)
			}
			if report.Codex.Mode != "disabled" || report.Codex.Invoked {
				t.Fatalf("codex guard = %+v, want disabled and not invoked", report.Codex)
			}
			for _, stage := range report.Stages {
				if stage.Name == "" || stage.Status != "passed" {
					t.Fatalf("stage = %+v, want named passed stage", stage)
				}
			}
			if fixture == "github-issue-delivery-dry-run.yaml" {
				assertDeliveryDryRunLoop(t, report.Workspace, report.Delivery)
			}
			if fixture == "raw-intake-delivery-dry-run.yaml" {
				assertRawIntakeDryRunLoop(t, report.Intake, report.Workspace, report.Delivery)
			}
		})
	}
}

func assertRawIntakeDryRunLoop(t *testing.T, intake struct {
	RawIntakeKey      string `json:"raw_intake_key"`
	RawStatus         string `json:"raw_status"`
	RawSource         string `json:"raw_source"`
	RawIntakeType     string `json:"raw_intake_type"`
	RoutedWorkItemKey string `json:"routed_work_item_key"`
}, workspace struct {
	SessionName       string `json:"session_name"`
	WorkspaceState    string `json:"workspace_state"`
	WorkspaceAttached int    `json:"workspace_attached"`
}, delivery struct {
	WorkItemKey                        string   `json:"work_item_key"`
	HandoffID                          int64    `json:"handoff_id"`
	HandoffReviewState                 string   `json:"handoff_review_state"`
	HandoffReviewRoles                 []string `json:"handoff_review_roles"`
	HandoffReviewResults               []string `json:"handoff_review_results"`
	ApprovalID                         int64    `json:"approval_id"`
	ApprovalStatus                     string   `json:"approval_status"`
	ApprovalResolverSupport            string   `json:"approval_resolver_support"`
	ApprovalTaskStatusBeforeResolution string   `json:"approval_task_status_before_resolution"`
	ApprovalTaskStatusAfterResolution  string   `json:"approval_task_status_after_resolution"`
	ApprovalTaskBlockedReason          string   `json:"approval_task_blocked_reason"`
	MergeVerified                      bool     `json:"merge_verified"`
	MergeTaskStatus                    string   `json:"merge_task_status"`
}) {
	t.Helper()

	if intake.RawIntakeKey != "intake-1" || intake.RawStatus != "routed" || intake.RawSource != "codex-cli" || intake.RawIntakeType != "prompt" {
		t.Fatalf("raw intake evidence = %+v, want routed codex-cli prompt intake-1", intake)
	}
	if intake.RoutedWorkItemKey != "raw-intake-1" || delivery.WorkItemKey != "raw-intake-1" {
		t.Fatalf("raw intake work item = %+v delivery=%+v, want raw-intake-1", intake, delivery)
	}
	assertDeliveryDryRunLoop(t, workspace, delivery)
}

func assertDeliveryDryRunLoop(t *testing.T, workspace struct {
	SessionName       string `json:"session_name"`
	WorkspaceState    string `json:"workspace_state"`
	WorkspaceAttached int    `json:"workspace_attached"`
}, delivery struct {
	WorkItemKey                        string   `json:"work_item_key"`
	HandoffID                          int64    `json:"handoff_id"`
	HandoffReviewState                 string   `json:"handoff_review_state"`
	HandoffReviewRoles                 []string `json:"handoff_review_roles"`
	HandoffReviewResults               []string `json:"handoff_review_results"`
	ApprovalID                         int64    `json:"approval_id"`
	ApprovalStatus                     string   `json:"approval_status"`
	ApprovalResolverSupport            string   `json:"approval_resolver_support"`
	ApprovalTaskStatusBeforeResolution string   `json:"approval_task_status_before_resolution"`
	ApprovalTaskStatusAfterResolution  string   `json:"approval_task_status_after_resolution"`
	ApprovalTaskBlockedReason          string   `json:"approval_task_blocked_reason"`
	MergeVerified                      bool     `json:"merge_verified"`
	MergeTaskStatus                    string   `json:"merge_task_status"`
}) {
	t.Helper()

	if workspace.SessionName == "" || workspace.WorkspaceState != "stopped" || workspace.WorkspaceAttached != 0 {
		t.Fatalf("workspace evidence = %+v, want stopped fixture session evidence", workspace)
	}
	if delivery.HandoffID <= 0 || delivery.HandoffReviewState != "review_selected" {
		t.Fatalf("handoff evidence = %+v, want persisted selected handoff", delivery)
	}
	wantRoles := []string{"reviewer", "qa", "security"}
	if !equalStrings(delivery.HandoffReviewRoles, wantRoles) {
		t.Fatalf("handoff roles = %#v, want %#v", delivery.HandoffReviewRoles, wantRoles)
	}
	if len(delivery.HandoffReviewResults) != len(wantRoles) {
		t.Fatalf("handoff review results = %#v, want one per role", delivery.HandoffReviewResults)
	}
	if delivery.ApprovalID <= 0 || delivery.ApprovalStatus != "approved" || delivery.ApprovalResolverSupport != "supported" {
		t.Fatalf("approval evidence = %+v, want supported approved resolver", delivery)
	}
	if delivery.ApprovalTaskStatusBeforeResolution != "blocked" || delivery.ApprovalTaskBlockedReason != "approval_required" || delivery.ApprovalTaskStatusAfterResolution != "queued" {
		t.Fatalf("approval task states = %+v, want blocked approval_required then queued", delivery)
	}
	if !delivery.MergeVerified || delivery.MergeTaskStatus != "completed" {
		t.Fatalf("merge evidence = %+v, want completed dry-run merge verification", delivery)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestSoftwareFactoryLaneScenario(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	var stdout bytes.Buffer
	err := Run(context.Background(), repoRoot, []string{
		"--scenario", filepath.Join("fixtures", "e2e", "software-factory-lane.yaml"),
		"--json",
	}, &stdout)
	if err != nil {
		t.Fatalf("Run(software-factory-lane) error = %v\noutput:\n%s", err, stdout.String())
	}

	var report struct {
		Status   string `json:"status"`
		Scenario string `json:"scenario"`
		Stages   []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Detail string `json:"detail"`
		} `json:"stages"`
		GitHub struct {
			Mode    string `json:"mode"`
			Mutated bool   `json:"mutated"`
		} `json:"github"`
		Delivery struct {
			WorkItemKey   string `json:"work_item_key"`
			RunID         int64  `json:"run_id"`
			PRReadyBranch string `json:"pr_ready_branch"`
		} `json:"delivery"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\noutput:\n%s", err, stdout.String())
	}
	if report.Status != "passed" || report.Scenario != "software-factory-lane" {
		t.Fatalf("report status/scenario = %q/%q\noutput:\n%s", report.Status, report.Scenario, stdout.String())
	}
	if report.GitHub.Mode != "fixture" || report.GitHub.Mutated {
		t.Fatalf("github = %+v, want fixture without mutation", report.GitHub)
	}
	if report.Delivery.WorkItemKey == "" || report.Delivery.RunID == 0 || report.Delivery.PRReadyBranch == "" {
		t.Fatalf("delivery = %+v, want factory work, run, and PR handoff", report.Delivery)
	}
	for _, want := range []string{
		"factory_operator_start",
		"intake_review_accept_factory",
		"review_pr_handoff",
		"factory_merge_gate",
		"work_status_readback",
		"review_list_readback",
	} {
		if !reportHasPassedStage(report.Stages, want) {
			t.Fatalf("stages = %+v, missing passed stage %q", report.Stages, want)
		}
	}
}

func reportHasPassedStage(stages []struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}, want string) bool {
	for _, stage := range stages {
		if stage.Name == want && stage.Status == "passed" {
			return true
		}
	}
	return false
}
