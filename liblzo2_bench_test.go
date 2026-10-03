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

// BenchmarkPacketCompress has no Go implementation to compare yet; it records the target for the encoder.
// Random input matters as much as compressible input: OpenVPN tries to compress every packet, and most
// traffic is already encrypted.
func BenchmarkPacketCompress(b *testing.B) {
	for _, name := range []string{"text", "binary", "random"} {
		data := corpusByName(b, name)
		for _, size := range packetSizes {
			ps := packets(data, size, benchPackets)
			raw := 0
			for _, p := range ps {
				raw += len(p)
			}

			b.Run(fmt.Sprintf("corpus=%s/size=%d/impl=liblzo2", name, size), func(b *testing.B) {
				batch := liblzo2.NewBatch(ps, maxPacketOut)
				b.SetBytes(int64(raw))
				for b.Loop() {
					if _, err := batch.Compress(liblzo2.LZO1X1_15); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
