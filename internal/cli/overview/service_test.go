package overview

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/initiatives"
	coreprojects "odin-os/internal/core/projects"
	knowledgememory "odin-os/internal/memory/knowledge"
	"odin-os/internal/registry"
	"odin-os/internal/store/sqlite"
)

func TestBuildReturnsCanonicalOverviewFromCurrentAuthority(t *testing.T) {
	ctx := context.Background()
	env := newOverviewTestEnvironment(t)
	nextDueAt := time.Date(2026, 4, 25, 9, 0, 0, 0, time.UTC)
	if _, err := env.store.CreateFollowUpObligation(ctx, sqlite.CreateFollowUpObligationParams{
		WorkspaceID:     env.workspaceID,
		InitiativeID:    &env.initiativeID,
		CompanionID:     &env.companionID,
		TargetProjectID: env.projectID,
		Title:           "Review automation trigger lane",
		Status:          "active",
		CadenceJSON:     `{"mode":"once"}`,
		NextDueAt:       nextDueAt,
		PolicyJSON:      `{}`,
	}); err != nil {
		t.Fatalf("CreateFollowUpObligation() error = %v", err)
	}
	if _, err := env.store.CreateTaskIntake(ctx, sqlite.CreateTaskIntakeParams{
		TaskID:      env.taskID,
		Source:      "n8n",
		IntakeType:  "ci_failure",
		DedupKey:    "ci_failure:alpha:42",
		RequestedBy: "n8n",
		PayloadJSON: `{"workflow_id":"alpha-ci","run_id":"42"}`,
	}); err != nil {
		t.Fatalf("CreateTaskIntake() error = %v", err)
	}

	view, err := Service{
		Store:            env.store,
		RegistrySnapshot: env.snapshot,
		Now: func() time.Time {
			return nextDueAt.Add(time.Hour)
		},
	}.Build(ctx, scope.Resolution{Kind: scope.ScopeGlobal})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if view.Workspace.Wiring != WiringLive {
		t.Fatalf("Workspace wiring = %q, want %q", view.Workspace.Wiring, WiringLive)
	}
	if view.Workspace.WorkspaceKey != "default" {
		t.Fatalf("Workspace key = %q, want default", view.Workspace.WorkspaceKey)
	}
	if len(view.Initiatives) != 1 {
		t.Fatalf("Initiatives len = %d, want 1", len(view.Initiatives))
	}
	if view.Initiatives[0].InitiativeKey != "alpha" {
		t.Fatalf("Initiative key = %q, want alpha", view.Initiatives[0].InitiativeKey)
	}
	if view.Companions.Wiring != WiringLive {
		t.Fatalf("Companions wiring = %q, want %q", view.Companions.Wiring, WiringLive)
	}
	if len(view.Companions.Items) != 1 {
		t.Fatalf("Companion items len = %d, want 1", len(view.Companions.Items))
	}
	if view.CapabilityCatalog.AgentDefinitionCount != 1 || view.CapabilityCatalog.SkillCount != 1 || view.CapabilityCatalog.WorkflowCount != 1 || view.CapabilityCatalog.CommandCount != 1 {
		t.Fatalf("Capability catalog = %+v, want one item per registry kind", view.CapabilityCatalog)
	}
	if view.CapabilityCatalog.ToolCount == 0 {
		t.Fatalf("Tool count = 0, want builtin tools")
	}
	if len(view.WorkItems) != 1 {
		t.Fatalf("Work items len = %d, want 1", len(view.WorkItems))
	}
	if view.WorkItems[0].InitiativeKey == nil || *view.WorkItems[0].InitiativeKey != "alpha" {
		t.Fatalf("Work item initiative = %v, want alpha", view.WorkItems[0].InitiativeKey)
	}
	if view.WorkItems[0].CompanionKey == nil || *view.WorkItems[0].CompanionKey != "primary" {
		t.Fatalf("Work item companion = %v, want primary", view.WorkItems[0].CompanionKey)
	}
	if len(view.WorkItems[0].RunAttempts) != 1 {
		t.Fatalf("Work item run attempts len = %d, want 1", len(view.WorkItems[0].RunAttempts))
	}
	if len(view.Approvals) != 1 {
		t.Fatalf("Approvals len = %d, want 1", len(view.Approvals))
	}
	if view.Approvals[0].RunID == nil {
		t.Fatalf("Approval RunID = nil, want linked run")
	}
	if view.Approvals[0].ResolverSupport != "unsupported" {
		t.Fatalf("Approval resolver support = %q, want unsupported", view.Approvals[0].ResolverSupport)
	}
	if len(view.Observability.ActiveRuns) != 1 {
		t.Fatalf("Active runs len = %d, want 1", len(view.Observability.ActiveRuns))
	}
	if view.Observability.ActiveRuns[0].CompanionKey == nil || *view.Observability.ActiveRuns[0].CompanionKey != "primary" {
		t.Fatalf("Active run companion = %v, want primary", view.Observability.ActiveRuns[0].CompanionKey)
	}
	if len(view.Memory.Recent) != 1 || view.Memory.Count != 1 {
		t.Fatalf("Memory = %+v, want one recent entry", view.Memory)
	}
	if view.IntakeInbox.Wiring != WiringLive {
		t.Fatalf("Intake wiring = %q, want %q", view.IntakeInbox.Wiring, WiringLive)
	}
	if view.IntakeInbox.Source != "task_intakes" {
		t.Fatalf("Intake source = %q, want task_intakes", view.IntakeInbox.Source)
	}
	if view.IntakeInbox.Status != "linked_evidence" {
		t.Fatalf("Intake status = %q, want linked_evidence", view.IntakeInbox.Status)
	}
	if !strings.Contains(view.IntakeInbox.Note, "linked intake evidence") {
		t.Fatalf("Intake note = %q, want linked intake evidence note", view.IntakeInbox.Note)
	}
	if len(view.IntakeInbox.Items) != 1 {
		t.Fatalf("Intake items len = %d, want 1", len(view.IntakeInbox.Items))
	}
	intake := view.IntakeInbox.Items[0]
	if intake.Source != "n8n" || intake.IntakeType != "ci_failure" || intake.DedupKey != "ci_failure:alpha:42" {
		t.Fatalf("Intake identity = %+v, want n8n ci_failure ci_failure:alpha:42", intake)
	}
	if intake.WorkItemKey != "alpha-task" || intake.WorkItemStatus != "blocked" || intake.ProjectKey != "alpha" {
		t.Fatalf("Intake linked work = %+v, want alpha-task blocked alpha", intake)
	}
	if intake.InitiativeKey == nil || *intake.InitiativeKey != "alpha" {
		t.Fatalf("Intake initiative = %v, want alpha", intake.InitiativeKey)
	}
	if intake.CompanionKey == nil || *intake.CompanionKey != "primary" {
		t.Fatalf("Intake companion = %v, want primary", intake.CompanionKey)
	}
	if view.AutomationTriggers.Wiring != WiringLive {
		t.Fatalf("Automation wiring = %q, want %q", view.AutomationTriggers.Wiring, WiringLive)
	}
	if len(view.AutomationTriggers.Items) != 1 {
		t.Fatalf("Automation trigger items len = %d, want 1", len(view.AutomationTriggers.Items))
	}
	trigger := view.AutomationTriggers.Items[0]
	if trigger.Title != "Review automation trigger lane" {
		t.Fatalf("Automation trigger title = %q, want Review automation trigger lane", trigger.Title)
	}
	if trigger.InitiativeKey == nil || *trigger.InitiativeKey != "alpha" {
		t.Fatalf("Automation trigger initiative = %v, want alpha", trigger.InitiativeKey)
	}
	if trigger.CompanionKey == nil || *trigger.CompanionKey != "primary" {
		t.Fatalf("Automation trigger companion = %v, want primary", trigger.CompanionKey)
	}
	if trigger.TargetProjectKey != "alpha" {
		t.Fatalf("Automation trigger target project = %q, want alpha", trigger.TargetProjectKey)
	}
	if trigger.Status != "active" {
		t.Fatalf("Automation trigger status = %q, want active", trigger.Status)
	}
	if trigger.DueStatus != "due" {
		t.Fatalf("Automation trigger due status = %q, want due", trigger.DueStatus)
	}
	if trigger.NextDueAt != "2026-04-25T09:00:00Z" {
		t.Fatalf("Automation trigger next due = %q, want 2026-04-25T09:00:00Z", trigger.NextDueAt)
	}
}

func TestOverviewIntakeInboxMapsCompatibilityStatuses(t *testing.T) {
	ctx := context.Background()
	env := newOverviewTestEnvironment(t)

	statuses := []struct {
		name    string
		status  string
		subject string
	}{
		{name: "duplicate", status: "duplicate_linked_or_suppressed", subject: "Duplicate proposal"},
		{name: "canonical_duplicate", status: "duplicate_linked", subject: "Canonical duplicate proposal"},
		{name: "accepted", status: "accepted", subject: "Accepted proposal"},
		{name: "canonical_accepted", status: "accepted_for_promotion", subject: "Canonical accepted proposal"},
		{name: "approval_required", status: "approval_required", subject: "Approval required proposal"},
		{name: "rejected", status: "rejected", subject: "Rejected proposal"},
		{name: "approval_denied", status: "approval_denied", subject: "Approval denied proposal"},
		{name: "archived", status: "archived", subject: "Archived proposal"},
	}
	for _, entry := range statuses {
		if _, err := env.store.CreateIntakeItem(ctx, sqlite.CreateIntakeItemParams{
			WorkspaceID:         "default",
			SourceFamily:        "cli",
			EventKind:           "request",
			Subject:             entry.subject,
			DedupeKey:           "overview-alias-" + entry.name,
			DedupeRecipeVersion: "test-v1",
			SourceFactsJSON:     `{}`,
			Status:              entry.status,
			Scope:               "project",
			ScopeKey:            "alpha",
			Summary:             entry.subject,
		}); err != nil {
			t.Fatalf("CreateIntakeItem(%s) error = %v", entry.name, err)
		}
	}

	view, err := Service{
		Store:            env.store,
		RegistrySnapshot: env.snapshot,
		Now:              time.Now,
	}.Build(ctx, scope.Resolution{Kind: scope.ScopeGlobal})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if view.IntakeInbox.DuplicateLinkedCount != 2 {
		t.Fatalf("DuplicateLinkedCount = %d, want 2", view.IntakeInbox.DuplicateLinkedCount)
	}
	if view.IntakeInbox.AcceptedCount != 2 {
		t.Fatalf("AcceptedCount = %d, want 2", view.IntakeInbox.AcceptedCount)
	}
	if view.IntakeInbox.ReviewRequiredCount != 1 {
		t.Fatalf("ReviewRequiredCount = %d, want approval_required to map to review_required", view.IntakeInbox.ReviewRequiredCount)
	}
	if view.IntakeInbox.ReviewQueueCount != 2 {
		t.Fatalf("ReviewQueueCount = %d, want only literal review-list statuses", view.IntakeInbox.ReviewQueueCount)
	}
	if view.IntakeInbox.IntakeApprovalRequiredCount != 1 {
		t.Fatalf("IntakeApprovalRequiredCount = %d, want approval_required compatibility count", view.IntakeInbox.IntakeApprovalRequiredCount)
	}
	if view.IntakeInbox.RejectedCount != 1 {
		t.Fatalf("RejectedCount = %d, want rejected compatibility count", view.IntakeInbox.RejectedCount)
	}
	if view.IntakeInbox.ApprovalDeniedCount != 1 {
		t.Fatalf("ApprovalDeniedCount = %d, want approval_denied compatibility count", view.IntakeInbox.ApprovalDeniedCount)
	}
	if view.IntakeInbox.ArchivedCount != 3 {
		t.Fatalf("ArchivedCount = %d, want archived canonical count for archived/rejected/approval_denied", view.IntakeInbox.ArchivedCount)
	}
}

func TestRawIntakeSummaryReadsSourceEnvelopeFacts(t *testing.T) {
	item := sqlite.IntakeItem{
		ID:              7,
		SourceFamily:    "operator",
		EventKind:       "request",
		DedupeKey:       "odin-intake:abc",
		Subject:         "Capture governed intake",
		Status:          "received",
		Summary:         "Capture governed intake",
		CreatedAt:       time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 5, 10, 12, 1, 0, 0, time.UTC),
		SourceFactsJSON: `{"source_family":"operator","event_kind":"request","actor":"operator","adapter_facts":{"operator":{"requested_by":"codex","payload_policy":"stored_in_source_facts_json"}}}`,
	}

	summary := rawIntakeSummary(item)

	if summary.RequestedBy != "codex" {
		t.Fatalf("RequestedBy = %q, want adapter requested_by", summary.RequestedBy)
	}
	if summary.PayloadPolicy != "stored_in_source_facts_json" {
		t.Fatalf("PayloadPolicy = %q, want adapter payload policy", summary.PayloadPolicy)
	}
}

func TestRawIntakeSummaryPreservesTopLevelSourceFactsCompatibility(t *testing.T) {
	item := sqlite.IntakeItem{
		ID:              7,
		SourceFamily:    "operator",
		EventKind:       "request",
		DedupeKey:       "governed-intake:1",
		Subject:         "Capture governed intake",
		Status:          "received",
		CreatedAt:       time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 5, 10, 12, 1, 0, 0, time.UTC),
		SourceFactsJSON: `{"requested_by":"codex","payload_policy":"stored_in_source_facts_json"}`,
	}

	summary := rawIntakeSummary(item)

	if summary.RequestedBy != "codex" {
		t.Fatalf("RequestedBy = %q, want top-level requested_by", summary.RequestedBy)
	}
	if summary.PayloadPolicy != "stored_in_source_facts_json" {
		t.Fatalf("PayloadPolicy = %q, want top-level payload policy", summary.PayloadPolicy)
	}
}

func TestBuildWorkItemsExposeBlockedReason(t *testing.T) {
	ctx := context.Background()
	env := newOverviewTestEnvironment(t)
	if _, err := env.store.UpdateTaskQueueState(ctx, sqlite.UpdateTaskQueueStateParams{
		TaskID:         env.taskID,
		Status:         "blocked",
		NextEligibleAt: time.Time{},
		Priority:       100,
		MaxAttempts:    3,
		BlockedReason:  "mutation_requires_isolated_worktree",
	}); err != nil {
		t.Fatalf("UpdateTaskQueueState() error = %v", err)
	}

	view, err := Service{
		Store:            env.store,
		RegistrySnapshot: env.snapshot,
	}.Build(ctx, scope.Resolution{Kind: scope.ScopeGlobal})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(view.WorkItems) != 1 {
		t.Fatalf("Work items len = %d, want 1", len(view.WorkItems))
	}
	if view.WorkItems[0].BlockedReason != "mutation_requires_isolated_worktree" {
		t.Fatalf("Work item blocked reason = %q, want mutation_requires_isolated_worktree", view.WorkItems[0].BlockedReason)
	}
}

func TestBuildIncludesRegistryInitiativesWithoutRuntimeRows(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	t.Cleanup(func() {
		store.Close()
	})

	registry := writeOverviewProjectRegistry(t, map[string]string{"alpha": t.TempDir()})
	view, err := Service{
		Store:    store,
		Registry: registry,
	}.Build(ctx, scope.Resolution{Kind: scope.ScopeGlobal})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if len(view.Initiatives) != 1 {
		t.Fatalf("Initiatives len = %d, want registry-backed initiative", len(view.Initiatives))
	}
	if view.Initiatives[0].InitiativeKey != "alpha" {
		t.Fatalf("Initiatives[0].InitiativeKey = %q, want alpha", view.Initiatives[0].InitiativeKey)
	}
	if view.Initiatives[0].LinkedProjectKey == nil || *view.Initiatives[0].LinkedProjectKey != "alpha" {
		t.Fatalf("Initiatives[0].LinkedProjectKey = %v, want alpha", view.Initiatives[0].LinkedProjectKey)
	}
	if view.Workspace.InitiativeCount != 1 {
		t.Fatalf("Workspace.InitiativeCount = %d, want registry-backed count", view.Workspace.InitiativeCount)
	}
}

func TestBuildNarrowsProjectScopedOverview(t *testing.T) {
	ctx := context.Background()
	env := newOverviewTestEnvironment(t)

	workspace, err := env.store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	secondaryCompanion, err := env.store.UpsertCompanion(ctx, sqlite.UpsertCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 "ops",
		Title:               "Ops Companion",
		Kind:                "operator",
		Status:              "active",
		InitiativeScopeJSON: "[]",
		ToolPolicyJSON:      "{}",
		MemoryPolicyJSON:    "{}",
		PlanningPolicyJSON:  "{}",
	})
	if err != nil {
		t.Fatalf("UpsertCompanion(ops) error = %v", err)
	}
	betaProject, err := env.store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "beta",
		Name:          "Beta",
		Scope:         "project",
		GitRoot:       "/tmp/beta",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(beta) error = %v", err)
	}
	betaInitiative, err := env.store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              betaProject.Key,
		Title:            betaProject.Name,
		Kind:             string(initiatives.KindManagedProject),
		Status:           "active",
		Summary:          "Beta initiative",
		OwnerCompanionID: &secondaryCompanion.ID,
		LinkedProjectID:  &betaProject.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative(beta) error = %v", err)
	}
	betaTask, err := env.store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    betaProject.ID,
		Key:          "beta-task",
		Title:        "Beta task",
		Status:       "blocked",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &betaInitiative.ID,
		CompanionID:  &secondaryCompanion.ID,
		WorkKind:     "automation",
	})
	if err != nil {
		t.Fatalf("CreateTask(beta-task) error = %v", err)
	}
	betaRun, err := env.store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   betaTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(beta-task) error = %v", err)
	}
	betaIncident, err := env.store.OpenIncident(ctx, sqlite.OpenIncidentParams{
		RunID:       &betaRun.ID,
		Severity:    "warning",
		Status:      "open",
		Summary:     "beta incident",
		DetailsJSON: "{}",
	})
	if err != nil {
		t.Fatalf("OpenIncident(beta) error = %v", err)
	}
	if _, err := env.store.StartRecovery(ctx, sqlite.StartRecoveryParams{
		IncidentID:  &betaIncident.ID,
		Status:      "running",
		Strategy:    "self_heal",
		DetailsJSON: "{}",
	}); err != nil {
		t.Fatalf("StartRecovery(beta incident) error = %v", err)
	}

	view, err := Service{
		Store:            env.store,
		RegistrySnapshot: env.snapshot,
	}.Build(ctx, scope.Resolution{Kind: scope.ScopeProject, ProjectKey: "alpha"})
	if err != nil {
		t.Fatalf("Build(alpha) error = %v", err)
	}

	if len(view.Initiatives) != 1 || view.Initiatives[0].InitiativeKey != "alpha" {
		t.Fatalf("Initiatives = %+v, want only alpha", view.Initiatives)
	}
	if len(view.Companions.Items) != 1 || view.Companions.Items[0].CompanionKey != "primary" {
		t.Fatalf("Companions = %+v, want only primary", view.Companions.Items)
	}
	if len(view.WorkItems) != 1 || view.WorkItems[0].WorkItemKey != "alpha-task" {
		t.Fatalf("Work items = %+v, want only alpha-task", view.WorkItems)
	}
	if len(view.Observability.Recoveries) != 0 {
		t.Fatalf("Recoveries = %+v, want none in alpha scope", view.Observability.Recoveries)
	}
}

func writeOverviewProjectRegistry(t *testing.T, projects map[string]string) coreprojects.Registry {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "projects.yaml")
	content := "version: 1\nprojects:\n"
	for key, gitRoot := range projects {
		ensureOverviewGitRoot(t, gitRoot)
		content += "  - key: " + key + "\n"
		content += "    name: " + key + "\n"
		content += "    project_class: local_git_project\n"
		content += "    git_root: " + gitRoot + "\n"
		content += "    default_branch: main\n"
		content += "    policy:\n"
		content += "      allowed_commands: [status]\n"
		content += "      branch_rules:\n"
		content += "        protected_branches: [main]\n"
		content += "        require_worktree: true\n"
		content += "        require_task_branch: true\n"
		content += "        allow_default_branch_mutation: false\n"
		content += "      approval_gates:\n"
		content += "        require_for_governance_changes: true\n"
		content += "        require_for_destructive_operations: true\n"
		content += "        require_for_system_project_changes: false\n"
		content += "      merge_policy:\n"
		content += "        mode: squash\n"
		content += "        allow_direct_to_default_branch: false\n"
		content += "      destructive_operations:\n"
		content += "        allow_reset: false\n"
		content += "        allow_clean: false\n"
		content += "        allow_force_push: false\n"
		content += "        require_explicit_approval: true\n"
	}
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(projects.yaml) error = %v", err)
	}
	registry, diagnostics, err := coreprojects.Register(configPath)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Register() diagnostics = %#v", diagnostics)
	}
	return registry
}

func ensureOverviewGitRoot(t *testing.T, gitRoot string) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(gitRoot, ".git")); err == nil {
		return
	}
	if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.git) error = %v", err)
	}
}

func TestBuildWorkItemRunAttemptsIncludeHistory(t *testing.T) {
	ctx := context.Background()
	env := newOverviewTestEnvironment(t)

	if _, _, err := env.store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
		RunID:      env.runID,
		RunStatus:  "completed",
		Summary:    "first attempt completed",
		TaskStatus: "queued",
	}); err != nil {
		t.Fatalf("FinishRunAndSetTaskStatus() error = %v", err)
	}
	if _, err := env.store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   env.taskID,
		Executor: "codex",
		Attempt:  2,
		Status:   "running",
	}); err != nil {
		t.Fatalf("StartRun(attempt 2) error = %v", err)
	}

	view, err := Service{
		Store:            env.store,
		RegistrySnapshot: env.snapshot,
	}.Build(ctx, scope.Resolution{Kind: scope.ScopeGlobal})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if len(view.WorkItems) != 1 {
		t.Fatalf("Work items len = %d, want 1", len(view.WorkItems))
	}
	if len(view.WorkItems[0].RunAttempts) != 2 {
		t.Fatalf("Run attempts len = %d, want 2", len(view.WorkItems[0].RunAttempts))
	}
	if view.WorkItems[0].RunAttempts[0].Status != "completed" || view.WorkItems[0].RunAttempts[1].Status != "running" {
		t.Fatalf("Run attempts = %+v, want completed then running", view.WorkItems[0].RunAttempts)
	}
}

func TestBuildUsesOdinCoreMemoryScope(t *testing.T) {
	ctx := context.Background()
	env := newOverviewTestEnvironment(t)

	odinCoreProject, err := env.store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "project",
		GitRoot:       "/tmp/odin-core",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(odin-core) error = %v", err)
	}
	if _, err := (knowledgememory.Service{Store: env.store}).Record(ctx, knowledgememory.Scope{
		ProjectID: &odinCoreProject.ID,
		Value:     "odin-core",
		Key:       "odin-core",
	}, "system_note", "Remember odin-core state", `{"source":"test"}`, nil); err != nil {
		t.Fatalf("Record(odin-core) error = %v", err)
	}

	view, err := Service{
		Store:            env.store,
		RegistrySnapshot: env.snapshot,
	}.Build(ctx, scope.Resolution{Kind: scope.ScopeOdinCore, ProjectKey: "odin-core"})
	if err != nil {
		t.Fatalf("Build(odin-core) error = %v", err)
	}

	if view.Memory.Count != 2 {
		t.Fatalf("Memory count = %d, want 2 including global memory", view.Memory.Count)
	}
	sawOdinCore := false
	for _, memory := range view.Memory.Recent {
		if memory.Scope == "odin-core" && memory.ScopeKey == "odin-core" {
			sawOdinCore = true
			break
		}
	}
	if !sawOdinCore {
		t.Fatalf("Memory recent = %+v, want odin-core memory", view.Memory.Recent)
	}
}

func TestBuildIncludesCompanionSwarmAndIncidentAttention(t *testing.T) {
	ctx := context.Background()
	env := newOverviewTestEnvironment(t)

	incident, err := env.store.OpenIncident(ctx, sqlite.OpenIncidentParams{
		RunID:       &env.runID,
		Severity:    "warning",
		Status:      "open",
		Summary:     "Browser verification paused",
		DetailsJSON: "{}",
	})
	if err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}
	if _, err := env.store.StartRecovery(ctx, sqlite.StartRecoveryParams{
		IncidentID:  &incident.ID,
		Status:      "running",
		Strategy:    "self_heal",
		DetailsJSON: "{}",
	}); err != nil {
		t.Fatalf("StartRecovery() error = %v", err)
	}

	delegation, err := env.store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    env.taskID,
		ParentRunID:     &env.runID,
		ProjectID:       env.projectID,
		Scope:           "project",
		DelegationKey:   "review",
		Role:            "reviewer",
		ActionClass:     "analysis",
		ActionKey:       "review",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "review_gate",
		ArtifactTarget:  "report",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"Review bid diff","swarm":{"requested_budget":1,"max_children":1}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation() error = %v", err)
	}
	childTask, err := env.store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    env.projectID,
		Key:          "alpha-review-child",
		Title:        "Review child task",
		Status:       "running",
		Scope:        "project",
		RequestedBy:  "supervisor",
		WorkspaceID:  &env.workspaceID,
		InitiativeID: &env.initiativeID,
		CompanionID:  &env.companionID,
		WorkKind:     "swarm_child",
	})
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}
	childRun, err := env.store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   childTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(child) error = %v", err)
	}
	if _, err := env.store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: delegation.ID,
		ChildTaskID:  childTask.ID,
		ChildRunID:   &childRun.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask() error = %v", err)
	}

	view, err := Service{
		Store:            env.store,
		RegistrySnapshot: env.snapshot,
	}.Build(ctx, scope.Resolution{Kind: scope.ScopeGlobal})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if len(view.Observability.Incidents) != 1 {
		t.Fatalf("Incidents len = %d, want 1", len(view.Observability.Incidents))
	}
	if len(view.Observability.Recoveries) != 1 {
		t.Fatalf("Recoveries len = %d, want 1", len(view.Observability.Recoveries))
	}
	if len(view.CompanionSwarms) != 1 {
		t.Fatalf("Companion swarms len = %d, want 1", len(view.CompanionSwarms))
	}
	if view.CompanionSwarms[0].ParentTaskKey != "alpha-task" {
		t.Fatalf("Companion swarm parent task = %q, want alpha-task", view.CompanionSwarms[0].ParentTaskKey)
	}
	if view.CompanionSwarms[0].CompanionKey == nil || *view.CompanionSwarms[0].CompanionKey != "primary" {
		t.Fatalf("Companion swarm companion = %v, want primary", view.CompanionSwarms[0].CompanionKey)
	}
	if view.CompanionSwarms[0].ActiveChildRunCount != 1 {
		t.Fatalf("Companion swarm active child runs = %d, want 1", view.CompanionSwarms[0].ActiveChildRunCount)
	}
}

type overviewTestEnvironment struct {
	store        *sqlite.Store
	snapshot     registry.Snapshot
	workspaceID  int64
	projectID    int64
	initiativeID int64
	companionID  int64
	taskID       int64
	runID        int64
}

func newOverviewTestEnvironment(t *testing.T) overviewTestEnvironment {
	t.Helper()

	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	t.Cleanup(func() {
		store.Close()
	})

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              project.Key,
		Title:            project.Name,
		Kind:             string(initiatives.KindManagedProject),
		Status:           "active",
		Summary:          "Alpha initiative",
		OwnerCompanionID: &companion.ID,
		LinkedProjectID:  &project.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "alpha-task",
		Title:        "Alpha task",
		Status:       "blocked",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "automation",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	if _, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	if _, err := (knowledgememory.Service{Store: store}).Record(ctx, knowledgememory.Scope{
		Value: "global",
		Key:   "global",
	}, "operator_note", "Remember this overview state", `{"source":"test"}`, nil); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	items := []registry.Item{
		{Kind: registry.KindAgent, Key: "finance-advisor", Title: "Finance Advisor"},
		{Kind: registry.KindSkill, Key: "triage-skill", Title: "Triage Skill"},
		{Kind: registry.KindWorkflow, Key: "daily-workflow", Title: "Daily Workflow"},
		{Kind: registry.KindCommand, Key: "approve-command", Title: "Approve Command"},
	}
	snapshot := registry.Snapshot{
		Items:  append([]registry.Item(nil), items...),
		ByKey:  make(map[string]registry.Item, len(items)),
		ByKind: make(map[registry.Kind][]registry.Item),
	}
	for _, item := range items {
		snapshot.ByKey[item.Key] = item
		snapshot.ByKind[item.Kind] = append(snapshot.ByKind[item.Kind], item)
	}

	return overviewTestEnvironment{
		store:        store,
		snapshot:     snapshot,
		workspaceID:  workspace.ID,
		projectID:    project.ID,
		initiativeID: initiative.ID,
		companionID:  companion.ID,
		taskID:       task.ID,
		runID:        run.ID,
	}
}
