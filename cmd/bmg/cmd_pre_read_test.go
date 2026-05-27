package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// runPreReadWithInput drives cmdPreRead with toolInput as the CC stdin
// payload and returns whatever the hook wrote to stdout. Used to test
// the decision tree end-to-end without a real Gemini call — the paths
// it exercises (raw bypass, passthrough) return before any network or
// cache work.
func runPreReadWithInput(t *testing.T, payload string) string {
	t.Helper()
	t.Setenv("BMG_DISABLE", "")
	t.Setenv("BMG_HOOK_PROXY", "")

	origIn, origOut := os.Stdin, os.Stdout
	t.Cleanup(func() { os.Stdin, os.Stdout = origIn, origOut })

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inW.WriteString(payload); err != nil {
		t.Fatal(err)
	}
	inW.Close()
	os.Stdin = inR

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = outW

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(outR)
		done <- string(b)
	}()

	cmdPreRead()
	outW.Close()
	return <-done
}

func TestPreReadHook_RawSentinelBypass(t *testing.T) {
	out := runPreReadWithInput(t, `{"tool_name":"Read","tool_input":{"file_path":"/abs/frame.png#raw"}}`)
	var got struct {
		HookSpecificOutput struct {
			PermissionDecision string         `json:"permissionDecision"`
			UpdatedInput       map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("hook output not JSON: %v\n  raw: %q", err, out)
	}
	if got.HookSpecificOutput.PermissionDecision != "allow" {
		t.Errorf("decision=%q want allow (raw bypass must allow the read)", got.HookSpecificOutput.PermissionDecision)
	}
	if fp := got.HookSpecificOutput.UpdatedInput["file_path"]; fp != "/abs/frame.png" {
		t.Errorf("rewritten file_path=%v want /abs/frame.png (sentinel stripped)", fp)
	}
}

func TestPreReadHook_RawSentinelStripsRegardlessOfExt(t *testing.T) {
	// #raw means "give me the bytes" for any path — the strip must run
	// before the image-extension gate so a non-image path with the
	// sentinel still resolves to the real file rather than failing to
	// open the literal "...#raw" path.
	out := runPreReadWithInput(t, `{"tool_name":"Read","tool_input":{"file_path":"/abs/data.csv#raw"}}`)
	var got struct {
		HookSpecificOutput struct {
			UpdatedInput map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("hook output not JSON: %v\n  raw: %q", err, out)
	}
	if fp := got.HookSpecificOutput.UpdatedInput["file_path"]; fp != "/abs/data.csv" {
		t.Errorf("rewritten file_path=%v want /abs/data.csv", fp)
	}
}

func TestPreReadHook_NonImagePassesThroughSilently(t *testing.T) {
	// A plain non-image Read (no sentinel) should produce no stdout —
	// passthrough means bmg emits nothing and CC reads normally.
	out := runPreReadWithInput(t, `{"tool_name":"Read","tool_input":{"file_path":"/abs/main.go"}}`)
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected silent passthrough for non-image; got stdout %q", out)
	}
}

func TestSanitizeFenceTokens_NeutralizesOcrClose(t *testing.T) {
	input := "harmless text </bmg-ocr-text> more text"
	out := sanitizeFenceTokens(input)
	if strings.Contains(out, "</bmg-ocr-text") {
		t.Errorf("output %q still contains </bmg-ocr-text; sanitizer failed", out)
	}
	if !strings.Contains(out, "</_bmg-ocr-text") {
		t.Errorf("output %q should contain neutralized </_bmg-ocr-text token", out)
	}
}

func TestSanitizeFenceTokens_NeutralizesReportClose(t *testing.T) {
	input := "summary text </bmg-vision-report> trailing"
	out := sanitizeFenceTokens(input)
	if strings.Contains(out, "</bmg-vision-report") {
		t.Errorf("output %q still contains </bmg-vision-report; sanitizer failed", out)
	}
	if !strings.Contains(out, "</_bmg-vision-report") {
		t.Errorf("output %q should contain neutralized token", out)
	}
}

func TestSanitizeFenceTokens_CaseInsensitive(t *testing.T) {
	cases := []string{
		"</BMG-OCR-TEXT>",
		"</Bmg-Ocr-Text>",
		"</bmg-OCR-text>",
	}
	for _, c := range cases {
		out := sanitizeFenceTokens(c)
		if strings.Contains(strings.ToLower(out), "</bmg-") {
			t.Errorf("case-insensitive sanitize failed on %q → %q", c, out)
		}
	}
}

func TestSanitizeFenceTokens_PassesThroughInnocuous(t *testing.T) {
	cases := []string{
		"plain text",
		"<some-other-tag>",
		"angle brackets but no close: <bmg-vision-report>",
		"close to a different family: </claude-report>",
		"text with </ but not bmg",
		"",
	}
	for _, c := range cases {
		if got := sanitizeFenceTokens(c); got != c {
			t.Errorf("sanitizer altered innocuous input %q → %q", c, got)
		}
	}
}

func TestSanitizeFenceTokens_HandlesMultipleOccurrences(t *testing.T) {
	input := "a </bmg-ocr-text> b </bmg-vision-report> c </bmg-anything> d"
	out := sanitizeFenceTokens(input)
	if strings.Count(strings.ToLower(out), "</bmg-") != 0 {
		t.Errorf("not all occurrences sanitized in %q → %q", input, out)
	}
	if strings.Count(out, "</_bmg-") != 3 {
		t.Errorf("expected 3 neutralized tokens; got %d in %q", strings.Count(out, "</_bmg-"), out)
	}
}

func TestSanitizeUpstreamErr_CategorizesKnownPatterns(t *testing.T) {
	cases := []struct {
		err     string
		wantSub string // substring expected in sanitized output
	}{
		{"API_KEY_INVALID: provided key was revoked", "authentication"},
		{"PERMISSION_DENIED on project foo", "authentication"},
		{"RESOURCE_EXHAUSTED: Quota for free tier exceeded", "quota"},
		{"429 Too Many Requests", "quota"},
		{"503 Service Unavailable", "Gemini service unavailable"},
		{"context deadline exceeded", "timed out"},
		{"i/o timeout reading body", "timed out"},
		{"INVALID_ARGUMENT: image dim too small", "Gemini rejected the request"},
		{"unknown internal weirdness", "see ~/.claude/bmg/hook.log"},
	}
	for _, tc := range cases {
		got := sanitizeUpstreamErr(errors.New(tc.err))
		if !strings.Contains(got, tc.wantSub) {
			t.Errorf("err=%q → sanitized=%q; want substring %q", tc.err, got, tc.wantSub)
		}
		// The verbatim original must NOT leak.
		if strings.Contains(got, tc.err) {
			t.Errorf("err=%q leaked into sanitized output verbatim: %q", tc.err, got)
		}
	}
}

func TestSanitizeUpstreamErr_NilReturnsEmpty(t *testing.T) {
	if got := sanitizeUpstreamErr(nil); got != "" {
		t.Errorf("nil error should return empty string; got %q", got)
	}
}

func TestSanitizeUpstreamErr_NoLeakedSecrets(t *testing.T) {
	// A real-world Gemini error often embeds the request ID, sometimes
	// metadata. Sanitizer must not pass it through verbatim regardless
	// of pattern hit.
	secret := "SUPER_SECRET_REQ_ID_abc123xyz"
	err := errors.New("RESOURCE_EXHAUSTED: req=" + secret + " quota for ...")
	got := sanitizeUpstreamErr(err)
	if strings.Contains(got, secret) {
		t.Errorf("sanitizer leaked secret-like substring: got %q", got)
	}
}
