package gen

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
)

// TestAnvilStorageRoundTrip saves chunks of every status reached around the
// centre in vanilla region files and reloads them unchanged.
func TestAnvilStorageRoundTrip(t *testing.T) {
	p := newPipe(t, 4, nil, synthOpts{}, 0)
	defer p.Close()
	if _, err := p.Get(context.Background(), testCenter, StatusFull); err != nil {
		t.Fatal(err)
	}
	st := NewAnvilStorage(t.TempDir(), testDim)
	defer st.Close()
	p.mu.Lock()
	var cs []*ProtoChunk
	for _, e := range p.entries {
		if e.chunk != nil {
			cs = append(cs, e.chunk)
		}
	}
	p.mu.Unlock()
	seen := map[Status]int{}
	for i, c := range cs {
		if seen[c.Status()] >= 40 {
			continue
		}
		if i == 0 {
			c.Data = nbt.Compound{"test:x": int32(5)}
			c.BlockTicks = append(c.BlockTicks, Tick{X: 1, Y: 2, Z: 3, Type: "minecraft:water", Delay: 4, Priority: -1})
			c.MarkPostProcess(int(c.X)*16+1, 70, int(c.Z)*16+2)
		}
		if err := st.SaveProto(c); err != nil {
			t.Fatal(err)
		}
		d, err := st.LoadProto(c.Pos())
		if err != nil {
			t.Fatal(err)
		}
		seen[c.Status()]++
		if fingerprint(c, true) != fingerprint(d, true) {
			t.Fatalf("chunk %v (%s) changed in the round trip", c.Pos(), c.Status())
		}
		if canon(c.Data) != canon(d.Data) || len(c.BlockTicks) != len(d.BlockTicks) || len(c.FluidTicks) != len(d.FluidTicks) {
			t.Fatalf("chunk %v: data/ticks lost: %v %v", c.Pos(), d.Data, d.BlockTicks)
		}
		for s := range c.PostProcessing {
			if len(c.PostProcessing[s]) != len(d.PostProcessing[s]) {
				t.Fatalf("chunk %v: post-processing section %d lost", c.Pos(), s)
			}
		}
		if len(c.BlockEntities) != len(d.BlockEntities) {
			t.Fatalf("chunk %v: block entities %d -> %d", c.Pos(), len(c.BlockEntities), len(d.BlockEntities))
		}
	}
	if len(seen) < 4 {
		t.Fatalf("statuses covered: %v", seen)
	}
	t.Logf("round-tripped by status: %v", seen)

	// The same files are readable by the full-chunk path the server uses.
	ws := anvil.NewStorage(st.St.Dir, testDim)
	defer ws.Close()
	full, err := ws.LoadChunk(testCenter)
	if err != nil || full == nil {
		t.Fatalf("world storage: %v %v", full, err)
	}
}

// TestAnvilRestart: TestRestart's scenario on vanilla region files, the
// second run evicting through a small cache.
func TestAnvilRestart(t *testing.T) {
	want, _ := reference(t)
	dir := filepath.Join(t.TempDir(), "region")
	runs := newRunCounter(t)
	ps := spiral(testCenter, testRadius)
	for i, part := range [][]world.ChunkPos{ps[:20], ps[10:50], ps} {
		st := NewAnvilStorage(dir, testDim)
		p := newPipe(t, []int{1, 8, 3}[i], st, synthOpts{runs: runs}, []int{256, 64, 256}[i])
		got := requestAll(t, p, part, StatusFull)
		if i == 2 {
			diff(t, "anvil restart", want, prints(got))
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
