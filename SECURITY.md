# Security policy

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security findings.

Report privately via GitHub Security Advisories:

  https://github.com/justinstimatze/be-my-geminis/security/advisories/new

You should expect an acknowledgement within 5 business days. Severe
findings (remote code execution, credential exfiltration, trust-fence
escape with a working PoC) are prioritized over hygiene issues.

When reporting, include:

- The bmg version (`bmg version` once available, or commit hash)
- The Claude Code version (`claude --version`)
- A minimal reproduction — for image-handling bugs, a synthetic test
  image is strongly preferred over a real-world one
- The threat model you have in mind

## Supported versions

bmg is pre-1.0. Only the latest tagged release receives security
backports. There is no LTS line.

## Threat model — what bmg defends against

bmg sits between Claude Code and the Google Gemini API. It mutates two
files Claude Code reads (`~/.claude/settings.json` and
`~/.claude.json`), reads arbitrary image paths Claude is asked to load,
and renders output that lands in Claude's context window. The
following threats are considered in scope:

- **Prompt injection via image OCR or Gemini summary text.** The
  trust-fence design (`<bmg-vision-report>` trusted vs
  `<bmg-ocr-text>` UNTRUSTED) plus SessionStart priming separates
  attacker-controllable image content from bmg's framing. The
  `cmd_pre_read.go` renderer sanitizes any closing fence tokens
  (`</bmg-`) that appear in attacker-influenceable strings.

- **Image decompression bombs.** `ReadImageBytesBounded` caps inputs
  at 50 MB; `ConvertForVision` rejects images whose declared
  dimensions exceed 32 megapixels via header-only `image.DecodeConfig`
  before `image.Decode` allocates a framebuffer.

- **Settings-file corruption.** All writes to `~/.claude/settings.json`
  and `~/.claude.json` use the temp-then-rename pattern
  (`cmd/bmg/atomic.go`), so an interrupted operation leaves the file
  at its previous content rather than truncated.

- **API key exposure.** Keys are never logged, never printed by `bmg
  doctor`, never echoed to error messages. `~/.config/bmg/api_key` is
  rejected if its mode is wider than 0600.

- **Multi-user cache side channels.** Cache directory mode is asserted
  to 0o700; pre-existing wider modes are corrected or refused. The
  default cache path is per-user (`$XDG_RUNTIME_DIR/bmg`,
  `~/.cache/bmg`, or `~/Library/Caches/bmg`) rather than world-readable
  `/tmp/bmg-cache`.

## Threat model — what bmg does NOT defend against

- **Vulnerabilities in Google's Gemini API or `google.golang.org/genai`
  SDK.** Track upstream. bmg pins SDK versions in `go.sum` to make
  vulnerable versions auditable but cannot patch them.

- **Vulnerabilities in Claude Code.** bmg communicates with CC via
  documented hook + MCP interfaces. If CC has a vulnerability that
  affects how it consumes hook output, that is Anthropic's surface.

- **A malicious user on the same machine with shell access.** bmg
  protects its own files (mode 0600 / dir 0700) but cannot defend
  against an attacker who can already read your shell environment or
  `~/.config/bmg/api_key` directly. Use OS-level user separation.

- **Network MITM between bmg and Gemini.** Relies on the Go stdlib
  TLS stack and Google's TLS termination.

- **Prompt injection that survives the trust-fence sanitizer.** The
  sanitizer is defense-in-depth, not a guarantee. A SessionStart
  prime that rotates out of Claude's context (long sessions, heavy
  compaction) reduces the model-side defense. For high-sensitivity
  work, `/clear` between sessions and consider `BMG_DISABLE=1`.

- **Opportunistic uploads via the PostToolUse:Bash cache pre-warmer.**
  When Claude runs Bash, bmg's post-tool hook scans `cwd` for images
  modified in the last 30s and pre-warms describes for them. If a
  Claude Code session is launched in a directory containing sensitive
  images (medical records, NDA'd designs, private screenshots) and
  any of those images are modified during the session, they may be
  uploaded to Gemini even though Claude never explicitly Read them.
  Mitigations: keep regulated-content directories out of CC's `cwd`,
  set `BMG_DISABLE=1` before launching CC for those workflows, or
  `bmg uninstall` (which removes the post-tool hook along with
  pre-read).

## Past advisories

None to date.
