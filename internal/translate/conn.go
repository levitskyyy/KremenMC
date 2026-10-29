package translate

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/pdef"
	"github.com/levitskyyy/KremenMC/internal/translate/v768"
)

// conn is the Codec of one connection. Encode keeps clientbound state and
// Decode serverbound state, so the two may run concurrently, but each must
// be called from one goroutine at a time.
type conn struct {
	t    *target
	emit func([]byte) error

	cbx, sbx []xctx
	encW     proto.Writer

	// clientbound state
	cbItems  *itemRewriter
	regs     map[string]*syncedReg
	biomes   container
	entities map[int32]int32
	scratch  scratch
	dayTime  int64
	ticking  bool

	// written by Encode, read by Decode
	tpMu      sync.Mutex
	teleports [8]teleportSlot
	loaded    atomic.Bool

	// serverbound state
	input      v768.ServerboundPlayerInputInputs
	yaw, pitch float32
	sbItems    *itemRewriter
	sbW        proto.Writer
	out        []packets.Packet
}

func newConn(t *target) *conn {
	c := &conn{t: t, regs: map[string]*syncedReg{}, entities: map[int32]int32{}}
	n := len(t.vers)
	c.cbx = make([]xctx, n)
	c.sbx = make([]xctx, n)
	for i := range n {
		c.cbx[i] = xctx{c: c, cb: true}
		c.sbx[i] = xctx{c: c}
	}
	c.biomes = t.biome.container
	c.cbItems = newItemRewriter(c, t.maps, false)
	c.sbItems = newItemRewriter(c, t.maps, true)
	return c
}

func (c *conn) Protocol() int32 { return c.t.last().protocol }
func (c *conn) Name() string    { return c.t.last().name }

func (c *conn) Encode(s packets.State, p packets.Packet, emit func(body []byte) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("translate: encode %T for %d: %v", p, c.Protocol(), r)
		}
		c.emit = nil
	}()
	c.emit = emit
	q, err := c.rewriteCB(s, p)
	if err != nil {
		return fmt.Errorf("translate: rewrite %T for %d: %w", p, c.Protocol(), err)
	}
	if q == nil {
		return nil
	}
	w := &c.encW
	w.B = append(w.B[:0], 0, 0, 0, 0, 0)
	q.Encode(w)
	if err := c.runCB(s, 0, q.ID(), w); err != nil {
		return fmt.Errorf("translate: encode %T for %d: %w", p, c.Protocol(), err)
	}
	return nil
}

func (c *conn) Decode(s packets.State, body []byte) (out []packets.Packet, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("translate: decode for %d: %v", c.Protocol(), r)
		}
	}()
	r := proto.Reader{B: body}
	id, err := r.VarInt()
	if err != nil {
		return nil, fmt.Errorf("translate: packet id: %w", err)
	}
	c.out = nil
	if err := c.runSB(s, len(c.t.vers)-1, id, body[r.Off:]); err != nil {
		return nil, fmt.Errorf("translate: decode %s 0x%02X from %d: %w", stateNames[s], id, c.Protocol(), err)
	}
	return c.out, nil
}

func (c *conn) finishSB(s packets.State, id int32, payload []byte) error {
	p := packets.New(s, packets.Serverbound, id)
	if p == nil {
		return fmt.Errorf("%w: 0x%02X", packets.ErrUnknownPacket, id)
	}
	if err := packets.DecodeBody(p, &proto.Reader{B: payload}); err != nil {
		return fmt.Errorf("decode %s: %w", packets.Name(s, packets.Serverbound, id), err)
	}
	p, err := c.rewriteSB(s, p)
	if err != nil {
		return err
	}
	if p != nil {
		c.out = append(c.out, p)
	}
	return nil
}

func (c *conn) NextState(s packets.State, p packets.Packet, serverbound bool) packets.State {
	return NextState(s, p, serverbound)
}

// NextState is the state transition shared by all protocols with a
// configuration phase (1.20.2+).
func NextState(s packets.State, p packets.Packet, serverbound bool) packets.State {
	if !serverbound {
		return s
	}
	switch q := p.(type) {
	case *packets.ServerboundHandshakeIntention:
		if q.NextState == 1 {
			return packets.Status
		}
		return packets.Login
	case *packets.ServerboundLoginAcknowledged:
		return packets.Configuration
	case *packets.ServerboundConfigFinishConfiguration:
		return packets.Play
	case *packets.ServerboundConfigurationAcknowledged:
		return packets.Configuration
	}
	return s
}

type teleport struct {
	x, y, z    float64
	yaw, pitch float32
}

func (c *conn) recordTeleport(p *packets.ClientboundPlayerPosition) {
	c.tpMu.Lock()
	c.teleports[p.TeleportID&7] = teleportSlot{id: p.TeleportID, ok: true, t: teleport{p.X, p.Y, p.Z, p.Yaw, p.Pitch}}
	c.tpMu.Unlock()
}

func (c *conn) teleport(id int32) (teleport, bool) {
	c.tpMu.Lock()
	defer c.tpMu.Unlock()
	s := c.teleports[id&7]
	return s.t, s.ok && s.id == id
}

type teleportSlot struct {
	id int32
	ok bool
	t  teleport
}

// skipTargetParticle skips a particle already rewritten to the target format.
func (c *conn) skipTargetParticle(r *proto.Reader) error {
	t, err := r.VarInt()
	if err != nil {
		return err
	}
	cases := c.t.maps.dstPartByID
	if t < 0 || int(t) >= len(cases) || cases[t] == nil {
		return errUnknownValue
	}
	return pdef.Skip(cases[t], r)
}

func (c *conn) rotation() (float32, float32) { return c.yaw, c.pitch }
