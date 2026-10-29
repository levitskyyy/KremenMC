package translate

import (
	"fmt"
	"sort"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// headroom is reserved before every payload so the final packet ID can be
// written in place.
const headroom = 5

// jump routes a packet at some version to the next version where a
// transform applies (fn != nil) or, with fn == nil, to the end of the chain.
type jump struct {
	fn  xform
	id  int32 // packet ID at version ver
	ver int16 // version index fn applies at (or the end version)
	ok  bool  // false: the packet has no counterpart and is dropped
}

// target is the immutable translation state for one protocol, shared by all
// connections speaking it.
type target struct {
	vers []*verInfo // [0] canonical ... [len-1] this protocol
	cb   [][5][]jump
	sb   [][5][]jump
	// gaps lists packets whose layouts differ without a transform or
	// acknowledgement; they are dropped.
	gaps   []string
	maps   *remaps
	blocks container
	biome  biomeDefaults
}

var (
	targetMu    sync.Mutex
	targetCache = map[int32]*target{}
)

func getTarget(protocol int32) (*target, error) {
	targetMu.Lock()
	defer targetMu.Unlock()
	if t, ok := targetCache[protocol]; ok {
		return t, nil
	}
	n := specIndex(protocol)
	if n <= 0 {
		return nil, fmt.Errorf("translate: unsupported protocol %d", protocol)
	}
	t := &target{}
	for i := 0; i <= n; i++ {
		v, err := loadVersion(&specs[i])
		if err != nil {
			return nil, err
		}
		t.vers = append(t.vers, v)
	}
	t.build()
	t.maps = buildRemaps(t.vers[0], t.vers[n])
	t.blocks = container{
		size: 4096, minBits: 4, maxBits: 8,
		srcGlobal: t.maps.srcStateBits, dstGlobal: t.maps.dstStateBits,
		table: t.maps.states,
	}
	t.biome = newBiomeDefaults(t.vers[0], t.vers[n])
	targetCache[protocol] = t
	return t, nil
}

func (t *target) last() *verInfo { return t.vers[len(t.vers)-1] }

func (t *target) build() {
	n := len(t.vers)
	t.cb = make([][5][]jump, n)
	t.sb = make([][5][]jump, n)
	gaps := map[string]bool{}
	for vi := range n {
		for s := range packets.State(5) {
			names := t.vers[vi].names[s][packets.Clientbound]
			t.cb[vi][s] = make([]jump, len(names))
			for id, name := range names {
				t.cb[vi][s][id] = t.routeCB(vi, s, name, gaps)
			}
			names = t.vers[vi].names[s][packets.Serverbound]
			t.sb[vi][s] = make([]jump, len(names))
			for id, name := range names {
				t.sb[vi][s][id] = t.routeSB(vi, s, name, gaps)
			}
		}
	}
	for g := range gaps {
		t.gaps = append(t.gaps, g)
	}
	sort.Strings(t.gaps)
}

var stateNames = [5]string{"handshake", "status", "login", "configuration", "play"}

func (t *target) routeCB(vi int, s packets.State, name string, gaps map[string]bool) jump {
	last := len(t.vers) - 1
	for v := vi; ; v++ {
		cur := t.vers[v]
		if v == last {
			id, _ := cur.id(s, packets.Clientbound, name)
			return jump{id: id, ver: int16(v), ok: true}
		}
		next := t.vers[v+1]
		st := next.step
		if fn := st.cb[s][name]; fn != nil {
			id, _ := cur.id(s, packets.Clientbound, name)
			return jump{fn: fn, id: id, ver: int16(v), ok: true}
		}
		nn := name
		if r, ok := st.cbRename[s][name]; ok {
			nn = r
		}
		if _, ok := next.id(s, packets.Clientbound, nn); !ok || st.dropped[s][packets.Clientbound][name] {
			if !st.dropped[s][packets.Clientbound][name] {
				gaps[fmt.Sprintf("%d->%d clientbound %s %s: missing", cur.protocol, next.protocol, stateNames[s], name)] = true
			}
			return jump{}
		}
		if cur.fingerprint(s, packets.Clientbound, name) != next.fingerprint(s, packets.Clientbound, nn) &&
			!st.same[s][packets.Clientbound][name] {
			gaps[fmt.Sprintf("%d->%d clientbound %s %s: layout differs", cur.protocol, next.protocol, stateNames[s], name)] = true
			return jump{}
		}
		name = nn
	}
}

func (t *target) routeSB(vi int, s packets.State, name string, gaps map[string]bool) jump {
	for v := vi; ; v-- {
		cur := t.vers[v]
		if v == 0 {
			id, ok := cur.id(s, packets.Serverbound, name)
			return jump{id: id, ver: 0, ok: ok}
		}
		st := cur.step
		newer := t.vers[v-1]
		if fn := st.sb[s][name]; fn != nil {
			id, _ := cur.id(s, packets.Serverbound, name)
			return jump{fn: fn, id: id, ver: int16(v), ok: true}
		}
		nn := name
		if r, ok := st.sbRename[s][name]; ok {
			nn = r
		}
		if _, ok := newer.id(s, packets.Serverbound, nn); !ok || st.dropped[s][packets.Serverbound][name] {
			if !st.dropped[s][packets.Serverbound][name] {
				gaps[fmt.Sprintf("%d->%d serverbound %s %s: missing", cur.protocol, newer.protocol, stateNames[s], name)] = true
			}
			return jump{}
		}
		if cur.fingerprint(s, packets.Serverbound, name) != newer.fingerprint(s, packets.Serverbound, nn) &&
			!st.same[s][packets.Serverbound][name] {
			gaps[fmt.Sprintf("%d->%d serverbound %s %s: layout differs", cur.protocol, newer.protocol, stateNames[s], name)] = true
			return jump{}
		}
		name = nn
	}
}

// xctx is handed to transforms. Each version level owns one per direction,
// so nested transforms never share scratch space.
type xctx struct {
	c   *conn
	s   packets.State
	ver int // version index of emitted packets
	cb  bool
	r   proto.Reader
	w   proto.Writer
}

// out returns the level's scratch writer, reset with headroom.
func (x *xctx) out() *proto.Writer {
	x.w.B = append(x.w.B[:0], 0, 0, 0, 0, 0)
	return &x.w
}

// emit forwards a payload written to w (from out) as packet name of the
// next version.
func (x *xctx) emit(name string, w *proto.Writer) error {
	v := x.c.t.vers[x.ver]
	d := packets.Serverbound
	if x.cb {
		d = packets.Clientbound
	}
	id, ok := v.id(x.s, d, name)
	if !ok {
		return fmt.Errorf("translate: %d has no %s packet %q", v.protocol, stateNames[x.s], name)
	}
	if x.cb {
		return x.c.runCB(x.s, x.ver, id, w)
	}
	return x.c.runSB(x.s, x.ver, id, w.B[headroom:])
}

// pass re-emits the input payload unchanged as packet name.
func (x *xctx) pass(name string) error {
	w := x.out()
	w.Raw(x.r.B)
	return x.emit(name, w)
}

func (c *conn) runCB(s packets.State, v int, id int32, w *proto.Writer) error {
	tab := c.t.cb[v][s]
	if id < 0 || int(id) >= len(tab) {
		return nil
	}
	j := &tab[id]
	if !j.ok {
		return nil
	}
	if j.fn == nil {
		n := proto.VarIntSize(j.id)
		start := headroom - n
		proto.AppendVarInt(w.B[:start], j.id)
		return c.emit(w.B[start:])
	}
	x := &c.cbx[j.ver]
	x.s, x.ver = s, int(j.ver)+1
	x.r = proto.Reader{B: w.B[headroom:]}
	return j.fn(x, &x.r)
}

func (c *conn) runSB(s packets.State, v int, id int32, payload []byte) error {
	tab := c.t.sb[v][s]
	if id < 0 || int(id) >= len(tab) {
		return nil
	}
	j := &tab[id]
	if !j.ok {
		return nil
	}
	if j.fn == nil {
		return c.finishSB(s, j.id, payload)
	}
	x := &c.sbx[j.ver]
	x.s, x.ver = s, int(j.ver)-1
	x.r = proto.Reader{B: payload}
	return j.fn(x, &x.r)
}
