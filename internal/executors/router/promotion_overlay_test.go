package router

import "testing"

func TestApplyRoutingRefinementsOverridesPreferredExecutors(t *testing.T) {
	t.Parallel()

	refined, err := ApplyRoutingRefinements(Config{
		Version: 1,
		Routes: []RouteConfig{
			{
				Name:      "default",
				Preferred: []string{"codex_headless", "openai_api"},
				Fallback:  []string{"openrouter_api"},
			},
		},
	}, []RoutingRefinement{
		{
			RouteName:  "default",
			Preferred:  []string{"openai_api"},
			Fallback:   []string{"codex_headless"},
			SourceKind: "promotion",
			SourceID:   7,
		},
	})
	if err != nil {
		t.Fatalf("ApplyRoutingRefinements() error = %v", err)
	}

	if got := refined.Routes[0].Preferred; len(got) != 1 || got[0] != "openai_api" {
		t.Fatalf("Preferred = %#v, want [openai_api]", got)
	}
	if got := refined.Routes[0].Fallback; len(got) != 1 || got[0] != "codex_headless" {
		t.Fatalf("Fallback = %#v, want [codex_headless]", got)
	}
}
