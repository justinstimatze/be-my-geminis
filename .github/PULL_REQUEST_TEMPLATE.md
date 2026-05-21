<!-- One PR per concern is preferred. If you're bundling, name the
     concerns and order them roughly by risk. -->

## What

<!-- One-sentence summary of the change. -->

## Why

<!-- The motivation: the bug it fixes, the use case it enables, or
     the constraint it lifts. Link to an issue if there is one. -->

## How

<!-- Implementation sketch — what changed, what stayed. Mention
     anything non-obvious: a constraint you tripped on, a path you
     tried that didn't work, a downstream contract you preserved. -->

## Testing

<!-- - [ ] gofmt -l clean, go vet clean, go test -race ./... green
     - [ ] If touching the hook: pressure_test/ green
     - [ ] If touching a profile: benchmark/cases.json updated
     - [ ] If touching the wire protocol: cacheSchemaVersion bumped
     - [ ] Manual repro of the original bug (if a fix)               -->

## Breaking changes

<!-- None / or describe. The two stable contracts to watch:
     - <bmg-vision-report> / <bmg-ocr-text> fenced markdown shape
       (Claude is primed on this via SessionStart)
     - mcp__bemygeminis__bmg_describe argument names                 -->
