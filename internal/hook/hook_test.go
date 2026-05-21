package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout swaps os.Stdout for a pipe, runs fn, returns whatever
// fn wrote. The Emit* helpers write JSON to stdout — this is how we
// assert their output shape without an integration harness.
func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
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

	if err := fn(); err != nil {
		w.Close()
		<-done
		t.Fatalf("fn returned error: %v", err)
	}
	w.Close()
	<-done
	return buf.String()
}

func TestGuard_RecoversPanic(t *testing.T) {
	// Guard must NOT propagate the panic; the process should exit
	// cleanly so Claude Code treats the hook as "pass through" rather
	// than as a deny.
	t.Setenv("HOME", t.TempDir()) // keep log out of real ~/.claude
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Guard let a panic escape: %v", r)
		}
	}()
	Guard("test", func() {
		panic("synthetic panic for test")
	})
}

func TestGuard_RunsFnWhenNoPanic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ran := false
	Guard("test", func() {
		ran = true
	})
	if !ran {
		t.Error("Guard didn't run fn when no panic")
	}
}

func TestEmitRedirect_ShapeMatchesCCContract(t *testing.T) {
	out := captureStdout(t, func() error {
		return EmitRedirect("/tmp/redirected-path.md")
	})

	var got PreToolUseOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n  raw: %q", err, out)
	}

	hso := got.HookSpecificOutput
	if hso.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName=%q want PreToolUse", hso.HookEventName)
	}
	if hso.PermissionDecision != "allow" {
		t.Errorf("permissionDecision=%q want allow (redirect must allow, not deny)", hso.PermissionDecision)
	}
	if hso.UpdatedInput == nil {
		t.Fatal("updatedInput missing — CC won't read the redirected path")
	}
	if got := hso.UpdatedInput["file_path"]; got != "/tmp/redirected-path.md" {
		t.Errorf("updatedInput.file_path=%v want /tmp/redirected-path.md", got)
	}
}

func TestEmitDeny_ShapeMatchesCCContract(t *testing.T) {
	out := captureStdout(t, func() error {
		return EmitDeny("Gemini describe failed: timeout")
	})

	var got PreToolUseOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}

	hso := got.HookSpecificOutput
	if hso.PermissionDecision != "deny" {
		t.Errorf("permissionDecision=%q want deny", hso.PermissionDecision)
	}
	if !strings.Contains(hso.AdditionalContext, "Gemini describe failed") {
		t.Errorf("additionalContext=%q should contain the reason", hso.AdditionalContext)
	}
	if hso.UpdatedInput != nil {
		t.Error("deny output should not carry updatedInput")
	}
}

func TestEmitSessionStart_ShapeMatchesCCContract(t *testing.T) {
	out := captureStdout(t, func() error {
		return EmitSessionStart("<bmg-routing>...</bmg-routing>")
	})

	var got SessionStartOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if got.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Errorf("hookEventName=%q want SessionStart", got.HookSpecificOutput.HookEventName)
	}
	if !strings.Contains(got.HookSpecificOutput.AdditionalContext, "bmg-routing") {
		t.Errorf("additionalContext lost the routing prime: %q", got.HookSpecificOutput.AdditionalContext)
	}
}

func TestReadInput_ParsesCCStdinPayload(t *testing.T) {
	// The eight keys verified empirically against CC 2.1.126.
	payload := `{
		"cwd": "/home/u/proj",
		"hook_event_name": "PreToolUse",
		"permission_mode": "default",
		"session_id": "abc-123",
		"tool_name": "Read",
		"tool_use_id": "toolu_456",
		"transcript_path": "/tmp/transcript.jsonl",
		"tool_input": {"file_path": "/path/to/image.png"}
	}`
	// Swap stdin
	r, w, _ := os.Pipe()
	orig := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = orig }()
	go func() {
		_, _ = w.Write([]byte(payload))
		w.Close()
	}()
	t.Setenv("HOME", t.TempDir())

	in, err := ReadInput("test")
	if err != nil {
		t.Fatalf("ReadInput: %v", err)
	}
	if in.ToolName != "Read" {
		t.Errorf("ToolName=%q want Read", in.ToolName)
	}
	if in.HookEventName != "PreToolUse" {
		t.Errorf("HookEventName=%q want PreToolUse", in.HookEventName)
	}
	var ti struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(in.ToolInput, &ti); err != nil {
		t.Fatalf("tool_input unmarshal: %v", err)
	}
	if ti.FilePath != "/path/to/image.png" {
		t.Errorf("tool_input.file_path=%q want /path/to/image.png", ti.FilePath)
	}
}

func TestLogPath_CreatesDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := LogPath()
	if err != nil {
		t.Fatalf("LogPath: %v", err)
	}
	if !strings.HasSuffix(path, "/.claude/bmg/hook.log") {
		t.Errorf("path=%q should end with /.claude/bmg/hook.log", path)
	}
	info, err := os.Stat(strings.TrimSuffix(path, "/hook.log"))
	if err != nil {
		t.Fatalf("dir not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode=%o want 0700", perm)
	}
}

func TestLogf_AppendsAndDoesNotBlockOnIOError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	Logf("test", "first line %d", 1)
	Logf("test", "second line %s", "more")
	path, _ := LogPath()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(body), "first line 1") {
		t.Errorf("missing first line in log: %q", body)
	}
	if !strings.Contains(string(body), "second line more") {
		t.Errorf("missing second line in log: %q", body)
	}
	if !strings.Contains(string(body), "[test]") {
		t.Errorf("log line should embed the name tag: %q", body)
	}
}

func TestLogf_GoesNonblockingOnHomeFailure(t *testing.T) {
	// Set HOME to an invalid value so LogPath fails. Logf must not
	// panic or block — it just silently no-ops.
	t.Setenv("HOME", "")
	Logf("test", "should not panic when HOME is unset")
	// Implicit pass: reaching this line means no panic / no error
	// surfaced.
}
