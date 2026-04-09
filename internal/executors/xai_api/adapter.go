package xai_api

import (
	"time"

	"odin-os/internal/executors/contract"
)

func New() contract.Executor {
	return contract.NewStaticExecutor(
		"xai_api",
		contract.ExecutorClassAPI,
		contract.HealthReport{Status: contract.HealthStatusUnknown, CheckedAt: time.Now().UTC()},
		contract.Capabilities{
			SupportsResume:       true,
			SupportsCancel:       true,
			SupportsTools:        true,
			SupportsCostEstimate: true,
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
