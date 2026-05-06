package huginnbrowser

import (
	"bytes"
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

	adapter "odin-os/internal/adapters/huginnbrowser"
)

func TestWorkerRunReadsJSONFetchesAllowedPageAndWritesCompletedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", request.Method)
		}
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(response, `<html><head><title>Fixture Docs</title></head><body><main>Public read only documentation for Odin.</main></body></html>`)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	if err := (Worker{HTTPClient: server.Client()}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Objective:          "Collect fixture docs",
		StartURLs:          []string{server.URL + "/docs"},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           2,
		MaxDurationSeconds: 5,
		EvidenceRequired:   true,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	var response adapter.Response
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("response JSON = %s, unmarshal error = %v", stdout.String(), err)
	}
	if response.Status != "completed" || response.AdapterKind != "huginn_live" {
		t.Fatalf("response = %+v, want completed huginn_live", response)
	}
	if len(response.VisitedURLs) != 1 || response.VisitedURLs[0] != server.URL+"/docs" {
		t.Fatalf("VisitedURLs = %#v, want fetched URL", response.VisitedURLs)
	}
	if !strings.Contains(response.ExtractedTextSummary, "Fixture Docs") || !strings.Contains(response.ExtractedTextSummary, "Public read only documentation") {
		t.Fatalf("ExtractedTextSummary = %q, want title and body summary", response.ExtractedTextSummary)
	}
	if strings.Contains(response.ExtractedTextSummary, "<title") {
		t.Fatalf("ExtractedTextSummary = %q, want plain text without HTML tags", response.ExtractedTextSummary)
	}
	if !contains(response.ActionLog, "opened_start_url") || !contains(response.ActionLog, "captured_read_only_evidence") {
		t.Fatalf("ActionLog = %#v, want read-only fetch actions", response.ActionLog)
	}
	if len(response.PageResults) != 1 {
		t.Fatalf("PageResults = %#v, want one successful page result", response.PageResults)
	}
	page := response.PageResults[0]
	if page.URL != server.URL+"/docs" || page.Status != "visited" || page.Mode != "fetch" || page.Title != "Fixture Docs" {
		t.Fatalf("page result = %+v, want visited fetch result with title", page)
	}
	if !strings.Contains(page.Summary, "Public read only documentation") {
		t.Fatalf("page summary = %q, want page text", page.Summary)
	}
}

func TestWorkerRunProcessesMultipleAllowedURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/one":
			fmt.Fprint(response, `<html><head><title>One</title></head><body>First page</body></html>`)
		case "/two":
			fmt.Fprint(response, `<html><head><title>Two</title></head><body>Second page</body></html>`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	if err := (Worker{HTTPClient: server.Client()}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Objective: "Collect multiple pages",
		StartURLs: []string{
			server.URL + "/one",
			server.URL + "/two",
		},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           2,
		MaxDurationSeconds: 5,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if response.Status != "completed" || len(response.VisitedURLs) != 2 {
		t.Fatalf("response = %+v, want two completed visits", response)
	}
	for _, want := range []string{"One", "First page", "Two", "Second page"} {
		if !strings.Contains(response.ExtractedTextSummary, want) {
			t.Fatalf("ExtractedTextSummary = %q, want %q", response.ExtractedTextSummary, want)
		}
	}
}

func TestWorkerRunSkipsDisallowedURLAndContinues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(response, `<html><head><title>Allowed</title></head><body>Allowed page</body></html>`)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	if err := (Worker{HTTPClient: server.Client()}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Objective: "Collect mixed pages",
		StartURLs: []string{
			server.URL + "/allowed",
			"https://not-allowed.example/skipped",
		},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           2,
		MaxDurationSeconds: 5,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if response.Status != "partial" || response.ErrorCode != "partial_failure" || len(response.VisitedURLs) != 1 {
		t.Fatalf("response = %+v, want partial response with one visited URL", response)
	}
	if !contains(response.ActionLog, "page_skipped_domain_not_allowed") {
		t.Fatalf("ActionLog = %#v, want disallowed skip", response.ActionLog)
	}
	if len(response.PageResults) != 2 {
		t.Fatalf("PageResults = %#v, want one visited and one skipped result", response.PageResults)
	}
	if response.PageResults[0].Status != "visited" || response.PageResults[0].URL != server.URL+"/allowed" {
		t.Fatalf("first page result = %+v, want visited allowed URL", response.PageResults[0])
	}
	if response.PageResults[1].Status != "skipped" || response.PageResults[1].URL != "https://not-allowed.example/skipped" || response.PageResults[1].ErrorCode != "domain_not_allowed" {
		t.Fatalf("second page result = %+v, want skipped domain_not_allowed", response.PageResults[1])
	}
}

func TestWorkerRunPartialFailureContinuesWithinLimits(t *testing.T) {
	client := sequenceClient{
		responses: []*http.Response{
			htmlResponse(`<html><head><title>First</title></head><body>First body</body></html>`),
			nil,
			htmlResponse(`<html><head><title>Third</title></head><body>Third body</body></html>`),
		},
		errors: []error{
			nil,
			fmt.Errorf("fixture fetch failed"),
			nil,
		},
	}
	var stdout bytes.Buffer
	if err := (Worker{HTTPClient: &client}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Objective: "Collect resilient pages",
		StartURLs: []string{
			"https://example.com/one",
			"https://example.com/two",
			"https://example.com/three",
		},
		AllowedDomains:     []string{"example.com"},
		MaxPages:           3,
		MaxDurationSeconds: 5,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if response.Status != "partial" || response.ErrorCode != "partial_failure" || len(response.VisitedURLs) != 2 {
		t.Fatalf("response = %+v, want partial response with two visited URLs", response)
	}
	if !strings.Contains(response.ExtractedTextSummary, "First body") || !strings.Contains(response.ExtractedTextSummary, "Third body") {
		t.Fatalf("ExtractedTextSummary = %q, want successful page summaries", response.ExtractedTextSummary)
	}
	if !contains(response.ActionLog, "page_fetch_failed") {
		t.Fatalf("ActionLog = %#v, want page_fetch_failed", response.ActionLog)
	}
	if len(response.PageResults) != 3 {
		t.Fatalf("PageResults = %#v, want one result per explicit URL", response.PageResults)
	}
	if response.PageResults[0].Status != "visited" || response.PageResults[1].Status != "failed" || response.PageResults[1].ErrorCode != "fetch_failed" || response.PageResults[2].Status != "visited" {
		t.Fatalf("PageResults = %#v, want visited/failed/visited diagnostics", response.PageResults)
	}
}

func TestWorkerRunDefaultsToFetchMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(response, `<html><head><title>Fetch Default</title></head><body>Default fetch mode.</body></html>`)
	}))
	defer server.Close()

	browser := &recordingBrowserRunner{}
	var stdout bytes.Buffer
	if err := (Worker{HTTPClient: server.Client(), BrowserRunner: browser}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Objective:          "Collect fixture docs",
		StartURLs:          []string{server.URL + "/docs"},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           1,
		MaxDurationSeconds: 5,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if browser.called {
		t.Fatal("browser runner was called without explicit browser mode")
	}
	response := decodeResponse(t, stdout.Bytes())
	if response.Status != "completed" || !contains(response.ActionLog, "fetch_mode_selected") {
		t.Fatalf("response = %+v, want completed fetch mode response", response)
	}
}

func TestWorkerRunBrowserModeUsesBrowserRunnerForLocalPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(response, `<html><head><title>Server should not be fetched directly</title></head><body>server</body></html>`)
	}))
	defer server.Close()

	browser := &recordingBrowserRunner{html: `<html><head><title>Rendered Fixture</title></head><body><main>JavaScript rendered content.</main></body></html>`}
	var stdout bytes.Buffer
	if err := (Worker{BrowserRunner: browser, ArtifactRoot: t.TempDir()}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Mode:               "browser",
		Objective:          "Collect rendered fixture docs",
		StartURLs:          []string{server.URL + "/docs"},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           1,
		MaxDurationSeconds: 5,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if !browser.called || browser.urls[0] != server.URL+"/docs" {
		t.Fatalf("browser runner called=%v urls=%#v, want local page URL", browser.called, browser.urls)
	}
	if response.Status != "completed" || !strings.Contains(response.ExtractedTextSummary, "Rendered Fixture") || !strings.Contains(response.ExtractedTextSummary, "JavaScript rendered content") {
		t.Fatalf("response = %+v, want rendered browser evidence", response)
	}
	if !contains(response.ActionLog, "browser_mode_selected") || !contains(response.ActionLog, "opened_start_url") || !contains(response.ActionLog, "captured_read_only_evidence") {
		t.Fatalf("ActionLog = %#v, want browser read-only actions", response.ActionLog)
	}
}

func TestWorkerRunBrowserModeHandlesPartialFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(response, `<html><head><title>server</title></head><body>server</body></html>`)
	}))
	defer server.Close()

	browser := &recordingBrowserRunner{
		htmlByURL: map[string]string{
			server.URL + "/one":   `<html><head><title>One</title></head><body>Rendered one</body></html>`,
			server.URL + "/three": `<html><head><title>Three</title></head><body>Rendered three</body></html>`,
		},
		errByURL: map[string]error{
			server.URL + "/two": fmt.Errorf("fixture browser open failed"),
		},
	}
	var stdout bytes.Buffer
	if err := (Worker{BrowserRunner: browser, ArtifactRoot: t.TempDir()}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Mode:      "browser",
		Objective: "Collect rendered pages with one failure",
		StartURLs: []string{
			server.URL + "/one",
			server.URL + "/two",
			server.URL + "/three",
		},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           3,
		MaxDurationSeconds: 5,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if response.Status != "partial" || response.ErrorCode != "partial_failure" || len(response.VisitedURLs) != 2 {
		t.Fatalf("response = %+v, want partial browser response with two visits", response)
	}
	if !strings.Contains(response.ExtractedTextSummary, "Rendered one") || !strings.Contains(response.ExtractedTextSummary, "Rendered three") {
		t.Fatalf("ExtractedTextSummary = %q, want successful browser summaries", response.ExtractedTextSummary)
	}
	if !contains(response.ActionLog, "page_browser_open_failed") {
		t.Fatalf("ActionLog = %#v, want page_browser_open_failed", response.ActionLog)
	}
}

func TestWorkerRunBrowserModeMaxPagesTruncatesStartURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(response, `<html><head><title>server</title></head><body>server</body></html>`)
	}))
	defer server.Close()

	browser := &recordingBrowserRunner{
		htmlByURL: map[string]string{
			server.URL + "/one": `<html><head><title>One</title></head><body>Rendered one</body></html>`,
			server.URL + "/two": `<html><head><title>Two</title></head><body>Rendered two</body></html>`,
		},
	}
	var stdout bytes.Buffer
	if err := (Worker{BrowserRunner: browser, ArtifactRoot: t.TempDir()}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Mode:      "browser",
		Objective: "Collect only one rendered page",
		StartURLs: []string{
			server.URL + "/one",
			server.URL + "/two",
		},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           1,
		MaxDurationSeconds: 5,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if response.Status != "completed" || len(response.VisitedURLs) != 1 || response.VisitedURLs[0] != server.URL+"/one" {
		t.Fatalf("response = %+v, want one visited browser URL", response)
	}
	if len(browser.urls) != 1 {
		t.Fatalf("browser urls = %#v, want only first URL opened", browser.urls)
	}
	if !contains(response.ActionLog, "max_pages_limit_reached") {
		t.Fatalf("ActionLog = %#v, want max_pages_limit_reached", response.ActionLog)
	}
	if len(response.PageResults) != 2 {
		t.Fatalf("PageResults = %#v, want visited and limited results", response.PageResults)
	}
	if response.PageResults[0].Status != "visited" || response.PageResults[1].Status != "limited" || response.PageResults[1].ErrorCode != "max_pages_limit_reached" {
		t.Fatalf("PageResults = %#v, want max_pages limited URL diagnostic", response.PageResults)
	}
}

func TestWorkerRunBrowserModeCapturesScreenshotWhenEvidenceRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(response, `<html><head><title>Screenshot Fixture</title></head><body><main>screen</main></body></html>`)
	}))
	defer server.Close()

	artifactRoot := t.TempDir()
	browser := &recordingBrowserRunner{html: `<html><head><title>Screenshot Fixture</title></head><body><main>screen</main></body></html>`}
	var stdout bytes.Buffer
	if err := (Worker{BrowserRunner: browser, ArtifactRoot: artifactRoot}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Mode:               "browser",
		Objective:          "Capture local screenshot",
		StartURLs:          []string{server.URL + "/docs?unsafe=../value"},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           1,
		MaxDurationSeconds: 5,
		EvidenceRequired:   true,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if len(response.Screenshots) != 1 {
		t.Fatalf("Screenshots = %#v, want one screenshot path", response.Screenshots)
	}
	screenshotPath := response.Screenshots[0]
	if !filepath.IsAbs(screenshotPath) || !strings.HasPrefix(screenshotPath, artifactRoot+string(os.PathSeparator)) {
		t.Fatalf("screenshot path = %q, want absolute path under artifact root %q", screenshotPath, artifactRoot)
	}
	if strings.Contains(screenshotPath, "..") || strings.Contains(screenshotPath, "?") || filepath.Base(screenshotPath) == "" {
		t.Fatalf("screenshot path = %q, want sanitized local path", screenshotPath)
	}
	if _, err := os.Stat(screenshotPath); err != nil {
		t.Fatalf("Stat(%q) error = %v, want screenshot file", screenshotPath, err)
	}
	if !browser.captureScreenshot {
		t.Fatal("browser runner did not receive screenshot capture request")
	}
	if code, message := validateWorkerResponse(stdout.Bytes()); code != "" {
		t.Fatalf("response contract code=%q message=%q, want valid worker JSON", code, message)
	}
	if !contains(response.ActionLog, "screenshot_captured") {
		t.Fatalf("ActionLog = %#v, want screenshot_captured", response.ActionLog)
	}
}

func TestWorkerRunBrowserModeWritesScreenshotMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(response, `<html><head><title>Metadata Fixture</title></head><body><main>screen</main></body></html>`)
	}))
	defer server.Close()

	artifactRoot := t.TempDir()
	browser := &recordingBrowserRunner{html: `<html><head><title>Metadata Fixture</title></head><body><main>screen</main></body></html>`}
	sourceURL := server.URL + "/docs"
	var stdout bytes.Buffer
	if err := (Worker{BrowserRunner: browser, ArtifactRoot: artifactRoot}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		GoalID:             42,
		Mode:               "browser",
		Objective:          "Capture local screenshot metadata",
		StartURLs:          []string{sourceURL},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           1,
		MaxDurationSeconds: 5,
		EvidenceRequired:   true,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if len(response.Screenshots) != 1 {
		t.Fatalf("Screenshots = %#v, want one screenshot path", response.Screenshots)
	}
	rawMetadata, err := os.ReadFile(metadataPathForScreenshot(response.Screenshots[0]))
	if err != nil {
		t.Fatalf("ReadFile(metadata) error = %v", err)
	}
	var metadata ArtifactMetadata
	if err := json.Unmarshal(rawMetadata, &metadata); err != nil {
		t.Fatalf("metadata JSON = %s, unmarshal error = %v", string(rawMetadata), err)
	}
	if metadata.EvidenceType != "screenshot" || metadata.GoalID != 42 || metadata.SourceURL != sourceURL || metadata.ScreenshotPath != response.Screenshots[0] {
		t.Fatalf("metadata = %+v, want screenshot metadata linked to goal/source/path", metadata)
	}
	if metadata.CreatedAt.IsZero() {
		t.Fatalf("metadata = %+v, want created_at", metadata)
	}
}

func TestWorkerRunBrowserModeSkipsScreenshotWhenEvidenceNotRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(response, `<html><head><title>No Screenshot</title></head><body><main>screen</main></body></html>`)
	}))
	defer server.Close()

	browser := &recordingBrowserRunner{html: `<html><head><title>No Screenshot</title></head><body><main>screen</main></body></html>`}
	var stdout bytes.Buffer
	if err := (Worker{BrowserRunner: browser, ArtifactRoot: t.TempDir()}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Mode:               "browser",
		Objective:          "Skip local screenshot",
		StartURLs:          []string{server.URL + "/docs"},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           1,
		MaxDurationSeconds: 5,
		EvidenceRequired:   false,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if len(response.Screenshots) != 0 {
		t.Fatalf("Screenshots = %#v, want none when evidence_required=false", response.Screenshots)
	}
	if browser.captureScreenshot {
		t.Fatal("browser runner received screenshot capture request when evidence_required=false")
	}
	if contains(response.ActionLog, "screenshot_captured") {
		t.Fatalf("ActionLog = %#v, want no screenshot_captured", response.ActionLog)
	}
}

func TestWorkerRunBrowserModeEnforcesSafetyBeforeLaunch(t *testing.T) {
	browser := &recordingBrowserRunner{}
	var stdout bytes.Buffer
	if err := (Worker{BrowserRunner: browser}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Mode:               "browser",
		Objective:          "Collect rendered fixture docs",
		StartURLs:          []string{"https://not-allowed.example/docs"},
		AllowedDomains:     []string{"example.com"},
		MaxPages:           1,
		MaxDurationSeconds: 5,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if browser.called {
		t.Fatal("browser runner was called before allowed_domains validation")
	}
	response := decodeResponse(t, stdout.Bytes())
	if response.Status != "failed" || response.ErrorCode != "domain_not_allowed" {
		t.Fatalf("response = %+v, want domain_not_allowed failure", response)
	}
}

func TestChromeBrowserRunnerRendersLocalStaticPage(t *testing.T) {
	if _, err := findChromeBinary(); err != nil {
		t.Skipf("chromium-compatible browser unavailable: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(response, `<html><head><title>Before JS</title><script>document.addEventListener("DOMContentLoaded", () => { document.title = "Rendered JS"; document.body.innerHTML = "<main>JavaScript rendered local content.</main>"; });</script></head><body>before</body></html>`)
	}))
	defer server.Close()

	page, err := (chromeBrowserRunner{}).Open(context.Background(), server.URL+"/js", BrowserOptions{MaxDurationSeconds: 5})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	summary := summarizePage([]byte(page.HTML))
	if !strings.Contains(summary, "Rendered JS") || !strings.Contains(summary, "JavaScript rendered local content") {
		t.Fatalf("summary = %q, want JavaScript-rendered local page evidence", summary)
	}
	if strings.Contains(summary, "document.addEventListener") {
		t.Fatalf("summary = %q, want script source omitted", summary)
	}
}

func TestWorkerRunBrowserModeWithRealChromeUsesLocalStaticPage(t *testing.T) {
	if _, err := findChromeBinary(); err != nil {
		t.Skipf("chromium-compatible browser unavailable: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(response, `<html><head><title>Worker JS</title><script>document.addEventListener("DOMContentLoaded", () => { document.body.innerHTML = "<main>Worker browser mode rendered JavaScript.</main>"; });</script></head><body>before</body></html>`)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	artifactRoot := t.TempDir()
	if err := (Worker{ArtifactRoot: artifactRoot}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Mode:               "browser",
		Objective:          "Collect rendered local fixture",
		StartURLs:          []string{server.URL + "/js"},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           1,
		MaxDurationSeconds: 5,
		EvidenceRequired:   true,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if response.Status != "completed" || !strings.Contains(response.ExtractedTextSummary, "Worker browser mode rendered JavaScript") {
		t.Fatalf("response = %+v, want browser mode completed with rendered JavaScript", response)
	}
	if len(response.Screenshots) != 1 {
		t.Fatalf("Screenshots = %#v, want one local screenshot", response.Screenshots)
	}
	if _, err := os.Stat(response.Screenshots[0]); err != nil {
		t.Fatalf("Stat(%q) error = %v, want real Chrome screenshot file", response.Screenshots[0], err)
	}
	if !strings.HasPrefix(response.Screenshots[0], artifactRoot+string(os.PathSeparator)) {
		t.Fatalf("screenshot path = %q, want under artifact root %q", response.Screenshots[0], artifactRoot)
	}
	if !contains(response.ActionLog, "browser_mode_selected") || !contains(response.ActionLog, "no_clicks_performed") || !contains(response.ActionLog, "no_cookies_or_session_profile") {
		t.Fatalf("ActionLog = %#v, want read-only browser safety markers", response.ActionLog)
	}
}

func TestWorkerRunRejectsDisallowedDomainWithoutFetching(t *testing.T) {
	client := &recordingClient{}
	var stdout bytes.Buffer
	if err := (Worker{HTTPClient: client}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Objective:          "Collect fixture docs",
		StartURLs:          []string{"https://not-allowed.example/docs"},
		AllowedDomains:     []string{"example.com"},
		MaxPages:           1,
		MaxDurationSeconds: 5,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if client.called {
		t.Fatal("HTTP client was called for disallowed domain")
	}
	response := decodeResponse(t, stdout.Bytes())
	if response.Status != "failed" || response.ErrorCode != "domain_not_allowed" {
		t.Fatalf("response = %+v, want domain_not_allowed failure", response)
	}
}

func TestWorkerRunEnforcesMaxPages(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		fmt.Fprintf(response, `<html><head><title>Page %d</title></head><body>Body %d</body></html>`, requests, requests)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	if err := (Worker{HTTPClient: server.Client()}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Objective: "Collect fixture docs",
		StartURLs: []string{
			server.URL + "/one",
			server.URL + "/two",
		},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           1,
		MaxDurationSeconds: 5,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if requests != 1 || len(response.VisitedURLs) != 1 || response.VisitedURLs[0] != server.URL+"/one" {
		t.Fatalf("requests=%d response=%+v, want only first page fetched", requests, response)
	}
	if !contains(response.ActionLog, "max_pages_limit_reached") {
		t.Fatalf("ActionLog = %#v, want max_pages_limit_reached", response.ActionLog)
	}
}

func TestWorkerRunSiteProfileReducesMaxPages(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		fmt.Fprintf(response, `<html><head><title>Page %d</title></head><body>Body %d</body></html>`, requests, requests)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	if err := (Worker{HTTPClient: server.Client()}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Objective: "Collect profile-limited docs",
		StartURLs: []string{
			server.URL + "/one",
			server.URL + "/two",
		},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           2,
		MaxDurationSeconds: 5,
		SiteProfiles: []adapter.SiteProfile{
			{Domain: serverURLHost(t, server.URL), MaxPages: 1, ModeAllowed: "both"},
		},
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if requests != 1 || len(response.VisitedURLs) != 1 || response.VisitedURLs[0] != server.URL+"/one" {
		t.Fatalf("requests=%d response=%+v, want site profile to reduce max_pages to first page", requests, response)
	}
	if !contains(response.ActionLog, "site_profile_applied") || !contains(response.ActionLog, "max_pages_limit_reached") {
		t.Fatalf("ActionLog = %#v, want site_profile_applied and max_pages_limit_reached", response.ActionLog)
	}
}

func TestWorkerRunConfiguredSiteProfileAppliesWithoutRequestProfile(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		fmt.Fprintf(response, `<html><head><title>%s</title></head><body>Configured profile page</body></html>`, request.URL.Path)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	worker := Worker{
		HTTPClient: server.Client(),
		SiteProfiles: []adapter.SiteProfile{
			{Domain: serverURLHost(t, server.URL), MaxPages: 1, ModeAllowed: "fetch"},
		},
	}
	if err := worker.Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Objective: "Collect configured profile docs",
		StartURLs: []string{
			server.URL + "/one",
			server.URL + "/two",
		},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           2,
		MaxDurationSeconds: 5,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if requests != 1 || len(response.VisitedURLs) != 1 {
		t.Fatalf("requests=%d response=%+v, want configured site profile max_pages applied", requests, response)
	}
	if !contains(response.ActionLog, "site_profile_applied") {
		t.Fatalf("ActionLog = %#v, want site_profile_applied", response.ActionLog)
	}
}

func TestWorkerRunSiteProfileDeniesBrowserMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(response, `<html><head><title>Denied Browser</title></head><body>server</body></html>`)
	}))
	defer server.Close()

	browser := &recordingBrowserRunner{}
	var stdout bytes.Buffer
	if err := (Worker{BrowserRunner: browser}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Mode:               "browser",
		Objective:          "Collect rendered docs",
		StartURLs:          []string{server.URL + "/docs"},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           1,
		MaxDurationSeconds: 5,
		SiteProfiles: []adapter.SiteProfile{
			{Domain: serverURLHost(t, server.URL), ModeAllowed: "fetch"},
		},
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if browser.called {
		t.Fatal("browser runner was called despite profile mode denial")
	}
	response := decodeResponse(t, stdout.Bytes())
	if response.Status != "failed" || response.ErrorCode != "site_profile_mode_denied" {
		t.Fatalf("response = %+v, want site_profile_mode_denied failure", response)
	}
	if !contains(response.ActionLog, "site_profile_applied") || !contains(response.ActionLog, "site_profile_mode_denied") {
		t.Fatalf("ActionLog = %#v, want profile mode denial markers", response.ActionLog)
	}
	if len(response.PageResults) != 1 || response.PageResults[0].Status != "skipped" || response.PageResults[0].ErrorCode != "site_profile_mode_denied" || response.PageResults[0].Mode != "browser" {
		t.Fatalf("PageResults = %#v, want skipped site_profile_mode_denied browser result", response.PageResults)
	}
}

func TestWorkerRunSiteProfileDelayHookIsApplied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fmt.Fprintf(response, `<html><head><title>%s</title></head><body>Delayed page</body></html>`, request.URL.Path)
	}))
	defer server.Close()

	var delays []time.Duration
	var stdout bytes.Buffer
	worker := Worker{
		HTTPClient: server.Client(),
		Delay: func(ctx context.Context, duration time.Duration) error {
			delays = append(delays, duration)
			return ctx.Err()
		},
	}
	if err := worker.Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Objective: "Collect delayed docs",
		StartURLs: []string{
			server.URL + "/one",
			server.URL + "/two",
		},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           2,
		MaxDurationSeconds: 5,
		SiteProfiles: []adapter.SiteProfile{
			{Domain: serverURLHost(t, server.URL), MinDelayMS: 125, ModeAllowed: "fetch"},
		},
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if response.Status != "completed" || len(response.VisitedURLs) != 2 {
		t.Fatalf("response = %+v, want completed delayed fetch", response)
	}
	if len(delays) != 2 || delays[0] != 125*time.Millisecond || delays[1] != 125*time.Millisecond {
		t.Fatalf("delays = %#v, want two injected 125ms delays without sleeping", delays)
	}
	if !contains(response.ActionLog, "rate_limit_delay_applied") {
		t.Fatalf("ActionLog = %#v, want rate_limit_delay_applied", response.ActionLog)
	}
}

func TestWorkerRunMissingSiteProfileUsesRequestLimits(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		fmt.Fprintf(response, `<html><head><title>%s</title></head><body>Fallback page</body></html>`, request.URL.Path)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	if err := (Worker{HTTPClient: server.Client()}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Objective: "Collect fallback docs",
		StartURLs: []string{
			server.URL + "/one",
			server.URL + "/two",
		},
		AllowedDomains:     []string{serverURLHost(t, server.URL)},
		MaxPages:           2,
		MaxDurationSeconds: 5,
		SiteProfiles: []adapter.SiteProfile{
			{Domain: "other.example", MaxPages: 1, MinDelayMS: 500, ModeAllowed: "fetch"},
		},
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if requests != 2 || len(response.VisitedURLs) != 2 {
		t.Fatalf("requests=%d response=%+v, want request limits when no profile matches", requests, response)
	}
	if contains(response.ActionLog, "site_profile_applied") || contains(response.ActionLog, "rate_limit_delay_applied") {
		t.Fatalf("ActionLog = %#v, want no site profile markers without matching profile", response.ActionLog)
	}
}

func TestWorkerRunAllowedDomainsStillEnforcedWithSiteProfile(t *testing.T) {
	client := &recordingClient{}
	var stdout bytes.Buffer
	if err := (Worker{HTTPClient: client}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Objective:          "Collect disallowed profile docs",
		StartURLs:          []string{"https://not-allowed.example/docs"},
		AllowedDomains:     []string{"example.com"},
		MaxPages:           1,
		MaxDurationSeconds: 5,
		SiteProfiles: []adapter.SiteProfile{
			{Domain: "not-allowed.example", MaxPages: 1, ModeAllowed: "fetch"},
		},
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if client.called {
		t.Fatal("HTTP client was called for disallowed domain with matching profile")
	}
	response := decodeResponse(t, stdout.Bytes())
	if response.Status != "failed" || response.ErrorCode != "domain_not_allowed" {
		t.Fatalf("response = %+v, want domain_not_allowed failure", response)
	}
	if contains(response.ActionLog, "site_profile_applied") {
		t.Fatalf("ActionLog = %#v, profile must not bypass allowed_domains", response.ActionLog)
	}
}

func TestWorkerRunEnforcesMaxDuration(t *testing.T) {
	var stdout bytes.Buffer
	if err := (Worker{HTTPClient: blockingClient{}}).Run(context.Background(), strings.NewReader(requestJSON(t, adapter.Request{
		Objective:          "Collect slow docs",
		StartURLs:          []string{"https://example.com/docs"},
		AllowedDomains:     []string{"example.com"},
		MaxPages:           1,
		MaxDurationSeconds: 1,
	})), &stdout); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	response := decodeResponse(t, stdout.Bytes())
	if response.Status != "timeout" || response.ErrorCode != "worker_timeout" {
		t.Fatalf("response = %+v, want worker_timeout response", response)
	}
}

func TestCleanupBrowserArtifactsRequiresOlderThan(t *testing.T) {
	_, err := CleanupBrowserArtifacts(CleanupOptions{ArtifactRoot: t.TempDir()})
	if err == nil {
		t.Fatal("CleanupBrowserArtifacts() error = nil, want older-than requirement")
	}
}

func TestCleanupBrowserArtifactsDryRunDoesNotDelete(t *testing.T) {
	now := time.Date(2026, 5, 5, 20, 0, 0, 0, time.UTC)
	root := t.TempDir()
	oldScreenshot := writeArtifactFile(t, root, "screenshots/old.png", now.Add(-2*time.Hour))
	oldMetadata := oldScreenshot + ".metadata.json"
	if err := os.WriteFile(oldMetadata, []byte(`{"evidence_type":"screenshot"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(metadata) error = %v", err)
	}
	if err := os.Chtimes(oldMetadata, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("Chtimes(metadata) error = %v", err)
	}

	result, err := CleanupBrowserArtifacts(CleanupOptions{ArtifactRoot: root, OlderThan: time.Hour, Now: now})
	if err != nil {
		t.Fatalf("CleanupBrowserArtifacts() error = %v", err)
	}
	if !result.DryRun || len(result.Candidates) != 2 || len(result.Deleted) != 0 {
		t.Fatalf("result = %+v, want dry-run candidates without deletion", result)
	}
	for _, path := range []string{oldScreenshot, oldMetadata} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Stat(%q) error = %v, want dry-run to keep file", path, err)
		}
	}
}

func TestCleanupBrowserArtifactsDeletesEligibleOldFilesOnly(t *testing.T) {
	now := time.Date(2026, 5, 5, 20, 0, 0, 0, time.UTC)
	root := t.TempDir()
	oldScreenshot := writeArtifactFile(t, root, "screenshots/old.png", now.Add(-2*time.Hour))
	oldMetadata := oldScreenshot + ".metadata.json"
	if err := os.WriteFile(oldMetadata, []byte(`{"evidence_type":"screenshot"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(metadata) error = %v", err)
	}
	if err := os.Chtimes(oldMetadata, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("Chtimes(metadata) error = %v", err)
	}
	newScreenshot := writeArtifactFile(t, root, "screenshots/new.png", now.Add(-10*time.Minute))
	otherFile := writeArtifactFile(t, root, "screenshots/not-huginn.txt", now.Add(-2*time.Hour))
	outsideFile := filepath.Join(t.TempDir(), "old.png")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatalf("WriteFile(outside) error = %v", err)
	}
	if err := os.Chtimes(outsideFile, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("Chtimes(outside) error = %v", err)
	}

	result, err := CleanupBrowserArtifacts(CleanupOptions{ArtifactRoot: root, OlderThan: time.Hour, Now: now, Apply: true})
	if err != nil {
		t.Fatalf("CleanupBrowserArtifacts() error = %v", err)
	}
	if result.DryRun || len(result.Deleted) != 2 {
		t.Fatalf("result = %+v, want two deleted Huginn files", result)
	}
	for _, path := range []string{oldScreenshot, oldMetadata} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("Stat(%q) error = %v, want deleted", path, err)
		}
	}
	for _, path := range []string{newScreenshot, otherFile, outsideFile} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Stat(%q) error = %v, want preserved", path, err)
		}
	}
}

func requestJSON(t *testing.T, request adapter.Request) string {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return string(raw)
}

func writeArtifactFile(t *testing.T, root string, relative string, modTime time.Time) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("artifact"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("Chtimes(%q) error = %v", path, err)
	}
	return path
}

func decodeResponse(t *testing.T, raw []byte) adapter.Response {
	t.Helper()
	var response adapter.Response
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("response JSON = %s, unmarshal error = %v", string(raw), err)
	}
	return response
}

func validateWorkerResponse(raw []byte) (string, string) {
	var response adapter.Response
	if err := json.Unmarshal(raw, &response); err != nil {
		return "invalid_response_json", err.Error()
	}
	if strings.TrimSpace(response.Status) == "" {
		return "response_contract_invalid", "status is required"
	}
	if strings.TrimSpace(response.AdapterKind) == "" {
		return "response_contract_invalid", "adapter_kind is required"
	}
	return "", ""
}

func serverURLHost(t *testing.T, rawURL string) string {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	return request.URL.Hostname()
}

type recordingClient struct {
	called bool
}

func (client *recordingClient) Do(request *http.Request) (*http.Response, error) {
	client.called = true
	return nil, fmt.Errorf("unexpected request to %s", request.URL.String())
}

type sequenceClient struct {
	responses []*http.Response
	errors    []error
	calls     int
}

func (client *sequenceClient) Do(_ *http.Request) (*http.Response, error) {
	index := client.calls
	client.calls++
	return client.responses[index], client.errors[index]
}

func htmlResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type blockingClient struct{}

func (blockingClient) Do(request *http.Request) (*http.Response, error) {
	<-request.Context().Done()
	return nil, request.Context().Err()
}

type recordingBrowserRunner struct {
	called            bool
	urls              []string
	html              string
	htmlByURL         map[string]string
	errByURL          map[string]error
	captureScreenshot bool
}

func (runner *recordingBrowserRunner) Open(ctx context.Context, rawURL string, options BrowserOptions) (BrowserPage, error) {
	runner.called = true
	runner.urls = append(runner.urls, rawURL)
	runner.captureScreenshot = options.CaptureScreenshot
	if err := runner.errByURL[rawURL]; err != nil {
		return BrowserPage{}, err
	}
	select {
	case <-ctx.Done():
		return BrowserPage{}, ctx.Err()
	default:
	}
	html := runner.html
	if runner.htmlByURL != nil {
		html = runner.htmlByURL[rawURL]
	}
	page := BrowserPage{URL: rawURL, HTML: html}
	if options.CaptureScreenshot {
		if err := os.WriteFile(options.ScreenshotPath, []byte("fake screenshot"), 0o644); err != nil {
			return BrowserPage{}, err
		}
		page.Screenshots = []string{options.ScreenshotPath}
	}
	return page, nil
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
