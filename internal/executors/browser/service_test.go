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
	if len(result.PageResults) != 1 || result.PageResults[0].Status != "visited" || result.PageResults[0].Title != "Docs" {
		t.Fatalf("PageResults = %#v, want adapter page results surfaced", result.PageResults)
	}
	if !strings.Contains(result.Evidence.PayloadJSON, `"adapter_kind":"stub_local"`) || !strings.Contains(result.Evidence.PayloadJSON, `"visited_urls":["https://example.com/docs"]`) {
		t.Fatalf("evidence payload = %s, want adapter response", result.Evidence.PayloadJSON)
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

func TestServiceMutatingRequestCreatesApprovalAndDoesNotExecute(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	ctx := context.Background()
	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Approve browser mutation"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
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
		ProjectID:       project.ID,
		Key:             "browser-mutation",
		Title:           "Submit browser form",
		Status:          "queued",
		Scope:           "odin-core",
		RequestedBy:     "operator",
		ExecutionIntent: "mutation",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	adapter := &recordingAdapter{}
	result, err := Service{Store: store, Adapter: adapter}.Run(ctx, ReadOnlyTask{
		GoalID:             goal.ID,
		TaskID:             task.ID,
		WorkerMode:         "browser",
		Objective:          "Submit the login form",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/login"},
		MaxPages:           1,
		MaxDurationSeconds: 30,
		EvidenceRequired:   true,
		Actions:            []string{"submit_form"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if adapter.called {
		t.Fatal("adapter was called for approval-required mutation")
	}
	if result.Status != "approval_required" || !result.ApprovalRequired || result.ApprovalID <= 0 || result.RiskClass != RiskExternalMutation {
		t.Fatalf("result = %+v, want approval_required external mutation with approval id", result)
	}
	if result.Evidence.ID <= 0 || result.EvidenceType != EvidenceTypeApprovalRequired {
		t.Fatalf("result evidence = %+v type=%q, want blocked mutation evidence", result.Evidence, result.EvidenceType)
	}
	if !strings.Contains(result.Evidence.PayloadJSON, `"risk_class":"external_mutation"`) || !strings.Contains(result.Evidence.PayloadJSON, `"approval_required":true`) {
		t.Fatalf("evidence payload = %s, want risk and approval flag", result.Evidence.PayloadJSON)
	}

	approval, err := store.GetLatestTaskApproval(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskApproval() error = %v", err)
	}
	if approval.ID != result.ApprovalID || approval.Status != "pending" {
		t.Fatalf("approval = %+v, want pending approval %d", approval, result.ApprovalID)
	}
	updatedTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if updatedTask.Status != "blocked" || updatedTask.BlockedReason != "approval_required" {
		t.Fatalf("updated task status=%q blocked_reason=%q, want blocked approval_required", updatedTask.Status, updatedTask.BlockedReason)
	}

	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var approvalEvent bool
	var evidenceEvent bool
	for _, event := range events {
		if event.Type == runtimeevents.EventApprovalRequested && event.StreamID == approval.ID {
			approvalEvent = true
		}
		if event.Type == runtimeevents.EventGoalEvidenceRecorded && event.StreamID == goal.ID {
			evidenceEvent = true
		}
	}
	if !approvalEvent || !evidenceEvent {
		t.Fatalf("events = %+v, want approval requested and goal evidence audit events", events)
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

func TestReadOnlyServiceAttachesVerifiedBrowserSessionMetadata(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	ctx := context.Background()
	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Collect authenticated browser evidence"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	session, err := store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
		Name:           "google-main",
		Domain:         "google.com",
		AccountHint:    "marcus",
		PermissionTier: sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	session, _, err = store.VerifyBrowserSession(ctx, sqlite.VerifyBrowserSessionParams{
		SessionID: session.ID,
		Actor:     "operator",
		Reason:    "operator attended login",
	})
	if err != nil {
		t.Fatalf("VerifyBrowserSession() error = %v", err)
	}

	adapter := &recordingAdapter{response: huginnbrowser.Response{
		Status:               "completed",
		AdapterKind:          "stub_local",
		VisitedURLs:          []string{"https://mail.google.com/mail/u/0/"},
		ExtractedTextSummary: "Authenticated page evidence",
		ActionLog:            []string{"browser_session_profile_attached"},
	}}
	result, err := Service{Store: store, Adapter: adapter}.Run(ctx, ReadOnlyTask{
		GoalID:             goal.ID,
		BrowserSessionID:   session.ID,
		WorkerMode:         "browser",
		Objective:          "Collect authenticated page evidence",
		AllowedDomains:     []string{"google.com"},
		StartURLs:          []string{"https://mail.google.com/mail/u/0/"},
		MaxPages:           1,
		MaxDurationSeconds: 30,
		Actions:            []string{"read"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !adapter.called || adapter.request.BrowserSession == nil {
		t.Fatalf("adapter called=%v browser_session=%#v, want attached session metadata", adapter.called, adapter.request.BrowserSession)
	}
	if adapter.request.BrowserSession.ID != session.ID || adapter.request.BrowserSession.Domain != "google.com" || adapter.request.BrowserSession.ProfilePath != session.ProfilePath {
		t.Fatalf("adapter browser session = %+v, want safe verified session metadata", adapter.request.BrowserSession)
	}
	if result.BrowserSessionID != session.ID || result.BrowserSession == nil || result.BrowserSession.ProfilePath != session.ProfilePath {
		t.Fatalf("result browser session = id:%d metadata:%+v, want safe session reference", result.BrowserSessionID, result.BrowserSession)
	}
	if strings.Contains(strings.ToLower(result.Evidence.PayloadJSON), "cookie") || strings.Contains(strings.ToLower(result.Evidence.PayloadJSON), "token") {
		t.Fatalf("evidence payload exposes forbidden secret marker: %s", result.Evidence.PayloadJSON)
	}

	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var attached bool
	for _, event := range events {
		if event.StreamType == runtimeevents.StreamBrowserSession && event.StreamID == session.ID && event.Type == runtimeevents.EventBrowserProfileAttached {
			attached = true
		}
	}
	if !attached {
		t.Fatalf("events = %+v, want browser.profile_attached", events)
	}
}

func TestReadOnlyServiceRejectsUnsafeBrowserSessionAttachment(t *testing.T) {
	store := openBrowserTestStore(t)
	defer store.Close()

	ctx := context.Background()
	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Reject unsafe browser session"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	session, err := store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
		Name:           "google-main",
		Domain:         "google.com",
		PermissionTier: sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	adapter := &recordingAdapter{}
	_, err = Service{Store: store, Adapter: adapter}.Run(ctx, ReadOnlyTask{
		GoalID:             goal.ID,
		BrowserSessionID:   session.ID,
		Objective:          "Collect authenticated page evidence",
		AllowedDomains:     []string{"google.com"},
		StartURLs:          []string{"https://mail.google.com/mail/u/0/"},
		MaxPages:           1,
		MaxDurationSeconds: 30,
		Actions:            []string{"read"},
	})
	if err == nil || !strings.Contains(err.Error(), "verified") {
		t.Fatalf("Run(unverified session) error = %v, want verified rejection", err)
	}
	if adapter.called {
		t.Fatal("adapter was called for unverified browser session")
	}

	session, _, err = store.VerifyBrowserSession(ctx, sqlite.VerifyBrowserSessionParams{
		SessionID: session.ID,
		Actor:     "operator",
		Reason:    "operator attended login",
	})
	if err != nil {
		t.Fatalf("VerifyBrowserSession() error = %v", err)
	}
	_, err = Service{Store: store, Adapter: adapter}.Run(ctx, ReadOnlyTask{
		GoalID:             goal.ID,
		BrowserSessionID:   session.ID,
		Objective:          "Collect authenticated page evidence",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/private"},
		MaxPages:           1,
		MaxDurationSeconds: 30,
		Actions:            []string{"read"},
	})
	if err == nil || !strings.Contains(err.Error(), "domain") {
		t.Fatalf("Run(domain mismatch) error = %v, want domain rejection", err)
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
