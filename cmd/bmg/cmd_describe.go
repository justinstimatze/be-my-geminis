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

// cmdDescribe is the manual one-shot describe — no Claude Code involved.
// Useful for testing the vision pipeline outside the hook context.
//
//	bmg describe path/to/image.png            -> JSON report on stdout
//	bmg describe -model gemini-2.5-pro img    -> override model
//	bmg describe -intent "fix layout" img     -> bias output to task
func cmdDescribe(args []string) {
	fs := flag.NewFlagSet("describe", flag.ExitOnError)
	model := fs.String("model", os.Getenv("BMG_DESCRIBE_MODEL"), "Gemini model name (default: BMG_DESCRIBE_MODEL or gemini-2.5-pro)")
	intent := fs.String("intent", "", "optional task hint (biases output)")
	profile := fs.String("profile", "general", "analysis profile: auto | general | chart | diagram | document | screenshot | code | video")
	pretty := fs.Bool("pretty", true, "pretty-print JSON output")
	fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "bmg describe: usage: bmg describe [-model M] [-profile P] [-intent I] <image-or-video-path>")
		fmt.Fprintln(os.Stderr, "  profile: auto | general | chart | diagram | document | screenshot | code | video")
		os.Exit(1)
	}
	path := fs.Arg(0)
	if *model == "" {
		*model = vision.DefaultProModel
	}

	key, _, err := apikey.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg describe: %s\n", err)
		os.Exit(1)
	}

	// Video files go through Gemini's Files API (upload → poll → ref)
	// which has a different timeout shape from inline-image describe.
	// We give it a generous 6 min ceiling: upload (~30s for 100MB) +
	// processing (~60s for 5min video) + generate (~30-60s) +
	// headroom.
	isVideo := vision.IsVideoPath(path)
	timeout := 90 * time.Second
	if isVideo {
		timeout = 6 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c, err := vision.New(ctx, key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg describe: %s\n", err)
		os.Exit(1)
	}

	var rep *vision.Report
	if isVideo {
		// Video path: profile is locked to "video"; the Files API +
		// FileData part replace the inline-image upload. -profile
		// is ignored with a warning if the user passed something
		// other than "video" or the default.
		// "auto" is treated as a silent route to video here — auto's
		// whole purpose is "pick the right profile for me", and for a
		// video that's unambiguously the video profile. Only warn on
		// an explicit image-profile name that doesn't fit video.
		if *profile != "general" && *profile != "video" && *profile != vision.AutoProfile {
			fmt.Fprintf(os.Stderr, "bmg describe: ignoring -profile=%q for video; video files use the video profile\n", *profile)
		}
		rep, err = c.DescribeVideo(ctx, path, vision.Options{
			Model:          *model,
			Intent:         *intent,
			Profile:        "video",
			ThinkingBudget: vision.DefaultThinkingBudgetFor(*model),
		})
	} else {
		imgBytes, readErr := vision.ReadImageBytesBounded(path)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "bmg describe: read %s: %s\n", path, readErr)
			os.Exit(1)
		}
		// `bmg describe` is the deliberate, quality-optimized surface
		// (parallels the MCP path). We bump the image-resize cap to
		// DeliberateMaxDim so OCR / fine-detail signal survives the
		// resize. Empirically (see benchmark/results/), bumping
		// thinking_budget above the pro floor HURTS visual-lookup
		// tasks like heatmap colorbar reads. We stick with the
		// per-model floor.
		rep, err = c.Describe(ctx, imgBytes, vision.Options{
			Model:          *model,
			Intent:         *intent,
			Profile:        *profile,
			ThinkingBudget: vision.DefaultThinkingBudgetFor(*model),
			MaxDim:         vision.DeliberateMaxDim,
		})
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg describe: %s\n", err)
		os.Exit(1)
	}
	// Decode the raw structured JSON so callers can use any profile-
	// specific fields directly (chart.series, diagram.nodes, etc.). The
	// flat `elements` field stays for backwards compat with the general
	// profile.
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
		"elements":        rep.Elements,
		"structured":      raw,
	}
	enc := json.NewEncoder(os.Stdout)
	if *pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "bmg describe: encode output: %s\n", err)
		os.Exit(1)
	}
}
