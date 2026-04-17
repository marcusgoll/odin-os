package commands

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestWriteStatusJSON(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	err := WriteStatusJSON(&stdout, StatusView{
		Health:           "healthy",
		PendingApprovals: 2,
		RegistryHealthy:  true,
	})
	if err != nil {
		t.Fatalf("WriteStatusJSON() error = %v", err)
	}

	var payload StatusView
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("status json = %v", err)
	}
	if payload.Health != "healthy" {
		t.Fatalf("Health = %q, want healthy", payload.Health)
	}
	if payload.PendingApprovals != 2 {
		t.Fatalf("PendingApprovals = %d, want 2", payload.PendingApprovals)
	}
	if !payload.RegistryHealthy {
		t.Fatalf("RegistryHealthy = false, want true")
	}
}
