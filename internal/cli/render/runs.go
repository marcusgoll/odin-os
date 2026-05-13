package render

import (
	"encoding/json"
	"fmt"
	"strings"

	runsvc "odin-os/internal/runtime/runs"
)

func RenderRunDetail(detail runsvc.Detail) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "run=%d task=%s status=%s executor=%s\n", detail.RunID, detail.TaskKey, detail.Status, detail.Executor)
	if strings.TrimSpace(detail.Summary) != "" {
		fmt.Fprintf(&builder, "summary=%s\n", detail.Summary)
	}
	if raw := strings.TrimSpace(detail.ArtifactsJSON); raw != "" && raw != "[]" {
		fmt.Fprintf(&builder, "artifacts_json=%s\n", raw)
	}
	if analysis := detail.FailureAnalysis; analysis != nil {
		renderFailureAnalysis(&builder, analysis)
	}
	for _, artifact := range detail.Artifacts {
		fmt.Fprintf(&builder, "artifact=%s summary=%s\n", artifact.ArtifactType, artifact.Summary)
		if details := strings.TrimSpace(artifact.DetailsJSON); details != "" && details != "{}" {
			fmt.Fprintf(&builder, "details=%s\n", details)
			renderRunEvidenceFields(&builder, details)
		}
	}
	return builder.String()
}

func renderFailureAnalysis(builder *strings.Builder, analysis *runsvc.FailureAnalysisDetail) {
	if value := strings.TrimSpace(analysis.Category); value != "" {
		fmt.Fprintf(builder, "failure_analysis_category=%s\n", value)
	}
	if value := strings.TrimSpace(analysis.SuggestedFix); value != "" {
		fmt.Fprintf(builder, "failure_analysis_suggested_fix=%s\n", value)
	}
	if value := strings.TrimSpace(analysis.NextStepTarget); value != "" {
		fmt.Fprintf(builder, "failure_analysis_next_step_target=%s\n", value)
	}
	fmt.Fprintf(builder, "failure_analysis_retry_recommended=%t\n", analysis.RetryRecommended)
	fmt.Fprintf(builder, "failure_analysis_follow_up_recommended=%t\n", analysis.FollowUpRecommended)
	if value := strings.TrimSpace(analysis.FollowUpTitle); value != "" {
		fmt.Fprintf(builder, "failure_analysis_follow_up_title=%s\n", value)
	}
	if value := strings.TrimSpace(analysis.FollowUpReason); value != "" {
		fmt.Fprintf(builder, "failure_analysis_follow_up_reason=%s\n", value)
	}
	if len(analysis.FollowUpLabels) > 0 {
		fmt.Fprintf(builder, "failure_analysis_follow_up_labels=%s\n", strings.Join(analysis.FollowUpLabels, ","))
	}
}

func renderRunEvidenceFields(builder *strings.Builder, raw string) {
	fields := runEvidenceFields(raw)
	for _, key := range []string{
		"executor_lane",
		"driver_kind",
		"operation",
		"external_id",
		"repo_root",
		"worktree_path",
		"branch_name",
		"driver_cwd",
		"branch_observed",
		"marker_path",
		"marker_written",
		"artifact_path",
	} {
		if value := strings.TrimSpace(fields[key]); value != "" {
			fmt.Fprintf(builder, "%s=%s\n", key, value)
		}
	}
}

func runEvidenceFields(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil
	}
	fields := make(map[string]string)
	for key, value := range decoded {
		if stringValue, ok := value.(string); ok {
			fields[key] = stringValue
		}
	}
	return fields
}
