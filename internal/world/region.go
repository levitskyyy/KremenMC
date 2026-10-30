package world

import (
	"container/heap"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// Region owns up to 8x8 chunks. Its chunk set, scheduled ticks and RNG are
// touched only by the tick goroutine between phases or by the worker
// running the region's phase.
type Region struct {
	Pos    RegionPos
	chunks map[uint64]*Chunk
	stitch []*Chunk
	ticks  tickHeap
	seq    uint64
	// fluidPending dedups scheduled fluid ticks by position.
	fluidPending map[uint64]struct{}
	running      []scheduled
	rng          *rand.Rand

	inboxMu sync.Mutex
	inbox   []inboxMsg
}

// inboxMsg is a queued change with the tick it was submitted at, so ticks
// it schedules count from the moment of the request.
type inboxMsg struct {
	fn func(*TickContext)
	at int64
}

func (w *World) region(rp RegionPos, create bool) *Region {
	k := rp.Key()
	w.regMu.RLock()
	r := w.regions[k]
	w.regMu.RUnlock()
	if r != nil || !create {
		return r
	}
	w.regMu.Lock()
	defer w.regMu.Unlock()
	if r = w.regions[k]; r == nil {
		seed := uint64(w.cfg.Seed) ^ k*0x9E3779B97F4A7C15
		r = &Region{Pos: rp, chunks: map[uint64]*Chunk{}, rng: rand.New(rand.NewPCG(seed, seed>>17|1))}
		w.regions[k] = r
	}
	return r
}

// Submit queues fn to run in the phase of the region owning block x,z.
// Safe from any goroutine; applied at that region's next phase start.
func (w *World) Submit(x, z int, fn func(*TickContext)) {
	rp := ChunkAt(x, z).Region()
	for {
		// hold regMu so applyUnloads cannot drop the region mid-append
		w.regMu.RLock()
		if r := w.regions[rp.Key()]; r != nil {
			r.inboxMu.Lock()
			r.inbox = append(r.inbox, inboxMsg{fn, w.tick.Load()})
			r.inboxMu.Unlock()
			w.regMu.RUnlock()
			return
		}
		w.regMu.RUnlock()
		w.region(rp, true)
	}
}

// SetBlock queues a block change through the owning region.
func (w *World) SetBlock(x, y, z int, s data.StateID) {
	w.Submit(x, z, func(t *TickContext) { t.SetBlock(x, y, z, s) })
}

// TickContext is handed to code running inside a region phase.
type TickContext struct {
	w     *World
	r     *Region
	light *LightEngine
	at    int64 // submission tick of the inbox message being applied, or 0
	depth int
}

func (t *TickContext) World() *World    { return t.w }
func (t *TickContext) Region() *Region  { return t.r }
func (t *TickContext) Tick() int64      { return t.w.tick.Load() }
func (t *TickContext) Rand() *rand.Rand { return t.r.rng }

// schedNow is the tick scheduled delays count from.
func (t *TickContext) schedNow() int64 {
	if t.at != 0 {
		return t.at
	}
	return t.Tick()
}

func (t *TickContext) near(rp RegionPos) bool {
	dx, dz := rp.X-t.r.Pos.X, rp.Z-t.r.Pos.Z
	return dx >= -1 && dx <= 1 && dz >= -1 && dz <= 1
}

// Block reads a block. Reads within this or adjacent regions are lock-free.
func (t *TickContext) Block(x, y, z int) data.StateID {
	p := ChunkAt(x, z)
	c := t.w.Chunk(p)
	if c == nil {
		return data.Air
	}
	if t.near(p.Region()) {
		return c.Block(x&15, y, z&15)
	}
	c.mu.RLock()
	s := c.Block(x&15, y, z&15)
	c.mu.RUnlock()
	return s
}

// SetBlock applies a change immediately when x,z belong to this region
// (updating light and notifying listeners) and otherwise forwards it to the
// owning region's inbox. It returns false if applied to an unloaded chunk.
func (t *TickContext) SetBlock(x, y, z int, s data.StateID) bool {
	p := ChunkAt(x, z)
	if p.Region() != t.r.Pos {
		t.w.SetBlock(x, y, z, s)
		return true
	}
	c := t.r.chunks[p.Key()]
	if c == nil || !c.Dim.Contains(y) {
		return false
	}
	old := c.SetBlock(x&15, y, z&15, s)
	if old == s {
		return true
	}
	if data.LightEmission(old) != data.LightEmission(s) || data.LightFilter(old) != data.LightFilter(s) {
		t.light.Update(x, y, z)
	}
	if f := t.w.cfg.Hooks.BlockChanged; f != nil {
		f(x, y, z, old, s)
	}
	t.fluidChanged(x, y, z, s)
	if f := t.w.cfg.Hooks.ShapeUpdate; f != nil && t.depth < MaxUpdateDepth {
		t.depth++
		f(t, x, y, z, old, s)
		t.depth--
	}
	return true
}

// ScheduleTick queues a block tick after delay ticks; it fires only if the
// block at the position is still the same block type.
func (t *TickContext) ScheduleTick(x, y, z int, delay int) {
	p := ChunkAt(x, z)
	st := t.Block(x, y, z)
	if p.Region() != t.r.Pos {
		due := t.schedNow() + int64(max(delay, 1))
		t.w.Submit(x, z, func(o *TickContext) { o.r.schedule(x, y, z, due, data.Canonical().BlockID(st)) })
		return
	}
	t.r.schedule(x, y, z, t.schedNow()+int64(max(delay, 1)), data.Canonical().BlockID(st))
}

func (r *Region) schedule(x, y, z int, due int64, b data.BlockID) {
	r.seq++
	heap.Push(&r.ticks, scheduled{due: due, seq: r.seq, x: int32(x), y: int32(y), z: int32(z), block: b})
}

type scheduled struct {
	due     int64
	seq     uint64
	x, y, z int32
	block   data.BlockID
	fluid   FluidKind // non-zero: a fluid tick, checked against the fluid kind
}

type tickHeap []scheduled

func (h tickHeap) Len() int { return len(h) }
func (h tickHeap) Less(i, j int) bool {
	if h[i].due != h[j].due {
		return h[i].due < h[j].due
	}
	return h[i].seq < h[j].seq
}
func (h tickHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *tickHeap) Push(x any)   { *h = append(*h, x.(scheduled)) }
func (h *tickHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

// DoTick advances the world one tick: install loaded chunks, run the four
// checkerboard phases in parallel, then unload chunks without tickets.
func (w *World) DoTick() {
	w.tickMu.Lock()
	defer w.tickMu.Unlock()
	if w.closed.Load() {
		return
	}
	w.tick.Add(1)
	w.applyInstalls()

	w.regMu.RLock()
	var phases [4][]*Region
	for _, r := range w.regions {
		phases[r.Pos.Color()] = append(phases[r.Pos.Color()], r)
	}
	w.regMu.RUnlock()

	total := 0
	for _, rs := range phases {
		if len(rs) == 0 {
			continue
		}
		total += len(rs)
		start := time.Now()
		n := min(len(rs), w.cfg.TickWorkers)
		var next atomic.Int64
		var wg sync.WaitGroup
		wg.Add(n)
		for range n {
			go func() {
				defer wg.Done()
				t0 := time.Now()
				t := w.ctxPool.Get().(*TickContext)
				for {
					i := next.Add(1) - 1
					if int(i) >= len(rs) {
						break
					}
					t.r = rs[i]
					w.tickRegion(t)
				}
				t.r = nil
				w.ctxPool.Put(t)
				w.busyNs.Add(int64(time.Since(t0)))
			}()
		}
		wg.Wait()
		w.phaseNs.Add(int64(time.Since(start)))
	}
	w.ticked.Store(int32(total))
	w.applyUnloads()
}

// Stats is a cumulative snapshot of a world's scheduler and IO counters.
type Stats struct {
	Regions     int // regions ticked in the last tick
	TickWorkers int
	// BusyNanos is total worker time spent ticking regions; PhaseNanos is
	// the wall time of all checkerboard phases. BusyNanos/PhaseNanos is the
	// average number of workers running concurrently.
	BusyNanos  int64
	PhaseNanos int64
	IOWorkers  int
	IOQueued   int
	IOActive   int
	IODone     int64
	Generated  int64
	Loaded     int
}

func (w *World) Stats() Stats {
	return Stats{
		Regions:     int(w.ticked.Load()),
		TickWorkers: w.cfg.TickWorkers,
		BusyNanos:   w.busyNs.Load(),
		PhaseNanos:  w.phaseNs.Load(),
		IOWorkers:   w.io.n,
		IOQueued:    w.io.queued(),
		IOActive:    int(w.io.active.Load()),
		IODone:      w.io.done.Load(),
		Generated:   w.genCount.Load(),
		Loaded:      w.Loaded(),
	}
}

func (w *World) applyInstalls() {
	w.installMu.Lock()
	ins := w.installs
	w.installs = nil
	w.installMu.Unlock()
	for _, c := range ins {
		if w.Chunk(c.Pos()) != c {
			continue
		}
		r := w.region(c.Pos().Region(), true)
		r.chunks[c.Pos().Key()] = c
		r.stitch = append(r.stitch, c)
	}
}

func (w *World) tickRegion(t *TickContext) {
	r := t.r
	for _, c := range r.stitch {
		t.light.Stitch(c.X, c.Z)
		if f := w.cfg.Hooks.ChunkLoaded; f != nil {
			f(c)
		}
	}
	r.stitch = r.stitch[:0]

	r.inboxMu.Lock()
	msgs := r.inbox
	r.inbox = nil
	r.inboxMu.Unlock()
	for _, m := range msgs {
		t.at = m.at
		m.fn(t)
	}
	t.at = 0

	now := w.tick.Load()
	// Due ticks are taken out first, so a position whose tick runs this
	// tick can be scheduled again while the batch runs; block ticks run
	// before fluid ticks.
	run := r.running[:0]
	for r.ticks.Len() > 0 && r.ticks[0].due <= now {
		s := heap.Pop(&r.ticks).(scheduled)
		if s.fluid != FluidNone {
			delete(r.fluidPending, fluidKey(int(s.x), int(s.y), int(s.z)))
		}
		run = append(run, s)
	}
	bt := w.cfg.Hooks.BlockTick
	for _, s := range run {
		if s.fluid == FluidNone && bt != nil {
			if st := t.Block(int(s.x), int(s.y), int(s.z)); data.Canonical().BlockID(st) == s.block {
				bt(t, int(s.x), int(s.y), int(s.z), st)
			}
		}
	}
	for _, s := range run {
		if s.fluid != FluidNone {
			if st := t.Block(int(s.x), int(s.y), int(s.z)); fluidData().of(st).kind() == s.fluid {
				t.fluidTick(int(s.x), int(s.y), int(s.z), st)
			}
		}
	}
	r.running = run[:0]

	if rt := w.cfg.Hooks.RandomTick; rt != nil && w.cfg.RandomTickSpeed > 0 {
		for _, c := range r.chunks {
			bx, bz := int(c.X)<<4, int(c.Z)<<4
			for si := range c.Sections {
				s := &c.Sections[si]
				if s.Empty() {
					continue
				}
				y0 := c.Dim.MinY + si<<4
				for range w.cfg.RandomTickSpeed {
					i := r.rng.IntN(4096)
					st := data.StateID(s.Blocks.Get(i))
					if data.IsAir(st) {
						continue
					}
					rt(t, bx+i&15, y0+i>>8, bz+i>>4&15, st)
				}
			}
		}
	}
	if f := w.cfg.Hooks.RegionTick; f != nil {
		f(t)
	}
}

func (w *World) applyUnloads() {
	w.ticketMu.Lock()
	var keys []uint64
	for k := range w.unloads {
		if w.refs[k] == 0 {
			keys = append(keys, k)
		}
	}
	clear(w.unloads)
	w.ticketMu.Unlock()

	for _, k := range keys {
		p := PosFromKey(k)
		w.loadMu.Lock()
		s := w.shard(k)
		s.mu.Lock()
		c := s.m[k]
		delete(s.m, k)
		s.mu.Unlock()
		if c == nil {
			w.loadMu.Unlock()
			continue
		}
		if r := w.region(p.Region(), false); r != nil {
			delete(r.chunks, k)
			for i, sc := range r.stitch {
				if sc == c {
					r.stitch = append(r.stitch[:i], r.stitch[i+1:]...)
					break
				}
			}
		}
		if w.cfg.Storage != nil && c.clearDirty() {
			o := &op{done: make(chan struct{}), c: c, save: true}
			w.inflight[k] = o
			if !w.io.submit(func() { w.save(k, o) }) {
				delete(w.inflight, k)
				close(o.done)
			}
		}
		w.loadMu.Unlock()
		if f := w.cfg.Hooks.ChunkUnloaded; f != nil {
			f(c)
		}
	}
	w.regMu.Lock()
	for k, r := range w.regions {
		if len(r.chunks) == 0 && len(r.stitch) == 0 && r.ticks.Len() == 0 {
			r.inboxMu.Lock()
			empty := len(r.inbox) == 0
			r.inboxMu.Unlock()
			if empty {
				delete(w.regions, k)
			}
		}
	}
	w.regMu.Unlock()
}

func (w *World) save(k uint64, o *op) {
	if err := w.cfg.Storage.SaveChunk(o.c); err != nil {
		o.err = err
		w.cfg.Logf("world: save chunk %d,%d: %v", o.c.X, o.c.Z, err)
	}
	w.loadMu.Lock()
	if w.inflight[k] == o {
		delete(w.inflight, k)
	}
	w.loadMu.Unlock()
	close(o.done)
}
