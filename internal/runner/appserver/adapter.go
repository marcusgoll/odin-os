package appserver

import (
	"context"

	"odin-os/internal/runner"
)

// Adapter is the future Codex app-server runner kept behind the runner boundary.
type Adapter struct{}

func NewAdapter() *Adapter {
	return &Adapter{}
}

func (Adapter) Run(context.Context, runner.Request) (runner.Result, error) {
	return runner.Result{}, runner.ErrNotImplemented
}
