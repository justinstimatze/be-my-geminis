package vision

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/genai"
)

// TestRetry_IntegratesWithRealSDK_503s stands up a tiny HTTP server
// that mimics generativelanguage.googleapis.com's 503 response shape,
// points the genai SDK at it, and verifies that withRetry actually
// fires the retry loop against the real SDK's error-handling
// pipeline — not just against synthetic *genai.APIError values our
// unit tests construct directly.
//
// What this catches that unit tests can't:
//   - SDK changed its error wrapping (errors.As stops finding
//     *genai.APIError)
//   - 503 response body parsing changed (Code/Status no longer
//     populate, isRetryable's APIError check stops matching)
//   - Network-layer errors take a different code path than HTTP
//     status errors
//
// Slow (~6s wall — exercises the real 2s/4s backoff schedule).
// Skipped under `go test -short` so the dev loop stays fast.
func TestRetry_IntegratesWithRealSDK_503s(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping retry integration test in -short mode (~6s wall)")
	}

	var hits int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		// Mimic the generativelanguage.googleapis.com error envelope:
		// {"error": {"code": 503, "message": "...", "status":
		// "UNAVAILABLE"}}. The genai SDK parses this into
		// *genai.APIError which isRetryable then matches on
		// Code >= 500.
		fmt.Fprintln(w, `{"error":{"code":503,"message":"integration test fake outage","status":"UNAVAILABLE"}}`)
	}))
	defer server.Close()

	// Wire a genai.Client at the fake server. APIKey can be any
	// non-empty string; the fake server doesn't validate it.
	g, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey: "test-integration-key",
		HTTPOptions: genai.HTTPOptions{
			BaseURL: server.URL,
		},
	})
	if err != nil {
		t.Fatalf("genai.NewClient against fake server: %v", err)
	}
	c := &Client{g: g}

	// Minimal valid PNG so ConvertForVision succeeds and we reach
	// the actual GenerateContent call.
	pngBytes := makeIntegrationPNG(t)

	t0 := time.Now()
	_, err = c.Describe(context.Background(), pngBytes, Options{
		Model:   "gemini-2.5-flash",
		Profile: "general",
	})
	elapsed := time.Since(t0)

	if err == nil {
		t.Fatal("expected error after retries exhaust against always-503 server; got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("final error should mention 503; got %q", err.Error())
	}

	// MaxRetryAttempts=3 → 3 HTTP hits.
	if got := atomic.LoadInt64(&hits); got != int64(MaxRetryAttempts) {
		t.Errorf("server hit count = %d, expected %d (=MaxRetryAttempts). If genai's HTTP layer started retrying internally too we'd see more — investigate.",
			got, MaxRetryAttempts)
	}

	// Backoff schedule: 0s + 2s + 4s = 6s minimum spent in sleeps,
	// plus the tiny per-call HTTP roundtrip overhead.
	if elapsed < 5500*time.Millisecond {
		t.Errorf("elapsed %v too short — backoff schedule didn't run end-to-end?", elapsed)
	}
}

// TestRetry_IntegratesWithRealSDK_RecoversAfterTwo503s is the
// success-after-retry counterpart: 2 of 3 attempts return 503, the
// third returns a valid GenerateContentResponse. withRetry must hand
// the success back to Describe cleanly.
//
// Catches:
//   - withRetry doesn't return early on a transient-but-recoverable
//     pattern
//   - the SDK's response decoder still likes our 200 envelope
//   - Describe's result-unmarshal handles the json the same way
//     production does
func TestRetry_IntegratesWithRealSDK_RecoversAfterTwo503s(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping retry recovery integration test in -short mode (~6s wall — both backoff sleeps fire before the successful third attempt)")
	}

	var hits int64
	// The minimum 200 response that satisfies genai's decoder.
	// candidates[].content.parts[].text is required for resp.Text().
	const happyBody = `{
		"candidates":[{
			"content":{"role":"model","parts":[{"text":"{\"summary\":\"ok\",\"elements\":[]}"}]},
			"finishReason":"STOP"
		}],
		"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":20,"totalTokenCount":30}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, `{"error":{"code":503,"message":"transient","status":"UNAVAILABLE"}}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, happyBody)
	}))
	defer server.Close()

	g, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey: "test-integration-key",
		HTTPOptions: genai.HTTPOptions{
			BaseURL: server.URL,
		},
	})
	if err != nil {
		t.Fatalf("genai.NewClient: %v", err)
	}
	c := &Client{g: g}

	rep, err := c.Describe(context.Background(), makeIntegrationPNG(t), Options{
		Model:   "gemini-2.5-flash",
		Profile: "general",
	})
	if err != nil {
		t.Fatalf("Describe should have recovered on attempt 3; got %v", err)
	}
	if rep.Summary != "ok" {
		t.Errorf("Summary=%q want \"ok\" (the canned happy-body)", rep.Summary)
	}
	if got := atomic.LoadInt64(&hits); got != 3 {
		t.Errorf("server hit count = %d, expected 3 (2 fails + 1 success)", got)
	}
}

// makeIntegrationPNG produces a 4x4 valid PNG so ConvertForVision
// doesn't fail before we hit the HTTP path under test.
func makeIntegrationPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
