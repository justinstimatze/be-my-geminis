package apikey

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve_PrecedenceOrder(t *testing.T) {
	clearEnv(t)
	t.Setenv("BMG_API_KEY", "from-bmg")
	t.Setenv("GEMINI_API_KEY", "from-gemini")
	t.Setenv("GOOGLE_API_KEY", "from-google")
	key, src, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "from-bmg" {
		t.Errorf("key=%q want from-bmg (BMG_API_KEY should win)", key)
	}
	if src != "env:BMG_API_KEY" {
		t.Errorf("source=%q want env:BMG_API_KEY", src)
	}
}

func TestResolve_FallsThrough(t *testing.T) {
	clearEnv(t)
	t.Setenv("GOOGLE_API_KEY", "from-google")
	key, src, _ := Resolve()
	if key != "from-google" {
		t.Errorf("key=%q want from-google", key)
	}
	if src != "env:GOOGLE_API_KEY" {
		t.Errorf("source=%q want env:GOOGLE_API_KEY", src)
	}
}

func TestResolve_TrimsWhitespace(t *testing.T) {
	clearEnv(t)
	t.Setenv("GEMINI_API_KEY", "  padded-key\n")
	key, _, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "padded-key" {
		t.Errorf("key=%q want trimmed", key)
	}
}

func TestResolve_ConfigFileFallback(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	bmgDir := filepath.Join(dir, "bmg")
	if err := os.MkdirAll(bmgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(bmgDir, "api_key")
	if err := os.WriteFile(keyPath, []byte("from-config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, src, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "from-config" {
		t.Errorf("key=%q want from-config", key)
	}
	if src != "config:"+keyPath {
		t.Errorf("source=%q want config:%s", src, keyPath)
	}
}

func TestResolve_ErrNotFound(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	_, _, err := Resolve()
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err=%v want ErrNotFound", err)
	}
}

func TestResolve_RejectsWideKeyFileMode(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	bmgDir := filepath.Join(dir, "bmg")
	if err := os.MkdirAll(bmgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(bmgDir, "api_key")
	if err := os.WriteFile(keyPath, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := Resolve()
	if err == nil {
		t.Fatal("expected error for world-readable api_key file; got nil")
	}
	if !strings.Contains(err.Error(), "permissions are too wide") {
		t.Errorf("error %q should mention wide permissions", err.Error())
	}
}

func TestResolve_AcceptsCorrectKeyFileMode(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	bmgDir := filepath.Join(dir, "bmg")
	if err := os.MkdirAll(bmgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(bmgDir, "api_key")
	if err := os.WriteFile(keyPath, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, _, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve on 0600 key file: %v", err)
	}
	if key != "secret" {
		t.Errorf("key=%q want secret", key)
	}
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, v := range EnvVars {
		t.Setenv(v, "")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
}
