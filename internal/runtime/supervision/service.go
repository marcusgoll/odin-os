package supervision

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"odin-os/internal/core/capabilities"
)

type Supervisor interface {
	Run(ctx context.Context, req capabilities.InvokeRequest, fn AttemptFunc) (capabilities.InvokeResponse, error)
}

type AttemptFunc func(context.Context, int) (capabilities.InvokeResponse, error)

type Service struct {
	MaxRetries int
}

type transientError interface {
	Transient() bool
}

func (service Service) Run(ctx context.Context, req capabilities.InvokeRequest, fn AttemptFunc) (capabilities.InvokeResponse, error) {
	if fn == nil {
		return capabilities.InvokeResponse{}, fmt.Errorf("attempt function is required")
	}

	timeout, err := time.ParseDuration(strings.TrimSpace(req.Execution.Timeout))
	if err != nil && strings.TrimSpace(req.Execution.Timeout) != "" {
		return capabilities.InvokeResponse{}, fmt.Errorf("invalid invocation timeout %q: %w", req.Execution.Timeout, err)
	}

	maxRetries := service.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	for attempt := 1; attempt <= maxRetries+1; attempt++ {
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				return capabilities.InvokeResponse{Status: "cancelled"}, nil
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return capabilities.InvokeResponse{Status: "timeout"}, nil
			}
			return capabilities.InvokeResponse{}, err
		}

		attemptCtx := ctx
		cancel := func() {}
		if timeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, timeout)
		}

		response, attemptErr := fn(attemptCtx, attempt)
		attemptCtxErr := attemptCtx.Err()
		cancel()

		switch {
		case errors.Is(attemptCtxErr, context.Canceled):
			return capabilities.InvokeResponse{Status: "cancelled"}, nil
		case errors.Is(attemptCtxErr, context.DeadlineExceeded):
			return capabilities.InvokeResponse{Status: "timeout"}, nil
		case attemptErr == nil:
			if strings.TrimSpace(response.Status) == "" {
				response.Status = "completed"
			}
			return response, nil
		case isTransient(attemptErr) && attempt < maxRetries+1:
			continue
		default:
			return capabilities.InvokeResponse{}, attemptErr
		}
	}

	return capabilities.InvokeResponse{}, nil
}

func isTransient(err error) bool {
	var transient transientError
	if errors.As(err, &transient) {
		return transient.Transient()
	}
	return false
}
