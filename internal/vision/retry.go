package vision

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"
)

// RetryLogger is the optional logger withRetry calls between attempts
// so a caller (the hook, the CLI, an integration test) can observe
// that a retry is in flight rather than puzzling over latency it
// can't explain. The default writes to stderr with a structured
// "vision-retry attempt=N error=... next-delay=Ns" line. Replace via
// SetRetryLogger if you want different routing (file logger, no-op,
// etc.). Pure side-effect; never returns.
type RetryLogger func(attempt int, err error, nextDelay time.Duration)

var retryLogger RetryLogger = func(attempt int, err error, nextDelay time.Duration) {
	fmt.Fprintf(os.Stderr, "vision-retry attempt=%d err=%q next-delay=%s\n",
		attempt, err.Error(), nextDelay)
}

// SetRetryLogger swaps the package-level retry logger. The default
// goes to stderr; the hook + describe-cached callers may want to
// route to ~/.claude/bmg/hook.log instead (call this from main
// before any Describe call).
func SetRetryLogger(l RetryLogger) {
	if l == nil {
		retryLogger = func(int, error, time.Duration) {}
		return
	}
	retryLogger = l
}

// MaxRetryAttempts is the default total-attempt cap (initial call +
// retries). 3 attempts with the default delay schedule below means a
// worst-case latency adder of 6s (2s + 4s) before the final failure
// surfaces, which is acceptable on the deliberate path and tolerable
// on the hook path (75s overall timeout there).
const MaxRetryAttempts = 3

// retryDelay returns the wait before attempt N (0-indexed: attempt 0
// is the initial call which has no wait). Exponential schedule:
// 0s, 2s, 4s, 8s, ... — caps at 30s so an over-configured caller can't
// hold the line indefinitely.
//
// Pure function; safe to call from anywhere. Splitting it out of
// withRetry makes it directly unit-testable and easy to swap if a
// caller needs a tighter / looser schedule.
func retryDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	d := 2 * time.Second
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= 30*time.Second {
			return 30 * time.Second
		}
	}
	return d
}

// isRetryable returns true if err is plausibly a transient upstream
// failure that backing off and retrying might recover. Two layers:
//
//  1. Errors wrapping a *genai.APIError: retry on 5xx (server-side
//     fault) and on 429 (rate limit, where backoff is the point).
//     4xx other than 429 are deterministic — don't retry.
//  2. Errors not wrapping APIError but with messages indicating
//     network-layer flakiness ("connection reset", "i/o timeout",
//     "deadline exceeded", "EOF") are also worth one more shot.
//
// Anything else (decode errors, schema rejections, our own
// MaxImageBytes cap) is returned as-is.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code >= 500 || apiErr.Code == 429
	}
	// String-fallback for network errors that don't surface as an
	// APIError (the SDK wraps these as Go net errors). Match
	// conservatively — anything in this list is a real "the HTTP
	// call didn't come back cleanly" signal.
	msg := err.Error()
	for _, marker := range []string{
		"connection reset",
		"connection refused",
		"i/o timeout",
		"deadline exceeded",
		"EOF",
		"broken pipe",
		"no such host", // transient DNS hiccup
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// withRetry runs fn with up to MaxRetryAttempts attempts, sleeping
// retryDelay between attempts. Returns the FIRST attempt's success,
// or — after exhaustion — the LAST attempt's error (so the user sees
// the actual upstream message, not a generic "retry exhausted" string).
//
// Sleeps are context-aware: ctx.Done() interrupts the wait and
// returns ctx.Err(). On non-retryable errors, returns immediately
// without further attempts — wasting time on a deterministic failure
// would only delay the inevitable.
func withRetry(ctx context.Context, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < MaxRetryAttempts; attempt++ {
		if d := retryDelay(attempt); d > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
			}
		}
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !isRetryable(lastErr) {
			return lastErr
		}
		// Log this attempt's failure + the wait before the next.
		// Skip the log for the final attempt (no next wait) so the
		// log line says "next-delay=Xs" only when an actual retry
		// follows.
		if attempt+1 < MaxRetryAttempts {
			retryLogger(attempt+1, lastErr, retryDelay(attempt+1))
		}
	}
	return lastErr
}
