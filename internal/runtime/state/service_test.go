package state

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

func TestRuntimeStateServiceBootAndHeartbeat(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "runtime-state.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	bootAt := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	readyAt := bootAt.Add(2 * time.Minute)
	heartbeatAt := readyAt.Add(30 * time.Second)
	stoppedAt := heartbeatAt.Add(45 * time.Second)

	now := bootAt
	service := Service{
		Store: store,
		Now: func() time.Time {
			return now
		},
	}

	got, err := service.MarkBooting(ctx, BootInput{BootID: "boot-1", PID: 1234})
	if err != nil {
		t.Fatalf("MarkBooting() error = %v", err)
	}
	if got.Status != "booting" {
		t.Fatalf("Status = %q, want %q", got.Status, "booting")
	}
	if got.SingletonKey != "primary" {
		t.Fatalf("SingletonKey = %q, want %q", got.SingletonKey, "primary")
	}
	if got.BootID != "boot-1" {
		t.Fatalf("BootID = %q, want %q", got.BootID, "boot-1")
	}
	if got.PID != 1234 {
		t.Fatalf("PID = %d, want %d", got.PID, 1234)
	}
	if !got.StartedAt.Equal(bootAt) {
		t.Fatalf("StartedAt = %v, want %v", got.StartedAt, bootAt)
	}
	if !got.LastHeartbeatAt.Equal(bootAt) {
		t.Fatalf("LastHeartbeatAt = %v, want %v", got.LastHeartbeatAt, bootAt)
	}

	now = readyAt
	got, err = service.MarkReady(ctx, TransitionInput{BootID: "boot-1"})
	if err != nil {
		t.Fatalf("MarkReady() error = %v", err)
	}
	if got.Status != "ready" {
		t.Fatalf("Status after ready = %q, want %q", got.Status, "ready")
	}
	if got.ReadyAt == nil || !got.ReadyAt.Equal(readyAt) {
		t.Fatalf("ReadyAt = %v, want %v", got.ReadyAt, readyAt)
	}

	now = heartbeatAt
	got, err = service.Heartbeat(ctx, HeartbeatInput{BootID: "boot-1"})
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if !got.LastHeartbeatAt.Equal(heartbeatAt) {
		t.Fatalf("LastHeartbeatAt after heartbeat = %v, want %v", got.LastHeartbeatAt, heartbeatAt)
	}

	now = stoppedAt
	got, err = service.MarkStopped(ctx, TransitionInput{
		BootID: "boot-1",
		Reason: "operator requested shutdown",
		Error:  "fatal shutdown",
	})
	if err != nil {
		t.Fatalf("MarkStopped() error = %v", err)
	}
	if got.Status != "stopped" {
		t.Fatalf("Status after stop = %q, want %q", got.Status, "stopped")
	}
	if got.LastShutdownReason != "operator requested shutdown" {
		t.Fatalf("LastShutdownReason = %q, want %q", got.LastShutdownReason, "operator requested shutdown")
	}
	if got.LastError != "fatal shutdown" {
		t.Fatalf("LastError = %q, want %q", got.LastError, "fatal shutdown")
	}

	stored, err := store.GetRuntimeState(ctx)
	if err != nil {
		t.Fatalf("GetRuntimeState() error = %v", err)
	}
	if stored.Status != "stopped" {
		t.Fatalf("GetRuntimeState().Status = %q, want %q", stored.Status, "stopped")
	}
	if stored.LastShutdownReason != "operator requested shutdown" {
		t.Fatalf("GetRuntimeState().LastShutdownReason = %q, want %q", stored.LastShutdownReason, "operator requested shutdown")
	}
	if stored.LastError != "fatal shutdown" {
		t.Fatalf("GetRuntimeState().LastError = %q, want %q", stored.LastError, "fatal shutdown")
	}

	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	var lifecycleCount int
	var heartbeatCount int
	for _, event := range events {
		switch event.Type {
		case runtimeevents.EventServiceLifecycleChanged:
			payload, err := runtimeevents.DecodePayload[runtimeevents.ServiceLifecyclePayload](event.Payload)
			if err != nil {
				t.Fatalf("DecodePayload(ServiceLifecyclePayload) error = %v", err)
			}
			if payload.BootID != "boot-1" {
				t.Fatalf("lifecycle payload boot_id = %q, want %q", payload.BootID, "boot-1")
			}
			lifecycleCount++
		case runtimeevents.EventServiceHeartbeatRecorded:
			heartbeatCount++
		}
	}

	if lifecycleCount != 3 {
		t.Fatalf("lifecycle event count = %d, want %d", lifecycleCount, 3)
	}
	if heartbeatCount != 1 {
		t.Fatalf("heartbeat event count = %d, want %d", heartbeatCount, 1)
	}
}

func TestRuntimeStateServiceTransitionStatuses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "runtime-state-transitions.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	bootAt := time.Date(2026, 4, 16, 13, 0, 0, 0, time.UTC)
	recoveringAt := bootAt.Add(30 * time.Second)
	degradedAt := recoveringAt.Add(45 * time.Second)
	drainingAt := degradedAt.Add(30 * time.Second)

	now := bootAt
	service := Service{
		Store: store,
		Now: func() time.Time {
			return now
		},
	}

	if _, err := service.MarkBooting(ctx, BootInput{BootID: "boot-1", PID: 1234}); err != nil {
		t.Fatalf("MarkBooting() error = %v", err)
	}

	now = recoveringAt
	recovering, err := service.MarkRecovering(ctx, TransitionInput{
		BootID: "boot-1",
		Error:  "startup recovery in progress",
	})
	if err != nil {
		t.Fatalf("MarkRecovering() error = %v", err)
	}
	if recovering.Status != "recovering" {
		t.Fatalf("recovering status = %q, want %q", recovering.Status, "recovering")
	}
	if recovering.LastError != "startup recovery in progress" {
		t.Fatalf("recovering LastError = %q, want %q", recovering.LastError, "startup recovery in progress")
	}

	now = degradedAt
	degraded, err := service.MarkDegraded(ctx, TransitionInput{
		BootID: "boot-1",
		Reason: "dependency stale",
	})
	if err != nil {
		t.Fatalf("MarkDegraded() error = %v", err)
	}
	if degraded.Status != "degraded" {
		t.Fatalf("degraded status = %q, want %q", degraded.Status, "degraded")
	}
	if degraded.LastError != "dependency stale" {
		t.Fatalf("degraded LastError = %q, want %q", degraded.LastError, "dependency stale")
	}

	now = drainingAt
	draining, err := service.MarkDraining(ctx, TransitionInput{
		BootID: "boot-1",
		Reason: "shutdown in progress",
	})
	if err != nil {
		t.Fatalf("MarkDraining() error = %v", err)
	}
	if draining.Status != "draining" {
		t.Fatalf("draining status = %q, want %q", draining.Status, "draining")
	}
	if draining.LastError != "dependency stale" {
		t.Fatalf("draining LastError = %q, want prior degraded error %q", draining.LastError, "dependency stale")
	}

	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	var statuses []string
	for _, event := range events {
		if event.Type != runtimeevents.EventServiceLifecycleChanged {
			continue
		}
		payload, err := runtimeevents.DecodePayload[runtimeevents.ServiceLifecyclePayload](event.Payload)
		if err != nil {
			t.Fatalf("DecodePayload(ServiceLifecyclePayload) error = %v", err)
		}
		statuses = append(statuses, payload.Status)
	}

	if len(statuses) != 4 {
		t.Fatalf("lifecycle statuses len = %d, want 4", len(statuses))
	}
	if statuses[0] != "booting" || statuses[1] != "recovering" || statuses[2] != "degraded" || statuses[3] != "draining" {
		t.Fatalf("lifecycle statuses = %v, want [booting recovering degraded draining]", statuses)
	}
}

func TestRuntimeStateServiceRejectsBootIdentityMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "runtime-state-mismatch.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	now := time.Date(2026, 4, 16, 14, 0, 0, 0, time.UTC)
	service := Service{
		Store: store,
		Now: func() time.Time {
			return now
		},
	}

	if _, err := service.MarkBooting(ctx, BootInput{BootID: "boot-1", PID: 1234}); err != nil {
		t.Fatalf("MarkBooting(boot-1) error = %v", err)
	}

	now = now.Add(1 * time.Minute)
	if _, err := service.MarkBooting(ctx, BootInput{BootID: "boot-2", PID: 4321}); err != nil {
		t.Fatalf("MarkBooting(boot-2) error = %v", err)
	}

	now = now.Add(30 * time.Second)
	if _, err := service.MarkReady(ctx, TransitionInput{BootID: "boot-1"}); !errors.Is(err, sqlite.ErrRuntimeStateBootMismatch) {
		t.Fatalf("MarkReady(stale boot) error = %v, want %v", err, sqlite.ErrRuntimeStateBootMismatch)
	}
	if _, err := service.Heartbeat(ctx, HeartbeatInput{BootID: "boot-1"}); !errors.Is(err, sqlite.ErrRuntimeStateBootMismatch) {
		t.Fatalf("Heartbeat(stale boot) error = %v, want %v", err, sqlite.ErrRuntimeStateBootMismatch)
	}

	got, err := store.GetRuntimeState(ctx)
	if err != nil {
		t.Fatalf("GetRuntimeState() error = %v", err)
	}
	if got.BootID != "boot-2" {
		t.Fatalf("GetRuntimeState().BootID = %q, want %q", got.BootID, "boot-2")
	}
	if got.Status != "booting" {
		t.Fatalf("GetRuntimeState().Status = %q, want %q", got.Status, "booting")
	}
}

func TestRuntimeStateServiceRejectsRevivingDrainingRuntime(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "runtime-state-draining.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	now := time.Date(2026, 4, 16, 15, 0, 0, 0, time.UTC)
	service := Service{
		Store: store,
		Now: func() time.Time {
			return now
		},
	}

	if _, err := service.MarkBooting(ctx, BootInput{BootID: "boot-1", PID: 1234}); err != nil {
		t.Fatalf("MarkBooting() error = %v", err)
	}
	now = now.Add(30 * time.Second)
	if _, err := service.MarkDraining(ctx, TransitionInput{
		BootID: "boot-1",
		Reason: "shutdown requested",
	}); err != nil {
		t.Fatalf("MarkDraining() error = %v", err)
	}

	now = now.Add(15 * time.Second)
	if _, err := service.MarkReady(ctx, TransitionInput{BootID: "boot-1"}); !errors.Is(err, ErrRuntimeStateDrainLatched) {
		t.Fatalf("MarkReady(draining) error = %v, want %v", err, ErrRuntimeStateDrainLatched)
	}
	if _, err := service.MarkDegraded(ctx, TransitionInput{
		BootID: "boot-1",
		Reason: "health degraded during shutdown",
	}); !errors.Is(err, ErrRuntimeStateDrainLatched) {
		t.Fatalf("MarkDegraded(draining) error = %v, want %v", err, ErrRuntimeStateDrainLatched)
	}

	got, err := store.GetRuntimeState(ctx)
	if err != nil {
		t.Fatalf("GetRuntimeState() error = %v", err)
	}
	if got.Status != "draining" {
		t.Fatalf("GetRuntimeState().Status = %q, want %q", got.Status, "draining")
	}
}
