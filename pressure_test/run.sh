#!/usr/bin/env bash
# Run all pressure tests, aggregate the per-test pass/fail counts,
# and emit results/summary.md. Exits non-zero if any test fails.
#
# Tests 6 and 7 (concurrent cache + image corpus) require a Gemini
# API key in env or ~/.config/bmg/api_key. They skip gracefully if
# unavailable.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
BMG="${BMG:-bmg}"
export BMG

mkdir -p "$HERE/results"

PY="${PY:-python3}"
PT_VENV="$HERE/../benchmark/.venv/bin/python"
if [[ -x "$PT_VENV" ]]; then
  PY="$PT_VENV"
fi

declare -A RESULT
tests=(
  "01_hook_failures.sh::bash"
  "02_install_roundtrip.sh::bash"
  "03_mcp_robustness.sh::bash"
  "04_prompt_injection.py::py"
  "05_cc_integration.sh::bash"
  "06_concurrent_cache.sh::bash"
  "07_image_corpus.py::py"
  "08_video_describe.sh::bash"
  "09_post_tool_prewarm.sh::bash"
)

overall_fail=0
echo "=== bmg pressure test suite ===" | tee "$HERE/results/summary.md"
echo "" | tee -a "$HERE/results/summary.md"

for spec in "${tests[@]}"; do
  name="${spec%%::*}"
  runner="${spec##*::}"
  script="$HERE/$name"
  echo "--- $name ---" | tee -a "$HERE/results/summary.md"
  if [[ "$runner" == "py" ]]; then
    "$PY" "$script"
  else
    bash "$script"
  fi
  rc=$?
  if [[ "$rc" -eq 0 ]]; then
    RESULT[$name]="PASS"
  else
    RESULT[$name]="FAIL"
    overall_fail=1
  fi
  echo "" | tee -a "$HERE/results/summary.md"
done

echo "=== aggregate ===" | tee -a "$HERE/results/summary.md"
for spec in "${tests[@]}"; do
  name="${spec%%::*}"
  echo "  ${RESULT[$name]:-MISSING}  $name" | tee -a "$HERE/results/summary.md"
done

[[ "$overall_fail" -eq 0 ]] && echo "ALL PASSED" | tee -a "$HERE/results/summary.md" \
                            || echo "ONE OR MORE FAILED" | tee -a "$HERE/results/summary.md"

exit "$overall_fail"
