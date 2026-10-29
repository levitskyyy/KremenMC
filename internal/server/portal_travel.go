package server

import (
	"context"
	"math"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

const (
	portalDelaySurvival = 80 // players_nether_portal_default_delay
	portalDelayCreative = 1  // players_nether_portal_creative_delay
	portalCooldownTicks = 10 // player dimension-changing delay
	portalTicketTicks   = 300
	portalSearchFar     = 128 // existing-portal search radius outside the nether
	portalSearchNether  = 16
	portalCreateRadius  = 16
)

type portalKind uint8

const (
	portalNone portalKind = iota
	portalNether
	portalEnd
)

// portalState is the player's portal processor, owned by the tick goroutine.
type portalState struct {
	time     int
	cooldown int
	busy     bool
	done     chan portalArrival
	tickets  []portalTicket
}

type portalTicket struct {
	w     *World
	id    world.TicketID
	until int64
}

type portalArrival struct {
	kind  portalKind
	dst   *World
	at    api.Vec3 // target position in dst
	axis  portalAxis
	found bool
}

// tickPortals runs the portal processors after the player phase, on the
// tick goroutine.
func (s *Server) tickPortals(tick int64) {
	for _, w := range s.worldList() {
		w.tickPortalBreaks()
	}
	for _, p := range s.list {
		if p.joined.Load() && !p.quit.Load() {
			p.portalTick(tick)
		}
	}
}

func (p *Player) portalTick(tick int64) {
	st := &p.portal
	st.tickets = dropTickets(st.tickets, tick)
	if st.busy {
		select {
		case a := <-st.done:
			st.busy = false
			p.arrive(a, tick)
		default:
		}
		return
	}
	w := p.world.Load()
	if w == nil || p.IsDead() {
		return
	}
	if st.cooldown > 0 {
		st.cooldown--
	}
	kind, axis := p.insidePortal(w)
	switch kind {
	case portalNone:
		st.time = max(0, st.time-4)
		return
	case portalEnd:
		if st.cooldown > 0 {
			st.cooldown = portalCooldownTicks
			return
		}
		p.departEnd(w)
		return
	}
	if st.cooldown > 0 {
		st.cooldown = portalCooldownTicks
		return
	}
	wait := portalDelaySurvival
	if gm := p.GameMode(); gm == api.Creative {
		wait = portalDelayCreative
	}
	st.time++
	if st.time < wait {
		return
	}
	st.time = 0
	p.departNether(w, axis)
}

// insidePortal reports the portal block the player's box overlaps.
func (p *Player) insidePortal(w *World) (portalKind, portalAxis) {
	p.mu.Lock()
	b := p.e.Box()
	p.mu.Unlock()
	pb := portalBlocks()
	const eps = 1e-7
	for y := int(math.Floor(b.MinY + eps)); y <= int(math.Floor(b.MaxY-eps)); y++ {
		for x := int(math.Floor(b.MinX + eps)); x <= int(math.Floor(b.MaxX-eps)); x++ {
			for z := int(math.Floor(b.MinZ + eps)); z <= int(math.Floor(b.MaxZ-eps)); z++ {
				s := w.w.Block(x, y, z)
				switch blockID(s) {
				case pb.portal:
					return portalNether, portalAxisOf(s)
				case pb.endPortal:
					// The end portal's shape is the bottom 12/16 of the block.
					if b.MinY < float64(y)+0.75 {
						return portalEnd, axisX
					}
				}
			}
		}
	}
	return portalNone, axisX
}

func dropTickets(ts []portalTicket, tick int64) []portalTicket {
	out := ts[:0]
	for _, t := range ts {
		if tick >= t.until {
			t.w.w.RemoveTicket(t.id)
			continue
		}
		out = append(out, t)
	}
	return out
}

// departNether starts a nether portal trip: the destination chunks load off
// the tick goroutine, then arrive builds or finds the portal.
func (p *Player) departNether(w *World, axis portalAxis) {
	var dst *World
	switch w.dim {
	case dimOverworld:
		dst = p.s.dimWorld(dimNether)
	case dimNether:
		dst = p.s.defaultWorld()
	}
	if dst == nil || dst == w {
		return
	}
	pos := p.Location().Pos
	scale := w.dim.coordScale() / dst.dim.coordScale()
	const border = 29999984 - 16
	tx := math.Max(-border, math.Min(border, pos.X*scale))
	tz := math.Max(-border, math.Min(border, pos.Z*scale))
	ty := math.Max(float64(dst.MinY()), math.Min(pos.Y, float64(dst.MinY()+dst.dim.logicalHeight()-1)))
	p.startTrip(portalArrival{kind: portalNether, dst: dst, at: api.Vec3{X: tx, Y: ty, Z: tz}, axis: axis})
}

func (p *Player) departEnd(w *World) {
	var a portalArrival
	if w.dim == dimEnd {
		dst := p.s.defaultWorld()
		at := dst.Spawn().Pos
		if r, ok := p.RespawnLocation(); ok && (r.World == "" || r.World == dst.name) {
			at = r.Pos
		}
		a = portalArrival{kind: portalEnd, dst: dst, at: at}
	} else {
		dst := p.s.dimWorld(dimEnd)
		if dst == nil {
			return
		}
		a = portalArrival{kind: portalEnd, dst: dst, at: api.Vec3{X: float64(endSpawn.X), Y: float64(endSpawn.Y), Z: float64(endSpawn.Z)}}
	}
	p.startTrip(a)
}

func (p *Player) startTrip(a portalArrival) {
	st := &p.portal
	tick := p.s.CurrentTick()
	c := world.ChunkAt(int(math.Floor(a.at.X)), int(math.Floor(a.at.Z)))
	r := 1
	if a.kind == portalNether {
		r = portalCreateRadius>>4 + 1
	}
	st.tickets = append(st.tickets, portalTicket{w: a.dst, id: a.dst.w.AddTicket(c, r, world.TicketForced), until: tick + portalTicketTicks})
	var extra []world.ChunkPos
	if a.kind == portalNether {
		rad := portalSearchFar
		if a.dst.dim == dimNether {
			rad = portalSearchNether
		}
		for i, q := range a.dst.portalCandidates(blockAt(a.at), rad) {
			if i == 4 {
				break
			}
			extra = append(extra, world.ChunkAt(q.X, q.Z))
		}
		for _, q := range extra {
			st.tickets = append(st.tickets, portalTicket{w: a.dst, id: a.dst.w.AddTicket(q, 0, world.TicketForced), until: tick + portalTicketTicks})
		}
	}
	st.busy = true
	if st.done == nil {
		st.done = make(chan portalArrival, 1)
	}
	done := st.done
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if _, err := a.dst.w.LoadChunk(ctx, world.ChunkPos{X: c.X + int32(dx), Z: c.Z + int32(dz)}); err != nil {
					a.dst = nil
				}
			}
		}
		for _, q := range extra {
			_, _ = a.dst.w.LoadChunk(ctx, q)
		}
		done <- a
	}()
}

func blockAt(v api.Vec3) api.BlockPos {
	return api.BlockPos{X: int(math.Floor(v.X)), Y: int(math.Floor(v.Y)), Z: int(math.Floor(v.Z))}
}

// arrive finishes a trip on the tick goroutine.
func (p *Player) arrive(a portalArrival, tick int64) {
	if a.dst == nil || p.IsDead() {
		return
	}
	loc := api.Location{Pos: a.at}
	p.mu.Lock()
	loc.Yaw, loc.Pitch = p.e.Yaw, p.e.Pitch
	p.mu.Unlock()
	switch {
	case a.kind == portalEnd && a.dst.dim == dimEnd:
		buildEndPlatform(a.dst)
		loc.Pos = api.Vec3{X: float64(endSpawn.X) + 0.5, Y: float64(endSpawn.Y - 1), Z: float64(endSpawn.Z) + 0.5}
		loc.Yaw, loc.Pitch = 90, 0
	case a.kind == portalEnd:
	default:
		loc.Pos = a.dst.netherArrival(a.at, a.axis)
	}
	p.portal.cooldown = portalCooldownTicks
	p.portal.time = 0
	p.changeDimension(a.dst, loc)
}

// netherArrival finds the nearest indexed portal around target, else builds
// one, and returns the feet position in it.
func (w *World) netherArrival(target api.Vec3, axis portalAxis) api.Vec3 {
	t := blockAt(target)
	rad := portalSearchFar
	if w.dim == dimNether {
		rad = portalSearchNether
	}
	for _, q := range w.portalCandidates(t, rad) {
		s := w.w.Block(q.X, q.Y, q.Z)
		if !isNetherPortal(s) {
			continue
		}
		return portalRectFeet(w.w, q, s)
	}
	return w.createPortal(t, axis)
}

// portalRectFeet is the bottom centre of the portal containing q.
func portalRectFeet(src exBlocks, q api.BlockPos, s data.StateID) api.Vec3 {
	a := portalAxisOf(s)
	dx, dz := a.step()
	for n := 0; n < portalMaxSize && src.Block(q.X, q.Y-1, q.Z) == s; n++ {
		q.Y--
	}
	for n := 0; n < portalMaxSize && src.Block(q.X-dx, q.Y, q.Z-dz) == s; n++ {
		q.X, q.Z = q.X-dx, q.Z-dz
	}
	width := 1
	for width < portalMaxSize && src.Block(q.X+width*dx, q.Y, q.Z+width*dz) == s {
		width++
	}
	half := float64(width) / 2
	return api.Vec3{X: float64(q.X) + 0.5 + float64(dx)*(half-0.5), Y: float64(q.Y), Z: float64(q.Z) + 0.5 + float64(dz)*(half-0.5)}
}

// createPortal builds a 2x3 portal in a 4x5 obsidian frame on the closest
// spot within portalCreateRadius that has solid ground under the frame and
// open space for it; otherwise at the clamped target with a platform.
func (w *World) createPortal(t api.BlockPos, axis portalAxis) api.Vec3 {
	dx, dz := axis.step()
	px, pz := dz, dx // perpendicular
	minY := w.MinY()
	top := minY + w.dim.logicalHeight() - 1
	solid := func(s data.StateID) bool { return !data.IsAir(s) && !isLiquid(s) && exFlags(s)&exSturdyUp != 0 }
	fits := func(x, y, z int) bool {
		for i := -1; i <= 2; i++ {
			for _, o := range [...]int{-1, 1} {
				if !solid(w.w.Block(x+i*dx+o*px, y-1, z+i*dz+o*pz)) {
					return false
				}
			}
			for h := range 4 {
				for o := -1; o <= 1; o++ {
					if !data.IsAir(w.w.Block(x+i*dx+o*px, y+h, z+i*dz+o*pz)) {
						return false
					}
				}
			}
		}
		return true
	}
	best, bestD := api.BlockPos{}, math.MaxInt
	for x := t.X - portalCreateRadius; x <= t.X+portalCreateRadius; x++ {
		for z := t.Z - portalCreateRadius; z <= t.Z+portalCreateRadius; z++ {
			for y := top - 4; y > minY; y-- {
				if !data.IsAir(w.w.Block(x, y, z)) {
					continue
				}
				q := api.BlockPos{X: x, Y: y, Z: z}
				if d := distSq(q, t); d < bestD && fits(x, y, z) {
					best, bestD = q, d
				}
			}
		}
	}
	ob, _ := data.DefaultState("obsidian")
	if bestD == math.MaxInt {
		best = t
		best.Y = max(min(t.Y, top-10), max(minY+1, 70))
		for i := range 2 {
			for _, o := range [...]int{-1, 1} {
				x, z := best.X+i*dx+o*px, best.Z+i*dz+o*pz
				w.w.SetBlock(x, best.Y-1, z, ob)
				for h := range 3 {
					w.w.SetBlock(x, best.Y+h, z, data.Air)
				}
			}
		}
	}
	st := axis.state()
	for i := -1; i <= 2; i++ {
		for h := -1; h <= 3; h++ {
			x, y, z := best.X+i*dx, best.Y+h, best.Z+i*dz
			if i == -1 || i == 2 || h == -1 || h == 3 {
				w.w.SetBlock(x, y, z, ob)
			} else {
				w.w.SetBlock(x, y, z, st)
			}
		}
	}
	return api.Vec3{X: float64(best.X) + 0.5 + float64(dx)*0.5, Y: float64(best.Y), Z: float64(best.Z) + 0.5 + float64(dz)*0.5}
}

func isLiquid(s data.StateID) bool {
	b := data.BlockOf(s)
	return b != nil && (b.Name == "water" || b.Name == "lava")
}

// buildEndPlatform is the 5x5 obsidian platform under the end spawn with
// three blocks of air above it.
func buildEndPlatform(w *World) {
	ob, _ := data.DefaultState("obsidian")
	c := endSpawn.Add(0, -1, 0)
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			for dy := -1; dy <= 2; dy++ {
				s := data.Air
				if dy == -1 {
					s = ob
				}
				x, y, z := c.X+dx, c.Y+dy, c.Z+dz
				if w.w.Block(x, y, z) != s {
					w.w.SetBlock(x, y, z, s)
				}
			}
		}
	}
}
