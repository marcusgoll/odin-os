package socialcopilot

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	SectionMarcusOwnedSurfaces = "marcus_owned_surfaces"
	SectionExplicitTargetURLs  = "explicit_target_urls"
	SectionWatchlistEntries    = "operator_maintained_watchlist_entries"

	TargetKindMarcusOwnTimeline = "marcus_own_timeline"
	TargetKindMarcusOwnMentions = "marcus_own_mentions"
	TargetKindXPost             = "x_post"
	TargetKindXThread           = "x_thread"
	TargetKindXAccount          = "x_account"
)

type WatchScopeInput struct {
	MarcusOwnedSurfaces []string
	ExplicitTargetURLs  []string
	WatchlistEntries    []WatchlistEntryInput
}

type WatchlistEntryInput struct {
	Kind   string
	Target string
	Label  string
	Reason string
	Notes  string
}

type WatchScope struct {
	Targets []WatchTarget
}

type WatchTarget struct {
	Section      string
	Kind         string
	StableKey    string
	CanonicalURL string
	Label        string
	Reason       string
	Notes        string
}

func NormalizeWatchScope(input WatchScopeInput) (WatchScope, error) {
	var scope WatchScope
	seenIdentity := map[string]string{}

	add := func(target WatchTarget, identityKey string) error {
		if existing, ok := seenIdentity[identityKey]; ok {
			return fmt.Errorf("duplicate watched target %q already represented by %s", identityKey, existing)
		}
		seenIdentity[identityKey] = target.StableKey
		scope.Targets = append(scope.Targets, target)
		return nil
	}

	for _, surface := range input.MarcusOwnedSurfaces {
		target, err := normalizeMarcusOwnedSurface(surface)
		if err != nil {
			return WatchScope{}, err
		}
		if err := add(target, target.StableKey); err != nil {
			return WatchScope{}, err
		}
	}

	for _, raw := range input.ExplicitTargetURLs {
		if isReservedMarcusOwnInput(raw) {
			return WatchScope{}, fmt.Errorf("reserved Marcus-owned target %q cannot be operator-entered", strings.TrimSpace(raw))
		}
		status, err := normalizeXStatusTarget(raw)
		if err != nil {
			return WatchScope{}, fmt.Errorf("explicit target URL %q: %w", strings.TrimSpace(raw), err)
		}
		target := WatchTarget{
			Section:      SectionExplicitTargetURLs,
			Kind:         TargetKindXPost,
			StableKey:    "x_post:" + status.StatusID,
			CanonicalURL: status.CanonicalURL,
		}
		if err := add(target, "x_status:"+status.StatusID); err != nil {
			return WatchScope{}, err
		}
	}

	for _, entry := range input.WatchlistEntries {
		target, identityKey, err := normalizeWatchlistEntry(entry)
		if err != nil {
			return WatchScope{}, err
		}
		if err := add(target, identityKey); err != nil {
			return WatchScope{}, err
		}
	}

	return scope, nil
}

func normalizeMarcusOwnedSurface(raw string) (WatchTarget, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "timeline", TargetKindMarcusOwnTimeline:
		return WatchTarget{
			Section:   SectionMarcusOwnedSurfaces,
			Kind:      TargetKindMarcusOwnTimeline,
			StableKey: TargetKindMarcusOwnTimeline,
		}, nil
	case "mentions", TargetKindMarcusOwnMentions:
		return WatchTarget{
			Section:   SectionMarcusOwnedSurfaces,
			Kind:      TargetKindMarcusOwnMentions,
			StableKey: TargetKindMarcusOwnMentions,
		}, nil
	default:
		return WatchTarget{}, fmt.Errorf("unsupported Marcus-owned surface %q", strings.TrimSpace(raw))
	}
}

func normalizeWatchlistEntry(entry WatchlistEntryInput) (WatchTarget, string, error) {
	if isReservedMarcusOwnInput(entry.Target) {
		return WatchTarget{}, "", fmt.Errorf("reserved Marcus-owned target %q cannot be operator-entered", strings.TrimSpace(entry.Target))
	}

	base := WatchTarget{
		Section: SectionWatchlistEntries,
		Label:   strings.TrimSpace(entry.Label),
		Reason:  strings.TrimSpace(entry.Reason),
		Notes:   strings.TrimSpace(entry.Notes),
	}

	switch strings.ToLower(strings.TrimSpace(entry.Kind)) {
	case "account":
		account, err := normalizeXAccountTarget(entry.Target)
		if err != nil {
			return WatchTarget{}, "", fmt.Errorf("watchlist account %q: %w", strings.TrimSpace(entry.Target), err)
		}
		base.Kind = TargetKindXAccount
		base.StableKey = "x_account:" + account.Handle
		base.CanonicalURL = account.CanonicalURL
		return base, base.StableKey, nil
	case "thread":
		status, err := normalizeXStatusTarget(entry.Target)
		if err != nil {
			return WatchTarget{}, "", fmt.Errorf("watchlist thread %q: %w", strings.TrimSpace(entry.Target), err)
		}
		base.Kind = TargetKindXThread
		base.StableKey = "x_thread:" + status.StatusID
		base.CanonicalURL = status.CanonicalURL
		return base, "x_status:" + status.StatusID, nil
	default:
		return WatchTarget{}, "", fmt.Errorf("unsupported watchlist entry kind %q", strings.TrimSpace(entry.Kind))
	}
}

type normalizedXStatus struct {
	Handle       string
	StatusID     string
	CanonicalURL string
}

func normalizeXStatusTarget(raw string) (normalizedXStatus, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return normalizedXStatus{}, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return normalizedXStatus{}, fmt.Errorf("scheme must be http or https")
	}
	if !isXHost(parsed.Hostname()) {
		return normalizedXStatus{}, fmt.Errorf("host must be x.com or twitter.com")
	}

	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) < 3 || strings.ToLower(parts[len(parts)-2]) != "status" {
		return normalizedXStatus{}, fmt.Errorf("path must include /<screen_name>/status/<status_id>")
	}
	handle, err := url.PathUnescape(parts[len(parts)-3])
	if err != nil {
		return normalizedXStatus{}, err
	}
	statusID, err := url.PathUnescape(parts[len(parts)-1])
	if err != nil {
		return normalizedXStatus{}, err
	}
	handle = normalizeHandle(handle)
	if handle == "" {
		return normalizedXStatus{}, fmt.Errorf("screen name is required")
	}
	if _, err := strconv.ParseInt(statusID, 10, 64); err != nil {
		return normalizedXStatus{}, fmt.Errorf("status id must be numeric")
	}

	return normalizedXStatus{
		Handle:       handle,
		StatusID:     statusID,
		CanonicalURL: fmt.Sprintf("https://x.com/%s/status/%s", handle, statusID),
	}, nil
}

type normalizedXAccount struct {
	Handle       string
	CanonicalURL string
}

func normalizeXAccountTarget(raw string) (normalizedXAccount, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return normalizedXAccount{}, fmt.Errorf("account target is required")
	}

	if strings.HasPrefix(trimmed, "@") {
		handle := normalizeHandle(strings.TrimPrefix(trimmed, "@"))
		if handle == "" {
			return normalizedXAccount{}, fmt.Errorf("account handle is required")
		}
		return normalizedXAccount{
			Handle:       handle,
			CanonicalURL: "https://x.com/" + handle,
		}, nil
	}

	parsed, err := url.Parse(trimmed)
	if err == nil && parsed.Scheme != "" {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return normalizedXAccount{}, fmt.Errorf("scheme must be http or https")
		}
		if !isXHost(parsed.Hostname()) {
			return normalizedXAccount{}, fmt.Errorf("host must be x.com or twitter.com")
		}
		parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
		if len(parts) != 1 {
			return normalizedXAccount{}, fmt.Errorf("profile URL must contain only the account handle")
		}
		handle, err := url.PathUnescape(parts[0])
		if err != nil {
			return normalizedXAccount{}, err
		}
		handle = normalizeHandle(handle)
		if handle == "" {
			return normalizedXAccount{}, fmt.Errorf("account handle is required")
		}
		return normalizedXAccount{
			Handle:       handle,
			CanonicalURL: "https://x.com/" + handle,
		}, nil
	}

	handle := normalizeHandle(trimmed)
	if handle == "" {
		return normalizedXAccount{}, fmt.Errorf("account handle is required")
	}
	return normalizedXAccount{
		Handle:       handle,
		CanonicalURL: "https://x.com/" + handle,
	}, nil
}

func isReservedMarcusOwnInput(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "marcus_own_")
}

func isXHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "x.com", "www.x.com", "twitter.com", "www.twitter.com":
		return true
	default:
		return false
	}
}

func normalizeHandle(raw string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(raw, "@")))
}
