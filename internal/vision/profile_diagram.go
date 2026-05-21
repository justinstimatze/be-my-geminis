package vision

import "google.golang.org/genai"

// diagram profile — flowcharts, architecture diagrams, mind maps,
// sequence diagrams. Optimized so Claude can convert the result into
// mermaid/graphviz directly: nodes have stable IDs, edges reference
// them, kinds carry semantic weight (decision diamond vs process box).

const diagramPrompt = `You are extracting a graph from a diagram (flowchart, architecture sketch, mind map, sequence diagram, etc.). Output JSON matching the schema.

- summary: 3-5 sentences describing what the diagram shows and its overall structure.
- diagram_type: flowchart | architecture | mindmap | sequence | state | network | tree | other.
- nodes: every distinct box/circle/component in the diagram. Each node has:
    id: short unique stable identifier (e.g. 'n1', 'parse_input', 'auth_service'). Reuse this id in edges.
    box_2d: [ymin, xmin, ymax, xmax] in [0, 1000] normalized coords.
    label: human-readable display label (verbatim text shown in the node, or your concise paraphrase if no text).
    kind: process | decision | start | end | data | external_system | actor | resource | other.
    text: verbatim text inside the node (multi-line allowed; "" if no text).
- edges: every arrow/line connecting nodes. Each edge has:
    from_id: source node id.
    to_id: target node id.
    label: text on or alongside the edge ("" if unlabeled).
    direction: forward | backward | bidirectional | undirected.

If two nodes are visually grouped but distinct (e.g. labeled subprocess), emit them as separate nodes. Be exhaustive on edges; missing arrows lose graph shape.`

var diagramSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"summary":      {Type: genai.TypeString},
		"diagram_type": {Type: genai.TypeString},
		"nodes": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"id": {Type: genai.TypeString},
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
				Required:         []string{"id", "box_2d", "label", "kind", "text"},
				PropertyOrdering: []string{"id", "box_2d", "label", "kind", "text"},
			},
		},
		"edges": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"from_id":   {Type: genai.TypeString},
					"to_id":     {Type: genai.TypeString},
					"label":     {Type: genai.TypeString},
					"direction": {Type: genai.TypeString},
				},
				Required:         []string{"from_id", "to_id", "label", "direction"},
				PropertyOrdering: []string{"from_id", "to_id", "label", "direction"},
			},
		},
	},
	Required:         []string{"summary", "diagram_type", "nodes", "edges"},
	PropertyOrdering: []string{"summary", "diagram_type", "nodes", "edges"},
}

func init() {
	registerProfile(Profile{
		Name:              "diagram",
		Prompt:            diagramPrompt,
		Schema:            diagramSchema,
		SystemInstruction: "You are extracting a complete, machine-readable graph from a diagram. Include EVERY visible node and EVERY visible edge; do not collapse, summarize, or omit nodes that look minor. Edge labels matter: transcribe text on or next to arrows verbatim. Node kind classification matters: distinguish start/end/decision/process correctly based on shape AND context. Stable node IDs let Claude convert to mermaid/graphviz without re-parsing, so prefer concise, content-derived IDs over n1/n2.",
	})
}
