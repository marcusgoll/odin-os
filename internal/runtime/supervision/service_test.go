package supervision

import (
	"context"
	"errors"
	"testing"
	"time"

	"odin-os/internal/core/capabilities"
)

type transientFailure struct {
	message string
}

func (failure transientFailure) Error() string {
	return failure.message
}

func (failure transientFailure) Transient() bool {
	return true
}

func TestSupervisorEnforcesTimeout(t *testing.T) {
	t.Parallel()

	service := Service{MaxRetries: 0}
	request := capabilities.InvokeRequest{
		RequestID: "request-timeout",
		Execution: capabilities.ExecutionRequest{
			Timeout: "25ms",
		},
	}

	attempts := 0
	response, err := service.Run(context.Background(), request, func(ctx context.Context, attempt int) (capabilities.InvokeResponse, error) {
		attempts++
		if attempt != 1 {
			t.Fatalf("attempt = %d, want 1", attempt)
		}

		select {
		case <-ctx.Done():
			if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				t.Fatalf("ctx.Err() = %v, want deadline exceeded", ctx.Err())
			}
			return capabilities.InvokeResponse{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
			t.Fatal("attempt was not cancelled by the timeout")
			return capabilities.InvokeResponse{}, nil
		}
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "timeout" {
		t.Fatalf("Run().Status = %q, want %q", response.Status, "timeout")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestSupervisorRetriesTransientFailure(t *testing.T) {
	t.Parallel()

	service := Service{MaxRetries: 2}
	request := capabilities.InvokeRequest{
		RequestID: "request-retry",
		Execution: capabilities.ExecutionRequest{
			Timeout: "500ms",
		},
	}

	attempts := 0
	response, err := service.Run(context.Background(), request, func(ctx context.Context, attempt int) (capabilities.InvokeResponse, error) {
		attempts++
		if attempt != attempts {
			t.Fatalf("attempt = %d, want %d", attempt, attempts)
		}
		switch attempts {
		case 1, 2:
			return capabilities.InvokeResponse{}, transientFailure{message: "transient executor failure"}
		case 3:
			return capabilities.InvokeResponse{Status: "completed"}, nil
		default:
			t.Fatalf("unexpected attempt count %d", attempts)
			return capabilities.InvokeResponse{}, nil
		}
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "completed" {
		t.Fatalf("Run().Status = %q, want %q", response.Status, "completed")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestSupervisorCancelsRun(t *testing.T) {
	t.Parallel()

	service := Service{MaxRetries: 0}
	ctx, cancel := context.WithCancel(context.Background())
	request := capabilities.InvokeRequest{
		RequestID: "request-cancel",
		Execution: capabilities.ExecutionRequest{
			Timeout: "500ms",
		},
	}

	started := make(chan struct{})
	go func() {
		<-started
		cancel()
	}()

	response, err := service.Run(ctx, request, func(ctx context.Context, attempt int) (capabilities.InvokeResponse, error) {
		if attempt != 1 {
			t.Fatalf("attempt = %d, want 1", attempt)
		}
		close(started)
		<-ctx.Done()
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("ctx.Err() = %v, want canceled", ctx.Err())
		}
		return capabilities.InvokeResponse{}, ctx.Err()
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "cancelled" {
		t.Fatalf("Run().Status = %q, want %q", response.Status, "cancelled")
	}
}
