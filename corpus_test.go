package lzo

// Test inputs shared by the tests of this package.

import (
	"bytes"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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

// roundTripInputs returns the inputs round trip tests use from a corpus: its first megabyte, all prefixes of up to
// 23 bytes, and packets of every packet size.
func roundTripInputs(data []byte) [][]byte {
	inputs := [][]byte{data[:min(len(data), 1<<20)]}
	for n := range 24 {
		inputs = append(inputs, data[:min(len(data), n)])
	}
	for _, size := range packetSizes {
		inputs = append(inputs, packets(data, size, 32)...)
	}
	return inputs
}
