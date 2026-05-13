package notifications

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	coreprofile "odin-os/internal/core/profile"
	coreworkspaces "odin-os/internal/core/workspaces"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store        *sqlite.Store
	WorkspaceKey string
	Now          func() time.Time
}

type Outcome struct {
	Notification sqlite.Notification `json:"notification"`
	Devices      int                 `json:"devices"`
}

type RoutePendingResult struct {
	Observed     int `json:"observed"`
	Created      int `json:"created"`
	Deduplicated int `json:"deduplicated"`
	Ignored      int `json:"ignored"`
}

type TestEventParams struct {
	NotificationType string
	Priority         string
	Title            string
	Body             string
	Route            string
}

type PreferencesView struct {
	Preferences coreprofile.Preferences     `json:"preferences"`
	Devices     []sqlite.NotificationDevice `json:"devices"`
}

func (service Service) RegisterSubscription(ctx context.Context, params sqlite.UpsertNotificationDeviceParams, optIn bool) (sqlite.NotificationDevice, error) {
	if !optIn {
		return sqlite.NotificationDevice{}, fmt.Errorf("notification subscription requires explicit opt_in")
	}
	if service.Store == nil {
		return sqlite.NotificationDevice{}, fmt.Errorf("notification store is required")
	}
	workspace, err := service.workspace(ctx)
	if err != nil {
		return sqlite.NotificationDevice{}, err
	}
	params.WorkspaceID = workspace.ID
	device, err := service.Store.UpsertNotificationDevice(ctx, params)
	if err != nil {
		return sqlite.NotificationDevice{}, err
	}
	enabled := true
	if _, err := (coreprofile.Service{Store: service.Store, WorkspaceKey: service.workspaceKey()}).Update(ctx, coreprofile.UpdateParams{
		NotificationsEnabled: &enabled,
	}); err != nil {
		return sqlite.NotificationDevice{}, err
	}
	return device, nil
}

func (service Service) Preferences(ctx context.Context) (PreferencesView, error) {
	profile, workspace, err := service.profileAndWorkspace(ctx)
	if err != nil {
		return PreferencesView{}, err
	}
	devices, err := service.Store.ListNotificationDevices(ctx, sqlite.ListNotificationDevicesParams{WorkspaceID: workspace.ID})
	if err != nil {
		return PreferencesView{}, err
	}
	return PreferencesView{Preferences: profile.Preferences, Devices: devices}, nil
}

func (service Service) RouteEvent(ctx context.Context, record runtimeevents.Record) (Outcome, error) {
	candidate, ok := candidateFromEvent(record)
	if !ok {
		return Outcome{}, fmt.Errorf("event %q is not notification-eligible", record.Type)
	}
	candidate.SourceEventID = &record.ID
	return service.create(ctx, candidate)
}

func (service Service) RoutePendingEvents(ctx context.Context) (RoutePendingResult, error) {
	_, workspace, err := service.profileAndWorkspace(ctx)
	if err != nil {
		return RoutePendingResult{}, err
	}
	records, err := service.Store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		return RoutePendingResult{}, err
	}
	existingNotifications, err := service.Store.ListNotifications(ctx, sqlite.ListNotificationsParams{WorkspaceID: workspace.ID, Limit: 200})
	if err != nil {
		return RoutePendingResult{}, err
	}
	seen := make(map[int64]struct{}, len(existingNotifications))
	for _, notification := range existingNotifications {
		if notification.SourceEventID != nil {
			seen[*notification.SourceEventID] = struct{}{}
		}
	}

	var result RoutePendingResult
	for _, record := range records {
		if _, ok := candidateFromEvent(record); !ok {
			result.Ignored++
			continue
		}
		result.Observed++
		if _, ok := seen[record.ID]; ok {
			result.Deduplicated++
			continue
		}
		if _, err := service.RouteEvent(ctx, record); err != nil {
			return RoutePendingResult{}, err
		}
		seen[record.ID] = struct{}{}
		result.Created++
	}
	return result, nil
}

func (service Service) RouteTestEvent(ctx context.Context, params TestEventParams) (Outcome, error) {
	return service.create(ctx, notificationCandidate{
		NotificationType: strings.TrimSpace(params.NotificationType),
		Priority:         strings.TrimSpace(params.Priority),
		Title:            strings.TrimSpace(params.Title),
		Body:             strings.TrimSpace(params.Body),
		Route:            strings.TrimSpace(params.Route),
	})
}

func (service Service) List(ctx context.Context, limit int) ([]sqlite.Notification, error) {
	_, workspace, err := service.profileAndWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return service.Store.ListNotifications(ctx, sqlite.ListNotificationsParams{WorkspaceID: workspace.ID, Limit: limit})
}

func (service Service) create(ctx context.Context, candidate notificationCandidate) (Outcome, error) {
	profile, workspace, err := service.profileAndWorkspace(ctx)
	if err != nil {
		return Outcome{}, err
	}
	candidate = normalizeCandidate(candidate)
	if candidate.NotificationType == "" {
		return Outcome{}, fmt.Errorf("notification type is required")
	}
	if candidate.Title == "" {
		return Outcome{}, fmt.Errorf("notification title is required")
	}
	if candidate.Route == "" {
		candidate.Route = "/pwa"
	}

	devices, err := service.Store.ListNotificationDevices(ctx, sqlite.ListNotificationDevicesParams{WorkspaceID: workspace.ID})
	if err != nil {
		return Outcome{}, err
	}
	status := "in_app_only"
	suppressionReason := ""
	pushPayload := ""
	if !profile.Preferences.NotificationsEnabled {
		status = "in_app_only"
		suppressionReason = "not_opted_in"
	} else if quietHoursApplies(profile.Preferences.QuietHours, service.now(), candidate.Priority) {
		if strings.EqualFold(profile.Preferences.NotificationBatching, "quiet_hours") {
			status = "batched_quiet_hours"
		} else {
			status = "suppressed_quiet_hours"
		}
		suppressionReason = "quiet_hours"
	} else if len(devices) > 0 {
		status = "push_ready"
		payload, err := webPushPayload(candidate)
		if err != nil {
			return Outcome{}, err
		}
		pushPayload = payload
	}

	notification, err := service.Store.CreateNotification(ctx, sqlite.CreateNotificationParams{
		WorkspaceID:       workspace.ID,
		SourceEventID:     candidate.SourceEventID,
		NotificationType:  candidate.NotificationType,
		Priority:          candidate.Priority,
		Title:             sanitizeText(candidate.Title),
		Body:              sanitizeText(candidate.Body),
		Route:             candidate.Route,
		Status:            status,
		PushPayloadJSON:   pushPayload,
		SuppressionReason: suppressionReason,
	})
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Notification: notification, Devices: len(devices)}, nil
}

func (service Service) profileAndWorkspace(ctx context.Context) (coreprofile.OperatingProfile, coreworkspaces.Workspace, error) {
	if service.Store == nil {
		return coreprofile.OperatingProfile{}, coreworkspaces.Workspace{}, fmt.Errorf("notification store is required")
	}
	workspace, err := service.workspace(ctx)
	if err != nil {
		return coreprofile.OperatingProfile{}, coreworkspaces.Workspace{}, err
	}
	profile, err := (coreprofile.Service{Store: service.Store, WorkspaceKey: workspace.Key}).Get(ctx)
	if err != nil {
		return coreprofile.OperatingProfile{}, coreworkspaces.Workspace{}, err
	}
	return profile, workspace, nil
}

func (service Service) workspace(ctx context.Context) (coreworkspaces.Workspace, error) {
	workspaceService := coreworkspaces.Service{Store: service.Store}
	workspace, err := workspaceService.GetWorkspaceByKey(ctx, service.workspaceKey())
	if err == nil {
		return workspace, nil
	}
	if err == sql.ErrNoRows && service.workspaceKey() == coreprofile.DefaultWorkspaceKey {
		return workspaceService.BootstrapDefaultWorkspace(ctx)
	}
	return coreworkspaces.Workspace{}, err
}

func (service Service) workspaceKey() string {
	if strings.TrimSpace(service.WorkspaceKey) != "" {
		return strings.TrimSpace(service.WorkspaceKey)
	}
	return coreprofile.DefaultWorkspaceKey
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now().UTC()
	}
	return time.Now().UTC()
}

type notificationCandidate struct {
	SourceEventID    *int64
	NotificationType string
	Priority         string
	Title            string
	Body             string
	Route            string
}

func candidateFromEvent(record runtimeevents.Record) (notificationCandidate, bool) {
	switch record.Type {
	case runtimeevents.EventApprovalRequested:
		return notificationCandidate{
			NotificationType: "approval_required",
			Priority:         "high",
			Title:            "Approval required",
			Body:             "Odin needs an operator decision.",
			Route:            "/approvals/" + strconv.FormatInt(record.StreamID, 10),
		}, true
	case runtimeevents.EventRunFinished:
		var payload runtimeevents.RunFinishedPayload
		if json.Unmarshal(record.Payload, &payload) != nil || !strings.EqualFold(payload.Status, "failed") {
			return notificationCandidate{}, false
		}
		return notificationCandidate{
			NotificationType: "work_failed",
			Priority:         "high",
			Title:            "Work failed",
			Body:             "A run finished with failed status.",
			Route:            "/runs/" + strconv.FormatInt(derefInt64(record.RunID), 10),
		}, true
	case runtimeevents.EventIntakeClarificationNeeded, runtimeevents.EventIntakeReviewClarificationRequested:
		return notificationCandidate{
			NotificationType: "clarification_needed",
			Priority:         "medium",
			Title:            "Clarification needed",
			Body:             "Odin needs more information before continuing.",
			Route:            "/review",
		}, true
	case runtimeevents.EventAutomationTriggerMaterialized:
		return notificationCandidate{
			NotificationType: "scheduled_review_ready",
			Priority:         "medium",
			Title:            "Scheduled review ready",
			Body:             "A scheduled Odin review item is ready.",
			Route:            "/review",
		}, true
	case runtimeevents.EventBrowserSessionLoginRequested:
		route := "/browser/session/handoff"
		var payload runtimeevents.BrowserSessionLoginRequestedPayload
		if json.Unmarshal(record.Payload, &payload) == nil && strings.TrimSpace(payload.HandoffID) != "" {
			route += "?handoff_id=" + payload.HandoffID
		}
		return notificationCandidate{
			NotificationType: "browser_login_required",
			Priority:         "high",
			Title:            "Browser login required",
			Body:             "An attended browser session needs manual login.",
			Route:            route,
		}, true
	case runtimeevents.EventIncidentOpened:
		return notificationCandidate{
			NotificationType: "critical_health",
			Priority:         "critical",
			Title:            "Critical Odin health issue",
			Body:             "Odin opened a readiness or health incident.",
			Route:            "/doctor",
		}, true
	case runtimeevents.EventFollowUpMaterialized:
		return notificationCandidate{
			NotificationType: "deadline_followup",
			Priority:         "medium",
			Title:            "Follow-up ready",
			Body:             "A deadline or waiting-for follow-up is ready.",
			Route:            "/agenda",
		}, true
	default:
		return notificationCandidate{}, false
	}
}

func normalizeCandidate(candidate notificationCandidate) notificationCandidate {
	candidate.NotificationType = strings.ToLower(strings.TrimSpace(candidate.NotificationType))
	candidate.Priority = strings.ToLower(strings.TrimSpace(candidate.Priority))
	if candidate.Priority == "" {
		candidate.Priority = "medium"
	}
	candidate.Title = strings.TrimSpace(candidate.Title)
	candidate.Body = strings.TrimSpace(candidate.Body)
	if candidate.Body == "" {
		candidate.Body = "Open Odin for details."
	}
	candidate.Route = normalizeRoute(candidate.Route)
	return candidate
}

func normalizeRoute(route string) string {
	route = strings.TrimSpace(route)
	if route == "" {
		return ""
	}
	if strings.HasPrefix(route, "/") {
		return route
	}
	return "/" + route
}

func webPushPayload(candidate notificationCandidate) (string, error) {
	payload := map[string]string{
		"title": candidate.Title,
		"body":  sanitizeText(candidate.Body),
		"url":   candidate.Route,
		"tag":   candidate.NotificationType,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func quietHoursApplies(rule string, now time.Time, priority string) bool {
	if priority == "high" || priority == "critical" {
		return false
	}
	start, end, ok := parseQuietHours(rule)
	if !ok {
		return false
	}
	minute := now.Hour()*60 + now.Minute()
	if start <= end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}

func parseQuietHours(rule string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(rule), "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, ok := parseClockMinute(parts[0])
	if !ok {
		return 0, 0, false
	}
	end, ok := parseClockMinute(parts[1])
	if !ok {
		return 0, 0, false
	}
	return start, end, true
}

func parseClockMinute(value string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, false
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, false
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)gh[pousr]_[A-Za-z0-9_]+`),
	regexp.MustCompile(`(?i)(password|token|secret|api[_-]?key)=\S+`),
}

func sanitizeText(value string) string {
	value = strings.TrimSpace(value)
	for _, pattern := range secretPatterns {
		value = pattern.ReplaceAllString(value, "[redacted]")
	}
	return value
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
