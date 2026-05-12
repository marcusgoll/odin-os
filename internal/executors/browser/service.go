package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"

	"odin-os/internal/adapters/huginnbrowser"
	"odin-os/internal/store/sqlite"
)

const (
	EvidenceType             = "browser_readonly"
	MaxPagesLimit            = 20
	MaxDurationSecondsLimit  = 300
	defaultEvidenceCreatedBy = "browser_executor"
)

type ReadOnlyTask struct {
	GoalID             int64                       `json:"goal_id"`
	WorkerMode         string                      `json:"worker_mode,omitempty"`
	Objective          string                      `json:"objective"`
	AllowedDomains     []string                    `json:"allowed_domains"`
	StartURLs          []string                    `json:"start_urls"`
	MaxPages           int                         `json:"max_pages"`
	MaxDurationSeconds int                         `json:"max_duration_seconds"`
	EvidenceRequired   bool                        `json:"evidence_required"`
	SiteProfiles       []huginnbrowser.SiteProfile `json:"site_profiles,omitempty"`
	BrowserSessionID   int64                       `json:"browser_session_id,omitempty"`
	BrowserSession     *BrowserSessionReference    `json:"browser_session,omitempty"`
	Actions            []string                    `json:"actions,omitempty"`
}

type PageResult = huginnbrowser.PageResult
type BrowserSessionReference = huginnbrowser.BrowserSessionReference

type Result struct {
	Status               string                      `json:"status"`
	GoalID               int64                       `json:"goal_id"`
	EvidenceID           int64                       `json:"evidence_id"`
	EvidenceType         string                      `json:"evidence_type"`
	AdapterStatus        string                      `json:"adapter_status,omitempty"`
	AdapterKind          string                      `json:"adapter_kind,omitempty"`
	StartURLs            []string                    `json:"start_urls"`
	AllowedDomains       []string                    `json:"allowed_domains"`
	MaxPages             int                         `json:"max_pages"`
	MaxDurationSeconds   int                         `json:"max_duration_seconds"`
	SiteProfiles         []huginnbrowser.SiteProfile `json:"site_profiles,omitempty"`
	BrowserSession       *BrowserSessionReference    `json:"browser_session,omitempty"`
	VisitedURLs          []string                    `json:"visited_urls,omitempty"`
	PageResults          []huginnbrowser.PageResult  `json:"page_results,omitempty"`
	ExtractedTextSummary string                      `json:"extracted_text_summary,omitempty"`
	Screenshots          []string                    `json:"screenshots,omitempty"`
	ActionLog            []string                    `json:"action_log,omitempty"`
	ErrorCode            string                      `json:"error_code,omitempty"`
	ErrorMessage         string                      `json:"error_message,omitempty"`
	Evidence             sqlite.GoalEvidence         `json:"-"`
}

type ReadOnlyRunner interface {
	Run(context.Context, ReadOnlyTask) (Result, error)
}

type Service struct {
	Store   *sqlite.Store
	Adapter huginnbrowser.Adapter
}

func (service Service) Run(ctx context.Context, task ReadOnlyTask) (Result, error) {
	if service.Store == nil {
		return Result{}, fmt.Errorf("browser executor requires store")
	}
	if err := ValidateReadOnlyTask(task); err != nil {
		return Result{}, err
	}
	resolvedTask := task
	sessionRef, err := service.resolveBrowserSession(ctx, task)
	if err != nil {
		return Result{}, err
	}
	resolvedTask.BrowserSession = sessionRef
	adapter := service.Adapter
	if adapter == nil {
		adapter = huginnbrowser.SelectAdapterFromEnv()
	}
	adapterResponse, err := adapter.Run(ctx, huginnbrowser.Request{
		GoalID:             resolvedTask.GoalID,
		Mode:               resolvedTask.WorkerMode,
		Objective:          resolvedTask.Objective,
		StartURLs:          append([]string{}, resolvedTask.StartURLs...),
		AllowedDomains:     append([]string{}, resolvedTask.AllowedDomains...),
		MaxPages:           resolvedTask.MaxPages,
		MaxDurationSeconds: resolvedTask.MaxDurationSeconds,
		EvidenceRequired:   resolvedTask.EvidenceRequired,
		SiteProfiles:       append([]huginnbrowser.SiteProfile{}, resolvedTask.SiteProfiles...),
		BrowserSession:     cloneBrowserSessionReference(resolvedTask.BrowserSession),
	})
	if err != nil {
		return Result{
			Status:       "failed",
			GoalID:       resolvedTask.GoalID,
			ErrorCode:    "adapter_failed",
			ErrorMessage: err.Error(),
		}, fmt.Errorf("browser adapter failed: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"executor": "browser_readonly",
		"status":   "adapter_response_recorded",
		"task":     resolvedTask,
		"adapter":  adapterResponse,
	})
	if err != nil {
		return Result{}, err
	}
	evidence, err := service.Store.AddGoalEvidence(ctx, sqlite.AddGoalEvidenceParams{
		GoalID:       resolvedTask.GoalID,
		EvidenceType: EvidenceType,
		Summary:      defaultEvidenceSummary(adapterResponse),
		URI:          defaultEvidenceURI(resolvedTask, adapterResponse),
		PayloadJSON:  string(payload),
		CreatedBy:    defaultEvidenceCreatedBy,
	})
	if err != nil {
		return Result{}, err
	}
	return Result{
		Status:               "recorded",
		GoalID:               resolvedTask.GoalID,
		EvidenceID:           evidence.ID,
		EvidenceType:         evidence.EvidenceType,
		AdapterStatus:        adapterResponse.Status,
		AdapterKind:          adapterResponse.AdapterKind,
		StartURLs:            append([]string{}, resolvedTask.StartURLs...),
		AllowedDomains:       append([]string{}, resolvedTask.AllowedDomains...),
		MaxPages:             resolvedTask.MaxPages,
		MaxDurationSeconds:   resolvedTask.MaxDurationSeconds,
		SiteProfiles:         append([]huginnbrowser.SiteProfile{}, resolvedTask.SiteProfiles...),
		BrowserSession:       cloneBrowserSessionReference(resolvedTask.BrowserSession),
		VisitedURLs:          append([]string{}, adapterResponse.VisitedURLs...),
		PageResults:          append([]huginnbrowser.PageResult{}, adapterResponse.PageResults...),
		ExtractedTextSummary: adapterResponse.ExtractedTextSummary,
		Screenshots:          append([]string{}, adapterResponse.Screenshots...),
		ActionLog:            append([]string{}, adapterResponse.ActionLog...),
		Evidence:             evidence,
	}, nil
}

func (service Service) resolveBrowserSession(ctx context.Context, task ReadOnlyTask) (*BrowserSessionReference, error) {
	if task.BrowserSessionID == 0 {
		return cloneBrowserSessionReference(task.BrowserSession), nil
	}
	session, err := service.Store.GetBrowserSession(ctx, task.BrowserSessionID)
	if err != nil {
		return nil, err
	}
	if session.Status != sqlite.BrowserSessionStatusVerified {
		return nil, fmt.Errorf("browser session %d must be verified before read-only browser use; status=%s", session.ID, session.Status)
	}
	if err := ensureBrowserSessionDomainMatchesTask(session, task); err != nil {
		return nil, err
	}
	ref := browserSessionReference(session)
	return &ref, nil
}

func ensureBrowserSessionDomainMatchesTask(session sqlite.BrowserSession, task ReadOnlyTask) error {
	allowedDomains, err := normalizeAllowedDomains(task.AllowedDomains)
	if err != nil {
		return err
	}
	sessionDomain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(session.Domain)), ".")
	if sessionDomain == "" {
		return fmt.Errorf("browser session domain is required")
	}
	if !domainAllowed(sessionDomain, allowedDomains) {
		return fmt.Errorf("browser session domain %q is not in allowed_domains", sessionDomain)
	}
	for _, rawURL := range task.StartURLs {
		host, err := readOnlyURLHost(rawURL)
		if err != nil {
			return err
		}
		if !domainAllowed(host, []string{sessionDomain}) {
			return fmt.Errorf("browser session domain %q cannot be used for start url host %q", sessionDomain, host)
		}
	}
	return nil
}

func browserSessionReference(session sqlite.BrowserSession) BrowserSessionReference {
	ref := BrowserSessionReference{
		ID:                   session.ID,
		Domain:               session.Domain,
		Status:               string(session.Status),
		PermissionTier:       string(session.PermissionTier),
		ProfileStoragePolicy: string(session.ProfileStoragePolicy),
		ProfilePath:          session.ProfilePath,
	}
	if session.LastVerifiedAt != nil {
		ref.LastVerifiedAt = session.LastVerifiedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return ref
}

func cloneBrowserSessionReference(ref *BrowserSessionReference) *BrowserSessionReference {
	if ref == nil {
		return nil
	}
	clone := *ref
	return &clone
}

func defaultEvidenceSummary(response huginnbrowser.Response) string {
	if strings.TrimSpace(response.ExtractedTextSummary) != "" {
		return response.ExtractedTextSummary
	}
	return "read-only browser task produced stub/local evidence"
}

func defaultEvidenceURI(task ReadOnlyTask, response huginnbrowser.Response) string {
	for _, uri := range response.VisitedURLs {
		if strings.TrimSpace(uri) != "" {
			return strings.TrimSpace(uri)
		}
	}
	return task.StartURLs[0]
}

func ValidateReadOnlyTask(task ReadOnlyTask) error {
	if task.GoalID <= 0 {
		return fmt.Errorf("goal_id must be positive")
	}
	if strings.TrimSpace(task.Objective) == "" {
		return fmt.Errorf("objective is required")
	}
	if len(task.AllowedDomains) == 0 {
		return fmt.Errorf("allowed_domains is required")
	}
	if len(task.StartURLs) == 0 {
		return fmt.Errorf("start_urls is required")
	}
	if task.MaxPages <= 0 || task.MaxPages > MaxPagesLimit {
		return fmt.Errorf("max_pages must be between 1 and %d", MaxPagesLimit)
	}
	if task.MaxDurationSeconds <= 0 || task.MaxDurationSeconds > MaxDurationSecondsLimit {
		return fmt.Errorf("max_duration_seconds must be between 1 and %d", MaxDurationSecondsLimit)
	}
	if task.BrowserSessionID < 0 {
		return fmt.Errorf("browser_session_id must not be negative")
	}
	if task.BrowserSession != nil && task.BrowserSession.ID <= 0 {
		return fmt.Errorf("browser session id must be positive")
	}
	allowedDomains, err := normalizeAllowedDomains(task.AllowedDomains)
	if err != nil {
		return err
	}
	for _, action := range task.Actions {
		if !isReadOnlyAction(action) {
			return fmt.Errorf("mutation action %q is not allowed for read-only browser tasks", action)
		}
	}
	for _, profile := range task.SiteProfiles {
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
	for _, rawURL := range task.StartURLs {
		host, err := readOnlyURLHost(rawURL)
		if err != nil {
			return err
		}
		if !domainAllowed(host, allowedDomains) {
			return fmt.Errorf("disallowed domain %q for read-only browser task", host)
		}
	}
	return nil
}

func normalizeAllowedDomains(domains []string) ([]string, error) {
	normalized := make([]string, 0, len(domains))
	for _, domain := range domains {
		candidate := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
		if candidate == "" {
			return nil, fmt.Errorf("allowed domain is required")
		}
		if strings.Contains(candidate, "/") || strings.Contains(candidate, ":") {
			return nil, fmt.Errorf("allowed domain %q must be a hostname", domain)
		}
		normalized = append(normalized, candidate)
	}
	return normalized, nil
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
	host := parsed.Hostname()
	if host == "" {
		host = parsed.Host
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if ip := net.ParseIP(host); ip != nil {
		return "", fmt.Errorf("start url %q must use a hostname, not an IP address", rawURL)
	}
	return host, nil
}

func domainAllowed(host string, allowedDomains []string) bool {
	for _, domain := range allowedDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func isReadOnlyAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", "read", "navigate", "snapshot", "extract":
		return true
	default:
		return false
	}
}
