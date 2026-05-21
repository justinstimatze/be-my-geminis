package main

// settings.json mutation logic — hooks merge / unmerge plus the stale-
// MCP cleanup that scrubs entries from the wrong file. Split out of
// cmd_init.go for readability (Future-Maintainer review #9).

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// mergeClaudeSettings installs bmg's hooks into the JSON file at path.
// Returns (changed, backup, err):
//   - changed=false → file was already up-to-date; no rewrite, no backup.
//   - changed=true & backup=="" → file did not previously exist.
//   - changed=true & backup!="" → existing file was backed up to that path.
//
// File mode is preserved on rewrite so a 0600 file stays 0600.
// MCP registration is handled separately by [mergeClaudeJSON] — this
// function only manages hooks.
func mergeClaudeSettings(path, exe string) (changed bool, backupPath string, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, "", err
	}

	var (
		settingsMode os.FileMode = 0o600
		settings     map[string]any
		existed      bool
		original     []byte
	)

	original, err = os.ReadFile(path)
	switch {
	case err == nil:
		existed = true
		if info, statErr := os.Stat(path); statErr == nil {
			settingsMode = info.Mode().Perm()
		}
		if err := json.Unmarshal(original, &settings); err != nil {
			return false, "", fmt.Errorf("existing settings.json is invalid JSON: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		settings = map[string]any{}
	default:
		return false, "", err
	}
	if settings == nil {
		settings = map[string]any{}
	}

	before, _ := json.Marshal(settings)

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	upsertBmgHook(hooks, "PreToolUse", "Read", exe+" hook pre-read")
	upsertBmgHook(hooks, "SessionStart", "", exe+" hook session-start")
	// PostToolUse:Bash → pre-warm the cache for images the Bash
	// command just produced (matplotlib savefig, screenshot tools,
	// gh diff with attachments, image conversion). Returns
	// immediately, spawns the actual warm as a detached child via
	// describe-cached.
	upsertBmgHook(hooks, "PostToolUse", "Bash", exe+" hook post-tool")
	settings["hooks"] = hooks

	// Legacy cleanup: earlier versions of bmg init wrote
	// mcpServers.bemygeminis here. CC ignores mcpServers in settings.json
	// (any scope), so the entry is noise. Strip it on every install so
	// the upgrade path from broken-init is automatic.
	stripStaleMCPFromSettings(settings)

	after, _ := json.Marshal(settings)
	if string(before) == string(after) {
		return false, "", nil
	}

	if existed {
		backupPath = path + ".bmg-backup-" + time.Now().Format("20060102-150405")
		if err := atomicWriteFile(backupPath, original, 0o600); err != nil {
			return false, "", fmt.Errorf("backup: %w", err)
		}
	}

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, "", err
	}
	if err := atomicWriteFile(path, append(out, '\n'), settingsMode); err != nil {
		return false, "", err
	}
	return true, backupPath, nil
}

// upsertBmgHook ensures cmd is registered under hooks[event] for the
// given matcher. Any existing bmg-owned entries for this event are
// stripped first — that way a stale binary path (e.g. /tmp/bmg-test
// from earlier development) gets replaced cleanly on the next install
// instead of accumulating duplicates. Foreign hooks under the same
// matcher are preserved.
func upsertBmgHook(hooks map[string]any, event, matcher, cmd string) {
	list, _ := hooks[event].([]any)

	filtered := make([]any, 0, len(list)+1)
	for _, entry := range list {
		em, ok := entry.(map[string]any)
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		inner, _ := em["hooks"].([]any)
		survivors := make([]any, 0, len(inner))
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				survivors = append(survivors, h)
				continue
			}
			if c, _ := hm["command"].(string); !isBmgCmd(c) {
				survivors = append(survivors, h)
			}
		}
		if len(survivors) == 0 {
			// Block contained only bmg entries — drop the matcher
			// wrapper entirely so we don't leave dangling empty
			// {"matcher": "Read", "hooks": []} blocks.
			continue
		}
		em["hooks"] = survivors
		filtered = append(filtered, em)
	}

	filtered = append(filtered, map[string]any{
		"matcher": matcher,
		"hooks": []any{
			map[string]any{"type": "command", "command": cmd},
		},
	})

	hooks[event] = filtered
}

// unmergeClaudeSettings strips every bmg-owned hook entry. Empty matcher
// blocks and empty event lists are removed, and "hooks" is dropped if
// nothing is left under it. Returns (changed, err).
//
// MCP registration is handled separately by [unmergeClaudeJSON].
func unmergeClaudeSettings(path string) (bool, error) {
	var settingsMode os.FileMode = 0o600
	if info, err := os.Stat(path); err == nil {
		settingsMode = info.Mode().Perm()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return false, fmt.Errorf("settings.json is invalid JSON: %w", err)
	}

	before, _ := json.Marshal(settings)

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks != nil {
		for event, val := range hooks {
			list, ok := val.([]any)
			if !ok {
				continue
			}
			filtered := make([]any, 0, len(list))
			for _, entry := range list {
				em, ok := entry.(map[string]any)
				if !ok {
					filtered = append(filtered, entry)
					continue
				}
				inner, _ := em["hooks"].([]any)
				survivors := make([]any, 0, len(inner))
				for _, h := range inner {
					hm, ok := h.(map[string]any)
					if !ok {
						survivors = append(survivors, h)
						continue
					}
					if c, _ := hm["command"].(string); !isBmgCmd(c) {
						survivors = append(survivors, h)
					}
				}
				if len(survivors) == 0 {
					continue
				}
				em["hooks"] = survivors
				filtered = append(filtered, em)
			}
			if len(filtered) == 0 {
				delete(hooks, event)
			} else {
				hooks[event] = filtered
			}
		}
		if len(hooks) == 0 {
			delete(settings, "hooks")
		}
	}

	// Same legacy cleanup as in merge: strip any bemygeminis entry from
	// settings.mcpServers (CC ignores it; uninstall should remove it).
	stripStaleMCPFromSettings(settings)

	after, _ := json.Marshal(settings)
	if string(before) == string(after) {
		return false, nil
	}

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, err
	}
	return true, atomicWriteFile(path, append(out, '\n'), settingsMode)
}

// stripStaleMCPFromSettings removes settings.mcpServers.bemygeminis if
// present. Earlier (broken) versions of bmg init wrote the MCP entry
// here; CC ignores mcpServers in settings.json (any scope), so we
// scrub it on every merge/unmerge to keep the file tidy across the
// upgrade path. If mcpServers becomes empty, the key is dropped.
func stripStaleMCPFromSettings(settings map[string]any) {
	mcps, _ := settings["mcpServers"].(map[string]any)
	if mcps == nil {
		return
	}
	if _, present := mcps["bemygeminis"]; !present {
		return
	}
	delete(mcps, "bemygeminis")
	if len(mcps) == 0 {
		delete(settings, "mcpServers")
	} else {
		settings["mcpServers"] = mcps
	}
}

// isBmgCmd recognizes a settings.json command string as one bmg
// installed. The shape is "<path>/bmg hook <subcommand>" where
// subcommand is one of our hook handlers. Matching by basename means
// install + uninstall stay symmetric across binary moves: a stale
// /tmp/bmg-test/bmg path is still recognized as ours and replaced.
func isBmgCmd(cmd string) bool {
	fields := strings.Fields(cmd)
	if len(fields) < 3 {
		return false
	}
	base := strings.TrimSuffix(filepath.Base(fields[0]), ".exe")
	if base != "bmg" {
		return false
	}
	if fields[1] != "hook" {
		return false
	}
	switch fields[2] {
	case "pre-read", "session-start", "post-tool", "deny-inline":
		return true
	}
	return false
}
