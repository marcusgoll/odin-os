package supervision

import (
	"context"
	"fmt"
	"time"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
	Now   func() time.Time
}

type TickResult struct {
	Promoted int
}

func (service Service) Tick(ctx context.Context) (TickResult, error) {
	if service.Store == nil {
		return TickResult{}, fmt.Errorf("scheduler store is required")
	}

	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}

	tasks, err := service.Store.ListEligibleQueuedTasks(ctx, now)
	if err != nil {
		return TickResult{}, err
	}

	result := TickResult{}
	for _, task := range tasks {
		if task.NextEligibleAt.IsZero() {
			continue
		}
		if task.NextEligibleAt.After(now) {
			continue
		}

		result.Promoted++
	}

	return result, nil
}
