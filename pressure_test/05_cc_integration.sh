#!/usr/bin/env bash
# Pressure test #5 — CC integration smoke.
#
# Three checks that the install is wired end-to-end into a real Claude
# Code session. Some of these are observed externally (we read
# config files + cache state); the SessionStart fence appearing in
# Claude's context is something only the running CC client can
# observe directly. We approximate that check by running the
# session-start hook ourselves and confirming it emits the routing
# instructions.
set -uo pipefail

BMG="${BMG:-bmg}"
HERE="$(cd "$(dirname "$0")" && pwd)"
RESULTS="$HERE/results/05_cc_integration.txt"
mkdir -p "$HERE/results"
: > "$RESULTS"

PASS=0
FAIL=0
log() { echo "$*" | tee -a "$RESULTS"; }

log "=== Pressure test 5: CC integration smoke ==="
log ""

# (a) SessionStart hook emits routing-instructions context.
log "(a) SessionStart hook emits routing instructions"
out=$(echo '{"session_id":"pt","transcript_path":"","cwd":"","hook_event_name":"SessionStart"}' \
       | "$BMG" hook session-start 2>/dev/null)
if grep -q "bmg-routing\|bmg-vision-report\|bmg-ocr-text" <<<"$out"; then
  log "  PASS  routing fence present in SessionStart additionalContext"
  PASS=$((PASS+1))
else
  log "  FAIL  routing fence missing; got: $(echo "$out" | head -c 200)"
  FAIL=$((FAIL+1))
fi

# (b) Doctor reports all 5 axes green.
log "(b) bmg doctor — all 5 axes green"
doctor_out=$("$BMG" doctor 2>&1 || true)
echo "$doctor_out" | sed 's/^/      /' >> "$RESULTS"
green_count=$(grep -c "✓" <<<"$doctor_out" || true)
all_ok="All checks passed"
if grep -q "$all_ok" <<<"$doctor_out" && [[ "$green_count" -ge 5 ]]; then
  log "  PASS  doctor: $green_count ✓ axes + final 'All checks passed'"
  PASS=$((PASS+1))
else
  log "  FAIL  doctor: $green_count ✓ axes; missing 'All checks passed'"
  FAIL=$((FAIL+1))
fi

# (c) Hook surface registered in user settings.json
log "(c) hook registered in ~/.claude/settings.json (or project)"
if grep -q "$BMG hook pre-read\|bmg hook pre-read" ~/.claude/settings.json 2>/dev/null \
   || grep -q "bmg hook pre-read" "$HERE/../.claude/settings.json" 2>/dev/null; then
  log "  PASS  PreToolUse:Read hook entry found"
  PASS=$((PASS+1))
else
  log "  FAIL  PreToolUse:Read hook entry missing from settings.json"
  FAIL=$((FAIL+1))
fi

# (d) MCP server registered in ~/.claude.json (the actual location CC reads from).
log "(d) MCP server registered in ~/.claude.json"
if python3 -c "
import json, sys
try:
    d = json.load(open('$HOME/.claude.json'))
    if 'bemygeminis' in d.get('mcpServers', {}):
        sys.exit(0)
except Exception:
    pass
sys.exit(1)
"; then
  log "  PASS  mcpServers.bemygeminis present in ~/.claude.json"
  PASS=$((PASS+1))
else
  log "  FAIL  mcpServers.bemygeminis missing — CC won't load the MCP tool"
  FAIL=$((FAIL+1))
fi

# (e) CC actually spawned bmg mcp at some point — look for log directory.
log "(e) CC has spawned bmg mcp (evidence: ~/.cache/claude-cli-nodejs/.../mcp-logs-bemygeminis/)"
cache_root="$HOME/.cache/claude-cli-nodejs"
found=$(find "$cache_root" -maxdepth 3 -name "mcp-logs-bemygeminis" -type d 2>/dev/null | head -1)
if [[ -n "$found" ]]; then
  log "  PASS  CC mcp-logs dir exists: $found"
  PASS=$((PASS+1))
else
  log "  FAIL  no mcp-logs-bemygeminis dir; CC has not yet spawned the MCP server (try restarting CC after bmg init --enable-mcp)"
  FAIL=$((FAIL+1))
fi

log ""
log "=== summary: $PASS pass, $FAIL fail ==="
[[ "$FAIL" -eq 0 ]] && exit 0 || exit 1
