package huginnbrowser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	adapter "odin-os/internal/adapters/huginnbrowser"
)

const (
	adapterKind       = "huginn_live"
	maxResponseBytes  = 512 * 1024
	defaultUserAgent  = "odin-huginn-browser-worker/0 read-only"
	defaultSummaryMax = 500
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type BrowserRunner interface {
	Open(context.Context, string, BrowserOptions) (BrowserPage, error)
}

type BrowserOptions struct {
	MaxDurationSeconds int
	CaptureScreenshot  bool
	ScreenshotPath     string
}

type BrowserPage struct {
	URL         string
	HTML        string
	Screenshots []string
}

type ArtifactMetadata struct {
	CreatedAt      time.Time `json:"created_at"`
	GoalID         int64     `json:"goal_id,omitempty"`
	SourceURL      string    `json:"source_url"`
	EvidenceType   string    `json:"evidence_type"`
	ScreenshotPath string    `json:"screenshot_path"`
}

type CleanupOptions struct {
	ArtifactRoot string
	OlderThan    time.Duration
	Now          time.Time
	Apply        bool
}

type CleanupResult struct {
	DryRun     bool     `json:"dry_run"`
	Candidates []string `json:"candidates"`
	Deleted    []string `json:"deleted"`
}

type Worker struct {
	HTTPClient    HTTPClient
	BrowserRunner BrowserRunner
	ArtifactRoot  string
	SiteProfiles  []adapter.SiteProfile
	Delay         func(context.Context, time.Duration) error
}

func (worker Worker) Run(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	var request adapter.Request
	if err := json.NewDecoder(stdin).Decode(&request); err != nil {
		return writeResponse(stdout, failedResponse(request, "invalid_request_json", err.Error()))
	}
	if err := validateWorkerRequest(request); err != nil {
		return writeResponse(stdout, failedResponse(request, "invalid_request", err.Error()))
	}
	if err := validateSiteProfiles(worker.SiteProfiles); err != nil {
		return writeResponse(stdout, failedResponse(request, "invalid_site_profile_config", err.Error()))
	}
	allowedDomains := normalizeDomains(request.AllowedDomains)
	siteProfiles := normalizeSiteProfiles(append(append([]adapter.SiteProfile{}, worker.SiteProfiles...), request.SiteProfiles...))
	maxDurationSeconds := effectiveMaxDurationSeconds(request, allowedDomains, siteProfiles)

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(maxDurationSeconds)*time.Second)
	defer cancel()

	visitedURLs := make([]string, 0, min(request.MaxPages, len(request.StartURLs)))
	summaries := make([]string, 0, min(request.MaxPages, len(request.StartURLs)))
	pageResults := make([]adapter.PageResult, 0, len(request.StartURLs))
	actionLog := []string{
		"live_adapter_selected",
		"validated_read_only_request",
		"no_login_or_session_handling",
		"no_form_submission",
		"no_external_mutation_performed",
	}
	mode := normalizeMode(request.Mode)
	switch mode {
	case "fetch":
		actionLog = append(actionLog, "fetch_mode_selected")
		return worker.runFetchMode(runCtx, request, allowedDomains, siteProfiles, visitedURLs, summaries, pageResults, actionLog, stdout)
	case "browser":
		actionLog = append(actionLog, "browser_mode_selected", "no_clicks_performed", "no_cookies_or_session_profile")
		return worker.runBrowserMode(runCtx, request, allowedDomains, siteProfiles, visitedURLs, summaries, pageResults, actionLog, stdout)
	default:
		return writeResponse(stdout, failedResponse(request, "unsupported_mode", fmt.Sprintf("unsupported worker mode %q", request.Mode)))
	}
}

func (worker Worker) runFetchMode(ctx context.Context, request adapter.Request, allowedDomains []string, siteProfiles []adapter.SiteProfile, visitedURLs []string, summaries []string, pageResults []adapter.PageResult, actionLog []string, stdout io.Writer) error {
	client := worker.HTTPClient
	if client == nil {
		client = defaultHTTPClient(time.Duration(request.MaxDurationSeconds) * time.Second)
	}
	var failures []string
	mode := "fetch"

	for index, startURL := range request.StartURLs {
		select {
		case <-ctx.Done():
			return writeResponse(stdout, timeoutResponse(request, visitedURLs, pageResults, actionLog))
		default:
		}
		if !startURLAllowed(startURL, allowedDomains) {
			failures = append(failures, "domain_not_allowed")
			actionLog = append(actionLog, "page_skipped_domain_not_allowed")
			pageResults = append(pageResults, failedPageResult(startURL, "skipped", mode, "domain_not_allowed", "URL domain is not in allowed_domains."))
			continue
		}
		profile := effectiveSiteProfile(startURL, siteProfiles)
		if profile.Applied {
			actionLog = append(actionLog, "site_profile_applied")
		}
		if index >= profile.effectiveMaxPages(request.MaxPages) {
			actionLog = append(actionLog, "max_pages_limit_reached")
			pageResults = append(pageResults, failedPageResult(startURL, "limited", mode, "max_pages_limit_reached", "URL was not collected because max_pages was reached."))
			continue
		}
		if !profile.allowsMode("fetch") {
			failures = append(failures, "site_profile_mode_denied")
			actionLog = append(actionLog, "site_profile_mode_denied")
			pageResults = append(pageResults, failedPageResult(startURL, "skipped", mode, "site_profile_mode_denied", "Site profile does not allow fetch mode."))
			continue
		}
		var delayErr error
		actionLog, delayErr = worker.applyRateLimitDelay(ctx, profile, actionLog)
		if ctx.Err() == context.DeadlineExceeded {
			return writeResponse(stdout, timeoutResponse(request, visitedURLs, pageResults, actionLog))
		}
		if delayErr != nil {
			failures = append(failures, "rate_limit_delay_failed")
			actionLog = append(actionLog, "rate_limit_delay_failed")
			pageResults = append(pageResults, failedPageResult(startURL, "failed", mode, "rate_limit_delay_failed", delayErr.Error()))
			continue
		}

		httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, startURL, nil)
		if err != nil {
			failures = append(failures, "invalid_start_url")
			actionLog = append(actionLog, "page_invalid_start_url")
			pageResults = append(pageResults, failedPageResult(startURL, "failed", mode, "invalid_start_url", err.Error()))
			continue
		}
		httpRequest.Header.Set("User-Agent", defaultUserAgent)
		httpResponse, err := client.Do(httpRequest)
		if ctx.Err() == context.DeadlineExceeded {
			return writeResponse(stdout, timeoutResponse(request, visitedURLs, pageResults, actionLog))
		}
		if err != nil {
			failures = append(failures, "fetch_failed")
			actionLog = append(actionLog, "page_fetch_failed")
			pageResults = append(pageResults, failedPageResult(startURL, "failed", mode, "fetch_failed", err.Error()))
			continue
		}
		func() {
			defer httpResponse.Body.Close()
			visitedURLs = append(visitedURLs, startURL)
			actionLog = append(actionLog, "opened_start_url")
			body, readErr := io.ReadAll(io.LimitReader(httpResponse.Body, maxResponseBytes))
			if readErr != nil {
				failures = append(failures, "read_failed")
				actionLog = append(actionLog, "read_failed")
				summaries = append(summaries, "Read failed: "+readErr.Error())
				pageResults = append(pageResults, failedPageResult(startURL, "failed", mode, "read_failed", readErr.Error()))
				return
			}
			title := pageTitle(body)
			summary := summarizePage(body)
			summaries = append(summaries, summary)
			pageResults = append(pageResults, adapter.PageResult{
				URL:     startURL,
				Status:  "visited",
				Mode:    mode,
				Title:   title,
				Summary: summary,
			})
			actionLog = append(actionLog, "captured_read_only_evidence")
		}()
	}

	return writeResponse(stdout, adapter.Response{
		Status:               responseStatus(visitedURLs, failures),
		AdapterKind:          adapterKind,
		VisitedURLs:          visitedURLs,
		PageResults:          pageResults,
		ExtractedTextSummary: strings.TrimSpace(strings.Join(nonEmptyStrings(summaries), "\n")),
		Screenshots:          []string{},
		ActionLog:            actionLog,
		ErrorCode:            partialErrorCode(visitedURLs, failures),
		ErrorMessage:         partialErrorMessage(visitedURLs, failures),
	})
}

func (worker Worker) runBrowserMode(ctx context.Context, request adapter.Request, allowedDomains []string, siteProfiles []adapter.SiteProfile, visitedURLs []string, summaries []string, pageResults []adapter.PageResult, actionLog []string, stdout io.Writer) error {
	runner := worker.BrowserRunner
	if runner == nil {
		runner = chromeBrowserRunner{}
	}
	screenshots := []string{}
	var failures []string
	mode := "browser"
	for index, startURL := range request.StartURLs {
		select {
		case <-ctx.Done():
			return writeResponse(stdout, timeoutResponse(request, visitedURLs, pageResults, actionLog))
		default:
		}
		if !startURLAllowed(startURL, allowedDomains) {
			failures = append(failures, "domain_not_allowed")
			actionLog = append(actionLog, "page_skipped_domain_not_allowed")
			pageResults = append(pageResults, failedPageResult(startURL, "skipped", mode, "domain_not_allowed", "URL domain is not in allowed_domains."))
			continue
		}
		profile := effectiveSiteProfile(startURL, siteProfiles)
		if profile.Applied {
			actionLog = append(actionLog, "site_profile_applied")
		}
		if index >= profile.effectiveMaxPages(request.MaxPages) {
			actionLog = append(actionLog, "max_pages_limit_reached")
			pageResults = append(pageResults, failedPageResult(startURL, "limited", mode, "max_pages_limit_reached", "URL was not collected because max_pages was reached."))
			continue
		}
		if !profile.allowsMode("browser") {
			failures = append(failures, "site_profile_mode_denied")
			actionLog = append(actionLog, "site_profile_mode_denied")
			pageResults = append(pageResults, failedPageResult(startURL, "skipped", mode, "site_profile_mode_denied", "Site profile does not allow browser mode."))
			continue
		}
		var delayErr error
		actionLog, delayErr = worker.applyRateLimitDelay(ctx, profile, actionLog)
		if ctx.Err() == context.DeadlineExceeded {
			return writeResponse(stdout, timeoutResponse(request, visitedURLs, pageResults, actionLog))
		}
		if delayErr != nil {
			failures = append(failures, "rate_limit_delay_failed")
			actionLog = append(actionLog, "rate_limit_delay_failed")
			pageResults = append(pageResults, failedPageResult(startURL, "failed", mode, "rate_limit_delay_failed", delayErr.Error()))
			continue
		}
		options := BrowserOptions{MaxDurationSeconds: profile.effectiveMaxDurationSeconds(request.MaxDurationSeconds)}
		if request.EvidenceRequired {
			screenshotPath, err := worker.screenshotPath(startURL, index)
			if err != nil {
				return writeResponse(stdout, failedResponse(request, "screenshot_path_failed", err.Error()))
			}
			options.CaptureScreenshot = true
			options.ScreenshotPath = screenshotPath
		}
		page, err := runner.Open(ctx, startURL, options)
		if ctx.Err() == context.DeadlineExceeded {
			return writeResponse(stdout, timeoutResponse(request, visitedURLs, pageResults, actionLog))
		}
		if err != nil {
			failures = append(failures, "browser_open_failed")
			actionLog = append(actionLog, "page_browser_open_failed")
			pageResults = append(pageResults, failedPageResult(startURL, "failed", mode, "browser_open_failed", err.Error()))
			continue
		}
		visitedURL := strings.TrimSpace(page.URL)
		if visitedURL == "" {
			visitedURL = startURL
		}
		visitedURLs = append(visitedURLs, visitedURL)
		actionLog = append(actionLog, "opened_start_url")
		title := pageTitle([]byte(page.HTML))
		summary := summarizePage([]byte(page.HTML))
		summaries = append(summaries, summary)
		pageResults = append(pageResults, adapter.PageResult{
			URL:     startURL,
			Status:  "visited",
			Mode:    mode,
			Title:   title,
			Summary: summary,
		})
		actionLog = append(actionLog, "captured_read_only_evidence")
		if len(page.Screenshots) > 0 {
			for _, screenshotPath := range page.Screenshots {
				if err := writeScreenshotMetadata(screenshotPath, ArtifactMetadata{
					CreatedAt:      time.Now().UTC(),
					GoalID:         request.GoalID,
					SourceURL:      startURL,
					EvidenceType:   "screenshot",
					ScreenshotPath: screenshotPath,
				}); err != nil {
					return writeResponse(stdout, failedResponse(request, "screenshot_metadata_failed", err.Error()))
				}
			}
			screenshots = append(screenshots, page.Screenshots...)
			actionLog = append(actionLog, "screenshot_captured")
		}
	}
	return writeResponse(stdout, adapter.Response{
		Status:               responseStatus(visitedURLs, failures),
		AdapterKind:          adapterKind,
		VisitedURLs:          visitedURLs,
		PageResults:          pageResults,
		ExtractedTextSummary: strings.TrimSpace(strings.Join(nonEmptyStrings(summaries), "\n")),
		Screenshots:          screenshots,
		ActionLog:            actionLog,
		ErrorCode:            partialErrorCode(visitedURLs, failures),
		ErrorMessage:         partialErrorMessage(visitedURLs, failures),
	})
}

func startURLAllowed(startURL string, allowedDomains []string) bool {
	host, err := readOnlyURLHost(startURL)
	if err != nil {
		return false
	}
	return domainAllowed(host, allowedDomains)
}

type effectiveProfile struct {
	Applied            bool
	MaxPages           int
	MinDelayMS         int
	MaxDurationSeconds int
	ModeAllowed        string
}

func normalizeSiteProfiles(profiles []adapter.SiteProfile) []adapter.SiteProfile {
	normalized := make([]adapter.SiteProfile, 0, len(profiles))
	for _, profile := range profiles {
		domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(profile.Domain)), ".")
		if domain == "" {
			continue
		}
		profile.Domain = domain
		profile.ModeAllowed = strings.ToLower(strings.TrimSpace(profile.ModeAllowed))
		normalized = append(normalized, profile)
	}
	return normalized
}

func effectiveMaxDurationSeconds(request adapter.Request, allowedDomains []string, profiles []adapter.SiteProfile) int {
	effective := request.MaxDurationSeconds
	for _, startURL := range request.StartURLs {
		if !startURLAllowed(startURL, allowedDomains) {
			continue
		}
		profile := effectiveSiteProfile(startURL, profiles)
		if profile.Applied && profile.MaxDurationSeconds > 0 && profile.MaxDurationSeconds < effective {
			effective = profile.MaxDurationSeconds
		}
	}
	return effective
}

func effectiveSiteProfile(startURL string, profiles []adapter.SiteProfile) effectiveProfile {
	host, err := readOnlyURLHost(startURL)
	if err != nil {
		return effectiveProfile{ModeAllowed: "both"}
	}
	effective := effectiveProfile{ModeAllowed: "both"}
	for _, profile := range profiles {
		if !domainAllowed(host, []string{profile.Domain}) {
			continue
		}
		effective.Applied = true
		if profile.MaxPages > 0 && (effective.MaxPages == 0 || profile.MaxPages < effective.MaxPages) {
			effective.MaxPages = profile.MaxPages
		}
		if profile.MinDelayMS > effective.MinDelayMS {
			effective.MinDelayMS = profile.MinDelayMS
		}
		if profile.MaxDurationSeconds > 0 && (effective.MaxDurationSeconds == 0 || profile.MaxDurationSeconds < effective.MaxDurationSeconds) {
			effective.MaxDurationSeconds = profile.MaxDurationSeconds
		}
		effective.ModeAllowed = combineModeAllowed(effective.ModeAllowed, profile.ModeAllowed)
	}
	return effective
}

func (profile effectiveProfile) effectiveMaxPages(requestMaxPages int) int {
	if profile.MaxPages > 0 && profile.MaxPages < requestMaxPages {
		return profile.MaxPages
	}
	return requestMaxPages
}

func (profile effectiveProfile) effectiveMaxDurationSeconds(requestMaxDurationSeconds int) int {
	if profile.MaxDurationSeconds > 0 && profile.MaxDurationSeconds < requestMaxDurationSeconds {
		return profile.MaxDurationSeconds
	}
	return requestMaxDurationSeconds
}

func (profile effectiveProfile) allowsMode(mode string) bool {
	allowed := strings.ToLower(strings.TrimSpace(profile.ModeAllowed))
	switch allowed {
	case "", "both":
		return true
	default:
		return allowed == strings.ToLower(strings.TrimSpace(mode))
	}
}

func combineModeAllowed(current string, next string) string {
	current = strings.ToLower(strings.TrimSpace(current))
	next = strings.ToLower(strings.TrimSpace(next))
	if current == "" {
		current = "both"
	}
	if next == "" || next == "both" {
		return current
	}
	if current == "both" || current == next {
		return next
	}
	return "none"
}

func (worker Worker) applyRateLimitDelay(ctx context.Context, profile effectiveProfile, actionLog []string) ([]string, error) {
	if !profile.Applied || profile.MinDelayMS <= 0 {
		return actionLog, nil
	}
	actionLog = append(actionLog, "rate_limit_delay_applied")
	duration := time.Duration(profile.MinDelayMS) * time.Millisecond
	if worker.Delay != nil {
		return actionLog, worker.Delay(ctx, duration)
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return actionLog, ctx.Err()
	case <-timer.C:
		return actionLog, nil
	}
}

func responseStatus(visitedURLs []string, failures []string) string {
	switch {
	case len(failures) == 0:
		return "completed"
	case len(visitedURLs) > 0:
		return "partial"
	default:
		return "failed"
	}
}

func partialErrorCode(visitedURLs []string, failures []string) string {
	if len(failures) == 0 {
		return ""
	}
	if len(visitedURLs) == 0 {
		return failures[0]
	}
	return "partial_failure"
}

func partialErrorMessage(visitedURLs []string, failures []string) string {
	if len(failures) == 0 {
		return ""
	}
	if len(visitedURLs) == 0 {
		return "No requested pages were collected."
	}
	return fmt.Sprintf("%d requested page(s) were skipped or failed.", len(failures))
}

func (worker Worker) screenshotPath(rawURL string, index int) (string, error) {
	root, err := worker.artifactRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "screenshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := sanitizeArtifactName(rawURL)
	if name == "" {
		name = "page"
	}
	return filepath.Join(dir, fmt.Sprintf("%02d-%s-%d.png", index+1, name, time.Now().UnixNano())), nil
}

func metadataPathForScreenshot(screenshotPath string) string {
	return screenshotPath + ".metadata.json"
}

func writeScreenshotMetadata(screenshotPath string, metadata ArtifactMetadata) error {
	raw, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metadataPathForScreenshot(screenshotPath), raw, 0o644)
}

func (worker Worker) artifactRoot() (string, error) {
	root := strings.TrimSpace(worker.ArtifactRoot)
	if root == "" {
		root = strings.TrimSpace(os.Getenv("ODIN_HUGINN_BROWSER_ARTIFACT_ROOT"))
	}
	if root == "" {
		if odinRoot := strings.TrimSpace(os.Getenv("ODIN_ROOT")); odinRoot != "" {
			root = filepath.Join(odinRoot, "artifacts", "huginn-browser")
		}
	}
	if root == "" {
		root = filepath.Join(".odin", "huginn-browser")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func sanitizeArtifactName(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil {
		return "page"
	}
	value := strings.TrimSpace(parsed.Hostname() + parsed.EscapedPath())
	value = strings.Trim(value, "/")
	if value == "" {
		value = "page"
	}
	return artifactNameUnsafeRegexp.ReplaceAllString(value, "-")
}

func CleanupBrowserArtifacts(options CleanupOptions) (CleanupResult, error) {
	if options.OlderThan <= 0 {
		return CleanupResult{}, fmt.Errorf("older-than duration is required")
	}
	worker := Worker{ArtifactRoot: options.ArtifactRoot}
	root, err := worker.artifactRoot()
	if err != nil {
		return CleanupResult{}, err
	}
	screenshotsDir := filepath.Join(root, "screenshots")
	now := options.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := CleanupResult{DryRun: !options.Apply}
	if _, err := os.Stat(screenshotsDir); os.IsNotExist(err) {
		return result, nil
	} else if err != nil {
		return CleanupResult{}, err
	}
	err = filepath.WalkDir(screenshotsDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !isHuginnBrowserArtifactFile(path) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if now.Sub(info.ModTime()) < options.OlderThan {
			return nil
		}
		result.Candidates = append(result.Candidates, path)
		if options.Apply {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			result.Deleted = append(result.Deleted, path)
		}
		return nil
	})
	sort.Strings(result.Candidates)
	sort.Strings(result.Deleted)
	return result, err
}

func isHuginnBrowserArtifactFile(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".png") || strings.HasSuffix(base, ".png.metadata.json")
}

func normalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "fetch":
		return "fetch"
	case "browser":
		return "browser"
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

type chromeBrowserRunner struct{}

func (chromeBrowserRunner) Open(ctx context.Context, rawURL string, options BrowserOptions) (BrowserPage, error) {
	binary, err := findChromeBinary()
	if err != nil {
		return BrowserPage{}, err
	}
	userDataDir, err := os.MkdirTemp("", "odin-huginn-browser-profile-*")
	if err != nil {
		return BrowserPage{}, err
	}
	defer os.RemoveAll(userDataDir)

	args := []string{
		"--headless=new",
		"--disable-background-networking",
		"--disable-default-apps",
		"--disable-extensions",
		"--disable-gpu",
		"--disable-sync",
		"--no-default-browser-check",
		"--no-first-run",
		"--disable-features=Translate,AutofillServerCommunication",
		"--user-data-dir=" + userDataDir,
		"--virtual-time-budget=" + fmt.Sprintf("%d", virtualTimeBudgetMillis(options.MaxDurationSeconds)),
		"--dump-dom",
	}
	if options.CaptureScreenshot {
		if strings.TrimSpace(options.ScreenshotPath) == "" {
			return BrowserPage{}, fmt.Errorf("screenshot path is required")
		}
		args = append(args, "--screenshot="+options.ScreenshotPath)
	}
	args = append(args, rawURL)
	command := exec.CommandContext(ctx, binary, args...)
	output, err := command.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return BrowserPage{}, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return BrowserPage{}, err
	}
	page := BrowserPage{URL: rawURL, HTML: string(output)}
	if options.CaptureScreenshot {
		if _, err := os.Stat(options.ScreenshotPath); err != nil {
			return BrowserPage{}, fmt.Errorf("screenshot was not captured: %w", err)
		}
		page.Screenshots = []string{options.ScreenshotPath}
	}
	return page, nil
}

func findChromeBinary() (string, error) {
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			return filepath.Clean(path), nil
		}
	}
	return "", fmt.Errorf("chromium-compatible browser is not available")
}

func virtualTimeBudgetMillis(maxDurationSeconds int) int {
	if maxDurationSeconds <= 0 {
		return 1000
	}
	millis := maxDurationSeconds * 1000
	if millis < 1000 {
		return 1000
	}
	if millis > 10000 {
		return 10000
	}
	return millis
}

func defaultHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func validateWorkerRequest(request adapter.Request) error {
	if strings.TrimSpace(request.Objective) == "" {
		return fmt.Errorf("objective is required")
	}
	if len(request.StartURLs) == 0 {
		return fmt.Errorf("start_urls is required")
	}
	if len(request.AllowedDomains) == 0 {
		return fmt.Errorf("allowed_domains is required")
	}
	if request.MaxPages <= 0 {
		return fmt.Errorf("max_pages must be positive")
	}
	if request.MaxDurationSeconds <= 0 {
		return fmt.Errorf("max_duration_seconds must be positive")
	}
	return validateSiteProfiles(request.SiteProfiles)
}

func validateSiteProfiles(profiles []adapter.SiteProfile) error {
	for _, profile := range profiles {
		if strings.TrimSpace(profile.Domain) == "" {
			return fmt.Errorf("site profile domain is required")
		}
		if profile.MaxPages < 0 {
			return fmt.Errorf("site profile max_pages must not be negative")
		}
		if profile.MinDelayMS < 0 {
			return fmt.Errorf("site profile min_delay_ms must not be negative")
		}
		if profile.MaxDurationSeconds < 0 {
			return fmt.Errorf("site profile max_duration_seconds must not be negative")
		}
		switch strings.ToLower(strings.TrimSpace(profile.ModeAllowed)) {
		case "", "fetch", "browser", "both":
		default:
			return fmt.Errorf("site profile mode_allowed must be fetch, browser, or both")
		}
	}
	return nil
}

func readOnlyURLHost(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil || parsed.Host == "" {
		return "", fmt.Errorf("start url %q must be an absolute URL", rawURL)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("start url %q must not include credentials", rawURL)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("start url %q must use http or https", rawURL)
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" {
		return "", fmt.Errorf("start url %q must include a hostname", rawURL)
	}
	return host, nil
}

func normalizeDomains(domains []string) []string {
	normalized := make([]string, 0, len(domains))
	for _, domain := range domains {
		if candidate := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), "."); candidate != "" {
			normalized = append(normalized, candidate)
		}
	}
	return normalized
}

func domainAllowed(host string, allowedDomains []string) bool {
	for _, domain := range allowedDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func writeResponse(stdout io.Writer, response adapter.Response) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(response)
}

func failedResponse(_ adapter.Request, code string, message string) adapter.Response {
	return adapter.Response{
		Status:               "failed",
		AdapterKind:          adapterKind,
		VisitedURLs:          []string{},
		ExtractedTextSummary: "",
		Screenshots:          []string{},
		ActionLog:            baseFailureActionLog(code),
		ErrorCode:            code,
		ErrorMessage:         message,
	}
}

func failedPageResult(url string, status string, mode string, code string, message string) adapter.PageResult {
	return adapter.PageResult{
		URL:          url,
		Status:       status,
		Mode:         mode,
		ErrorCode:    code,
		ErrorMessage: message,
	}
}

func timeoutResponse(request adapter.Request, visitedURLs []string, pageResults []adapter.PageResult, actionLog []string) adapter.Response {
	return adapter.Response{
		Status:               "timeout",
		AdapterKind:          adapterKind,
		VisitedURLs:          append([]string{}, visitedURLs...),
		PageResults:          completeMissingPageResults(request, pageResults, "failed", "worker_timeout", "Worker exceeded max_duration_seconds before completing evidence collection."),
		ExtractedTextSummary: "",
		Screenshots:          []string{},
		ActionLog:            append(append([]string{}, actionLog...), "worker_timeout"),
		ErrorCode:            "worker_timeout",
		ErrorMessage:         "Worker exceeded max_duration_seconds before completing evidence collection.",
	}
}

func completeMissingPageResults(request adapter.Request, pageResults []adapter.PageResult, status string, code string, message string) []adapter.PageResult {
	completed := append([]adapter.PageResult{}, pageResults...)
	seen := make(map[string]bool, len(completed))
	for _, result := range completed {
		seen[result.URL] = true
	}
	mode := normalizeMode(request.Mode)
	for _, uri := range request.StartURLs {
		if seen[uri] {
			continue
		}
		completed = append(completed, failedPageResult(uri, status, mode, code, message))
	}
	return completed
}

func baseFailureActionLog(code string) []string {
	return []string{
		"live_adapter_selected",
		"validated_read_only_request",
		"no_login_or_session_handling",
		"no_form_submission",
		"no_external_mutation_performed",
		code,
	}
}

func summarizePage(body []byte) string {
	text := string(body)
	title := pageTitle(body)
	withoutExecutableBlocks := scriptStyleRegexp.ReplaceAllString(text, " ")
	plain := strings.Join(strings.Fields(htmlText(stripTagsRegexp.ReplaceAllString(withoutExecutableBlocks, " "))), " ")
	if len(plain) > defaultSummaryMax {
		plain = strings.TrimSpace(plain[:defaultSummaryMax])
	}
	if title != "" && plain != "" {
		return title + ": " + plain
	}
	if title != "" {
		return title
	}
	return plain
}

func pageTitle(body []byte) string {
	if matches := titleRegexp.FindStringSubmatch(string(body)); len(matches) == 2 {
		return strings.TrimSpace(htmlText(matches[1]))
	}
	return ""
}

func htmlText(value string) string {
	replacements := []struct {
		from string
		to   string
	}{
		{from: "&amp;", to: "&"},
		{from: "&lt;", to: "<"},
		{from: "&gt;", to: ">"},
		{from: "&quot;", to: `"`},
		{from: "&#39;", to: "'"},
	}
	for _, replacement := range replacements {
		value = strings.ReplaceAll(value, replacement.from, replacement.to)
	}
	return value
}

func nonEmptyStrings(values []string) []string {
	nonEmpty := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			nonEmpty = append(nonEmpty, trimmed)
		}
	}
	return nonEmpty
}

var (
	titleRegexp              = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	scriptStyleRegexp        = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	stripTagsRegexp          = regexp.MustCompile(`(?is)<[^>]+>`)
	artifactNameUnsafeRegexp = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
)
