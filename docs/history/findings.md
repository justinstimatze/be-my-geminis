# bmg pre-v0.1.0 panel review — consolidated findings

Five panels (Security, Skeptical HN User, CV/Multimodal LLM Expert,
OSS Maintainer, Future Maintainer + API Stability) reviewed bmg
before the v0.1.0 tag + public-flip. This file consolidates every
block-release finding and tracks status. Original per-panel files
live next to this one (`batch1_*.md`, `batch2_*.md`).

## Status legend

- **FIXED** — addressed; commit hash linked
- **DEFERRED** — explicit post-v0.1.0 decision, rationale given
- **WONTFIX** — finding doesn't hold under verification (overclaim,
  user rejection, or fact-check failure)
- **OPEN** — still pending before tag (none at time of writing)

## Block-release findings — closed

### Security panel — closed

| # | Finding | Status |
|---|---|---|
| S1 | Trust-fence breakout via OCR text containing `</bmg-ocr-text>` | **FIXED** [`1f1e9a6`](https://github.com/justinstimatze/be-my-geminis/commit/1f1e9a6) — `sanitizeFenceTokens` in `cmd_pre_read.go` neutralizes any `</bmg-` token in attacker-influenceable strings |
| S2 | Image decoder bomb (`os.ReadFile` + `image.Decode` unbounded) | **FIXED** [`1f1e9a6`](https://github.com/justinstimatze/be-my-geminis/commit/1f1e9a6) — `ReadImageBytesBounded` caps at 50 MB; `image.DecodeConfig` pre-check rejects >32 megapixels before allocation |
| S3 | Non-atomic settings.json + ~/.claude.json writes | **FIXED** [`1f1e9a6`](https://github.com/justinstimatze/be-my-geminis/commit/1f1e9a6) — `atomicWriteFile` (temp + rename) at all six write sites |
| S4 | Cache dir mode not re-asserted on existing `/tmp/bmg-cache` | **FIXED** [`1f1e9a6`](https://github.com/justinstimatze/be-my-geminis/commit/1f1e9a6) — `cache.New` stats the dir post-`MkdirAll`, chmods to 0700 if owned, refuses if wider |
| S5 | `~/.config/bmg/api_key` mode not validated on read | **FIXED** [`1f1e9a6`](https://github.com/justinstimatze/be-my-geminis/commit/1f1e9a6) — SSH-style strictness; rejects modes wider than 0600 with `chmod 600` suggestion |
| S6 | Race window on ~/.claude.json read-modify-write | **FIXED** (mitigated) [`e43b2ac`](https://github.com/justinstimatze/be-my-geminis/commit/e43b2ac) — atomic writes close the partial-write failure mode; `bmg init` + `uninstall` now print a TIP to quit CC sessions to avoid the lost-update case. Full POSIX-lock approach deferred (CC itself doesn't use flock; the lock would only serialize against bmg's own concurrent runs) |
| S7 | No SECURITY.md / no signed releases / no SBOM | **FIXED** [`441e0f0`](https://github.com/justinstimatze/be-my-geminis/commit/441e0f0) — `SECURITY.md` with vulnerability-report flow + threat model. Release signing (sigstore / GitHub provenance attestation) deferred to v0.1.0 release-workflow setup |

### Skeptical HN panel — closed

| # | Finding | Status |
|---|---|---|
| H1 | README cherry-picks 4 of 9 cases; misses include losses | **FIXED** [`fdc3b41`](https://github.com/justinstimatze/be-my-geminis/commit/fdc3b41) — README replaced with per-case verdict table covering all 9 |
| H2 | Case 06: bmg returns wrong answer (~78 vs 85.7); README hides it | **FIXED** [`fdc3b41`](https://github.com/justinstimatze/be-my-geminis/commit/fdc3b41) — verdict table calls case 06 a bmg loss explicitly; FAQ names per-pixel chart readouts as Opus's domain |
| H3 | Case 07's wording was changed mid-development; reads as p-hacking | **FIXED** [`fdc3b41`](https://github.com/justinstimatze/be-my-geminis/commit/fdc3b41) — README has "Benchmark provenance" section disclosing the original ambiguity, the disambiguation rationale, and the generator-source ground truth |
| H4 | Zero privacy disclosure in README | **FIXED** [`fdc3b41`](https://github.com/justinstimatze/be-my-geminis/commit/fdc3b41) — "Data flow and privacy" section above Install names the endpoint, tier-dependent retention policy, kill switches, and explicit NDA/regulated-content warning |
| H5 | `BMG_DISABLE=1` "for one Read" wording is wrong | **FIXED** [`fdc3b41`](https://github.com/justinstimatze/be-my-geminis/commit/fdc3b41) (README), [`0caf8fd`](https://github.com/justinstimatze/be-my-geminis/commit/0caf8fd) (`bmg --help`) — both now say "set BMG_DISABLE=1 BEFORE launching Claude Code bypasses the hook for the entire session; no per-Read mid-session toggle exists" |
| H6 | Cost claims undercounted (~$5/yr extrapolation vs "cents") | **FIXED** (partial) [`fdc3b41`](https://github.com/justinstimatze/be-my-geminis/commit/fdc3b41) — README FAQ now says "single-digit dollars per heavy CC week, less for typical use" instead of "cents"; mentions `bmg cache stats` as the audit path. Worked-example with measured numbers deferred |
| H7 | "Why not Claude Opus" FAQ answer is defensive | **FIXED** [`fdc3b41`](https://github.com/justinstimatze/be-my-geminis/commit/fdc3b41) — FAQ rewritten to lead with the two architectural value-adds (typed shape + injection resistance) and explicitly names cases where bmg is NOT the right install |
| H8 | Hook fires on every image Read; no per-image opt-out | **FIXED** (documented) [`fdc3b41`](https://github.com/justinstimatze/be-my-geminis/commit/fdc3b41) — README "Data flow and privacy" section names the broader concern and recommends `bmg uninstall` + cache clean for sensitive-content workflows. Per-image opt-out (flag-file mechanism) deferred — CC-side `/bmg-pause` slash command would be ideal but needs CC support |

### CV / Multimodal panel — closed

| # | Finding | Status |
|---|---|---|
| CV1 | Image-then-text part order in `vision.go:170-173` | **FIXED** [`893cdcb`](https://github.com/justinstimatze/be-my-geminis/commit/893cdcb) — swapped to text-then-image, anchored against Google's own `genai@v1.55.0/example_test.go` |
| CV2 | `Role:"system"` on SystemInstruction Content | **FIXED** [`893cdcb`](https://github.com/justinstimatze/be-my-geminis/commit/893cdcb) — `Role` field dropped |
| CV3 | Temperature defaults to 1.0 from SDK | **FIXED** [`893cdcb`](https://github.com/justinstimatze/be-my-geminis/commit/893cdcb) — `Options.Temperature *float32` defaults to ptr(0); pointer-to-zero marshals correctly (not omitted by `omitempty`) |
| CV4 | Single-sample benchmark at temp=1.0 | **PARTIAL** [`f61ad2b`](https://github.com/justinstimatze/be-my-geminis/commit/f61ad2b) — benchmark refreshed at temp=0 (deterministic decode); formal n=5 with mean ± std deferred per "Benchmark provenance" section of README |
| CV5 | `intent` appended to user prompt instead of system_instruction | **FIXED** [`893cdcb`](https://github.com/justinstimatze/be-my-geminis/commit/893cdcb) — `intent` composed into `system_instruction` via `\n\n`. Empirically validated on Hackers reference frames: surfaces detail anomalies that the prior placement washed out |

### OSS Maintainer panel — closed

| # | Finding | Status |
|---|---|---|
| O1 | `go 1.26` in `go.mod` locks out audience | **WONTFIX** — user dismissed: Go 1.26 has been released ~3 months at this point, adoption time is sufficient |
| O2 | `go install ...@latest` 404s until repo is public + tag exists | **OPEN** (intentional) — README documents the clone-and-build fallback; resolves on public-flip + v0.1.0 tag |
| O3 | macOS cache fallback wrong (`$XDG_RUNTIME_DIR` unset → world-shared `/tmp/bmg-cache`) | **FIXED** [`1f1e9a6`](https://github.com/justinstimatze/be-my-geminis/commit/1f1e9a6) — `os.UserCacheDir()` inserted between XDG and `/tmp` fallback (gives `~/Library/Caches/bmg` on macOS) |
| O4 | Windows unaddressed; PreToolUse command may break | **DEFERRED** — README now says "Windows unsupported" explicitly; cross-platform CI deferred. Windows users get a clear non-claim instead of a broken implicit promise |
| O5 | CHANGELOG.md missing | **FIXED** [`441e0f0`](https://github.com/justinstimatze/be-my-geminis/commit/441e0f0) |
| O6 | SECURITY.md missing | **FIXED** [`441e0f0`](https://github.com/justinstimatze/be-my-geminis/commit/441e0f0) |
| O7 | CONTRIBUTING.md missing | **FIXED** [`441e0f0`](https://github.com/justinstimatze/be-my-geminis/commit/441e0f0) — includes the load-bearing institutional knowledge from memory (CC reads mcpServers from `~/.claude.json` exclusively; `mcp_tool` hook can't return `updatedInput.file_path`) |
| O8 | CODE_OF_CONDUCT.md missing | **FIXED** [`441e0f0`](https://github.com/justinstimatze/be-my-geminis/commit/441e0f0) — Contributor Covenant 2.1 |
| O9 | Issue / PR templates missing | **FIXED** [`441e0f0`](https://github.com/justinstimatze/be-my-geminis/commit/441e0f0) — `bug_report.yml`, `feature_request.yml`, `PULL_REQUEST_TEMPLATE.md` |
| O10 | Branch protection on main is OFF | **OPEN** — user action via `gh api ... protection` before public flip |
| O11 | CI single Go version + single OS | **OPEN (this session)** — adding macos-latest job in the in-progress Tier 3 work |
| O12 | No goreleaser / prebuilt binary releases | **DEFERRED** — `go install` covers the Go-savvy audience; binary release pipeline can ship post-v0.1.0 without breaking anyone |

### Future Maintainer + API Stability panel — closed

| # | Finding | Status |
|---|---|---|
| F1 | `usage()` advertises `--enable-snap` + `--enable-deny-inline` flags that don't exist | **FIXED** [`0caf8fd`](https://github.com/justinstimatze/be-my-geminis/commit/0caf8fd) |
| F2 | MCP `inputSchema` advertises `model` but README/spec describe `max_dim` | **PARTIAL** — `max_dim` is not in code, README, or spec (panel overclaimed; verified by grep). `model` arg now documented in README example [`fdc3b41`](https://github.com/justinstimatze/be-my-geminis/commit/fdc3b41) |
| F3 | Schema "version" string in `cmd_pre_read.go` is decorative — no real versioning | **FIXED** (cache surface) [`893cdcb`](https://github.com/justinstimatze/be-my-geminis/commit/893cdcb) — `cacheSchemaVersion="v1"` embedded in cache filenames so request-shape changes invalidate stale entries. Trust-fence string version still decorative; treated as documentation of the schema-as-of-build rather than a contract |
| F4 | No tests for hook stdout shape, MCP tool definition, or vision profile schemas | **PARTIAL** [`893cdcb`](https://github.com/justinstimatze/be-my-geminis/commit/893cdcb) + [`1f1e9a6`](https://github.com/justinstimatze/be-my-geminis/commit/1f1e9a6) — vision `buildRequest` shape now covered by 11 tests; convert pipeline + bounds covered by 5; fence sanitizer covered by 5; atomic-write helper covered by 4. Hook + MCP wire shape addressed in this Tier-3 batch |
| F5 | No CHANGELOG / CONTRIBUTING / schema-evolution policy | **FIXED** [`441e0f0`](https://github.com/justinstimatze/be-my-geminis/commit/441e0f0) — CONTRIBUTING.md documents the cache-schema-bump policy and the two stable contracts (fenced-markdown shape + MCP arg names) |
| F6 | Model name constants hardcoded; no runtime detection | **FIXED** [`893cdcb`](https://github.com/justinstimatze/be-my-geminis/commit/893cdcb) — `vision.DefaultThinkingBudgetFor(model)` picks budget by substring (flash → 0, pro/unknown → 128); robust across `gemini-pro-latest`, `gemini-3.x-pro`, future variants |
| F7 | genai SDK pinned with no documented update cadence | **PARTIAL** — `CHANGELOG.md` records the v1.55.0 pin; CONTRIBUTING.md notes the cache-schema-bump policy ties to SDK-shape changes. Formal update cadence deferred |
| F8 | CC hook fields documented as comments, not contract | **FIXED** [`441e0f0`](https://github.com/justinstimatze/be-my-geminis/commit/441e0f0) — CONTRIBUTING.md "Touching the wire protocol" section names the two stable contracts (`<bmg-vision-report>` / `<bmg-ocr-text>` fenced-markdown shape, MCP tool args) |
| F9 | `cmd/bmg/cmd_init.go` at 549 lines should split | **OPEN (this session)** — Tier 3 in-progress refactor |
| F10 | Vestigial `var _ = ...` lines in `cmd_cache.go:103` and `cmd_mcp.go:367` | **OPEN (this session)** — Tier 3 cleanup |

## Withdrawn / WONTFIX

| Origin | Finding | Why withdrawn |
|---|---|---|
| O1 | `go 1.26` audience-narrowing | User: ~3 months adoption time is sufficient |
| F2 (part) | "MCP advertises `max_dim` as a tool arg" | Verified by grep: `max_dim` is not in code, README, or `bootstrap.md` — agent overclaim |
| H10 | "Tested with CC <version>" matrix | Pre-1.0 single-version testing is the stated posture; adding a matrix is post-v0.1.0 |
| H12 | Be-My-Eyes naming gives nothing back | **FIXED** [`fdc3b41`](https://github.com/justinstimatze/be-my-geminis/commit/fdc3b41) — README footer now includes a donation pointer |
| H13 | `docs/PRIOR_ART.md` referenced but missing | The reference was in `bootstrap.md`; harmless dangling note. Add prior-art doc post-v0.1.0 |
| Various | Multiple findings about Phase 2 / Phase 5 / phase tracker | Phase markers are historical scaffolding from `bootstrap.md` design narrative; not load-bearing for users. `bmg --help` text scrubbed of phase forward-references in [`0caf8fd`](https://github.com/justinstimatze/be-my-geminis/commit/0caf8fd) |

## Open items remaining before v0.1.0 tag

| # | Item | Action owner |
|---|---|---|
| O2 | `go install ...@latest` 404 resolves on public-flip | resolved by user action: `gh repo edit ... --visibility public` + `git tag v0.1.0` |
| O10 | Branch protection on main | user action: `gh api repos/justinstimatze/be-my-geminis/branches/main/protection` |
| O11 | Cross-OS CI | bmg agent (this session) |
| F9 | `cmd_init.go` split | bmg agent (this session) |
| F10 | Vestigial `var _ = ...` lines | bmg agent (this session) |
| F4 | Hook + MCP wire-shape tests | bmg agent (this session) |

## Deferred to post-v0.1.0 (intentional)

- **CV / multimodal:** external benchmark slice (ChartQA / DocVQA ~50
  examples), 2-3 real-world screenshots in `benchmark/`, JPEG quality
  bump to 85 for the deliberate path, heatmap-specific profile schema
- **Future Maintainer:** formal genai SDK update cadence policy,
  schema evolution policy beyond the cache-version bump
- **HN:** worked-example cost estimate based on measured weekly usage;
  CC version compatibility matrix
- **OSS:** goreleaser-based prebuilt binary releases; release-signing
  via sigstore / GitHub provenance attestation
- **Security:** full POSIX advisory lock on `~/.claude.json` (current
  mitigation is documented TIP — CC itself doesn't use a lock)
- **Project:** prior-art document (`docs/PRIOR_ART.md`) with the
  justi gist link and any other relevant projects

## Source files

- [`batch1_security.md`](batch1_security.md) — Security Reviewer (13 substantive findings: 7 block-release, 5 nice-to-have, 6 info)
- [`batch1_skeptical_hn.md`](batch1_skeptical_hn.md) — Skeptical HN User (20 findings: 8 block-release, 6 nice-to-have, 6 info)
- [`batch1_cv_expert.md`](batch1_cv_expert.md) — CV / Multimodal LLM Expert (~20 findings: 5 block-release, 8 nice-to-have, 6 info)
- [`batch2_oss_maintainer.md`](batch2_oss_maintainer.md) — OSS Maintainer (~20 findings: 11 block-release, 6 nice-to-have, 3 info)
- [`batch2_future_maintainer.md`](batch2_future_maintainer.md) — Future Maintainer + API Stability (~17 findings: 5 block-release, ~8 nice-to-have, 4 info)
