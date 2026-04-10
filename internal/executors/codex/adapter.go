package codex

import (
	"odin-os/internal/executors/contract"
	"odin-os/internal/executors/harness"
)

func NewHeadless() contract.Executor {
	return harness.NewDriver("codex_headless", "ODIN_CODEX_DRIVER", "codex")
}
