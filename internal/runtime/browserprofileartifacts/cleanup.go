package browserprofileartifacts

import (
	"fmt"
	"os"
)

type CleanupParams struct {
	ODINRoot     string
	ArtifactPath string
}

type CleanupResult struct {
	ArtifactPath string `json:"artifact_path"`
	Removed      bool   `json:"removed"`
}

func Cleanup(params CleanupParams) (CleanupResult, error) {
	artifactAbs, artifactRel, err := normalizeArtifactPath(params.ODINRoot, params.ArtifactPath)
	if err != nil {
		return CleanupResult{}, err
	}
	if err := rejectForbiddenMetadata(artifactRel); err != nil {
		return CleanupResult{}, err
	}
	if err := os.Remove(artifactAbs); err != nil {
		if os.IsNotExist(err) {
			return CleanupResult{ArtifactPath: artifactRel, Removed: false}, nil
		}
		return CleanupResult{}, fmt.Errorf("browser profile artifact cleanup remove artifact: %w", err)
	}
	return CleanupResult{ArtifactPath: artifactRel, Removed: true}, nil
}
