//go:build liblzo2 && cgo

package lzo

// Differential tests against liblzo2, the reference implementation, used strictly as a black box.
// Run with: go test -tags liblzo2 ./...

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/buengese/go-lzo/internal/liblzo2"
)

// compressedPackets returns packets the way an OpenVPN 2.x peer sends them: compressed with lzo1x_1_15,
// and only when that actually saves space. It also returns the original packets that were kept.
func compressedPackets(tb testing.TB, data []byte, size, n int) (compressed, originals [][]byte) {
	tb.Helper()
	for _, p := range packets(data, size, n) {
		c, err := liblzo2.Compress(liblzo2.LZO1X1_15, p)
		if err != nil {
			tb.Fatal(err)
		}
		if len(c) < len(p) {
			compressed = append(compressed, c)
			originals = append(originals, p)
		}
	}
	return compressed, originals
}

func TestLiblzo2RoundTrip(t *testing.T) {
	for _, c := range loadCorpora() {
		inputs := roundTripInputs(c.data)
		for _, m := range liblzo2.Methods {
			t.Run(fmt.Sprintf("%s/%s", c.name, m), func(t *testing.T) {
				for _, in := range inputs {
					compressed, err := liblzo2.Compress(m, in)
					if err != nil {
						t.Fatal(err)
					}
					// both with the exact output size and with only an upper bound
					for _, dstLen := range []int{len(in), len(in) + 64} {
						dst := make([]byte, dstLen)
						out, err := Decompress(dst, compressed)
						if err != nil {
							t.Fatalf("%d bytes, dst %d: %v", len(in), dstLen, err)
						}
						if !bytes.Equal(out, in) {
							t.Fatalf("%d bytes, dst %d: output mismatch", len(in), dstLen)
						}
					}
				}
			})
		}
	}
}

// TestLiblzo2PacketErrors checks the ways OpenVPN can see a broken packet: a packet must only decode if
// liblzo2 would accept it too, otherwise OpenVPN peers drop packets we accept or vice versa.
func TestLiblzo2PacketErrors(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(compressed []byte, orig []byte) (in []byte, maxOut int)
	}{
		{"trailing byte", func(c, o []byte) ([]byte, int) { return append(bytes.Clone(c), 0), maxPacketOut }},
		{"truncated", func(c, o []byte) ([]byte, int) { return c[:len(c)-1], maxPacketOut }},
		{"output too small", func(c, o []byte) ([]byte, int) { return c, len(o) - 1 }},
	}
	for _, c := range loadCorpora() {
		compressed, originals := compressedPackets(t, c.data, 512, 64)
		for _, mut := range mutations {
			t.Run(c.name+"/"+mut.name, func(t *testing.T) {
				for i := range compressed {
					in, maxOut := mut.mutate(compressed[i], originals[i])
					_, refErr := liblzo2.Decompress(in, maxOut)
					_, err := Decompress(make([]byte, maxOut), in)
					if refErr == nil || err == nil {
						t.Fatalf("packet %d: liblzo2 err=%v, go err=%v; both must reject", i, refErr, err)
					}
				}
			})
		}
	}
}

// FuzzLiblzo2Differential feeds arbitrary input to both decoders: they must agree on whether it decodes,
// and on the output when it does.
func FuzzLiblzo2Differential(f *testing.F) {
	for _, c := range loadCorpora() {
		for _, size := range []int{128, 1400} {
			compressed, _ := compressedPackets(f, c.data, size, 4)
			for _, p := range compressed {
				f.Add(p)
			}
		}
	}
	f.Add([]byte{0x11, 0x00, 0x00})

	const maxOut = 1 << 12
	f.Fuzz(func(t *testing.T, in []byte) {
		ref, refErr := liblzo2.Decompress(in, maxOut)
		out, err := Decompress(make([]byte, maxOut), in)

		// Known difference: liblzo2 accepts an end-of-stream marker with a match length other than 3, we
		// reject it (like the Linux kernel's decoder). No encoder produces such a marker.
		if refErr == nil && errors.Is(err, ErrDecompressionFailed) {
			return
		}
		if (refErr == nil) != (err == nil) {
			t.Fatalf("liblzo2 err=%v, go err=%v for input %x", refErr, err, in)
		}
		if err == nil && !bytes.Equal(out, ref) {
			t.Fatalf("output mismatch for input %x", in)
		}
	})
}

// TestLiblzo2DecodesEncodings checks every instruction encoding the encoder can produce against liblzo2.
func TestLiblzo2DecodesEncodings(t *testing.T) {
	for _, tc := range encodingCases() {
		got, err := liblzo2.Decompress(tc.stream, len(tc.want))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !bytes.Equal(got, tc.want) {
			t.Fatalf("%s: decompressed data differs", tc.name)
		}
	}
}

// TestLiblzo2DecodesCompress checks that liblzo2, which OpenVPN peers use, decodes what Compress produces.
func TestLiblzo2DecodesCompress(t *testing.T) {
	var c Compressor
	for _, cs := range loadCorpora() {
		t.Run(cs.name, func(t *testing.T) {
			for _, in := range roundTripInputs(cs.data) {
				got, err := liblzo2.Decompress(c.Compress(nil, in), len(in))
				if err != nil {
					t.Fatalf("%d bytes: %v", len(in), err)
				}
				if !bytes.Equal(got, in) {
					t.Fatalf("%d bytes: decompressed data differs", len(in))
				}
			}
		})
	}
}

// TestLiblzo2CompressionRatio logs how well Compress compresses packets compared to lzo1x_1_15, which OpenVPN
// uses.
func TestLiblzo2CompressionRatio(t *testing.T) {
	var c Compressor
	for _, name := range []string{"text", "html", "binary"} {
		data := corpusByName(t, name)
		for _, size := range packetSizes {
			raw, ours, ref := 0, 0, 0
			for _, p := range packets(data, size, 256) {
				out, err := liblzo2.Compress(liblzo2.LZO1X1_15, p)
				if err != nil {
					t.Fatal(err)
				}
				raw += len(p)
				ref += len(out)
				ours += len(c.Compress(nil, p))
			}
			t.Logf("%-6s %4d bytes: go %.1f%%, lzo1x_1_15 %.1f%%", name, size,
				100*float64(ours)/float64(raw), 100*float64(ref)/float64(raw))
		}
	}
}

// FuzzLiblzo2Compress checks that liblzo2 decodes what Compress produces for arbitrary input.
func FuzzLiblzo2Compress(f *testing.F) {
	for _, cs := range loadCorpora() {
		for _, p := range packets(cs.data, 256, 2) {
			f.Add(p)
		}
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		got, err := liblzo2.Decompress(Compress(nil, in), len(in))
		if err != nil {
			t.Fatalf("%d bytes: %v", len(in), err)
		}
		if !bytes.Equal(got, in) {
			t.Fatalf("%d bytes: decompressed data differs", len(in))
		}
	})
}
