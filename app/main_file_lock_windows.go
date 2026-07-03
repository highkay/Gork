//go:build windows

package app

import (
	"context"
	"os"
	"path/filepath"

	platform "github.com/dslzl/gork/app/platform"
	"golang.org/x/sys/windows"
)

func acquireAppMainSchedulerFileLock(context.Context) (Hook, error) {
	path := platform.DataPath(".scheduler.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return func(context.Context) error { return nil }, nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return func(context.Context) error { return nil }, nil
	}
	var overlapped windows.Overlapped
	err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&overlapped,
	)
	if err != nil {
		_ = file.Close()
		return nil, nil
	}
	return func(context.Context) error {
		err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		return err
	}, nil
}
