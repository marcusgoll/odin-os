package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"odin-os/internal/executors/contract"
	"odin-os/internal/registry/loader"
	"odin-os/internal/runtime/checkpoints"
	"odin-os/internal/runtime/health"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

type BuiltinDependencies struct {
	Store           *sqlite.Store
	RegistryRoot    string
	ExecutorCatalog map[string]contract.Executor
	HealthConfig    health.Config
}

func NewBuiltinPlaybooks(deps BuiltinDependencies) map[string]Playbook {
	return map[string]Playbook{
		"refresh_executor_health": {
			Name:          "refresh_executor_health",
			FaultKey:      FaultExecutorHealthStale,
			AllowedScopes: []string{"global", "project", "odin-core"},
			MaxRetries:    2,
			Cooldown:      deps.HealthConfig.ExecutorFreshnessTTL / 2,
			ActionName:    "refresh_executor_health",
			Action:        refreshExecutorHealthAction(deps),
		},
		"refresh_projection_freshness": {
			Name:          "refresh_projection_freshness",
			FaultKey:      FaultProjectionStale,
			AllowedScopes: []string{"global", "project", "odin-core"},
			MaxRetries:    2,
			Cooldown:      deps.HealthConfig.ProjectionFreshnessTTL / 2,
			ActionName:    "refresh_projection_surface",
			Action:        refreshProjectionSurfaceAction(deps),
		},
		"reload_registry_source": {
			Name:          "reload_registry_source",
			FaultKey:      FaultSourceFreshnessStale,
			AllowedScopes: []string{"global", "project", "odin-core"},
			MaxRetries:    2,
			Cooldown:      deps.HealthConfig.SourceFreshnessTTL / 2,
			ActionName:    "reload_registry_snapshot",
			Action:        reloadRegistrySourceAction(deps),
		},
		"checkpoint_failed_run": {
			Name:          "checkpoint_failed_run",
			FaultKey:      FaultRunFailureRepeated,
			AllowedScopes: []string{"project", "odin-core"},
			MaxRetries:    1,
			Cooldown:      5 * time.Minute,
			ActionName:    "create_failed_run_wake_packet",
			Action:        checkpointFailedRunAction(deps),
		},
		"escalate_queue_pressure": {
			Name:          "escalate_queue_pressure",
			FaultKey:      FaultQueuePressureHigh,
			AllowedScopes: []string{"global", "project", "odin-core"},
			MaxRetries:    1,
			Cooldown:      5 * time.Minute,
			ActionName:    "escalate_queue_pressure",
			Action:        escalateQueuePressureAction(),
		},
	}
}

func refreshExecutorHealthAction(deps BuiltinDependencies) Action {
	return func(ctx context.Context, actionCtx ActionContext) (ActionResult, error) {
		executor, ok := deps.ExecutorCatalog[actionCtx.Observation.SubjectKey]
		if !ok {
			return ActionResult{}, fmt.Errorf("executor %q is not registered", actionCtx.Observation.SubjectKey)
		}

		report, err := executor.Health(ctx)
		if err != nil {
			return ActionResult{}, err
		}

		if _, err := deps.Store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
			Executor:    executor.Key(),
			Status:      string(report.Status),
			LatencyMS:   0,
			DetailsJSON: fmt.Sprintf(`{"source":"self_heal","details":%q}`, report.Details),
		}); err != nil {
			return ActionResult{}, err
		}

		if report.Status == contract.HealthStatusHealthy {
			return ActionResult{
				Status:      "completed",
				Description: "executor health refreshed",
				DetailsJSON: fmt.Sprintf(`{"executor":%q,"status":%q}`, executor.Key(), report.Status),
			}, nil
		}

		return ActionResult{
			Status:      "failed",
			Description: "executor health remains stale or unavailable",
			DetailsJSON: fmt.Sprintf(`{"executor":%q,"status":%q}`, executor.Key(), report.Status),
		}, nil
	}
}

func refreshProjectionSurfaceAction(deps BuiltinDependencies) Action {
	return func(ctx context.Context, actionCtx ActionContext) (ActionResult, error) {
		if err := refreshProjectionSurface(ctx, deps.Store, deps.HealthConfig, actionCtx.Observation.SubjectKey); err != nil {
			return ActionResult{}, err
		}
		if _, err := deps.Store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
			Surface:     actionCtx.Observation.SubjectKey,
			Status:      "healthy",
			DetailsJSON: `{"source":"self_heal"}`,
		}); err != nil {
			return ActionResult{}, err
		}
		return ActionResult{
			Status:      "completed",
			Description: "projection freshness refreshed",
			DetailsJSON: fmt.Sprintf(`{"surface":%q}`, actionCtx.Observation.SubjectKey),
		}, nil
	}
}

func reloadRegistrySourceAction(deps BuiltinDependencies) Action {
	return func(ctx context.Context, actionCtx ActionContext) (ActionResult, error) {
		root := deps.RegistryRoot
		if root == "" {
			root = "registry"
		}

		snapshot, err := loader.LoadDir(root)
		if err != nil {
			return ActionResult{}, err
		}
		if len(snapshot.Diagnostics) > 0 {
			return ActionResult{
				Status:      "failed",
				Description: "registry diagnostics prevent source refresh",
				DetailsJSON: fmt.Sprintf(`{"diagnostic_count":%d}`, len(snapshot.Diagnostics)),
			}, nil
		}

		versionHash, err := registryVersionHash(root)
		if err != nil {
			return ActionResult{}, err
		}

		if _, err := deps.Store.RecordRegistryVersion(ctx, sqlite.RecordRegistryVersionParams{
			Source:      "registry",
			VersionHash: versionHash,
			Notes:       "self-heal reload",
		}); err != nil {
			return ActionResult{}, err
		}

		return ActionResult{
			Status:      "completed",
			Description: "registry source reloaded",
			DetailsJSON: fmt.Sprintf(`{"version_hash":%q}`, versionHash),
		}, nil
	}
}

func checkpointFailedRunAction(deps BuiltinDependencies) Action {
	return func(ctx context.Context, actionCtx ActionContext) (ActionResult, error) {
		if actionCtx.Observation.TaskID == nil {
			return ActionResult{}, fmt.Errorf("checkpoint playbook requires a task id")
		}

		result, err := checkpoints.Service{Store: deps.Store}.Compact(ctx, checkpoints.CompactParams{
			TaskID:         *actionCtx.Observation.TaskID,
			RunID:          actionCtx.Observation.RunID,
			Trigger:        checkpoints.TriggerRestart,
			CheckpointKey:  fmt.Sprintf("self-heal-%s", actionCtx.Observation.FaultKey),
			Objective:      "Recover from repeated run failures",
			TaskStatus:     "blocked",
			BlockingReason: "repeated run failures require operator review",
			NextSteps: []string{
				"Review the latest failed run output",
				"Adjust the task plan or inputs before retrying",
			},
			Constraints:          []string{"automatic retries are exhausted"},
			SelectedCapabilities: []string{"self_heal"},
			Evidence: []checkpoints.Evidence{{
				Kind:    "fault",
				Summary: actionCtx.Observation.Summary,
			}},
			ManifestSummary: "managed project",
			PolicySummary:   "bounded self-heal escalation",
			OpenTaskSummary: "operator review required",
			ApprovalSummary: "none",
			ToolResults:     nil,
		})
		if err != nil {
			return ActionResult{}, err
		}

		return ActionResult{
			Status:      "escalated",
			Description: "created operator handoff wake packet after repeated run failures",
			DetailsJSON: fmt.Sprintf(`{"wake_packet_id":%d}`, result.WakePacket.ID),
		}, nil
	}
}

func escalateQueuePressureAction() Action {
	return func(context.Context, ActionContext) (ActionResult, error) {
		return ActionResult{
			Status:      "escalated",
			Description: "queue pressure requires operator intervention",
			DetailsJSON: `{"reason":"queue pressure requires manual intervention"}`,
		}, nil
	}
}

func refreshProjectionSurface(ctx context.Context, store *sqlite.Store, healthConfig health.Config, surface string) error {
	switch surface {
	case "doctor":
		_, err := health.Service{
			DB:     store.DB(),
			Config: healthConfig,
			Now:    store.Now,
		}.Doctor(ctx, true)
		return err
	case "active_runs":
		_, err := projections.ListActiveRunViews(ctx, store.DB())
		return err
	case "blocked_items":
		_, err := projections.ListBlockedItemViews(ctx, store.DB())
		return err
	case "approvals_waiting":
		_, err := projections.ListPendingApprovalViews(ctx, store.DB())
		return err
	case "incidents":
		_, err := projections.ListIncidentViews(ctx, store.DB())
		return err
	case "recoveries":
		_, err := projections.ListRecoveryViews(ctx, store.DB())
		return err
	case "freshness":
		_, err := projections.ListFreshnessViews(ctx, store.DB())
		return err
	case "project_portfolio":
		_, err := projections.ListProjectPortfolioViews(ctx, store.DB())
		return err
	default:
		return fmt.Errorf("projection surface %q is not refreshable", surface)
	}
}

func registryVersionHash(root string) (string, error) {
	files, err := loader.ScanDir(root)
	if err != nil {
		return "", err
	}

	hasher := sha256.New()
	for _, file := range files {
		content, err := os.ReadFile(file.Path)
		if err != nil {
			return "", err
		}
		_, _ = hasher.Write([]byte(filepath.ToSlash(file.RelativePath)))
		_, _ = hasher.Write(content)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
