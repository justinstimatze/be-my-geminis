# Real-world reference images

The 9 cases in `../cases.json` use matplotlib + PIL synthetic images:
deterministic, pixel-identical across runs, ground truth from the
generator source. That's great for regression testing but
unrepresentative of what bmg actually sees in production — real
screenshots have OS font anti-aliasing, JPEG re-encoding artifacts,
display gamma weirdness, and image content the generator can't
synthesize.

This directory adds three real-world references, all under
licenses that allow public-repo distribution. The CV-panel review's
nice-to-have on "synthetic-images-only" is what motivated this.

## Files

### `01-epa-degree-days.png` — chart profile

Source: [U.S. EPA, via Wikimedia Commons](https://commons.wikimedia.org/wiki/File:Heating_and_Cooling_Degree_Days_in_the_Contiguous_48_States,_1895%E2%80%932023.png)
License: **Public domain** (work of the U.S. federal government, 17 USC § 105)
Dimensions: 928 × 725 PNG, 90 KB
What it tests: dual-series time-series chart with a legend, 128-year
x-axis, and trend lines. The chart profile's `axes` + `series` +
`notable_values` extraction on a real EPA-styled chart vs. the
synthetic matplotlib renders.

### `02-gimp-screenshot.png` — screenshot profile

Source: [Wikimedia Commons, uploaded by GatoVerde95](https://commons.wikimedia.org/wiki/File:GIMP_-_2.10.34.png)
License: **CC0 1.0** (uploader-dedicated public domain); GIMP UI is GPL-licensed
Dimensions: 1366 × 768 PNG, 271 KB
What it tests: real desktop OS chrome (Xfce 4.18 on Debian 12.4),
real anti-aliased application UI, real toolbar + menu rendering.
The screenshot profile's `elements[]` with `role` / `state` /
`box_2d` on a non-synthetic UI vs. the matplotlib mock-UI in
synthetic case 05.

### `03-notld-ben.jpg` — general profile (real-photo content)

Source: [Wikimedia Commons, screencap from Night of the Living Dead (1968)](https://commons.wikimedia.org/wiki/File:Duane_Jones_as_Ben_in_Night_of_the_Living_Dead_bw.jpg)
License: **Public domain US** (1968 film released without copyright
notice; see Wikimedia page for the legal basis under pre-1989 US
copyright law)
Dimensions: 634 × 480 JPEG, grayscale, 99 KB
What it tests: real-photo content (vs. all-synthetic cases), film-
grain texture, B&W cinematography composition, real JPEG
compression artifacts. The general profile's `summary` +
`elements[]` on photographic content that the lucida-style use case
(visual recreation of a reference frame) cares about.

### `04-binarysearch-flowchart.png` — diagram profile

Source: [Wikimedia Commons, binary-search algorithm flowchart](https://commons.wikimedia.org/wiki/File:BinarySearch.Flowchart.png)
License: **Public domain** (uploader-released for any purpose
without conditions)
Dimensions: 459 × 722 PNG, 22 KB
What it tests: a real-world hand-styled algorithm flowchart vs.
the matplotlib graph-rendered synthetic case 09. Has the canonical
four node kinds (start, decision, process, end) AND non-trivial
edge labels — the decision branches carry algebraic conditions
(e.g. `A(p).Key < x`, `x = A(p).Key`) rather than the simpler
yes/no labels of the synthetic case. Tests whether the diagram
profile's `nodes` + `edges` extraction handles real-world
flowchart conventions and verbatim edge-label OCR.

## Why these and not others

These four were selected for:

1. **Real-world rendering** — none are matplotlib outputs; each
   carries the visual artifacts (anti-aliasing, JPEG compression,
   grain, hand-styled flowchart conventions) that synthetic
   images lack.
2. **Coverage of four bmg profiles** the synthetic benchmark
   doesn't differentiate strongly: chart on a real dataset, real
   OS UI rather than mock UI, photographic content rather than
   matplotlib geometry, real algorithm flowchart with non-trivial
   edge labels rather than graph-rendered synthetic.
3. **Verifiable provenance + license** — all four have clear
   Wikimedia source pages with explicit public-domain or CC0
   licensing; no fair-use gymnastics required.
4. **Manageable size** — each well under 1 MB and well under
   bmg's `MaxImageBytes` cap (50 MB). Cheap to run benchmarks
   against.

The `code` and `document` profiles remain uncovered in the
real-world set. v0.2 candidates: a code screenshot (PD source code
listing) and a scanned government document.

## Re-downloading

If you delete these and want them back:

```bash
curl -sSL -o 01-epa-degree-days.png \
  'https://upload.wikimedia.org/wikipedia/commons/2/20/Heating_and_Cooling_Degree_Days_in_the_Contiguous_48_States%2C_1895%E2%80%932023.png'

curl -sSL -o 02-gimp-screenshot.png \
  'https://upload.wikimedia.org/wikipedia/commons/e/e9/GIMP_-_2.10.34.png'

curl -sSL -o 03-notld-ben.jpg \
  'https://upload.wikimedia.org/wikipedia/commons/7/71/Duane_Jones_as_Ben_in_Night_of_the_Living_Dead_bw.jpg'

curl -sSL -o 04-binarysearch-flowchart.png \
  'https://upload.wikimedia.org/wikipedia/commons/4/41/BinarySearch.Flowchart.png'
```
