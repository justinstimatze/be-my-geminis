package main

// ~/.claude.json mutation logic — bmg's MCP server registration. Split
// out of cmd_init.go for readability (Future-Maintainer review #9).
//
// ~/.claude.json is large and Claude-managed; bmg only touches the
// mcpServers.bemygeminis subtree. Every mutation is preceded by a
// timestamped .bmg-backup-<UTC> sibling so a user can recover from a
// botched run.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// mergeClaudeJSON installs bmg's MCP server registration into the
// JSON file at path (~/.claude.json). Matches the existing-server
// shape in that file ("type": "stdio"). Foreign mcpServers entries
// are preserved. A timestamped backup is written before any mutation
// because ~/.claude.json is large and Claude-managed; if anything
// goes sideways the user can restore from it.
//
// Returns the same (changed, backup, err) shape as mergeClaudeSettings.
func mergeClaudeJSON(path, exe string) (changed bool, backupPath string, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, "", err
	}

	var (
		fileMode os.FileMode = 0o600
		root     map[string]any
		existed  bool
		original []byte
	)

	original, err = os.ReadFile(path)
	switch {
	case err == nil:
		existed = true
		if info, statErr := os.Stat(path); statErr == nil {
			fileMode = info.Mode().Perm()
		}
		if err := json.Unmarshal(original, &root); err != nil {
			return false, "", fmt.Errorf("existing ~/.claude.json is invalid JSON: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		root = map[string]any{}
	default:
		return false, "", err
	}
	if root == nil {
		root = map[string]any{}
	}

	before, _ := json.Marshal(root)

	mcps, _ := root["mcpServers"].(map[string]any)
	if mcps == nil {
		mcps = map[string]any{}
	}
	mcps["bemygeminis"] = map[string]any{
		"type":    "stdio",
		"command": exe,
		"args":    []any{"mcp"},
		"env":     map[string]any{},
	}
	root["mcpServers"] = mcps

	after, _ := json.Marshal(root)
	if string(before) == string(after) {
		return false, "", nil
	}

	if existed {
		backupPath = path + ".bmg-backup-" + time.Now().Format("20060102-150405")
		if err := atomicWriteFile(backupPath, original, 0o600); err != nil {
			return false, "", fmt.Errorf("backup: %w", err)
		}
	}

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return false, "", err
	}
	if err := atomicWriteFile(path, append(out, '\n'), fileMode); err != nil {
		return false, "", err
	}
	return true, backupPath, nil
}

// unmergeClaudeJSON strips the bemygeminis entry from root.mcpServers.
// If mcpServers becomes empty, the key is removed entirely so the file
// doesn't accumulate an empty container. Returns (changed, err);
// missing file is a clean no-op.
func unmergeClaudeJSON(path string) (bool, error) {
	var fileMode os.FileMode = 0o600
	if info, err := os.Stat(path); err == nil {
		fileMode = info.Mode().Perm()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return false, fmt.Errorf("~/.claude.json is invalid JSON: %w", err)
	}

	before, _ := json.Marshal(root)

	if mcps, _ := root["mcpServers"].(map[string]any); mcps != nil {
		if _, present := mcps["bemygeminis"]; present {
			delete(mcps, "bemygeminis")
			if len(mcps) == 0 {
				delete(root, "mcpServers")
			} else {
				root["mcpServers"] = mcps
			}
		}
	}

	after, _ := json.Marshal(root)
	if string(before) == string(after) {
		return false, nil
	}

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return false, err
	}
	return true, atomicWriteFile(path, append(out, '\n'), fileMode)
}
