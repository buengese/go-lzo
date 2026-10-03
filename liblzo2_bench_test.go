//go:build liblzo2 && cgo

package lzo

// Packet benchmarks against liblzo2. liblzo2 processes all packets in a single cgo call so call overhead stays
// out of its numbers. Compare implementations with: benchstat -col /impl results.txt

import (
	"fmt"
	"testing"

	"github.com/buengese/go-lzo/internal/liblzo2"
)

const benchPackets = 256

func BenchmarkPacketDecompress(b *testing.B) {
	for _, name := range []string{"text", "html", "binary"} {
		data := corpusByName(b, name)
		for _, size := range packetSizes {
			compressed, originals := compressedPackets(b, data, size, benchPackets)
			raw := 0
			for _, p := range originals {
				raw += len(p)
			}
			prefix := fmt.Sprintf("corpus=%s/size=%d", name, size)

			b.Run(prefix+"/impl=go", func(b *testing.B) {
				dst := make([]byte, maxPacketOut)
				b.SetBytes(int64(raw))
				b.ReportAllocs()
				for b.Loop() {
					for _, c := range compressed {
						if _, err := Decompress(dst, c); err != nil {
							b.Fatal(err)
						}
					}
				}
			})

			b.Run(prefix+"/impl=liblzo2", func(b *testing.B) {
				batch := liblzo2.NewBatch(compressed, maxPacketOut)
				b.SetBytes(int64(raw))
				for b.Loop() {
					if _, err := batch.Decompress(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkPacketCompress compares with lzo1x_1_15, which OpenVPN uses, and reports the size of the output as
// a ratio of the input. Random input matters as much as compressible input: OpenVPN tries to compress every
// packet, and most traffic is already encrypted.
func BenchmarkPacketCompress(b *testing.B) {
	for _, name := range []string{"text", "binary", "random"} {
		data := corpusByName(b, name)
		for _, size := range packetSizes {
			ps := packets(data, size, benchPackets)
			raw := 0
			for _, p := range ps {
				raw += len(p)
			}
			prefix := fmt.Sprintf("corpus=%s/size=%d", name, size)

			b.Run(prefix+"/impl=go", func(b *testing.B) {
				var c Compressor
				dst := make([]byte, MaxCompressedLen(size))
				compressed := 0
				b.SetBytes(int64(raw))
				b.ReportAllocs()
				for b.Loop() {
					compressed = 0
					for _, p := range ps {
						compressed += len(c.Compress(dst, p))
					}
				}
				b.ReportMetric(float64(compressed)/float64(raw), "ratio")
			})

			b.Run(prefix+"/impl=liblzo2", func(b *testing.B) {
				batch := liblzo2.NewBatch(ps, maxPacketOut)
				compressed := 0
				b.SetBytes(int64(raw))
				for b.Loop() {
					var err error
					if compressed, err = batch.Compress(liblzo2.LZO1X1_15); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(compressed)/float64(raw), "ratio")
			})
		}
	}
}

// BenchmarkBlockDecompress decompresses single large blocks, compressed with lzo1x_1_15, where long and
// overlapping matches matter more than for packets.
func BenchmarkBlockDecompress(b *testing.B) {
	for _, name := range []string{"text", "binary", "backrefs", "zeros"} {
		data := corpusByName(b, name)
		for _, size := range []int{16 << 10, 64 << 10, 256 << 10} {
			if size > len(data) {
				continue
			}
			block := data[:size]
			compressed, err := liblzo2.Compress(liblzo2.LZO1X1_15, block)
			if err != nil {
				b.Fatal(err)
			}
			prefix := fmt.Sprintf("corpus=%s/size=%d", name, size)

			b.Run(prefix+"/impl=go", func(b *testing.B) {
				dst := make([]byte, size)
				b.SetBytes(int64(size))
				b.ReportAllocs()
				for b.Loop() {
					if _, err := Decompress(dst, compressed); err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run(prefix+"/impl=liblzo2", func(b *testing.B) {
				batch := liblzo2.NewBatch([][]byte{compressed}, size)
				b.SetBytes(int64(size))
				for b.Loop() {
					if _, err := batch.Decompress(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
