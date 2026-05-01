package supervision

import (
	"path/filepath"
	"strings"
)

func EvaluateIssue(config Config, issue Issue) Eligibility {
	result := Eligibility{
		Labels:       append([]string(nil), issue.Labels...),
		ChangedPaths: append([]string(nil), issue.ChangedPaths...),
	}

	labels := make(map[string]bool, len(issue.Labels))
	for _, label := range issue.Labels {
		labels[label] = true
	}
	for _, required := range config.RequiredLabels {
		if !labels[required] {
			result.RefusalReason = RefusalMissingRequiredLabel
			return result
		}
	}

	if len(issue.ChangedPaths) == 0 {
		result.RefusalReason = RefusalUnknownScope
		return result
	}
	for _, rawPath := range issue.ChangedPaths {
		reason := evaluatePath(config, rawPath)
		if reason != "" {
			result.RefusalReason = reason
			return result
		}
	}

	result.Eligible = true
	return result
}

func evaluatePath(config Config, rawPath string) string {
	path := normalizedPath(rawPath)
	if path == "" || path == "." {
		return RefusalUnknownScope
	}

	if isTestPath(path) {
		if hasPrefix(path, config.ForbiddenPathPrefixes) || containsAny(path, config.SensitiveTestSubstrings) {
			return RefusalSensitiveTestScope
		}
		return ""
	}

	if hasPrefix(path, config.ForbiddenPathPrefixes) {
		return RefusalForbiddenPath
	}
	if hasPrefix(path, config.AllowedPathPrefixes) {
		return ""
	}
	return RefusalUnknownScope
}

func normalizedPath(value string) string {
	cleaned := filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
	cleaned = strings.TrimPrefix(cleaned, "./")
	if cleaned == "/" {
		return ""
	}
	return cleaned
}

func isTestPath(path string) bool {
	return strings.HasSuffix(path, "_test.go")
}

func hasPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) || path == strings.TrimSuffix(prefix, "/") {
			return true
		}
	}
	return false
}

func containsAny(path string, values []string) bool {
	for _, value := range values {
		if strings.Contains(path, value) {
			return true
		}
	}
	return false
}
