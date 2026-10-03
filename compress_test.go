package lzo

import (
	"bytes"
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
)

// checkRoundTrip compresses in with c, checks the size of the result and that it decompresses to in.
func checkRoundTrip(tb testing.TB, c *Compressor, in []byte) []byte {
	tb.Helper()
	out := c.Compress(nil, in)
	if len(out) > uncompressedLen(len(in)) || len(out) > MaxCompressedLen(len(in)) {
		tb.Fatalf("%d bytes compressed to %d bytes, more than uncompressed (%d)", len(in), len(out), uncompressedLen(len(in)))
	}
	got, err := Decompress(make([]byte, len(in)), out)
	if err != nil {
		tb.Fatalf("%d bytes: %v", len(in), err)
	}
	if !bytes.Equal(got, in) {
		tb.Fatalf("%d bytes: decompressed data differs", len(in))
	}
	return out
}

func TestCompressRoundTrip(t *testing.T) {
	var c Compressor
	for _, cs := range loadCorpora() {
		t.Run(cs.name, func(t *testing.T) {
			for _, in := range roundTripInputs(cs.data) {
				checkRoundTrip(t, &c, in)
			}
		})
	}
}

// TestCompressorReuse compresses many blocks with one Compressor: matches must never refer to earlier blocks.
func TestCompressorReuse(t *testing.T) {
	var c Compressor
	rng := rand.New(rand.NewPCG(7, 8))
	text := corpusByName(t, "text")
	for i := range 2000 {
		size := 1 + rng.IntN(2000)
		if i%2 == 0 {
			checkRoundTrip(t, &c, randomBytes(rng, size))
		} else {
			off := rng.IntN(len(text) - size)
			checkRoundTrip(t, &c, text[off:off+size])
		}
	}
}

// TestCompressorBaseOverflow checks that the hash table is reset before positions would overflow.
func TestCompressorBaseOverflow(t *testing.T) {
	var c Compressor
	text := corpusByName(t, "text")
	checkRoundTrip(t, &c, text[:4096])
	c.base = math.MaxUint32 - 1000
	checkRoundTrip(t, &c, text[4096:8192])
	if c.base != 4096 {
		t.Fatalf("base is %d after a reset and one 4096 byte block, want 4096", c.base)
	}
	checkRoundTrip(t, &c, text[:4096])
}

func TestCompressDst(t *testing.T) {
	in := corpusByName(t, "text")[:1400]

	dst := make([]byte, MaxCompressedLen(len(in)))
	if out := Compress(dst, in); &out[0] != &dst[0] {
		t.Error("Compress did not use a large enough dst")
	}
	small := make([]byte, 16)
	if out := Compress(small, in); &out[0] == &small[0] {
		t.Error("Compress wrote to a dst smaller than MaxCompressedLen")
	}
}

type encodingCase struct {
	name         string
	stream, want []byte
}

// encodingCases builds streams of a literal run, a match and trailing literals with emitLiterals and emitMatch
// directly, at the limits of every length and distance encoding. The match finder only hits some of these.
func encodingCases() []encodingCase {
	rng := rand.New(rand.NewPCG(5, 6))
	var cases []encodingCase
	add := func(name string, firstLits, dist, length, trailingLits int) {
		want := randomBytes(rng, firstLits)
		for range length {
			want = append(want, want[len(want)-dist])
		}
		trailing := randomBytes(rng, trailingLits)
		want = append(want, trailing...)

		dst := make([]byte, MaxCompressedLen(len(want)))
		outPos := emitLiterals(dst, 0, -1, want[:firstLits])
		outPos, sPos := emitMatch(dst, outPos, dist, length)
		outPos = emitLiterals(dst, outPos, sPos, trailing)
		outPos = emitEndOfStream(dst, outPos)
		cases = append(cases, encodingCase{name, dst[:outPos], want})
	}

	for _, dist := range []int{1, 2, 7, 8, 2047, 2048, 2049, 16383, 16384, 16385, 32768, 32769, 49151} {
		for _, length := range []int{4, 5, 8, 9, 10, 33, 34, 264, 265, 288, 289, 1000} {
			for _, trailing := range []int{0, 1, 3, 4} {
				add(fmt.Sprintf("dist=%d/len=%d/trailing=%d", dist, length, trailing), dist, dist, length, trailing)
			}
		}
	}
	for _, n := range []int{1, 2, 3, 4, 5, 17, 18, 19, 20, 237, 238, 239, 272, 273, 274, 528, 529, 530, 1000} {
		add(fmt.Sprintf("first=%d", n), n, 1, 4, 0)
		add(fmt.Sprintf("trailing=%d", n), 16, 16, 4, n)
	}
	return cases
}

func TestEncodings(t *testing.T) {
	for _, tc := range encodingCases() {
		got, err := Decompress(make([]byte, len(tc.want)), tc.stream)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !bytes.Equal(got, tc.want) {
			t.Fatalf("%s: decompressed data differs", tc.name)
		}
	}
}

func FuzzCompress(f *testing.F) {
	for _, cs := range loadCorpora() {
		for _, p := range packets(cs.data, 256, 2) {
			f.Add(p)
		}
	}
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, in []byte) {
		var c Compressor
		checkRoundTrip(t, &c, in)
	})
}
