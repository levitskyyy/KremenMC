package translate

import (
	"errors"

	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/translate/pdef"
)

const maxItemDepth = 8

var errDepth = errors.New("translate: item nesting too deep")

// itemRewriter rewrites item stacks, particles and other opaque values from
// canonical to target IDs (or back when up is set). One per direction per
// connection; not safe for concurrent use.
type itemRewriter struct {
	m       *remaps
	up      bool
	c       *conn
	depth   int
	bufs    [maxItemDepth]proto.Writer
	walkers [maxItemDepth]pdef.Walker
	compSrc []*pdef.Node // component payload schema by source type ID
}

func newItemRewriter(c *conn, m *remaps, up bool) *itemRewriter {
	rw := &itemRewriter{m: m, up: up, c: c}
	comps := m.srcComp
	if up {
		comps = m.dstComp
	}
	names := c.t.vers[0].meta.Enums["component"]
	if up {
		names = c.t.last().meta.Enums["component"]
	}
	rw.compSrc = make([]*pdef.Node, len(names))
	for i, n := range names {
		rw.compSrc[i] = comps[n]
	}
	hooks := map[string]pdef.Hook{
		"Slot":              rw.slot,
		"ItemStackTemplate": rw.template,
		"Particle":          rw.particleHook,
	}
	for i := range rw.walkers {
		rw.walkers[i].Hooks = hooks
	}
	return rw
}

func (rw *itemRewriter) itemID(id int32) (int32, bool) {
	if rw.up {
		return rw.m.itemUp(id), true
	}
	if id < 0 || int(id) >= len(rw.m.itemKnown) {
		return rw.m.stoneItem, false
	}
	return rw.m.items[id], rw.m.itemKnown[id]
}

func (rw *itemRewriter) compID(t int32) int32 {
	if rw.up {
		return at(rw.m.componentsUp, t)
	}
	return at(rw.m.components, t)
}

func (rw *itemRewriter) enter() (*proto.Writer, *pdef.Walker, error) {
	if rw.depth >= maxItemDepth-1 {
		return nil, nil, errDepth
	}
	rw.depth++
	b := &rw.bufs[rw.depth]
	b.B = b.B[:0]
	return b, &rw.walkers[rw.depth], nil
}

func (rw *itemRewriter) leave() { rw.depth-- }

// slot rewrites one Slot (count, then item and component patch).
func (rw *itemRewriter) slot(r *proto.Reader, w *proto.Writer) error {
	cnt, err := r.VarInt()
	if err != nil {
		return err
	}
	if cnt <= 0 {
		if w != nil {
			w.VarInt(cnt)
		}
		return nil
	}
	if w != nil {
		w.VarInt(cnt)
	}
	return rw.stack(r, w)
}

// template rewrites an ItemStackTemplate (item, count, component patch).
func (rw *itemRewriter) template(r *proto.Reader, w *proto.Writer) error {
	id, err := r.VarInt()
	if err != nil {
		return err
	}
	cnt, err := r.VarInt()
	if err != nil {
		return err
	}
	tid, known := rw.itemID(id)
	if w != nil {
		if rw.m.templateAsSlot && !rw.up {
			w.VarInt(cnt)
			w.VarInt(tid)
		} else {
			w.VarInt(tid)
			w.VarInt(cnt)
		}
	}
	return rw.patch(r, w, known)
}

func (rw *itemRewriter) stack(r *proto.Reader, w *proto.Writer) error {
	id, err := r.VarInt()
	if err != nil {
		return err
	}
	tid, known := rw.itemID(id)
	if w != nil {
		w.VarInt(tid)
	}
	return rw.patch(r, w, known)
}

// patch rewrites a component patch, dropping components the other side
// lacks or encodes differently (and all of them for substituted items).
func (rw *itemRewriter) patch(r *proto.Reader, w *proto.Writer, keep bool) error {
	add, err := r.VarInt()
	if err != nil {
		return err
	}
	rem, err := r.VarInt()
	if err != nil {
		return err
	}
	if add < 0 || rem < 0 || int(add)+int(rem) > r.Remaining() {
		return errCount
	}
	buf, wk, err := rw.enter()
	if err != nil {
		return err
	}
	defer rw.leave()
	var nAdd, nRem int32
	for range add {
		t, err := r.VarInt()
		if err != nil {
			return err
		}
		if t < 0 || int(t) >= len(rw.compSrc) || rw.compSrc[t] == nil {
			return errUnknownValue
		}
		tt := rw.compID(t)
		if !keep || tt < 0 {
			if err := pdef.Skip(rw.compSrc[t], r); err != nil {
				return err
			}
			continue
		}
		buf.VarInt(tt)
		if err := wk.Copy(rw.compSrc[t], r, buf); err != nil {
			return err
		}
		nAdd++
	}
	mark := len(buf.B)
	for range rem {
		t, err := r.VarInt()
		if err != nil {
			return err
		}
		if tt := rw.compID(t); tt >= 0 && keep {
			buf.VarInt(tt)
			nRem++
		}
	}
	if w != nil {
		w.VarInt(nAdd)
		w.VarInt(nRem)
		w.Raw(buf.B[:mark])
		w.Raw(buf.B[mark:])
	}
	return nil
}

// untrusted rewrites a serverbound UntrustedSlot (length-prefixed components).
func (rw *itemRewriter) untrusted(r *proto.Reader, w *proto.Writer) error {
	cnt, err := r.VarInt()
	if err != nil {
		return err
	}
	w.VarInt(cnt)
	if cnt <= 0 {
		return nil
	}
	id, err := r.VarInt()
	if err != nil {
		return err
	}
	tid, known := rw.itemID(id)
	w.VarInt(tid)
	add, err := r.VarInt()
	if err != nil {
		return err
	}
	rem, err := r.VarInt()
	if err != nil {
		return err
	}
	if add < 0 || rem < 0 || int(add)+int(rem) > r.Remaining() {
		return errCount
	}
	buf, _, err := rw.enter()
	if err != nil {
		return err
	}
	defer rw.leave()
	var nAdd, nRem int32
	for range add {
		t, err := r.VarInt()
		if err != nil {
			return err
		}
		d, err := r.ByteArray(1 << 21)
		if err != nil {
			return err
		}
		if tt := rw.compID(t); tt >= 0 && known {
			buf.VarInt(tt)
			buf.ByteArray(d)
			nAdd++
		}
	}
	mark := len(buf.B)
	for range rem {
		t, err := r.VarInt()
		if err != nil {
			return err
		}
		if tt := rw.compID(t); tt >= 0 && known {
			buf.VarInt(tt)
			nRem++
		}
	}
	w.VarInt(nAdd)
	w.VarInt(nRem)
	w.Raw(buf.B[:mark])
	w.Raw(buf.B[mark:])
	return nil
}

// hashed rewrites a serverbound HashedSlot. Component hashes are kept; the
// server resynchronizes slots whose hashes do not match.
func (rw *itemRewriter) hashed(r *proto.Reader, w *proto.Writer) error {
	id, err := r.VarInt()
	if err != nil {
		return err
	}
	cnt, err := r.VarInt()
	if err != nil {
		return err
	}
	tid, _ := rw.itemID(id)
	w.VarInt(tid)
	w.VarInt(cnt)
	for pass := range 2 {
		n, err := r.VarInt()
		if err != nil {
			return err
		}
		if n < 0 || int(n) > r.Remaining() {
			return errCount
		}
		buf, _, err := rw.enter()
		if err != nil {
			return err
		}
		var kept int32
		for range n {
			t, err := r.VarInt()
			if err != nil {
				rw.leave()
				return err
			}
			var h int32
			if pass == 0 {
				if h, err = r.I32(); err != nil {
					rw.leave()
					return err
				}
			}
			if tt := rw.compID(t); tt >= 0 {
				buf.VarInt(tt)
				if pass == 0 {
					buf.I32(h)
				}
				kept++
			}
		}
		w.VarInt(kept)
		w.Raw(buf.B)
		rw.leave()
	}
	return nil
}
