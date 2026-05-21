#!/usr/bin/env python3
"""Pressure test #7 — real-image variety corpus.

Generates a corpus of images that vary along axes the synthetic
benchmark didn't stress: format (PNG/JPEG/GIF/BMP/WEBP/TIFF), aspect
ratio, pixel size, color depth, mode. Runs `bmg describe` against
each and verifies:

  1. bmg accepts every format imageExtensions enumerates
  2. None of the calls fail catastrophically (returns text + isError=
     false; or graceful error)
  3. Very small / very large / weird-aspect images don't crash bmg

This intentionally does not check output quality — that's the
benchmark's job. This is a "does bmg's image pipeline survive
contact with weird real-world inputs" test.
"""

from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys

from PIL import Image, ImageDraw


HERE = pathlib.Path(__file__).resolve().parent
RESULTS = HERE / "results"
RESULTS.mkdir(exist_ok=True)
LOG = RESULTS / "07_image_corpus.txt"
CORPUS = HERE / "fixtures" / "corpus"
CORPUS.mkdir(parents=True, exist_ok=True)


def log(msg: str) -> None:
    print(msg)
    with LOG.open("a") as f:
        f.write(msg + "\n")


def synth_image(size: tuple[int, int], text: str = "test") -> Image.Image:
    """Make a simple colored image with a label so we can verify
    bmg sees it. Distinct enough to confirm not-blank."""
    img = Image.new("RGB", size, (80, 120, 200))
    draw = ImageDraw.Draw(img)
    draw.text((10, 10), text, fill="white")
    return img


def generate_corpus() -> list[pathlib.Path]:
    cases: list[tuple[str, tuple[int, int], str]] = [
        ("tiny_50x50.png",      (50, 50),     "tiny"),
        ("normal_640x480.jpg",  (640, 480),   "normal"),
        ("wide_2000x200.png",   (2000, 200),  "wide aspect"),
        ("tall_200x2000.png",   (200, 2000),  "tall aspect"),
        ("huge_3000x3000.png",  (3000, 3000), "huge"),
        ("gif_400x300.gif",     (400, 300),   "gif"),
        ("bmp_400x300.bmp",     (400, 300),   "bmp"),
        ("webp_400x300.webp",   (400, 300),   "webp"),
        ("tiff_400x300.tiff",   (400, 300),   "tiff"),
        ("greyscale_400x300.png", (400, 300), "greyscale"),
    ]
    paths = []
    for name, sz, text in cases:
        path = CORPUS / name
        if path.exists():
            paths.append(path)
            continue
        img = synth_image(sz, text=text)
        if name.startswith("greyscale"):
            img = img.convert("L")
        # Pillow figures out the format from the extension.
        img.save(path)
        paths.append(path)
    return paths


def run_describe(bmg: str, path: pathlib.Path) -> tuple[bool, str]:
    """Invoke `bmg describe -model gemini-2.5-flash` on the image.
    Returns (ok, snippet). ok=True if exit 0 and output parses as JSON
    with a summary field."""
    try:
        proc = subprocess.run(
            [bmg, "describe", "-model", "gemini-2.5-flash",
             "-profile", "general", "-pretty=false", str(path)],
            capture_output=True, text=True, timeout=60,
        )
    except subprocess.TimeoutExpired:
        return False, "TIMEOUT"
    if proc.returncode != 0:
        return False, f"exit={proc.returncode}; stderr={proc.stderr[:200]}"
    try:
        d = json.loads(proc.stdout)
    except json.JSONDecodeError:
        return False, f"non-JSON stdout (first 200): {proc.stdout[:200]}"
    summary = (d.get("summary") or "").strip()
    if not summary:
        return False, f"empty summary; structured keys: {list(d.keys())}"
    return True, summary[:100].replace("\n", " ")


def main() -> int:
    LOG.write_text("")
    log("=== Pressure test 7: real-image variety corpus ===\n")

    bmg = os.environ.get("BMG", "bmg")

    # Skip gracefully if no API key.
    if (not os.environ.get("GEMINI_API_KEY")
        and not os.environ.get("GOOGLE_API_KEY")
        and not os.environ.get("BMG_API_KEY")
        and not (pathlib.Path.home() / ".config" / "bmg" / "api_key").exists()):
        log("  SKIP  no Gemini API key available; corpus test requires API access")
        log("\n=== summary: 0 pass, 0 fail (skipped) ===")
        return 0

    paths = generate_corpus()
    log(f"corpus: {len(paths)} images in {CORPUS}\n")

    passes, fails = 0, 0
    for p in paths:
        size_bytes = p.stat().st_size
        ok, msg = run_describe(bmg, p)
        if ok:
            log(f"  PASS  {p.name:30s} ({size_bytes:>9d}B) — {msg}")
            passes += 1
        else:
            log(f"  FAIL  {p.name:30s} ({size_bytes:>9d}B) — {msg}")
            fails += 1

    log(f"\n=== summary: {passes} pass, {fails} fail ===")
    return 0 if fails == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
