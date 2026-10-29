package anvil

import (
	"encoding/binary"
	"errors"
	"math/bits"
)

// LZ4 support for region compression type 4, which wraps data in lz4-java's
// LZ4BlockOutputStream framing: per block "LZ4Block", token, compressed
// length, original length, xxhash32 checksum (all LE), then the payload.

const (
	lz4Magic      = "LZ4Block"
	lz4HeaderLen  = 8 + 1 + 4 + 4 + 4
	lz4MethodRaw  = 0x10
	lz4MethodLZ4  = 0x20
	lz4BlockSize  = 64 << 10
	lz4Seed       = 0x9747b28c
	lz4MinMatch   = 4
	lz4LastLits   = 5
	lz4MFLimit    = 12
	lz4HashLog    = 14
	lz4MaxOffset  = 65535
	lz4MaxBlock   = 32 << 20
	lz4ChecksumMk = 0x0FFFFFFF
)

var errLZ4 = errors.New("anvil: corrupt lz4 data")

func lz4BlockDecode(dst, src []byte, size int) ([]byte, error) {
	start := len(dst)
	for i := 0; i < len(src); {
		tok := src[i]
		i++
		lit := int(tok >> 4)
		if lit == 15 {
			for {
				if i >= len(src) {
					return nil, errLZ4
				}
				b := src[i]
				i++
				lit += int(b)
				if b != 255 {
					break
				}
			}
		}
		if i+lit > len(src) || len(dst)-start+lit > size {
			return nil, errLZ4
		}
		dst = append(dst, src[i:i+lit]...)
		i += lit
		if i == len(src) {
			break
		}
		if i+2 > len(src) {
			return nil, errLZ4
		}
		off := int(src[i]) | int(src[i+1])<<8
		i += 2
		ml := int(tok & 15)
		if ml == 15 {
			for {
				if i >= len(src) {
					return nil, errLZ4
				}
				b := src[i]
				i++
				ml += int(b)
				if b != 255 {
					break
				}
			}
		}
		ml += lz4MinMatch
		pos := len(dst) - off
		if off == 0 || pos < start || len(dst)-start+ml > size {
			return nil, errLZ4
		}
		for k := 0; k < ml; k++ {
			dst = append(dst, dst[pos+k])
		}
	}
	if len(dst)-start != size {
		return nil, errLZ4
	}
	return dst, nil
}

func lz4AppendLen(dst []byte, n int) []byte {
	for n >= 255 {
		dst = append(dst, 255)
		n -= 255
	}
	return append(dst, byte(n))
}

func lz4Sequence(dst, lits []byte, off, ml int) []byte {
	tok := byte(0)
	if len(lits) >= 15 {
		tok = 15 << 4
	} else {
		tok = byte(len(lits)) << 4
	}
	if off > 0 {
		m := ml - lz4MinMatch
		if m >= 15 {
			tok |= 15
		} else {
			tok |= byte(m)
		}
	}
	dst = append(dst, tok)
	if len(lits) >= 15 {
		dst = lz4AppendLen(dst, len(lits)-15)
	}
	dst = append(dst, lits...)
	if off > 0 {
		dst = append(dst, byte(off), byte(off>>8))
		if m := ml - lz4MinMatch; m >= 15 {
			dst = lz4AppendLen(dst, m-15)
		}
	}
	return dst
}

// lz4BlockEncode is a greedy single-probe compressor producing valid LZ4 blocks.
func lz4BlockEncode(dst, src []byte) []byte {
	var table [1 << lz4HashLog]int32
	anchor := 0
	if len(src) >= lz4MFLimit+1 {
		limit := len(src) - lz4MFLimit
		for i := 0; i < limit; {
			v := binary.LittleEndian.Uint32(src[i:])
			h := (v * 2654435761) >> (32 - lz4HashLog)
			cand := int(table[h]) - 1
			table[h] = int32(i + 1)
			if cand < 0 || i-cand > lz4MaxOffset || binary.LittleEndian.Uint32(src[cand:]) != v {
				i++
				continue
			}
			ml := lz4MinMatch
			maxML := len(src) - lz4LastLits - i
			for ml < maxML && src[cand+ml] == src[i+ml] {
				ml++
			}
			dst = lz4Sequence(dst, src[anchor:i], i-cand, ml)
			i += ml
			anchor = i
		}
	}
	return lz4Sequence(dst, src[anchor:], 0, 0)
}

func lz4Decompress(src []byte) ([]byte, error) {
	var out []byte
	for len(src) > 0 {
		if len(src) < lz4HeaderLen || string(src[:8]) != lz4Magic {
			return nil, errLZ4
		}
		tok := src[8]
		clen := int(binary.LittleEndian.Uint32(src[9:]))
		olen := int(binary.LittleEndian.Uint32(src[13:]))
		sum := binary.LittleEndian.Uint32(src[17:])
		src = src[lz4HeaderLen:]
		if clen == 0 && olen == 0 {
			break
		}
		if clen < 0 || clen > len(src) || olen < 0 || olen > lz4MaxBlock {
			return nil, errLZ4
		}
		start := len(out)
		var err error
		switch tok & 0xF0 {
		case lz4MethodRaw:
			if clen != olen {
				return nil, errLZ4
			}
			out = append(out, src[:clen]...)
		case lz4MethodLZ4:
			if out, err = lz4BlockDecode(out, src[:clen], olen); err != nil {
				return nil, err
			}
		default:
			return nil, errLZ4
		}
		if xxh32(out[start:], lz4Seed)&lz4ChecksumMk != sum {
			return nil, errors.New("anvil: lz4 checksum mismatch")
		}
		src = src[clen:]
	}
	return out, nil
}

func lz4Compress(dst, src []byte) []byte {
	level := byte(max(0, bits.Len(lz4BlockSize-1)-10))
	for len(src) > 0 {
		n := min(len(src), lz4BlockSize)
		blk := src[:n]
		src = src[n:]
		hdr := len(dst)
		dst = append(dst, lz4Magic...)
		dst = append(dst, make([]byte, 13)...)
		body := len(dst)
		dst = lz4BlockEncode(dst, blk)
		method := byte(lz4MethodLZ4)
		if len(dst)-body >= n {
			dst = append(dst[:body], blk...)
			method = lz4MethodRaw
		}
		dst[hdr+8] = method | level
		binary.LittleEndian.PutUint32(dst[hdr+9:], uint32(len(dst)-body))
		binary.LittleEndian.PutUint32(dst[hdr+13:], uint32(n))
		binary.LittleEndian.PutUint32(dst[hdr+17:], xxh32(blk, lz4Seed)&lz4ChecksumMk)
	}
	dst = append(dst, lz4Magic...)
	dst = append(dst, lz4MethodRaw|level)
	return append(dst, make([]byte, 12)...)
}

const (
	xxP1 uint32 = 2654435761
	xxP2 uint32 = 2246822519
	xxP3 uint32 = 3266489917
	xxP4 uint32 = 668265263
	xxP5 uint32 = 374761393
)

func xxRound(acc, in uint32) uint32 {
	return bits.RotateLeft32(acc+in*xxP2, 13) * xxP1
}

func xxh32(b []byte, seed uint32) uint32 {
	n := len(b)
	var h uint32
	if n >= 16 {
		v1, v2, v3, v4 := seed+xxP1+xxP2, seed+xxP2, seed, seed-xxP1
		for len(b) >= 16 {
			v1 = xxRound(v1, binary.LittleEndian.Uint32(b))
			v2 = xxRound(v2, binary.LittleEndian.Uint32(b[4:]))
			v3 = xxRound(v3, binary.LittleEndian.Uint32(b[8:]))
			v4 = xxRound(v4, binary.LittleEndian.Uint32(b[12:]))
			b = b[16:]
		}
		h = bits.RotateLeft32(v1, 1) + bits.RotateLeft32(v2, 7) + bits.RotateLeft32(v3, 12) + bits.RotateLeft32(v4, 18)
	} else {
		h = seed + xxP5
	}
	h += uint32(n)
	for len(b) >= 4 {
		h = bits.RotateLeft32(h+binary.LittleEndian.Uint32(b)*xxP3, 17) * xxP4
		b = b[4:]
	}
	for _, c := range b {
		h = bits.RotateLeft32(h+uint32(c)*xxP5, 11) * xxP1
	}
	h ^= h >> 15
	h *= xxP2
	h ^= h >> 13
	h *= xxP3
	h ^= h >> 16
	return h
}
