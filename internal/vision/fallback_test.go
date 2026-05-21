package vision

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/genai"
)

func TestReadBudget_FreshDayWhenFileMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opus-budget.json")
	got := readBudget(path)
	today := time.Now().UTC().Format("2006-01-02")
	if got.Date != today {
		t.Errorf("Date=%q want today (%s)", got.Date, today)
	}
	if got.SpentUSD != 0 {
		t.Errorf("SpentUSD=%f want 0 on fresh fallback file", got.SpentUSD)
	}
}

func TestReadBudget_RollsOverDateChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opus-budget.json")
	// Seed with yesterday's spend.
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	stale := opusBudgetState{Date: yesterday, SpentUSD: 4.99}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got := readBudget(path)
	today := time.Now().UTC().Format("2006-01-02")
	if got.Date != today {
		t.Errorf("expected date to roll over to today (%s); got %q", today, got.Date)
	}
	if got.SpentUSD != 0 {
		t.Errorf("expected SpentUSD to reset to 0 on rollover; got %f", got.SpentUSD)
	}
}

func TestReadBudget_PreservesToday(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opus-budget.json")
	today := time.Now().UTC().Format("2006-01-02")
	state := opusBudgetState{Date: today, SpentUSD: 2.50}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got := readBudget(path)
	if got.SpentUSD != 2.50 {
		t.Errorf("expected today's spend to persist; got %f", got.SpentUSD)
	}
}

func TestWriteBudget_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opus-budget.json")
	want := opusBudgetState{Date: "2026-05-21", SpentUSD: 1.23}
	if err := writeBudget(path, want); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got opusBudgetState
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %+v want %+v", got, want)
	}
	// Mode 0600 — same hygiene as the rest of bmg's owned state.
	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm=%o want 0600", perm)
	}
}

func TestLockedUpdateBudget_ConcurrentSums(t *testing.T) {
	// 50 goroutines each add $0.01. Without the flock, read-modify-
	// write races lose charges and the final tally is below $0.50.
	// With the flock, every charge lands.
	dir := t.TempDir()
	path := filepath.Join(dir, "bmg", "opus-budget.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	const goroutines = 50
	const each = 0.01
	done := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			done <- lockedUpdateBudget(path, func(s opusBudgetState) opusBudgetState {
				if s.Date == "" {
					s.Date = time.Now().UTC().Format("2006-01-02")
				}
				s.SpentUSD += each
				return s
			})
		}()
	}
	for i := 0; i < goroutines; i++ {
		if err := <-done; err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}

	final := readBudget(path)
	want := goroutines * each
	// Float arithmetic — allow tiny drift but not lost charges.
	if final.SpentUSD < want-1e-9 {
		t.Errorf("budget under-counted under concurrency: got $%.4f want $%.4f (lost %d charges?)",
			final.SpentUSD, want, int((want-final.SpentUSD)/each+0.5))
	}
}

func TestTryOpusFallback_SkipsOnNonRetryableError(t *testing.T) {
	// A schema-parse error is deterministic — Opus won't help.
	// tryOpusFallback should return the original error unchanged
	// without consulting any env / budget / API.
	geminiErr := errors.New("parse: invalid character at offset 5")
	rep, err := tryOpusFallback(context.Background(), []byte{}, Options{}, geminiErr)
	if rep != nil {
		t.Error("expected nil report on non-retryable error")
	}
	if err == nil || err.Error() != geminiErr.Error() {
		t.Errorf("expected original gemini error unchanged; got %v", err)
	}
}

func TestTryOpusFallback_SkipsWhenNoAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("BMG_OPUS_FALLBACK_BUDGET_USD", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir()) // sandbox

	geminiErr := genai.APIError{Code: 503, Message: "test outage", Status: "UNAVAILABLE"}
	rep, err := tryOpusFallback(context.Background(), []byte{}, Options{}, geminiErr)
	if rep != nil {
		t.Error("expected nil report when no Anthropic API key configured")
	}
	if !errors.Is(err, geminiErr) && err == nil {
		t.Errorf("expected original gemini error to pass through; got %v", err)
	}
}

func TestTryOpusFallback_SkipsWhenBudgetExhausted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "fake-key-budget-blocks-the-call")
	t.Setenv("BMG_OPUS_FALLBACK_BUDGET_USD", "5.00")
	t.Setenv("XDG_RUNTIME_DIR", dir)

	// Pre-seed today's spend over the cap.
	budgetPath := filepath.Join(dir, "bmg", "opus-budget.json")
	if err := os.MkdirAll(filepath.Dir(budgetPath), 0o700); err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Format("2006-01-02")
	exhausted := opusBudgetState{Date: today, SpentUSD: 5.01}
	data, _ := json.Marshal(exhausted)
	if err := os.WriteFile(budgetPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	geminiErr := genai.APIError{Code: 503, Message: "test outage", Status: "UNAVAILABLE"}
	rep, err := tryOpusFallback(context.Background(), []byte{}, Options{}, geminiErr)
	if rep != nil {
		t.Error("expected nil report when budget is exhausted")
	}
	if err == nil {
		t.Fatal("expected augmented error mentioning budget exhaustion")
	}
	if !strings.Contains(err.Error(), "budget exhausted") {
		t.Errorf("error should explain budget exhaustion; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "$5.01 of $5.00") {
		t.Errorf("error should cite the actual spent/cap; got %q", err.Error())
	}
}

func TestBuildOpusPrompt_IncludesIntentAndProfile(t *testing.T) {
	out := buildOpusPrompt(Options{
		Profile: "chart",
		Intent:  "extract Q3 revenue",
	})
	if !strings.Contains(out, "fallback") {
		t.Errorf("prompt should mention fallback context: %q", out)
	}
	if !strings.Contains(out, "extract Q3 revenue") {
		t.Errorf("prompt should include intent verbatim: %q", out)
	}
	if !strings.Contains(out, "chart") {
		t.Errorf("prompt should mention the original profile: %q", out)
	}
}

func TestBuildOpusPrompt_OmitsProfileGuidanceForGeneral(t *testing.T) {
	out := buildOpusPrompt(Options{Profile: "general"})
	// For general profile (the default), we don't add per-profile
	// guidance because the prose-only fallback IS the general-
	// profile shape.
	if strings.Contains(out, "originally requested") {
		t.Errorf("prompt should not append profile-specific guidance for 'general': %q", out)
	}
}

func TestShortGeminiError_TruncatesLong(t *testing.T) {
	short := errors.New("Error 503")
	if got := shortGeminiError(short); got != "Error 503" {
		t.Errorf("short error mangled: %q", got)
	}
	long := errors.New(strings.Repeat("x", 200))
	got := shortGeminiError(long)
	if len(got) > 90 { // 80 + "..." plus a few chars of slack
		t.Errorf("long error not truncated; len=%d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncated error should end with '...'; got %q", got)
	}
}
