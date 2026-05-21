package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/justinstimatze/be-my-geminis/internal/hook"
)

// cmdPostTool is the PostToolUse hook entry point. Currently scoped
// to the Bash tool: when Claude runs a shell command that produces
// an image file (matplotlib savefig, screenshot tools, gh pr diff
// containing attachments, image conversion utilities), we extract
// the resulting paths and pre-warm bmg's cache for them — so when
// Claude later Reads those files, the PreToolUse:Read hook gets an
// instant cache hit instead of a 2-5s describe.
//
// Critical: this hook must return immediately so it doesn't block
// the agent. The actual describe work happens in a detached child
// process (`bmg describe-cached`) spawned via setsid so it survives
// our exit. The hook's own latency budget is "scan output, fork,
// exit" — should be <50ms.
func cmdPostTool() {
	const name = "post-tool"

	in, err := hook.ReadInput(name)
	if err != nil {
		return
	}

	// Currently scoped to Bash. Other tools (Edit, Write, etc.)
	// don't typically produce image files; we'd add per-tool
	// parsing when there's a use case.
	if in.ToolName != "Bash" {
		return
	}

	paths := extractImagePaths(in)
	if len(paths) == 0 {
		return
	}

	exe, err := os.Executable()
	if err != nil {
		return
	}
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}

	// Spawn the warm in the background and exit. The detached child
	// will run for up to ~5min (per cmd_describe_cached's timeout)
	// without holding the hook process or the agent.
	args := append([]string{"describe-cached"}, paths...)
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		hook.Logf(name, "failed to spawn warm for %d paths: %s", len(paths), err)
		return
	}
	hook.Logf(name, "spawned warm pid=%d for %d path(s)", cmd.Process.Pid, len(paths))
}

// bashToolInput is the slice of tool_input we care about. Other
// fields (description, timeout, run_in_background) are ignored.
type bashToolInput struct {
	Command string `json:"command"`
}

// extractImagePaths combines two signals to find images worth
// pre-warming:
//
//  1. Path-shaped tokens with image extensions appearing in the
//     command itself or the tool's stdout/stderr. Catches tools
//     that announce their output ("saved to foo.png").
//  2. Files matching image extensions in the cwd whose mtime is
//     within a recent window (set conservatively at 30s so we
//     don't accidentally warm long-existing files that happen to
//     sit in the cwd of an unrelated Bash call).
//
// Returned paths are absolute, deduplicated, and filtered to those
// that actually exist on disk + are within bmg's MaxImageBytes cap.
func extractImagePaths(in *hook.Input) []string {
	cwd := in.CWD
	now := time.Now()
	// Pull command + response strings to grep over.
	var bi bashToolInput
	_ = json.Unmarshal(in.ToolInput, &bi)
	// Bash tool_response is typed as {stdout, stderr, ...} in CC.
	// We don't strictly need the structure — just want a string to
	// pattern-match against.
	respText := stringOfResponse(in.ToolResponse)
	corpus := bi.Command + "\n" + respText

	seen := map[string]bool{}
	out := make([]string, 0, 4)

	// Pass 1: regex-scan corpus for path-shaped image references.
	// FindAllStringSubmatch gives us [fullMatch, group1] per hit;
	// group1 is the path without the surrounding whitespace anchor.
	for _, m := range imagePathRe.FindAllStringSubmatch(corpus, -1) {
		if len(m) < 2 {
			continue
		}
		path := m[1]
		if !filepath.IsAbs(path) && cwd != "" {
			path = filepath.Join(cwd, path)
		}
		path = filepath.Clean(path)
		if !seen[path] && isUsableImage(path) {
			seen[path] = true
			out = append(out, path)
		}
	}

	// Pass 2: cwd recency scan. Skip if cwd is empty (shouldn't be —
	// CC always sends one — but defensive).
	if cwd != "" {
		entries, err := os.ReadDir(cwd)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				ext := strings.ToLower(filepath.Ext(e.Name()))
				if !imageExtensions[ext] {
					continue
				}
				full := filepath.Join(cwd, e.Name())
				info, err := e.Info()
				if err != nil {
					continue
				}
				if now.Sub(info.ModTime()) > recentImageWindow {
					continue
				}
				if !seen[full] && isUsableImage(full) {
					seen[full] = true
					out = append(out, full)
				}
			}
		}
	}

	return out
}

// recentImageWindow caps how old a cwd-discovered image can be and
// still be considered fresh. 30s is generous enough to catch slow
// matplotlib renders + slow gh fetches but tight enough to avoid
// warming pre-existing project assets that happen to share the cwd.
//
// SECURITY NOTE: this scan is opportunistic by design — it warms
// any recently-modified image in the cwd, not just images Claude
// is about to read. In a directory with sensitive images (medical
// records, NDA'd designs, private screenshots) that get modified
// during a Claude Code session, those images can be uploaded to
// Gemini even though Claude never explicitly asked to read them.
// Bounded by:
//   - The 30s window (sensitive image must be modified within the
//     last 30s of a Bash tool call to be picked up)
//   - The user's BMG_DISABLE=1 session-level bypass
//   - The user's bmg uninstall option for the post-tool hook
//   - The same MaxImageBytes + dimension cap that the hook path
//     uses, so a 50MB+ medical scan won't upload regardless
//
// Document in the trust model (SECURITY.md) so users with
// regulated-content workflows understand the surface.
const recentImageWindow = 30 * time.Second

// imagePathRe matches absolute or relative path-shaped tokens ending
// in a recognized image extension. Conservative: requires the path
// to look path-shaped (no whitespace, contains a slash or starts
// with ./), and to end with a known image extension. Will miss
// exotic cases like "Saved to: foo.png" where foo isn't slash-
// qualified — those still get caught by the cwd recency pass.
var imagePathRe = regexp.MustCompile(`(?:^|[\s'"])(/?(?:[^\s'"<>]+/)*[^\s'"/<>]+\.(?:png|jpg|jpeg|gif|bmp|webp|tiff|tif))(?:$|[\s'"])`)

// isUsableImage checks the path exists, is a file (not dir), is
// non-empty, and has the right size band (above 0, below
// MaxImageBytes which the describe path also enforces). Doesn't
// decode — just guards against trivially-bad paths before spending
// the cost of spawning the warm child.
func isUsableImage(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() || info.Size() == 0 {
		return false
	}
	// 50 MB cap matches vision.MaxImageBytes; if it's bigger the
	// warm would fail anyway.
	if info.Size() > 50*1024*1024 {
		return false
	}
	return true
}

// stringOfResponse coerces the tool_response RawMessage to a single
// text blob to grep over. ToolResponse can be a JSON string, a
// struct with stdout/stderr fields, or absent — handle all three.
func stringOfResponse(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try parsing as struct first (CC's Bash response shape).
	var s struct {
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
	}
	if err := json.Unmarshal(raw, &s); err == nil && (s.Stdout != "" || s.Stderr != "") {
		return s.Stdout + "\n" + s.Stderr
	}
	// Try parsing as a raw string.
	var asStr string
	if err := json.Unmarshal(raw, &asStr); err == nil {
		return asStr
	}
	// Last resort: just use the raw bytes (it's valid JSON of some
	// shape; the regex will pick out anything path-like).
	return string(raw)
}

// imagePathRe's match group needs trimming because we anchored with
// (?:^|[\s'"]) to avoid catching path fragments inside larger
// tokens. Re-extract the inner group at use sites.
func init() {
	// Validate the regex compiles at import time so a future edit
	// can't break the hook silently.
	if imagePathRe == nil {
		panic("imagePathRe failed to compile")
	}
}
