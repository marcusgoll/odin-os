package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"odin-os/internal/core/workspaces"
)

func TestCompanionRunAcceptance(t *testing.T) {
	ctx := context.Background()
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)

	t.Run("creates a companion-owned task in the default odin-core scope", func(t *testing.T) {
		runtimeRoot := t.TempDir()

		createOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "companion", "create", "--kind", "advisor", "--key", "finance", "--title", "Finance Advisor")
		if err != nil {
			t.Fatalf("runOdinCommand(companion create) error = %v\n%s", err, createOutput)
		}

		runOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "companion", "run", "finance", "--objective", "review April budget", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(companion run --json) error = %v\n%s", err, runOutput)
		}

		var payload struct {
			CompanionKey          string `json:"companion_key"`
			Objective             string `json:"objective"`
			RequestedSwarmTrigger string `json:"requested_swarm_trigger,omitempty"`
			Task                  struct {
				ID     int64  `json:"id"`
				Key    string `json:"key"`
				Status string `json:"status"`
				Scope  string `json:"scope"`
			} `json:"task"`
		}
		if err := json.Unmarshal([]byte(runOutput), &payload); err != nil {
			t.Fatalf("json.Unmarshal(companion run) error = %v\n%s", err, runOutput)
		}
		if payload.CompanionKey != "finance" {
			t.Fatalf("CompanionKey = %q, want finance", payload.CompanionKey)
		}
		if payload.Objective != "review April budget" {
			t.Fatalf("Objective = %q, want review April budget", payload.Objective)
		}
		if payload.RequestedSwarmTrigger != "" {
			t.Fatalf("RequestedSwarmTrigger = %q, want empty", payload.RequestedSwarmTrigger)
		}
		if payload.Task.Status != "queued" {
			t.Fatalf("Task.Status = %q, want queued", payload.Task.Status)
		}
		if payload.Task.Scope != "odin-core" {
			t.Fatalf("Task.Scope = %q, want odin-core", payload.Task.Scope)
		}

		store := openRuntimeStore(t, runtimeRoot)
		defer store.Close()

		task, err := store.GetTask(ctx, payload.Task.ID)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}
		if task.RequestedBy != "companion" {
			t.Fatalf("Task.RequestedBy = %q, want companion", task.RequestedBy)
		}
		if task.CompanionID == nil {
			t.Fatal("Task.CompanionID = nil, want finance companion")
		}
		workspace, err := store.GetWorkspaceByKey(ctx, workspaces.DefaultWorkspaceKey)
		if err != nil {
			t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
		}
		if task.WorkspaceID == nil || *task.WorkspaceID != workspace.ID {
			t.Fatalf("Task.WorkspaceID = %v, want %d", task.WorkspaceID, workspace.ID)
		}
		if task.ActionKey != "" {
			t.Fatalf("Task.ActionKey = %q, want empty without explicit trigger", task.ActionKey)
		}

		project, err := store.GetProjectByKey(ctx, "odin-core")
		if err != nil {
			t.Fatalf("GetProjectByKey(odin-core) error = %v", err)
		}
		if task.ProjectID != project.ID {
			t.Fatalf("Task.ProjectID = %d, want odin-core project %d", task.ProjectID, project.ID)
		}
		if task.InitiativeID == nil {
			t.Fatal("Task.InitiativeID = nil, want managed initiative")
		}
		initiative, err := store.GetInitiativeByID(ctx, *task.InitiativeID)
		if err != nil {
			t.Fatalf("GetInitiativeByID() error = %v", err)
		}
		if initiative.LinkedProjectID == nil || *initiative.LinkedProjectID != project.ID {
			t.Fatalf("Initiative.LinkedProjectID = %v, want %d", initiative.LinkedProjectID, project.ID)
		}
	})

	t.Run("persists a supported trigger only when one is explicitly requested", func(t *testing.T) {
		runtimeRoot := t.TempDir()

		createOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "companion", "create", "--kind", "advisor", "--key", "finance", "--title", "Finance Advisor")
		if err != nil {
			t.Fatalf("runOdinCommand(companion create) error = %v\n%s", err, createOutput)
		}

		runOutput, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "companion", "run", "finance", "--objective", "review April budget", "--trigger", "build_plus_review", "--json")
		if err != nil {
			t.Fatalf("runOdinCommand(companion run --trigger --json) error = %v\n%s", err, runOutput)
		}
		if !strings.Contains(runOutput, "build_plus_review") {
			t.Fatalf("companion run output = %q, want supported trigger metadata", runOutput)
		}

		var payload struct {
			RequestedSwarmTrigger string `json:"requested_swarm_trigger,omitempty"`
			Task                  struct {
				ID int64 `json:"id"`
			} `json:"task"`
		}
		if err := json.Unmarshal([]byte(runOutput), &payload); err != nil {
			t.Fatalf("json.Unmarshal(companion run trigger) error = %v\n%s", err, runOutput)
		}
		if payload.RequestedSwarmTrigger != "build_plus_review" {
			t.Fatalf("RequestedSwarmTrigger = %q, want build_plus_review", payload.RequestedSwarmTrigger)
		}

		store := openRuntimeStore(t, runtimeRoot)
		defer store.Close()

		task, err := store.GetTask(ctx, payload.Task.ID)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}
		if task.ActionKey != "build_plus_review" {
			t.Fatalf("Task.ActionKey = %q, want build_plus_review", task.ActionKey)
		}
	})
}
