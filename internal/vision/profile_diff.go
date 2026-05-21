package vision

import "google.golang.org/genai"

// diffPrompt drives DescribeDiff. Two images come in (labeled
// "before" and "after"); the model returns a structured diff naming
// what changed in the second relative to the first. Designed for
// visual regression testing, design review, UI iteration — the kind
// of comparison where a prose paragraph would force the consumer
// to re-parse.
const diffPrompt = `Two images are provided. The first is "before", the second is "after". \
Produce a structured diff describing what changed in the after relative to the before.

- summary: 2-4 sentences naming the overall change at a glance.
- diff_kind: one of {layout_shift, content_change, color_change, text_change, addition, removal, mixed, no_change}.
  Pick "mixed" only if multiple kinds genuinely apply.
- changes: array of every notable difference. For each:
    - kind: one of {added, removed, modified, moved}.
    - region: bbox in the AFTER image as [ymin, xmin, ymax, xmax] in [0,1000] normalized coords.
      For removed elements, use the bbox in the BEFORE image instead.
    - description: 1-2 sentences naming what changed and how. Quote verbatim text on either
      side when text changed.
    - before_text: verbatim text from the BEFORE image at this region, empty string if not text-bearing.
    - after_text: verbatim text from the AFTER image at this region, empty string if removed.
- unchanged_anchors: 1-5 short descriptions of structural elements that are identifiably the same
  in both images (helps the consumer trust that we compared the right things — if the anchors don't
  match what they expect, the comparison was probably on mis-paired inputs).`

const diffSystemInstruction = `You are a visual-diff substrate for an LLM agent doing visual regression testing, design review, or UI iteration. The two input images are labeled "before" and "after" in that order. Be exhaustive on changes; do not editorialize about whether changes are improvements. Report what's different, region-by-region. If the two images are essentially identical, return diff_kind="no_change" with an empty changes array and at least one unchanged_anchor that names what's consistent. If the images are not comparable (different subjects, different aspect ratios at incompatible scales), say so in the summary and return diff_kind="mixed" with a single change of kind="modified" describing the global mismatch — don't try to force a diff on incompatible inputs.`

func init() {
	registerProfile(Profile{
		Name:              "diff",
		Prompt:            diffPrompt,
		Schema:            diffSchema,
		SystemInstruction: diffSystemInstruction,
	})
}

var diffSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"summary": {Type: genai.TypeString},
		"diff_kind": {
			Type: genai.TypeString,
			Enum: []string{
				"layout_shift",
				"content_change",
				"color_change",
				"text_change",
				"addition",
				"removal",
				"mixed",
				"no_change",
			},
		},
		"changes": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"kind": {
						Type: genai.TypeString,
						Enum: []string{"added", "removed", "modified", "moved"},
					},
					"region": {
						Type:     genai.TypeArray,
						Items:    &genai.Schema{Type: genai.TypeInteger},
						MinItems: ptrInt64(4),
						MaxItems: ptrInt64(4),
					},
					"description": {Type: genai.TypeString},
					"before_text": {Type: genai.TypeString},
					"after_text":  {Type: genai.TypeString},
				},
				Required:         []string{"kind", "region", "description", "before_text", "after_text"},
				PropertyOrdering: []string{"kind", "region", "description", "before_text", "after_text"},
			},
		},
		"unchanged_anchors": {
			Type:  genai.TypeArray,
			Items: &genai.Schema{Type: genai.TypeString},
		},
	},
	Required:         []string{"summary", "diff_kind", "changes", "unchanged_anchors"},
	PropertyOrdering: []string{"summary", "diff_kind", "changes", "unchanged_anchors"},
}
