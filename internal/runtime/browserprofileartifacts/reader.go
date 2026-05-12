package browserprofileartifacts

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"odin-os/internal/runtime/browserprofilecrypto"
)

type ReadParams struct {
	ODINRoot     string
	ArtifactPath string
	KeyProvider  KeyProvider
}

func Read(params ReadParams) ([]byte, error) {
	if params.KeyProvider == nil {
		return nil, fmt.Errorf("browser profile artifact reader key provider is required")
	}
	artifactAbs, artifactRel, err := normalizeArtifactPath(params.ODINRoot, params.ArtifactPath)
	if err != nil {
		return nil, err
	}
	if err := rejectForbiddenMetadata(artifactRel); err != nil {
		return nil, err
	}
	material, err := params.KeyProvider()
	if err != nil {
		return nil, err
	}
	key := material.Bytes()
	if len(key) != browserprofilecrypto.KeySize {
		return nil, fmt.Errorf("browser profile artifact reader key must be %d bytes", browserprofilecrypto.KeySize)
	}
	if strings.TrimSpace(material.Ref) == "" {
		return nil, fmt.Errorf("browser profile artifact reader key reference is required")
	}
	if err := rejectForbiddenMetadata(material.Ref); err != nil {
		return nil, err
	}
	encoded, err := os.ReadFile(artifactAbs)
	if err != nil {
		return nil, fmt.Errorf("browser profile artifact reader read artifact: %w", err)
	}
	var envelope browserprofilecrypto.Envelope
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		return nil, fmt.Errorf("browser profile artifact reader decode envelope: %w", err)
	}
	plaintext, err := browserprofilecrypto.Decrypt(key, envelope)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}
