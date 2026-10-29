package pdef

import (
	"errors"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/proto"
)

// Hook replaces the default copy of a named type: it reads one value from r
// and writes its replacement to w (w is nil when only skipping).
type Hook func(r *proto.Reader, w *proto.Writer) error

var (
	errCount = errors.New("pdef: bad count")
	errLoop  = errors.New("pdef: unterminated loop")
)

// Walker copies values while running hooks on named types. The zero value
// is ready to use; a Walker is not safe for concurrent use.
type Walker struct {
	Hooks map[string]Hook
	vals  []int64
	bases []int
	w     *proto.Writer
	pend  int
}

var walkers = sync.Pool{New: func() any { return new(Walker) }}

// Skip advances r past one value of n.
func Skip(n *Node, r *proto.Reader) error {
	wk := walkers.Get().(*Walker)
	wk.Hooks = nil
	err := wk.Copy(n, r, nil)
	walkers.Put(wk)
	return err
}

// Copy reads one value of n from r and appends it to w (if not nil), with
// hooked named types (including n itself) replaced by their hook's output.
func (wk *Walker) Copy(n *Node, r *proto.Reader, w *proto.Writer) error {
	wk.w, wk.pend = w, r.Off
	wk.vals, wk.bases = wk.vals[:0], wk.bases[:0]
	if err := wk.walk(n, r, -1); err != nil {
		return err
	}
	if w != nil {
		w.Raw(r.B[wk.pend:r.Off])
	}
	return nil
}

func (wk *Walker) set(slot int, v int64) {
	if slot >= 0 && len(wk.bases) > 0 {
		wk.vals[wk.bases[len(wk.bases)-1]+slot] = v
	}
}

func (wk *Walker) get(rf ref) int64 {
	i := len(wk.bases) - 1 - rf.up
	if i < 0 {
		return 0
	}
	v := wk.vals[wk.bases[i]+rf.slot]
	if rf.mask != 0 {
		if v&rf.mask != 0 {
			return 1
		}
		return 0
	}
	return v
}

func (wk *Walker) walk(n *Node, r *proto.Reader, slot int) error {
	if n.name != "" && wk.Hooks != nil {
		if h := wk.Hooks[n.name]; h != nil {
			if wk.w != nil {
				wk.w.Raw(r.B[wk.pend:r.Off])
			}
			err := h(r, wk.w)
			wk.pend = r.Off
			return err
		}
	}
	switch n.kind {
	case kVoid:
		return nil
	case kFixed:
		b, err := r.Raw(n.size)
		if err != nil {
			return err
		}
		if slot >= 0 && n.size <= 8 {
			var v uint64
			for _, x := range b {
				v = v<<8 | uint64(x)
			}
			sv := int64(v)
			if n.signed {
				sh := uint(64 - 8*n.size)
				sv = int64(v<<sh) >> sh
			}
			wk.set(slot, sv)
		}
		return nil
	case kVarInt:
		v, err := r.VarInt()
		wk.set(slot, int64(v))
		return err
	case kVarLong:
		v, err := r.VarLong()
		wk.set(slot, v)
		return err
	case kString:
		l, err := r.VarInt()
		if err != nil {
			return err
		}
		if l < 0 {
			return errCount
		}
		_, err = r.Raw(int(l))
		return err
	case kBuffer:
		l := n.size
		if l < 0 {
			v, err := r.VarInt()
			if err != nil {
				return err
			}
			if v < 0 {
				return errCount
			}
			l = int(v)
		}
		_, err := r.Raw(l)
		return err
	case kRest:
		r.Off = len(r.B)
		return nil
	case kNBT:
		l, err := proto.NBTLen(r.B[r.Off:])
		if err != nil {
			return err
		}
		r.Off += l
		return nil
	case kLpVec3:
		var v proto.LpVec3
		return v.Decode(r)
	case kOption:
		b, err := r.U8()
		if err != nil {
			return err
		}
		wk.set(slot, int64(b))
		if b != 0 {
			return wk.walk(n.elem, r, -1)
		}
		return nil
	case kArray:
		cnt := int64(n.size)
		switch {
		case n.count != nil:
			cnt = wk.get(*n.count)
		case n.size < 0:
			v, err := r.VarInt()
			if err != nil {
				return err
			}
			cnt = int64(v)
		}
		if cnt < 0 || cnt > int64(r.Remaining()) {
			return errCount
		}
		for range cnt {
			if err := wk.walk(n.elem, r, -1); err != nil {
				return err
			}
		}
		return nil
	case kContainer:
		if n.nslots > 0 {
			base := len(wk.vals)
			for range n.nslots {
				wk.vals = append(wk.vals, 0)
			}
			wk.bases = append(wk.bases, base)
			defer func() {
				wk.vals = wk.vals[:base]
				wk.bases = wk.bases[:len(wk.bases)-1]
			}()
		}
		for i := range n.fields {
			f := &n.fields[i]
			if err := wk.walk(f.n, r, f.slot); err != nil {
				return err
			}
		}
		return nil
	case kSwitch:
		c, ok := n.cases[wk.get(n.cmp)]
		if !ok {
			c = n.def
		}
		return wk.walk(c, r, slot)
	case kBitfield:
		b, err := r.Raw(n.size)
		if err != nil {
			return err
		}
		var v uint64
		for _, x := range b {
			v = v<<8 | uint64(x)
		}
		shift := n.size * 8
		for _, p := range n.parts {
			shift -= p.size
			pv := int64(v>>uint(shift)) & (1<<uint(p.size) - 1)
			if p.signed && pv&(1<<uint(p.size-1)) != 0 {
				pv -= 1 << uint(p.size)
			}
			wk.set(p.slot, pv)
		}
		return nil
	case kHolder:
		id, err := r.VarInt()
		if err != nil {
			return err
		}
		if id == 0 {
			return wk.walk(n.alt, r, -1)
		}
		return nil
	case kHolderSet:
		c, err := r.VarInt()
		if err != nil {
			return err
		}
		if c == 0 {
			return wk.walk(n.alt, r, -1)
		}
		if int64(c-1) > int64(r.Remaining()) {
			return errCount
		}
		for range c - 1 {
			if err := wk.walk(n.elem, r, -1); err != nil {
				return err
			}
		}
		return nil
	case kMetaLoop:
		for {
			if r.Remaining() == 0 {
				return errLoop
			}
			if int(r.B[r.Off]) == n.endVal {
				r.Off++
				return nil
			}
			if err := wk.walk(n.elem, r, -1); err != nil {
				return err
			}
		}
	case kTopBitArray:
		for {
			if r.Remaining() == 0 {
				return errLoop
			}
			more := r.B[r.Off]&0x80 != 0
			if err := wk.walk(n.elem, r, -1); err != nil {
				return err
			}
			if !more {
				return nil
			}
		}
	}
	return errSchema
}
