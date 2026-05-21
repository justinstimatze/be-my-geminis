package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// cmdInit + cmdUninstall + path resolvers. The settings.json mutation
// logic lives in cmd_init_hooks.go; the ~/.claude.json mutation logic
// lives in cmd_init_mcp.go.
//
// CC sources hooks and MCP from different files:
//   - HOOKS come from ~/.claude/settings.json (user) or
//     <cwd>/.claude/settings.json (--project). Both scopes honored.
//   - mcpServers come from ~/.claude.json EXCLUSIVELY. Entries placed
//     in either settings.json file are silently ignored by CC's MCP
//     discovery — verified live in CC 2.1.126.
//
// `bmg init --project --enable-mcp` therefore writes hooks to the
// project's settings.json and MCP to ~/.claude.json — the project
// flag governs only the hook scope.
//
// Each file is backed up to a timestamped sibling before any mutation.
// Foreign hooks / MCP entries are preserved on both reads and writes.

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	project := fs.Bool("project", false, "install hooks into <cwd>/.claude/settings.json instead of ~/.claude/settings.json (MCP registration, if enabled, always goes to ~/.claude.json)")
	enableMCP := fs.Bool("enable-mcp", false, "also register the bmg MCP server (mcpServers.bemygeminis) in ~/.claude.json — exposes bmg_describe to Claude")
	fs.Parse(args)

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg init: cannot find own path: %s\n", err)
		os.Exit(1)
	}
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}

	hookPath, err := settingsPath(*project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg init: %s\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "bmg init (binary: %s)\n", exe)
	fmt.Fprintln(os.Stderr,
		"  TIP: quit all Claude Code sessions before continuing. bmg's writes to\n"+
			"       settings.json + ~/.claude.json are atomic (won't corrupt the file on\n"+
			"       Ctrl-C / OOM), but cannot serialize against CC's concurrent writes;\n"+
			"       a CC update landing between bmg's read and write would be lost.\n"+
			"       The timestamped .bmg-backup-* sibling is the recovery path either way.")
	fmt.Fprintf(os.Stderr, "  hooks target:  %s\n", hookPath)

	changed, backup, err := mergeClaudeSettings(hookPath, exe)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  settings.json: %s\n", err)
		os.Exit(1)
	}
	if !changed {
		fmt.Fprintln(os.Stderr, "  settings.json: already installed, no changes")
	} else {
		fmt.Fprintln(os.Stderr, "  settings.json: bmg PreToolUse:Read + PostToolUse:Bash + SessionStart hooks installed")
		if backup != "" {
			fmt.Fprintf(os.Stderr, "  hooks backup:  %s\n", backup)
		}
	}

	if *enableMCP {
		cjPath, err := claudeJSONPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "bmg init: %s\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "  MCP target:    %s\n", cjPath)
		mcpChanged, mcpBackup, err := mergeClaudeJSON(cjPath, exe)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  .claude.json:  %s\n", err)
			os.Exit(1)
		}
		if !mcpChanged {
			fmt.Fprintln(os.Stderr, "  .claude.json:  bemygeminis MCP already registered, no changes")
		} else {
			fmt.Fprintln(os.Stderr, "  .claude.json:  bemygeminis MCP server registered")
			if mcpBackup != "" {
				fmt.Fprintf(os.Stderr, "  MCP backup:    %s\n", mcpBackup)
			}
		}
	}

	if changed || *enableMCP {
		fmt.Fprintln(os.Stderr, "\nRestart Claude Code so the hook and MCP registration are loaded")
		fmt.Fprintln(os.Stderr, "(both files are snapshot at session start; not hot-reloaded).")
	}
}

// cmdUninstall is the inverse of cmdInit: strips every bmg-owned hook
// entry from the target settings.json AND, in user-scope mode, strips
// the bemygeminis MCP entry from ~/.claude.json. Foreign hooks and
// foreign MCP servers are left untouched. If a hooks block or
// mcpServers block becomes empty after stripping, the key is dropped
// so files don't accumulate empty containers across install/uninstall
// cycles.
//
// Scope symmetry with cmdInit:
//   - `bmg uninstall` (default, user scope) → removes user-scope
//     hooks AND removes the MCP registration from ~/.claude.json.
//   - `bmg uninstall --project` → removes project-scope hooks only.
//     Does NOT touch ~/.claude.json because MCP registrations are
//     user-scope by design (CC reads mcpServers from there
//     exclusively); a project-scope uninstall that also blasted the
//     user-scope MCP would surprise users with co-existing project
//   - user installs.
func cmdUninstall(args []string) {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	project := fs.Bool("project", false, "uninstall hooks from <cwd>/.claude/settings.json instead of ~/.claude/settings.json. Does not touch the user-scope MCP registration in ~/.claude.json.")
	fs.Parse(args)

	hookPath, err := settingsPath(*project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg uninstall: %s\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "bmg uninstall\n")
	fmt.Fprintln(os.Stderr,
		"  TIP: quit Claude Code first. uninstall's writes are atomic but\n"+
			"       cannot serialize against CC's concurrent edits.")
	fmt.Fprintf(os.Stderr, "  hooks target: %s\n", hookPath)
	changed, err := unmergeClaudeSettings(hookPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  settings.json: %s\n", err)
		os.Exit(1)
	}
	if changed {
		fmt.Fprintln(os.Stderr, "  settings.json: bmg hooks removed")
	} else {
		fmt.Fprintln(os.Stderr, "  settings.json: no bmg hooks present, no changes")
	}

	// MCP registration is user-scope only. --project uninstalls leave
	// it alone so a user with both project + user installs doesn't
	// lose their user-scope MCP when cleaning up a single project.
	if *project {
		fmt.Fprintln(os.Stderr, "  .claude.json: not touched (--project scope; MCP registration is user-scope only)")
		return
	}

	cjPath, err := claudeJSONPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg uninstall: %s\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "  MCP target:   %s\n", cjPath)
	mcpChanged, err := unmergeClaudeJSON(cjPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  .claude.json: %s\n", err)
		os.Exit(1)
	}
	if mcpChanged {
		fmt.Fprintln(os.Stderr, "  .claude.json: bemygeminis MCP server removed")
	} else {
		fmt.Fprintln(os.Stderr, "  .claude.json: no bemygeminis MCP entry present, no changes")
	}
}

// settingsPath resolves the hooks-file install target. Project-mode
// resolves to the cwd's .claude/settings.json (matching CC's project-
// level hook discovery); user-mode is ~/.claude/settings.json.
func settingsPath(project bool) (string, error) {
	if project {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Join(cwd, ".claude", "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// claudeJSONPath resolves the MCP-server registration target. This is
// always ~/.claude.json — CC reads mcpServers from this file
// exclusively. Verified live in CC 2.1.126 (servers registered in
// either settings.json scope are silently ignored).
func claudeJSONPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude.json"), nil
}
