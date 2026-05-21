package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/justinstimatze/be-my-geminis/internal/apikey"
	"github.com/justinstimatze/be-my-geminis/internal/cache"
	"github.com/justinstimatze/be-my-geminis/internal/hook"
	"github.com/justinstimatze/be-my-geminis/internal/vision"
)

// cmdDescribeCached is the "pre-warm the cache" entry point. Takes one
// or more image paths and runs the same describe pipeline the
// PreToolUse:Read hook would on a miss — same model (flash by default),
// same MaxDim (HookMaxDim), same general profile — so the cached
// markdown the hook later reads on Claude's actual Read call is
// fungible with what the hook would have produced.
//
// Designed for background invocation from cmd_post_tool: writes to
// stderr only on error (never stdout), short-circuits on cache hit,
// and processes paths in sequence so the first path Claude is likely
// to Read becomes available even if later paths take longer.
//
// Usage:
//
//	bmg describe-cached /abs/path/to/foo.png [/abs/path/to/bar.jpg ...]
//
// All paths must be absolute. Relative paths are silently skipped
// (the hook can't reason about cwd at warm-time).
func cmdDescribeCached(args []string) {
	if len(args) == 0 {
		return
	}
	const name = "pre-warm"
	hook.Logf(name, "entry: %d path(s) — first=%s pid=%d xdg=%q home=%q",
		len(args), args[0], os.Getpid(),
		os.Getenv("XDG_RUNTIME_DIR"), os.Getenv("HOME"))

	key, _, err := apikey.Resolve()
	if err != nil {
		hook.Logf(name, "apikey.Resolve failed: %s", err)
		return
	}
	hook.Logf(name, "apikey ok (len=%d)", len(key))

	c, err := cache.New()
	if err != nil {
		hook.Logf(name, "cache.New failed: %s", err)
		fmt.Fprintf(os.Stderr, "bmg describe-cached: %s\n", err)
		return
	}
	hook.Logf(name, "cache dir: %s", c.Dir())

	model := os.Getenv("BMG_HOOK_MODEL")
	if model == "" {
		model = vision.DefaultFlashModel
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client, err := vision.New(ctx, key)
	if err != nil {
		hook.Logf(name, "vision.New failed: %s", err)
		fmt.Fprintf(os.Stderr, "bmg describe-cached: %s\n", err)
		return
	}

	for _, path := range args {
		if !filepath.IsAbs(path) {
			hook.Logf(name, "skip relative path: %s", path)
			continue
		}
		warmOne(ctx, c, client, model, name, path)
	}
	hook.Logf(name, "done all paths")
}

func warmOne(ctx context.Context, c *cache.Cache, client *vision.Client, model, name, path string) {
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()

	imgBytes, err := vision.ReadImageBytesBounded(path)
	if err != nil {
		hook.Logf(name, "ReadImageBytesBounded %s failed: %s", path, err)
		return
	}
	sha := cache.Key(imgBytes)
	if _, hit := c.Get(sha); hit {
		hook.Logf(name, "cache hit (skip describe) sha=%s for %s", sha[:12], path)
		return
	}
	hook.Logf(name, "describe-start sha=%s for %s (model=%s, bytes=%d)", sha[:12], path, model, len(imgBytes))

	rep, err := client.Describe(ctx, imgBytes, vision.Options{
		Model:          model,
		ThinkingBudget: vision.DefaultThinkingBudgetFor(model),
	})
	if err != nil {
		hook.Logf(name, "Describe failed for %s: %s", path, err)
		return
	}

	md := renderReport(path, sha, rep)
	cp, err := c.Put(sha, []byte(md))
	if err != nil {
		hook.Logf(name, "cache.Put failed for %s: %s", path, err)
		fmt.Fprintf(os.Stderr, "bmg describe-cached: put %s: %s\n", path, err)
		return
	}
	hook.Logf(name, "warm complete: %s → %s (model=%s tokens=%d latency=%dms)", path, cp, rep.Model, rep.TotalTokens, rep.Latency.Milliseconds())
}
