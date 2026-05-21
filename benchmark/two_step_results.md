# Two-step deployment probe — bmg JSON → Opus answers Q

Tests whether Claude, given bmg's existing JSON for the 3 'loss' cases,
recovers the correct answer that direct-bmg failed to produce.

## 06-multi-series-readout

**Question:** On this multi-series line chart, at timestamp t=42, what is the value of the 'memory_kb' series? Give me the number.

**Truth:** ~85.7 (linear y = 50 + 0.85*t, t=42)

**bmg-direct (from .bmg.json summary):** This line chart displays six process telemetry metrics over time. The 'memory_kb' series, shown in orange, exhibits a steady, linear increase from a value of 50 at timestamp 0 to approximately 90 at timestamp 60. At the specific timestamp x=42, the value for 'memory_kb' is approximately 78. Other me...

**Opus-from-bmg-JSON:** Approximately 78.

---

## 07-heatmap-colorbar

**Question:** Find the cell at the intersection of the row labeled exactly 'r5' and the column labeled exactly 'c7' (these are the literal tick-label strings printed on the axes, not 1-based positional indices). Use the viridis colorbar on the right to convert that cell's color to a numeric value.

**Truth:** ~0.75 (grid[5,7] = 0.75 in generator)

**bmg-direct (from .bmg.json summary):** This chart is a 10x10 heatmap titled 'Activation heatmap (10x10)', displaying activation values for a grid of cells. The rows are labeled 'r0' through 'r9' and columns 'c0' through 'c9'. A colorbar on the right indicates that the activation values range from 0.0 (dark purple) to 1.0 (bright yellow)....

**Opus-from-bmg-JSON:** Based on the JSON, the cell at row 'r5' and column 'c7' is dark purple, corresponding to an activation value of approximately **0.1**.

---

## 08-cluster-annotated

**Question:** There are four labeled clusters in this scatter plot, with arrows annotating each. Which cluster is centered at approximately (3, 5)? Give me its label.

**Truth:** alpha (per expected_keywords)

**bmg-direct (from .bmg.json summary):** A scatter plot titled 'Annotated clusters' displays four distinct clusters of data points on a 2D plane. The x and y axes both range from 0 to 10. Each cluster is color-coded and labeled: 'alpha' (blue) is in the top-left quadrant, 'delta' (red) is in the top-right, 'gamma' (green) is in the bottom-...

**Opus-from-bmg-JSON:** Based on the JSON, I need to find the cluster centered at approximately (3, 5).

Looking at the axis tick positions:
- x=2 is at pixel ~310, x=4 is at ~434, so x=3 is at ~372
- y=4 is at pixel ~422, y=6 is at ~308, so y=5 is at ~365

The **alpha_cluster** bounding box is [420, 247, 555, 432], with center around (488, 340). Converting to data coordinates: x ≈ 3.0, y ≈ 4.5 — closest to (3, 5).

**Answer: alpha**

---

