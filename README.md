# go-lzo

> **This is a hard fork of [anchore/go-lzo](https://github.com/anchore/go-lzo).** It is developed independently of the
> original library and is not affiliated with Anchore.

This repository provides a pure Go implementation of LZO1X compression and decompression, the LZO variant used by
OpenVPN and many file formats. The streams it produces can be decompressed by liblzo2, the reference implementation,
and vice versa.

The decoder is derived from the [Linux kernel documentation for the LZO stream format](https://docs.kernel.org/staging/lzo.html)
and the [implementation from `lzokay` project](https://github.com/AxioDL/lzokay) (MIT licensed). The encoder is based on
the same format documentation, and its match finder follows the design of the pure Go encoders in
[klauspost/compress](https://github.com/klauspost/compress).

```go
compressed := lzo.Compress(nil, data)

// LZO1X does not record the size of the decompressed data, dst only needs to be large enough.
decompressed, err := lzo.Decompress(make([]byte, maxSize), compressed)
```

For many small blocks, like network packets, reuse a `Compressor` and the destination buffers to compress without
allocations.

To use this library:

```bash
go get github.com/buengese/go-lzo
```

See [DEVELOPING.md](DEVELOPING.md) for tests, benchmarks and the clean-room rules contributions must follow.
