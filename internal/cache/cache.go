// Package cache is the sha256-keyed disk cache for bmg vision reports.
// Currently: presence-check + atomic write + per-day TTL via manual
// `bmg cache clean --older-than`. An LRU eviction loop with a
// configurable size cap is a future addition.
//
// Cache location, in priority order:
//  1. BMG_CACHE_DIR  (env override)
//  2. $XDG_RUNTIME_DIR/bmg
//  3. /tmp/bmg-cache (fallback)
//
// The cache is intentionally session-scoped: $XDG_RUNTIME_DIR is wiped
// at logout on most Linux distributions, which keeps cached image
// descriptions from accumulating indefinitely without explicit cleanup.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// cacheSchemaVersion is embedded in cached entry filenames so that
// changes to the request shape (model, temperature, prompt order,
// system_instruction composition) implicitly invalidate older entries
// rather than serving stale results to upgrade users. Bump when any
// of those request-shape inputs changes.
//
// History:
//
//	v1 — initial explicit versioning. Bumps from the unversioned (pre-v1)
//	     schema that was paired with: image-then-text part order,
//	     temperature=1.0 default (SDK default), intent appended to the
//	     user prompt, Role:"system" on SystemInstruction.
//	v2 — deliberate path JPEG quality bumped from 70 to 85. Gemini's
//	     response on the same image bytes will differ (better OCR
//	     fidelity at fine-text scales), so v1 entries from before the
//	     quality bump should not be served as v2 results.
//
// Old unversioned files (filename "bmg-<sha>.md") remain on disk but are
// invisible to Get/Put; `bmg cache clean` reclaims them via the
// "bmg-*.md" glob.
const cacheSchemaVersion = "v2"

// Cache is a disk cache keyed by content hash. The zero value is not
// usable; call [New].
type Cache struct {
	dir string
}

// New creates (or opens) a cache rooted at the resolved cache directory.
// Creates the directory with mode 0700 if missing.
//
// If the directory already exists with a wider mode (e.g. 0o777 because
// an attacker pre-created /tmp/bmg-cache on a multi-user box to enable
// a filename-enumeration side channel — cache entries are sha256 of
// image bytes, so a known-image lookup confirms whether the target
// processed it), refuse to use it. Otherwise filenames leak even though
// per-file mode 0o600 protects the content.
func New() (*Cache, error) {
	dir, err := resolveDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("cache: mkdir %s: %w", dir, err)
	}
	// Re-assert mode on the directory whether we just created it or it
	// existed already (MkdirAll is a no-op on existing dirs and never
	// adjusts the mode). On any failure to enforce 0o700, refuse to use
	// the cache so we don't expose filenames.
	if info, statErr := os.Stat(dir); statErr == nil {
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			// Try a fix-up first: if we own the dir, chmod will work; if
			// we don't, this fails and we surface the error clearly.
			if chmodErr := os.Chmod(dir, 0o700); chmodErr != nil {
				return nil, fmt.Errorf("cache: %s has mode %04o (need 0700) and chmod failed: %w; refuse to use to avoid filename leak", dir, perm, chmodErr)
			}
		}
	}
	return &Cache{dir: dir}, nil
}

// Dir returns the resolved cache directory; useful for `bmg doctor`.
func (c *Cache) Dir() string { return c.dir }

// Key returns the canonical sha256 hex digest of payload.
func Key(payload []byte) string {
	h := sha256.Sum256(payload)
	return hex.EncodeToString(h[:])
}

// Path returns the on-disk path where the report for `sha` is/would-be
// stored. The path is "bmg-<schemaVersion>-<sha>.md" so it sorts cleanly,
// the extension hints to anyone browsing /tmp that this is text, and
// schema bumps don't have to physically purge old files — they just
// become invisible to Get/Put. Stats/Clean still see them via the
// broader "bmg-*.md" glob.
func (c *Cache) Path(sha string) string {
	return filepath.Join(c.dir, "bmg-"+cacheSchemaVersion+"-"+sha+".md")
}

// Get returns (path, true) if a cache entry exists for sha. Otherwise
// (path, false). The path is always populated so callers can use it as
// the destination for a subsequent Put.
func (c *Cache) Get(sha string) (string, bool) {
	p := c.Path(sha)
	if _, err := os.Stat(p); err == nil {
		return p, true
	}
	return p, false
}

// Put writes content to the cache atomically (tmp + rename) with mode
// 0600. Returns the final path. Concurrent writers will race; the last
// writer wins. An O_EXCL lock sidecar could be added if races ever
// produce divergent content; for now the content is content-addressable
// so a race produces identical files.
func (c *Cache) Put(sha string, content []byte) (string, error) {
	final := c.Path(sha)
	tmp, err := os.CreateTemp(c.dir, "bmg-"+cacheSchemaVersion+"-"+sha+"-*.tmp")
	if err != nil {
		return "", fmt.Errorf("cache: temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", fmt.Errorf("cache: write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", fmt.Errorf("cache: chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", fmt.Errorf("cache: close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		cleanup()
		return "", fmt.Errorf("cache: rename to %s: %w", final, err)
	}
	return final, nil
}

func resolveDir() (string, error) {
	if d := os.Getenv("BMG_CACHE_DIR"); d != "" {
		return d, nil
	}
	// $XDG_RUNTIME_DIR is the Linux/systemd canonical user-scoped runtime
	// dir (typically /run/user/<uid>, mode 0700). It's unset on macOS,
	// FreeBSD, headless servers, and CI runners.
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "bmg"), nil
	}
	// Cross-platform per-user cache: ~/.cache/bmg on Linux (when
	// XDG_CACHE_HOME is unset), ~/Library/Caches/bmg on macOS, and
	// %LocalAppData%/bmg on Windows. Prefer this over /tmp/bmg-cache
	// because /tmp is world-readable by default on multi-user boxes
	// and persists across reboots on macOS.
	if d, err := os.UserCacheDir(); err == nil && d != "" {
		return filepath.Join(d, "bmg"), nil
	}
	// Last resort. The cache directory mode is re-asserted to 0o700 in
	// New() so a pre-existing world-writable /tmp/bmg-cache is rejected.
	return "/tmp/bmg-cache", nil
}

// Stats returns cache occupancy. Used by `bmg doctor` and `bmg cache stats`.
// Counts only bmg-owned entries (filename "bmg-*.md") so a misconfigured
// BMG_CACHE_DIR pointed at a shared directory doesn't tally unrelated files.
func (c *Cache) Stats() (entries int, bytes int64, err error) {
	matches, err := filepath.Glob(filepath.Join(c.dir, "bmg-*.md"))
	if err != nil {
		return 0, 0, fmt.Errorf("cache: glob: %w", err)
	}
	for _, p := range matches {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		entries++
		bytes += info.Size()
	}
	return entries, bytes, nil
}

// Clean removes bmg-owned cache entries (filename "bmg-*.md") from the
// cache directory. If olderThan > 0, only entries whose mtime is older
// than that duration are removed; otherwise every entry is removed.
//
// Files that fail to stat or remove are skipped silently — the caller
// gets the count of files actually removed and bytes freed. This is
// best-effort by design: a partial clean is preferable to aborting the
// whole sweep on one stuck file.
func (c *Cache) Clean(olderThan time.Duration) (removed int, freedBytes int64, err error) {
	return c.cleanOrPreview(olderThan, false)
}

// Preview returns what Clean WOULD remove given olderThan, without
// actually removing anything. Used by `bmg cache clean --dry-run` so
// the user sees the actual count matching the filter rather than the
// total occupancy.
func (c *Cache) Preview(olderThan time.Duration) (wouldRemove int, wouldFreeBytes int64, err error) {
	return c.cleanOrPreview(olderThan, true)
}

func (c *Cache) cleanOrPreview(olderThan time.Duration, dry bool) (count int, bytes int64, err error) {
	matches, err := filepath.Glob(filepath.Join(c.dir, "bmg-*.md"))
	if err != nil {
		return 0, 0, fmt.Errorf("cache: glob: %w", err)
	}
	cutoff := time.Now().Add(-olderThan)
	for _, p := range matches {
		info, statErr := os.Stat(p)
		if statErr != nil {
			continue
		}
		if olderThan > 0 && info.ModTime().After(cutoff) {
			continue
		}
		size := info.Size()
		if !dry {
			if rmErr := os.Remove(p); rmErr != nil {
				continue
			}
		}
		count++
		bytes += size
	}
	return count, bytes, nil
}
