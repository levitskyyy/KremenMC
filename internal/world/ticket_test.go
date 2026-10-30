package world

import (
	"context"
	"sync"
	"testing"
	"time"
)

type orderGen struct {
	mu    sync.Mutex
	order []ChunkPos
	gate  chan struct{}
}

func (g *orderGen) Generate(c *Chunk) { g.GenerateContext(context.Background(), c) }

func (g *orderGen) GenerateContext(ctx context.Context, c *Chunk) error {
	<-g.gate
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	g.order = append(g.order, c.Pos())
	g.mu.Unlock()
	return nil
}

func (g *orderGen) generated() []ChunkPos {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]ChunkPos(nil), g.order...)
}

// After a ticket moves, pending loads run nearest the new centre first,
// not in the order they were requested.
func TestLoadsFollowMovedTicket(t *testing.T) {
	g := &orderGen{gate: make(chan struct{})}
	w, err := New(Config{Generator: g, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	id := w.AddTicket(ChunkPos{}, 5, TicketPlayer)
	g.gate <- struct{}{} // the worker is now on (0,0) or holds the gate
	moved := ChunkPos{X: 3}
	w.MoveTicket(id, moved)
	close(g.gate)
	want := 11 * 11
	deadline := time.Now().Add(10 * time.Second)
	for w.Loaded() < want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	order := g.generated()
	if len(order) < want {
		t.Fatalf("generated %d chunks, want >= %d", len(order), want)
	}
	// The first chunk and the one picked before the move may precede.
	prev := int64(-1)
	for i, p := range order[2:] {
		d := w.loadRank(p)
		if d < prev {
			t.Fatalf("load %d at %v has distance %d after %d: %v", i+2, p, d, prev, order)
		}
		prev = d
	}
	if far := (ChunkPos{X: -3}); w.Chunk(far) != nil {
		t.Fatalf("%v outside the moved ticket is loaded", far)
	}
}
