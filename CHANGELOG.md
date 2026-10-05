# Changelog

This library is a hard fork of [anchore/go-lzo](https://github.com/anchore/go-lzo). Releases up to v0.1.1 are
upstream releases.

## v0.2.0 (2026-10-05)

### Added

- LZO1X compression: `Compress`, `Compressor` and `MaxCompressedLen`. Its output decompresses with liblzo2, and on
  packet-sized inputs it is slightly smaller than that of `lzo1x_1_15`, the compressor OpenVPN uses.

### Changed

- The module path is now `github.com/buengese/go-lzo`.
- `Decompress` was rewritten as a single loop that copies in whole words where possible. It is 2.5-5× faster on
  packet-sized inputs (128-1400 bytes) and up to 60× faster on large, repetitive inputs.
- `Decompress(dst, src []byte) ([]byte, error)` replaces `Decompress(src, dst []byte) (int, error)`, following the
  block APIs of klauspost/compress. It returns the decompressed data, a prefix of `dst`, or `nil` on error.
- `Decompress` may now write to all of `dst`, not just the part it returns.

### Removed

- `Reader`. It treated everything it read at once as a single LZO1X block of at most 64KB, so it failed on larger
  or fragmented input. Use `Decompress` instead.

## v0.1.1 (2026-06-24)

Upstream release with CI and release tooling changes only, the library itself did not change.
[Release notes](https://github.com/anchore/go-lzo/releases/tag/v0.1.1)

## v0.1.0 (2025-05-29)

Initial upstream release: LZO1X `Decompress` and `Reader`.
[Release notes](https://github.com/anchore/go-lzo/releases/tag/v0.1.0)
