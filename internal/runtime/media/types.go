package media

import (
	"context"
	"time"

	coremedia "odin-os/internal/core/media"
	"odin-os/internal/core/projects"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/store/sqlite"
)

type Checker interface {
	Checks(context.Context, healthsvc.Config, time.Time) ([]healthsvc.Check, error)
}

type Service struct {
	Store         *sqlite.Store
	Config        *coremedia.Config
	RuntimeRoot   string
	SystemProject projects.Manifest
	Checker       Checker
	Now           func() time.Time
}

type CycleResult struct {
	Checks              []healthsvc.Check
	OpenedIncidentIDs   []int64
	ResolvedIncidentIDs []int64
	CandidateTaskID     *int64
}
