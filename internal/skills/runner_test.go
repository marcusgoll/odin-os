package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCFIProsCEOOperatorReportsKPITruthAndApprovalBoundary(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("Abs(repo root) error = %v", err)
	}
	cfiprosRoot := t.TempDir()
	writeFile(t, filepath.Join(cfiprosRoot, "docs", "integrations", "N8N.md"), "source_gsc_daily_metrics\nsource_ga4_daily_metrics\n")
	writeFile(t, filepath.Join(cfiprosRoot, "docs", "integrations", "POSTHOG.md"), "source_gsc_daily_metrics\nsource_ga4_daily_metrics\n")
	writeFile(t, filepath.Join(cfiprosRoot, "api", "app", "scripts", "verify_analytics_pipeline.py"), "POSTHOG_PERSONAL_API_KEY\nsource_ga4_daily_metrics\n")
	writeFile(t, filepath.Join(cfiprosRoot, "docs", "api-reference", "endpoints", "students.md"), "student_count\nactive_students\n")
	writeFile(t, filepath.Join(cfiprosRoot, "api", "app", "services", "organizations", "organization_service.py"), "student_count\ninstructor_count\n")
	writeFile(t, filepath.Join(cfiprosRoot, "docs", "api-reference", "endpoints", "aktr.md"), "total_uploads\ntotal_codes_extracted\n")
	writeFile(t, filepath.Join(cfiprosRoot, "OBSERVABILITY_VERIFICATION.md"), "/metrics\nPrometheus\naktr\nmetrics\n")
	writeFile(t, filepath.Join(cfiprosRoot, "docs", "api-reference", "endpoints", "billing.md"), "subscriptions/status\ncheckout\nstripe_customer_id\n")
	writeFile(t, filepath.Join(cfiprosRoot, "config", "deploy.api.yml"), "STRIPE_SECRET_KEY\nSTRIPE_WEBHOOK_SECRET\n")

	approvalBoundary := "internal CEO review only; external actions require approval"
	payload, err := json.Marshal(map[string]any{
		"input": map[string]any{
			"checkpoint":        "daily_morning_launch_health",
			"project_key":       "cfipros",
			"approval_boundary": approvalBoundary,
			"cfipros_repo_root": cfiprosRoot,
			"kpi_evidence": map[string]any{
				"paid_conversion": map[string]any{
					"value":  "subscriptions=3",
					"source": "read-only fixture",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal(payload) error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(repoRoot, "scripts", "skills", "cfipros-ceo-operator.sh"))
	cmd.Dir = repoRoot
	cmd.Stdin = bytes.NewReader(payload)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("cfipros-ceo-operator.sh error = %v stderr=%s", err, stderr.String())
	}

	var response struct {
		Status string         `json:"status"`
		Output map[string]any `json:"output"`
	}
	if err := json.Unmarshal(stdout, &response); err != nil {
		t.Fatalf("Unmarshal(response) error = %v\nstdout=%s", err, string(stdout))
	}
	if response.Status != "ok" {
		t.Fatalf("status = %q, want ok", response.Status)
	}
	if response.Output["agent_key"] != "cfipros-ceo-operator-agent" {
		t.Fatalf("agent_key = %v, want cfipros-ceo-operator-agent", response.Output["agent_key"])
	}
	if response.Output["approval_boundary"] != approvalBoundary {
		t.Fatalf("approval_boundary = %v, want preserved boundary", response.Output["approval_boundary"])
	}
	if response.Output["approval_required"] != true || response.Output["external_side_effect"] != "none" {
		t.Fatalf("approval fields = approval_required:%v external_side_effect:%v, want true/none", response.Output["approval_required"], response.Output["external_side_effect"])
	}

	kpiTruth, ok := response.Output["kpi_truth"].(map[string]any)
	if !ok {
		t.Fatalf("kpi_truth missing from output: %#v", response.Output)
	}
	if kpiTruth["collection_mode"] != "read_only" {
		t.Fatalf("collection_mode = %v, want read_only", kpiTruth["collection_mode"])
	}
	metrics, ok := kpiTruth["metrics"].([]any)
	if !ok || len(metrics) == 0 {
		t.Fatalf("metrics = %#v, want non-empty KPI scorecard", kpiTruth["metrics"])
	}
	var sawAcquisitionSource, sawPaidMeasured bool
	for _, rawMetric := range metrics {
		metric, ok := rawMetric.(map[string]any)
		if !ok {
			t.Fatalf("metric = %#v, want object", rawMetric)
		}
		switch metric["key"] {
		case "acquisition_traffic":
			sawAcquisitionSource = metric["status"] == "source_available_value_unmeasured" && metric["value"] == "unmeasured"
		case "paid_conversion":
			sawPaidMeasured = metric["status"] == "measured" && metric["value"] == "subscriptions=3"
		}
	}
	if !sawAcquisitionSource || !sawPaidMeasured {
		t.Fatalf("metrics = %#v, want acquisition source truth and supplied paid-conversion measurement", metrics)
	}
	if _, ok := response.Output["ceo_packet"].(map[string]any); !ok {
		t.Fatalf("ceo_packet missing from output: %#v", response.Output)
	}
}

func TestCFIProsCEOOperatorMapsCFIProsKPIExport(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("Abs(repo root) error = %v", err)
	}
	cfiprosRoot := t.TempDir()
	writeFile(t, filepath.Join(cfiprosRoot, "docs", "api-reference", "endpoints", "students.md"), "student_count\nactive_students\n")
	writeFile(t, filepath.Join(cfiprosRoot, "api", "app", "services", "organizations", "organization_service.py"), "student_count\ninstructor_count\n")
	writeFile(t, filepath.Join(cfiprosRoot, "docs", "api-reference", "endpoints", "aktr.md"), "total_uploads\ntotal_codes_extracted\n")
	writeFile(t, filepath.Join(cfiprosRoot, "OBSERVABILITY_VERIFICATION.md"), "/metrics\nPrometheus\naktr\nmetrics\n")
	writeFile(t, filepath.Join(cfiprosRoot, "docs", "api-reference", "endpoints", "billing.md"), "subscriptions/status\ncheckout\nstripe_customer_id\n")
	writeFile(t, filepath.Join(cfiprosRoot, "config", "deploy.api.yml"), "STRIPE_SECRET_KEY\nSTRIPE_WEBHOOK_SECRET\n")

	payload, err := json.Marshal(map[string]any{
		"input": map[string]any{
			"checkpoint":        "daily_morning_launch_health",
			"project_key":       "cfipros",
			"approval_boundary": "internal CEO review only; external actions require approval",
			"cfipros_repo_root": cfiprosRoot,
			"kpi_export": map[string]any{
				"status":               "measured",
				"source":               "cfipros-api:/api/webhooks/n8n/ceo-kpis",
				"as_of_utc":            "2026-05-18T12:30:00Z",
				"approval_required":    true,
				"external_side_effect": "none",
				"metrics": []map[string]any{
					{"key": "users_created_today", "value": 2, "unit": "count", "status": "measured"},
					{"key": "users_onboarded_today", "value": 1, "unit": "count", "status": "measured"},
					{"key": "students_created_today", "value": 1, "unit": "count", "status": "measured"},
					{"key": "aktr_uploads_today", "value": 1, "unit": "count", "status": "measured"},
					{"key": "aktr_codes_today", "value": 2, "unit": "count", "status": "measured"},
					{"key": "failed_extractions_today", "value": 1, "unit": "count", "status": "measured"},
					{"key": "paid_subscribers_total", "value": 2, "unit": "count", "status": "measured"},
					{"key": "estimated_mrr_cents", "value": 12800, "unit": "cents", "status": "measured"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal(payload) error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(repoRoot, "scripts", "skills", "cfipros-ceo-operator.sh"))
	cmd.Dir = repoRoot
	cmd.Stdin = bytes.NewReader(payload)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("cfipros-ceo-operator.sh error = %v stderr=%s", err, stderr.String())
	}

	var response struct {
		Status string         `json:"status"`
		Output map[string]any `json:"output"`
	}
	if err := json.Unmarshal(stdout, &response); err != nil {
		t.Fatalf("Unmarshal(response) error = %v\nstdout=%s", err, string(stdout))
	}
	kpiTruth := response.Output["kpi_truth"].(map[string]any)
	if kpiTruth["collection_status"] != "measured_values_supplied" {
		t.Fatalf("collection_status = %v, want measured_values_supplied", kpiTruth["collection_status"])
	}
	if kpiTruth["kpi_export_source"] != "cfipros-api:/api/webhooks/n8n/ceo-kpis" {
		t.Fatalf("kpi_export_source = %v, want CFIPros export source", kpiTruth["kpi_export_source"])
	}
	metrics := kpiTruth["metrics"].([]any)
	var sawActivation, sawProduct, sawPaid, sawQuality bool
	for _, rawMetric := range metrics {
		metric := rawMetric.(map[string]any)
		switch metric["key"] {
		case "activation_students":
			sawActivation = metric["status"] == "measured" && strings.Contains(metric["value"].(string), "users_created_today=2")
		case "product_value_aktr":
			sawProduct = metric["status"] == "measured" && strings.Contains(metric["value"].(string), "aktr_codes_today=2")
		case "paid_conversion":
			sawPaid = metric["status"] == "measured" && strings.Contains(metric["value"].(string), "estimated_mrr_cents=12800")
		case "quality_operations":
			sawQuality = metric["status"] == "measured" && strings.Contains(metric["value"].(string), "failed_extractions_today=1")
		}
	}
	if !sawActivation || !sawProduct || !sawPaid || !sawQuality {
		t.Fatalf("metrics = %#v, want measured CFIPros export values", metrics)
	}
	if response.Output["approval_required"] != true || response.Output["external_side_effect"] != "none" {
		t.Fatalf("approval fields = approval_required:%v external_side_effect:%v, want true/none", response.Output["approval_required"], response.Output["external_side_effect"])
	}
}

func TestRunRestrictedCommandUsesRepoRootCwd(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	cwdPath := filepath.Join(t.TempDir(), "cwd.txt")
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "cwd-skill.sh")
	writeExecutable(t, handlerPath, "#!/usr/bin/env bash\ncat >/dev/null\npwd >\""+cwdPath+"\"\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\"cwd ok\"}'\n")

	stdout, stderr, err := runRestrictedCommand(context.Background(), service.RepoRoot, restrictedSkillMetadata{
		Key:              "cwd-skill",
		Handler:          "scripts/skills/cwd-skill.sh",
		ExecutionProfile: restrictedSkillExecutionProfile,
	}, handlerPath, []byte("{}"))
	if err != nil {
		t.Fatalf("runRestrictedCommand() error = %v", err)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("runRestrictedCommand() stderr = %q, want empty", stderr)
	}
	if strings.TrimSpace(stdout) != "{\"status\":\"ok\",\"summary\":\"cwd ok\"}" {
		t.Fatalf("runRestrictedCommand() stdout = %q, want structured response", stdout)
	}

	got := strings.TrimSpace(mustReadFile(t, cwdPath))
	if got != service.RepoRoot {
		t.Fatalf("handler cwd = %q, want repo root %q", got, service.RepoRoot)
	}
}

func TestRunRestrictedCommandScrubsInheritedEnvironment(t *testing.T) {
	service := newTestService(t)
	leakPath := filepath.Join(t.TempDir(), "leak.txt")
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "env-scrub-skill.sh")

	t.Setenv("ODIN_SHOULD_NOT_LEAK", "secret-value")
	writeExecutable(t, handlerPath, "#!/usr/bin/env bash\ncat >/dev/null\nprintf '%s' \"${ODIN_SHOULD_NOT_LEAK-}\" >\""+leakPath+"\"\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\"env scrub ok\"}'\n")

	stdout, stderr, err := runRestrictedCommand(context.Background(), service.RepoRoot, restrictedSkillMetadata{
		Key:              "env-scrub-skill",
		Handler:          "scripts/skills/env-scrub-skill.sh",
		ExecutionProfile: restrictedSkillExecutionProfile,
	}, handlerPath, []byte("{}"))
	if err != nil {
		t.Fatalf("runRestrictedCommand() error = %v", err)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("runRestrictedCommand() stderr = %q, want empty", stderr)
	}
	if strings.TrimSpace(stdout) != "{\"status\":\"ok\",\"summary\":\"env scrub ok\"}" {
		t.Fatalf("runRestrictedCommand() stdout = %q, want structured response", stdout)
	}

	if got := mustReadFile(t, leakPath); got != "" {
		t.Fatalf("handler observed leaked env = %q, want empty", got)
	}
}

func TestRunRestrictedCommandPreservesPathForEnvBash(t *testing.T) {
	service := newTestService(t)
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "path-skill.sh")

	realBash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("LookPath(\"bash\") error = %v", err)
	}
	binDir := t.TempDir()
	if err := os.Symlink(realBash, filepath.Join(binDir, "bash")); err != nil {
		t.Fatalf("Symlink(%q, %q) error = %v", realBash, filepath.Join(binDir, "bash"), err)
	}
	t.Setenv("PATH", binDir)

	writeExecutable(t, handlerPath, "#!/usr/bin/env bash\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\"path ok\"}'\n")

	stdout, stderr, err := runRestrictedCommand(context.Background(), service.RepoRoot, restrictedSkillMetadata{
		Key:              "path-skill",
		Handler:          "scripts/skills/path-skill.sh",
		ExecutionProfile: restrictedSkillExecutionProfile,
	}, handlerPath, []byte("{}"))
	if err != nil {
		t.Fatalf("runRestrictedCommand() error = %v", err)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("runRestrictedCommand() stderr = %q, want empty", stderr)
	}
	if strings.TrimSpace(stdout) != "{\"status\":\"ok\",\"summary\":\"path ok\"}" {
		t.Fatalf("runRestrictedCommand() stdout = %q, want structured response", stdout)
	}
}

func TestRunRestrictedCommandPreservesRequiredEnvVars(t *testing.T) {
	service := newTestService(t)
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "metadata-skill.sh")
	metadataPath := filepath.Join(t.TempDir(), "metadata.txt")
	tmpDir := filepath.Join(t.TempDir(), "tmp")
	odinRoot := filepath.Join(t.TempDir(), "odin-root")
	handlerRef := filepath.ToSlash(filepath.Join("scripts", "skills", "metadata-skill.sh"))

	t.Setenv("TMPDIR", tmpDir)
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("ODIN_SHOULD_NOT_LEAK", "secret-value")

	writeExecutable(t, handlerPath, `#!/usr/bin/env bash
cat >/dev/null
{
  printf 'PATH=%s\n' "${PATH-}"
  printf 'TMPDIR=%s\n' "${TMPDIR-}"
  printf 'ODIN_ROOT=%s\n' "${ODIN_ROOT-}"
  printf 'ODIN_SKILL_KEY=%s\n' "${ODIN_SKILL_KEY-}"
  printf 'ODIN_SKILL_HANDLER=%s\n' "${ODIN_SKILL_HANDLER-}"
  printf 'ODIN_SKILL_EXECUTION_PROFILE=%s\n' "${ODIN_SKILL_EXECUTION_PROFILE-}"
  printf 'ODIN_SHOULD_NOT_LEAK=%s\n' "${ODIN_SHOULD_NOT_LEAK-}"
} >"`+metadataPath+`"
printf '%s\n' '{"status":"ok","summary":"metadata ok"}'
`)

	stdout, stderr, err := runRestrictedCommand(context.Background(), service.RepoRoot, restrictedSkillMetadata{
		Key:              "metadata-skill",
		Handler:          handlerRef,
		ExecutionProfile: restrictedSkillExecutionProfile,
	}, handlerPath, []byte("{}"))
	if err != nil {
		t.Fatalf("runRestrictedCommand() error = %v", err)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("runRestrictedCommand() stderr = %q, want empty", stderr)
	}
	if strings.TrimSpace(stdout) != "{\"status\":\"ok\",\"summary\":\"metadata ok\"}" {
		t.Fatalf("runRestrictedCommand() stdout = %q, want structured response", stdout)
	}

	got := mustReadFile(t, metadataPath)
	if !strings.Contains(got, "PATH=") {
		t.Fatalf("metadata env missing PATH:\n%s", got)
	}
	if !strings.Contains(got, "TMPDIR="+tmpDir) {
		t.Fatalf("metadata env missing TMPDIR:\n%s", got)
	}
	if !strings.Contains(got, "ODIN_ROOT="+odinRoot) {
		t.Fatalf("metadata env missing ODIN_ROOT:\n%s", got)
	}
	if !strings.Contains(got, "ODIN_SKILL_KEY=metadata-skill") {
		t.Fatalf("metadata env missing skill key:\n%s", got)
	}
	if !strings.Contains(got, "ODIN_SKILL_HANDLER="+handlerRef) {
		t.Fatalf("metadata env missing handler path:\n%s", got)
	}
	if !strings.Contains(got, "ODIN_SKILL_EXECUTION_PROFILE="+restrictedSkillExecutionProfile) {
		t.Fatalf("metadata env missing execution profile:\n%s", got)
	}
	if !strings.Contains(got, "ODIN_SHOULD_NOT_LEAK=") {
		t.Fatalf("metadata env missing leak probe:\n%s", got)
	}
}

func TestRunRestrictedCommandHonorsTimeout(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "timeout-skill.sh")
	writeExecutable(t, handlerPath, "#!/usr/bin/env bash\nsleep 2\nprintf '%s\\n' '{\"status\":\"ok\",\"summary\":\"slow\"}'\n")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, _, err := runRestrictedCommand(ctx, service.RepoRoot, restrictedSkillMetadata{
		Key:              "timeout-skill",
		Handler:          "scripts/skills/timeout-skill.sh",
		ExecutionProfile: restrictedSkillExecutionProfile,
	}, handlerPath, []byte("{}"))
	if err == nil {
		t.Fatal("runRestrictedCommand() error = nil, want timeout")
	}
	if !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "killed") {
		t.Fatalf("runRestrictedCommand() error = %v, want timeout-related failure", err)
	}
}

func TestRunRestrictedCommandKillsProcessGroupOnTimeout(t *testing.T) {
	service := newTestService(t)
	handlerPath := filepath.Join(service.RepoRoot, "scripts", "skills", "group-kill-skill.py")
	markerPath := filepath.Join(t.TempDir(), "child-marker.txt")

	script := fmt.Sprintf(`#!/usr/bin/env python3
import subprocess
import time

marker = %q
subprocess.Popen([
    "sh",
    "-c",
    f"sleep 0.3; printf '%%s\n' 'child survived' >{marker}",
])
time.sleep(2)
print('{"status":"ok","summary":"should not complete"}')
`, markerPath)
	writeExecutable(t, handlerPath, script)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, _, err := runRestrictedCommand(ctx, service.RepoRoot, restrictedSkillMetadata{
		Key:              "group-kill-skill",
		Handler:          "scripts/skills/group-kill-skill.py",
		ExecutionProfile: restrictedSkillExecutionProfile,
	}, handlerPath, []byte("{}"))
	if err == nil {
		t.Fatal("runRestrictedCommand() error = nil, want timeout")
	}

	time.Sleep(500 * time.Millisecond)

	if _, statErr := os.Stat(markerPath); !os.IsNotExist(statErr) {
		t.Fatalf("child marker = %v, want child process killed with wrapper", statErr)
	}
}
