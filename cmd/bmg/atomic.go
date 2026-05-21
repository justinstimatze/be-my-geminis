package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// atomicWriteFile writes data to path via the create-temp / write /
// rename pattern so that an interruption (Ctrl-C, OOM, power loss, or
// a concurrent writer truncating the file mid-write) leaves the file
// either at its previous content or at the new content — never half-
// written or empty. This matters most for ~/.claude.json and
// ~/.claude/settings.json which are sensitive files Claude Code itself
// reads on every operation; a naive os.WriteFile that's interrupted
// after open(O_TRUNC) but before write completes leaves the user with
// an empty or truncated file, effectively bricking CC.
//
// Implementation: os.CreateTemp in the same directory (so rename is
// guaranteed to stay on one filesystem and therefore atomic on POSIX)
// → Write → Chmod → Close → Rename. On rename success the temp file
// is consumed; on any earlier failure the temp file is removed.
//
// This does NOT serialize against external writers. If Claude Code
// rewrites the file between the caller's read and this write, CC's
// update is silently lost (overwritten by the caller's stale-with-
// modifications version). The .bmg-backup-<timestamp> sibling created
// by callers IS the recovery path for that case. Document loudly:
// users should not run `bmg init` while CC sessions are active.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".bmg-atomic-*.tmp")
	if err != nil {
		return fmt.Errorf("atomic write: create temp in %s: %w", dir, err)
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("atomic write: write %s: %w", name, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("atomic write: chmod %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("atomic write: close %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		cleanup()
		return fmt.Errorf("atomic write: rename to %s: %w", path, err)
	}
	return nil
}
