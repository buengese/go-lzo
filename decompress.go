/*
Package lzo is a direct implementation of the LZO decompression algorithm in Go using the following sources as references:
  - https://docs.kernel.org/staging/lzo.html (linux kernel documentation)
  - https://github.com/AxioDL/lzokay/blob/db2df1fcbebc2ed06c10f727f72567d40f06a2be/lzokay.cpp (lzokay C++ implementation, MIT licensed)
*/
package lzo

import (
	"encoding/binary"
	"errors"
)

var (
	ErrLookbehindOverrun   = errors.New("lzo: lookbehind overrun")
	ErrOutputOverrun       = errors.New("lzo: output overrun")
	ErrInputOverrun        = errors.New("lzo: input overrun")
	ErrDecompressionFailed = errors.New("lzo: error during decompression")
	ErrInputNotConsumed    = errors.New("lzo: input not fully consumed")
)

const (
	// lower bounds of the opcode ranges, see the table in the documentation of Decompress
	m2Marker = 0x40
	m3Marker = 0x20
	m4Marker = 0x10

	// a first byte above firstLiteralBias starts the stream with (first byte - firstLiteralBias) literals
	firstLiteralBias = 17

	// the state after copying 4 or more literals
	stateManyLiterals = 4

	// M1 matches after 4 or more literals start this much further back than the ones after 1 to 3 literals
	m1FarDistance = 2048

	// M4 matches start 16kB back, a distance of exactly 16kB marks the end of the stream
	m4Distance = 16384

	// the shortest stream is the 3 byte end of stream marker
	minStreamLen = 3

	// every zero byte of an extended length adds 255, more zero bytes than this would overflow the length
	maxZeroBytes = (^uint(0))/255 - 2
)

// Decompress decompresses the LZO1X stream in src into dst and returns the decompressed data, a prefix of dst.
//
// LZO1X streams do not record the size of the decompressed data: dst only needs to be large enough to hold it.
// Decompress may use all of dst as scratch space.
//
// An LZO1X stream is a sequence of instructions, each starting with an opcode byte. Most instructions copy a
// match, bytes that were decompressed before, followed by 0 to 3 literals, bytes taken verbatim from the stream.
// How opcodes 0x00..0x0f are read depends on the decoder's state, the number of literals copied by the previous
// instruction:
//
//	Opcode      State  Instruction
//	----------  -----  ------------------------------------------------------
//	0x00..0x0f  0      literal run: copy 4 or more literals
//	0x00..0x0f  1..3   M1: copy 2 bytes from up to 1kB back
//	0x00..0x0f  4+     M1: copy 3 bytes from 2..3kB back
//	0x10..0x1f  any    M4: copy from 16..48kB back, or end of stream
//	0x20..0x3f  any    M3: copy from up to 16kB back
//	0x40..0xff  any    M2: copy 3-8 bytes from up to 2kB back
//
// The first byte of a stream can also start it with a literal run, and every stream ends with the M4 instruction
// 0x11 0x00 0x00.
//
//nolint:funlen,gocognit,gocyclo // a single flat loop keeps the decoder state in registers
func Decompress(dst, src []byte) (out []byte, err error) {
	if len(src) < minStreamLen {
		return nil, ErrInputOverrun
	}

	// inPos and outPos are the read position in src and the write position in dst
	var inPos, outPos, state int

	if src[0] > firstLiteralBias {
		/* 18..21 : copy 1..4 literals
		 *          state = (byte - 17)
		 * 22..255 : copy literal string
		 *           length = (byte - 17) = 5..238
		 *           state = 4 [ don't copy extra literals ]
		 */
		litLen := int(src[0]) - firstLiteralBias
		inPos = 1
		if litLen > len(src)-inPos {
			return nil, ErrInputOverrun
		}
		if litLen > len(dst) {
			return nil, ErrOutputOverrun
		}
		copy(dst[:litLen], src[inPos:inPos+litLen])
		inPos += litLen
		outPos = litLen
		state = min(litLen, stateManyLiterals)
	}
	/* 0..17 : follow regular instruction encoding, see below. It is worth
	 *         noting that codes 16 and 17 will represent a block copy from
	 *         the dictionary which is empty, and that they will always be
	 *         invalid at this place.
	 */

	for {
		// Decode the next instruction into a match of matchLen bytes starting matchDist bytes back, followed by
		// litLen literals. Literal runs and the end of the stream are handled completely in their cases.
		if inPos >= len(src) {
			return nil, ErrInputOverrun
		}
		opcode := int(src[inPos])
		inPos++

		var matchDist, matchLen, litLen int
		switch {
		case opcode >= m2Marker:
			/* [M2]
			 * 1 L L D D D S S  (128..255)
			 *   Copy 5-8 bytes from block within 2kB distance
			 *   state = S (copy S literals after this block)
			 *   length = 5 + L
			 * Always followed by exactly one byte : H H H H H H H H
			 *   distance = (H << 3) + D + 1
			 *
			 * 0 1 L D D D S S  (64..127)
			 *   Copy 3-4 bytes from block within 2kB distance
			 *   state = S (copy S literals after this block)
			 *   length = 3 + L
			 * Always followed by exactly one byte : H H H H H H H H
			 *   distance = (H << 3) + D + 1
			 */
			if inPos >= len(src) {
				return nil, ErrInputOverrun
			}
			matchDist = int(src[inPos])<<3 + (opcode>>2)&7 + 1
			inPos++
			matchLen = opcode>>5 + 1
			litLen = opcode & 3

		case opcode >= m3Marker:
			/* [M3]
			 * 0 0 1 L L L L L  (32..63)
			 *   Copy of small block within 16kB distance (preferably less than 34B)
			 *   length = 2 + (L ?: 31 + (zero_bytes * 255) + non_zero_byte)
			 * Always followed by exactly one LE16 :  D D D D D D D D : D D D D D D S S
			 *   distance = D + 1
			 *   state = S (copy S literals after this block)
			 */
			matchLen = opcode&31 + 2
			if matchLen == 2 {
				if matchLen, inPos, err = extendedLength(src, inPos, 2+31); err != nil {
					return nil, err
				}
			}
			if len(src)-inPos < 2 {
				return nil, ErrInputOverrun
			}
			operand := int(binary.LittleEndian.Uint16(src[inPos:]))
			inPos += 2
			matchDist = operand>>2 + 1
			litLen = operand & 3

		case opcode >= m4Marker:
			/* [M4]
			 * 0 0 0 1 H L L L  (16..31)
			 *   Copy of a block within 16..48kB distance (preferably less than 10B)
			 *   length = 2 + (L ?: 7 + (zero_bytes * 255) + non_zero_byte)
			 * Always followed by exactly one LE16 :  D D D D D D D D : D D D D D D S S
			 *   distance = 16384 + (H << 14) + D
			 *   state = S (copy S literals after this block)
			 *   End of stream is reached if distance == 16384
			 */
			matchLen = opcode&7 + 2
			if matchLen == 2 {
				if matchLen, inPos, err = extendedLength(src, inPos, 2+7); err != nil {
					return nil, err
				}
			}
			if len(src)-inPos < 2 {
				return nil, ErrInputOverrun
			}
			operand := int(binary.LittleEndian.Uint16(src[inPos:]))
			inPos += 2
			matchDist = m4Distance + (opcode&8)<<11 + operand>>2
			litLen = operand & 3
			if matchDist == m4Distance {
				/* end of stream, which is always encoded as a 3 byte copy */
				switch {
				case matchLen != 3:
					return nil, ErrDecompressionFailed
				case inPos < len(src):
					return nil, ErrInputNotConsumed
				}
				return dst[:outPos], nil
			}

		case state == 0:
			/* If last instruction did not copy any literal (state == 0), this
			 * encoding will be a copy of 4 or more literal, and must be interpreted
			 * like this :
			 *
			 *    0 0 0 0 L L L L  (0..15)  : copy long literal string
			 *    length = 3 + (L ?: 15 + (zero_bytes * 255) + non_zero_byte)
			 *    state = 4  (no extra literals are copied)
			 */
			litLen = opcode + 3
			if litLen == 3 {
				if litLen, inPos, err = extendedLength(src, inPos, 3+15); err != nil {
					return nil, err
				}
			}
			if litLen > len(src)-inPos {
				return nil, ErrInputOverrun
			}
			if litLen > len(dst)-outPos {
				return nil, ErrOutputOverrun
			}
			if litLen <= 16 && len(src)-inPos >= 16 && len(dst)-outPos >= 16 {
				copy16(dst[outPos:outPos+16], src[inPos:inPos+16])
			} else {
				copy(dst[outPos:outPos+litLen], src[inPos:inPos+litLen])
			}
			inPos += litLen
			outPos += litLen
			state = stateManyLiterals
			continue

		default:
			/* If last instruction used to copy between 1 to 3 literals (encoded in
			 * the instruction's opcode or distance), the instruction is a copy of a
			 * 2-byte block from the dictionary within a 1kB distance. It is worth
			 * noting that this instruction provides little savings since it uses 2
			 * bytes to encode a copy of 2 other bytes but it encodes the number of
			 * following literals for free. It must be interpreted like this :
			 *
			 *    0 0 0 0 D D S S  (0..15)  : copy 2 bytes from <= 1kB distance
			 *    length = 2
			 *    state = S (copy S literals after this block)
			 *  Always followed by exactly one byte : H H H H H H H H
			 *    distance = (H << 2) + D + 1
			 *
			 * If last instruction used to copy 4 or more literals (as detected by
			 * state == 4), the instruction becomes a copy of a 3-byte block from the
			 * dictionary from a 2..3kB distance, and must be interpreted like this :
			 *
			 *    0 0 0 0 D D S S  (0..15)  : copy 3 bytes from 2..3 kB distance
			 *    length = 3
			 *    state = S (copy S literals after this block)
			 *  Always followed by exactly one byte : H H H H H H H H
			 *    distance = (H << 2) + D + 2049
			 */
			if inPos >= len(src) {
				return nil, ErrInputOverrun
			}
			matchDist = int(src[inPos])<<2 + opcode>>2 + 1
			inPos++
			matchLen = 2
			if state == stateManyLiterals {
				matchDist += m1FarDistance
				matchLen = 3
			}
			litLen = opcode & 3
		}

		// The match must start within the output written so far, and the literals must be present in src. Both
		// must fit into dst.
		if matchDist > outPos {
			return nil, ErrLookbehindOverrun
		}
		if litLen > len(src)-inPos {
			return nil, ErrInputOverrun
		}
		if matchLen+litLen > len(dst)-outPos {
			return nil, ErrOutputOverrun
		}

		// If the match is longer than its distance, it overlaps the bytes it is writing: it repeats the last
		// matchDist bytes, as a front to back byte by byte copy would. The built-in copy() does not do that.
		matchPos := outPos - matchDist
		switch {
		case matchDist >= 8 && matchLen <= 16 && len(dst)-outPos >= 16:
			// short match, copied as 8 byte words. Reading each word only after the previous one was written is
			// enough to repeat bytes correctly if they are at least 8 bytes back.
			to, from := dst[outPos:outPos+16], dst[matchPos:matchPos+16]
			copy8(to, from)
			if matchLen > 8 {
				copy8(to[8:], from[8:])
			}
			outPos += matchLen
		case matchDist >= matchLen:
			copy(dst[outPos:outPos+matchLen], dst[matchPos:matchPos+matchLen])
			outPos += matchLen
		default:
			// overlapping match: copy everything available so far, doubling the copied amount every round
			for end := outPos + matchLen; outPos < end; {
				outPos += copy(dst[outPos:end], dst[matchPos:outPos])
			}
		}

		if litLen > 0 {
			if len(src)-inPos >= 4 && len(dst)-outPos >= 4 {
				copy4(dst[outPos:outPos+4], src[inPos:inPos+4])
			} else {
				copy(dst[outPos:outPos+litLen], src[inPos:inPos+litLen])
			}
			inPos += litLen
			outPos += litLen
		}
		state = litLen
	}
}

// The following helpers copy short runs as whole words. Callers use them for runs shorter than a helper's size
// when both slices have room for the whole size: the bytes written past the end of the run are overwritten by
// what is decompressed next, or lie past the end of the decompressed data.

// copy16 copies 16 bytes as two 8 byte words.
func copy16(to, from []byte) {
	copy8(to, from)
	copy8(to[8:], from[8:])
}

// copy8 copies 8 bytes as one word.
func copy8(to, from []byte) {
	binary.LittleEndian.PutUint64(to, binary.LittleEndian.Uint64(from))
}

// copy4 copies 4 bytes as one word.
func copy4(to, from []byte) {
	binary.LittleEndian.PutUint32(to, binary.LittleEndian.Uint32(from))
}

// extendedLength decodes a length that did not fit into its instruction: base plus 255 for every zero byte,
// plus the first non-zero byte. It returns the length and the new read position in src.
func extendedLength(src []byte, inPos, base int) (length, newPos int, err error) {
	start := inPos
	for inPos < len(src) && src[inPos] == 0 {
		inPos++
	}
	zeroBytes := inPos - start
	if uint(zeroBytes) > maxZeroBytes {
		return 0, inPos, ErrDecompressionFailed
	}
	if inPos >= len(src) {
		return 0, inPos, ErrInputOverrun
	}
	return base + zeroBytes*255 + int(src[inPos]), inPos + 1, nil
}
