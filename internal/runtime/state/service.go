package state

import (
	"context"
	"fmt"
	"time"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
	Now   func() time.Time
}

type BootInput struct {
	BootID string
	PID    int
}

type TransitionInput struct {
	Reason string
	Error  string
}

func (service Service) MarkBooting(ctx context.Context, input BootInput) (sqlite.RuntimeState, error) {
	if service.Store == nil {
		return sqlite.RuntimeState{}, fmt.Errorf("runtime state store is required")
	}
	if input.BootID == "" {
		return sqlite.RuntimeState{}, fmt.Errorf("boot_id is required")
	}

	now := service.now()
	return service.Store.UpsertRuntimeState(ctx, sqlite.UpsertRuntimeStateParams{
		BootID:             input.BootID,
		Status:             "booting",
		PID:                input.PID,
		StartedAt:          now,
		LastHeartbeatAt:    now,
		LastShutdownReason: "",
		LastError:          "",
		UpdatedAt:          now,
	})
}

func (service Service) MarkRecovering(ctx context.Context, input TransitionInput) (sqlite.RuntimeState, error) {
	return service.transition(ctx, "recovering", input, func(params *sqlite.UpsertRuntimeStateParams, now time.Time) {
		params.LastHeartbeatAt = now
		if input.Error != "" {
			params.LastError = input.Error
		}
	})
}

func (service Service) MarkReady(ctx context.Context, input TransitionInput) (sqlite.RuntimeState, error) {
	return service.transition(ctx, "ready", input, func(params *sqlite.UpsertRuntimeStateParams, now time.Time) {
		params.ReadyAt = &now
		params.LastHeartbeatAt = now
		params.LastError = ""
	})
}

func (service Service) MarkDegraded(ctx context.Context, input TransitionInput) (sqlite.RuntimeState, error) {
	return service.transition(ctx, "degraded", input, func(params *sqlite.UpsertRuntimeStateParams, now time.Time) {
		params.LastHeartbeatAt = now
		if input.Error != "" {
			params.LastError = input.Error
			return
		}
		if input.Reason != "" {
			params.LastError = input.Reason
		}
	})
}

func (service Service) MarkDraining(ctx context.Context, input TransitionInput) (sqlite.RuntimeState, error) {
	return service.transition(ctx, "draining", input, func(params *sqlite.UpsertRuntimeStateParams, now time.Time) {
		params.LastHeartbeatAt = now
	})
}

func (service Service) MarkStopped(ctx context.Context, input TransitionInput) (sqlite.RuntimeState, error) {
	return service.transition(ctx, "stopped", input, func(params *sqlite.UpsertRuntimeStateParams, now time.Time) {
		params.LastHeartbeatAt = now
		params.LastShutdownReason = input.Reason
	})
}

func (service Service) Heartbeat(ctx context.Context) (sqlite.RuntimeState, error) {
	if service.Store == nil {
		return sqlite.RuntimeState{}, fmt.Errorf("runtime state store is required")
	}
	if service.Now == nil {
		return service.Store.UpdateRuntimeHeartbeat(ctx)
	}

	originalNow := service.Store.Now
	service.Store.Now = service.Now
	defer func() {
		service.Store.Now = originalNow
	}()

	return service.Store.UpdateRuntimeHeartbeat(ctx)
}

func (service Service) transition(ctx context.Context, status string, input TransitionInput, mutate func(*sqlite.UpsertRuntimeStateParams, time.Time)) (sqlite.RuntimeState, error) {
	if service.Store == nil {
		return sqlite.RuntimeState{}, fmt.Errorf("runtime state store is required")
	}

	current, err := service.Store.GetRuntimeState(ctx)
	if err != nil {
		return sqlite.RuntimeState{}, err
	}

	now := service.now()
	params := sqlite.UpsertRuntimeStateParams{
		BootID:             current.BootID,
		Status:             status,
		PID:                current.PID,
		StartedAt:          current.StartedAt,
		ReadyAt:            current.ReadyAt,
		LastHeartbeatAt:    current.LastHeartbeatAt,
		LastShutdownReason: current.LastShutdownReason,
		LastError:          current.LastError,
		UpdatedAt:          now,
		EventReason:        transitionReason(input),
	}
	if mutate != nil {
		mutate(&params, now)
	}

	return service.Store.UpsertRuntimeState(ctx, params)
}

func transitionReason(input TransitionInput) string {
	if input.Reason != "" {
		return input.Reason
	}
	return input.Error
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now().UTC()
	}
	return time.Now().UTC()
}
