//go:build liblzo2 && cgo

package lzo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/buengese/go-lzo/internal/liblzo2"
)

var updateGolden = flag.Bool("update-golden", false, "regenerate the golden files in "+goldenDir+" with liblzo2")

// TestLiblzo2UpdateGolden regenerates the golden files that TestGolden decompresses: excerpts of the test corpora,
// compressed with every liblzo2 compressor. Run it with: make golden
func TestLiblzo2UpdateGolden(t *testing.T) {
	if !*updateGolden {
		t.Skip("run with -update-golden to regenerate " + goldenDir)
	}

	// Only synthetic corpora, so that no third-party content is checked in. Excerpts are large enough for all
	// instructions to show up, M4 (16-48kB back) in the larger ones.
	excerpts := map[string]int{"words": 16 << 10, "records": 16 << 10, "backrefs": 48 << 10, "zeros": 4 << 10,
		"random": 1 << 10, "farmatches": 8 << 10}
	inputs := map[string][]byte{}
	for name, size := range excerpts {
		inputs[fmt.Sprintf("%s-%d", name, size)] = corpusByName(t, name)[:size]
	}
	words := corpusByName(t, "words")
	for _, size := range []int{0, 1, 3, 17, 1400} {
		inputs[fmt.Sprintf("words-%d", size)] = words[:size]
	}

	if err := os.RemoveAll(goldenDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(goldenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var files []goldenFile
	for name, in := range inputs {
		for _, m := range liblzo2.Methods {
			stream, err := liblzo2.Compress(m, in)
			if err != nil {
				t.Fatal(err)
			}
			f := goldenFile{Name: fmt.Sprintf("%s.%s.lzo", name, m), Size: len(in)}
			sum := sha256.Sum256(in)
			f.SHA256 = hex.EncodeToString(sum[:])
			if err := os.WriteFile(filepath.Join(goldenDir, f.Name), stream, 0o644); err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
		}
	}
	slices.SortFunc(files, func(a, b goldenFile) int { return strings.Compare(a.Name, b.Name) })
	manifest, err := json.MarshalIndent(files, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goldenDir, "manifest.json"), append(manifest, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
