package catalog

import (
	"context"
	"testing"

	"odin-os/internal/tools/invocation"
)

func TestBuiltinCatalogDoesNotExposePlaceholderOperationalTools(t *testing.T) {
	t.Parallel()

	definitions := BuiltinDefinitions()
	for _, key := range []string{"project_status", "task_list", "event_log"} {
		if _, ok := definitions[key]; ok {
			t.Fatalf("%s should not be exposed until it is runtime-backed", key)
		}
	}
}

func TestBuiltinProjectStatusInvokesRuntimeDriver(t *testing.T) {
	t.Parallel()

	invoker := &stubToolInvoker{
		result: invocation.Result{
			Source:  "script",
			Summary: "Project alpha status from runtime.",
			KeyFacts: map[string]string{
				"project_key":     "alpha",
				"open_task_count": "2",
			},
			FollowOnOptions: []string{"inspect tasks"},
			RawRef:          "driver://project_status/alpha",
			RawOutput:       "project=alpha open_tasks=2",
		},
	}

	definitions := BuiltinDefinitionsWithInvoker(invoker)
	result, err := definitions["project_status"].Invoke(map[string]string{"project_key": "alpha"})
	if err != nil {
		t.Fatalf("Invoke(project_status) error = %v", err)
	}
	if invoker.key != "project_status" {
		t.Fatalf("invoked key = %q, want project_status", invoker.key)
	}
	if invoker.args["project_key"] != "alpha" {
		t.Fatalf("project_key arg = %q, want alpha", invoker.args["project_key"])
	}
	if result.Source != "driver" {
		t.Fatalf("result source = %q, want driver", result.Source)
	}
	if result.RawRef != "driver://project_status/alpha" {
		t.Fatalf("raw ref = %q, want driver-backed ref", result.RawRef)
	}
}

func TestBuiltinProjectStatusRequiresRuntimeInvoker(t *testing.T) {
	t.Parallel()

	definitions := BuiltinDefinitions()
	if _, ok := definitions["project_status"]; ok {
		t.Fatal("project_status should not be exposed without a runtime invoker")
	}
}

type stubToolInvoker struct {
	key    string
	args   map[string]string
	result invocation.Result
}

func (invoker *stubToolInvoker) Invoke(_ context.Context, key string, request invocation.Request) (invocation.Result, error) {
	invoker.key = key
	invoker.args = request.Args
	return invoker.result, nil
}
