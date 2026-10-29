package translate

import (
	"encoding/binary"
	"errors"

	"github.com/levitskyyy/KremenMC/internal/proto"
)

var errPalette = errors.New("translate: bad paletted container")

// container describes one paletted container kind on both sides.
type container struct {
	size             int
	minBits, maxBits uint8
	srcGlobal        uint8
	dstGlobal        uint8
	table            []int32 // value remap; out-of-range values map to fallback
	fallback         int32
}

func (k *container) value(v int32) int32 {
	if v < 0 || int(v) >= len(k.table) {
		return k.fallback
	}
	if t := k.table[v]; t >= 0 {
		return t
	}
	return k.fallback
}

func longsFor(n int, bits uint8) int {
	per := 64 / int(bits)
	return (n + per - 1) / per
}

// rewriteSections translates canonical chunk section data (block count,
// fluid count, block states, biomes per section) into the target format.
func rewriteSections(dst []byte, src []byte, f chunkFormat, blocks, biomes *container, scratch []uint32) ([]byte, []uint32, error) {
	r := proto.Reader{B: src}
	var err error
	for r.Remaining() > 0 {
		cnt, err := r.Raw(4)
		if err != nil {
			return dst, scratch, err
		}
		dst = append(dst, cnt[0], cnt[1])
		if f&fmtFluidCount != 0 {
			dst = append(dst, cnt[2], cnt[3])
		}
		if dst, scratch, err = rewriteContainer(dst, &r, f, blocks, scratch); err != nil {
			return dst, scratch, err
		}
		if dst, scratch, err = rewriteContainer(dst, &r, f, biomes, scratch); err != nil {
			return dst, scratch, err
		}
	}
	return dst, scratch, err
}

// rewriteBiomes translates the biome containers of chunks_biomes data.
func rewriteBiomes(dst []byte, src []byte, f chunkFormat, biomes *container, scratch []uint32) ([]byte, []uint32, error) {
	r := proto.Reader{B: src}
	var err error
	for r.Remaining() > 0 {
		if dst, scratch, err = rewriteContainer(dst, &r, f, biomes, scratch); err != nil {
			return dst, scratch, err
		}
	}
	return dst, scratch, nil
}

func rewriteContainer(dst []byte, r *proto.Reader, f chunkFormat, k *container, scratch []uint32) ([]byte, []uint32, error) {
	bits, err := r.U8()
	if err != nil {
		return dst, scratch, err
	}
	lenPrefix := f&fmtLongArrayLength != 0
	if bits == 0 {
		v, err := r.VarInt()
		if err != nil {
			return dst, scratch, err
		}
		dst = append(dst, 0)
		dst = proto.AppendVarInt(dst, k.value(v))
		if lenPrefix {
			dst = append(dst, 0)
		}
		return dst, scratch, nil
	}
	if bits <= k.maxBits {
		if bits < k.minBits {
			bits = k.minBits
		}
		n, err := r.VarInt()
		if err != nil {
			return dst, scratch, err
		}
		if n < 1 || int(n) > 1<<bits {
			return dst, scratch, errPalette
		}
		dst = append(dst, bits)
		dst = proto.AppendVarInt(dst, n)
		for range n {
			v, err := r.VarInt()
			if err != nil {
				return dst, scratch, err
			}
			dst = proto.AppendVarInt(dst, k.value(v))
		}
		nl := longsFor(k.size, bits)
		raw, err := r.Raw(nl * 8)
		if err != nil {
			return dst, scratch, err
		}
		if lenPrefix {
			dst = proto.AppendVarInt(dst, int32(nl))
		}
		return append(dst, raw...), scratch, nil
	}
	if bits != k.srcGlobal {
		return dst, scratch, errPalette
	}
	raw, err := r.Raw(longsFor(k.size, bits) * 8)
	if err != nil {
		return dst, scratch, err
	}
	if cap(scratch) < k.size {
		scratch = make([]uint32, k.size)
	}
	vals := scratch[:k.size]
	unpack(vals, raw, bits)
	for i, v := range vals {
		vals[i] = uint32(k.value(int32(v)))
	}
	ob := k.dstGlobal
	nl := longsFor(k.size, ob)
	dst = append(dst, ob)
	if lenPrefix {
		dst = proto.AppendVarInt(dst, int32(nl))
	}
	return pack(dst, vals, ob), scratch, nil
}

func unpack(dst []uint32, raw []byte, bits uint8) {
	per := 64 / int(bits)
	mask := uint64(1)<<bits - 1
	for i := range dst {
		l := binary.BigEndian.Uint64(raw[(i/per)*8:])
		dst[i] = uint32(l >> (uint(i%per) * uint(bits)) & mask)
	}
}

func pack(dst []byte, vals []uint32, bits uint8) []byte {
	per := 64 / int(bits)
	for i := 0; i < len(vals); i += per {
		var l uint64
		for j := 0; j < per && i+j < len(vals); j++ {
			l |= uint64(vals[i+j]) << (uint(j) * uint(bits))
		}
		dst = binary.BigEndian.AppendUint64(dst, l)
	}
	return dst
}
