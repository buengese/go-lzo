# go-lzo

> **This is a hard fork of [anchore/go-lzo](https://github.com/anchore/go-lzo).** It is developed independently of the
> original library and is not affiliated with Anchore.

This repository provides an implementation of the LZO1X decompression algorithm in Go. 
The implementation is derived from the [Linux kernel documentation for the LZO stream format](https://docs.kernel.org/staging/lzo.html) and the [implementation from `lzokay` project](https://github.com/AxioDL/lzokay) (MIT licensed). It includes a `Reader` for streaming decompressed data and a standalone `Decompress` function for use with byte slices.

> **Note:** `Reader` currently assumes the whole compressed stream arrives in a single read of at most
> its block size (64KB by default). Prefer `Decompress` for anything else.

To use this library:

```bash
go get github.com/buengese/go-lzo
```
