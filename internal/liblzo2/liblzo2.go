//go:build liblzo2 && cgo

package liblzo2

/*
#cgo LDFLAGS: -llzo2
#include <lzo/lzo1x.h>

static int lzo2_init(void) { return lzo_init(); }

static size_t lzo2_wrkmem_size(void) {
	size_t m = LZO1X_1_MEM_COMPRESS;
	if (LZO1X_1_15_MEM_COMPRESS > m) m = LZO1X_1_15_MEM_COMPRESS;
	if (LZO1X_999_MEM_COMPRESS > m) m = LZO1X_999_MEM_COMPRESS;
	return m;
}

static int lzo2_compress(int method, const unsigned char *src, lzo_uint src_len,
                         unsigned char *dst, lzo_uint *dst_len, void *wrkmem) {
	switch (method) {
	case 0: return lzo1x_1_compress(src, src_len, dst, dst_len, wrkmem);
	case 1: return lzo1x_1_15_compress(src, src_len, dst, dst_len, wrkmem);
	case 2: return lzo1x_999_compress(src, src_len, dst, dst_len, wrkmem);
	}
	return LZO_E_ERROR;
}

static int lzo2_decompress(const unsigned char *src, lzo_uint src_len, unsigned char *dst, lzo_uint *dst_len) {
	return lzo1x_decompress_safe(src, src_len, dst, dst_len, NULL);
}

// lzo2_*_batch process many packets laid out back to back in one call, keeping cgo call overhead out of
// benchmarks. They return the total output size, or a negative liblzo2 error code.
static long lzo2_decompress_batch(const unsigned char *src, const lzo_uint *lens, int n,
                                  unsigned char *dst, lzo_uint dst_cap) {
	long total = 0;
	for (int i = 0; i < n; i++) {
		lzo_uint dst_len = dst_cap;
		int r = lzo1x_decompress_safe(src, lens[i], dst, &dst_len, NULL);
		if (r != LZO_E_OK) return r;
		src += lens[i];
		total += dst_len;
	}
	return total;
}

static long lzo2_compress_batch(int method, const unsigned char *src, const lzo_uint *lens, int n,
                                unsigned char *dst, void *wrkmem) {
	long total = 0;
	for (int i = 0; i < n; i++) {
		lzo_uint dst_len = 0;
		int r = lzo2_compress(method, src, lens[i], dst, &dst_len, wrkmem);
		if (r != LZO_E_OK) return r;
		src += lens[i];
		total += dst_len;
	}
	return total;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Method selects a liblzo2 LZO1X compressor. All of them produce the same LZO1X bitstream format.
type Method int

const (
	// LZO1X1 is lzo1x_1_compress.
	LZO1X1 Method = iota
	// LZO1X1_15 is lzo1x_1_15_compress, which OpenVPN uses.
	LZO1X1_15
	// LZO1X999 is lzo1x_999_compress, the slow high-ratio compressor.
	LZO1X999
)

func (m Method) String() string {
	switch m {
	case LZO1X1:
		return "1x_1"
	case LZO1X1_15:
		return "1x_1_15"
	case LZO1X999:
		return "1x_999"
	}
	return fmt.Sprintf("Method(%d)", int(m))
}

// Methods lists all supported compressors.
var Methods = []Method{LZO1X1, LZO1X1_15, LZO1X999}

// Error is a liblzo2 result code (LZO_E_*).
type Error int

func (e Error) Error() string {
	return fmt.Sprintf("liblzo2: error %d", int(e))
}

func init() {
	if r := C.lzo2_init(); r != C.LZO_E_OK {
		panic(fmt.Sprintf("liblzo2: lzo_init failed: %d", int(r)))
	}
}

// MaxCompressedLen is the worst-case output size of the LZO1X compressors for n input bytes.
func MaxCompressedLen(n int) int {
	return n + n/16 + 64 + 3
}

// Compress compresses src with the given liblzo2 compressor.
func Compress(m Method, src []byte) ([]byte, error) {
	dst := make([]byte, MaxCompressedLen(len(src)))
	wrk := make([]byte, C.lzo2_wrkmem_size())
	var dstLen C.lzo_uint
	r := C.lzo2_compress(C.int(m), ptr(src), C.lzo_uint(len(src)), ptr(dst), &dstLen, unsafe.Pointer(&wrk[0]))
	if r != C.LZO_E_OK {
		return nil, Error(r)
	}
	return dst[:dstLen], nil
}

// Decompress decodes src with lzo1x_decompress_safe into a buffer of maxOut bytes. Like OpenVPN, it only
// passes an upper bound for the output size, not the exact uncompressed length.
func Decompress(src []byte, maxOut int) ([]byte, error) {
	dst := make([]byte, max(maxOut, 1))
	dstLen := C.lzo_uint(maxOut)
	r := C.lzo2_decompress(ptr(src), C.lzo_uint(len(src)), ptr(dst), &dstLen)
	if r != C.LZO_E_OK {
		return nil, Error(r)
	}
	return dst[:dstLen], nil
}

// Batch holds packets back to back so a benchmark can process all of them with a single cgo call.
type Batch struct {
	src    []byte
	lens   []C.lzo_uint
	maxOut int
	dst    []byte
	wrkmem []byte
}

// NewBatch prepares packets for batch processing. maxOut bounds the decompressed size of any single packet.
func NewBatch(packets [][]byte, maxOut int) *Batch {
	b := &Batch{
		maxOut: maxOut,
		wrkmem: make([]byte, C.lzo2_wrkmem_size()),
	}
	maxLen := 0
	for _, p := range packets {
		b.src = append(b.src, p...)
		b.lens = append(b.lens, C.lzo_uint(len(p)))
		maxLen = max(maxLen, len(p))
	}
	b.dst = make([]byte, max(maxOut, MaxCompressedLen(maxLen), 1))
	return b
}

// Decompress decodes every packet with lzo1x_decompress_safe and returns the total decompressed size.
func (b *Batch) Decompress() (int, error) {
	if len(b.lens) == 0 {
		return 0, nil
	}
	r := C.lzo2_decompress_batch(ptr(b.src), &b.lens[0], C.int(len(b.lens)), ptr(b.dst), C.lzo_uint(b.maxOut))
	if r < 0 {
		return 0, Error(r)
	}
	return int(r), nil
}

// Compress compresses every packet and returns the total compressed size.
func (b *Batch) Compress(m Method) (int, error) {
	if len(b.lens) == 0 {
		return 0, nil
	}
	r := C.lzo2_compress_batch(C.int(m), ptr(b.src), &b.lens[0], C.int(len(b.lens)), ptr(b.dst), unsafe.Pointer(&b.wrkmem[0]))
	if r < 0 {
		return 0, Error(r)
	}
	return int(r), nil
}

// ptr returns a C pointer to the start of b, or to a dummy byte if b is empty.
func ptr(b []byte) *C.uchar {
	if len(b) == 0 {
		var dummy [1]byte
		return (*C.uchar)(unsafe.Pointer(&dummy[0]))
	}
	return (*C.uchar)(unsafe.Pointer(&b[0]))
}
