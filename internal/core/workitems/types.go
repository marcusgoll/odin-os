package workitems

import (
	"time"

	"odin-os/internal/core/controlscope"
)

type WorkItem struct {
	ID            int64
	Scope         controlscope.ControlScope
	WorkspaceKey  string
	InitiativeKey string
	ProjectKey    string
	CompanionKey  string
	Status        string
	Title         string
	RequestedBy   string
	CurrentRunID  *int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
