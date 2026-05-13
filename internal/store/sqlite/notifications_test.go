package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNotificationSubscriptionStorageRedactsDeviceSecrets(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "notifications.db")
	defer store.Close()

	workspace := seedNotificationWorkspace(t, ctx, store)

	subscription, err := store.UpsertNotificationDevice(ctx, UpsertNotificationDeviceParams{
		WorkspaceID: workspace.ID,
		DeviceKey:   "iphone-home-screen",
		Label:       "iPhone Home Screen",
		Endpoint:    "https://push.example.test/secret-endpoint-token",
		P256DH:      "public-key-material",
		Auth:        "auth-secret-material",
		UserAgent:   "Mobile Safari",
	})
	if err != nil {
		t.Fatalf("UpsertNotificationDevice() error = %v", err)
	}
	if subscription.EndpointHash == "" {
		t.Fatalf("EndpointHash empty, want stable dedupe hash")
	}

	devices, err := store.ListNotificationDevices(ctx, ListNotificationDevicesParams{WorkspaceID: workspace.ID})
	if err != nil {
		t.Fatalf("ListNotificationDevices() error = %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices len = %d, want 1", len(devices))
	}
	projected, err := json.Marshal(devices[0])
	if err != nil {
		t.Fatalf("Marshal(device) error = %v", err)
	}
	for _, forbidden := range []string{"secret-endpoint-token", "public-key-material", "auth-secret-material"} {
		if strings.Contains(string(projected), forbidden) {
			t.Fatalf("device projection leaked %q: %s", forbidden, string(projected))
		}
	}

	if _, err := store.RevokeNotificationDevice(ctx, RevokeNotificationDeviceParams{
		WorkspaceID: workspace.ID,
		DeviceID:    devices[0].ID,
		Reason:      "operator unsubscribed",
	}); err != nil {
		t.Fatalf("RevokeNotificationDevice() error = %v", err)
	}
	after, err := store.ListNotificationDevices(ctx, ListNotificationDevicesParams{WorkspaceID: workspace.ID, IncludeRevoked: true})
	if err != nil {
		t.Fatalf("ListNotificationDevices(include revoked) error = %v", err)
	}
	if after[0].Status != "revoked" {
		t.Fatalf("device status = %q, want revoked", after[0].Status)
	}
}

func TestNotificationRecordsAreAuditableAndDeduplicatedBySourceEvent(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "notification-records.db")
	defer store.Close()

	workspace := seedNotificationWorkspace(t, ctx, store)
	sourceEventID := seedNotificationSourceEvent(t, ctx, store)

	first, err := store.CreateNotification(ctx, CreateNotificationParams{
		WorkspaceID:       workspace.ID,
		SourceEventID:     &sourceEventID,
		NotificationType:  "approval_required",
		Priority:          "high",
		Title:             "Approval required",
		Body:              "Odin needs an operator decision.",
		Route:             "/approvals/12",
		Status:            "push_ready",
		PushPayloadJSON:   `{"title":"Approval required","url":"/approvals/12"}`,
		SuppressionReason: "",
	})
	if err != nil {
		t.Fatalf("CreateNotification(first) error = %v", err)
	}
	second, err := store.CreateNotification(ctx, CreateNotificationParams{
		WorkspaceID:      workspace.ID,
		SourceEventID:    &sourceEventID,
		NotificationType: "approval_required",
		Priority:         "high",
		Title:            "Approval required",
		Body:             "Changed body should not spam.",
		Route:            "/approvals/12",
		Status:           "push_ready",
	})
	if err != nil {
		t.Fatalf("CreateNotification(second) error = %v", err)
	}
	if second.ID != first.ID || second.Body != first.Body {
		t.Fatalf("dedupe notification = %+v, want original %+v", second, first)
	}

	records, err := store.ListNotifications(ctx, ListNotificationsParams{WorkspaceID: workspace.ID})
	if err != nil {
		t.Fatalf("ListNotifications() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("notifications len = %d, want 1", len(records))
	}
	if records[0].SourceEventID == nil || *records[0].SourceEventID != sourceEventID {
		t.Fatalf("SourceEventID = %v, want %d", records[0].SourceEventID, sourceEventID)
	}
}

func seedNotificationSourceEvent(t *testing.T, ctx context.Context, store *Store) int64 {
	t.Helper()

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "notifications-source",
		Name:          "Notifications Source",
		Scope:         "project",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(source) error = %v", err)
	}
	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "notify-source",
		Title:       "Notification source",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(source) error = %v", err)
	}
	events, err := store.ListEvents(ctx, ListEventsParams{TaskID: &task.ID})
	if err != nil {
		t.Fatalf("ListEvents(source) error = %v", err)
	}
	if len(events) == 0 {
		t.Fatal("source events len = 0, want task.created event")
	}
	return events[0].ID
}

func seedNotificationWorkspace(t *testing.T, ctx context.Context, store *Store) Workspace {
	t.Helper()

	if workspace, err := store.GetWorkspaceByKey(ctx, "default"); err == nil {
		return workspace
	}
	workspace, err := store.CreateWorkspace(ctx, CreateWorkspaceParams{
		Key:                 "default",
		Name:                "Default Workspace",
		OwnerRef:            "operator",
		DefaultCompanionKey: "primary",
		Status:              "active",
		PolicyJSON:          `{}`,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	return workspace
}
