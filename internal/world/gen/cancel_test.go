package gen

import (
	"context"
	"errors"
	"math/rand/v2"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/internal/world"
)

func waitFor(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// gate blocks every step of one status until opened.
type gate struct {
	st      Status
	open    chan struct{}
	blocked atomic.Int64
	once    sync.Once
}

func newGate(st Status) *gate { return &gate{st: st, open: make(chan struct{})} }

func (g *gate) hook(ctx *StageContext) error {
	if ctx.Target() == g.st {
		g.blocked.Add(1)
		<-g.open
	}
	return nil
}

func (g *gate) release() { g.once.Do(func() { close(g.open) }) }

func TestDedup(t *testing.T) {
	runs := newRunCounter(t)
	g := newGate(StatusTerrain)
	p := newPipe(t, 8, nil, synthOpts{runs: runs, hook: g.hook}, 0)
	defer p.Close()
	defer g.release()
	pos := world.ChunkPos{X: 2, Z: 2}
	var wg sync.WaitGroup
	res := make([]*ProtoChunk, 64)
	for i := range res {
		wg.Go(func() {
			c, err := p.Get(context.Background(), pos, StatusFull)
			if err != nil {
				t.Error(err)
			}
			res[i] = c
		})
	}
	waitFor(t, "terrain blocked", func() bool { return g.blocked.Load() > 0 })
	if j := p.Stats().LiveJobs; j == 0 {
		t.Fatal("no live jobs")
	}
	g.release()
	wg.Wait()
	for _, c := range res {
		if c != res[0] {
			t.Fatal("requesters got different chunks")
		}
	}
	if n := runs.count(StatusSpawn); n != 1 {
		t.Errorf("spawn ran for %d chunks", n)
	}
}

// TestCancelAbandonsWork: a cancelled request's unstarted work is dropped;
// steps already running finish; nothing stays queued.
func TestCancelAbandonsWork(t *testing.T) {
	runs := newRunCounter(t)
	g := newGate(StatusStructureStarts)
	p := newPipe(t, 4, nil, synthOpts{runs: runs, hook: g.hook}, 0)
	defer p.Close()
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := p.Get(ctx, world.ChunkPos{X: 1000, Z: -1000}, StatusFull)
		errc <- err
	}()
	waitFor(t, "workers blocked", func() bool { return g.blocked.Load() == 4 })
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	g.release()
	waitFor(t, "idle", p.Idle)
	s := p.Stats()
	if s.Steps[StatusStructureStarts] != 4 {
		t.Errorf("%d structure starts ran, want only the 4 already running", s.Steps[StatusStructureStarts])
	}
	if s.Steps[StatusTerrain] != 0 || s.Ready != 0 {
		t.Errorf("work survived cancellation: %+v", s)
	}
	if s.Cancelled == 0 || s.Entries != 4 || s.Loaded != 4 {
		t.Errorf("unexpected state after cancel: %+v", s)
	}
	// the abandoned work restarts cleanly
	want := newPipe(t, 1, nil, synthOpts{}, 0)
	defer want.Close()
	a, err := p.Get(context.Background(), world.ChunkPos{X: 1000, Z: -1000}, StatusFull)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := want.Get(context.Background(), world.ChunkPos{X: 1000, Z: -1000}, StatusFull)
	if fingerprint(a, true) != fingerprint(b, true) {
		t.Error("chunk differs after cancel and retry")
	}
}

// TestCancelShared: cancelling one requester keeps work another one needs.
func TestCancelShared(t *testing.T) {
	g := newGate(StatusBiomes)
	p := newPipe(t, 4, nil, synthOpts{runs: newRunCounter(t), hook: g.hook}, 0)
	defer p.Close()
	ctx, cancel := context.WithCancel(context.Background())
	a, err := p.Request(ctx, world.ChunkPos{X: 0, Z: 0}, StatusFull)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Request(context.Background(), world.ChunkPos{X: 1, Z: 0}, StatusFull)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "biomes blocked", func() bool { return g.blocked.Load() > 0 })
	a.Cancel()
	cancel()
	g.release()
	if _, err := a.Result(); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled request: %v", err)
	}
	cb, err := b.Result()
	if err != nil {
		t.Fatal(err)
	}
	ref := newPipe(t, 1, nil, synthOpts{}, 0)
	defer ref.Close()
	rb, _ := ref.Get(context.Background(), world.ChunkPos{X: 1, Z: 0}, StatusFull)
	if fingerprint(cb, true) != fingerprint(rb, true) {
		t.Error("surviving request differs")
	}
	if p.Peek(world.ChunkPos{X: 0, Z: 0}).Status() < StatusInitializeLight {
		t.Error("shared dependency was abandoned")
	}
}

// TestCancelMany: random requests, half cancelled at random moments; the
// rest match the reference, nothing runs twice, nothing stays queued.
func TestCancelMany(t *testing.T) {
	want, _ := reference(t)
	p := newPipe(t, 8, nil, synthOpts{runs: newRunCounter(t)}, 0)
	defer p.Close()
	ps := square(testCenter.X, testCenter.Z, testRadius)
	rnd := rand.New(rand.NewPCG(42, 1))
	var wg sync.WaitGroup
	for i := range 300 {
		pos := ps[rnd.IntN(len(ps))]
		st := Status(1 + rnd.IntN(NumStatuses-1))
		cancelAfter := time.Duration(-1)
		if i%2 == 0 {
			cancelAfter = time.Duration(rnd.IntN(3000)) * time.Microsecond
		}
		wg.Go(func() {
			ctx := context.Background()
			if cancelAfter >= 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, cancelAfter)
				defer cancel()
			}
			c, err := p.Get(ctx, pos, st)
			if err != nil {
				if cancelAfter < 0 {
					t.Errorf("%v %s: %v", pos, st, err)
				}
				return
			}
			if c.Status() < st {
				t.Errorf("%v: status %s < %s", pos, c.Status(), st)
			}
		})
	}
	wg.Wait()
	waitFor(t, "idle", p.Idle)
	diff(t, "after cancellations", want, prints(requestAll(t, p, ps, StatusFull)))
	if s := p.Stats(); s.LiveJobs != 0 || s.Ready != 0 {
		t.Errorf("leftover work %+v", s)
	}
}

func TestBackpressure(t *testing.T) {
	g := newGate(StatusTerrain)
	hook := func(ctx *StageContext) error {
		if ctx.Pos().Z == 0 && (ctx.Pos().X == 0 || ctx.Pos().X == 50) {
			return g.hook(ctx)
		}
		return nil
	}
	p, err := New(Config{Seed: 1, Dim: testDim, Workers: 4, MaxRequests: 2, Stages: synthStages(synthOpts{hook: hook})})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	defer g.release()
	r1, err := p.Request(context.Background(), world.ChunkPos{X: 0}, StatusFull)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := p.Request(context.Background(), world.ChunkPos{X: 50}, StatusFull)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := p.Request(ctx, world.ChunkPos{X: 100}, StatusFull); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("third request: %v", err)
	}
	r2.Cancel()
	r3, err := p.Request(context.Background(), world.ChunkPos{X: 100}, StatusBiomes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r3.Result(); err != nil {
		t.Fatal(err)
	}
	g.release()
	if _, err := r1.Result(); err != nil {
		t.Fatal(err)
	}
	// both slots are free again
	for i := range 2 {
		if _, err := p.Get(context.Background(), world.ChunkPos{X: int32(i)}, StatusFull); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStageError(t *testing.T) {
	bad := world.ChunkPos{X: 3, Z: 3}
	o := synthOpts{hook: func(ctx *StageContext) error {
		if ctx.Pos() == bad && ctx.Target() == StatusTerrain {
			return errors.New("boom")
		}
		if ctx.Pos() == (world.ChunkPos{X: 40}) && ctx.Target() == StatusBiomes {
			panic("kaboom")
		}
		return nil
	}}
	p := newPipe(t, 4, nil, o, 0)
	defer p.Close()
	for _, pos := range []world.ChunkPos{bad, {X: 4, Z: 3}, bad} {
		_, err := p.Get(context.Background(), pos, StatusFull)
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Errorf("%v: got %v", pos, err)
		}
	}
	if _, err := p.Get(context.Background(), world.ChunkPos{X: 40}, StatusFull); err == nil || !strings.Contains(err.Error(), "panic: kaboom") {
		t.Errorf("panic: got %v", err)
	}
	if _, err := p.Get(context.Background(), world.ChunkPos{X: -30, Z: 3}, StatusFull); err != nil {
		t.Errorf("unrelated chunk: %v", err)
	}
	waitFor(t, "idle", p.Idle)
}

func TestNoGoroutineLeak(t *testing.T) {
	runtime.GC()
	base := runtime.NumGoroutine()
	for range 3 {
		g := newGate(StatusTerrain)
		p := newPipe(t, 8, NewMemStorage(testDim), synthOpts{hook: g.hook}, 32)
		var wg sync.WaitGroup
		for i := range 40 {
			wg.Go(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(i%5)*time.Millisecond)
				defer cancel()
				p.Get(ctx, world.ChunkPos{X: int32(i % 7), Z: int32(i / 7)}, StatusFull)
			})
		}
		time.Sleep(3 * time.Millisecond)
		g.release()
		wg.Wait()
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "goroutines to exit", func() bool { return runtime.NumGoroutine() <= base })
}
