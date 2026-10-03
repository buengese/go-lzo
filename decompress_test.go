package lzo

import (
	"errors"
	"testing"
)

func TestDecompressEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		src    []byte
		dstLen int
		want   []byte
		err    error
	}{
		{name: "empty", src: nil, dstLen: 16, err: ErrInputOverrun},
		{name: "too short", src: []byte{0x00, 0x01}, dstLen: 16, err: ErrInputOverrun},
		{name: "unterminated literal run", src: []byte{0x00, 0x00, 0x00}, dstLen: 16, err: ErrInputOverrun},
		{name: "end of stream only", src: []byte{0x11, 0x00, 0x00}, dstLen: 0, want: []byte{}},
		{name: "first byte literals", src: []byte{0x12, 'a', 0x11, 0x00, 0x00}, dstLen: 1, want: []byte("a")},
		{name: "no room for literals", src: []byte{0x12, 'a', 0x11, 0x00, 0x00}, dstLen: 0, err: ErrOutputOverrun},
		{name: "match before start", src: []byte{0x12, 'a', 0x40, 0x01, 0x11, 0x00, 0x00}, dstLen: 16,
			err: ErrLookbehindOverrun},
		{name: "end of stream with length 4", src: []byte{0x12, 'a', 0x12, 0x00, 0x00}, dstLen: 16,
			err: ErrDecompressionFailed},
		{name: "trailing byte", src: []byte{0x11, 0x00, 0x00, 0x00}, dstLen: 16, err: ErrInputNotConsumed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Decompress(make([]byte, tt.dstLen), tt.src)
			if !errors.Is(err, tt.err) {
				t.Fatalf("got error %v, want %v", err, tt.err)
			}
			if tt.err == nil && string(out) != string(tt.want) {
				t.Fatalf("got %q, want %q", out, tt.want)
			}
			if tt.err != nil && out != nil {
				t.Fatalf("got output %q with an error", out)
			}
		})
	}
}
