package world

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/proto"
)

type countingGen struct {
	calls atomic.Int64
	delay time.Duration
}

func (g *countingGen) Generate(c *Chunk) {
	g.calls.Add(1)
	time.Sleep(g.delay)
	for x := range 16 {
		for z := range 16 {
			c.Sections[4].SetBlock(x, 0, z, stone)
		}
	}
	c.Finish()
}

type memStorage struct {
	mu    sync.Mutex
	m     map[uint64]*Chunk
	saves atomic.Int64
}

func (s *memStorage) LoadChunk(p ChunkPos) (*Chunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[p.Key()], nil
}

func (s *memStorage) SaveChunk(c *Chunk) error {
	s.saves.Add(1)
	s.mu.Lock()
	s.m[c.Pos().Key()] = c
	s.mu.Unlock()
	return nil
}

func TestConcurrentLoadDedup(t *testing.T) {
	gen := &countingGen{delay: 5 * time.Millisecond}
	w, err := New(Config{Generator: gen, Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make([]*Chunk, 64)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := w.LoadChunk(ctx, ChunkPos{int32(i % 4), 0})
			if err != nil {
				t.Error(err)
			}
			results[i] = c
		}()
	}
	wg.Wait()
	if n := gen.calls.Load(); n != 4 {
		t.Fatalf("generator ran %d times for 4 chunks", n)
	}
	for i, c := range results {
		if c != results[i%4] || c == nil || c.X != int32(i%4) {
			t.Fatalf("request %d got a different chunk", i)
		}
	}
}

func TestTicketsLoadUnloadSave(t *testing.T) {
	st := &memStorage{m: map[uint64]*Chunk{}}
	gen := &countingGen{}
	w, err := New(Config{Generator: gen, Storage: st, Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var loaded, unloaded atomic.Int64
	w.cfg.Hooks.ChunkLoaded = func(*Chunk) { loaded.Add(1) }
	w.cfg.Hooks.ChunkUnloaded = func(*Chunk) { unloaded.Add(1) }

	id := w.AddTicket(ChunkPos{0, 0}, 2, TicketPlayer)
	waitFor(t, func() bool { return w.Loaded() == 25 })
	w.DoTick()
	if loaded.Load() != 25 {
		t.Fatalf("loaded hook fired %d times", loaded.Load())
	}
	w.MoveTicket(id, ChunkPos{1, 0})
	waitFor(t, func() bool { w.DoTick(); return w.Loaded() == 25 && w.Chunk(ChunkPos{3, 0}) != nil })
	if w.Chunk(ChunkPos{-2, 0}) != nil {
		t.Fatal("chunk outside moved ticket still loaded")
	}
	w.RemoveTicket(id)
	w.DoTick()
	if w.Loaded() != 0 {
		t.Fatalf("%d chunks still loaded", w.Loaded())
	}
	waitFor(t, func() bool { return st.saves.Load() == 30 })
	if unloaded.Load() != 30 {
		t.Fatalf("unloaded hook %d", unloaded.Load())
	}
	gens := gen.calls.Load()
	// reload comes from storage, not the generator
	if _, err := w.LoadChunk(context.Background(), ChunkPos{0, 0}); err != nil {
		t.Fatal(err)
	}
	if gen.calls.Load() != gens {
		t.Fatal("reloaded chunk was regenerated")
	}
}

func TestUnloadThenReloadWaitsForSave(t *testing.T) {
	st := &memStorage{m: map[uint64]*Chunk{}}
	w, _ := New(Config{Generator: &countingGen{}, Storage: st, Workers: 2})
	defer w.Close()
	ctx := context.Background()
	for i := range 20 {
		id := w.AddTicket(ChunkPos{0, 0}, 0, TicketForced)
		c, err := w.LoadChunk(ctx, ChunkPos{0, 0})
		if err != nil {
			t.Fatal(err)
		}
		w.DoTick()
		w.Submit(0, 0, func(tc *TickContext) { tc.SetBlock(0, 100, 0, stone) })
		w.DoTick()
		if c.Block(0, 100, 0) != stone {
			t.Fatalf("iteration %d: block not set", i)
		}
		w.RemoveTicket(id)
		w.DoTick()
	}
	if st.saves.Load() == 0 {
		t.Fatal("no saves")
	}
}

func TestRegionTickParallel(t *testing.T) {
	w, _ := New(Config{Generator: &countingGen{}, Workers: 4, TickWorkers: 4, RandomTickSpeed: 3})
	defer w.Close()
	var changes, randoms, scheduled atomic.Int64
	w.cfg.Hooks.BlockChanged = func(x, y, z int, old, new data.StateID) { changes.Add(1) }
	w.cfg.Hooks.RandomTick = func(*TickContext, int, int, int, data.StateID) { randoms.Add(1) }
	w.cfg.Hooks.BlockTick = func(tc *TickContext, x, y, z int, s data.StateID) {
		scheduled.Add(1)
		tc.SetBlock(x, y+1, z, glass)
	}
	w.AddTicket(ChunkPos{0, 0}, 10, TicketForced)
	waitFor(t, func() bool { return w.Loaded() == 21*21 })
	w.DoTick()
	if len(w.regions) < 16 {
		t.Fatalf("only %d regions", len(w.regions))
	}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 50 {
				x, z := -150+g*37, -150+i*6
				w.SetBlock(x, 1, z, stone)
				w.Submit(x, z, func(tc *TickContext) { tc.ScheduleTick(x, 1, z, 2) })
			}
		}()
	}
	wg.Wait()
	w.SetBlock(0, 1, 0, stone)
	w.Submit(0, 0, func(tc *TickContext) { tc.ScheduleTick(0, 1, 0, 2) })
	for range 4 {
		w.DoTick()
	}
	if changes.Load() < 400 {
		t.Fatalf("block changes %d", changes.Load())
	}
	if scheduled.Load() == 0 || randoms.Load() == 0 {
		t.Fatalf("scheduled=%d random=%d", scheduled.Load(), randoms.Load())
	}
	if w.Block(0, 1, 0) != stone || w.Block(0, 2, 0) != glass {
		t.Fatalf("block at origin %s / %s", data.StateString(w.Block(0, 1, 0)), data.StateString(w.Block(0, 2, 0)))
	}
	// concurrent encoding while ticking must be race-free
	done := make(chan struct{})
	go func() {
		defer close(done)
		var buf = make([]byte, 0, 1<<20)
		for i := range 200 {
			if c := w.Chunk(ChunkPos{int32(i % 10), 0}); c != nil {
				buf = encodeForTest(buf[:0], c)
			}
		}
	}()
	for range 5 {
		w.Submit(1, 1, func(tc *TickContext) { tc.SetBlock(1, 5, 1, torch) })
		w.DoTick()
	}
	<-done
}

func encodeForTest(b []byte, c *Chunk) []byte {
	w := proto.Writer{B: b}
	EncodeChunkData(&w, c)
	return w.B
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timeout")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
