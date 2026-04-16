package catalog

import "testing"

func TestBuiltinCatalogDoesNotExposePlaceholderOperationalTools(t *testing.T) {
	t.Parallel()

	definitions := BuiltinDefinitions()
	for _, key := range []string{"project_status", "task_list", "event_log"} {
		if _, ok := definitions[key]; ok {
			t.Fatalf("%s should not be exposed until it is runtime-backed", key)
		}
	}
}
