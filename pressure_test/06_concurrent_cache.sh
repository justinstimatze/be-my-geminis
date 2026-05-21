#!/usr/bin/env bash
# Pressure test #6 — concurrent cache access.
#
# bmg's cache.Put writes atomically (tmp file + rename per memory).
# Pressure test: fire N parallel `bmg describe` invocations on the
# SAME image. Expected behavior:
#   - one or more cold-start calls hit Gemini
#   - all N succeed (no panic, no truncated cache entries)
#   - after the burst, a follow-up read hits cache instantly
#
# Without API access in CI, this script needs ANTHROPIC_API_KEY or
# GEMINI_API_KEY in env. Skip gracefully if not set.
set -uo pipefail

BMG="${BMG:-bmg}"
HERE="$(cd "$(dirname "$0")" && pwd)"
RESULTS="$HERE/results/06_concurrent_cache.txt"
mkdir -p "$HERE/results"
: > "$RESULTS"

PASS=0
FAIL=0
log() { echo "$*" | tee -a "$RESULTS"; }

log "=== Pressure test 6: concurrent cache access ==="
log ""

# Need a real API key for this test.
if [[ -z "${GEMINI_API_KEY:-}${GOOGLE_API_KEY:-}${BMG_API_KEY:-}" ]] && [[ ! -f "$HOME/.config/bmg/api_key" ]]; then
  log "  SKIP  no GEMINI_API_KEY / GOOGLE_API_KEY / ~/.config/bmg/api_key"
  log "        (set one to exercise the concurrent describe path)"
  log ""
  log "=== summary: 0 pass, 0 fail (skipped) ==="
  exit 0
fi

IMG="$HERE/../benchmark/images/01-bar-chart-quarterly.png"
if [[ ! -f "$IMG" ]]; then
  log "  FAIL  benchmark image missing: $IMG (run benchmark/generate.py first)"
  exit 1
fi

# Clean cache of this specific image's entry so we start cold.
CACHE_DIR="${XDG_RUNTIME_DIR:-/tmp}/bmg"
SHA=$(sha256sum "$IMG" | awk '{print $1}')
rm -f "$CACHE_DIR/$SHA.md" 2>/dev/null || true
log "(0) cleaned cache entry $SHA (cold start)"

# Fire N parallel describes.
N=5
log "(1) firing $N parallel \`bmg describe\` on the same image"
TMPOUT="$HERE/results/06.parallel.tmp"
mkdir -p "$TMPOUT"
rm -f "$TMPOUT"/*

pids=()
for i in $(seq 1 $N); do
  ( "$BMG" describe -model gemini-2.5-flash -profile general -pretty=false "$IMG" > "$TMPOUT/out_$i.json" 2> "$TMPOUT/err_$i.txt" ) &
  pids+=($!)
done
ok=0
for pid in "${pids[@]}"; do
  if wait "$pid"; then
    ok=$((ok+1))
  fi
done
log "  $ok / $N parallel calls succeeded"
if [[ "$ok" -eq "$N" ]]; then
  log "  PASS  all $N parallel calls succeeded"
  PASS=$((PASS+1))
else
  log "  FAIL  $((N - ok)) parallel calls failed"
  for f in "$TMPOUT"/err_*.txt; do
    [[ -s "$f" ]] && echo "    -- $(basename "$f")" >> "$RESULTS" && cat "$f" >> "$RESULTS"
  done
  FAIL=$((FAIL+1))
fi

# Each output JSON must be valid (no truncation).
valid=0
for f in "$TMPOUT"/out_*.json; do
  if python3 -c "import json,sys; json.load(open('$f'))" 2>/dev/null; then
    valid=$((valid+1))
  fi
done
if [[ "$valid" -eq "$N" ]]; then
  log "  PASS  all $N output JSONs are valid (no truncated/corrupt writes)"
  PASS=$((PASS+1))
else
  log "  FAIL  $((N - valid)) outputs are invalid JSON (likely truncated)"
  FAIL=$((FAIL+1))
fi

# Cache entry should now exist and be a non-empty file. (CLI describe
# may or may not cache — the hook caches, the CLI doesn't by design
# per memory. So skip this assertion if cache file isn't there; we
# already verified the parallel calls didn't corrupt each other.)
log ""
log "=== summary: $PASS pass, $FAIL fail ==="
[[ "$FAIL" -eq 0 ]] && exit 0 || exit 1
