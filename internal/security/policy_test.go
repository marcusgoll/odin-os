package security

import (
	"errors"
	"testing"
)

func TestValidateWorkerPolicyRejectsRoot(t *testing.T) {
	t.Parallel()

	err := ValidateWorkerPolicy(WorkerPolicy{UID: 0, SandboxMode: "workspace-write"})
	if !errors.Is(err, ErrRootWorker) {
		t.Fatalf("ValidateWorkerPolicy() error = %v, want %v", err, ErrRootWorker)
	}
}

func TestValidateWorkerPolicyRejectsDangerFullAccess(t *testing.T) {
	t.Parallel()

	err := ValidateWorkerPolicy(WorkerPolicy{UID: 1000, SandboxMode: "danger-full-access"})
	if !errors.Is(err, ErrDangerFullAccess) {
		t.Fatalf("ValidateWorkerPolicy() error = %v, want %v", err, ErrDangerFullAccess)
	}
}

func TestValidateWorkerPolicyRejectsRootBeforeSandboxMode(t *testing.T) {
	t.Parallel()

	err := ValidateWorkerPolicy(WorkerPolicy{UID: 0, SandboxMode: "danger-full-access"})
	if !errors.Is(err, ErrRootWorker) {
		t.Fatalf("ValidateWorkerPolicy() error = %v, want %v", err, ErrRootWorker)
	}
}

func TestValidateWorkerPolicyAllowsNonRootWorkspaceWrite(t *testing.T) {
	t.Parallel()

	err := ValidateWorkerPolicy(WorkerPolicy{UID: 1000, SandboxMode: "workspace-write"})
	if err != nil {
		t.Fatalf("ValidateWorkerPolicy() error = %v, want nil", err)
	}
}
