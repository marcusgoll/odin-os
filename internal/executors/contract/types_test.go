package contract

import "testing"

func TestCapabilitiesMatchTaskRequirements(t *testing.T) {
	t.Parallel()

	spec := TaskSpec{
		Kind:  TaskKindBuild,
		Scope: "project",
		Requirements: Requirements{
			AllowedClasses:    []ExecutorClass{ExecutorClassPlanBackedCLI},
			NeedsResume:       true,
			NeedsCancel:       true,
			NeedsTools:        true,
			NeedsHeadlessPlan: true,
		},
	}

	caps := Capabilities{
		ExecutorClass:        ExecutorClassPlanBackedCLI,
		SupportsResume:       true,
		SupportsCancel:       true,
		SupportsTools:        true,
		SupportsHeadlessPlan: true,
		TaskKinds:            []TaskKind{TaskKindBuild, TaskKindReview},
		Scopes:               []string{"project", "odin-core"},
	}

	if !caps.Matches(spec) {
		t.Fatalf("Matches() = false, want true")
	}
}

func TestCapabilitiesRejectUnsupportedClassAndFeatures(t *testing.T) {
	t.Parallel()

	spec := TaskSpec{
		Kind:  TaskKindResearch,
		Scope: "global",
		Requirements: Requirements{
			AllowedClasses:      []ExecutorClass{ExecutorClassAPI},
			NeedsCostEstimate:   true,
			NeedsBrokerFallback: true,
		},
	}

	caps := Capabilities{
		ExecutorClass:        ExecutorClassPlanBackedCLI,
		SupportsResume:       true,
		SupportsCancel:       true,
		SupportsTools:        true,
		TaskKinds:            []TaskKind{TaskKindResearch},
		Scopes:               []string{"global"},
		SupportsHeadlessPlan: true,
	}

	if caps.Matches(spec) {
		t.Fatalf("Matches() = true, want false")
	}
}

func TestCapabilitiesRejectStreamingWhenUnavailable(t *testing.T) {
	t.Parallel()

	spec := TaskSpec{
		Kind:  TaskKindResearch,
		Scope: "global",
		Requirements: Requirements{
			AllowedClasses: []ExecutorClass{ExecutorClassAPI},
			NeedsStreaming: true,
		},
	}

	caps := Capabilities{
		ExecutorClass: ExecutorClassAPI,
		TaskKinds:     []TaskKind{TaskKindResearch},
		Scopes:        []string{"global"},
	}

	if caps.Matches(spec) {
		t.Fatalf("Matches() = true, want false")
	}
}
