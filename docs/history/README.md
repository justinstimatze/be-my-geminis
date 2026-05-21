# bmg — historical review artifacts

A snapshot of pre-v0.1.0 review work, kept for accountability. Before
flipping the repo public, five panel-style reviews swept the codebase
for security, OSS hygiene, CV/multimodal correctness, future-
maintainer pain, and skeptical-user critique. Every block-release
finding was either fixed, deferred with a stated rationale, or ruled
an overclaim.

## Files

- [`findings.md`](findings.md) — consolidated status table across all
  panels (FIXED / DEFERRED / WONTFIX). Reads top-down.
- [`batch1_security.md`](batch1_security.md) — Security panel:
  trust-fence breakout, atomic writes, key file modes, etc.
- [`batch1_skeptical_hn.md`](batch1_skeptical_hn.md) — Skeptical user:
  cherry-picked benchmark cases, privacy disclosure, cost claims.
- [`batch1_cv_expert.md`](batch1_cv_expert.md) — CV / multimodal:
  prompt+image part order, system-instruction shape, temperature
  default.
- [`batch2_oss_maintainer.md`](batch2_oss_maintainer.md) — OSS
  maintainer: Go version floor, CHANGELOG hygiene, CI matrix, install
  path.
- [`batch2_future_maintainer.md`](batch2_future_maintainer.md) —
  Future-maintainer + API stability: SDK update cadence, schema
  evolution, tribal-knowledge constants.

## Reading notes

Commit hashes referenced in these documents (e.g.
`[fdc3b41](...)`) point at the development history that existed before
public release. If the v0.x release squashes development commits into
a single initial public commit, those references won't resolve in the
public history — they're frozen as labels for what work the finding
was retired against, not as live navigable links.

The "Open items remaining before v0.1.0 tag" section of
[`findings.md`](findings.md) is also frozen — most of those items have
since been addressed (see CHANGELOG `Unreleased` entries for the v0.2
cycle).
