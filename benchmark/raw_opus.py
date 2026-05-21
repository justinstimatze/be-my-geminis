#!/usr/bin/env python3
"""Send a single (image, question) pair to Claude Opus 4.7 via the
Anthropic API and print the response.

Used by benchmark/run.sh to capture the "raw Opus" condition for the
side-by-side README comparison. Requires ANTHROPIC_API_KEY in env.

Usage:
    python raw_opus.py path/to/image.png "What's in this image?"
"""

import base64
import os
import pathlib
import sys

import anthropic

MODEL = "claude-opus-4-7"


def main():
    if len(sys.argv) != 3:
        print("usage: raw_opus.py <image_path> <question>", file=sys.stderr)
        sys.exit(2)
    img_path = pathlib.Path(sys.argv[1])
    question = sys.argv[2]

    if not img_path.exists():
        print(f"raw_opus.py: image not found: {img_path}", file=sys.stderr)
        sys.exit(1)

    if not os.environ.get("ANTHROPIC_API_KEY"):
        print("raw_opus.py: ANTHROPIC_API_KEY not set", file=sys.stderr)
        sys.exit(1)

    media_type = "image/png" if img_path.suffix.lower() == ".png" else "image/jpeg"
    data = base64.standard_b64encode(img_path.read_bytes()).decode()

    client = anthropic.Anthropic()
    resp = client.messages.create(
        model=MODEL,
        max_tokens=1024,
        messages=[
            {
                "role": "user",
                "content": [
                    {
                        "type": "image",
                        "source": {
                            "type": "base64",
                            "media_type": media_type,
                            "data": data,
                        },
                    },
                    {"type": "text", "text": question},
                ],
            }
        ],
    )

    parts = [b.text for b in resp.content if b.type == "text"]
    print("\n".join(parts).strip())


if __name__ == "__main__":
    main()
