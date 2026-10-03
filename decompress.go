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

const max255Count = (^uint(0))/255 - 2

var (
	ErrLookbehindOverrun   = errors.New("lzo: lookbehind overrun")
	ErrOutputOverrun       = errors.New("lzo: output overrun")
	ErrInputOverrun        = errors.New("lzo: input overrun")
	ErrDecompressionFailed = errors.New("lzo: error during decompression")
	ErrInputNotConsumed    = errors.New("lzo: input not fully consumed")
)

// Decompress the given LZO1X compressed data to the destination buffer.
//
// dstBytes only needs to be large enough to hold the decompressed data. Decompress may use all of dstBytes as
// scratch space, only the first outSize bytes hold the result.
//
// Here's a summary of the state machine:
//
//	Instruction    Bits        Description
//	-------------- ------------ -------------------------------------------------
//	M1 (short)     0x00..0x0F  copy 2-3 bytes based on previous literal state
//	M1 (long)      0x00..0x0F  when state==0, long literal run (>=4 bytes)
//	M2             0x40..0xFF  copy 3-8 bytes within 2kB distance
//	M3             0x20..0x3F  copy small block within 16kB distance
//	M4             0x10..0x1F  copy block within 16..48kB, end-of-stream if distance==16384
//
//nolint:funlen,gocognit,gocyclo // a single flat loop keeps the decoder state in registers
func Decompress(srcBytes, dstBytes []byte) (outSize int, err error) {
	src, dst := srcBytes, dstBytes
	if len(src) < 3 {
		return 0, ErrInputOverrun
	}

	// s and d are the positions in src and dst. state is the number of literals copied by the last instruction
	// (4 meaning 4 or more), it decides how instructions 0..15 are interpreted.
	var s, d, state int

	if src[0] >= 18 {
		/* 18..21 : copy 1..4 literals
		 *          state = (byte - 17)
		 * 22..255 : copy literal string
		 *           length = (byte - 17) = 5..238
		 *           state = 4 [ don't copy extra literals ]
		 */
		n := int(src[0]) - 17
		s = 1
		if n > len(src)-s {
			return 0, ErrInputOverrun
		}
		if n > len(dst) {
			return 0, ErrOutputOverrun
		}
		copy(dst[:n], src[s:s+n])
		s += n
		d = n
		state = min(n, 4)
	}
	/* 0..17 : follow regular instruction encoding, see below. It is worth
	 *         noting that codes 16 and 17 will represent a block copy from
	 *         the dictionary which is empty, and that they will always be
	 *         invalid at this place.
	 */

	for {
		if s >= len(src) {
			return d, ErrInputOverrun
		}
		inst := int(src[s])
		s++

		// every instruction except a long literal run copies length bytes from dist bytes back, followed by
		// next (0..3) literals
		var dist, length, next int
		switch {
		case inst >= 64:
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
			if s >= len(src) {
				return d, ErrInputOverrun
			}
			dist = int(src[s])<<3 + (inst>>2)&7 + 1
			s++
			length = inst>>5 + 1
			next = inst & 3

		case inst >= 32:
			/* [M3]
			 * 0 0 1 L L L L L  (32..63)
			 *   Copy of small block within 16kB distance (preferably less than 34B)
			 *   length = 2 + (L ?: 31 + (zero_bytes * 255) + non_zero_byte)
			 * Always followed by exactly one LE16 :  D D D D D D D D : D D D D D D S S
			 *   distance = D + 1
			 *   state = S (copy S literals after this block)
			 */
			length = inst&31 + 2
			if length == 2 {
				if length, s, err = extendedLength(src, s, 2+31); err != nil {
					return d, err
				}
			}
			if len(src)-s < 2 {
				return d, ErrInputOverrun
			}
			v := int(binary.LittleEndian.Uint16(src[s:]))
			s += 2
			dist = v>>2 + 1
			next = v & 3

		case inst >= 16:
			/* [M4]
			 * 0 0 0 1 H L L L  (16..31)
			 *   Copy of a block within 16..48kB distance (preferably less than 10B)
			 *   length = 2 + (L ?: 7 + (zero_bytes * 255) + non_zero_byte)
			 * Always followed by exactly one LE16 :  D D D D D D D D : D D D D D D S S
			 *   distance = 16384 + (H << 14) + D
			 *   state = S (copy S literals after this block)
			 *   End of stream is reached if distance == 16384
			 */
			length = inst&7 + 2
			if length == 2 {
				if length, s, err = extendedLength(src, s, 2+7); err != nil {
					return d, err
				}
			}
			if len(src)-s < 2 {
				return d, ErrInputOverrun
			}
			v := int(binary.LittleEndian.Uint16(src[s:]))
			s += 2
			dist = (inst&8)<<11 + v>>2
			next = v & 3
			if dist == 0 {
				/* stream finished, the terminating M4 must be a 3 byte copy */
				switch {
				case length != 3:
					return d, ErrDecompressionFailed
				case s < len(src):
					return d, ErrInputNotConsumed
				}
				return d, nil
			}
			dist += 16384

		case state == 0:
			/* If last instruction did not copy any literal (state == 0), this
			 * encoding will be a copy of 4 or more literal, and must be interpreted
			 * like this :
			 *
			 *    0 0 0 0 L L L L  (0..15)  : copy long literal string
			 *    length = 3 + (L ?: 15 + (zero_bytes * 255) + non_zero_byte)
			 *    state = 4  (no extra literals are copied)
			 */
			n := inst + 3
			if n == 3 {
				if n, s, err = extendedLength(src, s, 3+15); err != nil {
					return d, err
				}
			}
			if n > len(src)-s {
				return d, ErrInputOverrun
			}
			if n > len(dst)-d {
				return d, ErrOutputOverrun
			}
			if n <= 16 && len(src)-s >= 16 && len(dst)-d >= 16 {
				// copy whole words, the bytes past the literals are overwritten later
				to, from := dst[d:d+16], src[s:s+16]
				binary.LittleEndian.PutUint64(to, binary.LittleEndian.Uint64(from))
				binary.LittleEndian.PutUint64(to[8:], binary.LittleEndian.Uint64(from[8:]))
			} else {
				copy(dst[d:d+n], src[s:s+n])
			}
			s += n
			d += n
			state = 4
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
			if s >= len(src) {
				return d, ErrInputOverrun
			}
			dist = int(src[s])<<2 + inst>>2 + 1
			s++
			length = 2
			if state == 4 {
				dist += 2048
				length = 3
			}
			next = inst & 3
		}

		if dist > d {
			return d, ErrLookbehindOverrun
		}
		if next > len(src)-s {
			return d, ErrInputOverrun
		}
		if length+next > len(dst)-d {
			return d, ErrOutputOverrun
		}

		m := d - dist
		switch {
		case dist >= 8 && length <= 16 && len(dst)-d >= 16:
			// short matches are copied as 8 byte words. The source of every word lies completely before its
			// destination, so this works even if the match overlaps itself. Bytes written past the match are
			// overwritten later.
			to, from := dst[d:d+16], dst[m:m+16]
			binary.LittleEndian.PutUint64(to, binary.LittleEndian.Uint64(from))
			if length > 8 {
				binary.LittleEndian.PutUint64(to[8:], binary.LittleEndian.Uint64(from[8:]))
			}
			d += length
		case dist >= length:
			copy(dst[d:d+length], dst[m:m+length])
			d += length
		default:
			// the match overlaps itself and repeats the last dist bytes: copy everything available so far,
			// doubling the copied amount every round
			for end := d + length; d < end; {
				d += copy(dst[d:end], dst[m:d])
			}
		}

		if next > 0 {
			if len(src)-s >= 4 && len(dst)-d >= 4 {
				// copy a whole word, the bytes past the literals are overwritten later
				binary.LittleEndian.PutUint32(dst[d:d+4], binary.LittleEndian.Uint32(src[s:s+4]))
			} else {
				copy(dst[d:d+next], src[s:s+next])
			}
			d += next
			s += next
		}
		state = next
	}
}

// extendedLength decodes a length that did not fit into its instruction: base plus 255 for every zero byte,
// plus the first non-zero byte. It returns the length and the new position in src.
func extendedLength(src []byte, s, base int) (length, pos int, err error) {
	start := s
	for s < len(src) && src[s] == 0 {
		s++
	}
	count := s - start
	if uint(count) > max255Count {
		return 0, s, ErrDecompressionFailed
	}
	if s >= len(src) {
		return 0, s, ErrInputOverrun
	}
	return base + count*255 + int(src[s]), s + 1, nil
}
