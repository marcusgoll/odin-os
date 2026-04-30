package dashboard

import "time"

type Status struct {
	GeneratedAt     time.Time
	KillSwitch      bool
	DryRun          bool
	ActiveWorkers   int
	PendingHandoffs int
}
