package recovery

import (
	"context"
	"slices"
	"time"

	"odin-os/internal/store/sqlite"
)

type ActionContext struct {
	Observation Observation
	Incident    sqlite.Incident
	Recovery    sqlite.Recovery
	Attempt     int
	Store       *sqlite.Store
	Now         time.Time
}

type ActionResult struct {
	Status      string
	Description string
	DetailsJSON string
}

type Action func(context.Context, ActionContext) (ActionResult, error)

type Playbook struct {
	Name          string
	FaultKey      FaultKey
	AllowedScopes []string
	MaxRetries    int
	Cooldown      time.Duration
	ActionName    string
	Action        Action
}

func (playbook Playbook) allowsScope(scope string) bool {
	if len(playbook.AllowedScopes) == 0 {
		return true
	}
	return slices.Contains(playbook.AllowedScopes, scope)
}

func normalizePlaybook(playbook Playbook) Playbook {
	if playbook.MaxRetries <= 0 {
		playbook.MaxRetries = 1
	}
	if playbook.ActionName == "" {
		playbook.ActionName = playbook.Name
	}
	return playbook
}
