# bmg — examples

Config files for bmg's surfaces.

## Files

### `deny_inline.default.json`

**Status: forward-looking. Not consumed by any shipped bmg binary.**

A classification of Claude Code MCP tools by whether they return inline
image bytes vs filesystem paths. The intent is a future PreToolUse hook
that denies tool calls returning inline bytes (the bmg `Read` redirect
only fires when an image is read by path; inline bytes bypass it).

The current bmg binary does not act on this file. It's published as a
research artifact so anyone scoping the same problem can start from a
verified tool list rather than re-classifying from scratch.

If/when the deny-inline hook ships, this file's user-editable location
will be `~/.config/bmg/deny_inline.json`.
