package world

type TicketID uint64

type TicketKind uint8

const (
	TicketPlayer TicketKind = iota
	TicketForced
	TicketPlugin
)

type ticket struct {
	kind   TicketKind
	center ChunkPos
	radius int32
}

func (t *ticket) contains(p ChunkPos) bool {
	dx, dz := p.X-t.center.X, p.Z-t.center.Z
	return dx >= -t.radius && dx <= t.radius && dz >= -t.radius && dz <= t.radius
}

// AddTicket keeps every chunk within Chebyshev radius of center loaded,
// requesting missing ones nearest-first.
func (w *World) AddTicket(center ChunkPos, radius int, kind TicketKind) TicketID {
	w.ticketMu.Lock()
	w.nextTicket++
	id := w.nextTicket
	t := &ticket{kind: kind, center: center, radius: int32(max(radius, 0))}
	w.tickets[id] = t
	var need []ChunkPos
	forRing(center, t.radius, func(p ChunkPos) {
		if w.ref(p, 1) {
			need = append(need, p)
		}
	})
	w.ticketMu.Unlock()
	for _, p := range need {
		w.request(p, false)
	}
	return id
}

// MoveTicket recenters a ticket, adjusting only the chunks that change.
func (w *World) MoveTicket(id TicketID, center ChunkPos) {
	w.ticketMu.Lock()
	t, ok := w.tickets[id]
	if !ok || t.center == center {
		w.ticketMu.Unlock()
		return
	}
	nt := ticket{kind: t.kind, center: center, radius: t.radius}
	var need []ChunkPos
	forRing(center, t.radius, func(p ChunkPos) {
		if !t.contains(p) && w.ref(p, 1) {
			need = append(need, p)
		}
	})
	forRing(t.center, t.radius, func(p ChunkPos) {
		if !nt.contains(p) {
			w.ref(p, -1)
		}
	})
	t.center = center
	w.ticketMu.Unlock()
	for _, p := range need {
		w.request(p, false)
	}
}

func (w *World) RemoveTicket(id TicketID) {
	w.ticketMu.Lock()
	defer w.ticketMu.Unlock()
	t, ok := w.tickets[id]
	if !ok {
		return
	}
	delete(w.tickets, id)
	forRing(t.center, t.radius, func(p ChunkPos) { w.ref(p, -1) })
}

// ref adjusts a chunk's ticket count; it returns true on 0 -> 1.
func (w *World) ref(p ChunkPos, d int32) bool {
	k := p.Key()
	n := w.refs[k] + d
	if n <= 0 {
		delete(w.refs, k)
		w.unloads[k] = struct{}{}
		w.loadMu.Lock()
		if o := w.inflight[k]; o != nil && !o.save && o.waiters == 0 {
			o.cancel()
		}
		w.loadMu.Unlock()
		return false
	}
	w.refs[k] = n
	if n == d {
		delete(w.unloads, k)
		return true
	}
	return false
}

// Ticketed reports whether any ticket covers p.
func (w *World) Ticketed(p ChunkPos) bool {
	w.ticketMu.Lock()
	defer w.ticketMu.Unlock()
	return w.refs[p.Key()] > 0
}

// forRing visits the square of radius r around c, nearest rings first.
func forRing(c ChunkPos, r int32, fn func(ChunkPos)) {
	fn(c)
	for d := int32(1); d <= r; d++ {
		for i := -d; i <= d; i++ {
			fn(ChunkPos{c.X + i, c.Z - d})
			fn(ChunkPos{c.X + i, c.Z + d})
		}
		for i := -d + 1; i <= d-1; i++ {
			fn(ChunkPos{c.X - d, c.Z + i})
			fn(ChunkPos{c.X + d, c.Z + i})
		}
	}
}
