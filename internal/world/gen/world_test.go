package gen

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

func newWorld(t *testing.T, p *Pipeline) *world.World {
	t.Helper()
	w, err := world.New(world.Config{Dim: testDim, Generator: WorldGenerator{P: p}, Workers: 4, TickWorkers: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

func sameBlocks(a, b *world.Chunk) bool {
	va, vb := make([]uint32, 4096), make([]uint32, 4096)
	for i := range a.Sections {
		x, y := a.Sections[i].Blocks.Values(va), b.Sections[i].Blocks.Values(vb)
		for k := range x {
			if x[k] != y[k] {
				return false
			}
		}
	}
	return true
}

// TestWorldTickets: chunks loaded through world tickets equal the
// pipeline's own output.
func TestWorldTickets(t *testing.T) {
	p := newPipe(t, 4, nil, synthOpts{runs: newRunCounter(t)}, 0)
	defer p.Close()
	w := newWorld(t, p)
	id := w.AddTicket(world.ChunkPos{X: 7, Z: 7}, 2, world.TicketPlayer)
	defer w.RemoveTicket(id)
	waitFor(t, "ticketed chunks", func() bool { w.DoTick(); return w.Loaded() == 25 })
	ref := newPipe(t, 1, nil, synthOpts{}, 0)
	defer ref.Close()
	plains, _ := data.Canonical().BiomeByName("plains")
	for _, pos := range square(7, 7, 2) {
		pc, err := ref.Get(context.Background(), pos, StatusFull)
		if err != nil {
			t.Fatal(err)
		}
		want := world.NewChunk(pos.X, pos.Z, testDim, plains)
		pc.Promote(want)
		got := w.Chunk(pos)
		got.RLock()
		ok := sameBlocks(want, got) && got.Height(world.MotionBlocking, 3, 4) == want.Height(world.MotionBlocking, 3, 4)
		got.RUnlock()
		if !ok {
			t.Errorf("world chunk %v differs from pipeline output", pos)
		}
	}
}

// TestWorldTicketCancel: removing the only ticket while generation is in
// progress abandons the pipeline work; a LoadChunk whose context ends does
// the same.
func TestWorldTicketCancel(t *testing.T) {
	g := newGate(StatusTerrain)
	p := newPipe(t, 4, nil, synthOpts{runs: newRunCounter(t), hook: g.hook}, 0)
	defer p.Close()
	w := newWorld(t, p)

	id := w.AddTicket(world.ChunkPos{X: 300, Z: 300}, 0, world.TicketPlayer)
	waitFor(t, "terrain blocked", func() bool { return g.blocked.Load() == 4 })
	w.RemoveTicket(id)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, err := w.LoadChunk(ctx, world.ChunkPos{X: -300, Z: 300}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("LoadChunk: %v", err)
	}
	g.release()
	waitFor(t, "pipeline idle", p.Idle)
	s := p.Stats()
	if s.Steps[StatusTerrain] != 4 || s.Steps[StatusFeatures] != 0 || s.Cancelled == 0 {
		t.Errorf("work was not abandoned: %+v", s)
	}
	w.DoTick()
	if w.Loaded() != 0 {
		t.Errorf("%d chunks installed", w.Loaded())
	}
	// a new ticket restarts the work and completes
	id = w.AddTicket(world.ChunkPos{X: 300, Z: 300}, 0, world.TicketPlayer)
	defer w.RemoveTicket(id)
	waitFor(t, "chunk after re-ticket", func() bool { w.DoTick(); return w.Loaded() == 1 })
}
