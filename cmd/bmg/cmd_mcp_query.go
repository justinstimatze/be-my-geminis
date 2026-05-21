package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/justinstimatze/be-my-geminis/internal/cache"
	"github.com/justinstimatze/be-my-geminis/internal/vision"
)

// queryToolDefinition is the MCP tool definition for bmg_query. Lets
// Claude pull a single value (or the whole structured JSON) out of a
// cached vision report without burning another Gemini call — useful
// when the report has rotated out of Claude's context but the cache
// still has it, or when Claude only wants one specific field rather
// than the whole markdown report re-injected.
func queryToolDefinition() map[string]any {
	return map[string]any{
		"name": "bmg_query",
		"description": "Query a cached bmg vision report's structured JSON by dotted path. " +
			"Cache-only; returns an error if the image hasn't been described yet (use " +
			"bmg_describe first). Dotted paths use map keys + array indices: " +
			"`summary`, `scenes.0.summary`, `series.1.notable_values`. Empty path returns " +
			"the entire structured JSON.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Absolute path to the image whose cached report we're querying.",
				},
				"query": map[string]any{
					"type":        "string",
					"description": "Dotted path into the structured JSON (e.g. 'summary', 'scenes.0.text', 'series.1.notable_values'). Empty string returns the entire JSON.",
				},
			},
			"required": []any{"path"},
		},
	}
}

// callQuery is the bmg_query implementation. Returns (text, isError)
// in the same shape callDescribe does so the MCP tools/call dispatch
// is uniform.
func callQuery(arguments json.RawMessage) (text string, isError bool) {
	var args struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	}
	if err := json.Unmarshal(arguments, &args); err != nil {
		return fmt.Sprintf("bmg_query: invalid arguments: %s", err), true
	}
	if args.Path == "" {
		return "bmg_query: path is required", true
	}

	imgBytes, err := vision.ReadImageBytesBounded(args.Path)
	if err != nil {
		return fmt.Sprintf("bmg_query: read %s: %s", args.Path, err), true
	}
	sha := cache.Key(imgBytes)
	c, err := cache.New()
	if err != nil {
		return fmt.Sprintf("bmg_query: %s", err), true
	}
	cachedPath, hit := c.Get(sha)
	if !hit {
		return fmt.Sprintf("bmg_query: no cached report for %s. Run bmg_describe first.", args.Path), true
	}

	// Cached entries are the trust-fenced markdown; the structured
	// JSON lives inside a ```json ... ``` code fence. Extract it.
	body, err := readFile(cachedPath)
	if err != nil {
		return fmt.Sprintf("bmg_query: read cache %s: %s", cachedPath, err), true
	}
	raw, ok := extractJSONFence(body)
	if !ok {
		return fmt.Sprintf("bmg_query: cached report at %s has no recognizable JSON fence (cache may be from an older bmg version; re-run bmg_describe to refresh)", cachedPath), true
	}

	if args.Query == "" {
		// Pretty-print the full structured JSON for the caller.
		var pretty any
		if err := json.Unmarshal([]byte(raw), &pretty); err != nil {
			return raw, false // return raw on parse failure rather than erroring
		}
		out, _ := json.MarshalIndent(pretty, "", "  ")
		return string(out), false
	}

	value, err := jsonPathWalk([]byte(raw), args.Query)
	if err != nil {
		return fmt.Sprintf("bmg_query: %s", err), true
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("bmg_query: encode result: %s", err), true
	}
	return string(encoded), false
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// jsonFenceRe matches the trust-fenced report's structured-analysis
// block. The renderer in cmd_pre_read.go wraps the JSON in
// ```json\n...\n``` inside <bmg-vision-report>. Greedy on newlines
// so we capture the full JSON body including nested braces.
var jsonFenceRe = regexp.MustCompile("(?s)```json\\n(.*?)\\n```")

// extractJSONFence pulls the first ```json ... ``` block out of the
// cached markdown body. Returns (json string, ok). Returns false if
// no fence found — the caller can surface a "cache shape changed,
// re-describe" message in that case.
func extractJSONFence(body string) (string, bool) {
	m := jsonFenceRe.FindStringSubmatch(body)
	if len(m) < 2 {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

// jsonPathWalk walks dotted into raw and returns the value at that
// path. Path syntax:
//   - empty string: returns the root (handled by caller)
//   - "key": map lookup at root
//   - "key.subkey": nested map lookups
//   - "key.0": array index (numeric segment indexes into array)
//   - "scenes.0.text": mixed array + map traversal
//
// Returns an error naming the path component where lookup failed
// (so the caller can surface "scenes.0 not found in cached JSON"
// rather than just "not found").
func jsonPathWalk(raw []byte, dotted string) (any, error) {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse cached JSON: %w", err)
	}
	if dotted == "" {
		return root, nil
	}
	segments := strings.Split(dotted, ".")
	cursor := root
	traversed := ""
	for _, seg := range segments {
		traversed = appendSeg(traversed, seg)
		switch v := cursor.(type) {
		case map[string]any:
			next, ok := v[seg]
			if !ok {
				return nil, fmt.Errorf("path %q: key %q not found at %q (available: %s)",
					dotted, seg, traversed, sortedKeys(v))
			}
			cursor = next
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil {
				return nil, fmt.Errorf("path %q: %q is not a numeric index at array position %q",
					dotted, seg, traversed)
			}
			if idx < 0 || idx >= len(v) {
				return nil, fmt.Errorf("path %q: index %d out of range at %q (array len %d)",
					dotted, idx, traversed, len(v))
			}
			cursor = v[idx]
		default:
			return nil, fmt.Errorf("path %q: cannot traverse %q into a non-collection value (type %T) at %q",
				dotted, seg, cursor, traversed)
		}
	}
	return cursor, nil
}

func appendSeg(traversed, seg string) string {
	if traversed == "" {
		return seg
	}
	return traversed + "." + seg
}

func sortedKeys(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Sort alphabetically for stable error messages — maps don't
	// preserve insertion order so we'd get unstable output otherwise.
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
