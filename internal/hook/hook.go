// Package hook is the shared runtime for bmg's Claude Code hooks:
// panic-guard, stdin-JSON decode, hook.log logging, and structured
// stdout output in the shape Claude Code expects.
//
// Every hook subcommand wraps its body with [Guard] so a panic can never
// reach Claude (which would silently kill the redirect and surface raw
// image bytes — the failure mode bmg exists to prevent).
package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Guard runs fn with panic recovery and logs any panic to hook.log.
// The hook still exits cleanly (exit 0) on panic, which Claude Code
// interprets as "pass through" — safer than letting CC see a non-zero
// exit and treat the tool call as denied.
func Guard(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			Logf(name, "PANIC %v", r)
		}
	}()
	fn()
}

// Input is the documented PreToolUse hook stdin payload.
// Verified empirically against Claude Code 2.1.126 in Phase 0:
// all eight keys are present (cwd, hook_event_name, permission_mode,
// session_id, tool_input, tool_name, tool_use_id, transcript_path).
//
// PostToolUse fires the same payload plus a tool_response field
// containing the tool's output. ToolResponse is omitempty so
// PreToolUse callers can ignore it.
type Input struct {
	CWD            string          `json:"cwd"`
	HookEventName  string          `json:"hook_event_name"`
	PermissionMode string          `json:"permission_mode"`
	SessionID      string          `json:"session_id"`
	ToolName       string          `json:"tool_name"`
	ToolUseID      string          `json:"tool_use_id"`
	TranscriptPath string          `json:"transcript_path"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolResponse   json.RawMessage `json:"tool_response,omitempty"`
}

// ReadInput parses the stdin JSON the harness sends to a PreToolUse hook.
func ReadInput(name string) (*Input, error) {
	var in Input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		Logf(name, "stdin decode error: %s", err)
		return nil, err
	}
	return &in, nil
}

// PreToolUseOutput is the JSON shape Claude Code honors when emitted
// to stdout from a PreToolUse hook. Verified end-to-end on CC 2.1.126:
// when PermissionDecision is "allow" and UpdatedInput.FilePath is set,
// CC reads the redirected path instead of the original.
type PreToolUseOutput struct {
	HookSpecificOutput PreToolUseHookOutput `json:"hookSpecificOutput"`
}

type PreToolUseHookOutput struct {
	HookEventName      string         `json:"hookEventName"`
	PermissionDecision string         `json:"permissionDecision"`
	UpdatedInput       map[string]any `json:"updatedInput,omitempty"`
	AdditionalContext  string         `json:"additionalContext,omitempty"`
}

// EmitRedirect writes a PreToolUse:Read redirect to stdout, telling
// Claude Code to read newPath instead of whatever path the tool was
// originally called with.
func EmitRedirect(newPath string) error {
	out := PreToolUseOutput{
		HookSpecificOutput: PreToolUseHookOutput{
			HookEventName:      "PreToolUse",
			PermissionDecision: "allow",
			UpdatedInput:       map[string]any{"file_path": newPath},
		},
	}
	return json.NewEncoder(os.Stdout).Encode(&out)
}

// EmitDeny writes a deny decision with a recovery message Claude can
// surface to the user. Used when the upstream Gemini call fails — we'd
// rather block the Read than leak raw image bytes that may crash CC.
func EmitDeny(reason string) error {
	out := PreToolUseOutput{
		HookSpecificOutput: PreToolUseHookOutput{
			HookEventName:      "PreToolUse",
			PermissionDecision: "deny",
			AdditionalContext:  reason,
		},
	}
	return json.NewEncoder(os.Stdout).Encode(&out)
}

// SessionStartOutput is the JSON shape Claude Code expects from a
// SessionStart hook. Unlike PreToolUse, there's no permissionDecision
// — the hook can only inject context, not gate anything.
type SessionStartOutput struct {
	HookSpecificOutput SessionStartHookOutput `json:"hookSpecificOutput"`
}

type SessionStartHookOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// EmitSessionStart writes the routing instructions that prime Claude
// for the trust-fenced markdown bmg's PreToolUse:Read hook produces.
// Without this (or an equivalent CLAUDE.md paste), Claude doesn't know
// the report is replacing image bytes and may treat OCR'd text inside
// the untrusted fence as actionable instructions.
func EmitSessionStart(additionalContext string) error {
	out := SessionStartOutput{
		HookSpecificOutput: SessionStartHookOutput{
			HookEventName:     "SessionStart",
			AdditionalContext: additionalContext,
		},
	}
	return json.NewEncoder(os.Stdout).Encode(&out)
}

// LogPath returns ~/.claude/bmg/hook.log, creating the directory if
// missing. Returns an error only if the home directory or directory
// creation fails.
func LogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".claude", "bmg")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "hook.log"), nil
}

// maxLogSize triggers single-segment rotation. When hook.log passes
// this size, it's renamed to hook.log.1 and a fresh hook.log starts.
// Bounded at 10 MB × 2 segments so logging can't fill the disk.
const maxLogSize = 10 * 1024 * 1024

// Logf appends a UTC-timestamped line to hook.log. Best-effort: any
// I/O error is swallowed so logging is guaranteed non-blocking.
func Logf(name, format string, args ...any) {
	path, err := LogPath()
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s [%s] %s\n",
		time.Now().UTC().Format(time.RFC3339),
		name,
		fmt.Sprintf(format, args...),
	)
	if stat, err := f.Stat(); err == nil && stat.Size() > maxLogSize {
		_ = os.Rename(path, path+".1")
	}
}
