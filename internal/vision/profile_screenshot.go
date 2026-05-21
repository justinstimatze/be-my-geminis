package vision

import "google.golang.org/genai"

// screenshot profile — UI state extraction from app/web/desktop screenshots.
// Heavier on element role + state than the general profile so Claude can
// reason about "what's clickable", "what's selected", "what's currently
// being entered" without re-asking.

const screenshotPrompt = `You are extracting UI state from a screenshot. Output JSON matching the schema.

- summary: 3-5 sentences naming the application/page/dialog and what the user appears to be doing.
- screen_title: the visible title bar, header, or page title; "" if none.
- elements: every interactive or labeled UI element. Each element has:
    box_2d: [ymin, xmin, ymax, xmax] in [0, 1000] normalized coords (origin top-left).
    role: button | input | link | text | heading | image | icon | container | menu | tab | other.
    label: short identifier you'd use to refer to this element (e.g. 'sign_in_button').
    text: verbatim visible text in the element (placeholder text counts; empty string if none).
    state: enabled | disabled | checked | unchecked | selected | empty | filled | active | inactive | unknown.
    is_interactive: true if a user can click/type into this element.
- focused_element_label: the label of whichever element appears to have focus or is the user's likely current target; "" if not determinable.

Be exhaustive on interactive elements; do not skip buttons, inputs, or menu items.`

var screenshotSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"summary":      {Type: genai.TypeString},
		"screen_title": {Type: genai.TypeString},
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
					"role":           {Type: genai.TypeString},
					"label":          {Type: genai.TypeString},
					"text":           {Type: genai.TypeString},
					"state":          {Type: genai.TypeString},
					"is_interactive": {Type: genai.TypeBoolean},
				},
				Required:         []string{"box_2d", "role", "label", "text", "state", "is_interactive"},
				PropertyOrdering: []string{"box_2d", "role", "label", "text", "state", "is_interactive"},
			},
		},
		"focused_element_label": {Type: genai.TypeString},
	},
	Required:         []string{"summary", "screen_title", "elements", "focused_element_label"},
	PropertyOrdering: []string{"summary", "screen_title", "elements", "focused_element_label"},
}

func init() {
	registerProfile(Profile{
		Name:              "screenshot",
		Prompt:            screenshotPrompt,
		Schema:            screenshotSchema,
		SystemInstruction: "You are extracting an accessibility-style UI tree from a screenshot. Every visible element gets a row: text, button, link, input, image, container, etc. Role and state matter for downstream test automation — distinguish disabled/enabled, focused/unfocused, error/normal. Transcribe visible text verbatim (button labels, error messages, placeholder text). Include the URL bar if present. Identify the focused or most prominent element via focused_element_label.",
	})
}
