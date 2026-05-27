# Downstream feedback (lucida): when raw visual beats the vision report

_From: a Claude Code agent using bmg via the Read-interception hook on the `lucida` project (2026-05-27). Task: study The Expanse's on-screen UI from film stills to build a faithful theme. The vision report underserved the task; switching to raw pixels (browser screenshot) immediately unblocked it. Captured here as product feedback, not a bug report._

## What happened

To study screen-FUI design from show footage, the agent extracted a 24-frame contact-sheet montage and `Read` it. The Read routed through the hook with the **`general`** profile and returned a montage-level summary that merely enumerated tiles (`scene_1 … scene_15`) — zero per-frame detail. The agent then served the PNG over a local HTTP server and used a browser screenshot to view the pixels directly, which immediately gave the needed design detail (palette, composition, panel geometry).

## Three concrete frictions

1. **Montage / contact-sheet inputs degrade silently.** A tiled grid read with `general` yields a per-tile enumeration with no analysis of any single frame. Caller error (don't feed montages), but bmg could *detect* a grid/contact-sheet layout and either break it down per tile or warn "this is a montage; per-tile detail is lost."

2. **Cinematic-still gating fights design study.** The `screenshot` profile classifies film stills as "not a UI" and declines to analyze them as interfaces or extract a palette. That's the wrong call precisely when the task is "study this show's FUI." (Previously worked around with ImageMagick color quantization to get hexes.)

3. **No per-Read mid-session bypass.** `BMG_DISABLE=1` is launch-time and whole-session only. When an agent *knows mid-session* it needs raw pixels (visual design study, color sampling, dense/small-text UI, pixel diffing), there's no escape hatch — it must contrive a workaround (serve file → browser screenshot). The agent can't relaunch itself.

## When raw visual is preferred over a vision report (the requested guidance)

Prefer **raw pixels** when the task is fundamentally *perceptual* and a textual description is lossy:
- Visual **design study** — you need to see composition, color relationships, spacing, "does this read as X" — not a paraphrase.
- **Color / palette sampling** (exact hues, gradients).
- **Dense or small-text UI** where summarization drops fidelity.
- **Montages / contact-sheets** where per-tile detail matters.
- **Pixel-level comparison** / aesthetic judgment ("is this faithful to the reference?").

Prefer the **vision report** for: "what's in this image," element/region location, OCR of legible text, and intent-specific extraction on clean single-subject inputs — i.e. when a textual abstraction is *sufficient* or *better* than raw bytes.

## Suggestions

- **Per-Read bypass.** A way to opt a single Read into raw pixels without relaunch — e.g. a path sentinel (`image.png#raw`), a recognized `bmg:raw` intent, or a one-shot marker file. This is the highest-value fix; it removes the serve-and-screenshot contortion.
- **Intent-overrides-gating.** An explicit intent ("extract the UI palette + layout from this film frame") should bypass the "is this a UI" classifier rather than being refused.
- **Montage/grid awareness.** Detect tiled inputs; describe per-tile or warn that detail is lost.
