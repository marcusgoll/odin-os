package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const bootstrapLockPollInterval = 25 * time.Millisecond

type BootstrapTimeoutError struct {
	Path    string
	Timeout time.Duration
}

func (err *BootstrapTimeoutError) Error() string {
	return fmt.Sprintf("bootstrap already in progress for %s after waiting %s", err.Path, err.Timeout)
}

type bootstrapLock struct {
	file *os.File
	path string
}

type bootstrapHooks struct {
	afterLockAcquired func()
}

var testBootstrapHooks bootstrapHooks

func acquireBootstrapLock(ctx context.Context, runtimeRoot string) (*bootstrapLock, error) {
	lockPath := filepath.Join(runtimeRoot, "state", "cache", "bootstrap.lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	timeout, hasTimeout, err := bootstrapLockTimeout()
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	start := time.Now()
	for {
		if err := flockNonBlocking(file); err == nil {
			if testBootstrapHooks.afterLockAcquired != nil {
				testBootstrapHooks.afterLockAcquired()
			}
			return &bootstrapLock{file: file, path: lockPath}, nil
		} else if !isWouldBlock(err) {
			_ = file.Close()
			return nil, err
		}

		if hasTimeout && time.Since(start) >= timeout {
			_ = file.Close()
			return nil, &BootstrapTimeoutError{Path: lockPath, Timeout: timeout}
		}

		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(bootstrapLockPollInterval):
		}
	}
}

type serviceLock struct {
	file *os.File
	path string
}

func AcquireServiceLock(runtimeRoot string) (*serviceLock, error) {
	return acquireServiceLock(runtimeRoot)
}

func ServiceLockHeld(runtimeRoot string) (bool, error) {
	lockDir := filepath.Join(runtimeRoot, "state", "cache")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return false, err
	}

	file, err := os.OpenFile(filepath.Join(lockDir, "service.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return false, err
	}
	defer file.Close()

	if err := flockNonBlocking(file); err != nil {
		if isWouldBlock(err) {
			return true, nil
		}
		return false, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
		return false, err
	}
	return false, nil
}

func acquireServiceLock(runtimeRoot string) (*serviceLock, error) {
	lockDir := filepath.Join(runtimeRoot, "state", "cache")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, err
	}

	lockPath := filepath.Join(lockDir, "service.lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	if err := flockNonBlocking(file); err != nil {
		_ = file.Close()
		if isWouldBlock(err) {
			return nil, fmt.Errorf("serve already in progress for %s", lockPath)
		}
		return nil, err
	}

	return &serviceLock{file: file, path: lockPath}, nil
}

func (lock *bootstrapLock) Release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	if err := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN); err != nil {
		_ = lock.file.Close()
		return err
	}
	return lock.file.Close()
}

func (lock *serviceLock) Release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	if err := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN); err != nil {
		_ = lock.file.Close()
		return err
	}
	return lock.file.Close()
}

func bootstrapLockTimeout() (time.Duration, bool, error) {
	raw := os.Getenv("ODIN_BOOTSTRAP_TIMEOUT")
	if raw == "" {
		return 0, false, nil
	}

	timeout, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false, fmt.Errorf("parse ODIN_BOOTSTRAP_TIMEOUT: %w", err)
	}
	if timeout <= 0 {
		return 0, false, fmt.Errorf("ODIN_BOOTSTRAP_TIMEOUT must be greater than zero")
	}
	return timeout, true, nil
}

func flockNonBlocking(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func isWouldBlock(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
