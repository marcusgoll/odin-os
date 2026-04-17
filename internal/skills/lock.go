package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const skillRegistryLockPollInterval = 25 * time.Millisecond

type registryLockMode int

const (
	registryLockShared registryLockMode = iota
	registryLockExclusive
)

type skillRegistryLock struct {
	file *os.File
	path string
}

func acquireSkillRegistryLock(ctx context.Context, repoRoot string, mode registryLockMode) (*skillRegistryLock, error) {
	lockPath := filepath.Join(repoRoot, "registry", ".skill-mutations.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	for {
		if err := flockSkillRegistry(file, mode); err == nil {
			return &skillRegistryLock{file: file, path: lockPath}, nil
		} else if !isSkillRegistryLockBlocked(err) {
			_ = file.Close()
			return nil, err
		}

		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(skillRegistryLockPollInterval):
		}
	}
}

func (lock *skillRegistryLock) Release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	if err := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN); err != nil {
		_ = lock.file.Close()
		return err
	}
	return lock.file.Close()
}

func flockSkillRegistry(file *os.File, mode registryLockMode) error {
	switch mode {
	case registryLockShared:
		return syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	case registryLockExclusive:
		return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	default:
		return fmt.Errorf("unknown skill registry lock mode %d", mode)
	}
}

func isSkillRegistryLockBlocked(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
