package cache

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestPutGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BMG_CACHE_DIR", dir)
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := c.Dir(); got != dir {
		t.Errorf("Dir=%q want %q", got, dir)
	}
	sha := Key([]byte("hello"))
	if _, hit := c.Get(sha); hit {
		t.Error("Get on empty cache should miss")
	}
	path, err := c.Put(sha, []byte("# vision report\n"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !strings.HasPrefix(path, dir) {
		t.Errorf("Put returned %q; expected to live under %s", path, dir)
	}
	got, hit := c.Get(sha)
	if !hit {
		t.Fatal("Get after Put missed")
	}
	if got != path {
		t.Errorf("Get path=%q Put path=%q (should match)", got, path)
	}
	body, _ := os.ReadFile(got)
	if string(body) != "# vision report\n" {
		t.Errorf("body=%q want %q", body, "# vision report\n")
	}
	info, _ := os.Stat(got)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm=%o want 0600", perm)
	}
}

func TestKey_Deterministic(t *testing.T) {
	a := Key([]byte("payload"))
	b := Key([]byte("payload"))
	if a != b {
		t.Errorf("Key not deterministic: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Errorf("sha256 hex should be 64 chars; got %d", len(a))
	}
}

func TestPathContainsSHA(t *testing.T) {
	t.Setenv("BMG_CACHE_DIR", t.TempDir())
	c, _ := New()
	p := c.Path("abc123")
	if !strings.Contains(p, "abc123") {
		t.Errorf("Path %q does not contain sha", p)
	}
	if !strings.HasSuffix(p, ".md") {
		t.Errorf("Path %q should end .md", p)
	}
}

func TestPathEmbedsSchemaVersion(t *testing.T) {
	t.Setenv("BMG_CACHE_DIR", t.TempDir())
	c, _ := New()
	p := c.Path("abc123")
	if !strings.Contains(p, cacheSchemaVersion) {
		t.Errorf("Path %q does not embed cacheSchemaVersion=%q — schema bumps will not invalidate", p, cacheSchemaVersion)
	}
	// Sanity: the unversioned legacy shape ("bmg-<sha>.md") should NOT
	// equal the new shape — otherwise the bump achieves nothing.
	legacy := "bmg-abc123.md"
	if strings.HasSuffix(p, legacy) {
		t.Errorf("Path %q matches legacy unversioned shape %q — old entries will be served as new", p, legacy)
	}
}

func TestPreview_MatchesCleanWithoutRemoving(t *testing.T) {
	// Preview must report what Clean would remove for the same
	// olderThan, but leave the files in place. Regression for the
	// dry-run-lies-about-counts polish item.
	dir := t.TempDir()
	t.Setenv("BMG_CACHE_DIR", dir)
	c, _ := New()
	// Seed three entries: two old, one fresh.
	sha1 := Key([]byte("a"))
	sha2 := Key([]byte("b"))
	sha3 := Key([]byte("c"))
	if _, err := c.Put(sha1, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Put(sha2, []byte("yy")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Put(sha3, []byte("zzz")); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, sha := range []string{sha1, sha2} {
		if err := os.Chtimes(c.Path(sha), old, old); err != nil {
			t.Fatal(err)
		}
	}

	// Preview with --older-than 1h should report 2 entries (the
	// backdated ones), not 3 (the total).
	count, bytes, err := c.Preview(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("Preview count=%d want 2 (only the >1h-old entries)", count)
	}
	if bytes != 3 { // "x"=1 + "yy"=2 = 3 bytes
		t.Errorf("Preview bytes=%d want 3", bytes)
	}

	// Files must still exist — Preview is non-destructive.
	for _, sha := range []string{sha1, sha2, sha3} {
		if _, hit := c.Get(sha); !hit {
			t.Errorf("Preview removed file for sha %s — should be pure read", sha[:12])
		}
	}

	// And Clean with the same filter actually removes the same set.
	removed, freed, err := c.Clean(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != count || freed != bytes {
		t.Errorf("Clean(%v) returned (%d, %d) but Preview(%v) said (%d, %d) — these must match",
			time.Hour, removed, freed, time.Hour, count, bytes)
	}
}

func TestSchemaBumpInvalidatesOldEntries(t *testing.T) {
	// Simulates the upgrade path: an entry written under the pre-v1
	// (unversioned) filename should be invisible to Get under the
	// current schema. We hand-write the legacy filename and confirm
	// Get misses.
	dir := t.TempDir()
	t.Setenv("BMG_CACHE_DIR", dir)
	c, _ := New()
	sha := Key([]byte("payload"))
	legacy := dir + "/bmg-" + sha + ".md"
	if err := os.WriteFile(legacy, []byte("# stale\n"), 0o600); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	if _, hit := c.Get(sha); hit {
		t.Error("Get returned a hit on legacy unversioned entry — schema bump did not invalidate")
	}
	// Stats/Clean glob should STILL see legacy entries so they can be
	// reclaimed by `bmg cache clean`.
	entries, _, err := c.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if entries == 0 {
		t.Error("Stats failed to count legacy entries — cleanup will leak them on disk")
	}
}
