#!/usr/bin/env bash
# Pressure test #1 — hook failure modes.
#
# For each adversarial input the PreToolUse:Read hook might see in
# real CC use, we want graceful behavior. Specifically:
#   - missing file        → exit 0, passthrough (no JSON output)
#   - zero-byte file      → exit 0 with deny OR passthrough — never panic
#   - non-image (.png ext, text bytes) → graceful deny via Gemini error
#   - BMG_DISABLE=1       → exit 0, passthrough (no Gemini call)
#   - BMG_HOOK_PROXY=1    → exit 0, passthrough (re-entry guard)
#   - no API key          → exit 0, passthrough (fail-open per design)
#
# "Passthrough" = no JSON written to stdout (CC then reads the raw
# file). "Deny" = `hookSpecificOutput.permissionDecision == "deny"`
# JSON on stdout (CC blocks the Read with a reason).
#
# We invoke `bmg hook pre-read` directly with a synthesized stdin
# payload that mirrors what CC sends.
set -uo pipefail

BMG="${BMG:-bmg}"
HERE="$(cd "$(dirname "$0")" && pwd)"
RESULTS="$HERE/results/01_hook_failures.txt"
mkdir -p "$HERE/results"
: > "$RESULTS"

PASS=0
FAIL=0
log() { echo "$*" | tee -a "$RESULTS"; }

# Make a stdin payload for the hook.
payload() {
  local path="$1"
  printf '{"session_id":"s","transcript_path":"","cwd":"","hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"%s"}}' "$path"
}

check_passthrough() {
  local name="$1"
  local stdout="$2"
  local exit_code="$3"
  if [[ "$exit_code" -eq 0 ]] && [[ -z "$(echo -n "$stdout" | tr -d '[:space:]')" ]]; then
    log "  PASS  $name — passthrough (exit 0, no stdout)"
    PASS=$((PASS + 1))
  else
    log "  FAIL  $name — exit=$exit_code stdout=${stdout:0:200}"
    FAIL=$((FAIL + 1))
  fi
}

check_no_panic() {
  local name="$1"
  local exit_code="$2"
  if [[ "$exit_code" -eq 0 ]]; then
    log "  PASS  $name — exit 0 (no panic)"
    PASS=$((PASS + 1))
  else
    log "  FAIL  $name — exit $exit_code"
    FAIL=$((FAIL + 1))
  fi
}

log "=== Pressure test 1: hook failure modes ==="
log "bmg binary: $($BMG version 2>/dev/null || echo "(no version subcommand)")"
log ""

# --- (a) missing file ---
log "(a) missing file"
out=$(payload "/tmp/definitely-does-not-exist.png" | "$BMG" hook pre-read 2>/dev/null)
check_passthrough "missing-file" "$out" "$?"

# --- (b) zero-byte file with image extension ---
log "(b) zero-byte file"
zero="$HERE/fixtures/zero.png"
mkdir -p "$HERE/fixtures"
: > "$zero"
out=$(payload "$zero" | "$BMG" hook pre-read 2>/dev/null)
check_no_panic "zero-byte" "$?"
log "       (stdout shape: $(echo -n "$out" | head -c 120))"

# --- (c) BMG_DISABLE=1 ---
log "(c) BMG_DISABLE=1 (bypass)"
real_img="$HERE/../benchmark/images/01-bar-chart-quarterly.png"
out=$(payload "$real_img" | BMG_DISABLE=1 "$BMG" hook pre-read 2>/dev/null)
check_passthrough "BMG_DISABLE=1" "$out" "$?"

# --- (d) BMG_HOOK_PROXY=1 (re-entry guard) ---
log "(d) BMG_HOOK_PROXY=1 (recursion guard)"
out=$(payload "$real_img" | BMG_HOOK_PROXY=1 "$BMG" hook pre-read 2>/dev/null)
check_passthrough "BMG_HOOK_PROXY=1" "$out" "$?"

# --- (e) non-image extension (should passthrough, not call Gemini) ---
log "(e) .txt file (non-image extension)"
text="$HERE/fixtures/sample.txt"
echo "this is text" > "$text"
out=$(payload "$text" | "$BMG" hook pre-read 2>/dev/null)
check_passthrough "txt-extension" "$out" "$?"

# --- (f) no API key — fail-open ---
log "(f) no API key (fail-open)"
out=$(payload "$real_img" | env -i PATH=/usr/bin:/bin "$BMG" hook pre-read 2>/dev/null)
check_passthrough "no-api-key" "$out" "$?"

# --- (g) corrupt JSON on stdin ---
log "(g) malformed stdin JSON"
out=$(echo '{not-json' | "$BMG" hook pre-read 2>/dev/null)
ec=$?
# Hook should swallow the parse error and exit 0 (don't break CC).
check_no_panic "malformed-stdin" "$ec"

# --- (h) empty stdin ---
log "(h) empty stdin"
out=$(: | "$BMG" hook pre-read 2>/dev/null)
check_no_panic "empty-stdin" "$?"

log ""
log "=== summary: $PASS pass, $FAIL fail ==="
[[ "$FAIL" -eq 0 ]] && exit 0 || exit 1
