package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadUsesDefaultsFromFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "config", "odin.yaml")
	mustWriteConfig(t, path, `
version: 1
runtime:
  root: /srv/odin
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: true
`)

	cfg, err := Load(path, repoRoot, map[string]string{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Version != 1 {
		t.Fatalf("Version = %d, want 1", cfg.Version)
	}
	if cfg.RuntimeRoot != "/srv/odin" {
		t.Fatalf("RuntimeRoot = %q, want %q", cfg.RuntimeRoot, "/srv/odin")
	}
	if cfg.Service.HTTPAddr != "127.0.0.1:9443" {
		t.Fatalf("Service.HTTPAddr = %q, want %q", cfg.Service.HTTPAddr, "127.0.0.1:9443")
	}
	if cfg.Service.AdminTokenEnv != "ODIN_ADMIN_TOKEN" {
		t.Fatalf("Service.AdminTokenEnv = %q, want ODIN_ADMIN_TOKEN", cfg.Service.AdminTokenEnv)
	}
	if !cfg.Service.StartupRecovery {
		t.Fatalf("Service.StartupRecovery = false, want true")
	}
}

func TestLoadAllowsEnvironmentOverrides(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "config", "odin.yaml")
	mustWriteConfig(t, path, `
version: 1
runtime:
  root: /srv/odin
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: false
`)

	cfg, err := Load(path, repoRoot, map[string]string{
		"ODIN_ROOT":        "/var/odin",
		"ODIN_HTTP_ADDR":   "0.0.0.0:9000",
		"ODIN_ADMIN_TOKEN": "local-admin-token",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.RuntimeRoot != "/var/odin" {
		t.Fatalf("RuntimeRoot = %q, want %q", cfg.RuntimeRoot, "/var/odin")
	}
	if cfg.Service.HTTPAddr != "0.0.0.0:9000" {
		t.Fatalf("Service.HTTPAddr = %q, want %q", cfg.Service.HTTPAddr, "0.0.0.0:9000")
	}
	if cfg.Service.StartupRecovery {
		t.Fatalf("Service.StartupRecovery = true, want false")
	}
	if cfg.AdminToken != "local-admin-token" {
		t.Fatalf("AdminToken = %q, want environment token", cfg.AdminToken)
	}
}

func TestLoadAcceptsSocialCopilotServiceSettings(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "config", "odin.yaml")
	mustWriteConfig(t, path, `
version: 1
runtime:
  root: /srv/odin
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: false
  social_copilot:
    enabled: true
    workflow_key: marcus-social-growth-workflow
    cadence_seconds: 1800
`)

	cfg, err := Load(path, repoRoot, map[string]string{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Service.SocialCopilot.Enabled {
		t.Fatal("Service.SocialCopilot.Enabled = false, want true")
	}
	if cfg.Service.SocialCopilot.WorkflowKey != "marcus-social-growth-workflow" {
		t.Fatalf("Service.SocialCopilot.WorkflowKey = %q", cfg.Service.SocialCopilot.WorkflowKey)
	}
	if cfg.Service.SocialCopilot.CadenceSeconds != 1800 {
		t.Fatalf("Service.SocialCopilot.CadenceSeconds = %d, want 1800", cfg.Service.SocialCopilot.CadenceSeconds)
	}
}

func TestValidateRepoRejectsUnknownAuxiliaryConfigFields(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	mustWriteConfig(t, filepath.Join(repoRoot, "config", "models.yaml"), `
version: 1
models:
  - key: codex-latest
    provider: openai
    access: plan_backed_cli
    adapter: codex_headless
    stale_field: true
`)
	mustWriteConfig(t, filepath.Join(repoRoot, "config", "telemetry.yaml"), `
version: 1
stale_field: true
`)

	if err := ValidateRepo(repoRoot); err == nil {
		t.Fatal("ValidateRepo() error = nil, want unknown field rejection")
	}
}

func TestValidateRepoAllowsMissingAuxiliaryConfigFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := ValidateRepo(repoRoot); err != nil {
		t.Fatalf("ValidateRepo() error = %v, want nil when auxiliary files are absent", err)
	}
}

func TestValidateRepoRejectsUnknownPolicyConfigFields(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	mustWriteConfig(t, filepath.Join(repoRoot, "config", "policies.yaml"), `
version: 1
stale_field: true
`)

	if err := ValidateRepo(repoRoot); err == nil {
		t.Fatal("ValidateRepo() error = nil, want unknown policy field rejection")
	}
}

func TestLoadTelemetryAcceptsValidatedBootstrapMarkerOnly(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "telemetry.yaml")
	mustWriteConfig(t, path, "version: 1\n")

	telemetry, err := LoadTelemetry(path)
	if err != nil {
		t.Fatalf("LoadTelemetry() error = %v", err)
	}
	if telemetry.Version != 1 {
		t.Fatalf("LoadTelemetry().Version = %d, want 1", telemetry.Version)
	}
}

func TestCapabilityReloadDocsMatchRuntimeSurface(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))

	gatewayDocPath := filepath.Join(repoRoot, "docs", "contracts", "capability-gateway.md")
	gatewayDoc, err := os.ReadFile(gatewayDocPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", gatewayDocPath, err)
	}

	reloadDocPath := filepath.Join(repoRoot, "docs", "operations", "capability-reload.md")
	reloadDoc, err := os.ReadFile(reloadDocPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", reloadDocPath, err)
	}

	for _, route := range []string{
		"GET /capabilities",
		"GET /capabilities/{id}",
		"POST /capabilities/{id}:invoke",
		"GET /runs/{run_id}",
	} {
		if !strings.Contains(string(gatewayDoc), route) {
			t.Fatalf("capability gateway doc missing runtime route %q", route)
		}
	}

	for _, token := range []string{
		"capability.snapshot_published",
		"capability.snapshot_rejected",
		"no public CLI or REPL reload command",
		"no public HTTP reload route",
	} {
		if !strings.Contains(string(reloadDoc), token) {
			t.Fatalf("capability reload doc missing runtime surface token %q", token)
		}
	}
}

func TestRepositoryPoliciesDefineUniversalWorkTaxonomy(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	path := filepath.Join(repoRoot, "config", "policies.yaml")

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}

	var policy struct {
		Version      int `yaml:"version"`
		WorkTaxonomy struct {
			Categories []string `yaml:"categories"`
			Statuses   []string `yaml:"statuses"`
		} `yaml:"work_taxonomy"`
		ApprovalPolicy struct {
			RequireApprovalBefore []string `yaml:"require_approval_before"`
		} `yaml:"approval_policy"`
	}
	if err := yaml.Unmarshal(content, &policy); err != nil {
		t.Fatalf("Unmarshal(policies.yaml) error = %v", err)
	}

	wantCategories := []string{
		"Projects",
		"Personal Admin",
		"Software Development",
		"Writing",
		"Research",
		"Calendar",
		"Email",
		"Finance/Admin",
		"Household",
		"Learning",
		"Health/Wellbeing",
		"Waiting For",
		"Archive",
	}
	if !slices.Equal(policy.WorkTaxonomy.Categories, wantCategories) {
		t.Fatalf("work_taxonomy.categories = %#v, want %#v", policy.WorkTaxonomy.Categories, wantCategories)
	}

	wantStatuses := []string{
		"inbox",
		"needs_clarification",
		"needs_review",
		"approved_for_plan",
		"approved_for_execution",
		"in_progress",
		"waiting_for",
		"blocked",
		"done",
		"archived",
		"deleted",
	}
	if !slices.Equal(policy.WorkTaxonomy.Statuses, wantStatuses) {
		t.Fatalf("work_taxonomy.statuses = %#v, want %#v", policy.WorkTaxonomy.Statuses, wantStatuses)
	}

	wantApprovalActions := []string{
		"send email",
		"create calendar event with others",
		"make purchase",
		"delete data",
		"deploy code",
		"modify production systems",
		"share public content",
		"change financial/legal/medical-related records",
	}
	if !slices.Equal(policy.ApprovalPolicy.RequireApprovalBefore, wantApprovalActions) {
		t.Fatalf("approval_policy.require_approval_before = %#v, want %#v", policy.ApprovalPolicy.RequireApprovalBefore, wantApprovalActions)
	}
}

func TestRepositoryPoliciesDefineMasterTriggerTaxonomy(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	path := filepath.Join(repoRoot, "config", "policies.yaml")

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}

	var policy struct {
		TriggerTaxonomy struct {
			TriggerTypes      []string `yaml:"trigger_types"`
			TriggerSources    []string `yaml:"trigger_sources"`
			ActionTypes       []string `yaml:"action_types"`
			RiskLevels        []string `yaml:"risk_levels"`
			HumanizationRules []string `yaml:"humanization_rules"`
		} `yaml:"trigger_taxonomy"`
	}
	if err := yaml.Unmarshal(content, &policy); err != nil {
		t.Fatalf("Unmarshal(policies.yaml) error = %v", err)
	}

	wantTriggerTypes := []string{
		"event_based",
		"cron_based",
		"humanized",
		"hybrid",
	}
	if !slices.Equal(policy.TriggerTaxonomy.TriggerTypes, wantTriggerTypes) {
		t.Fatalf("trigger_taxonomy.trigger_types = %#v, want %#v", policy.TriggerTaxonomy.TriggerTypes, wantTriggerTypes)
	}

	wantTriggerSources := []string{
		"inbox",
		"email",
		"calendar",
		"task_system",
		"logs",
		"github",
		"file_change",
		"automation_result",
		"habit_tracker",
		"household",
		"finance_admin",
		"personal_note",
		"external_webhook",
		"manual_user_command",
	}
	if !slices.Equal(policy.TriggerTaxonomy.TriggerSources, wantTriggerSources) {
		t.Fatalf("trigger_taxonomy.trigger_sources = %#v, want %#v", policy.TriggerTaxonomy.TriggerSources, wantTriggerSources)
	}

	wantActionTypes := []string{
		"classify",
		"summarize",
		"create_ticket",
		"create_task",
		"schedule",
		"defer",
		"remind",
		"route_to_agent",
		"request_approval",
		"run_workflow",
		"archive",
		"escalate",
		"retry",
		"self_heal",
		"log_only",
	}
	if !slices.Equal(policy.TriggerTaxonomy.ActionTypes, wantActionTypes) {
		t.Fatalf("trigger_taxonomy.action_types = %#v, want %#v", policy.TriggerTaxonomy.ActionTypes, wantActionTypes)
	}

	wantRiskLevels := []string{
		"low",
		"medium",
		"high",
		"critical",
	}
	if !slices.Equal(policy.TriggerTaxonomy.RiskLevels, wantRiskLevels) {
		t.Fatalf("trigger_taxonomy.risk_levels = %#v, want %#v", policy.TriggerTaxonomy.RiskLevels, wantRiskLevels)
	}

	wantHumanizationRules := []string{
		"quiet_hours",
		"calendar_aware",
		"energy_aware",
		"batchable",
		"defer_until_morning",
		"avoid_weekends",
		"prefer_weekends",
		"urgency_override",
		"manual_approval_required",
	}
	if !slices.Equal(policy.TriggerTaxonomy.HumanizationRules, wantHumanizationRules) {
		t.Fatalf("trigger_taxonomy.humanization_rules = %#v, want %#v", policy.TriggerTaxonomy.HumanizationRules, wantHumanizationRules)
	}
}

func mustWriteConfig(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
