// Command bmg is Be My Geminis — Claude Code's multimodal substrate via Gemini
// instead of crashing on them. Single static binary; subcommands are
// dispatched via plain switch on os.Args (no cobra) so the binary stays
// small and startup latency stays under 10ms (PreToolUse hooks fire on
// every Read).
//
// Subcommand surface:
//
//	bmg hook pre-read       PreToolUse:Read handler (called by Claude Code)
//	bmg hook post-tool      PostToolUse:Bash handler (cache pre-warming)
//	bmg hook session-start  SessionStart hook (trust-fence routing prime)
//	bmg init [--enable-mcp] Wire hooks into ~/.claude/settings.json (+ MCP)
//	bmg uninstall           Remove hook entries (and MCP, in user scope)
//	bmg doctor              Health check across 6 axes
//	bmg describe <path>     Manual one-shot describe (no Claude Code involved)
//	bmg diff a.png b.png    Structured visual diff
//	bmg cache stats|clean   Cache management
//	bmg mcp                 JSON-RPC stdio MCP server (CC spawns this)
//	bmg version             Print version
//
// All hook subcommands are wrapped with [hook.Guard] so a panic can
// never reach Claude Code (which would silently drop the redirect and
// surface raw image bytes — the failure mode bmg exists to prevent).
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/justinstimatze/be-my-geminis/internal/hook"
)

// version is the bmg release tag, exposed via `bmg --version` and
// embedded in the MCP serverInfo block. Declared as a var (not const)
// so release builds can inject the tag with -ldflags, e.g.:
//
//	go build -ldflags="-X main.version=v$(git describe --tags)" ./cmd/bmg
//
// goreleaser will do this automatically once we wire it up.
var version = "0.2.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(1)
	}
	args := os.Args[1:]
	switch args[0] {
	// Hooks — invoked by Claude Code via settings.json; never run by hand.
	case "hook":
		cmdHook(args[1:])

	// User-facing CLI.
	case "init":
		cmdInit(args[1:])
	case "doctor":
		cmdDoctor(args[1:])
	case "describe":
		cmdDescribe(args[1:])
	case "diff":
		cmdDiff(args[1:])
	case "describe-cached":
		// Internal: invoked by the post-tool hook to pre-warm the
		// cache for images Claude is likely to read next. Documented
		// in --help for transparency but most users never call it
		// directly.
		cmdDescribeCached(args[1:])
	case "cache":
		cmdCache(args[1:])
	case "mcp":
		cmdMCP()
	case "uninstall":
		cmdUninstall(args[1:])

	case "version", "--version", "-v":
		fmt.Println("bmg", version)
	case "help", "--help", "-h":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "bmg: unknown command %q\n\n", args[0])
		usage(os.Stderr)
		os.Exit(1)
	}
}

func cmdHook(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "bmg hook: missing subcommand (pre-read | session-start | post-tool)")
		os.Exit(1)
	}
	switch args[0] {
	case "pre-read":
		hook.Guard("pre-read", cmdPreRead)
	case "session-start":
		hook.Guard("session-start", cmdSessionStart)
	case "post-tool":
		hook.Guard("post-tool", cmdPostTool)
	default:
		fmt.Fprintf(os.Stderr, "bmg hook: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `bmg — Claude Code's persistent multimodal substrate via Gemini
       (Be My Geminis: be my eyes, but for an LLM)

Usage:
  bmg init [--project] [--enable-mcp]
                              Wire bmg hooks into Claude Code's settings.json.
                              --project installs hooks into <cwd>/.claude/
                                settings.json (vs the default ~/.claude/).
                              --enable-mcp also registers bmg as an MCP server
                                in ~/.claude.json (mcpServers.bemygeminis).
                              Restart Claude Code afterward — settings.json is
                              snapshot at session start, not hot-reloaded.
  bmg uninstall [--project]   Remove hook entries (and, in user scope, MCP).
  bmg doctor                  Health check (api key, cache dir, gemini reachable).
  bmg describe [-model M] [-intent I] [-profile P] <image-or-video>
                              Manual one-shot describe (no Claude Code involved).
                              P ∈ {auto, general, chart, diagram, document,
                                   screenshot, code, video}. Video extensions
                                   (.mp4/.webm/.mov/...) auto-route.
  bmg diff [-intent I] <before> <after>
                              Structured visual diff of two images
                              (regression testing, design review).
  bmg cache stats             Report cache occupancy (entries + bytes).
  bmg cache clean [--older-than DUR] [--dry-run]
                              Remove cached vision reports. Default removes all.
  bmg version                 Print version.

Hooks (invoked by Claude Code; not for manual use):
  bmg hook pre-read           PreToolUse:Read handler — redirects image reads
                              through Gemini for safe text descriptions.
  bmg hook post-tool          PostToolUse:Bash handler — pre-warms the cache
                              for images that Bash commands just produced
                              (matplotlib savefig, screenshot tools, etc.).
  bmg hook session-start      SessionStart — primes Claude on the trust-fenced
                              output format and the OCR-is-UNTRUSTED rule.

Configuration (env vars override the config file at ~/.config/bmg/api_key):
  BMG_API_KEY / GEMINI_API_KEY / GOOGLE_API_KEY
                              Gemini API key (resolution chain, in order).
  BMG_CACHE_DIR               Cache directory (default $XDG_RUNTIME_DIR/bmg
                              on Linux, ~/Library/Caches/bmg on macOS).
  BMG_DISABLE=1               Bypass the hook for the entire Claude Code
                              session (set before launching claude). There
                              is no per-Read mid-session toggle.
  BMG_HOOK_MODEL              Model for the hook (default gemini-2.5-flash).
  BMG_DESCRIBE_MODEL          Model for bmg describe (default gemini-2.5-pro).
  ANTHROPIC_API_KEY           Enables the opt-in Opus fallback when Gemini
                              retries exhaust. Unset = no fallback (the
                              Gemini error surfaces as-is). See README's
                              Resilience section for the threat model.
  BMG_OPUS_FALLBACK_MODEL     Anthropic model used for fallback (default
                              claude-opus-4-7).
  BMG_OPUS_FALLBACK_BUDGET_USD
                              Daily ceiling on Opus fallback spend (default
                              $5). Once exceeded, fallback stops firing
                              until UTC midnight.

See README.md for the pitch + install, SECURITY.md for the threat model,
CHANGELOG.md for release notes, CONTRIBUTING.md for the dev guide.
`)
}
