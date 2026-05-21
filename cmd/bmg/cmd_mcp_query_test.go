package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractJSONFence_BasicMarkdown(t *testing.T) {
	body := "# Vision report\n\n<bmg-vision-report>\nsummary text\n\n## Structured analysis\n\n```json\n{\n  \"summary\": \"hi\",\n  \"elements\": []\n}\n```\n\n</bmg-vision-report>\n"
	got, ok := extractJSONFence(body)
	if !ok {
		t.Fatal("expected fence extraction to succeed on canonical bmg report shape")
	}
	if !strings.Contains(got, `"summary": "hi"`) {
		t.Errorf("extracted JSON missing expected field: %q", got)
	}
}

func TestExtractJSONFence_NoFence(t *testing.T) {
	body := "just some markdown without any json fence"
	if _, ok := extractJSONFence(body); ok {
		t.Error("expected extraction to fail on body with no fence")
	}
}

func TestExtractJSONFence_FirstFenceWins(t *testing.T) {
	body := "first:\n```json\n{\"a\": 1}\n```\n\nsecond:\n```json\n{\"b\": 2}\n```\n"
	got, ok := extractJSONFence(body)
	if !ok {
		t.Fatal("expected extraction to find first fence")
	}
	if !strings.Contains(got, `"a": 1`) {
		t.Errorf("expected first fence content; got %q", got)
	}
	if strings.Contains(got, `"b": 2`) {
		t.Errorf("greedy match leaked into second fence; got %q", got)
	}
}

func TestJSONPathWalk_RootKeys(t *testing.T) {
	raw := []byte(`{"summary": "hello", "elements": []}`)
	got, err := jsonPathWalk(raw, "summary")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Errorf("got %v want hello", got)
	}
}

func TestJSONPathWalk_NestedMap(t *testing.T) {
	raw := []byte(`{"a": {"b": {"c": 42}}}`)
	got, err := jsonPathWalk(raw, "a.b.c")
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := got.(float64); !ok || f != 42 {
		t.Errorf("got %v (%T) want 42", got, got)
	}
}

func TestJSONPathWalk_ArrayIndex(t *testing.T) {
	raw := []byte(`{"scenes": [{"text": "first"}, {"text": "second"}]}`)
	got, err := jsonPathWalk(raw, "scenes.1.text")
	if err != nil {
		t.Fatal(err)
	}
	if got != "second" {
		t.Errorf("got %v want second", got)
	}
}

func TestJSONPathWalk_EmptyPathReturnsRoot(t *testing.T) {
	raw := []byte(`{"a": 1}`)
	got, err := jsonPathWalk(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T want map", got)
	}
	if v, _ := m["a"].(float64); v != 1 {
		t.Errorf("root[a]=%v want 1", v)
	}
}

func TestJSONPathWalk_MissingKey(t *testing.T) {
	raw := []byte(`{"a": 1, "b": 2}`)
	_, err := jsonPathWalk(raw, "c")
	if err == nil {
		t.Fatal("expected error on missing key")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not-found: %v", err)
	}
	// Available-keys list should be in the error for debuggability.
	if !strings.Contains(err.Error(), "available") {
		t.Errorf("error should list available keys: %v", err)
	}
}

func TestJSONPathWalk_IndexOutOfRange(t *testing.T) {
	raw := []byte(`{"arr": [10, 20]}`)
	_, err := jsonPathWalk(raw, "arr.5")
	if err == nil {
		t.Fatal("expected error on out-of-range index")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("error should mention out-of-range: %v", err)
	}
}

func TestJSONPathWalk_NonNumericIndex(t *testing.T) {
	raw := []byte(`{"arr": [10, 20]}`)
	_, err := jsonPathWalk(raw, "arr.abc")
	if err == nil {
		t.Fatal("expected error on non-numeric array index")
	}
}

func TestJSONPathWalk_TraverseIntoScalar(t *testing.T) {
	raw := []byte(`{"a": "scalar"}`)
	_, err := jsonPathWalk(raw, "a.b")
	if err == nil {
		t.Fatal("expected error when traversing into a string")
	}
	if !strings.Contains(err.Error(), "non-collection") {
		t.Errorf("error should explain the non-collection issue: %v", err)
	}
}

func TestCallQuery_MissingPath(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"query": "summary"})
	text, isErr := callQuery(args)
	if !isErr {
		t.Error("expected isError when path is missing")
	}
	if !strings.Contains(text, "path is required") {
		t.Errorf("error text %q should mention path", text)
	}
}

func TestCallQuery_InvalidJSON(t *testing.T) {
	text, isErr := callQuery(json.RawMessage("not json"))
	if !isErr {
		t.Error("expected isError on malformed arguments")
	}
	if !strings.Contains(text, "invalid arguments") {
		t.Errorf("error text %q should mention invalid arguments", text)
	}
}
