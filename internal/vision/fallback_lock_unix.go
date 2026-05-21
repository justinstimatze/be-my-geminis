//go:build unix

package vision

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockedUpdateBudget serializes read-modify-write on the opus budget file
// across processes via flock(2) on a sibling .lock file. Without it, two
// concurrent fallback firings can both read the same SpentUSD, both add
// their cost, and both write back — losing one charge to under-counting.
// Blast radius is small ($0.005 per concurrent firing) but real, so we
// pay one syscall per Opus charge to keep the budget honest.
//
// The lock is held for the duration of the read+mutate+write only. The
// Opus API call itself happens OUTSIDE this function so unrelated
// invocations don't serialize on a slow network call.
//
// Linux and macOS only. Non-unix builds fall through to the unlocked
// path in fallback_lock_other.go (bmg doesn't ship Windows binaries).
func lockedUpdateBudget(path string, mutate func(opusBudgetState) opusBudgetState) error {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return fmt.Errorf("budget lock: mkdir: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("budget lock: open: %w", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("budget lock: flock: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	state := readBudget(path)
	state = mutate(state)
	return writeBudget(path, state)
}
