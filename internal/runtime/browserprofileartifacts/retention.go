package browserprofileartifacts

import (
	"context"
	"fmt"
	"time"

	"odin-os/internal/store/sqlite"
)

const (
	RetentionActionWouldClean = "would_clean"
	RetentionActionCleaned    = "cleaned"
	RetentionActionFailed     = "failed"
)

type RetentionStore interface {
	ListBrowserEncryptedProfileArtifacts(context.Context, sqlite.ListBrowserEncryptedProfileArtifactsParams) ([]sqlite.BrowserEncryptedProfileArtifact, error)
	MarkBrowserEncryptedProfileArtifactCleaned(context.Context, sqlite.MarkBrowserEncryptedProfileArtifactCleanedParams) (sqlite.BrowserEncryptedProfileArtifact, error)
	RecordBrowserEncryptedProfileArtifactCleanupFailed(context.Context, sqlite.RecordBrowserEncryptedProfileArtifactCleanupFailedParams) (sqlite.BrowserEncryptedProfileArtifact, error)
}

type RetentionParams struct {
	Store     RetentionStore
	ODINRoot  string
	Now       time.Time
	SessionID int64
	DryRun    bool
	Apply     bool
}

type RetentionResult struct {
	DryRun    bool                    `json:"dry_run"`
	Eligible  int                     `json:"eligible"`
	Cleaned   int                     `json:"cleaned"`
	Failed    int                     `json:"failed"`
	Skipped   int                     `json:"skipped"`
	Artifacts []RetentionArtifactItem `json:"artifacts"`
}

type RetentionArtifactItem struct {
	ArtifactID   int64  `json:"artifact_id"`
	SessionID    int64  `json:"session_id"`
	Status       string `json:"status"`
	ArtifactPath string `json:"artifact_path"`
	Action       string `json:"action"`
	Removed      bool   `json:"removed"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

func Retain(ctx context.Context, params RetentionParams) (RetentionResult, error) {
	if params.Store == nil {
		return RetentionResult{}, fmt.Errorf("browser profile artifact retention store is required")
	}
	if params.SessionID < 0 {
		return RetentionResult{}, fmt.Errorf("browser profile artifact retention session_id must be positive")
	}
	if params.Apply && params.DryRun {
		return RetentionResult{}, fmt.Errorf("browser profile artifact retention cannot use apply and dry_run together")
	}
	dryRun := !params.Apply
	artifacts, err := params.Store.ListBrowserEncryptedProfileArtifacts(ctx, sqlite.ListBrowserEncryptedProfileArtifactsParams{SessionID: params.SessionID})
	if err != nil {
		return RetentionResult{}, err
	}
	result := RetentionResult{
		DryRun:    dryRun,
		Artifacts: make([]RetentionArtifactItem, 0),
	}
	for _, artifact := range artifacts {
		if !isRetentionEligibleStatus(artifact.Status) {
			result.Skipped++
			continue
		}
		result.Eligible++
		item := RetentionArtifactItem{
			ArtifactID:   artifact.ID,
			SessionID:    artifact.SessionID,
			Status:       string(artifact.Status),
			ArtifactPath: artifact.EncryptedArtifactPath,
			Action:       RetentionActionWouldClean,
		}
		if dryRun {
			result.Artifacts = append(result.Artifacts, item)
			continue
		}
		cleanup, cleanupErr := Cleanup(CleanupParams{
			ODINRoot:     params.ODINRoot,
			ArtifactPath: artifact.EncryptedArtifactPath,
		})
		if cleanupErr != nil {
			errorCode := "cleanup_failed"
			errorMessage := cleanupErr.Error()
			if _, err := params.Store.RecordBrowserEncryptedProfileArtifactCleanupFailed(ctx, sqlite.RecordBrowserEncryptedProfileArtifactCleanupFailedParams{
				ID:           artifact.ID,
				Actor:        "retention",
				Reason:       "encrypted profile artifact cleanup failed",
				ErrorCode:    &errorCode,
				ErrorMessage: &errorMessage,
			}); err != nil {
				return RetentionResult{}, err
			}
			item.Action = RetentionActionFailed
			item.ErrorCode = errorCode
			item.ErrorMessage = errorMessage
			result.Failed++
			result.Artifacts = append(result.Artifacts, item)
			continue
		}
		item.ArtifactPath = cleanup.ArtifactPath
		item.Removed = cleanup.Removed
		if _, err := params.Store.MarkBrowserEncryptedProfileArtifactCleaned(ctx, sqlite.MarkBrowserEncryptedProfileArtifactCleanedParams{
			ID:     artifact.ID,
			Actor:  "retention",
			Reason: retentionCleanedReason(cleanup.Removed),
		}); err != nil {
			return RetentionResult{}, err
		}
		item.Action = RetentionActionCleaned
		result.Cleaned++
		result.Artifacts = append(result.Artifacts, item)
	}
	return result, nil
}

func isRetentionEligibleStatus(status sqlite.BrowserEncryptedProfileArtifactStatus) bool {
	return status == sqlite.BrowserEncryptedProfileArtifactStatusRevoked ||
		status == sqlite.BrowserEncryptedProfileArtifactStatusExpired
}

func retentionCleanedReason(removed bool) string {
	if removed {
		return "encrypted profile artifact file removed"
	}
	return "encrypted profile artifact file already absent"
}
