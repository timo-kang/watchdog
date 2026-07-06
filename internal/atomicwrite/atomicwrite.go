// Package atomicwrite provides atomic file writes with an optional
// power-loss-durable variant. Both write to a temp file in the target
// directory and rename into place, so a crash never leaves a partially
// written target. WriteDurable additionally fsyncs the file and its parent
// directory so the data survives power loss, at the cost of extra flash I/O.
package atomicwrite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// WriteDurable atomically writes data to path with the given mode, fsyncing the
// file and its parent directory so the write survives power loss.
func WriteDurable(path string, data []byte, mode os.FileMode) error {
	return write(path, data, mode, true)
}

// WriteAtomic atomically writes data to path with the given mode via a
// temp-file-and-rename, without fsyncing.
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	return write(path, data, mode, false)
}

func write(path string, data []byte, mode os.FileMode, durable bool) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".atomic-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if durable {
		if err := tmp.Sync(); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("fsync temp file: %w", err)
		}
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	cleanup = false

	if durable {
		if err := fsyncDir(dir); err != nil {
			return fmt.Errorf("fsync dir: %w", err)
		}
	}
	return nil
}

// fsyncDir fsyncs a directory so a rename is durable. Filesystems that do not
// support directory fsync report EINVAL/ENOTSUP; those are treated as success.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
			return nil
		}
		return err
	}
	return nil
}
