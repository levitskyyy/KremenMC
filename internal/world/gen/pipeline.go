package gen

import (
	"container/heap"
	"container/list"
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/levitskyyy/KremenMC/internal/world"
)

// Storage persists proto-chunks with their status. LoadProto returns nil,
// nil for absent chunks. Implementations must be safe for concurrent use;
// the pipeline never loads or saves the same position concurrently.
type Storage interface {
	LoadProto(pos world.ChunkPos) (*ProtoChunk, error)
	SaveProto(c *ProtoChunk) error
}

type Config struct {
	Seed int64
	Dim  world.Dimension
	// Workers is the number of generation goroutines (default GOMAXPROCS).
	Workers int
	// Stages by target status; statuses without a stage pass through.
	Stages []Stage
	// Storage is optional. With storage, idle chunks beyond CacheSize are
	// saved and dropped; Close saves everything dirty.
	Storage   Storage
	CacheSize int // default 8192
	// MaxRequests bounds concurrently outstanding Request/Get calls;
	// further calls block until a slot frees or their context ends.
	// 0 means unbounded.
	MaxRequests int
	Logf        func(format string, args ...any)
}

var ErrClosed = errors.New("gen: pipeline closed")

type jobState uint8

const (
	jWaiting jobState = iota
	jReady
	jRunning
	jDone
	jFailed
	jCancelled
)

type job struct {
	e     *entry
	st    Status
	prio  uint64
	state jobState
	// expanded is false while the job only waits for its chunk to load.
	expanded   bool
	refs       int32
	pending    int32
	deps       []*job
	dependents []*job
	zone       int // pinned radius once expanded, -1 before
	heapIdx    int
	done       chan struct{}
	chunk      *ProtoChunk
	err        error
	depErr     error
	// extern is the number of MaxRequests slots held by requests on j.
	extern int32
	// loadDirty marks a chunk recovered from a failed eviction save.
	loadDirty bool
	// skip marks a step on a sealed chunk: only the status advances.
	skip bool
}

func (j *job) finished() bool { return j.state >= jDone }

type entry struct {
	pos    world.ChunkPos
	chunk  *ProtoChunk
	status Status
	pins   int32
	jobs   [NumStatuses]*job
	dirty  bool
	err    error
	// failed keeps per-status failures (a failed dependency is permanent,
	// since e.err is) so dependents are not rebuilt over and over.
	failed *[NumStatuses]error
	lru    *list.Element
}

func (e *entry) liveJobs() bool {
	for _, j := range e.jobs {
		if j != nil {
			return true
		}
	}
	return false
}

type saving struct {
	e    *entry
	done chan struct{}
	err  error
}

// Pipeline schedules generation steps on a worker pool. All scheduling
// state is guarded by mu; stages run without it.
type Pipeline struct {
	cfg    Config
	stages [NumStatuses]Stage

	mu      sync.Mutex
	cond    *sync.Cond // workers
	quiet   *sync.Cond // Flush waiting for running == 0
	entries map[world.ChunkPos]*entry
	ready   jobHeap
	saveQ   []*saving
	saving  map[world.ChunkPos]*saving
	lru     list.List
	loaded  int
	// sealed holds every position that reached StatusFull. Such a chunk
	// was handed to the world; if it is evicted and later needed again as
	// a neighbour, its steps from features on only advance the status, so
	// feature writes are never replayed into neighbours (see doc.go).
	sealed  map[world.ChunkPos]struct{}
	seq     uint64
	closed  bool
	saveErr []error
	running int // steps executing outside mu
	paused  int // Flush in progress: no new steps start

	sem   chan struct{}
	wg    sync.WaitGroup
	stats counters
}

type counters struct {
	steps     [NumStatuses]atomic.Int64
	loads     atomic.Int64
	saves     atomic.Int64
	evictions atomic.Int64
	cancelled atomic.Int64
	farReads  atomic.Int64
	farWrites atomic.Int64
	busyNs    atomic.Int64
}

// New starts a pipeline. Stages are indexed by their Status; duplicates
// are an error.
func New(cfg Config) (*Pipeline, error) {
	if cfg.Dim == (world.Dimension{}) {
		cfg.Dim = world.Overworld
	}
	if err := cfg.Dim.Validate(); err != nil {
		return nil, err
	}
	if cfg.Workers <= 0 {
		cfg.Workers = runtime.GOMAXPROCS(0)
	}
	if cfg.CacheSize <= 0 {
		cfg.CacheSize = 8192
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	p := &Pipeline{
		cfg:     cfg,
		entries: map[world.ChunkPos]*entry{},
		saving:  map[world.ChunkPos]*saving{},
		sealed:  map[world.ChunkPos]struct{}{},
	}
	for _, s := range cfg.Stages {
		st := s.Status()
		if int(st) >= NumStatuses || st == StatusEmpty {
			return nil, fmt.Errorf("gen: stage for invalid status %v", st)
		}
		if p.stages[st] != nil {
			return nil, fmt.Errorf("gen: duplicate stage for %v", st)
		}
		p.stages[st] = s
	}
	if cfg.MaxRequests > 0 {
		p.sem = make(chan struct{}, cfg.MaxRequests)
	}
	p.cond = sync.NewCond(&p.mu)
	p.quiet = sync.NewCond(&p.mu)
	p.wg.Add(cfg.Workers)
	for i := range cfg.Workers {
		go p.worker(i)
	}
	return p, nil
}

func (p *Pipeline) Config() Config { return p.cfg }

// Request is an outstanding interest in a chunk reaching a status. It keeps
// the work alive until it completes or Cancel is called.
type Request struct {
	p     *Pipeline
	j     *job
	done  <-chan struct{}
	chunk *ProtoChunk
	err   error
	ended bool // guarded by p.mu
}

var closedChan = func() chan struct{} { c := make(chan struct{}); close(c); return c }()

// Request asks for pos to reach status st. It blocks only for a
// MaxRequests slot. The caller must call Cancel or wait for Done; Cancel
// after completion is a no-op.
func (p *Pipeline) Request(ctx context.Context, pos world.ChunkPos, st Status) (*Request, error) {
	if int(st) >= NumStatuses {
		return nil, fmt.Errorf("gen: bad status %d", st)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.sem != nil {
		select {
		case p.sem <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	r := &Request{p: p}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		p.releaseSlots(1)
		return nil, ErrClosed
	}
	if e := p.entries[pos]; e != nil && e.err != nil {
		p.releaseSlots(1)
		return nil, e.err
	}
	if st == StatusFull {
		if e := p.entries[pos]; e == nil || e.chunk == nil || e.status < StatusFull {
			delete(p.sealed, pos) // the world asks again: generate it for real
		}
	}
	p.seq++
	j := p.ensure(pos, st, p.seq)
	if j == nil {
		e := p.entries[pos]
		r.chunk, r.done, r.ended = e.chunk, closedChan, true
		p.touch(e)
		p.releaseSlots(1)
		return r, nil
	}
	if j.state == jFailed {
		p.releaseSlots(1)
		return nil, j.err
	}
	j.refs++
	if p.sem != nil {
		j.extern++
	}
	if j.done == nil {
		j.done = make(chan struct{})
	}
	r.j, r.done = j, j.done
	return r, nil
}

func (p *Pipeline) releaseSlots(n int32) {
	if p.sem == nil {
		return
	}
	for range n {
		<-p.sem
	}
}

// ended releases a finished job's request slots.
func (p *Pipeline) ended(j *job) {
	p.releaseSlots(j.extern)
	j.extern = 0
	if j.done != nil {
		close(j.done)
	}
}

// Done is closed when the request completed, failed or was cancelled.
func (r *Request) Done() <-chan struct{} { return r.done }

// Result returns the chunk once Done is closed.
func (r *Request) Result() (*ProtoChunk, error) {
	<-r.done
	if r.j == nil {
		return r.chunk, r.err
	}
	r.p.mu.Lock()
	defer r.p.mu.Unlock()
	if !r.ended {
		r.ended = true
		switch r.j.state {
		case jDone:
			r.chunk = r.j.chunk
		case jFailed:
			r.err = r.j.err
		default:
			r.err = context.Canceled
		}
	}
	if r.err != nil {
		return nil, r.err
	}
	return r.chunk, nil
}

// Cancel drops the request's interest. Work nobody else needs and that
// has not started is abandoned; running steps finish and their results
// are kept.
func (r *Request) Cancel() {
	if r.j == nil {
		return
	}
	r.p.mu.Lock()
	defer r.p.mu.Unlock()
	if r.ended || r.j.finished() {
		return
	}
	r.ended, r.err = true, context.Canceled
	if r.p.sem != nil {
		r.j.extern--
		r.p.releaseSlots(1)
	}
	r.p.release(r.j)
}

// Get generates pos up to st and returns the chunk. If ctx ends first the
// request is cancelled. The chunk's content is stable from
// StatusInitializeLight on; below that, later neighbour feature steps may
// still modify it.
func (p *Pipeline) Get(ctx context.Context, pos world.ChunkPos, st Status) (*ProtoChunk, error) {
	r, err := p.Request(ctx, pos, st)
	if err != nil {
		return nil, err
	}
	select {
	case <-r.Done():
		return r.Result()
	case <-ctx.Done():
		r.Cancel()
		select {
		case <-r.Done():
			if c, err := r.Result(); err == nil {
				return c, nil
			}
		default:
		}
		return nil, ctx.Err()
	}
}

// Peek returns the in-memory chunk at pos, if any, without generating.
func (p *Pipeline) Peek(pos world.ChunkPos) *ProtoChunk {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e := p.entries[pos]; e != nil {
		return e.chunk
	}
	return nil
}

func (p *Pipeline) entryAt(pos world.ChunkPos) *entry {
	e := p.entries[pos]
	if e == nil {
		e = &entry{pos: pos}
		p.entries[pos] = e
	}
	return e
}

// ensure returns the live job producing st at pos, creating it and its
// dependencies, or nil if the chunk is loaded and already at st.
func (p *Pipeline) ensure(pos world.ChunkPos, st Status, prio uint64) *job {
	e := p.entryAt(pos)
	if e.err != nil {
		return &job{e: e, st: st, state: jFailed, err: e.err, zone: -1}
	}
	if e.failed != nil && e.failed[st] != nil {
		return &job{e: e, st: st, state: jFailed, err: e.failed[st], zone: -1}
	}
	if e.chunk != nil && e.status >= st {
		return nil
	}
	if j := e.jobs[st]; j != nil {
		return j
	}
	j := &job{e: e, st: st, prio: prio, zone: -1, heapIdx: -1}
	e.jobs[st] = j
	p.untouch(e)
	if st == StatusEmpty {
		j.expanded = true
		p.makeReady(j)
		return j
	}
	if e.chunk == nil {
		p.addDep(j, p.ensure(pos, StatusEmpty, prio))
		if j.depErr != nil {
			p.fail(j, j.depErr)
		}
		return j
	}
	p.expand(j)
	if j.depErr != nil {
		p.fail(j, j.depErr)
	} else if j.pending == 0 {
		p.makeReady(j)
	}
	return j
}

func (p *Pipeline) addDep(j, d *job) {
	if d == nil {
		return
	}
	if d.state == jFailed {
		if j.depErr == nil {
			j.depErr = d.err
		}
		return
	}
	d.refs++
	j.deps = append(j.deps, d)
	d.dependents = append(d.dependents, j)
	j.pending++
}

// expand adds the dependencies of a job whose chunk is loaded below j.st:
// the parent status, vanilla's direct dependencies, the fixed decoration
// order and sealing (see doc.go), and pins every chunk the stage may touch.
func (p *Pipeline) expand(j *job) {
	j.expanded = true
	pos, st := j.e.pos, j.st
	step := &Pyramid[st]
	p.addDep(j, p.ensure(pos, st.Parent(), j.prio))
	if _, ok := p.sealed[pos]; ok && st >= StatusFeatures {
		j.skip = true
		j.zone = 0
		e := p.entryAt(pos)
		e.pins++
		p.untouch(e)
		return
	}
	for d := 1; d < len(step.Direct); d++ {
		req := step.Direct[d]
		forRing(pos, int32(d), func(n world.ChunkPos) { p.addDep(j, p.ensure(n, req, j.prio)) })
	}
	if step.WriteRadius > 0 {
		forSquare(pos, conflictRadius, func(n world.ChunkPos) {
			if n != pos && decoratesBefore(n, pos) {
				p.addDep(j, p.ensure(n, st, j.prio))
			}
		})
	}
	if prev := &Pyramid[st.Parent()]; prev.WriteRadius > 0 {
		forSquare(pos, int32(prev.WriteRadius), func(n world.ChunkPos) {
			if n != pos {
				p.addDep(j, p.ensure(n, prev.Target, j.prio))
			}
		})
	}
	j.zone = max(step.Radius(), 0)
	forSquare(pos, int32(j.zone), func(n world.ChunkPos) {
		e := p.entryAt(n)
		e.pins++
		p.untouch(e)
	})
}

func (p *Pipeline) unpin(j *job) {
	if j.zone < 0 {
		return
	}
	forSquare(j.e.pos, int32(j.zone), func(n world.ChunkPos) {
		e := p.entries[n]
		e.pins--
		p.settle(e)
	})
	j.zone = -1
}

func (p *Pipeline) makeReady(j *job) {
	j.state = jReady
	heap.Push(&p.ready, j)
	p.cond.Signal()
}

// complete marks j done and advances its dependents.
func (p *Pipeline) complete(j *job) {
	e := j.e
	j.state = jDone
	j.chunk = e.chunk
	if e.jobs[j.st] == j {
		e.jobs[j.st] = nil
	}
	p.unpin(j)
	p.ended(j)
	j.deps = nil
	deps := j.dependents
	j.dependents = nil
	for _, d := range deps {
		if d.state != jWaiting {
			continue
		}
		d.pending--
		if d.pending == 0 {
			p.advance(d)
		}
	}
	p.settle(e)
}

func (p *Pipeline) advance(j *job) {
	if !j.expanded {
		if j.e.chunk == nil {
			p.fail(j, fmt.Errorf("gen: chunk %d,%d not loaded", j.e.pos.X, j.e.pos.Z))
			return
		}
		if j.e.status >= j.st {
			p.complete(j)
			return
		}
		p.expand(j)
		if j.depErr != nil {
			p.fail(j, j.depErr)
			return
		}
		if j.pending > 0 {
			return
		}
	}
	p.makeReady(j)
}

// fail fails j and, transitively, everything waiting on it.
func (p *Pipeline) fail(j *job, err error) {
	work := []*job{j}
	for len(work) > 0 {
		j := work[len(work)-1]
		work = work[:len(work)-1]
		if j.finished() {
			continue
		}
		if j.state == jReady {
			heap.Remove(&p.ready, j.heapIdx)
		}
		j.state, j.err = jFailed, err
		e := j.e
		if e.failed == nil {
			e.failed = new([NumStatuses]error)
		}
		e.failed[j.st] = err
		if e.jobs[j.st] == j {
			e.jobs[j.st] = nil
		}
		p.unpin(j)
		p.ended(j)
		for _, d := range j.deps {
			p.release(d)
		}
		j.deps = nil
		for _, d := range j.dependents {
			if d.state == jWaiting {
				work = append(work, d)
			}
		}
		j.dependents = nil
		p.settle(e)
	}
}

// release drops one reference to j and abandons it if it is no longer
// needed and has not started.
func (p *Pipeline) release(j *job) {
	work := []*job{j}
	for len(work) > 0 {
		j := work[len(work)-1]
		work = work[:len(work)-1]
		j.refs--
		if j.refs > 0 || (j.state != jWaiting && j.state != jReady) {
			continue
		}
		if j.state == jReady {
			heap.Remove(&p.ready, j.heapIdx)
		}
		j.state = jCancelled
		p.stats.cancelled.Add(1)
		e := j.e
		if e.jobs[j.st] == j {
			e.jobs[j.st] = nil
		}
		p.unpin(j)
		p.ended(j)
		work = append(work, j.deps...)
		j.deps = nil
		j.dependents = nil
		p.settle(e)
	}
}

// settle files an unused entry: loaded ones become evictable, unloaded
// ones are forgotten.
func (p *Pipeline) settle(e *entry) {
	if e.pins > 0 || e.liveJobs() || p.entries[e.pos] != e {
		return
	}
	if e.chunk == nil {
		if p.entries[e.pos] == e && e.err == nil && e.failed == nil {
			delete(p.entries, e.pos)
		}
		return
	}
	p.touch(e)
	p.evict()
}

func (p *Pipeline) touch(e *entry) {
	if e.pins > 0 || e.liveJobs() || e.chunk == nil || p.entries[e.pos] != e {
		return
	}
	if e.lru != nil {
		p.lru.MoveToBack(e.lru)
	} else {
		e.lru = p.lru.PushBack(e)
	}
}

func (p *Pipeline) untouch(e *entry) {
	if e.lru != nil {
		p.lru.Remove(e.lru)
		e.lru = nil
	}
}

func (p *Pipeline) evict() {
	if p.cfg.Storage == nil {
		return
	}
	for tries := p.lru.Len(); p.loaded > p.cfg.CacheSize && p.lru.Len() > 0 && tries > 0; tries-- {
		if e := p.lru.Front().Value.(*entry); e.status == StatusFull && p.loaded <= 2*p.cfg.CacheSize && !p.neighboursSealed(e.pos) {
			// A neighbour's light step may still read it; past twice the
			// cache size it goes anyway (the stand-in only affects light).
			p.lru.MoveToBack(e.lru)
			continue
		}
		e := p.lru.Remove(p.lru.Front()).(*entry)
		e.lru = nil
		delete(p.entries, e.pos)
		p.loaded--
		p.stats.evictions.Add(1)
		if !e.dirty {
			continue
		}
		s := &saving{e: e, done: make(chan struct{})}
		p.saving[e.pos] = s
		p.saveQ = append(p.saveQ, s)
		p.cond.Signal()
	}
}

func (p *Pipeline) neighboursSealed(pos world.ChunkPos) bool {
	for dz := int32(-1); dz <= 1; dz++ {
		for dx := int32(-1); dx <= 1; dx++ {
			if _, ok := p.sealed[world.ChunkPos{X: pos.X + dx, Z: pos.Z + dz}]; !ok {
				return false
			}
		}
	}
	return true
}

func (p *Pipeline) worker(id int) {
	defer p.wg.Done()
	sc := &StageContext{}
	p.mu.Lock()
	for {
		for (len(p.ready) == 0 && len(p.saveQ) == 0 && !p.closed) || (p.paused > 0 && len(p.saveQ) == 0) {
			p.cond.Wait()
		}
		if len(p.saveQ) > 0 {
			s := p.saveQ[0]
			p.saveQ = p.saveQ[1:]
			p.mu.Unlock()
			s.err = p.cfg.Storage.SaveProto(s.e.chunk)
			p.stats.saves.Add(1)
			p.mu.Lock()
			if s.err != nil {
				p.saveErr = append(p.saveErr, s.err)
				p.cfg.Logf("gen: save chunk %d,%d: %v", s.e.pos.X, s.e.pos.Z, s.err)
			}
			if p.saving[s.e.pos] == s {
				delete(p.saving, s.e.pos)
			}
			close(s.done)
			if len(p.saving) == 0 {
				p.quiet.Broadcast()
			}
			continue
		}
		if len(p.ready) == 0 {
			break
		}
		j := heap.Pop(&p.ready).(*job)
		j.state = jRunning
		p.running++
		var pending, skipped = (*saving)(nil), (*ProtoChunk)(nil)
		if j.st == StatusEmpty {
			pending = p.saving[j.e.pos]
		} else if j.skip {
			skipped = j.e.chunk
		} else {
			p.prepare(sc, j, id)
		}
		p.mu.Unlock()
		var err error
		var loaded *ProtoChunk
		t0 := time.Now()
		if j.st == StatusEmpty {
			loaded, err = p.load(j.e.pos, pending)
			j.loadDirty = pending != nil && pending.err != nil
		} else if skipped != nil {
			skipped.SetStatus(j.st)
		} else {
			err = p.run(sc, j)
			sc.clear()
		}
		p.stats.busyNs.Add(int64(time.Since(t0)))
		p.mu.Lock()
		p.running--
		if p.running == 0 && p.paused > 0 {
			p.quiet.Broadcast()
		}
		p.finish(j, loaded, err)
	}
	p.mu.Unlock()
}

func (p *Pipeline) prepare(sc *StageContext, j *job, worker int) {
	step := &Pyramid[j.st]
	sc.reset(p, step, j.e.pos, worker)
	r := int32(sc.r)
	i := 0
	for dz := -r; dz <= r; dz++ {
		for dx := -r; dx <= r; dx++ {
			sc.grid[i] = p.entries[world.ChunkPos{X: j.e.pos.X + dx, Z: j.e.pos.Z + dz}].chunk
			i++
		}
	}
}

func (p *Pipeline) load(pos world.ChunkPos, pending *saving) (*ProtoChunk, error) {
	if pending != nil {
		<-pending.done
		return pending.e.chunk, nil
	}
	p.stats.loads.Add(1)
	if p.cfg.Storage != nil {
		c, err := p.cfg.Storage.LoadProto(pos)
		if err != nil {
			return nil, fmt.Errorf("gen: load chunk %d,%d: %w", pos.X, pos.Z, err)
		}
		if c != nil {
			if c.X != pos.X || c.Z != pos.Z || c.Dim != p.cfg.Dim {
				return nil, fmt.Errorf("gen: stored chunk %d,%d is %d,%d %+v", pos.X, pos.Z, c.X, c.Z, c.Dim)
			}
			return c, nil
		}
	}
	return NewProtoChunk(pos, p.cfg.Dim), nil
}

func (p *Pipeline) run(sc *StageContext, j *job) error {
	c := sc.Chunk()
	if s := p.stages[j.st]; s != nil {
		if err := runStage(s, sc); err != nil {
			return err
		}
	}
	c.SetStatus(j.st)
	p.stats.steps[j.st].Add(1)
	return nil
}

func (p *Pipeline) finish(j *job, loaded *ProtoChunk, err error) {
	e := j.e
	if err != nil {
		e.err = err
		if w := Pyramid[j.st].WriteRadius; w > 0 {
			forSquare(e.pos, int32(w), func(n world.ChunkPos) { p.entries[n].err = err })
		}
		p.fail(j, err)
		return
	}
	if j.st == StatusEmpty {
		e.chunk, e.status = loaded, loaded.Status()
		e.dirty = j.loadDirty
		p.loaded++
		if e.status == StatusFull {
			p.sealed[e.pos] = struct{}{}
		}
	} else {
		e.status = j.st
		_, sealed := p.sealed[e.pos]
		// A sealed chunk rebuilt below full is a stand-in; never let it
		// overwrite the stored (world-owned) chunk.
		e.dirty = !sealed || j.st == StatusFull
		if j.st == StatusFull {
			p.sealed[e.pos] = struct{}{}
		}
		if w := Pyramid[j.st].WriteRadius; w > 0 && !j.skip {
			forSquare(e.pos, int32(w), func(n world.ChunkPos) { p.entries[n].dirty = true })
		}
	}
	p.complete(j)
}

// Flush saves every dirty in-memory chunk. Steps are paused meanwhile;
// pending work resumes afterwards.
func (p *Pipeline) Flush() error {
	if p.cfg.Storage == nil {
		return nil
	}
	p.mu.Lock()
	p.paused++
	for p.running > 0 || len(p.saving) > 0 {
		p.quiet.Wait()
	}
	var es []*entry
	for _, e := range p.entries {
		if e.chunk != nil && e.dirty && e.err == nil {
			e.dirty = false
			es = append(es, e)
		}
	}
	p.mu.Unlock()
	err := p.saveAll(es)
	p.mu.Lock()
	p.paused--
	p.cond.Broadcast()
	p.mu.Unlock()
	return err
}

func (p *Pipeline) saveAll(es []*entry) error {
	var (
		mu   sync.Mutex
		errs []error
		next atomic.Int64
		wg   sync.WaitGroup
	)
	for range min(p.cfg.Workers, len(es)) {
		wg.Go(func() {
			for {
				i := int(next.Add(1) - 1)
				if i >= len(es) {
					return
				}
				if err := p.cfg.Storage.SaveProto(es[i].chunk); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
					p.mu.Lock()
					es[i].dirty = true
					p.mu.Unlock()
				}
				p.stats.saves.Add(1)
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// Close abandons work that has not started, waits for running steps,
// saves all dirty chunks and stops the workers.
func (p *Pipeline) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	for len(p.ready) > 0 {
		p.fail(p.ready[0], ErrClosed)
	}
	for _, e := range p.entries {
		for _, j := range e.jobs {
			if j != nil && j.state == jWaiting {
				p.fail(j, ErrClosed)
			}
		}
	}
	p.cond.Broadcast()
	p.mu.Unlock()
	p.wg.Wait()

	p.mu.Lock()
	for _, e := range p.entries {
		for _, j := range e.jobs {
			if j != nil && !j.finished() {
				p.fail(j, ErrClosed)
			}
		}
	}
	var es []*entry
	if p.cfg.Storage != nil {
		for _, e := range p.entries {
			if e.chunk != nil && e.dirty && e.err == nil {
				e.dirty = false
				es = append(es, e)
			}
		}
	}
	errs := p.saveErr
	p.mu.Unlock()
	return errors.Join(append(errs, p.saveAll(es))...)
}

// Stats is a snapshot of pipeline counters.
type Stats struct {
	Steps     [NumStatuses]int64
	Loads     int64
	Saves     int64
	Evictions int64
	Cancelled int64
	FarReads  int64
	FarWrites int64
	// BusyNanos is worker time spent in steps and loads.
	BusyNanos int64
	Entries   int
	Loaded    int
	LiveJobs  int
	Ready     int
}

func (p *Pipeline) Stats() Stats {
	var s Stats
	for i := range s.Steps {
		s.Steps[i] = p.stats.steps[i].Load()
	}
	s.Loads = p.stats.loads.Load()
	s.Saves = p.stats.saves.Load()
	s.Evictions = p.stats.evictions.Load()
	s.Cancelled = p.stats.cancelled.Load()
	s.FarReads = p.stats.farReads.Load()
	s.FarWrites = p.stats.farWrites.Load()
	s.BusyNanos = p.stats.busyNs.Load()
	p.mu.Lock()
	s.Entries = len(p.entries)
	s.Loaded = p.loaded
	s.Ready = len(p.ready)
	for _, e := range p.entries {
		for _, j := range e.jobs {
			if j != nil {
				s.LiveJobs++
			}
		}
	}
	p.mu.Unlock()
	return s
}

// Idle reports whether no work is queued, running or waiting.
func (p *Pipeline) Idle() bool {
	s := p.Stats()
	return s.LiveJobs == 0
}

type jobHeap []*job

func (h jobHeap) Len() int { return len(h) }
func (h jobHeap) Less(i, k int) bool {
	a, b := h[i], h[k]
	if a.prio != b.prio {
		return a.prio < b.prio
	}
	if a.st != b.st {
		return a.st > b.st
	}
	if a.e.pos.X != b.e.pos.X {
		return a.e.pos.X < b.e.pos.X
	}
	return a.e.pos.Z < b.e.pos.Z
}
func (h jobHeap) Swap(i, k int) {
	h[i], h[k] = h[k], h[i]
	h[i].heapIdx, h[k].heapIdx = i, k
}
func (h *jobHeap) Push(x any) {
	j := x.(*job)
	j.heapIdx = len(*h)
	*h = append(*h, j)
}
func (h *jobHeap) Pop() any {
	old := *h
	j := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	j.heapIdx = -1
	return j
}

func forSquare(c world.ChunkPos, r int32, fn func(world.ChunkPos)) {
	for dz := -r; dz <= r; dz++ {
		for dx := -r; dx <= r; dx++ {
			fn(world.ChunkPos{X: c.X + dx, Z: c.Z + dz})
		}
	}
}

func forRing(c world.ChunkPos, d int32, fn func(world.ChunkPos)) {
	if d == 0 {
		fn(c)
		return
	}
	for i := -d; i <= d; i++ {
		fn(world.ChunkPos{X: c.X + i, Z: c.Z - d})
		fn(world.ChunkPos{X: c.X + i, Z: c.Z + d})
	}
	for i := -d + 1; i <= d-1; i++ {
		fn(world.ChunkPos{X: c.X - d, Z: c.Z + i})
		fn(world.ChunkPos{X: c.X + d, Z: c.Z + i})
	}
}
