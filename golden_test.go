package lzo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// goldenDir holds LZO1X streams produced by liblzo2 (see TestLiblzo2UpdateGolden), so that the decoder is tested
// against the reference implementation without needing liblzo2. They also cover the M1 instructions, which this
// package's encoder never produces.
const goldenDir = "testdata/golden"

// goldenFile describes a stream in goldenDir by the size and hash of its decompressed data.
type goldenFile struct {
	Name   string `json:"name"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

func readGoldenManifest(tb testing.TB) []goldenFile {
	tb.Helper()
	data, err := os.ReadFile(filepath.Join(goldenDir, "manifest.json"))
	if err != nil {
		tb.Fatal(err)
	}
	var files []goldenFile
	if err := json.Unmarshal(data, &files); err != nil {
		tb.Fatal(err)
	}
	return files
}

func TestGolden(t *testing.T) {
	files := readGoldenManifest(t)
	if len(files) == 0 {
		t.Fatal("no golden files")
	}
	for _, f := range files {
		t.Run(f.Name, func(t *testing.T) {
			stream, err := os.ReadFile(filepath.Join(goldenDir, f.Name))
			if err != nil {
				t.Fatal(err)
			}
			// both with the exact output size and with only an upper bound
			for _, dstLen := range []int{f.Size, f.Size + 1024} {
				out, err := Decompress(make([]byte, dstLen), stream)
				if err != nil {
					t.Fatalf("dst %d: %v", dstLen, err)
				}
				sum := sha256.Sum256(out)
				if len(out) != f.Size || hex.EncodeToString(sum[:]) != f.SHA256 {
					t.Fatalf("dst %d: decompressed data differs", dstLen)
				}
			}
			// broken streams must be rejected, like OpenVPN drops broken packets
			if f.Size > 0 {
				if _, err := Decompress(make([]byte, f.Size-1), stream); !errors.Is(err, ErrOutputOverrun) {
					t.Fatalf("dst one byte too small: got %v, want %v", err, ErrOutputOverrun)
				}
			}
			if _, err := Decompress(make([]byte, f.Size), append(stream, 0)); !errors.Is(err, ErrInputNotConsumed) {
				t.Fatalf("trailing byte: got %v, want %v", err, ErrInputNotConsumed)
			}
			// truncate at about 256 points spread over the stream, and right before each byte of its end marker
			truncations := []int{len(stream) - 3, len(stream) - 2, len(stream) - 1}
			for n := 0; n < len(stream)-3; n += 1 + len(stream)/256 {
				truncations = append(truncations, n)
			}
			for _, n := range truncations {
				if _, err := Decompress(make([]byte, f.Size), stream[:n]); err == nil {
					t.Fatalf("stream truncated to %d bytes was accepted", n)
				}
			}
		})
	}
}
