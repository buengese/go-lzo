# Changelog

This library is a hard fork of [anchore/go-lzo](https://github.com/anchore/go-lzo). Releases up to v0.1.1 are
upstream releases.

## Unreleased

### Changed

- The module path is now `github.com/buengese/go-lzo`.
- `Decompress` was rewritten as a single loop that copies in whole words where possible. It is 2.5-5× faster on
  packet-sized inputs (128-1400 bytes) and up to 60× faster on large, repetitive inputs.
- `Decompress` may now write to all of `dst`, not just the first `outSize` bytes it returns.

## v0.1.1 (2026-06-24)

Upstream release with CI and release tooling changes only, the library itself did not change.
[Release notes](https://github.com/anchore/go-lzo/releases/tag/v0.1.1)

## v0.1.0 (2025-05-29)

Initial upstream release: LZO1X `Decompress` and `Reader`.
[Release notes](https://github.com/anchore/go-lzo/releases/tag/v0.1.0)
