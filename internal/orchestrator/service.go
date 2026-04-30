package orchestrator

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("agency orchestrator not implemented")

// Service coordinates intake, workspace allocation, runner execution, review,
// and human handoff. Full behavior is added through roadmap phases.
type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (Service) RunOnce(context.Context) error {
	return ErrNotImplemented
}
