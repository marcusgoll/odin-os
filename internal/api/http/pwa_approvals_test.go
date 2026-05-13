package httpapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpapi "odin-os/internal/api/http"
)

func TestPWAStaticShellIncludesApprovalControls(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{}))
	defer server.Close()

	html := pwaGetText(t, server.URL+"/app/")
	for _, want := range []string{
		`<link rel="manifest" href="/app/manifest.webmanifest">`,
		`navigator.serviceWorker.register('/app/service-worker.js')`,
		`id="approvals-panel"`,
		`id="approvals-status"`,
		`id="approvals-list"`,
		`id="refresh-approvals"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("/app/ missing %q:\n%s", want, html)
		}
	}

	appJS := pwaGetText(t, server.URL+"/app/app.js")
	for _, want := range []string{
		`/mobile/approvals`,
		`data-approval-action`,
		`confirmation_text`,
		`expected_policy_snapshot_hash`,
		`expected_runtime_snapshot_hash`,
		`X-Odin-Admin-Token`,
		`decision_by: 'pwa'`,
	} {
		if !strings.Contains(appJS, want) {
			t.Fatalf("app.js missing %q:\n%s", want, appJS)
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
