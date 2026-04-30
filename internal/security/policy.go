package security

import "errors"

var (
	ErrRootWorker       = errors.New("workers must not run as root")
	ErrDangerFullAccess = errors.New("codex danger-full-access mode is forbidden")
)

type WorkerPolicy struct {
	UID         int
	SandboxMode string
}

func ValidateWorkerPolicy(policy WorkerPolicy) error {
	if policy.UID == 0 {
		return ErrRootWorker
	}
	if policy.SandboxMode == "danger-full-access" {
		return ErrDangerFullAccess
	}
	return nil
}
