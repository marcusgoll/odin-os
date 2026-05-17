package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	httpapi "odin-os/internal/api/http"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

func TestEmailActionPreviewGeneratesSignedApprovalLinksAndAppliesOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()
	approval := seedMobileApproval(t, ctx, store, "email-approval", "external_mutation")

	now := time.Date(2026, 5, 17, 18, 0, 0, 0, time.UTC)
	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:                store,
		ReadModels:           store.DB(),
		AdminToken:           "admin-token",
		EmailActionSecret:    "email-action-secret",
		EmailActionBaseURL:   "http://127.0.0.1",
		EmailActionRecipient: "marcusgoll@gmail.com",
		Now:                  func() time.Time { return now },
	}))
	defer server.Close()

	preview := getEmailActionPreview(t, server.URL, "admin-token")
	if preview.To != "marcusgoll@gmail.com" || !strings.Contains(preview.Text, "odin review show approval:") || !strings.Contains(preview.HTML, "Approve") {
		t.Fatalf("preview = %+v, want Marcus recipient, fallback command, and approve button", preview)
	}
	approveURL := findEmailActionLink(t, preview, fmt.Sprintf("approval:%d", approval.ID), "approve")
	approveURL = strings.Replace(approveURL, "http://127.0.0.1", server.URL, 1)

	response, body := getURL(t, approveURL)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Odin action applied") {
		t.Fatalf("GET approve link status=%d body=%s, want action applied", response.StatusCode, body)
	}
	updated, err := store.GetApproval(ctx, approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if updated.Status != "approved" || updated.DecisionBy != "email:marcusgoll@gmail.com" {
		t.Fatalf("approval after email action = %+v, want approved by email recipient", updated)
	}
	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if !hasRuntimeEventType(events, runtimeevents.EventApprovalResolved) {
		t.Fatalf("events = %+v, want approval.resolved audit event", events)
	}

	replayResponse, replayBody := getURL(t, approveURL)
	if replayResponse.StatusCode != http.StatusConflict || !strings.Contains(replayBody, "want pending") {
		t.Fatalf("replay status=%d body=%s, want fail-closed conflict", replayResponse.StatusCode, replayBody)
	}
}

func TestEmailActionTokensFailClosedWhenInvalidOrExpired(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()
	approval := seedMobileApproval(t, ctx, store, "email-expired", "external_mutation")

	now := time.Date(2026, 5, 17, 18, 0, 0, 0, time.UTC)
	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:                store,
		ReadModels:           store.DB(),
		AdminToken:           "admin-token",
		EmailActionSecret:    "email-action-secret",
		EmailActionBaseURL:   "http://127.0.0.1",
		EmailActionRecipient: "marcusgoll@gmail.com",
		Now:                  func() time.Time { return now },
	}))
	defer server.Close()

	invalidResponse, invalidBody := getURL(t, server.URL+"/email-actions/not-a-token")
	if invalidResponse.StatusCode != http.StatusForbidden || !strings.Contains(invalidBody, "malformed") {
		t.Fatalf("invalid token status=%d body=%s, want malformed forbidden", invalidResponse.StatusCode, invalidBody)
	}

	preview := getEmailActionPreview(t, server.URL, "admin-token")
	approveURL := findEmailActionLink(t, preview, fmt.Sprintf("approval:%d", approval.ID), "approve")
	approveURL = strings.Replace(approveURL, "http://127.0.0.1", server.URL, 1)
	now = now.Add(25 * time.Hour)

	expiredResponse, expiredBody := getURL(t, approveURL)
	if expiredResponse.StatusCode != http.StatusForbidden || !strings.Contains(expiredBody, "expired") {
		t.Fatalf("expired token status=%d body=%s, want expired forbidden", expiredResponse.StatusCode, expiredBody)
	}
}

func TestEmailActionArchiveUsesIntakeReviewAuditPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()
	item, err := store.CreateIntakeItem(ctx, sqlite.CreateIntakeItemParams{
		WorkspaceID:         "default",
		SourceFamily:        "email-test",
		ExternalObjectID:    "email-review-archive",
		EventKind:           "operator_review",
		Subject:             "Email review archive",
		DedupeKey:           "email-review-archive",
		DedupeRecipeVersion: "test-v1",
		SourceFactsJSON:     `{}`,
		Status:              "review_required",
		Scope:               "workspace",
		ScopeKey:            "default",
	})
	if err != nil {
		t.Fatalf("CreateIntakeItem() error = %v", err)
	}

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:                store,
		ReadModels:           store.DB(),
		AdminToken:           "admin-token",
		EmailActionSecret:    "email-action-secret",
		EmailActionBaseURL:   "http://127.0.0.1",
		EmailActionRecipient: "marcusgoll@gmail.com",
		Now:                  func() time.Time { return time.Date(2026, 5, 17, 18, 0, 0, 0, time.UTC) },
	}))
	defer server.Close()

	preview := getEmailActionPreview(t, server.URL, "admin-token")
	archiveURL := findEmailActionLink(t, preview, fmt.Sprintf("intake-review:%d", item.ID), "archive")
	archiveURL = strings.Replace(archiveURL, "http://127.0.0.1", server.URL, 1)

	response, body := getURL(t, archiveURL)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Odin action applied") {
		t.Fatalf("GET archive link status=%d body=%s, want action applied", response.StatusCode, body)
	}
	updated, err := store.GetIntakeItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetIntakeItem() error = %v", err)
	}
	if updated.Status != "archived" {
		t.Fatalf("intake status = %q, want archived", updated.Status)
	}
	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if !hasRuntimeEventType(events, runtimeevents.EventIntakeReviewArchived) {
		t.Fatalf("events = %+v, want intake.review_archived audit event", events)
	}
}

func TestEmailActionSendUsesConfiguredSendmail(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()
	approval := seedMobileApproval(t, ctx, store, "email-send", "external_mutation")
	capturePath := filepath.Join(t.TempDir(), "message.eml")
	sendmailPath := filepath.Join(t.TempDir(), "sendmail")
	script := "#!/bin/sh\ncat > " + shellQuote(capturePath) + "\n"
	if err := os.WriteFile(sendmailPath, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile(sendmail) error = %v", err)
	}

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:                store,
		ReadModels:           store.DB(),
		AdminToken:           "admin-token",
		EmailActionSecret:    "email-action-secret",
		EmailActionBaseURL:   "http://odin.example.test",
		EmailActionRecipient: "marcusgoll@gmail.com",
		EmailActionFrom:      "odin-os@example.test",
		EmailActionSendmail:  sendmailPath,
		Now:                  func() time.Time { return time.Date(2026, 5, 17, 18, 0, 0, 0, time.UTC) },
	}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/email-actions/send", nil)
	if err != nil {
		t.Fatalf("NewRequest send error = %v", err)
	}
	request.Header.Set("X-Odin-Admin-Token", "admin-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST send error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("send status=%d body=%s, want accepted", response.StatusCode, string(raw))
	}
	message, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("ReadFile(capture) error = %v", err)
	}
	body := string(message)
	for _, want := range []string{
		"To: marcusgoll@gmail.com",
		"From: odin-os@example.test",
		"Odin needs 1 approval/review decision",
		fmt.Sprintf("approval:%d", approval.ID),
		"Approve",
		"http://odin.example.test/email-actions/",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("email body missing %q:\n%s", want, body)
		}
	}
}

type emailActionPreviewTestView struct {
	To    string `json:"to"`
	Text  string `json:"text"`
	HTML  string `json:"html"`
	Items []struct {
		QueueID string `json:"queue_id"`
		Links   []struct {
			Action string `json:"action"`
			URL    string `json:"url"`
		} `json:"links"`
	} `json:"items"`
}

func getEmailActionPreview(t *testing.T, serverURL string, token string) emailActionPreviewTestView {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, serverURL+"/email-actions/preview", nil)
	if err != nil {
		t.Fatalf("NewRequest preview error = %v", err)
	}
	request.Header.Set("X-Odin-Admin-Token", token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET preview error = %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll preview error = %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("preview status=%d body=%s, want OK", response.StatusCode, string(body))
	}
	var preview emailActionPreviewTestView
	if err := json.Unmarshal(body, &preview); err != nil {
		t.Fatalf("Unmarshal preview error = %v body=%s", err, string(body))
	}
	return preview
}

func findEmailActionLink(t *testing.T, preview emailActionPreviewTestView, queueID string, action string) string {
	t.Helper()
	for _, item := range preview.Items {
		if item.QueueID != queueID {
			continue
		}
		for _, link := range item.Links {
			if link.Action == action {
				return link.URL
			}
		}
	}
	t.Fatalf("preview missing link queue=%s action=%s: %+v", queueID, action, preview)
	return ""
}

func getURL(t *testing.T, target string) (*http.Response, string) {
	t.Helper()
	response, err := http.Get(target)
	if err != nil {
		t.Fatalf("GET %s error = %v", target, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll(%s) error = %v", target, err)
	}
	return response, string(body)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
