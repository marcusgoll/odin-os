package browser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/adapters/huginnbrowser"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

var _ ReadOnlyRunner = Service{}
var _ PluginRunner = Service{}

func TestReadOnlyServiceRecordsGoalEvidenceAndAudit(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	ctx := context.Background()
	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Collect browser evidence"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}

	adapter := &recordingAdapter{response: huginnbrowser.Response{
		Status:               "completed",
		AdapterKind:          "stub_local",
		VisitedURLs:          []string{"https://example.com/docs"},
		PageResults:          []huginnbrowser.PageResult{{URL: "https://example.com/docs", Status: "visited", Mode: "browser", Title: "Docs", Summary: "Stub summary for https://example.com/docs"}},
		ExtractedTextSummary: "Stub summary for https://example.com/docs",
		Screenshots:          []string{"stub://screenshot/example-com-docs"},
		ActionLog:            []string{"validated_read_only_request", "stub_snapshot_recorded"},
	}}
	result, err := Service{Store: store, Adapter: adapter}.Run(ctx, ReadOnlyTask{
		GoalID:             goal.ID,
		WorkerMode:         "browser",
		Objective:          "Collect public documentation",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/docs"},
		MaxPages:           2,
		MaxDurationSeconds: 30,
		EvidenceRequired:   true,
		Actions:            []string{"read"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != "recorded" || result.Evidence.ID <= 0 || result.Evidence.GoalID != goal.ID {
		t.Fatalf("result = %+v, want recorded evidence linked to goal %d", result, goal.ID)
	}
	if !adapter.called {
		t.Fatal("adapter was not called")
	}
	if adapter.request.Mode != "browser" {
		t.Fatalf("adapter request mode = %q, want browser", adapter.request.Mode)
	}
	if result.AdapterStatus != "completed" || result.AdapterKind != "stub_local" || result.ExtractedTextSummary == "" {
		t.Fatalf("result = %+v, want adapter response surfaced", result)
	}
	if result.RealBrowserEvidence || result.BrowserProofKind != "stub_contract_only" {
		t.Fatalf("browser proof = real:%v kind:%q, want explicit stub-only proof classification", result.RealBrowserEvidence, result.BrowserProofKind)
	}
	if len(result.PageResults) != 1 || result.PageResults[0].Status != "visited" || result.PageResults[0].Title != "Docs" {
		t.Fatalf("PageResults = %#v, want adapter page results surfaced", result.PageResults)
	}
	if !strings.Contains(result.Evidence.PayloadJSON, `"adapter_kind":"stub_local"`) || !strings.Contains(result.Evidence.PayloadJSON, `"visited_urls":["https://example.com/docs"]`) {
		t.Fatalf("evidence payload = %s, want adapter response", result.Evidence.PayloadJSON)
	}
	if !strings.Contains(result.Evidence.PayloadJSON, `"browser_proof_kind":"stub_contract_only"`) || !strings.Contains(result.Evidence.PayloadJSON, `"real_browser_evidence":false`) {
		t.Fatalf("evidence payload = %s, want explicit stub-only proof classification", result.Evidence.PayloadJSON)
	}
	if !strings.Contains(result.Evidence.PayloadJSON, `"page_results":[{"url":"https://example.com/docs","status":"visited","mode":"browser","title":"Docs","summary":"Stub summary for https://example.com/docs"}]`) {
		t.Fatalf("evidence payload = %s, want adapter page results", result.Evidence.PayloadJSON)
	}

	shown, err := store.GetGoal(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetGoal() error = %v", err)
	}
	if shown.Status != sqlite.GoalStatusCreated {
		t.Fatalf("goal status = %q, want unchanged created", shown.Status)
	}

	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var found bool
	for _, event := range events {
		if event.StreamType == "goal" && event.StreamID == goal.ID && event.Type == runtimeevents.EventGoalEvidenceRecorded {
			found = true
		}
	}
	if !found {
		t.Fatalf("events = %+v, want goal.evidence_recorded", events)
	}
}

func TestReadOnlyServiceClassifiesLiveBrowserEvidenceAsRealBrowserProof(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	ctx := context.Background()
	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Collect live browser evidence"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}

	adapter := &recordingAdapter{response: huginnbrowser.Response{
		Status:               "completed",
		AdapterKind:          "huginn_live",
		VisitedURLs:          []string{"https://example.com/docs"},
		PageResults:          []huginnbrowser.PageResult{{URL: "https://example.com/docs", Status: "visited", Mode: "browser", Title: "Docs", Summary: "Live browser summary"}},
		ExtractedTextSummary: "Live browser summary",
		Screenshots:          []string{"/tmp/odin-browser-proof.png"},
		ActionLog:            []string{"validated_read_only_request", "browser_mode_selected", "opened_start_url", "captured_read_only_evidence", "screenshot_captured"},
	}}
	result, err := Service{Store: store, Adapter: adapter}.Run(ctx, ReadOnlyTask{
		GoalID:             goal.ID,
		WorkerMode:         "browser",
		Objective:          "Collect public documentation",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/docs"},
		MaxPages:           2,
		MaxDurationSeconds: 30,
		EvidenceRequired:   true,
		Actions:            []string{"read"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.RealBrowserEvidence || result.BrowserProofKind != "live_browser_readonly" {
		t.Fatalf("browser proof = real:%v kind:%q, want live browser proof classification", result.RealBrowserEvidence, result.BrowserProofKind)
	}
	if !strings.Contains(result.Evidence.PayloadJSON, `"browser_proof_kind":"live_browser_readonly"`) || !strings.Contains(result.Evidence.PayloadJSON, `"real_browser_evidence":true`) {
		t.Fatalf("evidence payload = %s, want live browser proof classification", result.Evidence.PayloadJSON)
	}
}

func TestPluginRunReadOnlyRecordsEvidenceWithoutApproval(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	ctx := context.Background()
	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Collect browser plugin evidence"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}

	adapter := &recordingAdapter{response: huginnbrowser.Response{
		Status:               "completed",
		AdapterKind:          "stub_local",
		VisitedURLs:          []string{"https://example.com/docs"},
		ExtractedTextSummary: "Plugin summary",
		ActionLog:            []string{"validated_read_only_request"},
	}}
	response, err := Service{Store: store, Adapter: adapter}.RunPlugin(ctx, PluginRequest{
		RequestID:          "browser-plugin-readonly-1",
		GoalID:             goal.ID,
		WorkerMode:         "browser",
		Objective:          "Collect public documentation",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/docs"},
		MaxPages:           1,
		MaxDurationSeconds: 30,
		EvidenceRequired:   true,
		Actions:            []string{"navigate", "snapshot"},
	})
	if err != nil {
		t.Fatalf("RunPlugin() error = %v", err)
	}
	if response.Status != "recorded" || response.RiskClass != RiskClassReadOnly || response.ApprovalRequired {
		t.Fatalf("response = %+v, want recorded read-only response without approval", response)
	}
	if response.Evidence == nil || response.Evidence.ID <= 0 || response.Evidence.Type != EvidenceType {
		t.Fatalf("response evidence = %+v, want browser evidence artifact", response.Evidence)
	}
	if !adapter.called {
		t.Fatal("adapter was not called for read-only plugin request")
	}
	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var evidenceEvent bool
	var approvalEvent bool
	for _, event := range events {
		if event.Type == runtimeevents.EventGoalEvidenceRecorded {
			evidenceEvent = true
		}
		if event.Type == runtimeevents.EventApprovalRequested {
			approvalEvent = true
		}
	}
	if !evidenceEvent {
		t.Fatalf("events = %+v, want goal.evidence_recorded", events)
	}
	if approvalEvent {
		t.Fatalf("events = %+v, want no approval for read-only plugin request", events)
	}
}

func TestPluginRunMutationCreatesApprovalAndDoesNotCallAdapter(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	ctx := context.Background()
	task := createBrowserApprovalTask(t, store)
	adapter := &recordingAdapter{}

	response, err := Service{Store: store, Adapter: adapter}.RunPlugin(ctx, PluginRequest{
		RequestID:          "browser-plugin-mutation-1",
		TaskID:             task.ID,
		Objective:          "Submit the reviewed external form",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/form"},
		MaxPages:           1,
		MaxDurationSeconds: 30,
		Actions:            []string{"navigate", "submit_form"},
		RequestedBy:        "operator",
	})
	if err != nil {
		t.Fatalf("RunPlugin() error = %v", err)
	}
	if response.Status != "approval_required" || response.RiskClass != RiskClassExternalMutation || !response.ApprovalRequired || response.ApprovalID <= 0 {
		t.Fatalf("response = %+v, want approval-required mutation response", response)
	}
	if len(response.MutatingActions) != 1 || response.MutatingActions[0] != "submit_form" {
		t.Fatalf("MutatingActions = %#v, want submit_form", response.MutatingActions)
	}
	if adapter.called {
		t.Fatal("adapter was called for mutation-class plugin request")
	}
	blocked, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if blocked.Status != "blocked" {
		t.Fatalf("task status = %q, want blocked", blocked.Status)
	}
	approval, err := store.GetApproval(ctx, response.ApprovalID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if approval.Status != "pending" || approval.TaskID != task.ID {
		t.Fatalf("approval = %+v, want pending approval linked to task %d", approval, task.ID)
	}
	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var approvalEvent bool
	var evidenceEvent bool
	for _, event := range events {
		if event.Type == runtimeevents.EventApprovalRequested {
			approvalEvent = true
		}
		if event.Type == runtimeevents.EventGoalEvidenceRecorded {
			evidenceEvent = true
		}
	}
	if !approvalEvent {
		t.Fatalf("events = %+v, want approval.requested", events)
	}
	if evidenceEvent {
		t.Fatalf("events = %+v, want no browser evidence because mutation did not execute", events)
	}
}

func TestPluginRunMutationRequiresTaskID(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	adapter := &recordingAdapter{}
	response, err := Service{Store: store, Adapter: adapter}.RunPlugin(context.Background(), PluginRequest{
		Objective:          "Submit external form",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/form"},
		MaxPages:           1,
		MaxDurationSeconds: 30,
		Actions:            []string{"submit_form"},
	})
	if err == nil || !strings.Contains(err.Error(), "task_id is required") {
		t.Fatalf("RunPlugin() error = %v, want task_id validation", err)
	}
	if response.RiskClass != RiskClassExternalMutation || response.ErrorCode != "invalid_request" {
		t.Fatalf("response = %+v, want invalid mutation request response", response)
	}
	if adapter.called {
		t.Fatal("adapter was called for invalid mutation-class plugin request")
	}
}

func TestWorkEvidenceRecordsRunArtifactWithoutCompletingTask(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	ctx := context.Background()
	task := createBrowserApprovalTask(t, store)
	adapter := &recordingAdapter{response: huginnbrowser.Response{
		Status:               "completed",
		AdapterKind:          "stub_local",
		VisitedURLs:          []string{"https://example.com/docs"},
		PageResults:          []huginnbrowser.PageResult{{URL: "https://example.com/docs", Status: "visited", Mode: "browser", Title: "Docs", Summary: "Browser evidence summary"}},
		ExtractedTextSummary: "Browser evidence summary",
		Screenshots:          []string{"stub://screenshot/example"},
		ScreenshotMetadata:   []huginnbrowser.ScreenshotMetadata{{Path: "stub://screenshot/example", URL: "https://example.com/docs", Title: "Docs"}},
		SelectedLinks:        []huginnbrowser.SelectedLink{{Text: "Docs", URL: "https://example.com/docs#details", Reason: "primary evidence link"}},
		DownloadedFiles:      []huginnbrowser.DownloadedFileMetadata{{Name: "evidence.txt", Path: "stub://downloads/evidence.txt", ContentType: "text/plain", SizeBytes: 12}},
		FormStateSummary:     "No form values captured or submitted.",
		BrowserErrorRecoveryNotes: []string{
			"No browser recovery required.",
		},
		Confidence:  "deterministic_test",
		Limitations: []string{"adapter fixture only"},
		ActionLog:   []string{"validated_read_only_request"},
	}}

	result, err := Service{Store: store, Adapter: adapter}.RunWorkEvidence(ctx, WorkEvidenceTask{
		TaskID:             task.ID,
		WorkerMode:         "browser",
		Objective:          "Collect public documentation",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/docs"},
		MaxPages:           1,
		MaxDurationSeconds: 30,
		EvidenceRequired:   true,
		Actions:            []string{"read"},
	})
	if err != nil {
		t.Fatalf("RunWorkEvidence() error = %v", err)
	}
	if !adapter.called {
		t.Fatal("adapter was not called")
	}
	if result.Status != "recorded" || result.TaskID != task.ID || result.RunID <= 0 || result.RunArtifactID <= 0 {
		t.Fatalf("result = %+v, want recorded run artifact linked to task", result)
	}
	artifacts, err := store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: result.RunID, ArtifactType: "browser_evidence"})
	if err != nil {
		t.Fatalf("ListRunArtifacts() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("browser artifacts len = %d, want 1", len(artifacts))
	}
	for _, want := range []string{"screenshot_metadata", "selected_links", "downloaded_files", "form_state_summary", "browser_error_recovery_notes", "confidence", "limitations"} {
		if !strings.Contains(artifacts[0].DetailsJSON, want) {
			t.Fatalf("artifact details = %s, want %q", artifacts[0].DetailsJSON, want)
		}
	}
	shown, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if shown.Status != "queued" {
		t.Fatalf("task status = %q, want queued so browser evidence does not count as completion", shown.Status)
	}
}

func TestWorkEvidenceTimeoutFailsTaskAndRecordsRecoveryRecommendation(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	ctx := context.Background()
	task := createBrowserApprovalTask(t, store)
	adapter := &recordingAdapter{response: huginnbrowser.Response{
		Status:               "timeout",
		AdapterKind:          "stub_local",
		VisitedURLs:          []string{"https://example.com/docs"},
		ExtractedTextSummary: "Browser capture timed out.",
		ErrorCode:            "worker_timeout",
		ErrorMessage:         "capture exceeded timeout",
		BrowserErrorRecoveryNotes: []string{
			"Retry with fewer pages or a longer timeout.",
		},
	}}

	result, err := Service{Store: store, Adapter: adapter}.RunWorkEvidence(ctx, WorkEvidenceTask{
		TaskID:             task.ID,
		WorkerMode:         "browser",
		Objective:          "Collect public documentation",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/docs"},
		MaxPages:           1,
		MaxDurationSeconds: 30,
		EvidenceRequired:   true,
		Actions:            []string{"read"},
	})
	if err != nil {
		t.Fatalf("RunWorkEvidence() error = %v", err)
	}
	if result.Status != "failed" || result.ErrorCode != "worker_timeout" || result.RunArtifactID <= 0 {
		t.Fatalf("result = %+v, want failed timeout artifact", result)
	}
	shown, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if shown.Status != "failed" {
		t.Fatalf("task status = %q, want failed after browser capture failure", shown.Status)
	}
	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var recoveryEvent bool
	for _, event := range events {
		if event.Type == runtimeevents.EventTaskRecoveryRecommended {
			recoveryEvent = true
		}
	}
	if !recoveryEvent {
		t.Fatalf("events = %+v, want task.recovery_recommended for failed browser capture", events)
	}
}

func TestReadOnlyServicePassesSiteProfilesToAdapter(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	ctx := context.Background()
	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Collect profiled browser evidence"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}

	adapter := &recordingAdapter{response: huginnbrowser.Response{
		Status:               "completed",
		AdapterKind:          "stub_local",
		VisitedURLs:          []string{"https://example.com/docs"},
		ExtractedTextSummary: "Profiled summary",
		ActionLog:            []string{"site_profile_applied"},
	}}
	result, err := Service{Store: store, Adapter: adapter}.Run(ctx, ReadOnlyTask{
		GoalID:             goal.ID,
		WorkerMode:         "fetch",
		Objective:          "Collect public documentation",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/docs"},
		MaxPages:           2,
		MaxDurationSeconds: 30,
		SiteProfiles: []huginnbrowser.SiteProfile{
			{Domain: "example.com", MaxPages: 1, MinDelayMS: 250, MaxDurationSeconds: 10, ModeAllowed: "fetch"},
		},
		Actions: []string{"read"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !adapter.called || len(adapter.request.SiteProfiles) != 1 {
		t.Fatalf("adapter called=%v profiles=%#v, want one site profile", adapter.called, adapter.request.SiteProfiles)
	}
	profile := adapter.request.SiteProfiles[0]
	if profile.Domain != "example.com" || profile.MaxPages != 1 || profile.MinDelayMS != 250 || profile.MaxDurationSeconds != 10 || profile.ModeAllowed != "fetch" {
		t.Fatalf("adapter profile = %+v, want task site profile passed through", profile)
	}
	if len(result.SiteProfiles) != 1 || result.SiteProfiles[0] != profile {
		t.Fatalf("result site profiles = %#v, want surfaced task profile", result.SiteProfiles)
	}
	if !strings.Contains(result.Evidence.PayloadJSON, `"site_profiles":[{"domain":"example.com","max_pages":1,"min_delay_ms":250,"max_duration_seconds":10,"mode_allowed":"fetch"}]`) {
		t.Fatalf("evidence payload = %s, want site profile persisted in task payload", result.Evidence.PayloadJSON)
	}
}

func TestReadOnlyServiceRejectsBrowserSessionUntilAttachIsImplemented(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	ctx := context.Background()
	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Collect authenticated browser evidence"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	session, err := store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
		Name:           "google-main",
		Domain:         "example.com",
		AccountHint:    "marcus",
		PermissionTier: sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	session, err = store.UpdateBrowserSessionStatus(ctx, sqlite.UpdateBrowserSessionStatusParams{
		SessionID: session.ID,
		Status:    sqlite.BrowserSessionStatusVerified,
		Actor:     "test",
		Reason:    "test verified metadata",
	})
	if err != nil {
		t.Fatalf("UpdateBrowserSessionStatus() error = %v", err)
	}

	adapter := &recordingAdapter{response: huginnbrowser.Response{
		Status:               "completed",
		AdapterKind:          "stub_local",
		VisitedURLs:          []string{"https://example.com/account"},
		ExtractedTextSummary: "Session referenced summary",
		ActionLog:            []string{"validated_read_only_request"},
	}}
	_, err = Service{Store: store, Adapter: adapter}.Run(ctx, ReadOnlyTask{
		GoalID:             goal.ID,
		WorkerMode:         "browser",
		Objective:          "Collect authenticated account evidence",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/account"},
		MaxPages:           1,
		MaxDurationSeconds: 30,
		BrowserSessionID:   session.ID,
		Actions:            []string{"read"},
	})
	if err == nil || !strings.Contains(err.Error(), "authenticated browser session attachment is not implemented") {
		t.Fatalf("Run() error = %v, want fail-closed attach boundary", err)
	}
	if adapter.called {
		t.Fatalf("adapter was called for unsupported authenticated browser session attach")
	}
}

func TestReadOnlyServiceDefaultsToStubAdapterSelection(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	goal, err := store.CreateGoal(context.Background(), sqlite.CreateGoalParams{Title: "Default adapter"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	t.Setenv(huginnbrowser.AdapterEnvVar, "")
	result, err := Service{Store: store}.Run(context.Background(), ReadOnlyTask{
		GoalID:             goal.ID,
		Objective:          "Collect public documentation",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/docs"},
		MaxPages:           2,
		MaxDurationSeconds: 30,
		EvidenceRequired:   true,
		Actions:            []string{"read"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.AdapterKind != "stub_local" || result.AdapterStatus != "completed" {
		t.Fatalf("result = %+v, want default stub adapter", result)
	}
}

func TestReadOnlyServiceCanSelectLiveAdapterFromEnv(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	goal, err := store.CreateGoal(context.Background(), sqlite.CreateGoalParams{Title: "Live adapter skeleton"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	t.Setenv(huginnbrowser.AdapterEnvVar, "live")
	command := writeBrowserExecutorFixture(t, `#!/usr/bin/env bash
cat >/dev/null
exit 0
`)
	t.Setenv(huginnbrowser.LiveCommandEnvVar, command)
	t.Setenv(huginnbrowser.LiveAllowedCommandsEnvVar, command)
	result, err := Service{Store: store}.Run(context.Background(), ReadOnlyTask{
		GoalID:             goal.ID,
		Objective:          "Collect public documentation",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/docs"},
		MaxPages:           2,
		MaxDurationSeconds: 30,
		EvidenceRequired:   true,
		Actions:            []string{"read"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.AdapterKind != "huginn_live" || result.AdapterStatus != "not_implemented" {
		t.Fatalf("result = %+v, want live adapter skeleton not_implemented", result)
	}
	if !strings.Contains(result.Evidence.PayloadJSON, `"adapter_kind":"huginn_live"`) || !strings.Contains(result.Evidence.PayloadJSON, `"status":"not_implemented"`) {
		t.Fatalf("evidence payload = %s, want live adapter not implemented response", result.Evidence.PayloadJSON)
	}
}

func TestReadOnlyServiceRecordsLiveAdapterEmptyAllowlistFailure(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	goal, err := store.CreateGoal(context.Background(), sqlite.CreateGoalParams{Title: "Live adapter empty allowlist"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	command := writeBrowserExecutorFixture(t, `#!/usr/bin/env bash
cat >/dev/null
printf '{"status":"completed","adapter_kind":"huginn_live"}'
`)
	t.Setenv(huginnbrowser.AdapterEnvVar, "live")
	t.Setenv(huginnbrowser.LiveCommandEnvVar, command)
	t.Setenv(huginnbrowser.LiveAllowedCommandsEnvVar, "")
	result, err := Service{Store: store}.Run(context.Background(), ReadOnlyTask{
		GoalID:             goal.ID,
		Objective:          "Collect public documentation",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/docs"},
		MaxPages:           2,
		MaxDurationSeconds: 30,
		EvidenceRequired:   true,
		Actions:            []string{"read"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.AdapterKind != "huginn_live" || result.AdapterStatus != "failed" || !strings.Contains(result.Evidence.PayloadJSON, `"error_code":"command_allowlist_empty"`) {
		t.Fatalf("result = %+v payload=%s, want persisted empty-allowlist evidence", result, result.Evidence.PayloadJSON)
	}
}

func TestReadOnlyServiceRecordsLiveAdapterMissingCommandFailure(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	goal, err := store.CreateGoal(context.Background(), sqlite.CreateGoalParams{Title: "Live adapter missing command"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	t.Setenv(huginnbrowser.AdapterEnvVar, "live")
	t.Setenv(huginnbrowser.LiveCommandEnvVar, "")
	result, err := Service{Store: store}.Run(context.Background(), ReadOnlyTask{
		GoalID:             goal.ID,
		Objective:          "Collect public documentation",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/docs"},
		MaxPages:           2,
		MaxDurationSeconds: 30,
		EvidenceRequired:   true,
		Actions:            []string{"read"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.AdapterKind != "huginn_live" || result.AdapterStatus != "failed" || !strings.Contains(result.Evidence.PayloadJSON, `"error_code":"command_not_configured"`) {
		t.Fatalf("result = %+v payload=%s, want persisted missing-command evidence", result, result.Evidence.PayloadJSON)
	}
}

func TestReadOnlyServiceRejectsDisallowedDomain(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	goal, err := store.CreateGoal(context.Background(), sqlite.CreateGoalParams{Title: "Domain check"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}

	adapter := &recordingAdapter{}
	_, err = Service{Store: store, Adapter: adapter}.Run(context.Background(), ReadOnlyTask{
		GoalID:             goal.ID,
		Objective:          "Collect public documentation",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://not-example.test/docs"},
		MaxPages:           2,
		MaxDurationSeconds: 30,
		EvidenceRequired:   true,
		Actions:            []string{"read"},
	})
	if err == nil || !strings.Contains(err.Error(), "disallowed domain") {
		t.Fatalf("Run() error = %v, want disallowed domain rejection", err)
	}
	if adapter.called {
		t.Fatal("adapter was called before validation rejected disallowed domain")
	}
}

func TestReadOnlyServiceReturnsStructuredAdapterFailure(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	goal, err := store.CreateGoal(context.Background(), sqlite.CreateGoalParams{Title: "Adapter failure"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}

	result, err := Service{Store: store, Adapter: &recordingAdapter{err: errors.New("stub adapter unavailable")}}.Run(context.Background(), ReadOnlyTask{
		GoalID:             goal.ID,
		Objective:          "Collect public documentation",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/docs"},
		MaxPages:           2,
		MaxDurationSeconds: 30,
		EvidenceRequired:   true,
		Actions:            []string{"read"},
	})
	if err == nil || !strings.Contains(err.Error(), "browser adapter failed") {
		t.Fatalf("Run() error = %v, want adapter failure", err)
	}
	if result.Status != "failed" || result.ErrorCode != "adapter_failed" || !strings.Contains(result.ErrorMessage, "stub adapter unavailable") {
		t.Fatalf("result = %+v, want structured adapter failure", result)
	}
}

func TestReadOnlyServiceRejectsMutationActionAndInvalidLimits(t *testing.T) {
	base := ReadOnlyTask{
		GoalID:             1,
		Objective:          "Collect public documentation",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/docs"},
		MaxPages:           2,
		MaxDurationSeconds: 30,
		EvidenceRequired:   true,
		Actions:            []string{"read"},
	}

	for _, test := range []struct {
		name string
		task ReadOnlyTask
		want string
	}{
		{name: "empty objective", task: withObjective(base, ""), want: "objective is required"},
		{name: "mutation action", task: withActions(base, "submit_form"), want: "mutation action"},
		{name: "too many pages", task: withMaxPages(base, MaxPagesLimit+1), want: "max_pages"},
		{name: "too long", task: withMaxDuration(base, MaxDurationSecondsLimit+1), want: "max_duration_seconds"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateReadOnlyTask(test.task)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateReadOnlyTask() error = %v, want %q", err, test.want)
			}
		})
	}
}

func withObjective(task ReadOnlyTask, objective string) ReadOnlyTask {
	task.Objective = objective
	return task
}

func withActions(task ReadOnlyTask, actions ...string) ReadOnlyTask {
	task.Actions = actions
	return task
}

func withMaxPages(task ReadOnlyTask, maxPages int) ReadOnlyTask {
	task.MaxPages = maxPages
	return task
}

func withMaxDuration(task ReadOnlyTask, maxDuration int) ReadOnlyTask {
	task.MaxDurationSeconds = maxDuration
	return task
}

func openBrowserTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}
	store.Now = func() time.Time {
		return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	}
	return store
}

func createBrowserApprovalTask(t *testing.T, store *sqlite.Store) sqlite.Task {
	t.Helper()
	ctx := context.Background()
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		GitHubRepo:    "example/odin-os",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "browser-mutation-approval",
		Title:       "Review browser mutation",
		Status:      "queued",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return task
}

func writeBrowserExecutorFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "browser-executor-fixture.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

type recordingAdapter struct {
	called   bool
	request  huginnbrowser.Request
	response huginnbrowser.Response
	err      error
}

func (adapter *recordingAdapter) Run(_ context.Context, request huginnbrowser.Request) (huginnbrowser.Response, error) {
	adapter.called = true
	adapter.request = request
	if adapter.err != nil {
		return huginnbrowser.Response{}, adapter.err
	}
	return adapter.response, nil
}
