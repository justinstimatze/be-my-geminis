#!/usr/bin/env bash
# Pressure test #2 — install / uninstall integration round-trip.
#
# Unit tests cover mergeClaudeSettings + mergeClaudeJSON at the
# function level. This script exercises the full CLI surface against
# fixture settings files containing rich foreign content:
#   - foreign hooks under multiple events
#   - foreign mcpServers (buddy, plancheck)
#   - top-level Claude-managed keys (projects, firstStartTime, ...)
#
# After `bmg init --project --enable-mcp` + `bmg uninstall --project`,
# everything foreign must be byte-equivalent (modulo JSON key
# ordering) to the pre-install state. bmg leaves no residue.
set -uo pipefail

BMG="${BMG:-bmg}"
HERE="$(cd "$(dirname "$0")" && pwd)"
RESULTS="$HERE/results/02_install_roundtrip.txt"
mkdir -p "$HERE/results"
: > "$RESULTS"

PASS=0
FAIL=0
log() { echo "$*" | tee -a "$RESULTS"; }

# Spin up an isolated HOME under a temp dir.
TMP="$(mktemp -d -t bmg-pressure.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/.claude"

# Fixture: realistic-shape settings.json with foreign hooks.
cat > "$TMP/.claude/settings.json" <<'JSON'
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [
          { "type": "command", "command": "/home/u/go/bin/hindcast inject" }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          { "type": "command", "command": "/home/u/.claude/hooks/defn-sync.sh" }
        ]
      }
    ]
  }
}
JSON

# Fixture: realistic-shape ~/.claude.json with foreign mcpServers +
# claude-managed top-level keys.
cat > "$TMP/.claude.json" <<'JSON'
{
  "mcpServers": {
    "buddy":     { "type": "stdio", "command": "node",          "args": ["/home/u/.buddy/server.js"], "env": {} },
    "plancheck": { "type": "stdio", "command": "/opt/plancheck", "args": ["mcp"], "env": { "PLANCHECK_SHARE": "full" } }
  },
  "firstStartTime": "2026-01-01T00:00:00Z",
  "hasCompletedOnboarding": true,
  "projects": {
    "/home/u/Documents/foo": {
      "lastSessionFirstPrompt": "do the thing",
      "projectOnboardingSeenCount": 3
    }
  }
}
JSON

# Canonicalize for diffing — Python json sort_keys + indent normalizes whitespace.
canonicalize() {
  python3 -c "
import json, sys
d = json.load(open(sys.argv[1]))
print(json.dumps(d, indent=2, sort_keys=True))
" "$1"
}

PRE_SETTINGS=$(canonicalize "$TMP/.claude/settings.json")
PRE_CLAUDEJSON=$(canonicalize "$TMP/.claude.json")

log "=== Pressure test 2: install/uninstall round-trip ==="
log "tmp HOME: $TMP"
log ""

# Run bmg init --project --enable-mcp; HOME points at $TMP so
# ~/.claude.json resolves under the temp dir; --project uses cwd
# which we set to $TMP for symmetry.
log "(1) bmg init --project --enable-mcp"
( cd "$TMP" && HOME="$TMP" "$BMG" init --project --enable-mcp 2>&1 | sed 's/^/      /' ) | tee -a "$RESULTS"

# Verify: hooks added, foreign hooks preserved.
post_settings=$(canonicalize "$TMP/.claude/settings.json")
post_claudejson=$(canonicalize "$TMP/.claude.json")

if grep -q '"command": "/home/u/go/bin/hindcast inject"' <<<"$post_settings"; then
  log "  PASS  foreign SessionStart hook (hindcast) preserved"
  PASS=$((PASS+1))
else
  log "  FAIL  hindcast hook lost"
  FAIL=$((FAIL+1))
fi
if grep -q '"command": "/home/u/.claude/hooks/defn-sync.sh"' <<<"$post_settings"; then
  log "  PASS  foreign PostToolUse hook (defn-sync) preserved"
  PASS=$((PASS+1))
else
  log "  FAIL  defn-sync hook lost"
  FAIL=$((FAIL+1))
fi
if grep -q 'hook pre-read' <<<"$post_settings"; then
  log "  PASS  bmg PreToolUse:Read hook registered"
  PASS=$((PASS+1))
else
  log "  FAIL  bmg hook not registered"
  FAIL=$((FAIL+1))
fi
if grep -q '"bemygeminis"' <<<"$post_claudejson"; then
  log "  PASS  bemygeminis MCP entry written to ~/.claude.json"
  PASS=$((PASS+1))
else
  log "  FAIL  bemygeminis MCP missing"
  FAIL=$((FAIL+1))
fi
if grep -q '"buddy"' <<<"$post_claudejson" && grep -q '"plancheck"' <<<"$post_claudejson"; then
  log "  PASS  foreign MCP servers (buddy, plancheck) preserved"
  PASS=$((PASS+1))
else
  log "  FAIL  foreign MCP servers lost"
  FAIL=$((FAIL+1))
fi
if grep -q '"firstStartTime"' <<<"$post_claudejson" && grep -q '"projects"' <<<"$post_claudejson"; then
  log "  PASS  Claude-managed top-level keys (projects, firstStartTime) preserved"
  PASS=$((PASS+1))
else
  log "  FAIL  Claude-managed keys lost"
  FAIL=$((FAIL+1))
fi

# Idempotent: second init reports no changes.
log ""
log "(2) bmg init --project --enable-mcp (idempotency check)"
second_out=$( cd "$TMP" && HOME="$TMP" "$BMG" init --project --enable-mcp 2>&1 )
if grep -q "no changes" <<<"$second_out"; then
  log "  PASS  second init is a no-op"
  PASS=$((PASS+1))
else
  log "  FAIL  second init reported changes (not idempotent)"
  echo "$second_out" | sed 's/^/      /' | tee -a "$RESULTS"
  FAIL=$((FAIL+1))
fi

# Now uninstall + round-trip diff.
log ""
log "(3) bmg uninstall --project"
( cd "$TMP" && HOME="$TMP" "$BMG" uninstall --project 2>&1 | sed 's/^/      /' ) | tee -a "$RESULTS"

post_uninstall_settings=$(canonicalize "$TMP/.claude/settings.json" 2>/dev/null || echo "{}")
post_uninstall_claudejson=$(canonicalize "$TMP/.claude.json" 2>/dev/null || echo "{}")

# settings.json should now match pre state.
if [[ "$post_uninstall_settings" == "$PRE_SETTINGS" ]]; then
  log "  PASS  settings.json round-trip identical to pre-install"
  PASS=$((PASS+1))
else
  log "  FAIL  settings.json drift after round-trip"
  diff <(echo "$PRE_SETTINGS") <(echo "$post_uninstall_settings") | head -20 | sed 's/^/      /' | tee -a "$RESULTS"
  FAIL=$((FAIL+1))
fi

# ~/.claude.json should match pre state.
if [[ "$post_uninstall_claudejson" == "$PRE_CLAUDEJSON" ]]; then
  log "  PASS  ~/.claude.json round-trip identical to pre-install"
  PASS=$((PASS+1))
else
  log "  FAIL  ~/.claude.json drift after round-trip"
  diff <(echo "$PRE_CLAUDEJSON") <(echo "$post_uninstall_claudejson") | head -20 | sed 's/^/      /' | tee -a "$RESULTS"
  FAIL=$((FAIL+1))
fi

log ""
log "=== summary: $PASS pass, $FAIL fail ==="
[[ "$FAIL" -eq 0 ]] && exit 0 || exit 1
