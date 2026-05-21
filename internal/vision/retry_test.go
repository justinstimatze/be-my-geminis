package vision

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/genai"
)

func TestIsRetryable_APIError(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{500, true},  // server fault
		{502, true},  // bad gateway
		{503, true},  // unavailable (the smoke we hit)
		{504, true},  // gateway timeout
		{429, true},  // rate limit — backoff is the point
		{400, false}, // invalid arg — deterministic
		{401, false}, // auth — won't recover
		{403, false}, // forbidden — won't recover
		{404, false}, // not found — won't recover
	}
	for _, c := range cases {
		err := genai.APIError{Code: c.code, Status: "TEST", Message: "test"}
		got := isRetryable(fmt.Errorf("wrapped: %w", err))
		if got != c.want {
			t.Errorf("isRetryable(code=%d)=%v want %v", c.code, got, c.want)
		}
	}
}

func TestIsRetryable_NetworkErrors(t *testing.T) {
	retryableMessages := []string{
		"connection reset by peer",
		"connection refused",
		"i/o timeout",
		"context deadline exceeded",
		"EOF",
		"write tcp: broken pipe",
		"no such host",
	}
	for _, msg := range retryableMessages {
		if !isRetryable(errors.New(msg)) {
			t.Errorf("expected %q to be retryable (network-flakiness pattern)", msg)
		}
	}
}

func TestIsRetryable_NonRetryable(t *testing.T) {
	cases := []error{
		nil,
		errors.New("parse failed"),
		errors.New("schema mismatch"),
		errors.New("image exceeds cap: 100MB > 50MB"),
		errors.New("unknown profile"),
	}
	for _, err := range cases {
		if isRetryable(err) {
			t.Errorf("isRetryable(%v) should be false (deterministic failure)", err)
		}
	}
}

func TestRetryDelay_ExponentialWithCap(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 0},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 30 * time.Second}, // capped
		{10, 30 * time.Second},
	}
	for _, c := range cases {
		got := retryDelay(c.attempt)
		if got != c.want {
			t.Errorf("retryDelay(%d)=%v want %v", c.attempt, got, c.want)
		}
	}
}

func TestWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Errorf("expected nil err on first-attempt success; got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call; got %d", calls)
	}
}

func TestWithRetry_RetriesOnRetryable(t *testing.T) {
	calls := 0
	retryableErr := genai.APIError{Code: 503, Status: "UNAVAILABLE", Message: "test"}
	// Override retryDelay-ish: we can't easily inject the schedule
	// (it's package-level), so this test uses a short MaxRetry-driven
	// path. Real wall: 0 + 2s + 4s = 6s for 3 attempts.
	//
	// We use a ctx with a 12s budget so the natural delay fits and
	// the test catches both "retried" and "eventually returned the
	// last error" without flaking.
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	start := time.Now()
	err := withRetry(ctx, func() error {
		calls++
		return retryableErr
	})
	elapsed := time.Since(start)
	if calls != MaxRetryAttempts {
		t.Errorf("expected %d calls (all retries used); got %d", MaxRetryAttempts, calls)
	}
	// withRetry should return the LAST error (not "retries exhausted").
	if !errors.Is(err, retryableErr) && err.Error() != retryableErr.Error() {
		// errors.Is may not match because APIError isn't a sentinel;
		// match on message text.
		if err == nil || err.Error() != retryableErr.Error() {
			t.Errorf("expected final err to be the last attempt's error; got %v", err)
		}
	}
	// 0s + 2s + 4s = 6s minimum. Allow some slack.
	if elapsed < 5500*time.Millisecond {
		t.Errorf("elapsed %v too short — backoff not applied?", elapsed)
	}
	if elapsed > 8*time.Second {
		t.Errorf("elapsed %v too long — backoff schedule may be wrong", elapsed)
	}
}

func TestWithRetry_StopsOnNonRetryable(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), func() error {
		calls++
		return errors.New("deterministic failure")
	})
	if calls != 1 {
		t.Errorf("expected 1 call (no retry on non-retryable); got %d", calls)
	}
	if err == nil || err.Error() != "deterministic failure" {
		t.Errorf("expected deterministic error to surface; got %v", err)
	}
}

func TestWithRetry_RespectsCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	retryableErr := genai.APIError{Code: 503}
	// Cancel after the first call returns, before the first retry-
	// delay completes.
	err := withRetry(ctx, func() error {
		calls++
		if calls == 1 {
			cancel()
		}
		return retryableErr
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled; got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 call before ctx cancel interrupted the sleep; got %d", calls)
	}
}

func TestSetRetryLogger_RoutesAttempts(t *testing.T) {
	type entry struct {
		attempt   int
		errMsg    string
		nextDelay time.Duration
	}
	var got []entry
	orig := retryLogger
	t.Cleanup(func() { retryLogger = orig })
	SetRetryLogger(func(a int, e error, d time.Duration) {
		got = append(got, entry{attempt: a, errMsg: e.Error(), nextDelay: d})
	})

	// Three retryable failures → 3 attempts → 2 retry events logged
	// (the final attempt has no "next" so we don't log it).
	retryableErr := genai.APIError{Code: 503, Message: "transient"}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	_ = withRetry(ctx, func() error { return retryableErr })

	if len(got) != MaxRetryAttempts-1 {
		t.Fatalf("expected %d retry-log entries (one per attempt that has a next); got %d: %+v", MaxRetryAttempts-1, len(got), got)
	}
	// First entry: attempt=1, err quotes the upstream, next-delay=2s
	if got[0].attempt != 1 {
		t.Errorf("first entry attempt=%d want 1", got[0].attempt)
	}
	if got[0].nextDelay != 2*time.Second {
		t.Errorf("first entry next-delay=%v want 2s", got[0].nextDelay)
	}
	// Second entry: attempt=2, next-delay=4s
	if got[1].attempt != 2 {
		t.Errorf("second entry attempt=%d want 2", got[1].attempt)
	}
	if got[1].nextDelay != 4*time.Second {
		t.Errorf("second entry next-delay=%v want 4s", got[1].nextDelay)
	}
}

func TestSetRetryLogger_NilSwapsToNoOp(t *testing.T) {
	orig := retryLogger
	t.Cleanup(func() { retryLogger = orig })
	SetRetryLogger(nil)
	// Should not panic when called.
	retryLogger(1, errors.New("x"), time.Second)
}

func TestWithRetry_EventualSuccessAfterRetries(t *testing.T) {
	calls := 0
	retryableErr := genai.APIError{Code: 503}
	err := withRetry(context.Background(), func() error {
		calls++
		if calls < 2 {
			return retryableErr
		}
		return nil // succeed on the second attempt
	})
	if err != nil {
		t.Errorf("expected nil err after recovery on attempt 2; got %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (one fail + one success); got %d", calls)
	}
}
