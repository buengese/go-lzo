//go:build liblzo2 && cgo

package lzo

// Differential tests against liblzo2, the reference implementation, used strictly as a black box.
// Run with: go test -tags liblzo2 ./...

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/buengese/go-lzo/internal/liblzo2"
)

// packetSizes covers OpenVPN-style packets: OpenVPN only compresses packets of at least 100 bytes and most
// traffic is close to the MTU.
var packetSizes = []int{128, 256, 512, 1024, 1400}

// maxPacketOut mirrors how OpenVPN decompresses: the output buffer has the size of the largest possible
// payload, the decoder never learns the exact uncompressed length.
const maxPacketOut = 1 << 16

type corpus struct {
	name string
	data []byte
}

// loadCorpora returns deterministic test inputs: text, HTML and a binary from the local Go installation (so
// nothing needs to be vendored), plus synthetic data. GOROOT inputs that cannot be found are skipped.
var loadCorpora = sync.OnceValue(func() []corpus {
	var cs []corpus
	if out, err := exec.Command("go", "env", "GOROOT").Output(); err == nil {
		goroot := strings.TrimSpace(string(out))
		if text := readGoSources(filepath.Join(goroot, "src", "net", "http")); len(text) > 0 {
			cs = append(cs, corpus{"text", text})
		}
		if html, err := os.ReadFile(filepath.Join(goroot, "doc", "go_spec.html")); err == nil {
			cs = append(cs, corpus{"html", html})
		}
		if bin, err := os.ReadFile(filepath.Join(goroot, "bin", "go")); err == nil {
			cs = append(cs, corpus{"binary", bin[:min(len(bin), 4<<20)]})
		}
	}

	rng := rand.New(rand.NewPCG(1, 2))
	random := make([]byte, 1<<20)
	for i := range random {
		random[i] = byte(rng.Uint32())
	}
	cs = append(cs,
		corpus{"random", random},
		corpus{"zeros", make([]byte, 256<<10)},
		corpus{"backrefs", syntheticBackrefs(rng, 1<<20)},
	)
	return cs
})

// readGoSources concatenates the non-test Go files of a directory, capped at 4MB.
func readGoSources(dir string) []byte {
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	var buf bytes.Buffer
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		buf.Write(data)
		if buf.Len() >= 4<<20 {
			break
		}
	}
	return buf.Bytes()
}

// syntheticBackrefs mixes random runs with copies of earlier data at distances up to 64KB, so that every
// match type, including the 16-48KB M4 matches and overlapping copies, shows up in compressed output.
func syntheticBackrefs(rng *rand.Rand, size int) []byte {
	out := make([]byte, 0, size+4096)
	for len(out) < size {
		if len(out) == 0 || rng.IntN(3) == 0 {
			for range rng.IntN(64) + 1 {
				out = append(out, byte(rng.Uint32()))
			}
			continue
		}
		dist := rng.IntN(min(len(out), 64<<10)) + 1
		start := len(out) - dist
		for i := range rng.IntN(300) + 2 {
			out = append(out, out[start+i])
		}
	}
	return out[:size]
}

func corpusByName(tb testing.TB, name string) []byte {
	tb.Helper()
	for _, c := range loadCorpora() {
		if c.name == name {
			return c.data
		}
	}
	tb.Skipf("corpus %q not available", name)
	return nil
}

// packets cuts n packets of the given size out of data at deterministic offsets.
func packets(data []byte, size, n int) [][]byte {
	if len(data) < size {
		return nil
	}
	rng := rand.New(rand.NewPCG(uint64(size), uint64(n)))
	ps := make([][]byte, n)
	for i := range ps {
		off := rng.IntN(len(data) - size + 1)
		ps[i] = data[off : off+size]
	}
	return ps
}

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
		inputs := [][]byte{c.data[:min(len(c.data), 1<<20)]}
		for n := range 24 {
			inputs = append(inputs, c.data[:min(len(c.data), n)])
		}
		for _, size := range packetSizes {
			inputs = append(inputs, packets(c.data, size, 32)...)
		}

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
