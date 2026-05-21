package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/justinstimatze/be-my-geminis/internal/apikey"
	"github.com/justinstimatze/be-my-geminis/internal/vision"
)

// cmdDiff is the user-facing vision-diff entry. Takes two image paths,
// runs them through gemini-2.5-pro with the diff profile, prints the
// structured diff JSON to stdout.
//
//	bmg diff before.png after.png
//	bmg diff -intent "find any UI regression" before.png after.png
//
// Both images go through the deliberate-path resize cap (2576) +
// JPEG q85 — diff calls are deliberate by nature; visible-pixel
// changes need the higher resolution to surface.
func cmdDiff(args []string) {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	model := fs.String("model", os.Getenv("BMG_DESCRIBE_MODEL"), "Gemini model name (default: BMG_DESCRIBE_MODEL or gemini-2.5-pro)")
	intent := fs.String("intent", "", "optional task hint biasing the diff (e.g. 'find UI regressions', 'look for chart value changes')")
	pretty := fs.Bool("pretty", true, "pretty-print JSON output")
	fs.Parse(args)
	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "bmg diff: usage: bmg diff [-intent I] <before-image> <after-image>")
		os.Exit(1)
	}
	beforePath := fs.Arg(0)
	afterPath := fs.Arg(1)
	if *model == "" {
		*model = vision.DefaultProModel
	}

	beforeBytes, err := vision.ReadImageBytesBounded(beforePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg diff: read %s: %s\n", beforePath, err)
		os.Exit(1)
	}
	afterBytes, err := vision.ReadImageBytesBounded(afterPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg diff: read %s: %s\n", afterPath, err)
		os.Exit(1)
	}

	key, _, err := apikey.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg diff: %s\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	c, err := vision.New(ctx, key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg diff: %s\n", err)
		os.Exit(1)
	}

	rep, err := c.DescribeDiff(ctx, beforeBytes, afterBytes, vision.Options{
		Model:          *model,
		Intent:         *intent,
		Profile:        "diff",
		ThinkingBudget: vision.DefaultThinkingBudgetFor(*model),
		MaxDim:         vision.DeliberateMaxDim,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg diff: %s\n", err)
		os.Exit(1)
	}

	var raw map[string]any
	_ = json.Unmarshal(rep.Raw, &raw)
	out := map[string]any{
		"model":           rep.Model,
		"profile":         rep.Profile,
		"latency_ms":      rep.Latency.Milliseconds(),
		"prompt_tokens":   rep.PromptTokens,
		"thoughts_tokens": rep.ThoughtsTokens,
		"total_tokens":    rep.TotalTokens,
		"summary":         rep.Summary,
		"structured":      raw,
		"before":          beforePath,
		"after":           afterPath,
	}
	enc := json.NewEncoder(os.Stdout)
	if *pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "bmg diff: encode output: %s\n", err)
		os.Exit(1)
	}
}
