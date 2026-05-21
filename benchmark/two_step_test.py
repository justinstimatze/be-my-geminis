#!/usr/bin/env python3
"""Two-step deployment probe: does Claude, given bmg's typed JSON,
recover the correct answer on the 3 cases where bmg "lost" to direct
Opus in benchmark/results/summary.md?

If yes → the "bmg loses 3/9" verdict is a measurement artifact of
the apples-to-oranges synthetic comparison (bmg-direct-answer vs
Opus-direct-answer). The realistic deployment is bmg-JSON →
Claude-reads-JSON-and-answers, which is what this script measures.

If no → there's a real regression to scope.

Usage:
    benchmark/.venv/bin/python benchmark/two_step_test.py

The companion result file benchmark/two_step_results.md is the
captured output from the run that informed README's "Where bmg
loses" footnote.
"""

import json
import os
import pathlib
import sys

import anthropic

REPO = pathlib.Path(__file__).resolve().parent.parent
RESULTS = REPO / "benchmark" / "results"
CASES_JSON = REPO / "benchmark" / "cases.json"

# The 3 cases where bmg "lost" per README's per-case verdict table.
CASE_IDS = [
    "06-multi-series-readout",
    "07-heatmap-colorbar",
    "08-cluster-annotated",
]

# Ground truth for each, harvested from generate.py + cases.json.
TRUTH = {
    "06-multi-series-readout": "~85.7 (linear y = 50 + 0.85*t, t=42)",
    "07-heatmap-colorbar":     "~0.75 (grid[5,7] = 0.75 in generator)",
    "08-cluster-annotated":    "alpha (per expected_keywords)",
}

PROMPT_TEMPLATE = """A vision analysis system has produced the typed JSON below by examining an image. \
Use this JSON to answer the user's question. If the JSON contains enough information to answer, do so concisely. \
If the JSON is missing the necessary detail, say so honestly rather than guessing.

User's question:
{question}

Vision analysis (typed JSON from a profile-schema extractor):
```json
{vision_json}
```

Your answer:"""


def main():
    if not os.environ.get("ANTHROPIC_API_KEY"):
        print("ANTHROPIC_API_KEY not set", file=sys.stderr)
        sys.exit(1)

    cases = {c["id"]: c for c in json.load(open(CASES_JSON))["cases"]}
    client = anthropic.Anthropic()

    print(f"# Two-step deployment probe — bmg JSON → Opus answers Q\n")
    print("Tests whether Claude, given bmg's existing JSON for the 3 'loss' cases,")
    print("recovers the correct answer that direct-bmg failed to produce.\n")

    for cid in CASE_IDS:
        case = cases[cid]
        bmg_path = RESULTS / f"{cid}.bmg.json"
        bmg_raw = json.load(open(bmg_path))
        # The structured field has the full Gemini response; we hand
        # that to Claude as the "vision JSON" payload.
        vision_json = json.dumps(bmg_raw.get("structured", {}), indent=2)

        question = case["question"]
        truth = TRUTH[cid]

        prompt = PROMPT_TEMPLATE.format(
            question=question,
            vision_json=vision_json,
        )

        resp = client.messages.create(
            model="claude-opus-4-7",
            max_tokens=512,
            messages=[{"role": "user", "content": prompt}],
        )
        answer = "\n".join(b.text for b in resp.content if b.type == "text").strip()

        print(f"## {cid}\n")
        print(f"**Question:** {question}\n")
        print(f"**Truth:** {truth}\n")
        # The bmg-direct answer is the first non-empty line of the
        # summary the vision pipeline produced — short proxy for "what
        # did bmg-on-its-own say?"
        bmg_summary = bmg_raw.get("summary", "(no summary)")
        print(f"**bmg-direct (from .bmg.json summary):** {bmg_summary[:300]}{'...' if len(bmg_summary) > 300 else ''}\n")
        print(f"**Opus-from-bmg-JSON:** {answer}\n")
        print("---\n")


if __name__ == "__main__":
    main()
