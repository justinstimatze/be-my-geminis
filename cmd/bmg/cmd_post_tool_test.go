package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/justinstimatze/be-my-geminis/internal/hook"
)

// writeTestPNG drops a small valid PNG at path. Used by the cwd
// recency scan + isUsableImage gating tests so the candidates we
// extract are actually readable.
func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func makePostToolInput(t *testing.T, cwd, command, stdout string) *hook.Input {
	t.Helper()
	ti, _ := json.Marshal(map[string]string{"command": command})
	tr, _ := json.Marshal(map[string]string{"stdout": stdout, "stderr": ""})
	return &hook.Input{
		CWD:           cwd,
		ToolName:      "Bash",
		ToolInput:     ti,
		ToolResponse:  tr,
		HookEventName: "PostToolUse",
	}
}

func TestExtractImagePaths_StdoutPathMatch(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "out.png")
	writeTestPNG(t, imgPath)

	in := makePostToolInput(t, dir,
		"python -c 'import matplotlib.pyplot as plt; plt.savefig(\"out.png\")'",
		"Figure saved to out.png\n",
	)
	got := extractImagePaths(in)
	if len(got) == 0 {
		t.Fatal("expected at least 1 path from stdout match + cwd recency")
	}
	// Both the stdout match and the cwd recency scan should produce
	// the same path — dedup keeps it to one entry.
	found := false
	for _, p := range got {
		if p == imgPath {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %q in extracted paths; got %v", imgPath, got)
	}
}

func TestExtractImagePaths_DedupAcrossSources(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "screenshot.jpg")
	writeTestPNG(t, imgPath) // bytes don't have to be valid JPEG for the path test

	in := makePostToolInput(t, dir,
		"./screenshot.jpg",
		"saved screenshot.jpg",
	)
	got := extractImagePaths(in)
	// stdout mentions it, command mentions it, cwd recency scan picks
	// it up — dedup should give exactly one.
	count := 0
	for _, p := range got {
		if p == imgPath {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 entry for %q (dedup); got %d in %v", imgPath, count, got)
	}
}

func TestExtractImagePaths_IgnoresOldFiles(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.png")
	writeTestPNG(t, oldPath)
	// Backdate the file outside the recent-image window.
	old := time.Now().Add(-recentImageWindow * 2)
	if err := os.Chtimes(oldPath, old, old); err != nil {
		t.Fatal(err)
	}

	in := makePostToolInput(t, dir,
		"echo hello",
		"hello\n",
	)
	got := extractImagePaths(in)
	for _, p := range got {
		if p == oldPath {
			t.Errorf("expected old file %q to be skipped (mtime older than %s)", p, recentImageWindow)
		}
	}
}

func TestExtractImagePaths_NoImagesInOutput(t *testing.T) {
	dir := t.TempDir()
	in := makePostToolInput(t, dir,
		"ls -la",
		"total 0\n",
	)
	got := extractImagePaths(in)
	if len(got) != 0 {
		t.Errorf("expected no paths from a no-image Bash invocation; got %v", got)
	}
}

func TestExtractImagePaths_SkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	imgDirPath := filepath.Join(dir, "fake.png")
	if err := os.Mkdir(imgDirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	in := makePostToolInput(t, dir,
		"mkdir fake.png",
		"",
	)
	got := extractImagePaths(in)
	for _, p := range got {
		if p == imgDirPath {
			t.Errorf("extracted a directory %q as an image path", p)
		}
	}
}

func TestExtractImagePaths_AbsolutePathFromOutput(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "abs.png")
	writeTestPNG(t, imgPath)

	// Cwd is unrelated; only the stdout absolute path should match.
	in := makePostToolInput(t, "/some/other/dir",
		"convert in.jpg "+imgPath,
		"converted to "+imgPath,
	)
	got := extractImagePaths(in)
	found := false
	for _, p := range got {
		if p == imgPath {
			found = true
		}
	}
	if !found {
		t.Errorf("expected absolute path %q from stdout; got %v", imgPath, got)
	}
}

func TestExtractImagePaths_MultipleDistinct(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.jpg")
	writeTestPNG(t, a)
	writeTestPNG(t, b)

	in := makePostToolInput(t, dir,
		"do_things",
		"wrote a.png and b.jpg",
	)
	got := extractImagePaths(in)
	sort.Strings(got)
	wantA, wantB := false, false
	for _, p := range got {
		if p == a {
			wantA = true
		}
		if p == b {
			wantB = true
		}
	}
	if !wantA || !wantB {
		t.Errorf("expected both %q and %q; got %v", a, b, got)
	}
}
