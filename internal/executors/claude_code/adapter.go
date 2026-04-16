package claude_code

import (
	"time"

	"odin-os/internal/executors/contract"
)

func NewHeadless() contract.Executor {
	return contract.NewStaticExecutor(
		"claude_code_headless",
		contract.ExecutorClassPlanBackedCLI,
		contract.HealthReport{Status: contract.HealthStatusUnknown, CheckedAt: time.Now().UTC()},
		contract.Capabilities{
			SupportsResume:       true,
			SupportsCancel:       true,
			SupportsTools:        true,
			SupportsHeadlessPlan: true,
			TaskKinds: []contract.TaskKind{
				contract.TaskKindGeneral,
				contract.TaskKindPlan,
				contract.TaskKindBuild,
				contract.TaskKindReview,
				contract.TaskKindQA,
				contract.TaskKindResearch,
			},
			Scopes: []string{"global", "odin-core", "project", "new-project"},
		},
	)
}

func NewCapabilityBridge() Bridge {
	return NewBridge()
}
