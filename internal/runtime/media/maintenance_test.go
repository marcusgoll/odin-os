package media

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appmedia "odin-os/internal/core/media"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/store/sqlite"
)

func TestMediaMaintenancePreflightBlocksWhenBackupVerificationMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	store := openMediaStore(t, now)
	defer store.Close()
	task := seedMaintenanceTask(t, ctx, store)

	service := MaintenanceService{
		Store:       store,
		Config:      approvalMediaConfig(),
		RuntimeRoot: t.TempDir(),
		Now:         func() time.Time { return now },
	}

	result, err := service.Preflight(ctx, PreflightRequest{
		TaskID: &task.ID,
		Action: "restart_plex",
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if result.BlockedReason == "" || !strings.Contains(result.BlockedReason, "backup") {
		t.Fatalf("BlockedReason = %q, want backup freshness failure", result.BlockedReason)
	}
	if result.EvidencePacketID == nil {
		t.Fatalf("EvidencePacketID = nil, want preflight evidence packet")
	}
	if result.ApprovalID != nil {
		t.Fatalf("ApprovalID = %v, want nil while backup gate is failing", *result.ApprovalID)
	}
}

func TestMediaMaintenancePostflightEmitsRollbackRecommendationForCriticalSignal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	store := openMediaStore(t, now)
	defer store.Close()
	task := seedMaintenanceTask(t, ctx, store)

	service := MaintenanceService{
		Store:       store,
		Config:      approvalMediaConfig(),
		RuntimeRoot: t.TempDir(),
		Now:         func() time.Time { return now },
	}

	result, err := service.Postflight(ctx, PostflightRequest{
		TaskID: &task.ID,
		Action: "restart_plex",
		Checks: []healthsvc.Check{
			{Name: "media.mounts", Status: healthsvc.StatusFailed, Summary: "mount mismatch detected", ObservedAt: now},
		},
	})
	if err != nil {
		t.Fatalf("Postflight() error = %v", err)
	}
	if !result.RollbackRecommended {
		t.Fatalf("RollbackRecommended = false, want true")
	}
	if result.RecommendationPacketID == nil {
		t.Fatalf("RecommendationPacketID = nil, want rollback recommendation packet")
	}

	packet, err := store.GetContextPacket(ctx, *result.RecommendationPacketID)
	if err != nil {
		t.Fatalf("GetContextPacket() error = %v", err)
	}
	if !strings.Contains(packet.Summary, "rollback") {
		t.Fatalf("packet summary = %q, want rollback recommendation", packet.Summary)
	}
}

func approvalMediaConfig() *appmedia.Config {
	return &appmedia.Config{
		Enabled: true,
		Policies: appmedia.Policies{
			ApprovalRequired: []string{"restart_plex", "retry_import_move"},
			Forbidden:        []string{"delete_media", "change_vpn_networking"},
		},
	}
}

func seedMaintenanceTask(t *testing.T, ctx context.Context, store *sqlite.Store) sqlite.Task {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       "/tmp/odin-os",
		DefaultBranch: "main",
		ManifestPath:  filepath.Join("config", "projects.yaml"),
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "media-maintenance-request",
		Title:       "Media maintenance request",
		Status:      "blocked",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return task
}
