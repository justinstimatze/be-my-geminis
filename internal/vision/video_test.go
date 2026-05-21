package vision

import (
	"encoding/json"
	"math"
	"testing"
)

func TestIsVideoPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"clip.mp4", true},
		{"CLIP.MP4", true}, // case-insensitive
		{"clip.mov", true},
		{"clip.webm", true},
		{"clip.mkv", true},
		{"clip.avi", true},
		{"clip.flv", true},
		{"clip.mpeg", true},
		{"clip.mpg", true},
		{"image.png", false},
		{"image.jpg", false},
		{"no-extension", false},
		{"audio.mp3", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsVideoPath(c.path); got != c.want {
			t.Errorf("IsVideoPath(%q)=%v want %v", c.path, got, c.want)
		}
	}
}

// makeVideoResponse fabricates a video-profile JSON response matching
// the videoSchema shape. Used by correctVideoTimestamps tests so we
// don't have to hand-write JSON in every case.
func makeVideoResponse(declared float64, sceneSecs, transcriptSecs, keyframeSecs []float64) []byte {
	root := map[string]any{
		"summary":          "test summary",
		"duration_seconds": declared,
	}
	scenes := make([]any, 0, len(sceneSecs)/2)
	for i := 0; i+1 < len(sceneSecs); i += 2 {
		scenes = append(scenes, map[string]any{
			"start_seconds":   sceneSecs[i],
			"end_seconds":     sceneSecs[i+1],
			"summary":         "scene",
			"visual_elements": []any{},
			"audio_summary":   "",
			"on_screen_text":  []any{},
		})
	}
	root["scenes"] = scenes

	transcript := make([]any, 0, len(transcriptSecs)/2)
	for i := 0; i+1 < len(transcriptSecs); i += 2 {
		transcript = append(transcript, map[string]any{
			"start_seconds": transcriptSecs[i],
			"end_seconds":   transcriptSecs[i+1],
			"speaker":       "narrator",
			"text":          "hello",
		})
	}
	root["transcript"] = transcript

	keyframes := make([]any, 0, len(keyframeSecs))
	for _, t := range keyframeSecs {
		keyframes = append(keyframes, map[string]any{
			"timestamp_seconds": t,
			"description":       "kf",
		})
	}
	root["keyframes"] = keyframes

	b, _ := json.Marshal(root)
	return b
}

func TestCorrectVideoTimestamps_ScalesAllFields(t *testing.T) {
	// Declared 100s, real 50s → scale 0.5.
	raw := makeVideoResponse(100.0,
		[]float64{0, 40, 40, 100}, // 2 scenes
		[]float64{10, 20, 60, 80}, // 2 transcript lines
		[]float64{5, 50, 90},      // 3 keyframes
	)
	corrected, did := correctVideoTimestamps(raw, 50.0)
	if !did {
		t.Fatal("expected correction to be applied (scale 0.5 well outside threshold)")
	}

	var got map[string]any
	if err := json.Unmarshal(corrected, &got); err != nil {
		t.Fatalf("corrected output is not valid JSON: %v", err)
	}

	if dur, _ := toFloat(got["duration_seconds"]); !approxEq(dur, 50.0, 0.001) {
		t.Errorf("duration_seconds=%v want 50", dur)
	}

	scenes := got["scenes"].([]any)
	want := [][2]float64{{0, 20}, {20, 50}}
	for i, s := range scenes {
		m := s.(map[string]any)
		start, _ := toFloat(m["start_seconds"])
		end, _ := toFloat(m["end_seconds"])
		if !approxEq(start, want[i][0], 0.001) || !approxEq(end, want[i][1], 0.001) {
			t.Errorf("scene[%d]=[%v,%v] want %v", i, start, end, want[i])
		}
	}

	tr := got["transcript"].([]any)
	wantTR := [][2]float64{{5, 10}, {30, 40}}
	for i, e := range tr {
		m := e.(map[string]any)
		start, _ := toFloat(m["start_seconds"])
		end, _ := toFloat(m["end_seconds"])
		if !approxEq(start, wantTR[i][0], 0.001) || !approxEq(end, wantTR[i][1], 0.001) {
			t.Errorf("transcript[%d]=[%v,%v] want %v", i, start, end, wantTR[i])
		}
	}

	kf := got["keyframes"].([]any)
	wantKF := []float64{2.5, 25, 45}
	for i, k := range kf {
		m := k.(map[string]any)
		ts, _ := toFloat(m["timestamp_seconds"])
		if !approxEq(ts, wantKF[i], 0.001) {
			t.Errorf("keyframe[%d]=%v want %v", i, ts, wantKF[i])
		}
	}
}

func TestCorrectVideoTimestamps_SkipsBelowThreshold(t *testing.T) {
	// Declared 100, real 102 → 2% drift, below 5% threshold.
	raw := makeVideoResponse(100.0,
		[]float64{0, 50},
		nil, nil,
	)
	corrected, did := correctVideoTimestamps(raw, 102.0)
	if did {
		t.Error("expected no correction for 2% drift")
	}
	// And the output should be byte-identical when no correction
	// happens — auditability matters.
	if string(corrected) != string(raw) {
		t.Errorf("expected raw to pass through unchanged when correction skipped")
	}
}

func TestCorrectVideoTimestamps_HandlesMalformedJSON(t *testing.T) {
	// Bad JSON → return original bytes, no panic.
	raw := []byte("not json")
	corrected, did := correctVideoTimestamps(raw, 100.0)
	if did {
		t.Error("expected no correction on bad JSON")
	}
	if string(corrected) != string(raw) {
		t.Error("expected raw passthrough on bad JSON")
	}
}

func TestCorrectVideoTimestamps_HandlesMissingDuration(t *testing.T) {
	// No duration_seconds → conservative skip.
	raw := []byte(`{"summary":"x","scenes":[],"transcript":[],"keyframes":[]}`)
	_, did := correctVideoTimestamps(raw, 100.0)
	if did {
		t.Error("expected no correction when duration_seconds is missing")
	}
}

func TestCorrectVideoTimestamps_HandlesZeroDeclared(t *testing.T) {
	// Declared 0 → can't compute scale; skip.
	raw := makeVideoResponse(0.0, []float64{0, 5}, nil, nil)
	_, did := correctVideoTimestamps(raw, 100.0)
	if did {
		t.Error("expected no correction when declared duration is 0")
	}
}

func TestCorrectVideoTimestamps_HandlesZeroReal(t *testing.T) {
	// Real 0 → skip (probeVideoDuration returns (0, false) in that case
	// but defensive double-check).
	raw := makeVideoResponse(100.0, []float64{0, 50}, nil, nil)
	_, did := correctVideoTimestamps(raw, 0.0)
	if did {
		t.Error("expected no correction when real duration is 0")
	}
}

func TestCorrectVideoTimestamps_UpscalesToo(t *testing.T) {
	// Declared 50, real 150 → scale 3.0 (upscale rather than the more
	// common downscale).
	raw := makeVideoResponse(50.0,
		[]float64{0, 10, 10, 50},
		nil, nil,
	)
	corrected, did := correctVideoTimestamps(raw, 150.0)
	if !did {
		t.Fatal("expected correction for 3.0x drift")
	}
	var got map[string]any
	json.Unmarshal(corrected, &got)
	scenes := got["scenes"].([]any)
	end, _ := toFloat(scenes[1].(map[string]any)["end_seconds"])
	if !approxEq(end, 150.0, 0.001) {
		t.Errorf("upscaled end=%v want 150", end)
	}
}

func approxEq(a, b, tol float64) bool {
	return math.Abs(a-b) < tol
}
