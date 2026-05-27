// Package vision is bmg's wrapper around the Gemini SDK
// (google.golang.org/genai). It owns: client construction, the
// per-profile prompt+schema registry, image + video conversion,
// the describe call, structured-output parsing, retry-with-backoff
// on transient upstream failures, and the opt-in Opus fallback
// when Gemini retries exhaust.
//
// Profiles: general, chart, diagram, document, screenshot, code,
// video, plus auto (classifier picks at call time) and diff
// (multi-image comparison).
package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"google.golang.org/genai"
)

// DefaultProModel is the Gemini model used for the deliberate (MCP /
// describe) surface. Pro requires thinking_budget≥128 (verified Phase 0).
const DefaultProModel = "gemini-2.5-pro"

// DefaultFlashModel is the Gemini model used for the drive-by hook
// surface. Flash accepts thinking_budget=0 and is ~10x cheaper.
const DefaultFlashModel = "gemini-2.5-flash"

// MinProThinkingBudget is the empirical floor for gemini-2.5-pro
// (rejects 0 and 64-127 with INVALID_ARGUMENT). Verified Phase 0.
// Used by the drive-by hook to keep token costs minimal.
const MinProThinkingBudget int32 = 128

// DefaultThinkingBudgetFor returns the safe default thinking_budget for
// model. Flash variants accept (and reward) tb=0 to skip thinking-token
// spend; pro variants require tb>=128 and reject 0 with INVALID_ARGUMENT.
// For unknown model names we default to MinProThinkingBudget — pro and
// pro-like models all currently accept ≥128, and a too-low budget on an
// unknown model surfaces a clearer SDK error than a too-high one.
//
// Returns a fresh pointer so callers can pass directly to genai.ThinkingConfig
// without aliasing a shared mutable.
func DefaultThinkingBudgetFor(model string) *int32 {
	if strings.Contains(strings.ToLower(model), "flash") {
		zero := int32(0)
		return &zero
	}
	v := MinProThinkingBudget
	return &v
}

// HookMaxDim is the image-resize cap applied to PreToolUse:Read hook
// calls. Keeps drive-by Gemini calls cheap.
const HookMaxDim = 800

// DeliberateMaxDim is the resize cap applied to the deliberate MCP /
// describe path, optimizing for quality. 2576 matches Opus 4.7's
// long-edge image cap so users get the same upper bound regardless
// of which path they choose. Substantially improves OCR and dense-
// figure readouts where small text or fine gradient detail matters
// (heatmap colorbar test in benchmark/ goes from "approx 0.7-0.8"
// to a tighter estimate at the higher resolution).
const DeliberateMaxDim = 2576

// Element is one entry in a vision report's elements array.
type Element struct {
	Box2D []int  `json:"box_2d"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
}

// Report is the parsed Gemini response plus call metadata. The Summary
// field is populated for every profile (every profile schema requires
// summary at top level). Elements is populated only for the general
// profile (preserved for the PreToolUse:Read hook's logging path);
// other profiles return their own structured shape in Raw.
//
// Raw is the verbatim JSON Gemini returned, ready to dump into the
// structured-analysis fence of the trust-fenced report. Renderers walk
// Raw for OCR text rather than relying on a typed Elements slice that
// only general carries.
type Report struct {
	Summary  string          `json:"summary"`
	Elements []Element       `json:"elements"`
	Raw      json.RawMessage `json:"-"`

	// Call metadata — not part of the schema; populated by Describe.
	Model           string        `json:"-"`
	Profile         string        `json:"-"`
	Latency         time.Duration `json:"-"`
	PromptTokens    int32         `json:"-"`
	CandidateTokens int32         `json:"-"`
	ThoughtsTokens  int32         `json:"-"`
	TotalTokens     int32         `json:"-"`
}

// Options control a single Describe call.
type Options struct {
	// Model is the Gemini model name. Defaults to DefaultProModel.
	Model string
	// Profile selects which (prompt, schema) pair drives the call.
	// Empty = "general" (the hook's default; deliberate callers can
	// pass "auto" to route via the classifier or pick explicitly).
	Profile string
	// Intent is an optional task hint biasing the description toward
	// task-relevant detail.
	Intent string
	// ThinkingBudget is the gemini-2.5-pro thinking-mode token budget.
	// nil → use 128 (the empirical floor for pro). Set explicitly to
	// int32Ptr(0) when calling flash (which accepts 0). Higher budgets
	// were measured to HURT perceptual cases (heatmap colorbar) — see
	// MinProThinkingBudget docstring — so callers should not raise this
	// without per-profile measurement.
	ThinkingBudget *int32
	// MaxDim is the longest-edge cap applied during image conversion.
	// 0 → use HookMaxDim=800 (cheap, drive-by). Set to
	// DeliberateMaxDim=2576 for the MCP / describe surface where
	// preserving small-text and fine-detail signal matters.
	MaxDim int
	// Temperature is the sampling temperature passed to Gemini.
	// nil → 0 (deterministic, the right default for schema-constrained
	// extraction — same image yields same JSON, which also stabilizes
	// the content-keyed cache). Callers who want exploration can pass
	// a non-zero pointer explicitly.
	Temperature *float32
}

// Client is a thin wrapper around *genai.Client that hides bmg-specific
// defaults (image conversion, general profile, retry policy hooks).
type Client struct {
	g *genai.Client
}

// New builds a vision client. The caller is responsible for resolving
// the API key (via internal/apikey).
func New(ctx context.Context, apiKey string) (*Client, error) {
	g, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("vision: new genai client: %w", err)
	}
	return &Client{g: g}, nil
}

// Describe runs the general-profile vision call against rawImage. The
// caller passes raw image bytes (any format Go's image package can
// decode); Describe converts to JPEG-q70 ≤800px before transmission.
//
// If opts.Profile == AutoProfile ("auto"), Describe first runs a fast
// gemini-2.5-flash classifier call (ClassifyImage) to pick the
// best-fit profile, then proceeds with that. The classifier cost is
// negligible (~$0.0001) but adds ~1s latency, so callers that
// already know the profile should pass it directly.
func (c *Client) Describe(ctx context.Context, rawImage []byte, opts Options) (*Report, error) {
	if opts.Profile == AutoProfile {
		result, _ := c.ClassifyImage(ctx, rawImage)
		// ClassifyImage always returns a usable profile (fallback to
		// "general" on any error), so we use result.Profile
		// unconditionally and discard the error — it's already
		// reflected in the fallback.
		opts.Profile = result.Profile
	}
	prof, err := GetProfile(opts.Profile)
	if err != nil {
		return nil, err
	}
	maxDim := opts.MaxDim
	if maxDim == 0 {
		maxDim = HookMaxDim
	}
	jpgBytes, err := ConvertForVision(rawImage, maxDim)
	if err != nil {
		return nil, fmt.Errorf("vision: convert: %w", err)
	}
	model := opts.Model
	if model == "" {
		model = DefaultProModel
	}
	tb := opts.ThinkingBudget
	if tb == nil {
		// Default to the pro floor. For flash, callers should pass
		// int32Ptr(0) explicitly since flash accepts it and we'd
		// otherwise spend thinking tokens unnecessarily.
		v := MinProThinkingBudget
		tb = &v
	}

	cfg, contents := buildRequest(prof, opts, jpgBytes, tb)

	t0 := time.Now()
	var resp *genai.GenerateContentResponse
	err = withRetry(ctx, func() error {
		var e error
		resp, e = c.g.Models.GenerateContent(ctx, model, contents, cfg)
		return e
	})
	if err != nil {
		wrappedGemini := fmt.Errorf("vision: generate (%s): %w", model, err)
		if fb, fbErr := tryOpusFallback(ctx, jpgBytes, opts, wrappedGemini); fb != nil {
			return fb, nil
		} else if fbErr != nil {
			return nil, fbErr
		}
		return nil, wrappedGemini
	}
	text := resp.Text()
	if text == "" {
		return nil, fmt.Errorf("vision: %s returned empty response", model)
	}
	var rep Report
	// Always preserve raw JSON so renderers / non-general profiles can
	// inspect the full structured response; per-typed-field unmarshal
	// only populates Summary + (where present) Elements.
	rep.Raw = json.RawMessage(text)
	if err := json.Unmarshal([]byte(text), &rep); err != nil {
		return nil, fmt.Errorf("vision: parse response: %w (head=%q)", err, head(text, 200))
	}
	rep.Model = model
	rep.Profile = prof.Name
	rep.Latency = time.Since(t0)
	if u := resp.UsageMetadata; u != nil {
		rep.PromptTokens = u.PromptTokenCount
		rep.CandidateTokens = u.CandidatesTokenCount
		rep.ThoughtsTokens = u.ThoughtsTokenCount
		rep.TotalTokens = u.TotalTokenCount
	}
	return &rep, nil
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// buildRequest is the pure config-assembly half of Describe — kept
// separate so the cfg / contents shape is unit-testable without an API
// call. Responsibilities:
//
//   - Compose system_instruction from prof.SystemInstruction plus any
//     per-call intent. Intent lives alongside the persona prime rather
//     than appended to the user prompt — the schema-respect signal in
//     the user message tends to drown task-prioritization hints when
//     they share that turn.
//   - Default Temperature to 0 when opts.Temperature is nil. Schema-
//     constrained extraction benefits from deterministic decode (same
//     image → same JSON, which also stabilizes the content-keyed cache).
//   - Put Text before InlineData in parts. Google's published Gemini
//     SDK examples use this order; lets vision tokens condition on the
//     task during cross-modal fusion rather than being read naively
//     before the prompt arrives.
//   - Omit Role on the SystemInstruction Content (Gemini's content
//     schema validates user/model only; system instructions live in a
//     dedicated config slot).
func buildRequest(prof Profile, opts Options, jpgBytes []byte, tb *int32) (*genai.GenerateContentConfig, []*genai.Content) {
	systemText := prof.SystemInstruction
	if opts.Intent != "" {
		intentText := "The downstream agent's specific task: " + opts.Intent +
			". Prioritize information relevant to this task throughout your " +
			"structured output, especially in the summary. Do not omit " +
			"standard schema fields, but their ordering and the depth of " +
			"detail should reflect this priority. An explicit task overrides " +
			"any instinct to decline based on the image's apparent type: if " +
			"asked to analyze a film still as a UI, extract a palette from a " +
			"photo, or treat any image as the requested subject, do so rather " +
			"than refusing because the image isn't a conventional example."
		if systemText != "" {
			systemText += "\n\n" + intentText
		} else {
			systemText = intentText
		}
	}
	temp := opts.Temperature
	if temp == nil {
		zero := float32(0)
		temp = &zero
	}
	cfg := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema:   prof.Schema,
		ThinkingConfig:   &genai.ThinkingConfig{ThinkingBudget: tb},
		Temperature:      temp,
	}
	if systemText != "" {
		cfg.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: systemText}},
		}
	}
	parts := []*genai.Part{
		{Text: prof.Prompt},
		{InlineData: &genai.Blob{Data: jpgBytes, MIMEType: "image/jpeg"}},
	}
	return cfg, []*genai.Content{{Role: "user", Parts: parts}}
}

// Ping makes a minimal text-only generate call to the configured flash
// model, used by `bmg doctor` to verify API key + network reachability.
// Returns latency and any error from the call. Output token cap is 1 to
// keep the cost negligible (~3 prompt tokens + 1 output token).
func (c *Client) Ping(ctx context.Context) (time.Duration, error) {
	zero := int32(0)
	one := int32(1)
	cfg := &genai.GenerateContentConfig{
		MaxOutputTokens: one,
		ThinkingConfig:  &genai.ThinkingConfig{ThinkingBudget: &zero},
	}
	contents := []*genai.Content{{
		Role:  "user",
		Parts: []*genai.Part{{Text: "ok"}},
	}}
	t0 := time.Now()
	_, err := c.g.Models.GenerateContent(ctx, DefaultFlashModel, contents, cfg)
	return time.Since(t0), err
}
