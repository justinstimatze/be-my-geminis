package vision

import "google.golang.org/genai"

// videoPrompt structures the Gemini 2.5 Pro video analysis. Gemini's
// video pipeline samples at 1 frame/sec plus audio; that's enough to
// catch scene boundaries and on-screen text without burning the
// per-frame token cost of a true frame-by-frame extraction.
const videoPrompt = `Analyze this video. Produce a structured analysis covering:
- summary: 3-6 sentences naming the video's subject, structure, and tone.
- duration_seconds: integer or float, the video's total length.
- scenes: every distinct scene with start/end seconds, what's visible,
  what's heard, and any verbatim on-screen text. A "scene" is a continuous
  shot or visually coherent segment; new scene at every cut, slide
  change, or major composition shift.
- transcript: spoken dialogue with start/end seconds + speaker label
  ("narrator", "speaker_1", or any named person identified on-screen).
  If no dialogue, return an empty array.
- keyframes: 3-10 semantically important moments — title cards, key UI
  states, dramatic visual events, anything a downstream consumer would
  want to jump to. Each entry is a timestamp + 1-2 sentence description.
Be precise about timestamps to one-second resolution. Quote on-screen
text verbatim — including typos and capitalization.`

const videoSystemInstruction = `You are a video-analysis substrate for an LLM agent that cannot consume video bytes. Your output is the agent's only view of the video. Be exhaustive on observable content; distinguish camera cuts (scene boundaries) from continuous motion (within a scene). Quote on-screen text verbatim. For dialogue, prefer summarized transcript over word-for-word unless the exchange is short and consequential. Timestamps should reflect what you observe in the video, not approximations — Gemini's video pipeline gives you per-second resolution, so use it.`

func init() {
	registerProfile(Profile{
		Name:              "video",
		Prompt:            videoPrompt,
		Schema:            videoSchema,
		SystemInstruction: videoSystemInstruction,
	})
}

var videoSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"summary":          {Type: genai.TypeString},
		"duration_seconds": {Type: genai.TypeNumber},
		"scenes": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"start_seconds": {Type: genai.TypeNumber},
					"end_seconds":   {Type: genai.TypeNumber},
					"summary":       {Type: genai.TypeString},
					"visual_elements": {
						Type:  genai.TypeArray,
						Items: &genai.Schema{Type: genai.TypeString},
					},
					"audio_summary": {Type: genai.TypeString},
					"on_screen_text": {
						Type:  genai.TypeArray,
						Items: &genai.Schema{Type: genai.TypeString},
					},
				},
				Required:         []string{"start_seconds", "end_seconds", "summary", "visual_elements", "audio_summary", "on_screen_text"},
				PropertyOrdering: []string{"start_seconds", "end_seconds", "summary", "visual_elements", "audio_summary", "on_screen_text"},
			},
		},
		"transcript": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"start_seconds": {Type: genai.TypeNumber},
					"end_seconds":   {Type: genai.TypeNumber},
					"speaker":       {Type: genai.TypeString},
					"text":          {Type: genai.TypeString},
				},
				Required:         []string{"start_seconds", "end_seconds", "speaker", "text"},
				PropertyOrdering: []string{"start_seconds", "end_seconds", "speaker", "text"},
			},
		},
		"keyframes": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"timestamp_seconds": {Type: genai.TypeNumber},
					"description":       {Type: genai.TypeString},
				},
				Required:         []string{"timestamp_seconds", "description"},
				PropertyOrdering: []string{"timestamp_seconds", "description"},
			},
		},
	},
	Required:         []string{"summary", "duration_seconds", "scenes", "transcript", "keyframes"},
	PropertyOrdering: []string{"summary", "duration_seconds", "scenes", "transcript", "keyframes"},
}
