package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFile_CreatesAndChmods(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := atomicWriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content=%q want hello", got)
	}
	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm=%04o want 0600", perm)
	}
}

func TestAtomicWriteFile_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("content=%q want new", got)
	}
	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm after overwrite=%04o want 0600 (new mode should take effect)", perm)
	}
}

func TestAtomicWriteFile_NoTempLeftoverOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := atomicWriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// We should see exactly one file ("out.json") — no .bmg-atomic-*.tmp
	// detritus left behind on the happy path.
	if len(entries) != 1 || entries[0].Name() != "out.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only out.json in dir; got %v", names)
	}
}

func TestAtomicWriteFile_NonexistentDirFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-subdir", "out.json")
	err := atomicWriteFile(path, []byte("x"), 0o600)
	if err == nil {
		t.Fatal("expected error writing into nonexistent directory; got nil")
	}
}
