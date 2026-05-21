#!/usr/bin/env bash
# Pressure test #3 — MCP stdio server robustness.
#
# The bmg MCP server is hand-rolled JSON-RPC 2.0 over stdio. We send
# various malformed and adversarial inputs and verify:
#   1. The server does NOT crash (subsequent valid requests still work)
#   2. Malformed input gets a proper JSON-RPC error response or is
#      silently ignored (per spec — invalid JSON has no id to reply to)
#   3. Schema-violating tools/call gets isError:true, not a panic
#
# We send a sequence of requests in one stdin stream and check that
# the server processes the valid ones even after seeing garbage.
set -uo pipefail

BMG="${BMG:-bmg}"
HERE="$(cd "$(dirname "$0")" && pwd)"
RESULTS="$HERE/results/03_mcp_robustness.txt"
mkdir -p "$HERE/results"
: > "$RESULTS"

PASS=0
FAIL=0
log() { echo "$*" | tee -a "$RESULTS"; }

# Mixed batch: garbage + valid initialize + valid tools/list + invalid
# tools/call. The server should answer the valid ones without crashing.
batch=$(cat <<'JSONRPC'
{not valid json at all
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"pt","version":"0"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"bmg_describe","arguments":{}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"bmg_describe","arguments":{"path":"relative/path.png"}}}
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"bmg_describe","arguments":{"path":"/tmp/nonexistent-bmg-image.png"}}}
{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"bmg_unknown_tool","arguments":{}}}
JSONRPC
)

log "=== Pressure test 3: MCP server robustness ==="
log ""

out=$(echo "$batch" | timeout 10 "$BMG" mcp 2>/dev/null || true)
echo "$out" | sed 's/^/      /' >> "$RESULTS"

# Helper: read response object with given id from the line-delimited
# JSON stream.
response_for() {
  local id="$1"
  echo "$out" | python3 -c "
import json, sys
for line in sys.stdin:
    line = line.strip()
    if not line: continue
    try:
        d = json.loads(line)
    except json.JSONDecodeError:
        continue
    if d.get('id') == $id:
        print(json.dumps(d))
        break
"
}

# (a) initialize succeeded
r=$(response_for 1)
if [[ -n "$r" ]] && grep -q '"protocolVersion"' <<<"$r"; then
  log "  PASS  initialize succeeded despite garbage on prior line"
  PASS=$((PASS+1))
else
  log "  FAIL  initialize failed; response=$r"
  FAIL=$((FAIL+1))
fi

# (b) tools/list returned the bmg_describe tool
r=$(response_for 2)
if [[ -n "$r" ]] && grep -q 'bmg_describe' <<<"$r"; then
  log "  PASS  tools/list returned bmg_describe"
  PASS=$((PASS+1))
else
  log "  FAIL  tools/list missing bmg_describe; response=$r"
  FAIL=$((FAIL+1))
fi

# (c) tools/call missing required path → error response, not crash
r=$(response_for 3)
if [[ -n "$r" ]] && (grep -q 'isError' <<<"$r" || grep -q '"error"' <<<"$r"); then
  log "  PASS  missing path → error response (server still alive)"
  PASS=$((PASS+1))
else
  log "  FAIL  missing path: server crashed or accepted invalid args; response=$r"
  FAIL=$((FAIL+1))
fi

# (d) tools/call relative path → error (we enforce absolute paths)
r=$(response_for 4)
if [[ -n "$r" ]] && (grep -q 'isError' <<<"$r" || grep -q '"error"' <<<"$r" || grep -q -i 'absolute' <<<"$r"); then
  log "  PASS  relative path rejected"
  PASS=$((PASS+1))
else
  log "  FAIL  relative path not rejected; response=$r"
  FAIL=$((FAIL+1))
fi

# (e) tools/call nonexistent file → error
r=$(response_for 5)
if [[ -n "$r" ]] && (grep -q 'isError' <<<"$r" || grep -q '"error"' <<<"$r" || grep -q -i 'not.*found\|no such' <<<"$r"); then
  log "  PASS  nonexistent file rejected"
  PASS=$((PASS+1))
else
  log "  FAIL  nonexistent file not rejected; response=$r"
  FAIL=$((FAIL+1))
fi

# (f) unknown tool name → error
r=$(response_for 6)
if [[ -n "$r" ]] && (grep -q '"error"' <<<"$r" || grep -q -i 'unknown\|not found' <<<"$r"); then
  log "  PASS  unknown tool name rejected"
  PASS=$((PASS+1))
else
  log "  FAIL  unknown tool not rejected; response=$r"
  FAIL=$((FAIL+1))
fi

log ""
log "=== summary: $PASS pass, $FAIL fail ==="
[[ "$FAIL" -eq 0 ]] && exit 0 || exit 1
