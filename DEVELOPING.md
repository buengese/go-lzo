# Developing

## Requirements

- Go 1.24+
- liblzo2 and its headers (`liblzo2-dev` on Debian/Ubuntu, `lzo` on Arch Linux and Homebrew) plus a C/C++ toolchain.
  The upstream tests can fall back to Docker instead, the differential tests cannot.

## Tasks

| Command              | What it does                                                                        |
|----------------------|-------------------------------------------------------------------------------------|
| `make test`          | unit tests                                                                          |
| `make test-liblzo2`  | unit tests plus the differential tests against liblzo2                              |
| `make fuzz`          | fuzz the decoder against liblzo2 (`FUZZTIME=2m` by default)                         |
| `make bench`         | packet benchmarks against liblzo2 (narrow down with `BENCH=...`, repeat `COUNT=6`)  |
| `make lint`          | golangci-lint, pinned to the same version as CI                                     |
| `make lint-fix`      | format and apply lint fixes                                                         |

## Tests

Two sets of tests check this implementation against liblzo2, the reference implementation:

- The upstream tests (`reader_test.go`) compress generated data with a small C++ tool around liblzo2. They build it
  to `testdata/bin` with g++, or fall back to a Docker image, and cache fixtures in `testdata/cache`. Inputs of
  failing tests are saved to `testdata/crash`.
- The differential tests (`liblzo2_test.go`, build tag `liblzo2`) call liblzo2 directly through cgo
  (`internal/liblzo2`). They round-trip data through every LZO1X compressor, check OpenVPN-style packet handling
  (the decoder only gets an upper bound for the output size, and must reject trailing, truncated or oversized input
  exactly when liblzo2 does), and fuzz both decoders against each other.

The differential tests use the local Go installation (`net/http` sources, the language spec, the `go` binary) and
synthetic data as input, so no test data needs to be checked in.

One difference is known and accepted: liblzo2 accepts an end-of-stream marker with a match length other than 3, this
decoder rejects it like the Linux kernel's decoder does. No encoder produces such a marker.

To work on the tagged files in your editor, add `-tags=liblzo2` to gopls' `buildFlags`.

## Benchmarks

`BenchmarkPacketDecompress` and `BenchmarkPacketCompress` use packets of 128 to 1400 bytes, compressed the way an
OpenVPN 2.x peer does it: with `lzo1x_1_15`, and only kept if that saves space. liblzo2 processes all packets in a
single cgo call so that cgo overhead does not count against it. Compare results with
[benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat):

```sh
make bench BENCH=PacketDecompress > new.txt
benchstat -col /impl new.txt   # this implementation vs liblzo2
benchstat old.txt new.txt      # before vs after a change
```

## Clean-room rules

The [suite of LZO algorithms](https://github.com/nemequ/lzo/blob/master/doc/LZO.TXT) from the
[original project](http://www.oberhumer.com/opensource/lzo/) is licensed under GPL v2+. This implementation was
derived from the [Linux kernel documentation for the LZO stream format](https://docs.kernel.org/staging/lzo.html) and
the [implementation from the `lzokay` project](https://github.com/AxioDL/lzokay) (MIT licensed).

Any additional features and fixes must be written without referencing the original implementation, any pseudocode
describing it, or any code that is not permissively licensed. In particular, do not read:

- liblzo2 or minilzo sources,
- the Linux kernel's LZO implementation (`lib/lzo/`),
- ports or derivatives of these in other languages, including Go ones.

liblzo2 may only be used as a black box: called through its public API from tests (`internal/liblzo2`), never
imported by library code.
