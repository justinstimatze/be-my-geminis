package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Cross-provider fallback. When Gemini fails repeatedly (withRetry
// exhausted on a retryable error), this layer can route the same
// describe call to Anthropic's Opus API and return a synthetic
// Report wrapping Opus's prose response. The fence's origin
// attribute reflects the actual provider so Claude (and downstream
// consumers) see the swap explicitly.
//
// MVP scope:
//   - Prose-only output. Opus returns a free-form description; the
//     synthesized Report has Summary = the prose, and Raw set to a
//     fallback-marker JSON. Profile-typed extraction (chart series,
//     diagram nodes, etc.) is NOT preserved — schema-fidelity tier
//     B (Anthropic tool-call) is v0.4 territory.
//   - Triggered only after withRetry's 3 attempts exhaust on a
//     retryable Gemini error (5xx, network timeout). 4xx and
//     deterministic failures don't fall back.
//   - Daily $5 budget cap (configurable). Once exceeded today, the
//     fallback degrades to surfacing the Gemini error so the user
//     knows what happened.
//   - Hand-rolled HTTP to api.anthropic.com/v1/messages. The
//     Anthropic Go SDK is one POST endpoint of overhead we don't
//     need yet.

// OpusFallbackConfig controls when + how the Opus fallback fires.
// Resolve via loadOpusFallbackConfig (reads env + defaults).
type OpusFallbackConfig struct {
	// APIKey is the Anthropic API key. Empty → fallback disabled
	// silently (we surface the Gemini error as-is so users without
	// an Anthropic account get the same baseline behavior as
	// before this feature shipped).
	APIKey string
	// Model is the Anthropic model name. Defaults to
	// "claude-opus-4-7". Override via BMG_OPUS_FALLBACK_MODEL.
	Model string
	// DailyBudgetUSD is the maximum daily spend before fallback
	// stops firing. Default $5. Override via
	// BMG_OPUS_FALLBACK_BUDGET_USD.
	DailyBudgetUSD float64
	// BudgetFile tracks today's spend. Defaults to a path under
	// $XDG_RUNTIME_DIR/bmg/.
	BudgetFile string
	// InputPriceUSDPerMTok / OutputPriceUSDPerMTok are the per-
	// million-token prices used to estimate cost from Opus's usage
	// metadata. Defaults reflect public Opus pricing as of
	// 2026-05: $15 input, $75 output. Update these if Anthropic's
	// pricing moves.
	InputPriceUSDPerMTok  float64
	OutputPriceUSDPerMTok float64
}

const (
	defaultOpusModel       = "claude-opus-4-7"
	defaultOpusDailyBudget = 5.0

	// Pricing constants below are public per-million-token rates from
	// https://www.anthropic.com/pricing as of the date in the comment.
	// THESE GO STALE: Anthropic moves pricing on a roughly-quarterly
	// cadence (sometimes faster on new model rollouts). The MVP
	// fallback uses these to estimate cost for the daily budget cap.
	// Worst case if stale: the budget caps higher or lower than the
	// dollar amount the user configured — the system still works,
	// just the $/day number drifts. Re-check + update when bumping
	// Opus model versions or quarterly, whichever comes first.
	//
	// Last verified: 2026-05. Source page:
	//   https://www.anthropic.com/pricing
	defaultOpusInputPrice  = 15.0 // $/Mtok input
	defaultOpusOutputPrice = 75.0 // $/Mtok output

	anthropicMessagesURL    = "https://api.anthropic.com/v1/messages"
	anthropicVersion        = "2023-06-01"
	opusFallbackMaxTokens   = 1024
	opusFallbackHTTPTimeout = 60 * time.Second
	// opusResponseSizeCap caps the bytes we'll read from the Anthropic
	// response. A correct response for opusFallbackMaxTokens=1024 is
	// well under 10 KB; we allow 2 MiB so error envelopes, retry-after
	// HTML pages, and future schema growth all fit, but a hung or
	// malicious peer can't stream gigabytes into the hook child's
	// memory.
	opusResponseSizeCap = 2 << 20 // 2 MiB
)

func loadOpusFallbackConfig() OpusFallbackConfig {
	cfg := OpusFallbackConfig{
		APIKey:                os.Getenv("ANTHROPIC_API_KEY"),
		Model:                 os.Getenv("BMG_OPUS_FALLBACK_MODEL"),
		DailyBudgetUSD:        defaultOpusDailyBudget,
		InputPriceUSDPerMTok:  defaultOpusInputPrice,
		OutputPriceUSDPerMTok: defaultOpusOutputPrice,
	}
	if cfg.Model == "" {
		cfg.Model = defaultOpusModel
	}
	if s := os.Getenv("BMG_OPUS_FALLBACK_BUDGET_USD"); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil && v > 0 {
			cfg.DailyBudgetUSD = v
		}
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		cfg.BudgetFile = filepath.Join(d, "bmg", "opus-budget.json")
	} else if d, err := os.UserCacheDir(); err == nil && d != "" {
		cfg.BudgetFile = filepath.Join(d, "bmg", "opus-budget.json")
	} else {
		cfg.BudgetFile = "/tmp/bmg-opus-budget.json"
	}
	return cfg
}

// opusBudgetState is the on-disk tally. Per-day rollover: any read
// for a Date != today resets SpentUSD to 0.
type opusBudgetState struct {
	Date     string  `json:"date"`      // YYYY-MM-DD (UTC)
	SpentUSD float64 `json:"spent_usd"` // accumulated today
}

func readBudget(path string) opusBudgetState {
	today := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(path)
	if err != nil {
		return opusBudgetState{Date: today}
	}
	var s opusBudgetState
	if err := json.Unmarshal(data, &s); err != nil {
		return opusBudgetState{Date: today}
	}
	if s.Date != today {
		// Date rollover — start fresh.
		return opusBudgetState{Date: today}
	}
	return s
}

func writeBudget(path string, s opusBudgetState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	// Atomic-ish: write to tmp + rename. This function is the inner
	// half of a read-modify-write; cross-process serialization is the
	// caller's responsibility via lockedUpdateBudget (see
	// fallback_lock_unix.go).
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// tryOpusFallback decides whether to invoke Opus on a Gemini failure
// and either returns a synthetic Report (success) or surfaces the
// original Gemini error wrapped with whatever fallback context is
// relevant.
//
// Returns:
//   - (*Report, nil) on successful fallback
//   - (nil, geminiErr) when fallback is skipped (no API key, budget
//     exhausted, non-retryable gemini error). geminiErr is returned
//     unchanged so the caller surfaces the original message.
//   - (nil, augmentedErr) when fallback fired but itself failed. The
//     augmented error mentions both the gemini failure and the opus
//     failure so the user sees the full chain.
func tryOpusFallback(ctx context.Context, imgBytes []byte, opts Options, geminiErr error) (*Report, error) {
	if !isRetryable(geminiErr) {
		// Deterministic failures (auth, schema, invalid args) won't
		// recover under Opus either. Surface the original error.
		return nil, geminiErr
	}
	cfg := loadOpusFallbackConfig()
	if cfg.APIKey == "" {
		// Fallback not configured — pass the Gemini error through.
		return nil, geminiErr
	}
	budget := readBudget(cfg.BudgetFile)
	if budget.SpentUSD >= cfg.DailyBudgetUSD {
		return nil, fmt.Errorf("vision: gemini failed (%w); opus fallback budget exhausted today (spent $%.2f of $%.2f cap; raise BMG_OPUS_FALLBACK_BUDGET_USD or wait until UTC midnight)",
			geminiErr, budget.SpentUSD, cfg.DailyBudgetUSD)
	}

	t0 := time.Now()
	text, inputTok, outputTok, err := callOpus(ctx, cfg, imgBytes, opts)
	if err != nil {
		return nil, fmt.Errorf("vision: gemini failed (%w); opus fallback also failed: %v", geminiErr, err)
	}
	cost := (float64(inputTok)/1_000_000.0)*cfg.InputPriceUSDPerMTok +
		(float64(outputTok)/1_000_000.0)*cfg.OutputPriceUSDPerMTok
	// Best-effort: lockedUpdateBudget serializes the read-modify-write
	// across concurrent bmg processes so the daily tally stays honest.
	// If the lock or write fails we still return the report (the user
	// already paid for the Opus call); the next firing will read a
	// slightly stale tally.
	updateErr := lockedUpdateBudget(cfg.BudgetFile, func(s opusBudgetState) opusBudgetState {
		s.SpentUSD += cost
		return s
	})
	// Reflect the new running total in the Raw block we're about to
	// emit. budget.SpentUSD was read pre-call; add our cost so the
	// report shows post-call state regardless of update success.
	budget.SpentUSD += cost
	_ = updateErr

	// Synthesize a Report. Summary leads with the fallback notice
	// so Claude reads it before the prose body. Raw is the
	// fallback-marker JSON — explicitly NOT a profile-typed shape
	// so consumers know not to grep for chart_type / nodes / etc.
	summary := fmt.Sprintf("_Fallback used: gemini returned %s after retries; this report is from %s._\n\n%s",
		shortGeminiError(geminiErr), cfg.Model, text)
	rawObj := map[string]any{
		"summary":                       summary,
		"fallback_provider":             cfg.Model,
		"fallback_reason":               geminiErr.Error(),
		"structured_output_unavailable": true,
		"fallback_cost_usd":             cost,
		"fallback_budget_remaining_usd": cfg.DailyBudgetUSD - budget.SpentUSD,
	}
	rawBytes, _ := json.Marshal(rawObj)
	return &Report{
		Summary:         summary,
		Raw:             rawBytes,
		Model:           cfg.Model,
		Profile:         opts.Profile,
		Latency:         time.Since(t0),
		PromptTokens:    int32(inputTok),
		CandidateTokens: int32(outputTok),
		TotalTokens:     int32(inputTok + outputTok),
	}, nil
}

// shortGeminiError extracts a concise label from the wrapped Gemini
// error for the fallback notice ("503 UNAVAILABLE" rather than the
// full multi-line wrapped error).
func shortGeminiError(err error) string {
	msg := err.Error()
	if len(msg) > 80 {
		return msg[:80] + "..."
	}
	return msg
}

// callOpus does a single POST to api.anthropic.com/v1/messages with
// the image as a base64 inline block. Returns the prose text,
// usage tokens, and any error.
//
// Hand-rolled HTTP rather than the Anthropic Go SDK: this is one
// endpoint of overhead we don't need yet, and avoiding the SDK
// keeps bmg's dependency surface small.
func callOpus(ctx context.Context, cfg OpusFallbackConfig, imgBytes []byte, opts Options) (text string, inputTok, outputTok int, err error) {
	prompt := buildOpusPrompt(opts)
	encoded := base64.StdEncoding.EncodeToString(imgBytes)

	body := map[string]any{
		"model":      cfg.Model,
		"max_tokens": opusFallbackMaxTokens,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": "image/jpeg",
							"data":       encoded,
						},
					},
					map[string]any{
						"type": "text",
						"text": prompt,
					},
				},
			},
		},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", 0, 0, fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, opusFallbackHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicMessagesURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", 0, 0, err
	}
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)
	req.Header.Set("content-type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, opusResponseSizeCap))
	if err != nil {
		return "", 0, 0, fmt.Errorf("read response body: %w", err)
	}
	if int64(len(respBody)) == opusResponseSizeCap {
		// We hit the cap. The body is suspect — either truncated or
		// malicious. Treat as a parse failure so the caller surfaces it.
		return "", 0, 0, fmt.Errorf("anthropic response exceeded %d-byte cap", opusResponseSizeCap)
	}
	if resp.StatusCode >= 400 {
		return "", 0, 0, fmt.Errorf("anthropic HTTP %d: %s", resp.StatusCode, head(string(respBody), 200))
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", 0, 0, fmt.Errorf("parse response: %w (head=%q)", err, head(string(respBody), 200))
	}
	for _, c := range parsed.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	if text == "" {
		return "", 0, 0, fmt.Errorf("opus returned no text content")
	}
	return text, parsed.Usage.InputTokens, parsed.Usage.OutputTokens, nil
}

// buildOpusPrompt is the user-message text bmg sends to Opus. Plain
// prose request — no schema instructions because the MVP doesn't try
// to reconstruct profile-typed output. The agent reading the
// fallback report should be able to answer common questions about
// the image without seeing it.
func buildOpusPrompt(opts Options) string {
	base := "The user's primary image analysis service (Gemini) is currently unavailable. " +
		"Provide a thorough prose description of this image as a fallback. " +
		"The description should be detailed enough that a downstream agent can answer common questions about the image without seeing it. " +
		"Aim for 4-8 sentences covering layout, key visual elements, any visible text quoted verbatim, and notable details."
	if opts.Intent != "" {
		base += "\n\nThe downstream agent's specific task: " + opts.Intent +
			". Prioritize information relevant to this task."
	}
	if opts.Profile != "" && opts.Profile != "general" {
		base += fmt.Sprintf("\n\nThe profile the agent originally requested was %q — prioritize details that schema would care about (e.g. for 'chart' name axes + series + values; for 'diagram' name nodes + edges; for 'screenshot' name UI elements + their text). Note that you're producing prose, not the typed JSON schema — the agent knows this is a fallback.", opts.Profile)
	}
	return base
}
