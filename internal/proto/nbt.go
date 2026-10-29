package proto

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/nbt"
)

var ErrNBT = errors.New("proto: malformed nbt")

const maxNBTDepth = 512

// RawNBT holds one undecoded network NBT value, root tag type included.
// An empty RawNBT encodes as TAG_End.
type RawNBT []byte

func (n *RawNBT) Encode(w *Writer) {
	if len(*n) == 0 {
		w.U8(nbt.TagEnd)
		return
	}
	w.Raw(*n)
}

func (n *RawNBT) Decode(r *Reader) error {
	l, err := NBTLen(r.B[r.Off:])
	if err != nil {
		return err
	}
	p, _ := r.take(l)
	*n = append((*n)[:0], p...)
	return nil
}

// IsEmpty reports whether n is absent or TAG_End.
func (n RawNBT) IsEmpty() bool { return len(n) == 0 || n[0] == nbt.TagEnd }

// NBTCompound encodes c as network NBT.
func NBTCompound(c nbt.Compound) (RawNBT, error) {
	b, err := nbt.Marshal(nil, "", c, true)
	if err != nil {
		return nil, fmt.Errorf("nbt compound: %w", err)
	}
	return RawNBT(b), nil
}

// Compound decodes n as a compound. TAG_End yields nil.
func (n RawNBT) Compound() (nbt.Compound, error) {
	if n.IsEmpty() {
		return nil, nil
	}
	_, c, _, err := nbt.Unmarshal(n, true)
	return c, err
}

// NBTLen returns the byte length of the network NBT value at the start of b.
func NBTLen(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, ErrShortBuffer
	}
	t := b[0]
	if t == nbt.TagEnd {
		return 1, nil
	}
	n, err := nbtPayloadLen(b, 1, t, 0)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func nbtPayloadLen(b []byte, off int, t byte, depth int) (int, error) {
	if depth > maxNBTDepth {
		return 0, ErrNBT
	}
	need := func(n int) error {
		if n < 0 || off+n > len(b) {
			return ErrShortBuffer
		}
		return nil
	}
	switch t {
	case nbt.TagByte:
		return off + 1, need(1)
	case nbt.TagShort:
		return off + 2, need(2)
	case nbt.TagInt, nbt.TagFloat:
		return off + 4, need(4)
	case nbt.TagLong, nbt.TagDouble:
		return off + 8, need(8)
	case nbt.TagString:
		if err := need(2); err != nil {
			return 0, err
		}
		l := int(binary.BigEndian.Uint16(b[off:]))
		off += 2
		return off + l, need(l)
	case nbt.TagByteArray, nbt.TagIntArray, nbt.TagLongArray:
		if err := need(4); err != nil {
			return 0, err
		}
		l := int64(int32(binary.BigEndian.Uint32(b[off:])))
		off += 4
		size := int64(1)
		if t == nbt.TagIntArray {
			size = 4
		} else if t == nbt.TagLongArray {
			size = 8
		}
		if l < 0 || l*size > int64(len(b)-off) {
			return 0, ErrNBT
		}
		return off + int(l*size), nil
	case nbt.TagList:
		if err := need(5); err != nil {
			return 0, err
		}
		et := b[off]
		l := int(int32(binary.BigEndian.Uint32(b[off+1:])))
		off += 5
		if l < 0 || l > len(b)-off {
			return 0, ErrNBT
		}
		if et == nbt.TagEnd && l > 0 {
			return 0, ErrNBT
		}
		var err error
		for i := 0; i < l; i++ {
			if off, err = nbtPayloadLen(b, off, et, depth+1); err != nil {
				return 0, err
			}
		}
		return off, nil
	case nbt.TagCompound:
		var err error
		for {
			if err = need(1); err != nil {
				return 0, err
			}
			ct := b[off]
			off++
			if ct == nbt.TagEnd {
				return off, nil
			}
			if err = need(2); err != nil {
				return 0, err
			}
			l := int(binary.BigEndian.Uint16(b[off:]))
			off += 2
			if err = need(l); err != nil {
				return 0, err
			}
			off += l
			if off, err = nbtPayloadLen(b, off, ct, depth+1); err != nil {
				return 0, err
			}
		}
	}
	return 0, ErrNBT
}
