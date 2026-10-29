package gen

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// TestEviction: a tiny cache forces chunks to be saved and reloaded mid-run
// (including chunks a later feature step writes into); output is unchanged
// and no step runs twice.
func TestEviction(t *testing.T) {
	want, _ := reference(t)
	st := NewMemStorage(testDim)
	runs := newRunCounter(t)
	p := newPipe(t, 8, st, synthOpts{runs: runs}, 64)
	got := prints(getEach(t, p, spiral(testCenter, testRadius), StatusFull))
	s := p.Stats()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	diff(t, "evicting", want, got)
	if s.Evictions == 0 || s.Saves == 0 || s.Loads <= int64(s.Steps[StatusStructureStarts]) {
		t.Errorf("cache never cycled: %+v", s)
	}
	t.Logf("evictions %d saves %d loads %d", s.Evictions, s.Saves, s.Loads)
}

// TestRestart: generate part of the area, close, reopen on the same storage
// with another worker count and finish; identical to one-shot, no step
// repeated across restarts.
func TestRestart(t *testing.T) {
	want, _ := reference(t)
	for _, kind := range []string{"mem", "file"} {
		var st Storage
		if kind == "mem" {
			st = NewMemStorage(testDim)
		} else {
			fs, err := NewFileStorage(t.TempDir(), testDim)
			if err != nil {
				t.Fatal(err)
			}
			st = fs
		}
		runs := newRunCounter(t)
		ps := spiral(testCenter, testRadius)
		for i, part := range [][]world.ChunkPos{ps[:20], ps[10:50], ps} {
			p := newPipe(t, []int{1, 8, 3}[i], st, synthOpts{runs: runs}, 256)
			got := requestAll(t, p, part, StatusFull)
			if i == 2 {
				diff(t, kind+" restart", want, prints(got))
			}
			if err := p.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestCloseMidRun: close while requests are in flight, then continue on the
// same storage. Interrupted requests fail with ErrClosed; the final world is
// unchanged.
func TestCloseMidRun(t *testing.T) {
	want, _ := reference(t)
	st := NewMemStorage(testDim)
	runs := newRunCounter(t)
	var feats atomic.Int64
	stop := make(chan struct{})
	o := synthOpts{runs: runs, hook: func(ctx *StageContext) error {
		if ctx.Target() == StatusFeatures && feats.Add(1) == 60 {
			close(stop)
		}
		return nil
	}}
	p := newPipe(t, 8, st, o, 0)
	var rs []*Request
	for _, pos := range spiral(testCenter, testRadius) {
		r, err := p.Request(context.Background(), pos, StatusFull)
		if err != nil {
			t.Fatal(err)
		}
		rs = append(rs, r)
	}
	<-stop
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	closed := 0
	for _, r := range rs {
		if _, err := r.Result(); errors.Is(err, ErrClosed) {
			closed++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if closed == 0 {
		t.Error("close interrupted nothing")
	}
	if _, err := p.Get(context.Background(), testCenter, StatusFull); !errors.Is(err, ErrClosed) {
		t.Errorf("get after close: %v", err)
	}
	o.hook = nil
	p2 := newPipe(t, 4, st, o, 0)
	defer p2.Close()
	diff(t, "close mid-run", want, prints(requestAll(t, p2, square(testCenter.X, testCenter.Z, testRadius), StatusFull)))
	t.Logf("%d of %d requests interrupted, %d chunks stored", closed, len(rs), st.Len())
}

func TestCodecRoundTrip(t *testing.T) {
	p := newPipe(t, 4, nil, synthOpts{}, 0)
	defer p.Close()
	c, err := p.Get(context.Background(), testCenter, StatusFull)
	if err != nil {
		t.Fatal(err)
	}
	c.Data = nbt.Compound{"test:x": int32(5)}
	c.BlockTicks = []Tick{{X: 1, Y: 2, Z: 3, Type: "minecraft:water", Delay: 4, Priority: -1}}
	c.MarkPostProcess(65, 70, -17)
	cd := Codec{Dim: testDim}
	b, err := cd.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	d, err := cd.Unmarshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint(c, true) != fingerprint(d, true) {
		t.Error("round trip changed the chunk")
	}
	if d.Data["test:x"] != int32(5) || len(d.BlockTicks) != 1 || d.BlockTicks[0] != c.BlockTicks[0] {
		t.Errorf("extras lost: %+v %+v", d.Data, d.BlockTicks)
	}
	if len(d.PostProcessing[70>>4]) != 1 || d.PostProcessing[70>>4][0] != c.PostProcessing[70>>4][0] {
		t.Error("post-processing lost")
	}
}

func (s *MemStorage) clone() *MemStorage {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := NewMemStorage(s.Dim)
	for k, v := range s.m {
		c.m[k] = v
	}
	return c
}

// TestFlushMidRun: Flush while generation runs captures a consistent
// state (as if the process died right after); continuing from that copy
// gives the one-shot result.
func TestFlushMidRun(t *testing.T) {
	want, _ := reference(t)
	st := NewMemStorage(testDim)
	var feats atomic.Int64
	flushAt := make(chan struct{})
	p := newPipe(t, 8, st, synthOpts{hook: func(ctx *StageContext) error {
		if ctx.Target() == StatusFeatures && feats.Add(1) == 40 {
			close(flushAt)
		}
		return nil
	}}, 0)
	defer p.Close()
	var rs []*Request
	for _, pos := range spiral(testCenter, testRadius) {
		r, err := p.Request(context.Background(), pos, StatusFull)
		if err != nil {
			t.Fatal(err)
		}
		rs = append(rs, r)
	}
	<-flushAt
	if err := p.Flush(); err != nil {
		t.Fatal(err)
	}
	snap := st.clone()
	for _, r := range rs {
		if _, err := r.Result(); err != nil {
			t.Fatal(err)
		}
	}
	p2 := newPipe(t, 3, snap, synthOpts{runs: newRunCounter(t)}, 0)
	defer p2.Close()
	diff(t, "flush mid-run", want, prints(requestAll(t, p2, square(testCenter.X, testCenter.Z, testRadius), StatusFull)))
	t.Logf("snapshot held %d chunks", snap.Len())
}

// dropFull is the server's proto storage: full chunks belong to the world
// and are never saved by the pipeline.
type dropFull struct{ *MemStorage }

func (d dropFull) SaveProto(c *ProtoChunk) error {
	if c.Status() == StatusFull {
		return nil
	}
	return d.MemStorage.SaveProto(c)
}

// TestEvictionDroppedFull: with full chunks not persisted, an evicted full
// chunk that a later step needs again must not decorate a second time
// (that replayed its feature writes into neighbours still in progress).
func TestEvictionDroppedFull(t *testing.T) {
	want, _ := reference(t)
	var mu sync.Mutex
	runs := map[world.ChunkPos]int{}
	hook := func(ctx *StageContext) error {
		if ctx.Target() >= StatusFeatures {
			mu.Lock()
			runs[ctx.Pos()]++
			if n := runs[ctx.Pos()]; n > 1 && ctx.Target() == StatusFeatures {
				t.Errorf("features ran %d times for %v", n, ctx.Pos())
			}
			mu.Unlock()
		}
		return nil
	}
	p := newPipe(t, 8, dropFull{NewMemStorage(testDim)}, synthOpts{hook: hook}, 64)
	got := prints(getEach(t, p, spiral(testCenter, testRadius), StatusFull))
	s := p.Stats()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	diff(t, "dropped full", want, got)
	if s.Evictions == 0 {
		t.Errorf("cache never cycled: %+v", s)
	}
}
