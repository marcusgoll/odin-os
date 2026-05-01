package supervision

import "testing"

func TestEligibilityRequiresBothStage7Labels(t *testing.T) {
	config := DefaultConfig()

	tests := []struct {
		name   string
		labels []string
	}{
		{name: "missing ready", labels: []string{"safety:low-risk"}},
		{name: "missing low risk", labels: []string{"odin:ready"}},
		{name: "missing both", labels: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateIssue(config, Issue{
				Repo:         "marcusgoll/odin-os",
				Number:       12,
				Labels:       tt.labels,
				ChangedPaths: []string{"docs/stage7.md"},
			})

			if got.Eligible {
				t.Fatalf("EvaluateIssue().Eligible = true, want false")
			}
			if got.RefusalReason != RefusalMissingRequiredLabel {
				t.Fatalf("EvaluateIssue().RefusalReason = %q, want %q", got.RefusalReason, RefusalMissingRequiredLabel)
			}
		})
	}
}

func TestEligibilityAllowsReviewedLowRiskContentScopes(t *testing.T) {
	config := DefaultConfig()

	tests := []struct {
		name string
		path string
	}{
		{name: "docs", path: "docs/plans/stage7.md"},
		{name: "prompts", path: "prompts/reviewer.md"},
		{name: "fixtures", path: "fixtures/stage7/sample.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateIssue(config, eligibleIssue(tt.path))
			if !got.Eligible {
				t.Fatalf("EvaluateIssue(%q) = refused %q, want eligible", tt.path, got.RefusalReason)
			}
		})
	}
}

func TestEligibilityAllowsNonSensitiveTests(t *testing.T) {
	got := EvaluateIssue(DefaultConfig(), eligibleIssue("internal/runtime/supervision/service_test.go"))
	if !got.Eligible {
		t.Fatalf("EvaluateIssue(non-sensitive test) = refused %q, want eligible", got.RefusalReason)
	}
}

func TestEligibilityRefusesForbiddenPaths(t *testing.T) {
	got := EvaluateIssue(DefaultConfig(), eligibleIssue(".github/workflows/deploy-production.yml"))
	if got.Eligible {
		t.Fatalf("EvaluateIssue(forbidden path).Eligible = true, want false")
	}
	if got.RefusalReason != RefusalForbiddenPath {
		t.Fatalf("EvaluateIssue().RefusalReason = %q, want %q", got.RefusalReason, RefusalForbiddenPath)
	}
}

func TestEligibilityRefusesTestsUnderForbiddenPackages(t *testing.T) {
	got := EvaluateIssue(DefaultConfig(), eligibleIssue("internal/runner/codexexec/adapter_test.go"))
	if got.Eligible {
		t.Fatalf("EvaluateIssue(forbidden package test).Eligible = true, want false")
	}
	if got.RefusalReason != RefusalSensitiveTestScope {
		t.Fatalf("EvaluateIssue().RefusalReason = %q, want %q", got.RefusalReason, RefusalSensitiveTestScope)
	}
}

func TestEligibilityRefusesUnknownScope(t *testing.T) {
	got := EvaluateIssue(DefaultConfig(), Issue{
		Repo:   "marcusgoll/odin-os",
		Number: 18,
		Labels: []string{"odin:ready", "safety:low-risk"},
	})
	if got.Eligible {
		t.Fatalf("EvaluateIssue(unknown scope).Eligible = true, want false")
	}
	if got.RefusalReason != RefusalUnknownScope {
		t.Fatalf("EvaluateIssue().RefusalReason = %q, want %q", got.RefusalReason, RefusalUnknownScope)
	}
}

func eligibleIssue(path string) Issue {
	return Issue{
		Repo:         "marcusgoll/odin-os",
		Number:       17,
		Labels:       []string{"odin:ready", "safety:low-risk"},
		ChangedPaths: []string{path},
	}
}
