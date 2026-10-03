package lzo

import (
	"encoding/binary"
	"math"
	"math/bits"
	"sync"
)

const (
	// Matches are found through a hash table that maps 4 byte sequences to the position they were last seen at.
	minMatchLen   = 4
	hashTableBits = 14
	hashTableSize = 1 << hashTableBits

	// While no match is found, the search skips ahead faster the longer the pending literal run gets: one more byte
	// per 2^skipLog literals. Incompressible input is passed over quickly this way.
	skipLog = 5

	// the largest distances and the longest lengths the match instructions encode in their opcode and operand
	m2MaxDist = 2048
	m2MaxLen  = 8
	m3MaxDist = 16384
	m3MaxLen  = 2 + 31
	m4MaxDist = m4Distance + 0x7fff
	m4MaxLen  = 2 + 7

	// the longest literal runs encoded in the first byte of a stream, and in a literal run opcode
	firstLiteralMaxLen = 255 - firstLiteralBias
	literalRunMaxLen   = 3 + 15
)

// MaxCompressedLen returns the maximum size of the LZO1X stream that Compress produces for srcLen bytes of input.
func MaxCompressedLen(srcLen int) int {
	return srcLen + srcLen/255 + 5
}

var compressorPool = sync.Pool{New: func() any { return new(Compressor) }}

// Compress compresses src into an LZO1X stream. It returns the stream in dst if dst is at least
// MaxCompressedLen(len(src)) bytes long, and in a newly allocated slice otherwise.
//
// The output is never more than a few bytes larger than src, which happens if src does not compress.
//
// Compress is safe for concurrent use, it takes a Compressor from a pool. To compress without allocations, use a
// Compressor directly and pass a large enough dst.
func Compress(dst, src []byte) []byte {
	c := compressorPool.Get().(*Compressor)
	dst = c.Compress(dst, src)
	compressorPool.Put(c)
	return dst
}

// Compressor compresses data into LZO1X streams like Compress, but owns the state needed for that. Compressing
// many small blocks, like network packets, with one Compressor avoids setting that state up again every time.
//
// The zero value is ready to use. A Compressor must not be used concurrently.
type Compressor struct {
	// table maps the hash of 4 bytes of input to the position they were last seen at, plus base. A position is
	// only a hint: the bytes there are compared before a match is used.
	table [hashTableSize]uint32
	// base grows by the size of every compressed block. Entries of earlier blocks then point before the start of
	// the current one and are ignored, so the table never needs to be cleared between blocks.
	base uint32
}

// Compress compresses src into an LZO1X stream, see the package level Compress.
func (c *Compressor) Compress(dst, src []byte) []byte {
	if n := MaxCompressedLen(len(src)); len(dst) < n {
		dst = make([]byte, n)
	}
	if uint64(c.base)+uint64(len(src)) > math.MaxUint32 {
		clear(c.table[:])
		c.base = 0
	}
	n := c.compress(dst, src)
	c.base += uint32(len(src))
	return dst[:n]
}

// compress writes the LZO1X stream for src to dst, which must have room for MaxCompressedLen(len(src)) bytes, and
// returns its size.
//
// The stream alternates literal runs and matches. Every position of src is looked up in the hash table until a
// match of at least 4 bytes is found, which is then extended in both directions. The literals before it and the
// match are emitted, and the search continues after the match.
func (c *Compressor) compress(dst, src []byte) int {
	// outPos is the write position in dst, litStart the start of the literals not emitted yet, and sPos the
	// position of the byte that holds the S bits of the last match, which encode up to 3 literals following it.
	outPos, litStart, sPos := 0, 0, -1

	for inPos := 0; inPos <= len(src)-minMatchLen; {
		seq := binary.LittleEndian.Uint32(src[inPos:])
		h := hash4(seq)
		matchPos := int(c.table[h] - c.base)
		c.table[h] = c.base + uint32(inPos)
		if uint(matchPos) >= uint(inPos) || inPos-matchPos > m4MaxDist ||
			binary.LittleEndian.Uint32(src[matchPos:]) != seq {
			inPos += 1 + (inPos-litStart)>>skipLog
			continue
		}

		// Extend the match backwards into the pending literals, and forwards as far as it goes.
		for matchPos > 0 && inPos > litStart && src[matchPos-1] == src[inPos-1] {
			matchPos--
			inPos--
		}
		matchEnd := inPos + minMatchLen + matchLen(src[inPos+minMatchLen:], src[matchPos+minMatchLen:])

		// A match takes fewer bytes than it covers, but a literal run can take a few more. Once the output would
		// grow larger than src, compressing does not pay off.
		litLen := inPos - litStart
		if outPos+litLen+litLen/255+2+(matchEnd-inPos) > len(src) {
			return emitUncompressed(dst, src)
		}
		outPos = emitLiterals(dst, outPos, sPos, src[litStart:inPos])
		outPos, sPos = emitMatch(dst, outPos, inPos-matchPos, matchEnd-inPos)

		inPos, litStart = matchEnd, matchEnd
	}

	litLen := len(src) - litStart
	if outPos+literalHeaderLen(outPos, litLen)+litLen+3 > uncompressedLen(len(src)) {
		return emitUncompressed(dst, src)
	}
	outPos = emitLiterals(dst, outPos, sPos, src[litStart:])
	return emitEndOfStream(dst, outPos)
}

// hash4 hashes 4 bytes of input to an index into the hash table.
func hash4(seq uint32) uint32 {
	return (seq * 2654435761) >> (32 - hashTableBits)
}

// matchLen returns the number of equal bytes at the start of a and b, which must not be shorter than a.
func matchLen(a, b []byte) int {
	n := 0
	for len(a) >= 8 {
		if diff := binary.LittleEndian.Uint64(a) ^ binary.LittleEndian.Uint64(b); diff != 0 {
			return n + bits.TrailingZeros64(diff)>>3
		}
		a, b = a[8:], b[8:]
		n += 8
	}
	for i := range a {
		if a[i] != b[i] {
			return n + i
		}
	}
	return n + len(a)
}

// emitLiterals writes a literal run to dst at outPos and returns the new write position. At the start of the
// stream the run's length goes into the first byte. After a match, runs of 1 to 3 literals go into the match's S
// bits at sPos, longer runs get their own instruction.
func emitLiterals(dst []byte, outPos, sPos int, lits []byte) int {
	n := len(lits)
	switch {
	case n == 0:
		return outPos
	case outPos == 0 && n <= firstLiteralMaxLen:
		dst[0] = byte(firstLiteralBias + n)
		outPos = 1
	case n <= 3:
		dst[sPos] |= byte(n)
	case n <= literalRunMaxLen:
		/* 0 0 0 0 L L L L : copy long literal string, length = 3 + L */
		dst[outPos] = byte(n - 3)
		outPos++
	default:
		/* 0 0 0 0 0 0 0 0 : copy long literal string, length = 18 + (zero_bytes * 255) + non_zero_byte */
		dst[outPos] = 0
		outPos = emitExtendedLength(dst, outPos+1, n-literalRunMaxLen)
	}
	return outPos + copy(dst[outPos:], lits)
}

// literalHeaderLen returns how many bytes emitLiterals writes in addition to the n literals themselves.
func literalHeaderLen(outPos, n int) int {
	switch {
	case n == 0:
		return 0
	case outPos == 0 && n <= firstLiteralMaxLen:
		return 1
	case n <= 3:
		return 0
	case n <= literalRunMaxLen:
		return 1
	}
	return 2 + (n-literalRunMaxLen-1)/255
}

// emitMatch writes a match of length bytes from dist bytes back to dst at outPos. It returns the new write
// position and the position of the byte holding the match's S bits, which are left 0.
func emitMatch(dst []byte, outPos, dist, length int) (newPos, sPos int) {
	switch {
	case dist <= m2MaxDist && length <= m2MaxLen:
		/* [M2] L L L D D D S S, H H H H H H H H : length = 1 + L, distance = (H << 3) + D + 1 */
		dist--
		dst[outPos] = byte((length-1)<<5 | (dist&7)<<2)
		dst[outPos+1] = byte(dist >> 3)
		return outPos + 2, outPos

	case dist <= m3MaxDist:
		/* [M3] 0 0 1 L L L L L, LE16 D...D S S : length = 2 + (L ?: 31 + zero_bytes * 255 + non_zero_byte)
		 *                                        distance = D + 1
		 */
		dist--
		if length <= m3MaxLen {
			dst[outPos] = byte(m3Marker | (length - 2))
			outPos++
		} else {
			dst[outPos] = m3Marker
			outPos = emitExtendedLength(dst, outPos+1, length-m3MaxLen)
		}

	default:
		/* [M4] 0 0 0 1 H L L L, LE16 D...D S S : length = 2 + (L ?: 7 + zero_bytes * 255 + non_zero_byte)
		 *                                        distance = 16384 + (H << 14) + D
		 */
		dist -= m4Distance
		high := (dist >> 14) << 3
		if length <= m4MaxLen {
			dst[outPos] = byte(m4Marker | high | (length - 2))
			outPos++
		} else {
			dst[outPos] = byte(m4Marker | high)
			outPos = emitExtendedLength(dst, outPos+1, length-m4MaxLen)
		}
		dist &= 0x3fff
	}
	dst[outPos] = byte(dist << 2)
	dst[outPos+1] = byte(dist >> 6)
	return outPos + 2, outPos
}

// emitExtendedLength writes the part n > 0 of a length that does not fit into its opcode: a zero byte for every 255,
// then the remainder.
func emitExtendedLength(dst []byte, outPos, n int) int {
	for n > 255 {
		dst[outPos] = 0
		outPos++
		n -= 255
	}
	dst[outPos] = byte(n)
	return outPos + 1
}

// emitEndOfStream writes the end of stream marker, an M4 match with a distance of exactly 16kB.
func emitEndOfStream(dst []byte, outPos int) int {
	dst[outPos] = m4Marker | 1
	dst[outPos+1] = 0
	dst[outPos+2] = 0
	return outPos + 3
}

// emitUncompressed writes src as a single literal run.
func emitUncompressed(dst, src []byte) int {
	return emitEndOfStream(dst, emitLiterals(dst, 0, -1, src))
}

// uncompressedLen returns the size of the stream emitUncompressed writes for n bytes.
func uncompressedLen(n int) int {
	return literalHeaderLen(0, n) + n + 3
}
