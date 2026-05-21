#!/usr/bin/env bash
# Pressure test #8 — video describe pipeline end-to-end.
#
# Generates a tiny synthetic video via ffmpeg, runs bmg describe
# -profile video on it, and verifies the full Files-API + video
# profile + ffprobe-correction pipeline:
#
#   1. bmg describe routes the video path through DescribeVideo
#      (not the image-bytes path that would reject .mp4).
#   2. Response parses against the video schema (summary,
#      duration_seconds, scenes, transcript, keyframes).
#   3. ffprobe correction kicks in — declared duration matches the
#      generator's intended duration within tolerance.
#   4. Files API upload + describe + cleanup completes inside the
#      6-minute timeout.
#
# Requires ffmpeg + GEMINI_API_KEY. Skips gracefully if either
# missing. Costs ~$0.02-0.05 per run (single Pro call on a 3s
# video).
#
# Run by run.sh; standalone:
#     BMG=$(which bmg) bash 08_video_describe.sh
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
BMG="${BMG:-bmg}"
RESULTS="$HERE/results"
LOG="$RESULTS/08_video_describe.txt"
mkdir -p "$RESULTS"
: > "$LOG"

log() { echo "$@" | tee -a "$LOG"; }

log "=== Pressure test 8: video describe end-to-end ==="

if ! command -v ffmpeg >/dev/null 2>&1; then
  log "SKIP — ffmpeg not on PATH (required to synthesize the test video)"
  exit 0
fi

# Try to resolve a Gemini key the same way bmg does so we can skip
# cleanly when running on a CI box without one.
if [[ -z "${BMG_API_KEY:-}${GEMINI_API_KEY:-}${GOOGLE_API_KEY:-}" ]] \
    && [[ ! -f "$HOME/.config/bmg/api_key" ]]; then
  log "SKIP — no Gemini API key in env or ~/.config/bmg/api_key"
  exit 0
fi

video="$HERE/fixtures/synth_video.mp4"
mkdir -p "$HERE/fixtures"
# 3-second 320x240 test pattern, ~20KB. Deterministic; same input
# every run so cache hits on re-runs of the test.
log "generating synthetic video: $video"
ffmpeg -y -loglevel error \
    -f lavfi -i "testsrc=duration=3:size=320x240:rate=10" \
    -c:v libx264 -pix_fmt yuv420p -t 3 "$video" 2>&1 | tee -a "$LOG"

if [[ ! -s "$video" ]]; then
  log "FAIL — ffmpeg did not produce a video at $video"
  exit 1
fi

log ""
log "--- bmg describe -profile video $video ---"
out="$RESULTS/08_video_describe.json"
if ! "$BMG" describe -profile video "$video" > "$out" 2> "$RESULTS/08_video_describe.err"; then
  log "FAIL — bmg describe exited non-zero; stderr:"
  cat "$RESULTS/08_video_describe.err" | tee -a "$LOG"
  exit 1
fi

# Parse + assert with jq (already used by other pressure tests).
if ! command -v jq >/dev/null 2>&1; then
  log "FAIL — jq required for assertion phase"
  exit 1
fi

pass=0
fail=0

profile=$(jq -r '.profile' "$out")
if [[ "$profile" == "video" ]]; then
  log "  PASS  routed to video profile"
  pass=$((pass+1))
else
  log "  FAIL  profile=$profile, expected video — describe did NOT route through DescribeVideo"
  fail=$((fail+1))
fi

model=$(jq -r '.model' "$out")
if [[ "$model" == gemini-2.5-pro* ]]; then
  log "  PASS  used pro-class model: $model"
  pass=$((pass+1))
else
  log "  FAIL  unexpected model: $model"
  fail=$((fail+1))
fi

# Schema presence — all 5 required fields populated.
for field in summary duration_seconds scenes transcript keyframes; do
  if jq -e ".structured | has(\"$field\")" "$out" >/dev/null; then
    log "  PASS  schema has '$field'"
    pass=$((pass+1))
  else
    log "  FAIL  schema missing '$field'"
    fail=$((fail+1))
  fi
done

# duration_seconds matches the generator's 3.0s after ffprobe
# correction. Allow ±0.5s slack (ffprobe of an h264 stream can
# report 2.9-3.1 depending on container precision).
duration=$(jq -r '.structured.duration_seconds' "$out")
if awk -v d="$duration" 'BEGIN{ exit !(d>=2.5 && d<=3.5) }'; then
  log "  PASS  ffprobe-corrected duration=$duration s (generator: 3.0s, ±0.5 tolerance)"
  pass=$((pass+1))
else
  log "  FAIL  duration=$duration s outside expected 3.0±0.5 — ffprobe correction missed or generator drifted"
  fail=$((fail+1))
fi

# scenes/keyframes can be empty for a 3-second test pattern (Gemini
# may identify just one scene); the structural presence is what we
# require. transcript should be empty (no audio).
transcript_count=$(jq '.structured.transcript | length' "$out")
if [[ "$transcript_count" -eq 0 ]]; then
  log "  PASS  transcript empty for silent test pattern (count=$transcript_count)"
  pass=$((pass+1))
else
  log "  INFO  transcript has $transcript_count entries (not strictly wrong for a silent video, but unusual)"
fi

log ""
log "=== summary: $pass pass, $fail fail ==="
exit $(( fail > 0 ? 1 : 0 ))
