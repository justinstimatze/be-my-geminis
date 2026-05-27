# Changelog

All notable changes are documented here. Format roughly follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning
follows [SemVer](https://semver.org/) once 0.1.0 is tagged.

## [Unreleased]

_Nothing yet — see [0.3.0] below for the most recent release._

## [0.3.0] - 2026-05-27

### Added
- **Per-Read `#raw` escape hatch.** Append `#raw` to a Read path
  (`Read "frame.png#raw"`) to get raw pixels for that single Read
  instead of a vision report. The hook strips the sentinel and
  rewrites the Read to the real path. First mid-session bypass —
  `BMG_DISABLE` remains whole-session/launch-time. For perceptual
  tasks a description loses (design study, palette sampling, dense
  small-text UI, montages, pixel diffing). Surfaced in the
  SessionStart routing prime so agents discover it.

### Changed
- **Explicit intent overrides profile gating.** When an `intent` is
  passed, the system instruction now tells Gemini to honor the
  requested extraction even when the image isn't a conventional
  example of the profile (e.g. analyzing a film still as a UI for
  design study) rather than declining.
- **General profile flags montages.** Contact-sheet / grid inputs are
  now called out in the summary with a pointer to per-frame Reads or
  `#raw`, instead of silently returning a per-tile enumeration.

- **Dependencies.** Bumped `google.golang.org/genai` 1.55.0 → 1.57.0,
  `golang.org/x/image` 0.39.0 → 0.40.0, and the GitHub Actions
  (`checkout` 4→6, `setup-go` 5→6, `goreleaser-action` 6→7) via
  dependabot.

_The vision-surface changes address downstream friction reported in
`docs/feedback/2026-05-27-raw-visual-vs-vision-report.md`._

## [0.2.0] - 2026-05-21

## [0.2.0] - 2026-05-21

### Added — new capabilities
- **Video profile.** `.mp4` / `.webm` / `.mov` / `.mkv` / `.avi` / `.mpeg`
  / `.flv` routes through Gemini's Files API + a structured schema:
  summary, duration_seconds, scenes (with per-scene visual + audio
  + verbatim on-screen text), transcript (speaker-labeled), keyframes.
  ffprobe-based correction overwrites Gemini's commonly-drifted
  duration_seconds and scales all timestamps proportionally if
  ffprobe is on PATH.
- **Vision diff profile + `bmg diff` CLI.** Multi-image describe
  with a diff_kind enum (layout_shift, content_change, color_change,
  text_change, addition, removal, mixed, no_change), per-change
  region bboxes, before_text / after_text for text-bearing regions,
  unchanged_anchors as a sanity-check signal. For visual regression
  testing or design review.
- **Auto-profile routing.** Pass `profile=auto` and a fast
  gemini-2.5-flash classifier (<$0.0001, ~1s) picks one of the
  image profiles (general/chart/diagram/document/screenshot/code)
  before the deliberate describe. Available on both `bmg describe`
  and `bmg_describe` MCP. Hook path keeps general as the default
  to avoid the classifier latency.
- **PostToolUse:Bash hook for cache pre-warming.** When Claude runs
  Bash that produces images (matplotlib savefig, screenshot tools,
  gh diff attachments, image conversion utilities), bmg extracts
  the paths and spawns a detached describe-cached child. By the
  time Claude's follow-up Read fires, the cache is already warm.
  Hook itself returns in <100ms so it never blocks CC.
- **`bmg_query` MCP tool.** Dotted-path lookup against cached vision
  reports (`scenes.0.text`, `series.1.notable_values`). Cache-only
  — no Gemini call. Lets Claude retrieve a single value from a
  report that's rotated out of context, or pull one field without
  re-injecting the whole markdown.
- **MCP `bmg_describe` routes video paths.** Same routing pattern
  as the CLI: video extensions go through DescribeVideo with the
  6 min timeout and cache under a path-identity SHA
  (sha256(path|size|mtime) — bytes-hashing a 200MB video is wasteful).

### Added — resilience
- **Retry-with-exponential-backoff** on transient upstream failures.
  3 attempts at 0s / 2s / 4s. Triggers on `*genai.APIError` 5xx +
  429 and on network-error string patterns (connection-reset,
  i/o-timeout, EOF, no-such-host). Wired into Describe,
  DescribeVideo, DescribeDiff, ClassifyImage.
- **Per-attempt retry log lines** routed to `~/.claude/bmg/hook.log`
  via vision.SetRetryLogger so the hook's audit trail surfaces
  flaky upstream calls.
- **Integration tests against fake genai SDK server** (httptest)
  verify retry fires under the real SDK error pipeline, not just
  inside withRetry's unit tests. Both an exhausted-503 case and a
  recovers-after-two-503s case.
- **Opt-in Opus fallback** after retry exhaustion. Anthropic API
  via direct HTTP POST (no SDK dep), prose-only output wrapped in
  a synthetic Report with `fallback_provider=claude-opus-4-7` in
  the structured JSON. Daily $5 budget cap (configurable via
  `BMG_OPUS_FALLBACK_BUDGET_USD`); silently skipped if
  `ANTHROPIC_API_KEY` is unset. The trust fence's `origin=` attr
  reflects the swap, and the summary's first line names the
  fallback explicitly.

### Added — operational
- **`bmg doctor` dual-scope detection.** Scans both user-scope
  (~/.claude/settings.json) and project-scope
  (./.claude/settings.json) for bmg hooks. Any (event, matcher)
  registered in >1 file prints a "⚠ DUAL SCOPE" warning naming
  the conflict + recovery commands. Exit code 1 when detected.
  Direct response to the "every Read fired twice" bug observed
  during the run-through-paces session.
- **pressure_test/ extended** to cover v0.2 new surfaces:
  - 04 fence-integrity probe — verifies sanitizeFenceTokens
    defends against attacker-controlled `</bmg-` close tags
  - 08 video describe — end-to-end ffmpeg-synth → bmg-describe →
    schema + ffprobe correction
  - 09 post-tool prewarm — synthetic Bash payload → hook spawn →
    detached child → cache write
- **benchmark/ real-world set.** 4 public-domain reference images
  (EPA degree-days chart, GIMP-on-Linux screenshot, Night of the
  Living Dead frame, binary-search flowchart) with their own
  `run_real.sh` + `real_results/summary.md`. Methodologically
  distinct from synthetic regression baseline.

### Added — existing security hardening (pre-Opus-fallback batch)
- `vision.DefaultThinkingBudgetFor(model)` helper that picks the right
  thinking_budget per Gemini model (flash → 0, pro/unknown → 128).
  Replaces the brittle `HasPrefix("gemini-2.5-pro")` check that broke
  on `gemini-pro-latest` and the 3.x-pro line.
- `vision.ReadImageBytesBounded(path)` with a 50 MB cap and
  `image.DecodeConfig` pre-check rejecting >32-megapixel inputs.
  Defends against decompression-bomb / OOM patterns at the three
  attacker-influenceable read sites (hook, MCP, CLI describe).
- Trust-fence token sanitizer (`</bmg-` → `</_bmg-`) in the
  PreToolUse:Read renderer. Applied to Gemini's summary,
  pretty-printed structured JSON, and every OCR string so that an
  attacker-controlled image cannot collapse the fence boundary mid-
  document.
- `cmd/bmg/atomic.go` with `atomicWriteFile` (temp + rename) used for
  all six writes to `~/.claude/settings.json` and `~/.claude.json`.
  Prevents brick-on-Ctrl-C / OOM / partial-write of files Claude Code
  reads on every operation.
- SSH-style strictness on `~/.config/bmg/api_key`: refuses keys whose
  mode is wider than 0600 with a clear `chmod 600` suggestion.
- Cache directory mode re-assertion: stats the directory after
  `MkdirAll` (which never adjusts an existing dir's mode) and either
  chmods to 0o700 or refuses to use the cache so filenames (sha256 of
  image bytes) can't leak on multi-user boxes.
- `os.UserCacheDir()` fallback in `cache.resolveDir()`. macOS,
  FreeBSD, headless servers, and CI runners (where `$XDG_RUNTIME_DIR`
  is unset) now use `~/Library/Caches/bmg` / `~/.cache/bmg` /
  `%LocalAppData%/bmg` instead of world-readable `/tmp/bmg-cache`.
- `internal/cache/cache.go` schema versioning: cache filenames now
  embed `v1` so request-shape changes (model, temperature, prompt
  order, system_instruction composition) implicitly invalidate older
  entries rather than serving stale results to upgrade users.
- `SECURITY.md` (this changelog's sibling) with vulnerability-report
  contact, threat model, and explicit out-of-scope notes.

### Changed
- **`bmg uninstall --project`** no longer touches the user-scope MCP
  registration in `~/.claude.json`. The flag governs hook scope only;
  the prior behavior (always removing the MCP regardless of flag)
  surprised users with concurrent project + user installs.
- **`cache clean --dry-run`** now reports the actual count + bytes
  matching `--older-than`, not total occupancy with a "(actual count
  depends on filter)" disclaimer. Added `cache.Preview(olderThan)`
  that mirrors Clean's filter logic non-destructively.
- **`bmg --help` brought current**: usage list adds `bmg diff` +
  `bmg hook post-tool`, describe usage line includes `-profile`,
  tagline updated from "reads images via Gemini" to
  "Claude Code's persistent multimodal substrate via Gemini".
- **SessionStart routing prime** corrects the BMG_DISABLE wording:
  now says "set BEFORE launching Claude Code to bypass the hook for
  the entire session" instead of the operationally-wrong "bypass
  the hook for one Read". This is what Claude sees on every CC
  session, so the fix propagates immediately.
- **bmg describe usage error** when invoked without a path now
  enumerates the profile enum + names the new `image-or-video`
  surface.
- **README rewritten** to surface all v0.2 capabilities (video,
  auto-profile, pre-warm, diff, query) and reframe the pitch from
  "vision substrate" to "Claude Code's persistent multimodal
  substrate inside CC."
- **`cmd_init.go` split** into 3 files: entry points + path
  helpers, settings.json mutation, ~/.claude.json mutation.
- **Version constant** 0.1.0-dev → 0.2.0-dev.
- **Gemini request shape:**
  - Text-then-image part order (Google's own SDK example_test.go
    uses this; matches their current prompting guidance for task-
    conditioned multimodal calls).
  - `Role` field dropped on `SystemInstruction` Content (Gemini's
    content schema validates `user`/`model` only; system slot is
    dedicated and doesn't use roles).
  - `Temperature` defaults to 0 (schema-constrained extraction is
    deterministic-by-default; same image → same JSON, which also
    stabilizes the cache).
  - `intent` moved from user-prompt append into `system_instruction`
    (composed via `\n\n` if a profile system instruction is present)
    so task-priority hints aren't washed out by the schema-respect
    signal in the user message.
- `Describe` refactored to extract a pure `buildRequest` function for
  unit testability. Public signature unchanged.

### Fixed
- `gemini-pro-latest` and 3.x-pro variants no longer 400 with
  "Budget 0 is invalid" — the old `HasPrefix("gemini-2.5-pro")`
  heuristic in `cmd_describe.go` and `cmd_pre_read.go` zeroed the
  thinking budget for any model not starting with `gemini-2.5-pro`.

### Validated
- 144 unit tests (was 33 at v0.1 prep): adds 9 pressure-test
  surfaces (extended), 9 retry tests, 11 fallback tests
  (including a flock concurrency test), 4 deny-reason sanitizer
  tests, 11 bmg_query path-walker tests, 2 dual-scope regression
  tests, 2 cache Preview tests, plus all the v0.1-era additions
  enumerated below. Race-clean, gofmt clean, vet clean.
- Linux + macOS CI matrix.
- Integration tests against fake genai SDK server (skip in
  `-short`); ~12s wall when included.
- Targeted intent-priming verification on Hackers (1995) reference
  frames: with intent moved to `system_instruction`, bmg-pro surfaces
  detail anomalies (color-coded menu items, lone-warm-band lighting)
  that the prior user-prompt-append placement washed out.

## Initial (pre-0.1.0 history)

The pre-versioning commits, in reverse chronological order:

- `1271274` — Refresh benchmark + swap README case D from heatmap to
  bar chart
- `8d806bd` — Pro quality levers: differentiated MaxDim per surface +
  per-profile `system_instruction`
- `5523898` — `pressure_test/` suite (7 axes); fix MCP `isError` flag
  and missing image decoders
- `f7347a4` — GitHub Actions CI (gofmt + vet + test -race + build)
- `12cd5b9` — Initial README with 4 side-by-sides + profile reference
- `0851a90` — `benchmark/` — 9-case A/B raw Opus vs bmg profile-routed
- `dcc372b` — Write MCP registration to `~/.claude.json` (CC reads
  `mcpServers` from there exclusively, not from `settings.json`)
- `f52c1a7` — Initial commit: hook-routed Gemini vision + MCP server
  with 6 profiles

[Unreleased]: https://github.com/justinstimatze/be-my-geminis/compare/v0.3.0...main
[0.3.0]: https://github.com/justinstimatze/be-my-geminis/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/justinstimatze/be-my-geminis/releases/tag/v0.2.0
