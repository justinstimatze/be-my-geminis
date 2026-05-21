#!/usr/bin/env python3
"""Render the 5 synthetic benchmark images from cases.json.

Deterministic — same input config always produces the same PNG bytes.
Run from the benchmark/ directory with the venv active:

    benchmark/.venv/bin/python benchmark/generate.py

Outputs land in benchmark/images/.
"""

import json
import pathlib
import random

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import matplotlib.patches as mpatches
from PIL import Image, ImageDraw, ImageFont


HERE = pathlib.Path(__file__).resolve().parent
IMG_DIR = HERE / "images"
IMG_DIR.mkdir(exist_ok=True)


# ----- 01: bar chart with specific values ---------------------------------

def gen_bar_chart_quarterly():
    fig, ax = plt.subplots(figsize=(7, 4.5))
    quarters = ["Q1", "Q2", "Q3", "Q4"]
    values = [42, 18, 7, 23]
    bars = ax.bar(quarters, values, color=["#3b82f6", "#3b82f6", "#ef4444", "#3b82f6"])
    ax.set_title("Quarterly Revenue 2026 ($M)", fontsize=14, fontweight="bold")
    ax.set_ylabel("$ millions")
    ax.set_ylim(0, 50)
    ax.grid(axis="y", alpha=0.3)
    for bar, value in zip(bars, values):
        ax.text(bar.get_x() + bar.get_width() / 2, value + 1.5,
                str(value), ha="center", fontsize=11)
    fig.tight_layout()
    fig.savefig(IMG_DIR / "01-bar-chart-quarterly.png", dpi=110)
    plt.close(fig)


# ----- 02: multi-panel correlation; panel C is strongest -------------------

def gen_multi_panel_correlation():
    rng = random.Random(42)
    fig, axes = plt.subplots(2, 2, figsize=(8, 7))
    panels = [
        ("A", 0.15),  # weak
        ("B", -0.55),  # moderate negative
        ("C", 0.97),  # strongest linear
        ("D", 0.40),  # moderate positive
    ]
    for ax, (label, target_r) in zip(axes.flat, panels):
        n = 40
        x = [rng.uniform(0, 10) for _ in range(n)]
        # Add y as target_r * x + noise (rough)
        y = [target_r * xi + rng.gauss(0, max(0.4, 4 * (1 - abs(target_r)))) for xi in x]
        ax.scatter(x, y, s=18, alpha=0.7, color="#1f2937")
        ax.set_title(f"Panel {label}", fontsize=12, fontweight="bold")
        ax.grid(alpha=0.25)
        ax.set_xticks([])
        ax.set_yticks([])
    fig.suptitle("Scatter comparison across four panels", fontsize=13, fontweight="bold")
    fig.tight_layout()
    fig.savefig(IMG_DIR / "02-multi-panel-correlation.png", dpi=110)
    plt.close(fig)


# ----- 03: directed graph (matplotlib boxes + arrows) ----------------------

def gen_directed_graph():
    """A simple 6-node directed graph rendered with matplotlib patches.
    Shortest path start -> validate -> approve -> end.
    """
    fig, ax = plt.subplots(figsize=(8.5, 5))
    ax.set_xlim(0, 10)
    ax.set_ylim(0, 6)
    ax.axis("off")

    nodes = {
        "start":    (1.0, 3.0),
        "validate": (3.5, 3.0),
        "reject":   (3.5, 0.8),
        "approve":  (6.0, 3.0),
        "queue":    (6.0, 0.8),
        "end":      (8.5, 3.0),
    }
    for name, (x, y) in nodes.items():
        ax.add_patch(mpatches.FancyBboxPatch(
            (x - 0.7, y - 0.35), 1.4, 0.7,
            boxstyle="round,pad=0.05",
            facecolor="#e0f2fe", edgecolor="#0369a1", linewidth=1.5))
        ax.text(x, y, name, ha="center", va="center", fontsize=11, fontweight="bold")

    edges = [
        ("start", "validate"),
        ("validate", "approve"),
        ("validate", "reject"),
        ("approve", "end"),
        ("reject", "queue"),
        ("queue", "validate"),
    ]
    for src, dst in edges:
        x1, y1 = nodes[src]
        x2, y2 = nodes[dst]
        # Shorten line so arrow doesn't overlap box.
        ax.annotate(
            "", xy=(x2, y2), xytext=(x1, y1),
            arrowprops=dict(
                arrowstyle="->,head_length=0.4,head_width=0.25",
                color="#0369a1",
                lw=1.5,
                shrinkA=22, shrinkB=22,
                connectionstyle="arc3,rad=0.0"))

    ax.set_title("Approval workflow (directed graph)", fontsize=13, fontweight="bold")
    fig.tight_layout()
    fig.savefig(IMG_DIR / "03-directed-graph.png", dpi=110)
    plt.close(fig)


# ----- 04: synthetic receipt (PIL) -----------------------------------------

def gen_synthetic_receipt():
    W, H = 480, 720
    img = Image.new("RGB", (W, H), "white")
    draw = ImageDraw.Draw(img)

    # Try real fonts; fall back to default.
    try:
        title_font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 20)
        body_font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf", 15)
        small_font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 12)
    except OSError:
        title_font = ImageFont.load_default()
        body_font = ImageFont.load_default()
        small_font = ImageFont.load_default()

    y = 30
    draw.text((W / 2, y), "The Corner Cafe", anchor="mm", font=title_font, fill="black")
    y += 30
    draw.text((W / 2, y), "123 Main St · Receipt #4081", anchor="mm", font=small_font, fill="black")
    y += 18
    draw.text((W / 2, y), "2026-05-19  18:42", anchor="mm", font=small_font, fill="black")
    y += 30
    draw.line([(40, y), (W - 40, y)], fill="black", width=1)
    y += 18

    items = [
        ("Avocado Toast",    "12.50"),
        ("Black Coffee",     " 4.25"),
        ("Side Salad",       " 9.75"),
        ("Cheesecake",       " 7.50"),
    ]
    for name, price in items:
        draw.text((50, y), name, font=body_font, fill="black")
        draw.text((W - 50, y), f"${price.strip()}", anchor="ra", font=body_font, fill="black")
        y += 24

    y += 8
    draw.line([(40, y), (W - 40, y)], fill="black", width=1)
    y += 14

    subtotal_lines = [
        ("Subtotal", "34.00"),
        ("Tax (8%)", " 2.72"),
        ("Tip",      " 1.73"),
    ]
    for name, price in subtotal_lines:
        draw.text((50, y), name, font=body_font, fill="black")
        draw.text((W - 50, y), f"${price.strip()}", anchor="ra", font=body_font, fill="black")
        y += 22

    y += 6
    draw.line([(40, y), (W - 40, y)], fill="black", width=2)
    y += 16
    draw.text((50, y), "TOTAL", font=title_font, fill="black")
    draw.text((W - 50, y), "$38.45", anchor="ra", font=title_font, fill="black")
    y += 50
    draw.text((W / 2, y), "Thank you!", anchor="mm", font=small_font, fill="black")

    img.save(IMG_DIR / "04-synthetic-receipt.png")


# ----- 05: mock UI with an error banner (PIL) ------------------------------

def gen_mock_ui_error():
    W, H = 960, 600
    img = Image.new("RGB", (W, H), "#f8fafc")
    draw = ImageDraw.Draw(img)

    try:
        title_font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 22)
        body_font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 16)
        small_font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 13)
        chrome_font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 14)
    except OSError:
        title_font = ImageFont.load_default()
        body_font = ImageFont.load_default()
        small_font = ImageFont.load_default()
        chrome_font = ImageFont.load_default()

    # Browser-ish top chrome.
    draw.rectangle([(0, 0), (W, 56)], fill="#e2e8f0")
    for cx, color in [(20, "#ef4444"), (44, "#f59e0b"), (68, "#10b981")]:
        draw.ellipse([(cx - 8, 20), (cx + 8, 36)], fill=color)
    draw.rectangle([(110, 16), (W - 30, 40)], fill="white", outline="#94a3b8")
    draw.text((125, 28), "https://app.example.com/dashboard", anchor="lm", font=chrome_font, fill="#475569")

    # App header.
    draw.rectangle([(0, 56), (W, 110)], fill="#1e293b")
    draw.text((40, 83), "Acme Dashboard", anchor="lm", font=title_font, fill="white")
    draw.text((W - 40, 83), "user@example.com", anchor="rm", font=body_font, fill="#cbd5e1")

    # Sidebar.
    draw.rectangle([(0, 110), (200, H)], fill="#0f172a")
    sidebar_items = ["Overview", "Reports", "Settings", "Help"]
    for i, item in enumerate(sidebar_items):
        draw.text((24, 144 + i * 36), item, font=body_font, fill="#cbd5e1")

    # Main content area.
    draw.rectangle([(200, 110), (W, H)], fill="#f8fafc")

    # ERROR BANNER (the thing to read).
    banner_top = 140
    banner_bottom = 200
    draw.rectangle([(224, banner_top), (W - 24, banner_bottom)], fill="#fee2e2", outline="#dc2626", width=2)
    draw.text((244, banner_top + 14), "Session expired", font=title_font, fill="#991b1b")
    draw.text((244, banner_top + 40),
              "Please sign in again to continue.",
              font=body_font, fill="#7f1d1d")
    draw.text((W - 40, banner_top + 30), "✕", anchor="rm", font=title_font, fill="#dc2626")

    # Some greyed-out content below to make the banner stand out.
    draw.rectangle([(224, 230), (W - 24, 290)], fill="white", outline="#cbd5e1")
    draw.text((244, 260), "Recent activity", font=body_font, fill="#94a3b8")
    draw.rectangle([(224, 310), (W - 24, 510)], fill="white", outline="#cbd5e1")
    draw.text((244, 340), "Reports loading…", font=body_font, fill="#94a3b8")

    img.save(IMG_DIR / "05-mock-ui-error.png")


# ----- 06: multi-series line chart with precise readout --------------------

def gen_multi_series_readout():
    rng = random.Random(7)
    fig, ax = plt.subplots(figsize=(8, 4.5))
    series_specs = [
        ("cpu_pct",      lambda t: 30 + 8 * (t % 7) + rng.gauss(0, 1.5)),
        ("memory_kb",    lambda t: 50 + t * 0.85 + rng.gauss(0, 2)),  # at t=42 ≈ 86
        ("disk_io",      lambda t: 70 - 0.5 * t + rng.gauss(0, 3)),
        ("net_in_mbps",  lambda t: 20 + 15 * abs(((t - 30) / 30)) + rng.gauss(0, 1.5)),
        ("net_out_mbps", lambda t: 25 + 10 * ((t // 10) % 3) + rng.gauss(0, 1.5)),
        ("requests_qps", lambda t: 60 + 12 * (1 - (t % 13) / 13) + rng.gauss(0, 2)),
    ]
    xs = list(range(0, 60))
    palette = ["#1f77b4", "#ff7f0e", "#2ca02c", "#d62728", "#9467bd", "#8c564b"]
    for (name, fn), color in zip(series_specs, palette):
        rng2 = random.Random(7 + hash(name) % 1000)
        # Re-seed each so memory_kb is reproducible at t=42 = 86
        def gen_series(name, fn):
            return [fn(t) for t in xs]
        if name == "memory_kb":
            ys = [50 + t * 0.85 for t in xs]  # deterministic, no noise -> t=42 = 50+35.7 = 85.7
        else:
            ys = gen_series(name, fn)
        ax.plot(xs, ys, label=name, color=color, linewidth=1.4)
    ax.set_title("Process telemetry", fontsize=12, fontweight="bold")
    ax.set_xlabel("timestamp (t)")
    ax.set_ylabel("value")
    ax.grid(alpha=0.3)
    ax.legend(fontsize=8, loc="upper right", ncol=2, frameon=True)
    fig.tight_layout()
    fig.savefig(IMG_DIR / "06-multi-series-readout.png", dpi=110)
    plt.close(fig)


# ----- 07: heatmap with colorbar requiring lookup -------------------------

def gen_heatmap_colorbar():
    import numpy as np
    rng = np.random.default_rng(11)
    grid = rng.random((10, 10))
    # Bake known value at (row=5, col=7).
    grid[5, 7] = 0.75
    fig, ax = plt.subplots(figsize=(7, 5.5))
    im = ax.imshow(grid, cmap="viridis", vmin=0, vmax=1, aspect="auto")
    ax.set_xticks(range(10))
    ax.set_xticklabels([f"c{i}" for i in range(10)], fontsize=9)
    ax.set_yticks(range(10))
    ax.set_yticklabels([f"r{i}" for i in range(10)], fontsize=9)
    ax.set_title("Activation heatmap (10x10)", fontsize=12, fontweight="bold")
    cbar = fig.colorbar(im, ax=ax, fraction=0.05, pad=0.04)
    cbar.set_label("activation")
    fig.tight_layout()
    fig.savefig(IMG_DIR / "07-heatmap-colorbar.png", dpi=110)
    plt.close(fig)


# ----- 08: cluster scatter with arrow annotations --------------------------

def gen_cluster_annotated():
    rng = random.Random(23)
    fig, ax = plt.subplots(figsize=(8, 6))
    clusters = [
        ("alpha",  (3.0, 5.0), 30, "#1f77b4"),
        ("beta",   (8.0, 2.0), 30, "#ff7f0e"),
        ("gamma",  (1.5, 1.5), 30, "#2ca02c"),
        ("delta",  (7.0, 7.5), 30, "#d62728"),
    ]
    for name, (cx, cy), n, color in clusters:
        xs = [rng.gauss(cx, 0.45) for _ in range(n)]
        ys = [rng.gauss(cy, 0.45) for _ in range(n)]
        ax.scatter(xs, ys, color=color, s=22, alpha=0.85)
    # Annotations: arrow + label OFFSET away from cluster centers so the
    # labels are clearly separate from the data points.
    label_offsets = {
        "alpha": (-1.6, 1.8),
        "beta":  (1.7, -1.0),
        "gamma": (-1.5, -1.5),
        "delta": (1.5, 1.2),
    }
    for (name, (cx, cy), _, color) in clusters:
        ox, oy = label_offsets[name]
        ax.annotate(
            name,
            xy=(cx, cy),
            xytext=(cx + ox, cy + oy),
            fontsize=12, fontweight="bold", color=color,
            arrowprops=dict(arrowstyle="->", color=color, lw=1.5))
    ax.set_xlim(0, 10)
    ax.set_ylim(0, 10)
    ax.set_xlabel("x")
    ax.set_ylabel("y")
    ax.set_title("Annotated clusters", fontsize=13, fontweight="bold")
    ax.grid(alpha=0.3)
    fig.tight_layout()
    fig.savefig(IMG_DIR / "08-cluster-annotated.png", dpi=110)
    plt.close(fig)


# ----- 09: complex flow diagram (10 nodes, 13+ edges, mixed kinds) --------

def gen_complex_flow_json():
    fig, ax = plt.subplots(figsize=(11, 7))
    ax.set_xlim(0, 12)
    ax.set_ylim(0, 8)
    ax.axis("off")

    # node: name -> (x, y, kind)
    nodes = {
        "start":     (1.0, 4.0, "start"),
        "fetch":     (3.0, 4.0, "process"),
        "validate":  (5.0, 4.0, "decision"),
        "transform": (7.0, 5.5, "process"),
        "reject":    (5.0, 2.0, "process"),
        "retry":     (3.0, 2.0, "decision"),
        "approve":   (9.0, 5.5, "process"),
        "audit":     (7.0, 2.0, "process"),
        "deadletter":(9.0, 2.0, "end"),
        "end":       (11.0, 5.5, "end"),
    }
    kind_color = {
        "start": "#4ade80", "process": "#e0f2fe",
        "decision": "#fde68a", "end": "#fca5a5",
    }
    for name, (x, y, kind) in nodes.items():
        shape = mpatches.FancyBboxPatch(
            (x - 0.75, y - 0.32), 1.5, 0.64,
            boxstyle="round,pad=0.05" if kind != "decision" else "round4,pad=0.05",
            facecolor=kind_color[kind], edgecolor="#0369a1", linewidth=1.5)
        ax.add_patch(shape)
        ax.text(x, y, name, ha="center", va="center", fontsize=10, fontweight="bold")
        ax.text(x, y - 0.55, f"({kind})", ha="center", va="center", fontsize=7, color="#475569")

    edges = [
        ("start", "fetch", ""),
        ("fetch", "validate", ""),
        ("validate", "transform", "ok"),
        ("validate", "reject", "bad"),
        ("transform", "approve", ""),
        ("approve", "end", ""),
        ("reject", "retry", ""),
        ("retry", "fetch", "yes"),
        ("retry", "audit", "no"),
        ("audit", "deadletter", "fail"),
        ("audit", "approve", "pass"),
        ("approve", "audit", "sample"),
        ("transform", "audit", "spot-check"),
    ]
    for src, dst, label in edges:
        x1, y1, _ = nodes[src]
        x2, y2, _ = nodes[dst]
        ax.annotate(
            "", xy=(x2, y2), xytext=(x1, y1),
            arrowprops=dict(arrowstyle="->,head_length=0.4,head_width=0.25",
                            color="#0369a1", lw=1.2,
                            shrinkA=24, shrinkB=24))
        if label:
            mx, my = (x1 + x2) / 2, (y1 + y2) / 2
            ax.text(mx, my + 0.18, label, fontsize=8, color="#0369a1",
                    ha="center", va="center",
                    bbox=dict(boxstyle="round,pad=0.15", fc="white", ec="none"))

    ax.set_title("Payment processing workflow", fontsize=13, fontweight="bold")
    fig.tight_layout()
    fig.savefig(IMG_DIR / "09-complex-flow-json.png", dpi=110)
    plt.close(fig)


def main():
    gen_bar_chart_quarterly()
    gen_multi_panel_correlation()
    gen_directed_graph()
    gen_synthetic_receipt()
    gen_mock_ui_error()
    gen_multi_series_readout()
    gen_heatmap_colorbar()
    gen_cluster_annotated()
    gen_complex_flow_json()

    cases_path = HERE / "cases.json"
    with cases_path.open() as f:
        cases = json.load(f)["cases"]

    print(f"Generated {len(list(IMG_DIR.glob('*.png')))} images in {IMG_DIR}/")
    for c in cases:
        img_path = HERE / c["image"]
        size = img_path.stat().st_size if img_path.exists() else 0
        print(f"  {c['id']:32s} {size:>8d} bytes")


if __name__ == "__main__":
    main()
