package vision

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/genai"
)

// AutoProfile is the sentinel that callers pass to Options.Profile to
// trigger pre-call classification. The classifier picks one of the
// registered image profiles (general / chart / diagram / document /
// screenshot / code) by examining the image at the hook resize cap
// via gemini-2.5-flash with thinking_budget=0. Cost is negligible
// (~100 prompt tokens + ~30 output, <$0.0001 per call) and latency
// is ~1s, which is acceptable on the deliberate path but not on the
// drive-by hook — the hook should keep using "general" as its
// default.
const AutoProfile = "auto"

// classifyProfileNames is the closed enum the classifier picks from.
// Locked to the image profiles only — video is detected by extension
// upstream of any classification, so it never appears here. If a new
// image profile is added, add it here AND in classifyPrompt's option
// list so the model has a definition to match against.
var classifyProfileNames = []string{
	"general",
	"chart",
	"diagram",
	"document",
	"screenshot",
	"code",
}

const classifyPrompt = `Classify this image into the best-fit profile for downstream structured extraction. Pick exactly one:

- chart: a chart, graph, plot, or data visualization (bar, line, scatter, heatmap, pie, multi-series).
- diagram: a flowchart, network diagram, system schematic, mind map, tree, or other structured node-and-edge diagram.
- document: a scanned page, receipt, form, invoice, table, or text-heavy document.
- screenshot: a UI screenshot from any application (browser, desktop app, mobile app, terminal-as-app). Pick this whenever the image is mostly application chrome + content.
- code: a code listing, syntax-highlighted source snippet, or pure-text technical display where the content IS the source code.
- general: anything else — photos, scenes, illustrations, mixed-content composites, anything that doesn't fit the above more precisely.

Return the profile name and a 1-line reason.`

const classifySystemInstruction = `You are a fast image classifier for a vision-routing substrate. Pick the profile whose schema would best capture this image's content. Tiebreaker: prefer the more specific profile over general. If a screenshot contains a chart, prefer screenshot (the UI context is what the agent will want). If the image is genuinely ambiguous between two specific profiles, pick the one whose schema would extract more verbatim text from the image.`

var classifySchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"profile": {
			Type: genai.TypeString,
			Enum: classifyProfileNames,
		},
		"reason": {Type: genai.TypeString},
	},
	Required:         []string{"profile", "reason"},
	PropertyOrdering: []string{"profile", "reason"},
}

// ClassifyResult is what ClassifyImage returns — the picked profile
// name (always one of classifyProfileNames) plus the model's 1-line
// reason (useful for logging + debugging mis-classifications, not
// for caller logic).
type ClassifyResult struct {
	Profile string
	Reason  string
}

// ClassifyImage runs a fast gemini-2.5-flash call on rawImage and
// returns the recommended profile. Uses HookMaxDim resize + JPEG
// encoding (so classification is bounded by the same byte cap that
// the hook uses for its describe call). On any error — API failure,
// schema mismatch, unrecognized profile — returns
// ("general", err) so the caller can fall back gracefully.
func (c *Client) ClassifyImage(ctx context.Context, rawImage []byte) (ClassifyResult, error) {
	jpgBytes, err := ConvertForVision(rawImage, HookMaxDim)
	if err != nil {
		return ClassifyResult{Profile: "general"}, fmt.Errorf("vision: classify convert: %w", err)
	}

	zero := int32(0)
	temp := float32(0)
	cfg := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema:   classifySchema,
		ThinkingConfig:   &genai.ThinkingConfig{ThinkingBudget: &zero},
		Temperature:      &temp,
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{{Text: classifySystemInstruction}},
		},
	}
	parts := []*genai.Part{
		{Text: classifyPrompt},
		{InlineData: &genai.Blob{Data: jpgBytes, MIMEType: "image/jpeg"}},
	}
	contents := []*genai.Content{{Role: "user", Parts: parts}}

	var resp *genai.GenerateContentResponse
	err = withRetry(ctx, func() error {
		var e error
		resp, e = c.g.Models.GenerateContent(ctx, DefaultFlashModel, contents, cfg)
		return e
	})
	if err != nil {
		return ClassifyResult{Profile: "general"}, fmt.Errorf("vision: classify: %w", err)
	}
	text := resp.Text()
	if text == "" {
		return ClassifyResult{Profile: "general"}, fmt.Errorf("vision: classify returned empty response")
	}
	var out struct {
		Profile string `json:"profile"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return ClassifyResult{Profile: "general"}, fmt.Errorf("vision: classify parse: %w", err)
	}
	if !isKnownClassifyProfile(out.Profile) {
		return ClassifyResult{Profile: "general", Reason: out.Reason},
			fmt.Errorf("vision: classify returned unknown profile %q; fallback to general", out.Profile)
	}
	return ClassifyResult{Profile: out.Profile, Reason: out.Reason}, nil
}

func isKnownClassifyProfile(p string) bool {
	for _, name := range classifyProfileNames {
		if name == p {
			return true
		}
	}
	return false
}
