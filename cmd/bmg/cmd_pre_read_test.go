package main

import (
	"errors"
	"strings"
	"testing"
)

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
