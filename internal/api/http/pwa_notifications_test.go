package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpapi "odin-os/internal/api/http"
	coreprofile "odin-os/internal/core/profile"
	healthsvc "odin-os/internal/runtime/health"
	metricsvc "odin-os/internal/telemetry/metrics"
)

func TestPWAExposesServiceWorkerManifestAndUserActionSubscriptionEndpoint(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	defer store.Close()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{DB: store.DB()},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		Store:           store,
		ReadModels:      store.DB(),
		RegistryHealthy: true,
	}))
	defer server.Close()

	pwaBody := getBody(t, server.URL+"/pwa", http.StatusOK)
	for _, want := range []string{"navigator.serviceWorker.register", "Notification.requestPermission", "/notifications/subscriptions"} {
		if !strings.Contains(pwaBody, want) {
			t.Fatalf("/pwa body missing %q:\n%s", want, pwaBody)
		}
	}
	if !strings.Contains(pwaBody, "Push subscription failed. Odin will use the in-app notification center.") {
		t.Fatalf("/pwa body missing push subscription failure fallback:\n%s", pwaBody)
	}
	if got := getBody(t, server.URL+"/manifest.webmanifest", http.StatusOK); !strings.Contains(got, `"display":"standalone"`) {
		t.Fatalf("/manifest.webmanifest = %s, want standalone manifest", got)
	}
	if got := getBody(t, server.URL+"/service-worker.js", http.StatusOK); !strings.Contains(got, "self.addEventListener('push'") || !strings.Contains(got, "clients.openWindow") {
		t.Fatalf("/service-worker.js = %s, want push click handler", got)
	}

	payload := []byte(`{
		"device_key":"iphone-home-screen",
		"label":"iPhone Home Screen",
		"subscription":{
			"endpoint":"https://push.example.test/secret-subscription",
			"keys":{"p256dh":"public-key","auth":"auth-secret"}
		},
		"opt_in":true
	}`)
	response, err := http.Post(server.URL+"/notifications/subscriptions", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /notifications/subscriptions error = %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll(subscription) error = %v", err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("subscription status = %d body=%s, want %d", response.StatusCode, string(body), http.StatusCreated)
	}
	if strings.Contains(string(body), "secret-subscription") || strings.Contains(string(body), "auth-secret") || strings.Contains(string(body), "public-key") {
		t.Fatalf("subscription response leaked push secrets: %s", string(body))
	}

	devices := getBody(t, server.URL+"/notifications/devices", http.StatusOK)
	if !strings.Contains(devices, "iphone-home-screen") || strings.Contains(devices, "auth-secret") {
		t.Fatalf("/notifications/devices = %s, want redacted device projection", devices)
	}
}

func TestNotificationTestEventCreatesInAppNotificationAndPushPayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	enabled := true
	if _, err := (coreprofile.Service{Store: store, WorkspaceKey: coreprofile.DefaultWorkspaceKey}).Update(ctx, coreprofile.UpdateParams{
		NotificationsEnabled: &enabled,
	}); err != nil {
		t.Fatalf("profile.Update(enable) error = %v", err)
	}

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{DB: store.DB()},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		Store:           store,
		ReadModels:      store.DB(),
		RegistryHealthy: true,
	}))
	defer server.Close()

	subscriptionPayload := []byte(`{"device_key":"desktop","label":"Desktop","subscription":{"endpoint":"https://push.example.test/desktop","keys":{"p256dh":"public","auth":"auth"}},"opt_in":true}`)
	response, err := http.Post(server.URL+"/notifications/subscriptions", "application/json", bytes.NewReader(subscriptionPayload))
	if err != nil {
		t.Fatalf("POST subscription error = %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("subscription status = %d, want created", response.StatusCode)
	}

	eventPayload := []byte(`{"type":"work_failed","priority":"high","title":"Work failed","route":"/runs/44","body":"Inspect the failed run."}`)
	response, err = http.Post(server.URL+"/notifications/test-event", "application/json", bytes.NewReader(eventPayload))
	if err != nil {
		t.Fatalf("POST /notifications/test-event error = %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll(test-event) error = %v", err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("test-event status = %d body=%s, want created", response.StatusCode, string(body))
	}
	if !strings.Contains(string(body), `"status":"push_ready"`) || !strings.Contains(string(body), `"/runs/44"`) {
		t.Fatalf("test-event body = %s, want push_ready deep-link payload", string(body))
	}

	center := getBody(t, server.URL+"/notifications", http.StatusOK)
	if !strings.Contains(center, "Work failed") || !strings.Contains(center, "/runs/44") {
		t.Fatalf("/notifications = %s, want in-app notification center row", center)
	}

	var preferences struct {
		Preferences struct {
			NotificationsEnabled bool   `json:"notifications_enabled"`
			QuietHours           string `json:"quiet_hours"`
		} `json:"preferences"`
		Devices []struct {
			DeviceKey string `json:"device_key"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(getBody(t, server.URL+"/notifications/preferences", http.StatusOK)), &preferences); err != nil {
		t.Fatalf("preferences JSON error = %v", err)
	}
	if !preferences.Preferences.NotificationsEnabled || len(preferences.Devices) != 1 {
		t.Fatalf("preferences = %+v, want enabled with one device", preferences)
	}
}

func getBody(t *testing.T, url string, wantStatus int) string {
	t.Helper()

	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll(%s) error = %v", url, err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("GET %s status = %d body=%s, want %d", url, response.StatusCode, string(body), wantStatus)
	}
	return string(body)
}
