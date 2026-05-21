package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/justinstimatze/be-my-geminis/internal/cache"
)

// cmdCache is the umbrella for cache management subverbs. Currently
// manual-only (clean / stats / clean --older-than). An automatic LRU
// eviction loop with a configurable size cap is a planned addition;
// for now this subcommand is the answer for users who set
// BMG_CACHE_DIR to a persistent path and need to bound it.
func cmdCache(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "bmg cache: usage: bmg cache (clean|stats) [flags]")
		os.Exit(1)
	}
	switch args[0] {
	case "clean":
		cmdCacheClean(args[1:])
	case "stats":
		cmdCacheStats(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "bmg cache: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func cmdCacheClean(args []string) {
	fs := flag.NewFlagSet("cache clean", flag.ExitOnError)
	olderThan := fs.Duration("older-than", 0, "only remove entries with mtime older than this duration (e.g. 24h, 7d→168h). Default 0 = remove all.")
	dryRun := fs.Bool("dry-run", false, "report what would be removed without removing")
	fs.Parse(args)

	c, err := cache.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg cache clean: %s\n", err)
		os.Exit(1)
	}
	if *dryRun {
		entries, bytes, err := c.Preview(*olderThan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bmg cache clean: %s\n", err)
			os.Exit(1)
		}
		noun := "entries"
		if entries == 1 {
			noun = "entry"
		}
		scope := ""
		if *olderThan > 0 {
			scope = fmt.Sprintf(" older than %s", *olderThan)
		}
		fmt.Printf("dry-run: would remove %d %s%s (%s) under %s\n",
			entries, noun, scope, humanBytes(bytes), c.Dir())
		return
	}
	removed, freed, err := c.Clean(*olderThan)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg cache clean: %s\n", err)
		os.Exit(1)
	}
	noun := "entries"
	if removed == 1 {
		noun = "entry"
	}
	scope := ""
	if *olderThan > 0 {
		scope = fmt.Sprintf(" older than %s", *olderThan)
	}
	fmt.Printf("removed %d %s%s (%s freed) from %s\n", removed, noun, scope, humanBytes(freed), c.Dir())
}

func cmdCacheStats(args []string) {
	c, err := cache.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg cache stats: %s\n", err)
		os.Exit(1)
	}
	entries, bytes, err := c.Stats()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmg cache stats: %s\n", err)
		os.Exit(1)
	}
	fmt.Printf("dir:     %s\n", c.Dir())
	fmt.Printf("entries: %d\n", entries)
	fmt.Printf("size:    %s\n", humanBytes(bytes))
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	suffix := []string{"KiB", "MiB", "GiB", "TiB"}[exp]
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), suffix)
}
