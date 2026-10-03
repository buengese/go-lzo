// Package lzo implements LZO1X compression and decompression in pure Go.
//
// LZO1X is the LZO variant used by OpenVPN, lzop and squashfs, among others. Compress produces streams that
// liblzo2, the reference implementation, decompresses, and Decompress reads the streams of liblzo2's LZO1X
// compressors (lzo1x_1, lzo1x_1_15, lzo1x_999, ...).
//
// An LZO1X stream does not record the size of the data it decompresses to. Protocols and file formats that use it
// store that size themselves, or know an upper bound for it.
//
// The decoder was written from these references:
//   - https://docs.kernel.org/staging/lzo.html (Linux kernel documentation)
//   - https://github.com/AxioDL/lzokay/blob/db2df1fcbebc2ed06c10f727f72567d40f06a2be/lzokay.cpp (lzokay C++
//     implementation, MIT licensed)
//
// The encoder follows the same format documentation, its match finder follows the design of the pure Go encoders
// in github.com/klauspost/compress.
package lzo
