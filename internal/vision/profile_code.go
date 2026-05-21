package vision

import "google.golang.org/genai"

// code profile — IDE/editor screenshots, terminal sessions, commits,
// diffs, error stack traces shown as images. Optimized so Claude can
// reconstruct or operate on the code text directly: language hint,
// verbatim code as a single multi-line string, and per-line annotations
// for syntax highlights/error markers/cursor position.

const codePrompt = `You are extracting code or terminal text from an image (IDE editor, REPL, terminal session, commit/diff view, traceback). Output JSON matching the schema.

- summary: 2-4 sentences describing what's shown (file/language/operation).
- language: best guess of the programming language or shell ('python', 'go', 'bash', 'sql', 'mixed', 'unknown').
- context: ide | terminal | repl | diff_view | traceback | rendered_code_block | commit_view | other.
- code_text: verbatim code or terminal text as a single string with embedded \n. Preserve indentation. If line numbers are visible in the gutter, do NOT include them in code_text — record those in annotations instead.
- annotations: array of per-line annotations the image shows visually. Each annotation has:
    line_number: 1-based index into code_text. -1 if not anchored to a specific line.
    kind: highlight | error | warning | info | comment | cursor | breakpoint | gutter_line_number | other.
    text: the annotation text or marker description.
- has_error: true if the image is showing an error/exception/diagnostic state.

Be faithful to the original character-for-character (including whitespace and unusual characters); do not modernize, format, or "fix" the code.`

var codeSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"summary":   {Type: genai.TypeString},
		"language":  {Type: genai.TypeString},
		"context":   {Type: genai.TypeString},
		"code_text": {Type: genai.TypeString},
		"annotations": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"line_number": {Type: genai.TypeInteger},
					"kind":        {Type: genai.TypeString},
					"text":        {Type: genai.TypeString},
				},
				Required:         []string{"line_number", "kind", "text"},
				PropertyOrdering: []string{"line_number", "kind", "text"},
			},
		},
		"has_error": {Type: genai.TypeBoolean},
	},
	Required:         []string{"summary", "language", "context", "code_text", "annotations", "has_error"},
	PropertyOrdering: []string{"summary", "language", "context", "code_text", "annotations", "has_error"},
}

func init() {
	registerProfile(Profile{
		Name:              "code",
		Prompt:            codePrompt,
		Schema:            codeSchema,
		SystemInstruction: "You are extracting verbatim code and editor state from an IDE/terminal/diff screenshot. The code_text field MUST be byte-accurate to what's visible — preserve indentation, syntax, blank lines. Do not auto-correct typos, complete partial code, or substitute equivalent constructs. Per-line annotations carry the visual layer (red squiggle = error, cursor position, breakpoint dot, highlight). Identify the language from syntax cues plus any visible filename/extension/repl prompt.",
	})
}
