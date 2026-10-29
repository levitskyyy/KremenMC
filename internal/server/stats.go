package server

import (
	"sync/atomic"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/metrics"
	mcnet "github.com/levitskyyy/KremenMC/internal/net"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

const (
	statsInterval = time.Second
	bossTicks     = 10
)

// statsSampler gathers api.ServerStats once per second. sample runs only on
// the stats goroutine; the latest snapshot is published atomically.
type statsSampler struct {
	s    *Server
	proc *metrics.Proc
	tps  *metrics.Window

	last     time.Time
	lastTick int64
	net      mcnet.Traffic
	busy     int64
	phase    int64
	gen      int64

	snap atomic.Pointer[api.ServerStats]
}

func newStatsSampler(s *Server) *statsSampler {
	return &statsSampler{s: s, proc: metrics.NewProc(), tps: metrics.NewWindow(15 * 60)}
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func (ss *statsSampler) sample(now time.Time) *api.ServerStats {
	s := ss.s
	st := &api.ServerStats{
		Sampled: now, Version: EngineVersion, GameVersion: packets.Version, Protocol: packets.Protocol,
		MaxPlayers: s.props.MaxPlayers, TPS: s.TPS(),
	}
	if !s.startTime.IsZero() {
		st.Uptime = now.Sub(s.startTime)
	}
	var dt float64
	if !ss.last.IsZero() {
		dt = now.Sub(ss.last).Seconds()
	}
	tick := s.tick.Load()
	if dt > 0 {
		ss.tps.Add(min(20, float64(tick-ss.lastTick)/dt))
	}
	ss.lastTick = tick
	st.TPS1m, st.TPS5m, st.TPS15m = ss.tps.Avg(60), ss.tps.Avg(300), ss.tps.Avg(900)
	if dt == 0 {
		st.TPS1m, st.TPS5m, st.TPS15m = st.TPS, st.TPS, st.TPS
	}
	avg, p95, peak := s.tickTimes.Stats()
	st.MSPT, st.MSPTP95, st.MSPTMax = ms(avg), ms(p95), ms(peak)

	ss.proc.Sample(now, st)

	var busy, phase, gen int64
	for _, w := range s.worldList() {
		ws := w.w.Stats()
		st.Regions += ws.Regions
		st.TickWorkers = max(st.TickWorkers, ws.TickWorkers)
		st.IOWorkers += ws.IOWorkers
		st.IOQueued += ws.IOQueued
		st.IOActive += ws.IOActive
		st.LoadedChunks += ws.Loaded
		busy += ws.BusyNanos
		phase += ws.PhaseNanos
		gen += ws.Generated
		w.mobsMu.RLock()
		st.Entities += len(w.mobs)
		w.mobsMu.RUnlock()
	}
	if db, dp := busy-ss.busy, phase-ss.phase; dp > 0 && db >= 0 && st.TickWorkers > 0 {
		st.Parallelism = float64(db) / float64(dp)
		st.WorkerBusy = min(1, st.Parallelism/float64(st.TickWorkers))
	}
	if dt > 0 && gen >= ss.gen {
		st.ChunksGenerated = float64(gen-ss.gen) / dt
	}
	ss.busy, ss.phase, ss.gen = busy, phase, gen

	st.Players = s.playerCount()
	st.Entities += st.Players

	if s.ln != nil {
		tr := s.ln.Traffic()
		if dt > 0 {
			st.NetIn = float64(tr.BytesIn-ss.net.BytesIn) / dt
			st.NetOut = float64(tr.BytesOut-ss.net.BytesOut) / dt
			st.PacketsIn = float64(tr.PacketsIn-ss.net.PacketsIn) / dt
			st.PacketsOut = float64(tr.PacketsOut-ss.net.PacketsOut) / dt
		}
		ss.net = tr
	}
	ss.last = now
	ss.snap.Store(st)
	return st
}

// latest returns the newest snapshot; never nil once the server started.
func (ss *statsSampler) latest() *api.ServerStats {
	if st := ss.snap.Load(); st != nil {
		return st
	}
	return &api.ServerStats{Version: EngineVersion, GameVersion: packets.Version, Protocol: packets.Protocol, TPS: ss.s.TPS()}
}

func (s *Server) playerCount() int {
	s.playersMu.RLock()
	defer s.playersMu.RUnlock()
	return len(s.players)
}

// Stats returns the most recent once-per-second sample.
func (s *Server) Stats() api.ServerStats {
	st := *s.stats.latest()
	st.CPUCores = append([]float64(nil), st.CPUCores...)
	return st
}

var _ api.StatsProvider = (*Server)(nil)

// statsLoop samples once per second and drives the admin bar: the sidebar
// on every sample, the boss bar every bossTicks ticks.
func (s *Server) statsLoop() {
	t := time.NewTicker(statsInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-t.C:
			s.bar.refresh(s.stats.sample(now))
		case <-s.bossTick:
			s.bar.updateBoss()
		}
	}
}
