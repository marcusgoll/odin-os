package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpapi "odin-os/internal/api/http"
	healthsvc "odin-os/internal/runtime/health"
	metricsvc "odin-os/internal/telemetry/metrics"
)

func TestOperationalHandlerServesInstallablePWAShellAssets(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	defer store.Close()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health:          healthsvc.Service{DB: store.DB()},
		Metrics:         metricsvc.Service{DB: store.DB()},
		Store:           store,
		ReadModels:      store.DB(),
		RegistryHealthy: true,
		AdminToken:      "secret",
	}))
	defer server.Close()

	html := pwaGetText(t, server.URL+"/app/")
	for _, want := range []string{
		`<link rel="manifest" href="/app/manifest.webmanifest">`,
		`id="home-title"`,
		`Action Required`,
		`Approvals`,
		`Failed/Blocked`,
		`Browser Needs Help`,
		`id="register-device"`,
		`data-capture-kind="note"`,
		`data-capture-kind="voice_note"`,
		`data-capture-kind="photo"`,
		`accept="image/*"`,
		`id="voice-record"`,
		`id="failed-uploads"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("/app/ missing %q:\n%s", want, html)
		}
	}

	manifest := pwaGetText(t, server.URL+"/app/manifest.webmanifest")
	for _, want := range []string{
		`"name":"Odin Operator"`,
		`"start_url":"/app/"`,
		`"display":"standalone"`,
		`"src":"/app/icons/icon-192.svg"`,
		`"src":"/app/icons/icon-512.svg"`,
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("manifest missing %q:\n%s", want, manifest)
		}
	}

	serviceWorker := pwaGetText(t, server.URL+"/app/service-worker.js")
	for _, want := range []string{
		`/app/offline.html`,
		`shell-only`,
		`event.request.method !== 'GET'`,
	} {
		if !strings.Contains(serviceWorker, want) {
			t.Fatalf("service worker missing %q:\n%s", want, serviceWorker)
		}
	}
}

func TestOperationalHandlerExposesMobileRuntimeAPIs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedRuntimeState(t, ctx, store, "ready")
	seedOperatorReadModels(t, ctx, store)

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health:          healthsvc.Service{DB: store.DB()},
		Metrics:         metricsvc.Service{DB: store.DB()},
		Store:           store,
		ReadModels:      store.DB(),
		RegistryHealthy: true,
		AdminToken:      "secret",
	}))
	defer server.Close()

	var summary struct {
		Readiness struct {
			Ready        bool   `json:"ready"`
			HealthStatus string `json:"health_status"`
		} `json:"readiness"`
		Counts struct {
			Approvals          int `json:"approvals"`
			ReviewQueue        int `json:"review_queue"`
			WorkItems          int `json:"work_items"`
			RunAttempts        int `json:"run_attempts"`
			AutomationTriggers int `json:"automation_triggers"`
			IntakeItems        int `json:"intake_items"`
		} `json:"counts"`
	}
	pwaDecodeURLJSON(t, server.URL+"/mobile/summary", "secret", &summary)
	if !summary.Readiness.Ready || summary.Readiness.HealthStatus != "healthy" {
		t.Fatalf("/mobile/summary readiness = %+v, want healthy ready", summary.Readiness)
	}
	if summary.Counts.Approvals == 0 || summary.Counts.WorkItems == 0 || summary.Counts.RunAttempts == 0 {
		t.Fatalf("/mobile/summary counts = %+v, want runtime-backed operator counts", summary.Counts)
	}

	for _, path := range []string{
		"/mobile/approvals",
		"/mobile/review",
		"/mobile/work",
		"/mobile/inbox",
		"/mobile/settings",
	} {
		request, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatalf("NewRequest(%s) error = %v", path, err)
		}
		request.Header.Set("X-Odin-Admin-Token", "secret")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("GET %s error = %v", path, err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d body=%s, want %d", path, response.StatusCode, string(body), http.StatusOK)
		}
		if !strings.Contains(response.Header.Get("Content-Type"), "application/json") {
			t.Fatalf("GET %s Content-Type = %q, want JSON", path, response.Header.Get("Content-Type"))
		}
	}
}

func pwaGetText(t *testing.T, url string) string {
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
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d body=%s, want %d", url, response.StatusCode, string(body), http.StatusOK)
	}
	return string(body)
}

func pwaDecodeURLJSON(t *testing.T, url string, token string, target any) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest(%s) error = %v", url, err)
	}
	request.Header.Set("X-Odin-Admin-Token", token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET %s status = %d body=%s, want %d", url, response.StatusCode, string(body), http.StatusOK)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("Decode(%s) error = %v", url, err)
	}
}
