package catalog

import (
	"context"
	"testing"

	"odin-os/internal/tools/invocation"
)

func TestBuiltinDefinitionsIncludeSchemasAndHandlers(t *testing.T) {
	t.Parallel()

	definitions := BuiltinDefinitions()
	if len(definitions) == 0 {
		t.Fatalf("BuiltinDefinitions() len = 0, want > 0")
	}

	taskList, ok := definitions["task_list"]
	if !ok {
		t.Fatalf("missing task_list definition")
	}
	if taskList.Schema == nil {
		t.Fatalf("task_list schema = nil, want schema")
	}
	if taskList.Invoke == nil {
		t.Fatalf("task_list invoke = nil, want handler")
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

func TestBuiltinProjectStatusKeepsLegacyFallbackWithoutInvoker(t *testing.T) {
	t.Parallel()

	definitions := BuiltinDefinitions()
	result, err := definitions["project_status"].Invoke(map[string]string{"project_key": "alpha"})
	if err != nil {
		t.Fatalf("Invoke(project_status) error = %v", err)
	}
	if result.Source != "builtin" {
		t.Fatalf("result source = %q, want builtin", result.Source)
	}
	if result.Summary != "Project status prepared for alpha." {
		t.Fatalf("summary = %q, want legacy canned summary", result.Summary)
	}
	if result.KeyFacts["project_key"] != "alpha" {
		t.Fatalf("project_key fact = %q, want alpha", result.KeyFacts["project_key"])
	}
	if result.RawRef != "builtin://project_status/result" {
		t.Fatalf("raw ref = %q, want legacy builtin ref", result.RawRef)
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
