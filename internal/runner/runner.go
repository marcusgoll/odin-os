package runner

import (
	"context"
	"errors"
	"time"
)

var ErrNotImplemented = errors.New("runner not implemented")

type Request struct {
	WorkItemID  string
	Role        string
	Worktree    string
	Prompt      string
	DryRun      bool
	Timeout     time.Duration
	SandboxMode string
}

type Result struct {
	Summary string
}

// AgentRunner executes one role attempt for one work item in one worktree.
type AgentRunner interface {
	Run(ctx context.Context, request Request) (Result, error)
}

// Runner is kept as the compatibility name while callers migrate to AgentRunner.
type Runner = AgentRunner
