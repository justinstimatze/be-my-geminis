package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"google.golang.org/genai"
)

// MaxVideoBytes caps the on-disk video size bmg will upload. 200 MB is
// generous for ~10 min of compressed mobile video or ~5 min of 720p
// screen-recording while keeping per-call cost bounded. Files API itself
// accepts up to 2 GB; raise this only after measuring real cost.
const MaxVideoBytes int64 = 200 * 1024 * 1024

// videoMIMETypes maps file extensions to the MIME types Gemini's Files
// API recognizes for video. Anything not in this map is rejected by
// IsVideoPath / DescribeVideo before we burn an upload roundtrip.
var videoMIMETypes = map[string]string{
	".mp4":  "video/mp4",
	".mov":  "video/quicktime",
	".webm": "video/webm",
	".mkv":  "video/x-matroska",
	".avi":  "video/x-msvideo",
	".mpeg": "video/mpeg",
	".mpg":  "video/mpeg",
	".flv":  "video/x-flv",
}

// IsVideoPath returns true if path's extension matches a video format
// bmg knows how to upload. Used by callers (cmd_describe, future hook)
// to decide between Describe (images) and DescribeVideo.
func IsVideoPath(path string) bool {
	_, ok := videoMIMETypes[strings.ToLower(filepath.Ext(path))]
	return ok
}

// videoPollInterval is how often DescribeVideo polls Files.Get while
// the upload is in PROCESSING. Gemini's docs suggest 1-5s; we use 2s
// as a reasonable balance.
const videoPollInterval = 2 * time.Second

// videoProcessingTimeout caps how long DescribeVideo waits for the
// upload to become ACTIVE. Realistic processing time for a 5-min
// 720p video is 20-60s; 5 min is generous.
const videoProcessingTimeout = 5 * time.Minute

// DescribeVideo is the video-profile analog of Describe. Unlike images
// (which are sent inline as base64), videos go through Gemini's Files
// API: upload → poll for ACTIVE → reference by URI in GenerateContent
// → delete after use.
//
// The uploaded file is best-effort deleted on return so transient
// uploads don't accumulate against the user's Files quota. Failures
// to delete are logged but not surfaced as errors — the file will
// auto-expire after 48 hours per Gemini's retention policy.
//
// Only the video profile is supported here; other opts.Profile values
// return an error to prevent silent misuse.
func (c *Client) DescribeVideo(ctx context.Context, path string, opts Options) (*Report, error) {
	if opts.Profile != "" && opts.Profile != "video" {
		return nil, fmt.Errorf("vision: DescribeVideo: only profile=video is supported, got %q", opts.Profile)
	}
	prof, err := GetProfile("video")
	if err != nil {
		return nil, fmt.Errorf("vision: video profile not registered: %w", err)
	}

	// Header check: size + MIME.
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("vision: stat %s: %w", path, err)
	}
	if info.Size() > MaxVideoBytes {
		return nil, fmt.Errorf("video exceeds cap: %d bytes > %d (%d MB); raise MaxVideoBytes or trim the file",
			info.Size(), MaxVideoBytes, MaxVideoBytes/(1024*1024))
	}
	mime, ok := videoMIMETypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return nil, fmt.Errorf("vision: %s has no recognized video extension (have: %v)", path, videoExtensions())
	}

	model := opts.Model
	if model == "" {
		model = DefaultProModel
	}
	tb := opts.ThinkingBudget
	if tb == nil {
		tb = DefaultThinkingBudgetFor(model)
	}

	// Upload + poll.
	uploaded, err := uploadVideo(ctx, c.g, path, mime)
	if err != nil {
		return nil, err
	}
	// Best-effort cleanup. Don't use ctx here — if the parent ctx was
	// cancelled we still want the delete to land so we don't leak the
	// upload against the user's Files quota.
	defer func() {
		dctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = c.g.Files.Delete(dctx, uploaded.Name, nil)
	}()

	// Build request — same composition path as buildRequest but with
	// a FileData part instead of InlineData.
	systemText := prof.SystemInstruction
	if opts.Intent != "" {
		intentText := "The downstream agent's specific task: " + opts.Intent +
			". Prioritize information relevant to this task throughout your " +
			"structured output, especially in the summary and the keyframes. " +
			"Do not omit standard schema fields, but their ordering and depth " +
			"of detail should reflect this priority."
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
	parts := []*genai.Part{
		{Text: prof.Prompt},
		{FileData: &genai.FileData{
			FileURI:  uploaded.URI,
			MIMEType: uploaded.MIMEType,
		}},
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
		return nil, fmt.Errorf("vision: generate video (%s): %w", model, err)
	}
	text := resp.Text()
	if text == "" {
		return nil, fmt.Errorf("vision: video %s returned empty response", model)
	}
	raw := []byte(text)
	// Best-effort timestamp correction: Gemini's video pipeline
	// consistently mis-reports duration_seconds (observed ~40-50%
	// high on smoke tests of 92s + 149s reference videos). If
	// ffprobe is available locally and the declared duration
	// drifts >5% from the real duration, scale all *_seconds
	// fields proportionally so per-scene + per-keyframe timestamps
	// match the actual file. If ffprobe isn't installed or the
	// probe fails, return Gemini's response unchanged.
	if realDur, ok := probeVideoDuration(path); ok {
		if corrected, did := correctVideoTimestamps(raw, realDur); did {
			raw = corrected
		}
	}
	var rep Report
	rep.Raw = json.RawMessage(raw)
	if err := json.Unmarshal(raw, &rep); err != nil {
		return nil, fmt.Errorf("vision: parse video response: %w (head=%q)", err, head(string(raw), 200))
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

func uploadVideo(ctx context.Context, g *genai.Client, path, mime string) (*genai.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("vision: open %s: %w", path, err)
	}
	defer f.Close()

	uploaded, err := g.Files.Upload(ctx, f, &genai.UploadFileConfig{
		MIMEType:    mime,
		DisplayName: filepath.Base(path),
	})
	if err != nil {
		return nil, fmt.Errorf("vision: upload video: %w", err)
	}
	return waitForActive(ctx, g, uploaded)
}

func waitForActive(ctx context.Context, g *genai.Client, file *genai.File) (*genai.File, error) {
	deadline := time.Now().Add(videoProcessingTimeout)
	for file.State == genai.FileStateProcessing {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("vision: video file %s still PROCESSING after %s; giving up", file.Name, videoProcessingTimeout)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("vision: ctx cancelled while waiting for video %s to be ACTIVE: %w", file.Name, ctx.Err())
		case <-time.After(videoPollInterval):
		}
		got, err := g.Files.Get(ctx, file.Name, nil)
		if err != nil {
			return nil, fmt.Errorf("vision: poll video %s state: %w", file.Name, err)
		}
		file = got
	}
	if file.State != genai.FileStateActive {
		return nil, fmt.Errorf("vision: video upload landed in state %s (expected ACTIVE)", file.State)
	}
	return file, nil
}

func videoExtensions() []string {
	out := make([]string, 0, len(videoMIMETypes))
	for ext := range videoMIMETypes {
		out = append(out, ext)
	}
	return out
}

// probeVideoDuration shells out to ffprobe to get the real duration
// of path in seconds. Returns (0, false) if ffprobe isn't on PATH,
// the file is unreadable, or the output isn't parseable — silent
// fallback because the correction this enables is opportunistic.
func probeVideoDuration(path string) (float64, bool) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}
	dur, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || dur <= 0 {
		return 0, false
	}
	return dur, true
}

// durationCorrectionThreshold is the relative drift at which timestamps
// get scaled. Below this we leave the response alone — scaling by 1.02x
// is just noise and would muddle the audit trail (the report's raw
// content should match Gemini's output when correction wasn't needed).
const durationCorrectionThreshold = 0.05

// timestampFieldsByPath lists the JSON paths inside a video-profile
// response whose values are seconds. Used by correctVideoTimestamps
// to know what to scale. Tied to videoSchema in profile_video.go;
// any schema change that adds timestamp fields needs a sibling entry
// here, otherwise the new fields silently won't be corrected.
var timestampFieldsByPath = []struct {
	arrayKey string   // "" for root-level scalar; otherwise the array to walk
	fields   []string // field names within each array element to scale
}{
	{"", []string{"duration_seconds"}}, // root scalar
	{"scenes", []string{"start_seconds", "end_seconds"}},
	{"transcript", []string{"start_seconds", "end_seconds"}},
	{"keyframes", []string{"timestamp_seconds"}},
}

// correctVideoTimestamps reads raw (a video-profile JSON response),
// computes the scale factor real / declared, and if that factor differs
// from 1.0 by more than durationCorrectionThreshold scales every
// known timestamp field. Returns (corrected JSON, true) when a
// correction was applied, (raw, false) otherwise.
//
// Conservative: any parse error or missing declared duration → return
// the original bytes unchanged. The video-profile schema guarantees
// duration_seconds is Required so a missing value almost certainly
// means the response is malformed in some other way; safer to pass
// through than mutate.
func correctVideoTimestamps(raw []byte, realDur float64) ([]byte, bool) {
	if realDur <= 0 {
		return raw, false
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return raw, false
	}
	declared, ok := toFloat(root["duration_seconds"])
	if !ok || declared <= 0 {
		return raw, false
	}
	scale := realDur / declared
	if math.Abs(scale-1.0) < durationCorrectionThreshold {
		return raw, false
	}

	for _, spec := range timestampFieldsByPath {
		if spec.arrayKey == "" {
			root["duration_seconds"] = realDur
			continue
		}
		arr, ok := root[spec.arrayKey].([]any)
		if !ok {
			continue
		}
		for _, el := range arr {
			m, ok := el.(map[string]any)
			if !ok {
				continue
			}
			for _, f := range spec.fields {
				if v, ok := toFloat(m[f]); ok {
					m[f] = v * scale
				}
			}
		}
	}

	out, err := json.Marshal(root)
	if err != nil {
		return raw, false
	}
	return out, true
}

// toFloat coerces a JSON-decoded value (float64 from encoding/json,
// json.Number from a decoder with UseNumber, or an int when the
// caller built the map manually in a test) to float64. Returns
// (0, false) for any other type or for nil.
func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// Compile-time check that io.Reader satisfies the Upload signature; the
// genai SDK takes io.Reader, not *os.File specifically.
var _ io.Reader = (*os.File)(nil)
