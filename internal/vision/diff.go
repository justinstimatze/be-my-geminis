package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/genai"
)

// DescribeDiff runs the diff profile across two images, returning a
// structured Report whose Raw JSON contains the diff schema's fields
// (summary, diff_kind, changes[], unchanged_anchors[]).
//
// Both images go through ConvertForVision with the deliberate MaxDim
// (2576 by default — diff calls are deliberate by nature, not hook
// drive-bys, so the higher resolution is the right default to catch
// subtle pixel-level changes). The two parts ride in the same
// user-message Content in before-then-after order so the model
// receives them in the labeled sequence the prompt references.
//
// Only the diff profile is supported; other opts.Profile values
// return an error to prevent silent misuse — callers that want
// per-image describe should use Describe.
func (c *Client) DescribeDiff(ctx context.Context, beforeImage, afterImage []byte, opts Options) (*Report, error) {
	if opts.Profile != "" && opts.Profile != "diff" {
		return nil, fmt.Errorf("vision: DescribeDiff: only profile=diff is supported, got %q", opts.Profile)
	}
	prof, err := GetProfile("diff")
	if err != nil {
		return nil, fmt.Errorf("vision: diff profile not registered: %w", err)
	}

	maxDim := opts.MaxDim
	if maxDim == 0 {
		maxDim = DeliberateMaxDim
	}
	beforeJPG, err := ConvertForVision(beforeImage, maxDim)
	if err != nil {
		return nil, fmt.Errorf("vision: diff: convert before: %w", err)
	}
	afterJPG, err := ConvertForVision(afterImage, maxDim)
	if err != nil {
		return nil, fmt.Errorf("vision: diff: convert after: %w", err)
	}

	model := opts.Model
	if model == "" {
		model = DefaultProModel
	}
	tb := opts.ThinkingBudget
	if tb == nil {
		tb = DefaultThinkingBudgetFor(model)
	}

	// Same system_instruction + intent composition pattern as Describe.
	systemText := prof.SystemInstruction
	if opts.Intent != "" {
		intentText := "The downstream agent's specific task: " + opts.Intent +
			". Prioritize information relevant to this task throughout your " +
			"structured output, especially in the summary and the changes list."
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
	// Text first, then before image, then after image — same
	// text-conditioning argument as Describe. The model needs to know
	// the task (and the order convention) before the images land.
	parts := []*genai.Part{
		{Text: prof.Prompt},
		{InlineData: &genai.Blob{Data: beforeJPG, MIMEType: "image/jpeg"}},
		{InlineData: &genai.Blob{Data: afterJPG, MIMEType: "image/jpeg"}},
	}
	contents := []*genai.Content{{Role: "user", Parts: parts}}

	t0 := time.Now()
	var resp *genai.GenerateContentResponse
	err = withRetry(ctx, func() error {
		var e error
		resp, e = c.g.Models.GenerateContent(ctx, model, contents, cfg)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("vision: diff generate (%s): %w", model, err)
	}
	text := resp.Text()
	if text == "" {
		return nil, fmt.Errorf("vision: diff %s returned empty response", model)
	}
	var rep Report
	rep.Raw = json.RawMessage(text)
	if err := json.Unmarshal([]byte(text), &rep); err != nil {
		return nil, fmt.Errorf("vision: parse diff response: %w (head=%q)", err, head(text, 200))
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
