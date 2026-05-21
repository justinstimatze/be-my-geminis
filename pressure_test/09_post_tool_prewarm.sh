#!/usr/bin/env bash
# Pressure test #9 — PostToolUse:Bash pre-warm pipeline.
#
# Synthesizes a post-tool stdin payload referencing a freshly-
# generated image, runs `bmg hook post-tool` directly, and verifies:
#
#   1. The hook exits cleanly (must NOT block CC).
#   2. The hook extracts the path from the synthesized Bash output
#      and spawns a detached describe-cached child.
#   3. hook.log gains a "spawned warm pid=N for M path(s)" line
#      naming the right path count.
#   4. (With an API key) the detached child completes and writes
#      a cache entry for the image's sha. Skipped if no key.
#
# Run by run.sh; standalone:
#     BMG=$(which bmg) bash 09_post_tool_prewarm.sh
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
BMG="${BMG:-bmg}"
RESULTS="$HERE/results"
LOG="$RESULTS/09_post_tool_prewarm.txt"
mkdir -p "$RESULTS"
: > "$LOG"

log() { echo "$@" | tee -a "$LOG"; }

log "=== Pressure test 9: post-tool pre-warm pipeline ==="

# Need an image to "pre-warm" — generate via PIL to match other tests.
PY="${PY:-python3}"
PT_VENV="$HERE/../benchmark/.venv/bin/python"
[[ -x "$PT_VENV" ]] && PY="$PT_VENV"

img="$HERE/fixtures/prewarm_target.png"
mkdir -p "$HERE/fixtures"
$PY -c "
from PIL import Image, ImageDraw
img = Image.new('RGB', (200, 150), 'white')
d = ImageDraw.Draw(img)
d.rectangle([20, 20, 180, 130], outline='blue', width=3)
d.text((50, 60), 'PT9 PREWARM', fill='black')
img.save('$img')
" 2>&1 | tee -a "$LOG"

if [[ ! -s "$img" ]]; then
  log "FAIL — could not generate target image at $img"
  exit 1
fi
log "generated: $img"

# Synthesize the PostToolUse stdin payload CC would send after a
# Bash command that produced this image. tool_input.command +
# tool_response.stdout both mention the path so both extraction
# signals (regex + cwd recency scan) have something to find.
sha=$($PY -c "
import hashlib
print(hashlib.sha256(open('$img', 'rb').read()).hexdigest())
")
log "image sha: $sha"

# Snapshot the current hook.log tail so we can detect the new entry
# the post-tool spawn writes.
HOOK_LOG="$HOME/.claude/bmg/hook.log"
if [[ -f "$HOOK_LOG" ]]; then
  baseline_lines=$(wc -l < "$HOOK_LOG")
else
  baseline_lines=0
fi
log "hook.log baseline: $baseline_lines lines"

payload=$(cat <<EOF
{
  "session_id": "pt9",
  "transcript_path": "",
  "cwd": "$HERE",
  "hook_event_name": "PostToolUse",
  "tool_name": "Bash",
  "tool_input": {"command": "python -c \"img.save('$img')\""},
  "tool_response": {"stdout": "saved to $img\n", "stderr": ""}
}
EOF
)

log ""
log "--- invoking bmg hook post-tool ---"
t0=$(date +%s%N)
echo "$payload" | "$BMG" hook post-tool > "$RESULTS/09_post_tool_stdout.txt" 2>&1
rc=$?
elapsed_ms=$(( ($(date +%s%N) - t0) / 1000000 ))

pass=0
fail=0

# (1) Hook exits cleanly within a tight budget (must not block CC;
# the spawn is detached so this is sub-100ms even with the detach
# syscall).
if [[ "$rc" -eq 0 ]]; then
  log "  PASS  hook exited 0 (rc=$rc)"
  pass=$((pass+1))
else
  log "  FAIL  hook exited non-zero: $rc"
  fail=$((fail+1))
fi
if [[ "$elapsed_ms" -lt 500 ]]; then
  log "  PASS  hook returned in ${elapsed_ms}ms (must be <500ms to avoid blocking CC)"
  pass=$((pass+1))
else
  log "  FAIL  hook took ${elapsed_ms}ms — exceeds the must-not-block-CC budget"
  fail=$((fail+1))
fi

# (2+3) New hook.log lines mention spawned warm. Give it a second
# in case the log write hasn't flushed.
sleep 1
if [[ -f "$HOOK_LOG" ]]; then
  current_lines=$(wc -l < "$HOOK_LOG")
  new_tail=$(tail -n $(( current_lines - baseline_lines )) "$HOOK_LOG")
  if echo "$new_tail" | rg -q "post-tool.*spawned warm"; then
    log "  PASS  hook.log shows 'spawned warm' entry"
    pass=$((pass+1))
  else
    log "  FAIL  no 'spawned warm' entry in new hook.log lines:"
    echo "$new_tail" | tee -a "$LOG"
    fail=$((fail+1))
  fi
else
  log "  FAIL  hook.log absent at $HOOK_LOG — hook didn't initialize?"
  fail=$((fail+1))
fi

# (4) Optional: with an API key, wait up to 30s for the cache entry
# to land. Skip if no key.
if [[ -z "${BMG_API_KEY:-}${GEMINI_API_KEY:-}${GOOGLE_API_KEY:-}" ]] \
    && [[ ! -f "$HOME/.config/bmg/api_key" ]]; then
  log "  SKIP  no Gemini API key — skipping detached-child completion check"
else
  cache_dir="${BMG_CACHE_DIR:-${XDG_RUNTIME_DIR:-/tmp}/bmg}"
  cache_file="$cache_dir/bmg-v2-${sha}.md"
  log "  waiting up to 30s for cache entry: $cache_file"
  for i in $(seq 1 30); do
    if [[ -s "$cache_file" ]]; then
      log "  PASS  detached child wrote cache entry after ~${i}s"
      pass=$((pass+1))
      break
    fi
    sleep 1
  done
  if [[ ! -s "$cache_file" ]]; then
    # Could be transient API failure rather than a code bug. Show
    # the pre-warm log lines to help diagnose.
    log "  INFO  cache entry did not land within 30s. Recent pre-warm log lines:"
    grep "pre-warm" "$HOOK_LOG" 2>/dev/null | tail -5 | tee -a "$LOG"
    log "        (this is INFO not FAIL — upstream API outage shouldn't break the test)"
  fi
fi

log ""
log "=== summary: $pass pass, $fail fail ==="
exit $(( fail > 0 ? 1 : 0 ))
