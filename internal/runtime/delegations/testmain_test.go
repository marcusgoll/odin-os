package delegations

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("ODIN_SQLITE_FAST_TEST_PRAGMAS") == "" {
		_ = os.Setenv("ODIN_SQLITE_FAST_TEST_PRAGMAS", "1")
	}
	os.Exit(m.Run())
}
