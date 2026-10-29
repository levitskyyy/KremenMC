package menu

import "github.com/levitskyyy/KremenMC/internal/item"

// Sender delivers menu synchronisation packets to the client.
type Sender interface {
	SetSlot(container, state int32, slot int, s item.Stack)
	SetCursor(s item.Stack)
	SetContent(container, state int32, items []item.Stack, carried item.Stack)
}

// remote is the server's model of what the client shows in one slot
// (RemoteSlot.Synchronized): an exact stack after the server sent one, or
// the client's hashed view after a click.
type remote struct {
	stack  item.Stack
	hash   item.Hashed
	exact  bool
	hashed bool
}

func (r *remote) force(s item.Stack) { *r = remote{stack: s, exact: true} }

func (r *remote) receive(h item.Hashed) { *r = remote{hash: h, hashed: true} }

func (r *remote) matches(s item.Stack) bool {
	switch {
	case r.exact:
		return item.Matches(r.stack, s)
	case r.hashed && r.hash.Matches(s):
		r.force(s)
		return true
	}
	return false
}

// Sync tracks the client's view of a menu and its state id.
type Sync struct {
	Container int32
	state     int32
	slots     []remote
	carried   remote
	suppress  bool
}

func (y *Sync) StateID() int32 { return y.state }

func (y *Sync) increment() int32 {
	y.state = (y.state + 1) & 32767
	return y.state
}

func (y *Sync) ensure(n int) {
	if len(y.slots) != n {
		y.slots = make([]remote, n)
	}
}

// SendAll is sendAllDataToRemote: the full window content.
func (y *Sync) SendAll(m *Menu, out Sender) {
	y.ensure(len(m.Items))
	for i, s := range m.Items {
		y.slots[i].force(s)
	}
	y.carried.force(m.Carried)
	out.SetContent(y.Container, y.increment(), m.Items, m.Carried)
}

// BroadcastChanges sends every slot (then the carried stack) the client
// does not already show.
func (y *Sync) BroadcastChanges(m *Menu, out Sender) {
	y.ensure(len(m.Items))
	if y.suppress {
		return
	}
	for i, s := range m.Items {
		if !y.slots[i].matches(s) {
			y.slots[i].force(s)
			out.SetSlot(y.Container, y.increment(), i, s)
		}
	}
	if !y.carried.matches(m.Carried) {
		y.carried.force(m.Carried)
		out.SetCursor(m.Carried)
	}
}

// Click is ServerGamePacketListenerImpl.handleContainerClick for an open
// menu: apply the click, record the client's predicted slots, then send
// what differs (or everything when the state id was stale).
func (y *Sync) Click(m *Menu, stateID int32, slot int, button, kind int32, changed map[int]item.Hashed, carried item.Hashed, p Player, spectator bool, out Sender) {
	y.ensure(len(m.Items))
	if spectator {
		y.SendAll(m, out)
		return
	}
	if !m.ValidSlot(slot) {
		return
	}
	full := stateID != y.state
	y.suppress = true
	m.Click(slot, button, kind, p)
	for i, h := range changed {
		if i >= 0 && i < len(y.slots) {
			y.slots[i].receive(h)
		}
	}
	y.carried.receive(carried)
	y.suppress = false
	if full {
		y.SendAll(m, out)
		return
	}
	y.BroadcastChanges(m, out)
}

// SetRemote records that the client already shows s in slot i
// (setRemoteSlot), e.g. after a creative slot packet or a drop.
func (y *Sync) SetRemote(m *Menu, i int, s item.Stack) {
	y.ensure(len(m.Items))
	if i >= 0 && i < len(y.slots) {
		y.slots[i].force(s)
	}
}
