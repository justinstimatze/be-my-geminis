package vision

import "google.golang.org/genai"

// generalPrompt is the default instruction sent alongside every
// drive-by PreToolUse:Read hook describe call. Profile-specific
// prompts (for the deliberate MCP / describe surfaces) extend this
// base.
const generalPrompt = `Describe this image as JSON matching the provided schema.
- summary: 3-6 sentences of dense prose covering what's shown.
- elements: every notable visible element. Each element has:
    box_2d: [ymin, xmin, ymax, xmax] in [0, 1000] normalized coords (origin top-left).
    label: short identifier (e.g. 'sign_in_button').
    kind: ui_button | text | input_field | image | icon | container | other.
    text: verbatim OCR of any text in the element, empty string if none.
Be exhaustive on elements; do not skip text labels, fields, or icons.`

// generalSchema is the responseSchema enforced by Gemini's structured-output
// API for the default profile. Verified to compile and round-trip cleanly
// against gemini-2.5-pro and gemini-2.5-flash on 2026-05-04 (Phase 0).
const generalSystemInstruction = `You are a visual-description substrate for an LLM agent that cannot reliably consume raw image bytes. Your output is the agent's only view of the image. Be exhaustive on visible content; do not editorialize, speculate beyond what's visually present, or omit text. The downstream agent treats your summary as ground truth (modulo your accuracy) and uses elements[] for spatial reasoning. When in doubt about a label, err toward concrete description over interpretation.`

func init() {
	registerProfile(Profile{
		Name:              "general",
		Prompt:            generalPrompt,
		Schema:            generalSchema,
		SystemInstruction: generalSystemInstruction,
	})
}

var generalSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"summary": {Type: genai.TypeString},
		"elements": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"box_2d": {
						Type:     genai.TypeArray,
						Items:    &genai.Schema{Type: genai.TypeInteger},
						MinItems: ptrInt64(4),
						MaxItems: ptrInt64(4),
					},
					"label": {Type: genai.TypeString},
					"kind":  {Type: genai.TypeString},
					"text":  {Type: genai.TypeString},
				},
				Required:         []string{"box_2d", "label", "kind", "text"},
				PropertyOrdering: []string{"box_2d", "label", "kind", "text"},
			},
		},
	},
	Required:         []string{"summary", "elements"},
	PropertyOrdering: []string{"summary", "elements"},
}

func ptrInt64(v int64) *int64 { return &v }
