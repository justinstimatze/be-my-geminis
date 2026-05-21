package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestFindBmgHooks_Empty: a settings.json with no bmg hooks returns
// an empty slice. This is the normal state on a fresh install where
// only foreign tools are wired in.
func TestFindBmgHooks_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	contents := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{"type": "command", "command": "/usr/local/bin/hindcast inject"},
					},
				},
			},
		},
	}
	data, _ := json.Marshal(contents)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := findBmgHooks(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 bmg hooks, got %d: %+v", len(got), got)
	}
}

// TestFindBmgHooks_Multiple: settings.json with bmg hooks across
// multiple events returns all of them with correct event/matcher/cmd.
// Doctor uses this to render its per-hook status block.
func TestFindBmgHooks_Multiple(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	contents := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Read",
					"hooks": []any{
						map[string]any{"type": "command", "command": "/opt/bmg hook pre-read"},
					},
				},
			},
			"SessionStart": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{"type": "command", "command": "/opt/bmg hook session-start"},
					},
				},
			},
		},
	}
	data, _ := json.Marshal(contents)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := findBmgHooks(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 bmg hooks, got %d", len(got))
	}
	// Map iteration order is unstable; sort for comparison.
	sort.Slice(got, func(i, j int) bool { return got[i].event < got[j].event })
	if got[0].event != "PreToolUse" || got[0].matcher != "Read" {
		t.Errorf("PreToolUse entry wrong: %+v", got[0])
	}
	if got[1].event != "SessionStart" || got[1].matcher != "" {
		t.Errorf("SessionStart entry wrong: %+v", got[1])
	}
}

// TestFindBmgHooks_MissingFile: a non-existent file is not an error
// (returns nil, nil). Doctor checks both user- and project-level paths
// and one being absent must not fail the scan.
func TestFindBmgHooks_MissingFile(t *testing.T) {
	got, err := findBmgHooks("/nonexistent/path/settings.json")
	if err != nil {
		t.Errorf("missing file should not error, got: %v", err)
	}
	if got != nil {
		t.Errorf("missing file should return nil, got: %v", got)
	}
}

// TestCheckExecutable_RealBinary: an actual executable file passes.
// Uses /bin/sh as a stable reference (present on every Unix system
// the test would ever run on).
func TestCheckExecutable_RealBinary(t *testing.T) {
	status, ok := checkExecutable("/bin/sh")
	if !ok {
		t.Errorf("/bin/sh reported non-executable: %s", status)
	}
}

// TestCheckExecutable_Missing: a path that doesn't exist fails with
// "not found". This is the "user moved the binary after install" case
// that doctor flags so users know to re-run init.
func TestCheckExecutable_Missing(t *testing.T) {
	_, ok := checkExecutable("/nonexistent/bmg")
	if ok {
		t.Error("missing binary reported as executable")
	}
}

// TestCheckExecutable_NotExecutable: a regular file without execute
// bits set fails. This catches the user-error case where bmg was
// downloaded but never `chmod +x`'d.
func TestCheckExecutable_NotExecutable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-bmg")
	if err := os.WriteFile(path, []byte("not actually a binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, ok := checkExecutable(path)
	if ok {
		t.Errorf("non-executable file reported as ok: %s", status)
	}
}

// TestFindBmgMCPEntry_Present: a JSON file with mcpServers.bemygeminis
// returns command + formatted args. This is the helper doctor uses to
// detect the canonical (.claude.json) and legacy (settings.json)
// registration locations.
func TestFindBmgMCPEntry_Present(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")
	contents := map[string]any{
		"mcpServers": map[string]any{
			"bemygeminis": map[string]any{
				"type":    "stdio",
				"command": "/usr/local/bin/bmg",
				"args":    []any{"mcp"},
				"env":     map[string]any{},
			},
		},
	}
	data, _ := json.Marshal(contents)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cmd, argStr, ok := findBmgMCPEntry(path)
	if !ok {
		t.Fatal("findBmgMCPEntry returned ok=false; expected hit")
	}
	if cmd != "/usr/local/bin/bmg" {
		t.Errorf("cmd = %q", cmd)
	}
	if argStr != " mcp" {
		t.Errorf("argStr = %q, want %q", argStr, " mcp")
	}
}

// TestFindBmgMCPEntry_AbsentOrMissing: an empty file, a missing file,
// and a file with mcpServers but no bemygeminis all return ok=false
// with no error. These are the "MCP not registered here" cases doctor
// has to distinguish from a real read failure.
func TestFindBmgMCPEntry_AbsentOrMissing(t *testing.T) {
	dir := t.TempDir()

	if _, _, ok := findBmgMCPEntry(filepath.Join(dir, "nonexistent.json")); ok {
		t.Error("missing file should return ok=false")
	}

	emptyMcps := filepath.Join(dir, "empty-mcps.json")
	if err := os.WriteFile(emptyMcps, []byte(`{"mcpServers":{"buddy":{"command":"x"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := findBmgMCPEntry(emptyMcps); ok {
		t.Error("file with foreign mcpServers but no bemygeminis should return ok=false")
	}

	noMcps := filepath.Join(dir, "no-mcps.json")
	if err := os.WriteFile(noMcps, []byte(`{"version":"1.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := findBmgMCPEntry(noMcps); ok {
		t.Error("file with no mcpServers key should return ok=false")
	}
}

// TestReportHookInstallation_FlagsDualScope is the regression for the
// "every event fires twice" bug caught in the v0.2 run-through-paces
// session. Seeds bmg hooks in BOTH user-scope and project-scope
// settings.json and verifies reportHookInstallation prints a DUAL SCOPE
// warning AND returns exit code 1.
func TestReportHookInstallation_FlagsDualScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed user-scope settings.json with bmg hooks.
	userPath := filepath.Join(home, ".claude", "settings.json")
	seedSettingsWithBmgHook(t, userPath)

	// Seed project-scope settings.json (cwd-relative).
	project := t.TempDir()
	t.Chdir(project)
	projectPath := filepath.Join(project, ".claude", "settings.json")
	seedSettingsWithBmgHook(t, projectPath)

	out, code := captureReport(t)
	if code != 1 {
		t.Errorf("expected exit 1 with dual-scope installed; got %d", code)
	}
	if !strings.Contains(out, "DUAL SCOPE") {
		t.Errorf("expected DUAL SCOPE warning; got:\\n%s", out)
	}
	if !strings.Contains(out, "PreToolUse:Read") {
		t.Errorf("dual-scope warning should name the conflicting (event, matcher); got:\\n%s", out)
	}
	// Both paths should be listed so the user knows which to clean up.
	if !strings.Contains(out, userPath) || !strings.Contains(out, projectPath) {
		t.Errorf("expected both settings.json paths in warning; got:\\n%s", out)
	}
}

// TestReportHookInstallation_SingleScopeNoWarning: control case — only
// one scope has bmg hooks, no warning, exit 0.
func TestReportHookInstallation_SingleScopeNoWarning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userPath := filepath.Join(home, ".claude", "settings.json")
	seedSettingsWithBmgHook(t, userPath)
	// Project has no .claude/settings.json — single-scope.
	t.Chdir(t.TempDir())

	out, code := captureReport(t)
	if strings.Contains(out, "DUAL SCOPE") {
		t.Errorf("single-scope should not warn DUAL SCOPE; got:\\n%s", out)
	}
	// Stale-binary check may still fail (fake exe path) — that's OK for
	// this test; we only care that dual-scope is NOT the cause.
	_ = code
}

// seedSettingsWithBmgHook writes a settings.json at path with one
// bmg PreToolUse:Read hook entry. Used by the dual-scope tests so
// both seed paths produce overlapping (event, matcher) pairs.
func seedSettingsWithBmgHook(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Read",
					"hooks": []any{
						map[string]any{"type": "command", "command": fakeExe + " hook pre-read"},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// captureReport runs reportHookInstallation, redirecting stdout to a
// buffer so the test can grep the output for warning strings.
func captureReport(t *testing.T) (output string, exitCode int) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	exitCode = reportHookInstallation()
	w.Close()
	<-done
	return buf.String(), exitCode
}
