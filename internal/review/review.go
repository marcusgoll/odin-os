package review

import "context"

type Finding struct {
	Severity string
	Message  string
}

// Reviewer checks worker output before human handoff.
type Reviewer interface {
	Review(ctx context.Context, runID string) ([]Finding, error)
}
