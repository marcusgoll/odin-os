package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/runtime/events"
	"odin-os/internal/skills"
	"odin-os/internal/store/sqlite"
)

func TestRunDesignStatusReportsOpenDesignDriverAndODDataDir(t *testing.T) {
	root := testRepoRoot(t)
	configureOpenDesignDriver(
		t,
		`#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"completed","tool_key":"browser_open_design","summary":"design daemon ok","artifacts":{}}'
`,
	)
	t.Setenv("OD_DATA_DIR", filepath.Join(root, "od-data"))

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"design", "status", "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(design status --json) error = %v", err)
	}

	var view designStatusView
	if err := json.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal status output = %v", err)
	}

	if view.ODDataDir != filepath.Join(root, "od-data") {
		t.Fatalf("ODDataDir = %q, want %q", view.ODDataDir, filepath.Join(root, "od-data"))
	}
	if !view.OpenDesignAvailable {
		t.Fatalf("OpenDesignAvailable = false, want true")
	}
	if view.OpenDesignError != "ok" {
		t.Fatalf("OpenDesignError = %q, want ok", view.OpenDesignError)
	}
	if view.OpenDesignDriver == "" {
		t.Fatalf("OpenDesignDriver = empty, want path")
	}
}

func TestRunDesignSkillsAndSystemsList(t *testing.T) {
	root := testRepoRoot(t)
	configureOpenDesignDriver(
		t,
		`#!/usr/bin/env bash
printf '%s\n' '{"status":"completed","tool_key":"browser_open_design","summary":"design skills and systems","artifacts":{"skills":[{"key":"browser_open_design","title":"Browser Open Design","version":"1.0.0","summary":"Launch designs from a browser sidecar","status":"active","source_key":"open-design"},{"key":"figma-plugin","title":"Figma Plugin","version":"0.1.0","summary":"Figma assisted design","status":"active","source_key":"open-design"}],"systems":["design","visual","ui"]}}'
`,
	)

	var skillsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"design", "skills", "list", "--json"}, strings.NewReader(""), &skillsOutput); err != nil {
		t.Fatalf("Run(design skills list --json) error = %v", err)
	}

	var listed designSkillListView
	if err := json.Unmarshal(skillsOutput.Bytes(), &listed); err != nil {
		t.Fatalf("unmarshal design skills output = %v", err)
	}
	if len(listed.Items) != 2 {
		t.Fatalf("design skills count = %d, want 2", len(listed.Items))
	}
	if listed.Items[0].Key != "browser_open_design" {
		t.Fatalf("design skill key = %q, want browser_open_design", listed.Items[0].Key)
	}
	if listed.Items[1].Key != "figma-plugin" {
		t.Fatalf("design skill key = %q, want figma-plugin", listed.Items[1].Key)
	}

	var systemsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"design", "systems", "list", "--json"}, strings.NewReader(""), &systemsOutput); err != nil {
		t.Fatalf("Run(design systems list --json) error = %v", err)
	}

	var systems designSystemsView
	if err := json.Unmarshal(systemsOutput.Bytes(), &systems); err != nil {
		t.Fatalf("unmarshal design systems output = %v", err)
	}
	if len(systems.Systems) != 3 {
		t.Fatalf("design systems count = %d, want 3", len(systems.Systems))
	}
	expectedSystems := map[string]struct{}{"design": {}, "ui": {}, "visual": {}}
	for _, system := range systems.Systems {
		if _, found := expectedSystems[system]; !found {
			t.Fatalf("unexpected design system = %q", system)
		}
	}
}

func TestRunDesignRequestCreateRejectsDisallowedPermissions(t *testing.T) {
	root := testRepoRoot(t)
	briefPath := filepath.Join(root, "brief.json")
	if err := os.WriteFile(briefPath, []byte(`{
  "skill_key": "browser_open_design",
  "title": "Landing page concept",
  "summary": "Generate landing page concept",
  "permissions": ["repo.read"],
  "input": {
    "permissions": ["repo.mutate.full"]
  }
}`), 0o644); err != nil {
		t.Fatalf("write brief = %v", err)
	}

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"design", "request", "create", "--brief", briefPath, "--json"}, strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatal("Run(design request create --json) unexpectedly succeeded")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not allowed for design") {
		t.Fatalf("design request create error = %v, want disallowed permission", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()
	artifacts, err := store.ListSkillArtifacts(context.Background(), sqlite.ListSkillArtifactsParams{})
	if err != nil {
		t.Fatalf("ListSkillArtifacts() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("design artifacts count = %d, want 0", len(artifacts))
	}
}

func TestRunDesignInvokeRejectsDisallowedPermissionsFromInput(t *testing.T) {
	root := testRepoRoot(t)
	seedDesignFixture(t, root, "browser_open_design", "Open Design", "Launch browser design", []string{"design"}, []string{"design"}, []string{"repo.read"})

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"design", "invoke", "browser_open_design", "--input", `{"summary":"landing page concept","permissions":["repo.mutate.isolated:layout"]}`, "--json"}, strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatal("Run(design invoke --input --json) unexpectedly succeeded")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not allowed for design") {
		t.Fatalf("design invoke error = %v, want disallowed permission", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()
	artifacts, err := store.ListSkillArtifacts(context.Background(), sqlite.ListSkillArtifactsParams{})
	if err != nil {
		t.Fatalf("ListSkillArtifacts() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("design artifacts count = %d, want 0", len(artifacts))
	}
}

func TestRunDesignRequestCreatePersistsReviewableArtifactAndNoExecutionRun(t *testing.T) {
	root := testRepoRoot(t)
	briefPath := filepath.Join(root, "brief.json")
	if err := os.WriteFile(briefPath, []byte(`{
  "skill_key": "browser_open_design",
  "title": "Landing page redesign",
  "summary": "Create landing page concept"
}`), 0o644); err != nil {
		t.Fatalf("write brief = %v", err)
	}

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"design", "request", "create", "--brief", briefPath, "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(design request create --json) error = %v", err)
	}

	var created skills.ReviewArtifact
	if err := json.Unmarshal(stdout.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal request create output = %v", err)
	}
	if created.ArtifactType != designRequestArtifactType {
		t.Fatalf("artifact_type = %q, want %s", created.ArtifactType, designRequestArtifactType)
	}
	if created.Status != designRequestQueue {
		t.Fatalf("status = %q, want %s", created.Status, designRequestQueue)
	}
	if created.ExecutionProfile != "design_request" {
		t.Fatalf("execution_profile = %q, want design_request", created.ExecutionProfile)
	}
	if len(created.Permissions) != 0 {
		t.Fatalf("permissions = %#v, want empty", created.Permissions)
	}

	var artifactsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"design", "artifacts", "--json"}, strings.NewReader(""), &artifactsOutput); err != nil {
		t.Fatalf("Run(design artifacts --json) error = %v", err)
	}
	var artifacts designArtifactListView
	if err := json.Unmarshal(artifactsOutput.Bytes(), &artifacts); err != nil {
		t.Fatalf("unmarshal design artifacts output = %v", err)
	}
	if len(artifacts.Items) != 1 {
		t.Fatalf("design artifacts count = %d, want 1", len(artifacts.Items))
	}
	if artifacts.Items[0].ArtifactType != designRequestArtifactType {
		t.Fatalf("design artifact type = %q, want %s", artifacts.Items[0].ArtifactType, designRequestArtifactType)
	}
	if artifacts.Items[0].Status != designRequestQueue {
		t.Fatalf("design artifact status = %q, want %s", artifacts.Items[0].Status, designRequestQueue)
	}
}

func TestRunDesignRequestAcceptCreatesOutputArtifactAndRecordsEvents(t *testing.T) {
	root := testRepoRoot(t)
	configureOpenDesignDriver(
		t,
		`#!/usr/bin/env bash
printf '%s\n' '{"status":"completed","summary":"landing page concept generated","tool_key":"browser_open_design","artifacts":{"html_file":"/tmp/landing-page-concept.html"}}'
`,
	)

	briefPath := filepath.Join(root, "brief.json")
	if err := os.WriteFile(briefPath, []byte(`{
  "skill_key": "browser_open_design",
  "title": "Landing page concept",
  "summary": "Generate landing page concept",
  "permissions": ["repo.read"]
}`), 0o644); err != nil {
		t.Fatalf("write brief = %v", err)
	}

	var requestOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"design", "request", "create", "--brief", briefPath, "--json"}, strings.NewReader(""), &requestOutput); err != nil {
		t.Fatalf("Run(design request create --json) error = %v", err)
	}
	var requestArtifact skills.ReviewArtifact
	if err := json.Unmarshal(requestOutput.Bytes(), &requestArtifact); err != nil {
		t.Fatalf("unmarshal request create output = %v", err)
	}

	var acceptOutput bytes.Buffer
	if err := Run(
		context.Background(),
		root,
		[]string{"design", "artifact", "review", "accept", fmt.Sprintf("%d", requestArtifact.ID), "--json"},
		strings.NewReader(""),
		&acceptOutput,
	); err != nil {
		t.Fatalf("Run(design artifact review accept --json) error = %v", err)
	}
	var accepted struct {
		Artifact         skills.ReviewArtifact `json:"artifact"`
		Decision         string                `json:"decision"`
		OutputArtifactID int64                 `json:"output_artifact_id"`
	}
	if err := json.Unmarshal(acceptOutput.Bytes(), &accepted); err != nil {
		t.Fatalf("unmarshal review accept output = %v", err)
	}
	if accepted.Decision != "accepted" {
		t.Fatalf("decision = %q, want accepted", accepted.Decision)
	}
	if accepted.OutputArtifactID <= 0 {
		t.Fatalf("output_artifact_id = %d, want >0", accepted.OutputArtifactID)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()
	requestRow, err := store.GetSkillArtifact(context.Background(), requestArtifact.ID)
	if err != nil {
		t.Fatalf("GetSkillArtifact(request) error = %v", err)
	}
	outputRow, err := store.GetSkillArtifact(context.Background(), accepted.OutputArtifactID)
	if err != nil {
		t.Fatalf("GetSkillArtifact(output) error = %v", err)
	}

	if requestRow.Status != "accepted" {
		t.Fatalf("request status = %q, want accepted", requestRow.Status)
	}
	if outputRow.ArtifactType != designArtifactType {
		t.Fatalf("output artifact_type = %q, want %s", outputRow.ArtifactType, designArtifactType)
	}
	if outputRow.Status != designArtifactQueue {
		t.Fatalf("output status = %q, want %s", outputRow.Status, designArtifactQueue)
	}
	if outputRow.PermissionsJSON != "[\"repo.read\"]" {
		t.Fatalf("output permissions = %s, want [\"repo.read\"]", outputRow.PermissionsJSON)
	}

	var outputPayload map[string]any
	if err := json.Unmarshal([]byte(outputRow.OutputJSON), &outputPayload); err != nil {
		t.Fatalf("unmarshal output payload = %v", err)
	}
	if fmt.Sprintf("%v", outputPayload["request_id"]) == "" {
		t.Fatalf("output payload missing request_id: %v", outputPayload)
	}
	artifactsValue, ok := outputPayload["artifacts"].(map[string]any)
	if !ok {
		t.Fatalf("output payload artifacts type = %T, want map", outputPayload["artifacts"])
	}
	htmlPath, ok := artifactsValue["html_file"]
	if !ok || strings.TrimSpace(fmt.Sprintf("%v", htmlPath)) == "" {
		t.Fatalf("output payload artifacts = %v, want html_file", artifactsValue)
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	for _, wantType := range []string{
		string(events.EventDesignRequestCreated),
		string(events.EventDesignExecutionStarted),
		string(events.EventDesignArtifactCreated),
	} {
		if !designHasEvent(logsOutput.Bytes(), wantType) {
			t.Fatalf("logs missing %s", wantType)
		}
	}

	var reviewedArtifacts designArtifactListView
	var reviewedArtifactsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"design", "artifacts", "--json"}, strings.NewReader(""), &reviewedArtifactsOutput); err != nil {
		t.Fatalf("Run(design artifacts --json) error = %v", err)
	}
	if err := json.Unmarshal(reviewedArtifactsOutput.Bytes(), &reviewedArtifacts); err != nil {
		t.Fatalf("unmarshal design artifacts output = %v", err)
	}
	if len(reviewedArtifacts.Items) != 2 {
		t.Fatalf("design artifacts count = %d, want 2", len(reviewedArtifacts.Items))
	}
}

func TestRunDesignRequestAcceptFailureRejectsRequestAndLeavesNoExpandedPermissions(t *testing.T) {
	root := testRepoRoot(t)
	configureOpenDesignDriver(
		t,
		`#!/usr/bin/env bash
printf '%s\n' '{"status":"failed","summary":"render failed","tool_key":"browser_open_design","artifacts":{}}'
`,
	)

	briefPath := filepath.Join(root, "brief.json")
	if err := os.WriteFile(briefPath, []byte(`{
  "skill_key": "browser_open_design",
  "title": "Landing page concept",
  "summary": "Generate landing page concept",
  "permissions": []
}`), 0o644); err != nil {
		t.Fatalf("write brief = %v", err)
	}

	var requestOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"design", "request", "create", "--brief", briefPath, "--json"}, strings.NewReader(""), &requestOutput); err != nil {
		t.Fatalf("Run(design request create --json) error = %v", err)
	}
	var requestArtifact skills.ReviewArtifact
	if err := json.Unmarshal(requestOutput.Bytes(), &requestArtifact); err != nil {
		t.Fatalf("unmarshal request create output = %v", err)
	}

	var acceptOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"design", "artifact", "review", "accept", fmt.Sprintf("%d", requestArtifact.ID), "--json"}, strings.NewReader(""), &acceptOutput); err != nil {
		t.Fatalf("Run(design artifact review accept --json) error = %v", err)
	}
	var rejected struct {
		Artifact         skills.ReviewArtifact `json:"artifact"`
		Decision         string                `json:"decision"`
		OutputArtifactID int64                 `json:"output_artifact_id"`
	}
	if err := json.Unmarshal(acceptOutput.Bytes(), &rejected); err != nil {
		t.Fatalf("unmarshal review accept output = %v", err)
	}
	if rejected.Decision != "rejected" {
		t.Fatalf("decision = %q, want rejected", rejected.Decision)
	}
	if rejected.OutputArtifactID != 0 {
		t.Fatalf("output_artifact_id = %d, want 0", rejected.OutputArtifactID)
	}
	if rejected.Artifact.Status != "rejected" {
		t.Fatalf("artifact status = %q, want rejected", rejected.Artifact.Status)
	}
	if !strings.Contains(strings.ToLower(rejected.Artifact.ReviewReason), "design execution failed") {
		t.Fatalf("review reason = %q, want design execution failed", rejected.Artifact.ReviewReason)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	row, err := store.GetSkillArtifact(context.Background(), requestArtifact.ID)
	if err != nil {
		t.Fatalf("GetSkillArtifact() error = %v", err)
	}
	if row.Status != "rejected" {
		t.Fatalf("request status = %q, want rejected", row.Status)
	}
	if row.PermissionsJSON != "[]" {
		t.Fatalf("permissions = %s, want []", row.PermissionsJSON)
	}

	allArtifacts, err := store.ListSkillArtifacts(context.Background(), sqlite.ListSkillArtifactsParams{})
	if err != nil {
		t.Fatalf("ListSkillArtifacts() error = %v", err)
	}
	var designOutputCount int
	var designRequestCount int
	for _, artifact := range allArtifacts {
		if isDesignOutputArtifactType(artifact.ArtifactType) {
			designOutputCount++
		}
		if isDesignRequestArtifactType(artifact.ArtifactType) {
			designRequestCount++
		}
	}
	if designOutputCount != 0 {
		t.Fatalf("design output artifact count = %d, want 0", designOutputCount)
	}
	if designRequestCount != 1 {
		t.Fatalf("design request artifact count = %d, want 1", designRequestCount)
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	if !designHasEvent(logsOutput.Bytes(), string(events.EventDesignExecutionStarted)) {
		t.Fatalf("logs missing %s", events.EventDesignExecutionStarted)
	}
	if designHasEvent(logsOutput.Bytes(), string(events.EventDesignArtifactCreated)) {
		t.Fatalf("logs contain %s", events.EventDesignArtifactCreated)
	}
}

func designHasEvent(payload []byte, eventType string) bool {
	var logs struct {
		Logs []struct {
			Type string `json:"type"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(payload, &logs); err != nil {
		return false
	}
	for _, entry := range logs.Logs {
		if entry.Type == eventType {
			return true
		}
	}
	return false
}

func configureOpenDesignDriver(t *testing.T, source string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "open-design-driver.sh")
	if err := os.WriteFile(path, []byte(source), 0o755); err != nil {
		t.Fatalf("write open design driver = %v", err)
	}
	t.Setenv("ODIN_HUGINN_OPEN_DESIGN_DRIVER", path)
}

func seedDesignFixture(t *testing.T, root string, key string, title string, summary string, tags []string, appliesTo []string, permissions []string) {
	t.Helper()

	scriptPath := filepath.Join(root, "scripts", "skills", key+".sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(skill script dir) error = %v", err)
	}
	scriptBytes := []byte(`#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"ok","summary":"ok","output":{"message":"ok"}}'
`)
	if err := os.WriteFile(scriptPath, scriptBytes, 0o755); err != nil {
		t.Fatalf("write skill script error = %v", err)
	}

	tagsJSON, _ := json.Marshal(tags)
	appliesToJSON, _ := json.Marshal(appliesTo)
	if len(permissions) == 0 {
		permissions = []string{"repo.read"}
	}
	permissionsJSON, _ := json.Marshal(permissions)
	specPath := filepath.Join(root, key+".json")
	spec := fmt.Sprintf(`{
  "key": %q,
  "title": %q,
  "summary": %q,
  "status": "active",
  "version": "1.0.0",
  "enabled": true,
  "tags": %s,
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": %s,
  "scopes": ["project"],
  "permissions": %s,
  "handler_type": "command",
  "handler_ref": %q,
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Test skill.",
    "When to Use": "Testing.",
    "Inputs": "Any.",
    "Procedure": "Return JSON.",
    "Outputs": "A review response.",
    "Constraints": "None.",
    "Success Criteria": "Works in tests."
  }
}`, key, title, summary, tagsJSON, appliesToJSON, permissionsJSON, filepath.ToSlash(filepath.Join("scripts", "skills", key+".sh")))

	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write skill spec error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"skills", "create", "--spec", specPath, "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(skills create) error = %v", err)
	}
}
