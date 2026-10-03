# go-lzo

> **This is a hard fork of [anchore/go-lzo](https://github.com/anchore/go-lzo).** It is developed independently of the
> original library and is not affiliated with Anchore.

This repository provides an implementation of the LZO1X decompression algorithm in Go. 
The implementation is derived from the [Linux kernel documentation for the LZO stream format](https://docs.kernel.org/staging/lzo.html) and the [implementation from `lzokay` project](https://github.com/AxioDL/lzokay) (MIT licensed). `Decompress` decompresses a single LZO1X block from one byte slice into another.

To use this library:

```bash
go get github.com/buengese/go-lzo
```

See [DEVELOPING.md](DEVELOPING.md) for tests, benchmarks and the clean-room rules contributions must follow.
