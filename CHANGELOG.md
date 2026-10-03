# Changelog

This library is a hard fork of [anchore/go-lzo](https://github.com/anchore/go-lzo). Releases up to v0.1.1 are
upstream releases.

## Unreleased

### Changed

- The module path is now `github.com/buengese/go-lzo`.
- `Decompress` copies literal and match runs longer than 8 bytes with `copy()`. This makes it about 1.2× faster on
  1400 byte packets and up to 2× faster on 128 byte packets.

## v0.1.1 (2026-06-24)

Upstream release with CI and release tooling changes only, the library itself did not change.
[Release notes](https://github.com/anchore/go-lzo/releases/tag/v0.1.1)

## v0.1.0 (2025-05-29)

Initial upstream release: LZO1X `Decompress` and `Reader`.
[Release notes](https://github.com/anchore/go-lzo/releases/tag/v0.1.0)
