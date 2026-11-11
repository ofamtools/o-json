package main

import (
	"errors"
	"io"
	"os"
	"syscall"
)

// A hard link claims the destination atomically on one filesystem. Across
// filesystems, copy into an exclusively created file and sync before unlinking
// the source. An error never falls back to replacing a destination.
func moveDocumentFile(from, to string, link func(string, string) error) error {
	if err := link(from, to); err == nil {
		return os.Remove(from)
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	target, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(target, source)
	if copyErr == nil {
		copyErr = target.Sync()
	}
	closeErr := target.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return errors.Join(err, os.Remove(to))
	}
	return os.Remove(from)
}
