# Contributing to bmg

bmg is a small Go project with a tight contract surface (a Claude Code
hook + an MCP server). Most contributions touch one of three areas:
profiles, the wire protocol, or the install path. This guide is
deliberately specific about each.

## Local development

```bash
# clone + install
git clone https://github.com/justinstimatze/be-my-geminis
cd be-my-geminis
go install ./cmd/bmg

# verify your install works end-to-end
bmg doctor

# run the full test suite (must be green before sending a PR)
go test -count=1 -race ./...

# format + vet (CI fails on either)
gofmt -l . && go vet ./...
```

CI runs the same four commands. There are no other gates.

## Adding a new profile

Profiles live in `internal/vision/profile_*.go`. A profile is a
`Profile` struct with four required fields:

```go
Profile{
    Name:              "your-name",        // matches the user-facing -profile flag
    Prompt:            "...",              // the user-message text Gemini sees
    SystemInstruction: "...",              // role priming; survives schema pressure
    Schema:            &genai.Schema{...}, // structured-output contract
}
```

Register it in `profiles.go` via `init()`. The schema's `Required` and
`PropertyOrdering` fields meaningfully affect Gemini's structured-
output behavior — set them. Look at `profile_chart.go` or
`profile_diagram.go` for the patterns that work.

Add a benchmark case in `benchmark/cases.json` so regressions are
catchable. Generate a synthetic test image in `benchmark/generate.py`
if one doesn't exist that exercises the profile.

## Touching the wire protocol

Two contracts that downstream agents depend on:

**Hook output format** — the `<bmg-vision-report>` /
`<bmg-ocr-text>` fenced markdown structure in
`cmd/bmg/cmd_pre_read.go:renderReport`. Claude is primed via the
SessionStart hook to treat the OCR fence as UNTRUSTED. Changes here
need a corresponding update to the SessionStart prime AND a bump of
`cacheSchemaVersion` in `internal/cache/cache.go` so cached entries
from older renderers don't get served as new.

**MCP tool surface** — `mcp__bemygeminis__bmg_describe` in
`cmd/bmg/cmd_mcp.go`. The argument shape (`file_path`, `profile`,
`intent`, `region`, `model`) is the public API. Adding optional
arguments is fine; renaming or removing existing ones is breaking
and needs a major-version bump (once we have one).

**One thing not to do:** the `mcp_tool` PreToolUse hook event exists
in Claude Code but cannot return an `updatedInput.file_path` redirect
(verified live in CC 2.1.126). Don't waste time trying to redirect
MCP tool inputs that way; the working pattern is the shell-wrapper
already in place.

## Touching the install path

`bmg init` writes to two files Claude Code reads:

- `~/.claude/settings.json` (or `<project>/.claude/settings.json` with
  `--project`): hooks live here.
- `~/.claude.json`: `mcpServers` registrations live here exclusively.
  CC does NOT read `mcpServers` from `settings.json` — verified live
  in CC 2.1.126. Code in `cmd_init.go:stripStaleMCPFromSettings`
  cleans up the historical broken case where bmg wrote MCP entries
  to the wrong file.

Both writes go through `atomicWriteFile` (temp + rename). Don't add a
new write site without lifting the same pattern; an interrupted
naive `os.WriteFile` can brick a user's CC install.

## Running integration tests against your install

```bash
# After a code change, reinstall and re-run the pressure suite.
go install ./cmd/bmg
./pressure_test/run.sh
```

`pressure_test/` covers nine axes: hook-handler failures, install
round-trip, MCP robustness, prompt-injection resistance (including
fence-integrity), CC-version integration, concurrent cache writes,
image-format coverage, video describe round-trip, and PostToolUse
prewarm. The unit-test suite (`go test ./...`) doesn't cover these
wire-protocol surfaces.

The `benchmark/` directory is for quality regression, not
correctness. `./benchmark/run.sh` calls real Gemini and Anthropic APIs
and costs cents per run. Don't add it to CI.

## Style

- Follow existing patterns. `cmd/bmg/cmd_*.go` is one file per
  subcommand; `internal/vision/profile_*.go` is one file per profile.
  Keep these symmetric.
- Comments explain *why*, not *what*. The mechanical what is in the
  diff; the why is what survives years of refactoring.
- `gofmt -l` must be clean. Tabs not spaces. The CI config is in
  `.github/workflows/ci.yml`.
- Tests live next to code they test (`*_test.go` in the same
  package). For pure helpers, table-driven tests are preferred.

## Reporting issues

Open a GitHub Issue with the templates provided. For security
findings, see `SECURITY.md` — these go to the GitHub Security
Advisory pipeline, not the public issue tracker.
