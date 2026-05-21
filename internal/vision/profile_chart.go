package vision

import "google.golang.org/genai"

// chart profile — line/bar/pie/scatter charts, dashboards, metric
// panels. Optimized for "what does this data say" questions: extract
// axes (with units), series (with trends), and a small list of
// human-readable observations.

const chartPrompt = `You are extracting structured data from a chart, plot, dashboard, or metrics panel. Output JSON matching the schema.

- summary: 3-5 sentences naming what the chart shows and the headline takeaway.
- chart_type: line | bar | pie | scatter | area | combo | histogram | heatmap | gauge | other.
- title: the chart title; "" if none.
- axes: array of axis descriptors. Each axis has:
    orientation: x | y | secondary_y | radial.
    label: axis label as written.
    unit: unit shown or implied (e.g. 'ms', '%', 'requests/sec'); "" if not specified.
    range_text: human-readable range as written (e.g. '0-100', '2024 Jan-Dec'); "" if continuous.
- series: every distinct data series shown. Each series has:
    name: legend entry or series label.
    color: color described in plain English (e.g. 'blue', 'orange dashed'); "" if monochrome.
    trend: rising | falling | flat | cyclic | volatile | unknown.
    notable_values: array of {label, value} pairs for explicitly readable points (peaks, troughs, named bars). Empty array OK if all values are interpolated.
- key_observations: 3-5 strings, each a single-sentence factual claim about the data ('Series A peaks in March', 'Series B is flat'). Treat these as data points Claude can quote.

Do not invent precise numerical values not visibly readable from the chart.`

var chartSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"summary":    {Type: genai.TypeString},
		"chart_type": {Type: genai.TypeString},
		"title":      {Type: genai.TypeString},
		"axes": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"orientation": {Type: genai.TypeString},
					"label":       {Type: genai.TypeString},
					"unit":        {Type: genai.TypeString},
					"range_text":  {Type: genai.TypeString},
				},
				Required:         []string{"orientation", "label", "unit", "range_text"},
				PropertyOrdering: []string{"orientation", "label", "unit", "range_text"},
			},
		},
		"series": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"name":  {Type: genai.TypeString},
					"color": {Type: genai.TypeString},
					"trend": {Type: genai.TypeString},
					"notable_values": {
						Type: genai.TypeArray,
						Items: &genai.Schema{
							Type: genai.TypeObject,
							Properties: map[string]*genai.Schema{
								"label": {Type: genai.TypeString},
								"value": {Type: genai.TypeString},
							},
							Required:         []string{"label", "value"},
							PropertyOrdering: []string{"label", "value"},
						},
					},
				},
				Required:         []string{"name", "color", "trend", "notable_values"},
				PropertyOrdering: []string{"name", "color", "trend", "notable_values"},
			},
		},
		"key_observations": {
			Type:  genai.TypeArray,
			Items: &genai.Schema{Type: genai.TypeString},
		},
	},
	Required:         []string{"summary", "chart_type", "title", "axes", "series", "key_observations"},
	PropertyOrdering: []string{"summary", "chart_type", "title", "axes", "series", "key_observations"},
}

func init() {
	registerProfile(Profile{
		Name:              "chart",
		Prompt:            chartPrompt,
		Schema:            chartSchema,
		SystemInstruction: "You are extracting structured data from a chart. Respect the schema exactly. For every series, cite SPECIFIC numeric values you can read off the chart (use notable_values for start/end/peak/trough); do not summarize as 'rising trend' without giving the actual numbers. When values are estimated rather than directly labeled, prefix with '~' or '≈'. For axis labels and units, transcribe verbatim from the image; do not invent unit strings.",
	})
}
