package world

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// Storage persists chunks. LoadChunk returns nil, nil for absent chunks.
// Implementations must be safe for concurrent use.
type Storage interface {
	LoadChunk(ChunkPos) (*Chunk, error)
	SaveChunk(*Chunk) error
}

// Generator fills a fresh chunk (blocks and biomes) and must be deterministic
// and safe for concurrent use. It must call c.Finish (or keep counters and
// heightmaps consistent itself).
type Generator interface {
	Generate(c *Chunk)
}

// ContextGenerator is a Generator whose work can be abandoned. The world
// calls GenerateContext instead of Generate; ctx is cancelled once no
// ticket and no LoadChunk caller wants the chunk. On error the chunk is
// not installed.
type ContextGenerator interface {
	Generator
	GenerateContext(ctx context.Context, c *Chunk) error
}

type BlockChangeFunc func(x, y, z int, old, new data.StateID)

// Hooks are invoked from region workers; functions must be safe for
// concurrent calls from different regions.
type Hooks struct {
	RandomTick func(t *TickContext, x, y, z int, s data.StateID)
	BlockTick  func(t *TickContext, x, y, z int, s data.StateID)
	RegionTick func(t *TickContext)
	// ChunkLoaded fires in the owning region's phase after border light is stitched.
	ChunkLoaded   func(c *Chunk)
	ChunkUnloaded func(c *Chunk)
	BlockChanged  BlockChangeFunc
	// ShapeUpdate runs in the owning region's phase after a block change
	// applied there, for neighbour shape updates. Changes it makes through t
	// recurse up to MaxUpdateDepth.
	ShapeUpdate func(t *TickContext, x, y, z int, old, new data.StateID)
	// Fluid performs fluid side effects (drops, fizz); FluidConversion
	// reports the water/lava source conversion game rules.
	Fluid           func(t *TickContext, ev FluidEvent, x, y, z int, old data.StateID)
	FluidConversion func(k FluidKind) bool
}

// MaxUpdateDepth bounds recursive ShapeUpdate chains.
const MaxUpdateDepth = 512

type Config struct {
	Dim             Dimension
	Generator       Generator
	Storage         Storage
	Workers         int  // load/generate/save workers (default GOMAXPROCS)
	TickWorkers     int  // region tick workers (default GOMAXPROCS)
	RandomTickSpeed int  // random ticks per section per tick (default 3; 0 disables)
	Ultrawarm       bool // nether rules: faster, farther lava
	Seed            int64
	Hooks           Hooks
	Logf            func(format string, args ...any)
}

const numShards = 64

type shard struct {
	mu sync.RWMutex
	m  map[uint64]*Chunk
}

type World struct {
	cfg    Config
	shards [numShards]shard

	regMu   sync.RWMutex
	regions map[uint64]*Region

	loadMu   sync.Mutex
	inflight map[uint64]*op
	io       *pool

	installMu sync.Mutex
	installs  []*Chunk

	ticketMu   sync.Mutex
	refs       map[uint64]int32
	tickets    map[TicketID]*ticket
	nextTicket TicketID
	unloads    map[uint64]struct{}
	// centres snapshots ticket centres for load ranking; epoch bumps on
	// every change.
	centres     atomic.Pointer[[]ChunkPos]
	ticketEpoch atomic.Uint64

	tick     atomic.Int64
	tickMu   sync.Mutex
	ctxPool  sync.Pool
	closed   atomic.Bool
	genCount atomic.Int64
	ticked   atomic.Int32
	busyNs   atomic.Int64
	phaseNs  atomic.Int64
}

type op struct {
	done chan struct{}
	c    *Chunk
	err  error
	save bool
	// load ops only; waiters is guarded by loadMu
	ctx     context.Context
	cancel  context.CancelFunc
	waiters int
}

var ErrClosed = errors.New("world: closed")

func New(cfg Config) (*World, error) {
	if cfg.Dim == (Dimension{}) {
		cfg.Dim = Overworld
	}
	if err := cfg.Dim.Validate(); err != nil {
		return nil, err
	}
	if cfg.Workers <= 0 {
		cfg.Workers = runtime.GOMAXPROCS(0)
	}
	if cfg.TickWorkers <= 0 {
		cfg.TickWorkers = runtime.GOMAXPROCS(0)
	}
	if cfg.RandomTickSpeed < 0 {
		cfg.RandomTickSpeed = 0
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	w := &World{
		cfg:      cfg,
		regions:  map[uint64]*Region{},
		inflight: map[uint64]*op{},
		refs:     map[uint64]int32{},
		tickets:  map[TicketID]*ticket{},
		unloads:  map[uint64]struct{}{},
	}
	for i := range w.shards {
		w.shards[i].m = map[uint64]*Chunk{}
	}
	w.io = newPool(cfg.Workers)
	w.io.rank, w.io.epoch = w.loadRank, w.ticketEpoch.Load
	w.ctxPool.New = func() any {
		return &TickContext{w: w, light: NewLightEngine(cfg.Dim, w.chunkSource)}
	}
	return w, nil
}

func (w *World) Dim() Dimension { return w.cfg.Dim }
func (w *World) Tick() int64    { return w.tick.Load() }

// Generated reports how many chunks the generator produced.
func (w *World) Generated() int64 { return w.genCount.Load() }

func (w *World) shard(k uint64) *shard {
	return &w.shards[(k^k>>29^k>>37)%numShards]
}

// Chunk returns a loaded chunk or nil.
func (w *World) Chunk(p ChunkPos) *Chunk {
	k := p.Key()
	s := w.shard(k)
	s.mu.RLock()
	c := s.m[k]
	s.mu.RUnlock()
	return c
}

func (w *World) chunkSource(cx, cz int32) *Chunk { return w.Chunk(ChunkPos{cx, cz}) }

// Loaded returns the number of loaded chunks.
func (w *World) Loaded() int {
	n := 0
	for i := range w.shards {
		s := &w.shards[i]
		s.mu.RLock()
		n += len(s.m)
		s.mu.RUnlock()
	}
	return n
}

// Block reads a block from any goroutine (takes the chunk read lock).
func (w *World) Block(x, y, z int) data.StateID {
	c := w.Chunk(ChunkAt(x, z))
	if c == nil {
		return data.Air
	}
	c.mu.RLock()
	s := c.Block(x&15, y, z&15)
	c.mu.RUnlock()
	return s
}

// LoadChunk loads or generates p, deduplicating concurrent requests. The
// chunk stays loaded only while a ticket covers it.
func (w *World) LoadChunk(ctx context.Context, p ChunkPos) (*Chunk, error) {
	o := w.request(p, true)
	if o == nil {
		return nil, ErrClosed
	}
	select {
	case <-o.done:
		return o.c, o.err
	case <-ctx.Done():
		w.dropWaiter(p, o)
		return nil, ctx.Err()
	}
}

// dropWaiter cancels a load nobody wants any more.
func (w *World) dropWaiter(p ChunkPos, o *op) {
	k := p.Key()
	w.ticketMu.Lock()
	w.loadMu.Lock()
	o.waiters--
	if o.waiters == 0 && w.refs[k] == 0 && w.inflight[k] == o && o.cancel != nil {
		o.cancel()
	}
	w.loadMu.Unlock()
	w.ticketMu.Unlock()
}

// request returns an op that completes when p is loaded; waiter counts the
// caller as a LoadChunk waiter.
func (w *World) request(p ChunkPos, waiter bool) *op {
	if w.closed.Load() {
		return nil
	}
	k := p.Key()
	w.loadMu.Lock()
	defer w.loadMu.Unlock()
	if c := w.Chunk(p); c != nil {
		o := &op{done: make(chan struct{}), c: c}
		close(o.done)
		return o
	}
	prev := w.inflight[k]
	if prev != nil && !prev.save && prev.ctx.Err() == nil {
		if waiter {
			prev.waiters++
		}
		return prev
	}
	o := &op{done: make(chan struct{})}
	o.ctx, o.cancel = context.WithCancel(context.Background())
	if waiter {
		o.waiters++
	}
	w.inflight[k] = o
	if !w.io.submitLoad(p, o.ctx, func() { w.load(p, o, prev) }) {
		delete(w.inflight, k)
		o.cancel()
		o.err = ErrClosed
		close(o.done)
	}
	return o
}

func (w *World) load(p ChunkPos, o, after *op) {
	defer o.cancel()
	if after != nil {
		<-after.done
	}
	c, err := w.produce(o.ctx, p)
	k := p.Key()
	w.loadMu.Lock()
	if err == nil {
		s := w.shard(k)
		s.mu.Lock()
		s.m[k] = c
		s.mu.Unlock()
	}
	if w.inflight[k] == o {
		delete(w.inflight, k)
	}
	w.loadMu.Unlock()
	o.c, o.err = c, err
	if err == nil {
		w.installMu.Lock()
		w.installs = append(w.installs, c)
		w.installMu.Unlock()
		w.ticketMu.Lock()
		if w.refs[k] == 0 {
			w.unloads[k] = struct{}{}
		}
		w.ticketMu.Unlock()
	}
	close(o.done)
}

func (w *World) produce(ctx context.Context, p ChunkPos) (c *Chunk, err error) {
	defer func() {
		if r := recover(); r != nil {
			c, err = nil, fmt.Errorf("world: chunk %d,%d: panic: %v", p.X, p.Z, r)
		}
	}()
	if w.cfg.Storage != nil {
		c, err = w.cfg.Storage.LoadChunk(p)
		if err != nil {
			return nil, fmt.Errorf("world: load chunk %d,%d: %w", p.X, p.Z, err)
		}
		if c != nil && c.Dim != w.cfg.Dim {
			return nil, fmt.Errorf("world: chunk %d,%d has dimension %+v", p.X, p.Z, c.Dim)
		}
	}
	if c == nil {
		plains, _ := data.Canonical().BiomeByName("plains")
		c = NewChunk(p.X, p.Z, w.cfg.Dim, plains)
		if g, ok := w.cfg.Generator.(ContextGenerator); ok {
			if err := g.GenerateContext(ctx, c); err != nil {
				return nil, fmt.Errorf("world: generate chunk %d,%d: %w", p.X, p.Z, err)
			}
			w.genCount.Add(1)
		} else if w.cfg.Generator != nil {
			w.cfg.Generator.Generate(c)
			w.genCount.Add(1)
		}
		c.MarkDirty()
	}
	if !c.LightReady {
		NewLightEngine(c.Dim, nil).LightChunk(c)
	}
	return c, nil
}

// SaveAll writes every dirty loaded chunk, with the scheduled block and
// fluid ticks of live chunks, and waits for completion. It must not be
// called from the tick goroutine.
func (w *World) SaveAll() error {
	if w.cfg.Storage == nil {
		return nil
	}
	w.tickMu.Lock()
	if !w.closed.Load() {
		w.snapshotPendingTicks()
	}
	w.tickMu.Unlock()
	return w.saveDirty()
}

func (w *World) saveDirty() error {
	if w.cfg.Storage == nil {
		return nil
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for i := range w.shards {
		s := &w.shards[i]
		s.mu.RLock()
		for _, c := range s.m {
			if !c.clearDirty() {
				continue
			}
			wg.Add(1)
			c := c
			if !w.io.submit(func() {
				defer wg.Done()
				if err := w.cfg.Storage.SaveChunk(c); err != nil {
					c.MarkDirty()
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			}) {
				wg.Done()
				c.MarkDirty()
			}
		}
		s.mu.RUnlock()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// Close saves all chunks and stops workers. The world must not tick afterwards.
func (w *World) Close() error {
	if w.closed.Swap(true) {
		return nil
	}
	w.tickMu.Lock()
	defer w.tickMu.Unlock()
	w.loadMu.Lock()
	for _, o := range w.inflight {
		if !o.save {
			o.cancel()
		}
	}
	w.loadMu.Unlock()
	w.snapshotPendingTicks()
	err := w.saveDirty()
	w.io.close()
	return err
}

// Run ticks at 20 TPS until ctx is done.
func (w *World) Run(ctx context.Context) {
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.DoTick()
		}
	}
}

// BlockReader reads blocks through a one-chunk cache, for bursts of nearby
// reads on a single goroutine (explosion rays, collision clips).
type BlockReader struct {
	w   *World
	key uint64
	c   *Chunk
	set bool
}

func (w *World) Reader() BlockReader { return BlockReader{w: w} }

func (r *BlockReader) Block(x, y, z int) data.StateID {
	p := ChunkAt(x, z)
	if k := p.Key(); !r.set || k != r.key {
		r.key, r.c, r.set = k, r.w.Chunk(p), true
	}
	if r.c == nil {
		return data.Air
	}
	r.c.mu.RLock()
	s := r.c.Block(x&15, y, z&15)
	r.c.mu.RUnlock()
	return s
}
