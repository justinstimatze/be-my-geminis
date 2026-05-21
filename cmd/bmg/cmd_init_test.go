package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeExe = "/usr/local/bin/bmg"

// TestMergeFreshInstall: no existing settings.json. Merge creates one
// with the bmg PreToolUse:Read hook and reports changed=true with no
// backup (nothing to back up).
func TestMergeFreshInstall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	changed, backup, err := mergeClaudeSettings(path, fakeExe)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !changed {
		t.Fatal("changed=false on fresh install")
	}
	if backup != "" {
		t.Errorf("unexpected backup on fresh install: %q", backup)
	}

	var settings map[string]any
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("written settings.json invalid: %v", err)
	}
	hooks := settings["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Fatalf("expected 1 PreToolUse entry, got %d", len(pre))
	}
	entry := pre[0].(map[string]any)
	if entry["matcher"] != "Read" {
		t.Errorf("matcher = %v, want Read", entry["matcher"])
	}
	inner := entry["hooks"].([]any)[0].(map[string]any)
	if inner["command"] != fakeExe+" hook pre-read" {
		t.Errorf("command = %v", inner["command"])
	}

	// settings.json MUST NOT carry an mcpServers block anymore — that
	// moved to ~/.claude.json. CC ignores mcpServers in settings.json
	// at any scope, so leaving an entry here would be misleading.
	if _, present := settings["mcpServers"]; present {
		t.Errorf("settings.json should not contain mcpServers (CC ignores them here); got: %s", raw)
	}
}

// TestMergeIdempotent: running merge twice produces no second-pass
// changes. This is the load-bearing property — `bmg init` should be
// safe to re-run after upgrades or in shell-init scripts.
func TestMergeIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if _, _, err := mergeClaudeSettings(path, fakeExe); err != nil {
		t.Fatal(err)
	}
	changed, _, err := mergeClaudeSettings(path, fakeExe)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("second merge reported changed=true; expected no-op")
	}
}

// TestMergeReplacesStalePath: a settings.json with a bmg hook pointing
// at a stale install location must be rewritten to the new path on
// re-install. Without this, users who move the binary accumulate
// duplicate entries and the old path silently takes precedence.
func TestMergeReplacesStalePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	stale := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Read",
					"hooks": []any{
						map[string]any{"type": "command", "command": "/tmp/bmg-test/bmg hook pre-read"},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(stale, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	changed, backup, err := mergeClaudeSettings(path, fakeExe)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true when replacing stale path")
	}
	if backup == "" {
		t.Error("expected a backup to be written")
	}

	var settings map[string]any
	raw, _ := os.ReadFile(path)
	_ = json.Unmarshal(raw, &settings)
	pre := settings["hooks"].(map[string]any)["PreToolUse"].([]any)
	commands := []string{}
	for _, e := range pre {
		em := e.(map[string]any)
		for _, h := range em["hooks"].([]any) {
			commands = append(commands, h.(map[string]any)["command"].(string))
		}
	}
	if len(commands) != 1 {
		t.Errorf("expected 1 bmg command after replace, got %d: %v", len(commands), commands)
	}
	if commands[0] != fakeExe+" hook pre-read" {
		t.Errorf("stale path not replaced; got %q", commands[0])
	}
}

// TestMergePreservesForeignHooks: hooks belonging to other tools
// (hindcast, slimemold, plancheck) must survive a bmg install. Cross-
// tool coexistence is the foundation of Claude Code's hook ecosystem;
// any merge that clobbers foreign hooks would silently break the
// user's other tooling.
func TestMergePreservesForeignHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	existing := map[string]any{
		"hooks": map[string]any{
			"PostToolUse": []any{
				map[string]any{
					"matcher": "Edit|Write",
					"hooks": []any{
						map[string]any{"type": "command", "command": "/home/u/.claude/hooks/defn-sync.sh"},
					},
				},
			},
			"SessionStart": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{"type": "command", "command": "/home/u/go/bin/hindcast inject"},
					},
				},
			},
			"PreToolUse": []any{
				map[string]any{
					"matcher": "ExitPlanMode",
					"hooks": []any{
						map[string]any{"type": "command", "command": "/home/u/.claude/hooks/plancheck-gate.sh"},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := mergeClaudeSettings(path, fakeExe); err != nil {
		t.Fatal(err)
	}

	var settings map[string]any
	raw, _ := os.ReadFile(path)
	_ = json.Unmarshal(raw, &settings)

	hooks := settings["hooks"].(map[string]any)

	// PostToolUse: foreign defn-sync survives AND bmg post-tool gets
	// added. The foreign Edit|Write matcher is distinct from bmg's
	// Bash matcher so they live in separate entries.
	post := hooks["PostToolUse"].([]any)
	if len(post) != 2 {
		t.Errorf("PostToolUse expected 2 entries (defn-sync + bmg post-tool), got %d", len(post))
	}
	foundDefnSync, foundBmgPostTool := false, false
	for _, e := range post {
		em := e.(map[string]any)
		for _, h := range em["hooks"].([]any) {
			cmd := h.(map[string]any)["command"].(string)
			if cmd == "/home/u/.claude/hooks/defn-sync.sh" {
				foundDefnSync = true
			}
			if cmd == fakeExe+" hook post-tool" {
				foundBmgPostTool = true
			}
		}
	}
	if !foundDefnSync {
		t.Error("foreign defn-sync hook lost from PostToolUse — install clobbered other tooling")
	}
	if !foundBmgPostTool {
		t.Error("bmg post-tool hook not registered in PostToolUse")
	}

	// SessionStart has hindcast (foreign) AND new bmg session-start entry.
	sess := hooks["SessionStart"].([]any)
	if len(sess) != 2 {
		t.Errorf("SessionStart expected 2 entries (hindcast + bmg), got %d", len(sess))
	}
	foundHindcast, foundBmgSession := false, false
	for _, e := range sess {
		em := e.(map[string]any)
		for _, h := range em["hooks"].([]any) {
			cmd := h.(map[string]any)["command"].(string)
			if cmd == "/home/u/go/bin/hindcast inject" {
				foundHindcast = true
			}
			if cmd == fakeExe+" hook session-start" {
				foundBmgSession = true
			}
		}
	}
	if !foundHindcast {
		t.Error("hindcast SessionStart hook lost during bmg install")
	}
	if !foundBmgSession {
		t.Error("bmg session-start hook not registered")
	}

	// PreToolUse has plancheck-gate AND new bmg Read entry.
	pre := hooks["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Errorf("PreToolUse expected 2 entries (plancheck + bmg), got %d", len(pre))
	}
	foundPlancheck, foundBmg := false, false
	for _, e := range pre {
		em := e.(map[string]any)
		for _, h := range em["hooks"].([]any) {
			cmd := h.(map[string]any)["command"].(string)
			if cmd == "/home/u/.claude/hooks/plancheck-gate.sh" {
				foundPlancheck = true
			}
			if cmd == fakeExe+" hook pre-read" {
				foundBmg = true
			}
		}
	}
	if !foundPlancheck {
		t.Error("plancheck-gate hook lost during bmg install")
	}
	if !foundBmg {
		t.Error("bmg hook not registered")
	}
}

// TestUnmergeRoundTrip: install + uninstall → settings.json byte-equal
// to original (modulo formatting). This is the property that lets
// users uninstall bmg cleanly without leaving residue in their
// settings file.
func TestUnmergeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	original := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{"type": "command", "command": "/home/u/go/bin/hindcast inject"},
					},
				},
			},
		},
	}
	canonicalPre, _ := json.MarshalIndent(original, "", "  ")
	if err := os.WriteFile(path, append(canonicalPre, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := mergeClaudeSettings(path, fakeExe); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := unmergeClaudeSettings(path); err != nil {
		t.Fatalf("unmerge: %v", err)
	}

	got, _ := os.ReadFile(path)
	want := append(canonicalPre, '\n')
	if string(got) != string(want) {
		t.Errorf("round-trip differs:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestMergeStripsLegacyMCPFromSettings: a settings.json carrying a
// stale mcpServers.bemygeminis entry (from a pre-fix version of
// `bmg init --enable-mcp` that wrote MCP to the wrong file) must be
// cleaned up on the next install. CC ignores mcpServers in
// settings.json at any scope, so the entry is misleading noise.
// Foreign mcpServers entries (if any) survive — only bemygeminis is
// stripped.
func TestMergeStripsLegacyMCPFromSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	stale := map[string]any{
		"mcpServers": map[string]any{
			"bemygeminis": map[string]any{
				"command": "/tmp/bmg-test/bmg",
				"args":    []any{"mcp"},
				"env":     map[string]any{},
			},
			"foreign": map[string]any{
				"type":    "stdio",
				"command": "/some/other/binary",
				"args":    []any{"mcp"},
				"env":     map[string]any{},
			},
		},
	}
	data, _ := json.MarshalIndent(stale, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	changed, _, err := mergeClaudeSettings(path, fakeExe)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true when stripping legacy mcpServers entry")
	}

	var settings map[string]any
	raw, _ := os.ReadFile(path)
	_ = json.Unmarshal(raw, &settings)
	mcps, _ := settings["mcpServers"].(map[string]any)
	if mcps == nil {
		t.Fatal("foreign mcpServers entry was lost during legacy cleanup")
	}
	if _, present := mcps["bemygeminis"]; present {
		t.Errorf("bemygeminis still present after merge cleanup: %s", raw)
	}
	if _, present := mcps["foreign"]; !present {
		t.Errorf("foreign mcpServers entry lost: %s", raw)
	}
}

// TestMergeStripsLegacyMCP_EmptyResult: when bemygeminis is the ONLY
// mcpServers entry, stripping it removes the whole mcpServers key so
// the file doesn't accumulate an empty container.
func TestMergeStripsLegacyMCP_EmptyResult(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	stale := map[string]any{
		"mcpServers": map[string]any{
			"bemygeminis": map[string]any{
				"command": "/tmp/bmg-test/bmg",
				"args":    []any{"mcp"},
				"env":     map[string]any{},
			},
		},
	}
	data, _ := json.MarshalIndent(stale, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := mergeClaudeSettings(path, fakeExe); err != nil {
		t.Fatal(err)
	}

	var settings map[string]any
	raw, _ := os.ReadFile(path)
	_ = json.Unmarshal(raw, &settings)
	if _, present := settings["mcpServers"]; present {
		t.Errorf("empty mcpServers should be dropped, got: %s", raw)
	}
}

// TestUnmergeMissingFile: uninstalling when no settings.json exists is
// a no-op (changed=false, no error). Users can run uninstall safely
// even if they never ran init.
func TestUnmergeMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	changed, err := unmergeClaudeSettings(path)
	if err != nil {
		t.Fatalf("unmerge missing file: %v", err)
	}
	if changed {
		t.Error("changed=true on missing file")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("uninstall created a settings.json when none existed")
	}
}

// TestMergeClaudeJSONFresh: no existing ~/.claude.json. Merge creates
// one with mcpServers.bemygeminis registered. The entry uses the
// "type": "stdio" shape matching working servers in real CC configs.
func TestMergeClaudeJSONFresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	changed, backup, err := mergeClaudeJSON(path, fakeExe)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !changed {
		t.Fatal("changed=false on fresh install")
	}
	if backup != "" {
		t.Errorf("unexpected backup on fresh install: %q", backup)
	}

	var root map[string]any
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("written .claude.json invalid: %v", err)
	}
	mcps, ok := root["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers missing; root:\n%s", raw)
	}
	bmg, ok := mcps["bemygeminis"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers.bemygeminis missing")
	}
	if bmg["type"] != "stdio" {
		t.Errorf(`bemygeminis.type = %v, want "stdio"`, bmg["type"])
	}
	if bmg["command"] != fakeExe {
		t.Errorf("bemygeminis.command = %v, want %s", bmg["command"], fakeExe)
	}
	args, _ := bmg["args"].([]any)
	if len(args) != 1 || args[0] != "mcp" {
		t.Errorf("bemygeminis.args = %v, want [mcp]", args)
	}
}

// TestUninstall_ProjectScope_PreservesUserMCP is the regression for
// the "uninstall --project blasted my user-scope MCP" bug discovered
// during the v0.2 run-through-paces session. The semantic contract:
// --project means "this project only" — user-scope state (notably
// the MCP registration in ~/.claude.json) must be left alone.
func TestUninstall_ProjectScope_PreservesUserMCP(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// Seed ~/.claude.json with bmg's MCP entry.
	claudeJSON := filepath.Join(home, ".claude.json")
	initial := map[string]any{
		"mcpServers": map[string]any{
			"bemygeminis": map[string]any{
				"type":    "stdio",
				"command": "/foo/bmg",
				"args":    []any{"mcp"},
				"env":     map[string]any{},
			},
		},
	}
	raw, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(claudeJSON, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	// Create a project dir with a .claude/settings.json containing a
	// bmg hook (so unmergeClaudeSettings has something to remove).
	projectDir := t.TempDir()
	projectSettings := filepath.Join(projectDir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(projectSettings), 0o700); err != nil {
		t.Fatal(err)
	}
	projectInitial := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Read",
					"hooks": []any{
						map[string]any{"type": "command", "command": "/foo/bmg hook pre-read"},
					},
				},
			},
		},
	}
	pj, _ := json.MarshalIndent(projectInitial, "", "  ")
	if err := os.WriteFile(projectSettings, pj, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(projectDir)

	// Run `bmg uninstall --project` — the regression target.
	cmdUninstall([]string{"--project"})

	// User-scope ~/.claude.json must still have bemygeminis.
	got, err := os.ReadFile(claudeJSON)
	if err != nil {
		t.Fatalf("post-uninstall ~/.claude.json missing: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(got, &root); err != nil {
		t.Fatalf("post-uninstall ~/.claude.json invalid JSON: %v", err)
	}
	mcps, _ := root["mcpServers"].(map[string]any)
	if mcps == nil {
		t.Fatal("--project uninstall blasted mcpServers; the user-scope MCP registration must survive")
	}
	if _, ok := mcps["bemygeminis"]; !ok {
		t.Error("--project uninstall removed user-scope bemygeminis MCP — regression of the uninstall-blasts-MCP bug")
	}

	// And the project-scope settings.json should now have bmg hooks
	// stripped — i.e. the --project removal DID happen.
	projGot, err := os.ReadFile(projectSettings)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(projGot), "bmg hook") {
		t.Errorf("project settings.json still contains bmg hook after uninstall --project; got:\n%s", projGot)
	}
}

// TestMergeClaudeJSONIdempotent: a second merge of an unchanged file
// must report changed=false. This lets `bmg init --enable-mcp` be
// safely re-run.
func TestMergeClaudeJSONIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	if _, _, err := mergeClaudeJSON(path, fakeExe); err != nil {
		t.Fatal(err)
	}
	changed, _, err := mergeClaudeJSON(path, fakeExe)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("second merge reported changed=true; expected no-op")
	}
}

// TestMergeClaudeJSONPreservesForeign: foreign mcpServers entries
// (buddy, plancheck, etc.) and unrelated top-level keys (projects,
// version, billing state) must survive a bmg MCP install. The real
// ~/.claude.json carries large amounts of Claude-managed state.
func TestMergeClaudeJSONPreservesForeign(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	existing := map[string]any{
		"mcpServers": map[string]any{
			"buddy": map[string]any{
				"type":    "stdio",
				"command": "node",
				"args":    []any{"/home/u/.buddy/server.js"},
				"env":     map[string]any{},
			},
			"plancheck": map[string]any{
				"type":    "stdio",
				"command": "/home/u/Documents/plancheck/plancheck",
				"args":    []any{"mcp"},
				"env":     map[string]any{"PLANCHECK_SHARE": "full"},
			},
		},
		"projects": map[string]any{
			"/home/u/Documents/foo": map[string]any{
				"lastSessionFirstPrompt":     "do the thing",
				"projectOnboardingSeenCount": float64(3),
			},
		},
		"firstStartTime":         "2026-01-01T00:00:00Z",
		"hasCompletedOnboarding": true,
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := mergeClaudeJSON(path, fakeExe); err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	raw, _ := os.ReadFile(path)
	_ = json.Unmarshal(raw, &root)

	mcps := root["mcpServers"].(map[string]any)
	if _, ok := mcps["buddy"]; !ok {
		t.Error("buddy MCP server lost during bmg install")
	}
	if _, ok := mcps["plancheck"]; !ok {
		t.Error("plancheck MCP server lost during bmg install")
	}
	if _, ok := mcps["bemygeminis"]; !ok {
		t.Error("bemygeminis MCP server not registered")
	}
	if buddy := mcps["buddy"].(map[string]any); buddy["type"] != "stdio" {
		t.Errorf("buddy.type field corrupted: %v", buddy["type"])
	}

	// Top-level Claude-managed keys must survive.
	if _, ok := root["projects"]; !ok {
		t.Error("projects key lost during bmg install")
	}
	if root["firstStartTime"] != "2026-01-01T00:00:00Z" {
		t.Errorf("firstStartTime corrupted: %v", root["firstStartTime"])
	}
	if root["hasCompletedOnboarding"] != true {
		t.Errorf("hasCompletedOnboarding corrupted: %v", root["hasCompletedOnboarding"])
	}
}

// TestMergeClaudeJSONReplacesStalePath: a ~/.claude.json containing
// a bemygeminis entry with an old binary path must have that path
// rewritten on re-install. Same property as the hooks merge — moving
// the binary should not require a separate cleanup step.
func TestMergeClaudeJSONReplacesStalePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	stale := map[string]any{
		"mcpServers": map[string]any{
			"bemygeminis": map[string]any{
				"type":    "stdio",
				"command": "/tmp/bmg-test/bmg",
				"args":    []any{"mcp"},
				"env":     map[string]any{},
			},
		},
	}
	data, _ := json.MarshalIndent(stale, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	changed, backup, err := mergeClaudeJSON(path, fakeExe)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true when replacing stale path")
	}
	if backup == "" {
		t.Error("expected a backup to be written when overwriting existing file")
	}

	var root map[string]any
	raw, _ := os.ReadFile(path)
	_ = json.Unmarshal(raw, &root)
	bmg := root["mcpServers"].(map[string]any)["bemygeminis"].(map[string]any)
	if bmg["command"] != fakeExe {
		t.Errorf("stale command not replaced; got %q", bmg["command"])
	}
}

// TestUnmergeClaudeJSONStripsBemygeminis: uninstall removes the
// bemygeminis MCP entry from ~/.claude.json while preserving foreign
// servers and unrelated top-level keys.
func TestUnmergeClaudeJSONStripsBemygeminis(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	existing := map[string]any{
		"mcpServers": map[string]any{
			"buddy": map[string]any{
				"type":    "stdio",
				"command": "node",
				"args":    []any{"/home/u/.buddy/server.js"},
				"env":     map[string]any{},
			},
		},
		"projects": map[string]any{
			"/home/u/p": map[string]any{"lastSessionFirstPrompt": "x"},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// Install bmg, then uninstall.
	if _, _, err := mergeClaudeJSON(path, fakeExe); err != nil {
		t.Fatal(err)
	}
	changed, err := unmergeClaudeJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("unmerge reported changed=false after install")
	}

	var root map[string]any
	raw, _ := os.ReadFile(path)
	_ = json.Unmarshal(raw, &root)

	mcps := root["mcpServers"].(map[string]any)
	if _, present := mcps["bemygeminis"]; present {
		t.Errorf("bemygeminis still present after uninstall: %s", raw)
	}
	if _, ok := mcps["buddy"]; !ok {
		t.Error("buddy lost during uninstall")
	}
	if _, ok := root["projects"]; !ok {
		t.Error("projects lost during uninstall")
	}
}

// TestUnmergeClaudeJSONMissingFile: uninstall when no ~/.claude.json
// exists is a clean no-op.
func TestUnmergeClaudeJSONMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	changed, err := unmergeClaudeJSON(path)
	if err != nil {
		t.Fatalf("unmerge missing file: %v", err)
	}
	if changed {
		t.Error("changed=true on missing file")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("uninstall created a .claude.json when none existed")
	}
}

// TestUnmergeClaudeJSONDropsEmptyMcpServers: after stripping the only
// MCP entry, the mcpServers key itself is removed so the file doesn't
// accumulate an empty container across install/uninstall cycles.
func TestUnmergeClaudeJSONDropsEmptyMcpServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	if _, _, err := mergeClaudeJSON(path, fakeExe); err != nil {
		t.Fatal(err)
	}
	if _, err := unmergeClaudeJSON(path); err != nil {
		t.Fatal(err)
	}

	var root map[string]any
	raw, _ := os.ReadFile(path)
	_ = json.Unmarshal(raw, &root)
	if _, present := root["mcpServers"]; present {
		t.Errorf("empty mcpServers should be dropped, got: %s", raw)
	}
}

func TestIsBmgCmd(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"/usr/local/bin/bmg hook pre-read", true},
		{"/tmp/bmg-test/bmg hook pre-read", true},
		{"bmg hook pre-read", true},
		{"bmg hook session-start", true},
		{"bmg hook deny-inline", true},
		{"bmg.exe hook pre-read", true},
		{"bmg describe /img.png", false}, // user-facing command, not a hook
		{"bmg doctor", false},
		{"hindcast inject", false},
		{"/usr/local/bin/somethingelse hook pre-read", false},
		{"bmg hook unknown-subcommand", false},
		{"bmg", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isBmgCmd(tc.cmd); got != tc.want {
			t.Errorf("isBmgCmd(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}
