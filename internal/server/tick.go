package server

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

const (
	tickTime    = 50 * time.Millisecond
	latencyTick = 100
	timeSync    = 20
)

func f64bits(v float64) uint64 { return math.Float64bits(v) }
func f64(b uint64) float64     { return math.Float64frombits(b) }

func (s *Server) run() {
	defer close(s.done)
	s.sched.gid.Store(goid())
	maxLag := s.cfg.Engine.World.MaxTickLag.D()
	if maxLag <= 0 {
		maxLag = 2 * time.Second
	}
	autosave := s.cfg.Engine.World.AutosaveInterval.D()
	lastSave := time.Now()
	next := time.Now()
	var window [100]time.Duration
	var times [20]time.Time
	var n int
	for {
		select {
		case <-s.stop:
			s.shutdownPlayers()
			return
		default:
		}
		start := time.Now()
		s.doTick()
		el := time.Since(start)
		s.tickTimes.Record(el)
		window[n%len(window)] = el
		times[n%len(times)] = start
		n++
		var sum time.Duration
		cnt := min(n, len(window))
		for _, d := range window[:cnt] {
			sum += d
		}
		s.msptBits.Store(f64bits(float64(sum) / float64(cnt) / float64(time.Millisecond)))
		if n >= len(times) {
			span := start.Sub(times[n%len(times)])
			if span > 0 {
				s.tpsBits.Store(f64bits(min(20, float64(len(times)-1)*float64(time.Second)/float64(span))))
			}
		}
		if s.bus.HasHandlers(api.KindServerTickEnd) {
			s.bus.Dispatch(&api.ServerTickEndEvent{Tick: s.CurrentTick(), DurationNanos: int64(el), RemainingNanos: int64(time.Until(next.Add(tickTime)))})
		}
		if autosave > 0 && s.autoSave.Load() && time.Since(lastSave) >= autosave {
			lastSave = time.Now()
			for _, w := range s.worldList() {
				go func(w *World) {
					if err := w.Save(); err != nil {
						s.log.Error("autosave", "world", w.name, "err", err)
					}
				}(w)
			}
		}
		next = next.Add(tickTime)
		if behind := time.Since(next); behind > maxLag {
			s.log.Warn("can't keep up, skipping ticks", "behind", behind.Round(time.Millisecond), "ticks", int64(behind/tickTime))
			next = time.Now()
		}
		if d := time.Until(next); d > 0 {
			t := time.NewTimer(d)
			select {
			case <-t.C:
			case <-s.stop:
				t.Stop()
			}
		}
	}
}

func (s *Server) shutdownPlayers() {
	reason := api.Translatable("multiplayer.disconnect.server_shutdown")
	if r := s.shutdown.Load(); r != nil && !r.IsEmpty() {
		reason = *r
	}
	for _, p := range s.list {
		p.c.Disconnect(toText(reason))
	}
	deadline := time.After(2 * time.Second)
	for _, p := range s.list {
		select {
		case <-p.c.Done():
		case <-deadline:
		}
	}
	for _, p := range s.list {
		p.c.Close(nil)
	}
	for {
		select {
		case p := <-s.quits:
			s.leave(p)
		default:
			return
		}
	}
}

// parallel runs fn for every player on the tick worker pool.
func (s *Server) parallel(ps []*Player, fn func(*Player)) {
	if len(ps) < 8 {
		for _, p := range ps {
			fn(p)
		}
		return
	}
	nw := min(s.workers(), len(ps))
	var next atomic.Int64
	var wg sync.WaitGroup
	wg.Add(nw)
	for range nw {
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1) - 1)
				if i >= len(ps) {
					return
				}
				fn(ps[i])
			}
		}()
	}
	wg.Wait()
}

func (s *Server) doTick() {
	tick := s.tick.Add(1)
	if s.bus.HasHandlers(api.KindServerTickStart) {
		s.bus.Dispatch(&api.ServerTickStartEvent{Tick: tick})
	}
	for {
		select {
		case p := <-s.quits:
			s.leave(p)
			continue
		default:
		}
		break
	}
	for {
		select {
		case p := <-s.joins:
			s.join(p)
			continue
		default:
		}
		break
	}
	for {
		select {
		case line := <-s.consoleQ:
			s.consoleCommand(line)
			continue
		default:
		}
		break
	}
	s.sched.global.run(tick)

	s.parallel(s.list, (*Player).process)
	s.tickPortals(tick)

	worlds := s.worldList()
	for _, w := range worlds {
		w.gameTime.Add(1)
		if v, ok := w.GameRule("doDaylightCycle"); !ok || v != "false" {
			w.dayTime.Add(1)
		}
		w.tickSpawn(s.list)
		w.tickEntities()
		w.aiPrepare(s.list)
		w.w.DoTick()
		w.spawnGenEntities()
		w.aiApply()
		if tick%timeSync == 0 {
			w.broadcastTime()
		}
	}
	for _, w := range worlds {
		w.flushChanges(w.playersIn(s.list))
	}
	for _, p := range s.list {
		p.flushAck()
	}
	for _, w := range worlds {
		w.snapshot(s.list)
	}
	s.parallel(s.list, (*Player).stream)
	if tick%latencyTick == 0 && len(s.list) > 0 {
		s.broadcastLatency()
	}
	if tick%bossTicks == 0 {
		select {
		case s.bossTick <- struct{}{}:
		default:
		}
	}
	if b := s.javaBridge(); b != nil {
		b.Tick(tick)
	}
}

func (w *World) playersIn(all []*Player) []*Player {
	out := all[:0:0]
	for _, p := range all {
		if p.world.Load() == w {
			out = append(out, p)
		}
	}
	return out
}

func (s *Server) broadcastLatency() {
	info := &packets.ClientboundPlayerInfoUpdate{Action: packets.ClientboundPlayerInfoUpdateActionUpdateLatency}
	for _, p := range s.list {
		info.Data = append(info.Data, packets.ClientboundPlayerInfoUpdateDataEntry{UUID: p.prof.UUID, Latency: int32(p.c.Ping().Milliseconds())})
	}
	raw := encode(info)
	for _, p := range s.list {
		p.c.SendRaw(raw)
	}
}

func (s *Server) consoleCommand(line string) {
	if s.bus.HasHandlers(api.KindServerCommand) {
		ev := &api.ServerCommandEvent{Sender: s.console, Line: line}
		if s.bus.Dispatch(ev).(*api.ServerCommandEvent).Cancelled() {
			return
		}
		line = ev.Line
	}
	s.cmds.Dispatch(s.console, line)
}

// trackEntry is one entity's state for this tick's tracking pass.
type trackEntry struct {
	trk  *entity.Tracked
	pos  api.Vec3
	upd  [][]byte
	self *Player
	uuid api.UUID
}

// snapshot computes per-entity update packets once per tick; viewers share them.
func (w *World) snapshot(all []*Player) {
	w.track = w.track[:0]
	for _, p := range all {
		if p.world.Load() != w || !p.joined.Load() {
			continue
		}
		p.mu.Lock()
		pos, yaw, pitch, head, og := p.e.Pos, p.e.Yaw, p.e.Pitch, p.e.HeadYaw, p.e.OnGround
		dirty := p.metaDirty
		p.metaDirty = false
		extra := p.extra
		p.extra = nil
		spectator := p.gm == api.Spectator
		p.mu.Unlock()
		if spectator {
			continue
		}
		if dirty {
			p.trk.SetMeta(p.playerMeta())
		}
		upd := p.trk.Update(pos, yaw, pitch, head, og)
		upd = append(upd, extra...)
		w.track = append(w.track, trackEntry{trk: &p.trk, pos: pos, upd: upd, self: p, uuid: p.prof.UUID})
	}
	for _, m := range w.mobList() {
		m.mu.Lock()
		e := m.e
		pos, yaw, pitch, head, og := e.Pos, e.Yaw, e.Pitch, e.HeadYaw, e.OnGround
		m.mu.Unlock()
		upd := m.trk.Update(pos, yaw, pitch, head, og)
		w.track = append(w.track, trackEntry{trk: &m.trk, pos: pos, upd: upd})
	}
	clear(w.trackSet)
	for i := range w.track {
		w.trackSet[w.track[i].trk.ID] = struct{}{}
	}
}

// stream sends chunks and entity updates to one player; runs in parallel
// after the world snapshot.
func (p *Player) stream() {
	if !p.joined.Load() || p.quit.Load() {
		return
	}
	w := p.world.Load()
	p.streamChunks(w)
	p.trackEntities(w)
}

func (p *Player) trackEntities(w *World) {
	p.mu.Lock()
	me := p.e.Pos
	p.mu.Unlock()
	rangeLimit := float64(p.vd * 16)
	var gone []int32
	var out [][]byte
	for i := range w.track {
		t := &w.track[i]
		if t.self == p {
			continue
		}
		r := min(t.trk.Range, rangeLimit)
		dx, dz := t.pos.X-me.X, t.pos.Z-me.Z
		in := dx*dx+dz*dz <= r*r
		if in && t.self != nil && !p.canSeeID(t.uuid) {
			in = false
		}
		_, has := p.tracking[t.trk.ID]
		switch {
		case has && in:
			out = append(out, t.upd...)
		case has:
			delete(p.tracking, t.trk.ID)
			gone = append(gone, t.trk.ID)
		case in:
			p.tracking[t.trk.ID] = struct{}{}
			out = append(out, t.trk.Spawn()...)
		}
	}
	for id := range p.tracking {
		if _, ok := w.trackSet[id]; !ok {
			delete(p.tracking, id)
			gone = append(gone, id)
		}
	}
	if len(gone) > 0 {
		out = append(out, entity.Despawn(gone))
	}
	p.c.SendRaws(out)
}
