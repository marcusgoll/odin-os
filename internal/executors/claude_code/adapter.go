package claude_code

import (
	"odin-os/internal/executors/contract"
	"odin-os/internal/executors/harness"
)

func NewHeadless() contract.Executor {
	return harness.NewDriver("claude_code_headless", "ODIN_CLAUDE_DRIVER", "claude")
}
