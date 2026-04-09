package catalog

import "testing"

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
