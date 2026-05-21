//go:build !unix

package vision

// lockedUpdateBudget on non-unix platforms degrades to an unsynchronized
// read-modify-write. bmg's release artifacts are unix-only (Linux,
// macOS); this stub exists so an adventurous porter doesn't hit a build
// failure on day one. If you're using bmg on Windows in earnest, write
// a real lock implementation here and remove this comment.
func lockedUpdateBudget(path string, mutate func(opusBudgetState) opusBudgetState) error {
	state := readBudget(path)
	state = mutate(state)
	return writeBudget(path, state)
}
