package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	httpapi "odin-os/internal/api/http"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

func TestMobileDeviceSessionAllowsAuthenticatedCaptureAndRevokedSessionDenied(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:      store,
		ReadModels: store.DB(),
		AdminToken: "secret",
	}))
	defer server.Close()

	unauthenticated := postJSON(t, server.URL+"/mobile/intake/raw", "", "", `{"kind":"idea","content":"must fail"}`)
	defer unauthenticated.Body.Close()
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated intake status = %d, want %d", unauthenticated.StatusCode, http.StatusUnauthorized)
	}

	sessionCookie, csrfToken, deviceID := registerMobileDevice(t, server, "secret")

	allowed := postJSON(t, server.URL+"/mobile/intake/raw", sessionCookie.String(), csrfToken, `{"kind":"idea","title":"Session capture","content":"captured from device session"}`)
	defer allowed.Body.Close()
	if allowed.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(allowed.Body)
		t.Fatalf("session intake status = %d body=%s, want %d", allowed.StatusCode, string(body), http.StatusAccepted)
	}

	revoke := postJSON(t, server.URL+"/mobile/devices/"+deviceID+"/revoke", sessionCookie.String(), csrfToken, `{"reason":"lost phone"}`)
	defer revoke.Body.Close()
	if revoke.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(revoke.Body)
		t.Fatalf("device revoke status = %d body=%s, want %d", revoke.StatusCode, string(body), http.StatusOK)
	}

	revoked := postJSON(t, server.URL+"/mobile/intake/raw", sessionCookie.String(), csrfToken, `{"kind":"idea","content":"must fail after revoke"}`)
	defer revoked.Body.Close()
	if revoked.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(revoked.Body)
		t.Fatalf("revoked session intake status = %d body=%s, want %d", revoked.StatusCode, string(body), http.StatusForbidden)
	}

	items, err := store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: "default"})
	if err != nil {
		t.Fatalf("ListIntakeItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("intake items len = %d, want only authenticated capture persisted", len(items))
	}

	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[runtimeevents.Type]int{}
	for _, event := range events {
		counts[event.Type]++
	}
	if counts[runtimeevents.EventMobileLogin] != 1 {
		t.Fatalf("mobile.login events = %d, want 1", counts[runtimeevents.EventMobileLogin])
	}
	if counts[runtimeevents.EventMobileLogout] != 1 {
		t.Fatalf("mobile.logout events = %d, want 1", counts[runtimeevents.EventMobileLogout])
	}
	if counts[runtimeevents.EventMobileIntakeCreated] != 1 {
		t.Fatalf("mobile.intake_created events = %d, want 1", counts[runtimeevents.EventMobileIntakeCreated])
	}
}

func TestMobileSessionRequiresCSRFForCookieMutations(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	defer store.Close()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:      store,
		ReadModels: store.DB(),
		AdminToken: "secret",
	}))
	defer server.Close()

	sessionCookie, _, _ := registerMobileDevice(t, server, "secret")

	response := postJSON(t, server.URL+"/mobile/intake/raw", sessionCookie.String(), "", `{"kind":"idea","content":"csrf missing"}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("cookie mutation without csrf status = %d body=%s, want %d", response.StatusCode, string(body), http.StatusForbidden)
	}

	reviewResponse := postJSON(t, server.URL+"/mobile/review-queue/intake-review:1/decision", sessionCookie.String(), "", `{"action":"clarify","reason":"csrf missing"}`)
	defer reviewResponse.Body.Close()
	if reviewResponse.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(reviewResponse.Body)
		t.Fatalf("review mutation without csrf status = %d body=%s, want %d", reviewResponse.StatusCode, string(body), http.StatusForbidden)
	}
}

func TestMobileIntakeRejectsOversizeUploadForAuthenticatedSession(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	defer store.Close()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:      store,
		ReadModels: store.DB(),
		AdminToken: "secret",
	}))
	defer server.Close()

	sessionCookie, csrfToken, _ := registerMobileDevice(t, server, "secret")
	body, contentType := multipartMobileCapture(t, map[string]string{
		"kind":    "photo",
		"title":   "too large",
		"content": "oversize upload should fail",
	}, "large.jpg", "image/jpeg", bytes.Repeat([]byte("x"), 10<<20+1))

	request, err := http.NewRequest(http.MethodPost, server.URL+"/mobile/intake/raw", body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Cookie", sessionCookie.String())
	request.Header.Set("X-Odin-CSRF", csrfToken)
	request.Header.Set("Content-Type", contentType)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST multipart oversize error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("oversize upload status = %d body=%s, want %d", response.StatusCode, string(body), http.StatusBadRequest)
	}
}

func TestMobileIntakeRateLimitFailsClosed(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	defer store.Close()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:      store,
		ReadModels: store.DB(),
		AdminToken: "secret",
	}))
	defer server.Close()

	sessionCookie, csrfToken, _ := registerMobileDevice(t, server, "secret")
	for index := 0; index < 30; index++ {
		response := postJSON(t, server.URL+"/mobile/intake/raw", sessionCookie.String(), csrfToken, `{"kind":"idea","content":"within rate"}`)
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("rate request %d status = %d, want %d", index+1, response.StatusCode, http.StatusAccepted)
		}
	}
	limited := postJSON(t, server.URL+"/mobile/intake/raw", sessionCookie.String(), csrfToken, `{"kind":"idea","content":"over rate"}`)
	defer limited.Body.Close()
	if limited.StatusCode != http.StatusTooManyRequests {
		body, _ := io.ReadAll(limited.Body)
		t.Fatalf("over-rate status = %d body=%s, want %d", limited.StatusCode, string(body), http.StatusTooManyRequests)
	}
}

func TestMobileSecurityHeadersAndCORSAreConservative(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	defer store.Close()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:      store,
		ReadModels: store.DB(),
		AdminToken: "secret",
	}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/app/", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Origin", "https://evil.example")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET /app/ error = %v", err)
	}
	defer response.Body.Close()
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := response.Header.Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
	if got := response.Header.Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Fatalf("Content-Security-Policy = %q, want self policy", got)
	}
	if got := response.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want no cross-origin allow header", got)
	}
}

func TestMobilePWADoesNotPersistAdminTokenInLocalStorage(t *testing.T) {
	t.Parallel()

	appJS, err := os.ReadFile("app_static/app.js")
	if err != nil {
		t.Fatalf("ReadFile(app.js) error = %v", err)
	}
	source := string(appJS)
	for _, forbidden := range []string{"adminToken", "save-token", "localStorage.getItem(tokenKey)", "localStorage.setItem(tokenKey"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("app.js contains forbidden admin-token storage marker %q", forbidden)
		}
	}
}

func TestMobileRegisterRejectsMissingAdminToken(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	defer store.Close()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:      store,
		ReadModels: store.DB(),
		AdminToken: "secret",
	}))
	defer server.Close()

	response := postJSON(t, server.URL+"/mobile/devices/register", "", "", `{"device_name":"phone"}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated register status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}
}

func registerMobileDevice(t *testing.T, server *httptest.Server, adminToken string) (*http.Cookie, string, string) {
	t.Helper()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/mobile/devices/register", strings.NewReader(`{"device_name":"test phone"}`))
	if err != nil {
		t.Fatalf("NewRequest(register) error = %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+adminToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST register mobile device error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("register mobile device status = %d body=%s, want %d", response.StatusCode, string(body), http.StatusCreated)
	}
	var payload struct {
		DeviceID  string `json:"device_id"`
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(register response) error = %v", err)
	}
	if payload.DeviceID == "" || payload.CSRFToken == "" {
		t.Fatalf("register response = %+v, want device id and csrf token", payload)
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == "odin_mobile_session" {
			if !cookie.HttpOnly {
				t.Fatalf("session cookie HttpOnly = false, want true")
			}
			return cookie, payload.CSRFToken, payload.DeviceID
		}
	}
	t.Fatalf("register response cookies = %+v, want odin_mobile_session", response.Cookies())
	return nil, "", ""
}

func postJSON(t *testing.T, target string, cookieHeader string, csrfToken string, body string) *http.Response {
	t.Helper()

	request, err := http.NewRequest(http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest(%s) error = %v", target, err)
	}
	request.Header.Set("Content-Type", "application/json")
	if cookieHeader != "" {
		request.Header.Set("Cookie", cookieHeader)
	}
	if csrfToken != "" {
		request.Header.Set("X-Odin-CSRF", csrfToken)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST %s error = %v", target, err)
	}
	return response
}
