# benchmark/

A reproducible 9-case A/B comparing **raw Claude Opus 4.7** vs
**bmg → Gemini-2.5-Pro with a typed profile**, on the same (image,
question) pairs.

The point isn't a leaderboard — it's a side-by-side rendering of what
each surface actually returns, so a reader can see the shape
difference (prose vs profile-typed JSON) without trusting an aggregate
score.

## What's here

```
benchmark/
├── cases.json         declarative case definitions (image gen + question + profile)
├── generate.py        deterministic matplotlib/PIL image renderers
├── raw_opus.py        sends (image, question) to claude-opus-4-7 via the Anthropic SDK
├── run.sh             orchestrates the 9-case A/B and emits results/summary.md
├── images/            9 generated PNGs (committed for reproducibility)
└── results/           per-case .raw.txt + .bmg.{json,txt} + summary.md
```

## Run it

```bash
# one-time
cd benchmark
uv venv .venv
uv pip install --python .venv/bin/python matplotlib pillow numpy anthropic

# regenerate images (deterministic; rerun if cases.json changes)
.venv/bin/python generate.py

# full A/B
ANTHROPIC_API_KEY=sk-...  GOOGLE_API_KEY=...  BMG=$(which bmg)  ./run.sh

# read the result
$EDITOR results/summary.md
```

Cost per run: ~9 Anthropic calls + 9 Gemini-2.5-Pro calls ≈ $1–2.

## Case categories

| id | profile | tests |
|---|---|---|
| 01 bar chart | chart | reading specific values off a bar chart |
| 02 four-panel scatter | general | identifying the panel with strongest correlation |
| 03 directed graph | diagram | tracing a path through a simple graph |
| 04 receipt | document | total + item count from a synthetic receipt |
| 05 UI error banner | screenshot | reading an error message and its suggested action |
| 06 multi-series chart | chart | precise per-series readout at a specific x |
| 07 heatmap + colorbar | chart | cell color → colorbar → numeric value lookup |
| 08 annotated clusters | general | matching arrow labels to scatter centroids |
| 09 complex flow JSON | diagram | full graph (10 nodes, 13 edges) as structured JSON |

## Reading the result

For each case `results/summary.md` shows three blocks:

1. **raw Opus 4.7** — prose response to the question.
2. **bmg summary** — Gemini-2.5-Pro's natural-language summary.
3. **bmg structured** — the profile-typed JSON excerpt (`series` /
   `nodes` / `edges` / `key_facts` / etc.).

The reader gets to judge "is the structured JSON actually useful, or
is it noise?" without trusting our aggregate scoring.
