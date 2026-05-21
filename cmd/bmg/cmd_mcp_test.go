package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makePNG writes a w x h PNG to a temp file and returns its path. Used
// by tests that exercise file-path validation in callDescribe without
// making a Gemini call (anything that fails validation returns before
// the API call).
func makePNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "test.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDescribeToolDefinition_HasRequiredKeys(t *testing.T) {
	def := describeToolDefinition()
	if def["name"] != "bmg_describe" {
		t.Errorf("name=%v want bmg_describe", def["name"])
	}
	if _, ok := def["description"].(string); !ok {
		t.Error("description missing or not a string")
	}
	schema, ok := def["inputSchema"].(map[string]any)
	if !ok {
		t.Fatal("inputSchema missing or not a map")
	}
	if schema["type"] != "object" {
		t.Errorf("inputSchema.type=%v want object", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("inputSchema.properties missing")
	}
	for _, want := range []string{"path", "profile", "intent", "region", "model"} {
		if _, ok := props[want]; !ok {
			t.Errorf("inputSchema.properties missing %q — MCP clients won't know about this arg", want)
		}
	}
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatal("inputSchema.required missing")
	}
	if len(required) != 1 || required[0] != "path" {
		t.Errorf("required=%v want [\"path\"] only — adding required fields is a breaking change", required)
	}
}

func TestCallDescribe_InvalidJSON(t *testing.T) {
	text, isErr := callDescribe(json.RawMessage("not json"))
	if !isErr {
		t.Error("expected isError=true on malformed arguments")
	}
	if !strings.Contains(text, "invalid arguments") {
		t.Errorf("error text %q should mention invalid arguments", text)
	}
}

func TestCallDescribe_MissingPath(t *testing.T) {
	text, isErr := callDescribe(json.RawMessage(`{"profile": "general"}`))
	if !isErr {
		t.Error("expected isError=true when path is missing")
	}
	if !strings.Contains(text, "path is required") {
		t.Errorf("error text %q should mention path is required", text)
	}
}

func TestCallDescribe_RelativePath(t *testing.T) {
	text, isErr := callDescribe(json.RawMessage(`{"path": "relative/path.png"}`))
	if !isErr {
		t.Error("expected isError=true on relative path")
	}
	if !strings.Contains(text, "absolute") {
		t.Errorf("error text %q should say path must be absolute", text)
	}
}

func TestCallDescribe_NonexistentPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.png")
	args, _ := json.Marshal(map[string]string{"path": missing})
	text, isErr := callDescribe(args)
	if !isErr {
		t.Error("expected isError=true on missing file")
	}
	if !strings.Contains(text, "read") {
		t.Errorf("error text %q should mention read failure", text)
	}
}

func TestCropRegion_DegenerateReturnsOriginal(t *testing.T) {
	path := makePNG(t, 100, 100)
	raw, _ := os.ReadFile(path)
	// Degenerate region (ymax <= ymin) should return original unchanged.
	out, err := cropRegion(raw, []int{500, 0, 100, 1000})
	if err != nil {
		t.Fatalf("cropRegion: %v", err)
	}
	if !bytes.Equal(out, raw) {
		t.Error("degenerate region should pass through unchanged")
	}
}

func TestCropRegion_WrongLengthFails(t *testing.T) {
	path := makePNG(t, 100, 100)
	raw, _ := os.ReadFile(path)
	_, err := cropRegion(raw, []int{0, 0, 100}) // 3 elements, not 4
	if err == nil {
		t.Fatal("expected error on 3-element region")
	}
	if !strings.Contains(err.Error(), "4") && !strings.Contains(err.Error(), "ymin") {
		t.Errorf("error %q should explain region shape", err.Error())
	}
}

func TestCropRegion_ClampsOutOfBounds(t *testing.T) {
	path := makePNG(t, 100, 100)
	raw, _ := os.ReadFile(path)
	// Way out of bounds: 2000 = 200% of normalized range. Should clamp,
	// not panic.
	_, err := cropRegion(raw, []int{0, 0, 2000, 2000})
	if err != nil {
		t.Errorf("cropRegion should clamp out-of-range coords, not error: %v", err)
	}
}

func TestCropRegion_ValidCropProducesJPEG(t *testing.T) {
	path := makePNG(t, 100, 100)
	raw, _ := os.ReadFile(path)
	out, err := cropRegion(raw, []int{100, 100, 800, 800})
	if err != nil {
		t.Fatalf("cropRegion: %v", err)
	}
	// JPEG starts with FF D8.
	if len(out) < 2 || out[0] != 0xFF || out[1] != 0xD8 {
		t.Errorf("cropRegion output is not JPEG: first bytes %x %x", out[0], out[1])
	}
}

func TestClampInt(t *testing.T) {
	cases := []struct {
		v, lo, hi, want int
	}{
		{5, 0, 10, 5},
		{-1, 0, 10, 0},
		{15, 0, 10, 10},
		{0, 0, 10, 0},
		{10, 0, 10, 10},
	}
	for _, c := range cases {
		if got := clampInt(c.v, c.lo, c.hi); got != c.want {
			t.Errorf("clampInt(%d, %d, %d)=%d want %d", c.v, c.lo, c.hi, got, c.want)
		}
	}
}
