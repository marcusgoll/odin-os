package notifications

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	coreprofile "odin-os/internal/core/profile"
	coreworkspaces "odin-os/internal/core/workspaces"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

func TestRouteActionRequiredEventCreatesInAppAndPushPayloadWhenOptedIn(t *testing.T) {
	ctx := context.Background()
	store := openNotificationStore(t)
	workspace := bootstrapNotificationWorkspace(t, ctx, store)
	enableNotifications(t, ctx, store)
	registerNotificationDevice(t, ctx, store, workspace.ID)

	record := seedApprovalRequestedEvent(t, ctx, store)

	outcome, err := Service{
		Store:        store,
		WorkspaceKey: coreprofile.DefaultWorkspaceKey,
		Now:          func() time.Time { return record.OccurredAt },
	}.RouteEvent(ctx, record)
	if err != nil {
		t.Fatalf("RouteEvent() error = %v", err)
	}
	if outcome.Notification.Status != "push_ready" {
		t.Fatalf("notification status = %q, want push_ready", outcome.Notification.Status)
	}
	if outcome.Notification.NotificationType != "approval_required" || outcome.Notification.Priority != "high" {
		t.Fatalf("notification = %+v, want approval_required high", outcome.Notification)
	}
	wantRoute := "/approvals/" + strconv.FormatInt(record.StreamID, 10)
	if outcome.Notification.Route != wantRoute {
		t.Fatalf("route = %q, want %s", outcome.Notification.Route, wantRoute)
	}
	if !strings.Contains(outcome.Notification.PushPayloadJSON, `"url":"`+wantRoute+`"`) {
		t.Fatalf("push payload = %s, want deep link", outcome.Notification.PushPayloadJSON)
	}
}

func TestQuietHoursBatchLowAndMediumPriorityButAllowCriticalWhenOptedIn(t *testing.T) {
	ctx := context.Background()
	store := openNotificationStore(t)
	workspace := bootstrapNotificationWorkspace(t, ctx, store)
	enableNotifications(t, ctx, store)
	registerNotificationDevice(t, ctx, store, workspace.ID)

	quietHours := "22:00-07:00"
	batching := "quiet_hours"
	if _, err := (coreprofile.Service{Store: store, WorkspaceKey: coreprofile.DefaultWorkspaceKey}).Update(ctx, coreprofile.UpdateParams{
		QuietHours:           &quietHours,
		NotificationBatching: &batching,
	}); err != nil {
		t.Fatalf("profile.Update(quiet) error = %v", err)
	}

	service := Service{
		Store:        store,
		WorkspaceKey: coreprofile.DefaultWorkspaceKey,
		Now:          func() time.Time { return time.Date(2026, 5, 13, 23, 30, 0, 0, time.UTC) },
	}

	medium, err := service.RouteTestEvent(ctx, TestEventParams{
		NotificationType: "deadline_followup",
		Priority:         "medium",
		Title:            "Waiting-for follow-up",
		Route:            "/agenda",
	})
	if err != nil {
		t.Fatalf("RouteTestEvent(medium) error = %v", err)
	}
	if medium.Notification.Status != "batched_quiet_hours" {
		t.Fatalf("medium status = %q, want batched_quiet_hours", medium.Notification.Status)
	}

	critical, err := service.RouteTestEvent(ctx, TestEventParams{
		NotificationType: "critical_health",
		Priority:         "critical",
		Title:            "Readiness failed",
		Route:            "/doctor",
	})
	if err != nil {
		t.Fatalf("RouteTestEvent(critical) error = %v", err)
	}
	if critical.Notification.Status != "push_ready" {
		t.Fatalf("critical status = %q, want push_ready despite quiet hours", critical.Notification.Status)
	}
}

func TestPushUnavailableFallsBackToInAppNotificationCenter(t *testing.T) {
	ctx := context.Background()
	store := openNotificationStore(t)
	bootstrapNotificationWorkspace(t, ctx, store)
	enableNotifications(t, ctx, store)

	outcome, err := Service{
		Store:        store,
		WorkspaceKey: coreprofile.DefaultWorkspaceKey,
		Now:          func() time.Time { return time.Date(2026, 5, 13, 15, 0, 0, 0, time.UTC) },
	}.RouteTestEvent(ctx, TestEventParams{
		NotificationType: "work_failed",
		Priority:         "high",
		Title:            "Work failed",
		Route:            "/runs/44",
	})
	if err != nil {
		t.Fatalf("RouteTestEvent() error = %v", err)
	}
	if outcome.Notification.Status != "in_app_only" {
		t.Fatalf("status = %q, want in_app_only", outcome.Notification.Status)
	}
	if outcome.Notification.PushPayloadJSON != "" {
		t.Fatalf("PushPayloadJSON = %q, want empty without active push device", outcome.Notification.PushPayloadJSON)
	}
}

func TestRoutePendingEventsIsIdempotentForActionRequiredEvents(t *testing.T) {
	ctx := context.Background()
	store := openNotificationStore(t)
	workspace := bootstrapNotificationWorkspace(t, ctx, store)
	enableNotifications(t, ctx, store)
	registerNotificationDevice(t, ctx, store, workspace.ID)
	seedApprovalRequestedEvent(t, ctx, store)

	service := Service{
		Store:        store,
		WorkspaceKey: coreprofile.DefaultWorkspaceKey,
		Now:          func() time.Time { return time.Date(2026, 5, 13, 15, 0, 0, 0, time.UTC) },
	}
	first, err := service.RoutePendingEvents(ctx)
	if err != nil {
		t.Fatalf("RoutePendingEvents(first) error = %v", err)
	}
	second, err := service.RoutePendingEvents(ctx)
	if err != nil {
		t.Fatalf("RoutePendingEvents(second) error = %v", err)
	}
	if first.Created != 1 || second.Created != 0 || second.Deduplicated != 1 {
		t.Fatalf("first=%+v second=%+v, want one created then deduplicated", first, second)
	}
	records, err := store.ListNotifications(ctx, sqlite.ListNotificationsParams{WorkspaceID: workspace.ID})
	if err != nil {
		t.Fatalf("ListNotifications() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("notifications len = %d, want no spam duplicate", len(records))
	}
}

func TestSupportedActionRequiredEventsHaveRoutes(t *testing.T) {
	runID := int64(41)
	for _, tc := range []struct {
		name  string
		event runtimeevents.Record
		typ   string
		route string
	}{
		{
			name:  "approval",
			event: runtimeevents.Record{ID: 1, StreamID: 12, Type: runtimeevents.EventApprovalRequested, Payload: []byte(`{"status":"pending"}`)},
			typ:   "approval_required",
			route: "/approvals/12",
		},
		{
			name:  "failed work",
			event: runtimeevents.Record{ID: 2, RunID: &runID, Type: runtimeevents.EventRunFinished, Payload: []byte(`{"status":"failed"}`)},
			typ:   "work_failed",
			route: "/runs/41",
		},
		{
			name:  "clarification",
			event: runtimeevents.Record{ID: 3, Type: runtimeevents.EventIntakeClarificationNeeded, Payload: []byte(`{}`)},
			typ:   "clarification_needed",
			route: "/review",
		},
		{
			name:  "scheduled review",
			event: runtimeevents.Record{ID: 4, Type: runtimeevents.EventAutomationTriggerMaterialized, Payload: []byte(`{}`)},
			typ:   "scheduled_review_ready",
			route: "/review",
		},
		{
			name:  "browser login",
			event: runtimeevents.Record{ID: 5, Type: runtimeevents.EventBrowserSessionLoginRequested, Payload: []byte(`{"handoff_id":"abc123"}`)},
			typ:   "browser_login_required",
			route: "/browser/session/handoff?handoff_id=abc123",
		},
		{
			name:  "critical health",
			event: runtimeevents.Record{ID: 6, Type: runtimeevents.EventIncidentOpened, Payload: []byte(`{"severity":"critical"}`)},
			typ:   "critical_health",
			route: "/doctor",
		},
		{
			name:  "deadline follow-up",
			event: runtimeevents.Record{ID: 7, Type: runtimeevents.EventFollowUpMaterialized, Payload: []byte(`{}`)},
			typ:   "deadline_followup",
			route: "/agenda",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate, ok := candidateFromEvent(tc.event)
			if !ok {
				t.Fatalf("candidateFromEvent() ok=false")
			}
			if candidate.NotificationType != tc.typ || candidate.Route != tc.route {
				t.Fatalf("candidate = %+v, want type=%s route=%s", candidate, tc.typ, tc.route)
			}
		})
	}
}

func openNotificationStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func bootstrapNotificationWorkspace(t *testing.T, ctx context.Context, store *sqlite.Store) coreworkspaces.Workspace {
	t.Helper()

	workspace, err := (coreworkspaces.Service{Store: store}).BootstrapDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}
	return workspace
}

func enableNotifications(t *testing.T, ctx context.Context, store *sqlite.Store) {
	t.Helper()

	enabled := true
	if _, err := (coreprofile.Service{Store: store, WorkspaceKey: coreprofile.DefaultWorkspaceKey}).Update(ctx, coreprofile.UpdateParams{
		NotificationsEnabled: &enabled,
	}); err != nil {
		t.Fatalf("profile.Update(enable notifications) error = %v", err)
	}
}

func registerNotificationDevice(t *testing.T, ctx context.Context, store *sqlite.Store, workspaceID int64) sqlite.NotificationDevice {
	t.Helper()

	device, err := store.UpsertNotificationDevice(ctx, sqlite.UpsertNotificationDeviceParams{
		WorkspaceID: workspaceID,
		DeviceKey:   "test-device",
		Label:       "Test device",
		Endpoint:    "https://push.example.test/subscription",
		P256DH:      "p256dh",
		Auth:        "auth",
		UserAgent:   "test",
	})
	if err != nil {
		t.Fatalf("UpsertNotificationDevice() error = %v", err)
	}
	return device
}

func seedApprovalRequestedEvent(t *testing.T, ctx context.Context, store *sqlite.Store) runtimeevents.Record {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "project",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "approval-needed",
		Title:       "Needs approval",
		Status:      "blocked",
		Scope:       "project",
		RequestedBy: "operator",
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
		RequestedBy: "operator",
	}); err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{TaskID: &task.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	for _, event := range events {
		if event.Type == runtimeevents.EventApprovalRequested {
			return event
		}
	}
	t.Fatal("approval.requested event not found")
	return runtimeevents.Record{}
}
