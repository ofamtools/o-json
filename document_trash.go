package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// The platform owns trash placement and collision handling. No permanent-delete
// fallback is allowed. The runner is supplied separately so tests never invoke
// a destructive system program.
func trashDocument(ctx context.Context, path string) error {
	return runDocumentTrash(ctx, runtime.GOOS, path, func(cmd *exec.Cmd) error { return cmd.Run() })
}
func runDocumentTrash(ctx context.Context, system, path string, run func(*exec.Cmd) error) error {
	var cmd *exec.Cmd
	switch system {
	case "darwin":
		cmd = exec.CommandContext(ctx, "osascript", "-e", `on run argv
 tell application "Finder" to delete (POSIX file (item 1 of argv))
end run`, path)
	case "linux":
		cmd = exec.CommandContext(ctx, "gio", "trash", "--", path)
	default:
		return fmt.Errorf("system Trash is unsupported on %s; the file was not removed", system)
	}
	if err := run(cmd); err != nil {
		return fmt.Errorf("move to system Trash: %w", err)
	}
	return nil
}
