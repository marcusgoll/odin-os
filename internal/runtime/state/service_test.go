package state

import (
	"context"
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
	got, err = service.MarkReady(ctx, TransitionInput{})
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
	got, err = service.Heartbeat(ctx)
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if !got.LastHeartbeatAt.Equal(heartbeatAt) {
		t.Fatalf("LastHeartbeatAt after heartbeat = %v, want %v", got.LastHeartbeatAt, heartbeatAt)
	}

	now = stoppedAt
	got, err = service.MarkStopped(ctx, TransitionInput{Reason: "operator requested shutdown"})
	if err != nil {
		t.Fatalf("MarkStopped() error = %v", err)
	}
	if got.Status != "stopped" {
		t.Fatalf("Status after stop = %q, want %q", got.Status, "stopped")
	}
	if got.LastShutdownReason != "operator requested shutdown" {
		t.Fatalf("LastShutdownReason = %q, want %q", got.LastShutdownReason, "operator requested shutdown")
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
