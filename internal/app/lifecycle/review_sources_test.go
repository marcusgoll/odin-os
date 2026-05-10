package lifecycle

import (
	"reflect"
	"testing"
)

func TestReviewQueueDefaultSourcesIncludeGovernedDecisionSources(t *testing.T) {
	sources := defaultReviewQueueSources()
	got := make([]string, 0, len(sources))
	for _, source := range sources {
		got = append(got, source.Name())
	}

	want := []string{
		"intake",
		"goal",
		"approval",
		"skill_artifact",
		"context_pack",
		"memory_proposal",
		"failed_work",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaultReviewQueueSources() names = %#v, want %#v", got, want)
	}
}
