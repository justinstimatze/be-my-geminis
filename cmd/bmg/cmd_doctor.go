package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/justinstimatze/be-my-geminis/internal/apikey"
	"github.com/justinstimatze/be-my-geminis/internal/cache"
	"github.com/justinstimatze/be-my-geminis/internal/vision"
)

// cmdDoctor reports bmg's health across six axes: API key resolution,
// cache directory, hook installation in settings.json, MCP server
// registration in ~/.claude.json (informational), ffprobe availability
// for video duration correction (warns only), and Gemini API
// reachability. Exits non-zero if any required check fails so it can
// gate CI / shell-init logic.
//
// Network call (Gemini ping) is skipped automatically if the API key is
// missing, since it would just produce a noisy second failure.
func cmdDoctor(args []string) {
	exitCode := 0

	fmt.Println("bmg doctor")
	fmt.Println("==========")

	// API key
	key, source, err := apikey.Resolve()
	switch {
	case err == nil:
		fmt.Printf("  ✓ API key:      resolved from %s (len=%d)\n", source, len(key))
	case errors.Is(err, apikey.ErrNotFound):
		fmt.Printf("  ✗ API key:      not found\n      checked %v and config file\n      set GEMINI_API_KEY or write to ~/.config/bmg/api_key (mode 0600)\n", apikey.EnvVars)
		exitCode = 1
	default:
		fmt.Printf("  ✗ API key:      %s\n", err)
		exitCode = 1
	}

	// Cache dir
	c, err := cache.New()
	if err != nil {
		fmt.Printf("  ✗ Cache dir:    %s\n", err)
		exitCode = 1
	} else {
		entries, bytes, _ := c.Stats()
		fmt.Printf("  ✓ Cache dir:    %s (%d entries, %s)\n", c.Dir(), entries, humanBytes(bytes))
	}

	// Hook installation
	hookExitCode := reportHookInstallation()
	if hookExitCode != 0 {
		exitCode = hookExitCode
	}

	// MCP registration — informational only (failure to find an MCP
	// entry is not an error: the user may have intentionally installed
	// without --enable-mcp; the hook surface alone is fully functional).
	reportMCPInstallation()

	// ffprobe — soft requirement for video duration correction.
	// Without it Gemini's commonly-drifted duration_seconds is left
	// unchanged in video reports (off by ~40-50% on observed runs).
	// Doctor warns but does not fail; videos still work, the
	// timestamps are just less reliable.
	reportFfprobe()

	// Gemini reachability — only attempt if API key resolved.
	if key != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		client, err := vision.New(ctx, key)
		if err != nil {
			fmt.Printf("  ✗ Gemini:       client construction failed: %s\n", err)
			exitCode = 1
		} else {
			latency, err := client.Ping(ctx)
			if err != nil {
				fmt.Printf("  ✗ Gemini:       ping failed (%s): %s\n", latency.Round(time.Millisecond), err)
				exitCode = 1
			} else {
				fmt.Printf("  ✓ Gemini:       reachable (%s, model=%s)\n", latency.Round(time.Millisecond), vision.DefaultFlashModel)
			}
		}
	} else {
		fmt.Println("  - Gemini:       skipped (no API key)")
	}

	if exitCode == 0 {
		fmt.Println("\nAll checks passed. bmg is ready.")
	} else {
		fmt.Println("\nOne or more checks failed. See messages above.")
	}
	os.Exit(exitCode)
}

// reportHookInstallation scans user- and project-level settings.json,
// reports each bmg hook found, and validates that the command's binary
// path exists and is executable. A missing binary path (e.g. install
// from /tmp/bmg-test that's since been deleted) is flagged as failed —
// users hitting this need to re-run `bmg init` from the new location.
//
// Returns 1 if no bmg hooks are installed anywhere, or any registered
// hook points at a stale binary; 0 otherwise.
func reportHookInstallation() int {
	userPath, _ := settingsPath(false)
	projectPath, _ := settingsPath(true)

	type located struct {
		path  string
		hooks []bmgHookEntry
	}

	var (
		all      []located
		anyFound bool
	)
	for _, p := range []string{userPath, projectPath} {
		entries, err := findBmgHooks(p)
		if err != nil {
			fmt.Printf("  ✗ Hook install: %s: %s\n", p, err)
			continue
		}
		if len(entries) > 0 {
			anyFound = true
		}
		all = append(all, located{path: p, hooks: entries})
	}

	if !anyFound {
		fmt.Printf("  ✗ Hook install: no bmg hooks found in %s or %s\n", userPath, projectPath)
		fmt.Println("      run `bmg init` (or `bmg init --project` from a project root)")
		return 1
	}

	staleBinary := false
	// Track which (event, matcher) pairs appear in which scopes so
	// we can flag the dual-scope-fires-twice bug (caught in the
	// v0.2 run-through-paces session: both user + project had a
	// PreToolUse:Read entry pointing at different binaries, so
	// every image Read fired twice and wrote two cache entries).
	type scopeKey struct{ event, matcher string }
	scopes := map[scopeKey][]string{}
	for _, loc := range all {
		if len(loc.hooks) == 0 {
			continue
		}
		fmt.Printf("  ✓ Hook install: %s\n", loc.path)
		for _, h := range loc.hooks {
			binPath := firstField(h.command)
			binStatus, ok := checkExecutable(binPath)
			marker := "✓"
			if !ok {
				marker = "✗"
				staleBinary = true
			}
			fmt.Printf("      %s %s:%s → %s [%s]\n", marker, h.event, h.matcher, h.command, binStatus)
			k := scopeKey{event: h.event, matcher: h.matcher}
			scopes[k] = append(scopes[k], loc.path)
		}
	}

	// Dual-scope warning: any (event, matcher) registered in more
	// than one settings.json file fires every hook event twice in
	// CC. Project scope takes precedence but both run; the result
	// is duplicate API calls + duplicate cache entries +
	// non-deterministic ordering of which response wins. Surface
	// loudly so the user knows to `bmg uninstall --project` (or
	// vice versa).
	for k, paths := range scopes {
		if len(paths) > 1 {
			fmt.Printf("  ⚠ DUAL SCOPE: %s:%s registered in %d settings files — every event fires twice\n",
				k.event, k.matcher, len(paths))
			for _, p := range paths {
				fmt.Printf("      • %s\n", p)
			}
			fmt.Println("      run `bmg uninstall --project` (from the project root) to keep only user-scope,")
			fmt.Println("      or `bmg uninstall` (then re-init with --project) to keep only project-scope")
		}
	}

	if staleBinary {
		fmt.Println("      one or more hook binaries are missing or non-executable")
		fmt.Println("      re-run `bmg init` to register the current bmg location")
		return 1
	}
	for _, paths := range scopes {
		if len(paths) > 1 {
			// Dual-scope is functional but wasteful + non-
			// deterministic. Fail the doctor so users notice.
			return 1
		}
	}
	return 0
}

// reportMCPInstallation prints whether bmg's MCP server is registered.
// The authoritative location is ~/.claude.json — Claude Code reads
// `mcpServers` from there exclusively. settings.json files (user or
// project) are checked secondarily so a legacy mis-registration can
// be flagged explicitly rather than passing silently.
//
// Informational: not registering MCP is a valid install profile (hook
// surface alone covers most users), so we don't fail the doctor over
// its absence.
func reportMCPInstallation() {
	cjPath, _ := claudeJSONPath()
	if cmd, argStr, ok := findBmgMCPEntry(cjPath); ok {
		fmt.Printf("  ✓ MCP server:   %s — %s%s\n", cjPath, cmd, argStr)
		// Even if the canonical entry is present, surface any stale
		// settings.json registrations so the user can clean them up.
		warnLegacyMCP()
		return
	}

	// Authoritative location is missing — but if the entry only lives
	// in a settings.json file (where CC ignores it), that's a worse
	// signal than "not installed" because the user thinks they're set
	// up and they're not. Flag it loudly.
	userPath, _ := settingsPath(false)
	projectPath, _ := settingsPath(true)
	var stale []string
	for _, p := range []string{userPath, projectPath} {
		if _, _, ok := findBmgMCPEntry(p); ok {
			stale = append(stale, p)
		}
	}
	if len(stale) > 0 {
		fmt.Printf("  ✗ MCP server:   not registered in %s (where CC reads from)\n", cjPath)
		for _, p := range stale {
			fmt.Printf("      stale entry in %s — CC ignores mcpServers in this file\n", p)
		}
		fmt.Println("      re-run `bmg init --enable-mcp` to register in the correct location")
		return
	}

	fmt.Printf("  - MCP server:   not registered (run `bmg init --enable-mcp` to expose mcp__bemygeminis__bmg_describe)\n")
}

// findBmgMCPEntry returns the bemygeminis mcpServers entry's command +
// formatted args from the JSON file at path, if present. Missing file
// or absent entry returns ok=false with no error. Used by doctor to
// scan multiple candidate locations.
func findBmgMCPEntry(path string) (cmd, argStr string, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return "", "", false
	}
	mcps, _ := root["mcpServers"].(map[string]any)
	if mcps == nil {
		return "", "", false
	}
	entry, ok := mcps["bemygeminis"].(map[string]any)
	if !ok {
		return "", "", false
	}
	cmd, _ = entry["command"].(string)
	args, _ := entry["args"].([]any)
	for _, a := range args {
		if s, ok := a.(string); ok {
			argStr += " " + s
		}
	}
	return cmd, argStr, true
}

// warnLegacyMCP emits a soft warning line if a bemygeminis MCP entry
// is also present in either settings.json scope. CC ignores those
// entries, but they're noise that confuses future diagnoses; users
// should know to clean them up.
func warnLegacyMCP() {
	userPath, _ := settingsPath(false)
	projectPath, _ := settingsPath(true)
	for _, p := range []string{userPath, projectPath} {
		if _, _, ok := findBmgMCPEntry(p); ok {
			fmt.Printf("      note: stale bemygeminis entry also present in %s (CC ignores it; remove with `bmg uninstall`)\n", p)
		}
	}
}

// bmgHookEntry is a single bmg-owned hook found in a settings.json,
// flattened from the nested {event → [{matcher, hooks: [{command}]}]}
// shape into one struct per command for easy reporting.
type bmgHookEntry struct {
	event   string
	matcher string
	command string
}

// findBmgHooks reads a settings.json and returns every bmg-owned hook
// entry. Returns (nil, nil) if the file does not exist (not an error
// — checking both user- and project-level is expected in doctor).
func findBmgHooks(path string) ([]bmgHookEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return nil, nil
	}

	var found []bmgHookEntry
	for event, val := range hooks {
		list, ok := val.([]any)
		if !ok {
			continue
		}
		for _, entry := range list {
			em, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			matcher, _ := em["matcher"].(string)
			inner, _ := em["hooks"].([]any)
			for _, h := range inner {
				hm, ok := h.(map[string]any)
				if !ok {
					continue
				}
				cmd, _ := hm["command"].(string)
				if isBmgCmd(cmd) {
					found = append(found, bmgHookEntry{
						event:   event,
						matcher: matcher,
						command: cmd,
					})
				}
			}
		}
	}
	return found, nil
}

// firstField returns the first whitespace-separated token of cmd, which
// for a settings.json hook command corresponds to the binary path.
func firstField(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// checkExecutable verifies that binPath resolves to a regular file with
// at least one execute bit set. Returns a short status string for
// reporting and a boolean ok flag.
func checkExecutable(binPath string) (string, bool) {
	if binPath == "" {
		return "empty path", false
	}
	abs := binPath
	if !filepath.IsAbs(binPath) {
		// PATH lookup — handles `bmg hook pre-read` style relative
		// commands users might paste manually.
		if resolved, err := exec_LookPath(binPath); err == nil {
			abs = resolved
		}
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "not found", false
		}
		return err.Error(), false
	}
	if info.IsDir() {
		return "is a directory", false
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "not executable", false
	}
	return "executable", true
}

// exec_LookPath is os/exec.LookPath via local var so the import doesn't
// shadow the package-name "exec" (used nowhere else in this file). Keeps
// the import surface narrower and easier to grep.
var exec_LookPath = func(file string) (string, error) {
	// Manual PATH lookup avoids pulling in os/exec just for this.
	pathEnv := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		candidate := filepath.Join(dir, file)
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", os.ErrNotExist
}

// reportFfprobe checks whether ffprobe is on PATH. Without it, video
// describes will keep Gemini's commonly-drifted duration_seconds
// unchanged (observed: 40-50% off on real-world clips). Doctor warns
// but does not fail — videos still work, the timestamps are just less
// reliable. Pure observational; reads $PATH only.
func reportFfprobe() {
	if _, err := exec.LookPath("ffprobe"); err == nil {
		fmt.Println("  ✓ ffprobe:      present (video duration correction enabled)")
		return
	}
	fmt.Println("  ⚠ ffprobe:      not found on PATH")
	fmt.Println("      video describes will use Gemini's declared duration_seconds")
	fmt.Println("      unchanged — frequently drifts 40-50% off the actual length.")
	fmt.Println("      install ffmpeg (which ships ffprobe) to enable correction:")
	fmt.Println("        macOS: brew install ffmpeg")
	fmt.Println("        debian/ubuntu: sudo apt install ffmpeg")
}
