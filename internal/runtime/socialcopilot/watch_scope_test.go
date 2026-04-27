package socialcopilot

import (
	"slices"
	"testing"
)

func TestWatchScopeNormalizesExplicitXPostURLsByStatusID(t *testing.T) {
	scope, err := NormalizeWatchScope(WatchScopeInput{
		MarcusOwnedSurfaces: []string{"timeline", "mentions"},
		ExplicitTargetURLs: []string{
			"https://twitter.com/Example/status/12345?s=20#frag",
		},
		WatchlistEntries: []WatchlistEntryInput{{
			Kind:   "account",
			Target: "@AviationDaily",
		}},
	})
	if err != nil {
		t.Fatalf("NormalizeWatchScope() error = %v", err)
	}

	wantKeys := []string{
		"marcus_own_timeline",
		"marcus_own_mentions",
		"x_post:12345",
		"x_account:aviationdaily",
	}
	for _, want := range wantKeys {
		if !slices.Contains(stableKeys(scope), want) {
			t.Fatalf("stable keys = %v, want %q", stableKeys(scope), want)
		}
	}

	post := targetByStableKey(t, scope, "x_post:12345")
	if post.CanonicalURL != "https://x.com/example/status/12345" {
		t.Fatalf("post.CanonicalURL = %q, want canonical x.com status URL", post.CanonicalURL)
	}

	account := targetByStableKey(t, scope, "x_account:aviationdaily")
	if account.CanonicalURL != "https://x.com/aviationdaily" {
		t.Fatalf("account.CanonicalURL = %q, want canonical x.com profile URL", account.CanonicalURL)
	}
}

func TestWatchScopeNormalizesWatchlistThreadByRootStatusID(t *testing.T) {
	scope, err := NormalizeWatchScope(WatchScopeInput{
		WatchlistEntries: []WatchlistEntryInput{{
			Kind:   "thread",
			Target: "https://x.com/FlightSchool/status/998877?ref=share",
			Label:  "training-thread",
		}},
	})
	if err != nil {
		t.Fatalf("NormalizeWatchScope() error = %v", err)
	}

	thread := targetByStableKey(t, scope, "x_thread:998877")
	if thread.CanonicalURL != "https://x.com/flightschool/status/998877" {
		t.Fatalf("thread.CanonicalURL = %q, want canonical x.com status URL", thread.CanonicalURL)
	}
	if thread.Label != "training-thread" {
		t.Fatalf("thread.Label = %q, want label carried forward", thread.Label)
	}
}

func TestWatchScopeRejectsDuplicateStableTargetAcrossSections(t *testing.T) {
	_, err := NormalizeWatchScope(WatchScopeInput{
		ExplicitTargetURLs: []string{
			"https://x.com/Example/status/12345",
		},
		WatchlistEntries: []WatchlistEntryInput{{
			Kind:   "thread",
			Target: "https://twitter.com/example/status/12345?s=20",
		}},
	})
	if err == nil {
		t.Fatalf("NormalizeWatchScope() error = nil, want duplicate stable target rejection")
	}
}

func TestWatchScopeRejectsReservedMarcusOwnKeysOutsideBuiltins(t *testing.T) {
	cases := []WatchScopeInput{
		{
			ExplicitTargetURLs: []string{"marcus_own_timeline"},
		},
		{
			WatchlistEntries: []WatchlistEntryInput{{
				Kind:   "account",
				Target: "marcus_own_mentions",
			}},
		},
		{
			WatchlistEntries: []WatchlistEntryInput{{
				Kind:   "thread",
				Target: "marcus_own_custom",
			}},
		},
	}

	for _, input := range cases {
		if _, err := NormalizeWatchScope(input); err == nil {
			t.Fatalf("NormalizeWatchScope(%+v) error = nil, want reserved key rejection", input)
		}
	}
}

func TestWatchScopeRejectsUnknownSectionKinds(t *testing.T) {
	_, err := NormalizeWatchScope(WatchScopeInput{
		MarcusOwnedSurfaces: []string{"likes"},
	})
	if err == nil {
		t.Fatalf("NormalizeWatchScope() error = nil, want unknown Marcus-owned surface rejection")
	}

	_, err = NormalizeWatchScope(WatchScopeInput{
		WatchlistEntries: []WatchlistEntryInput{{
			Kind:   "hashtag",
			Target: "#aviation",
		}},
	})
	if err == nil {
		t.Fatalf("NormalizeWatchScope() error = nil, want unknown watchlist entry kind rejection")
	}
}

func stableKeys(scope WatchScope) []string {
	keys := make([]string, 0, len(scope.Targets))
	for _, target := range scope.Targets {
		keys = append(keys, target.StableKey)
	}
	return keys
}

func targetByStableKey(t *testing.T, scope WatchScope, stableKey string) WatchTarget {
	t.Helper()
	for _, target := range scope.Targets {
		if target.StableKey == stableKey {
			return target
		}
	}
	t.Fatalf("stable key %q not found in %+v", stableKey, scope.Targets)
	return WatchTarget{}
}
