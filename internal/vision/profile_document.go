package vision

import "google.golang.org/genai"

// document profile — text-heavy images: receipts, contracts, articles,
// slide decks, invoices, forms. Optimized for "what does this say"
// questions: extract headings, paragraphs, tables, and a key-facts
// summary so Claude doesn't have to OCR-walk the whole image to answer.

const documentPrompt = `You are extracting text and structure from a document image (receipt, contract, slide, invoice, article, form, etc.). Output JSON matching the schema.

- summary: 3-5 sentences describing what the document is and its main contents.
- doc_type: article | receipt | contract | slide | invoice | letter | form | email | book_page | screenshot_of_text | other.
- headings: every visible heading or section title. Each heading has:
    level: integer 1 (largest/topmost) through 6.
    text: verbatim heading text.
    box_2d: [ymin, xmin, ymax, xmax] in [0, 1000] normalized coords.
- paragraphs: every distinct prose block. Each paragraph has:
    text: verbatim text content with original line breaks preserved as \n.
    box_2d: [ymin, xmin, ymax, xmax] in [0, 1000] normalized coords.
- tables: any tabular content. Each table has:
    caption: caption or title; "" if none.
    rows: array of arrays of strings (first row is header if there is one).
- key_facts: 3-7 strings, each a single factual claim from the document (totals, dates, names, IDs, signature lines). Format as 'label: value' where helpful.

Be exhaustive on visible text. Preserve numbers, currency symbols, and punctuation exactly as shown.`

var documentSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"summary":  {Type: genai.TypeString},
		"doc_type": {Type: genai.TypeString},
		"headings": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"level": {Type: genai.TypeInteger},
					"text":  {Type: genai.TypeString},
					"box_2d": {
						Type:     genai.TypeArray,
						Items:    &genai.Schema{Type: genai.TypeInteger},
						MinItems: ptrInt64(4),
						MaxItems: ptrInt64(4),
					},
				},
				Required:         []string{"level", "text", "box_2d"},
				PropertyOrdering: []string{"level", "text", "box_2d"},
			},
		},
		"paragraphs": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"text": {Type: genai.TypeString},
					"box_2d": {
						Type:     genai.TypeArray,
						Items:    &genai.Schema{Type: genai.TypeInteger},
						MinItems: ptrInt64(4),
						MaxItems: ptrInt64(4),
					},
				},
				Required:         []string{"text", "box_2d"},
				PropertyOrdering: []string{"text", "box_2d"},
			},
		},
		"tables": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"caption": {Type: genai.TypeString},
					"rows": {
						Type: genai.TypeArray,
						Items: &genai.Schema{
							Type:  genai.TypeArray,
							Items: &genai.Schema{Type: genai.TypeString},
						},
					},
				},
				Required:         []string{"caption", "rows"},
				PropertyOrdering: []string{"caption", "rows"},
			},
		},
		"key_facts": {
			Type:  genai.TypeArray,
			Items: &genai.Schema{Type: genai.TypeString},
		},
	},
	Required:         []string{"summary", "doc_type", "headings", "paragraphs", "tables", "key_facts"},
	PropertyOrdering: []string{"summary", "doc_type", "headings", "paragraphs", "tables", "key_facts"},
}

func init() {
	registerProfile(Profile{
		Name:              "document",
		Prompt:            documentPrompt,
		Schema:            documentSchema,
		SystemInstruction: "You are extracting text and structure from a document image. Transcribe text VERBATIM; do not paraphrase. Preserve original formatting cues (currency symbols, decimal points, percent signs) exactly. For receipts/invoices, the key_facts array is load-bearing — extract vendor, date, line items, subtotal, tax, tip, and total as labeled key-value pairs. For tables, preserve column order and full row content. Doc_type classification matters: pick the most specific applicable label.",
	})
}
