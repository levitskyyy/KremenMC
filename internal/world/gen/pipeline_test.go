package gen

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/world"
)

// testDim keeps synthetic chunks small; the synthetic world lives in 0..127.
var testDim = world.Dimension{MinY: 0, Height: 128, HasSkylight: true}

const testSeed = 0x5eed_c0ffee

func newPipe(t testing.TB, workers int, st Storage, o synthOpts, cache int) *Pipeline {
	t.Helper()
	p, err := New(Config{Seed: testSeed, Dim: testDim, Workers: workers, Stages: synthStages(o), Storage: st, CacheSize: cache})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func square(cx, cz, r int32) []world.ChunkPos {
	var ps []world.ChunkPos
	for z := cz - r; z <= cz+r; z++ {
		for x := cx - r; x <= cx+r; x++ {
			ps = append(ps, world.ChunkPos{X: x, Z: z})
		}
	}
	return ps
}

func spiral(c world.ChunkPos, r int32) []world.ChunkPos {
	var ps []world.ChunkPos
	for d := int32(0); d <= r; d++ {
		forRing(c, d, func(p world.ChunkPos) { ps = append(ps, p) })
	}
	return ps
}

// orders returns request orders over the same set.
func orders(c world.ChunkPos, r int32) map[string][]world.ChunkPos {
	sp := spiral(c, r)
	rev := slices.Clone(sp)
	slices.Reverse(rev)
	far := make([]world.ChunkPos, 0, len(sp))
	for i, k := 0, len(sp)-1; i <= k; i, k = i+1, k-1 {
		far = append(far, sp[k])
		if i != k {
			far = append(far, sp[i])
		}
	}
	m := map[string][]world.ChunkPos{
		"rowmajor": square(c.X, c.Z, r),
		"spiral":   sp,
		"reverse":  rev,
		"farnear":  far,
	}
	for _, seed := range []uint64{1, 2, 3} {
		perm := slices.Clone(sp)
		rnd := rand.New(rand.NewPCG(seed, 99))
		rnd.Shuffle(len(perm), func(i, k int) { perm[i], perm[k] = perm[k], perm[i] })
		m["random"+string(rune('0'+seed))] = perm
	}
	return m
}

// requestAll issues all requests up front (in order) and waits for them.
func requestAll(t testing.TB, p *Pipeline, ps []world.ChunkPos, st Status) map[world.ChunkPos]*ProtoChunk {
	t.Helper()
	rs := make([]*Request, len(ps))
	for i, pos := range ps {
		r, err := p.Request(context.Background(), pos, st)
		if err != nil {
			t.Fatal(err)
		}
		rs[i] = r
	}
	out := map[world.ChunkPos]*ProtoChunk{}
	for i, r := range rs {
		c, err := r.Result()
		if err != nil {
			t.Fatalf("%v: %v", ps[i], err)
		}
		out[ps[i]] = c
	}
	return out
}

// getEach requests one chunk at a time, so completion order follows ps.
func getEach(t testing.TB, p *Pipeline, ps []world.ChunkPos, st Status) map[world.ChunkPos]*ProtoChunk {
	t.Helper()
	out := map[world.ChunkPos]*ProtoChunk{}
	for _, pos := range ps {
		c, err := p.Get(context.Background(), pos, st)
		if err != nil {
			t.Fatalf("%v: %v", pos, err)
		}
		out[pos] = c
	}
	return out
}

func prints(m map[world.ChunkPos]*ProtoChunk) map[world.ChunkPos]uint64 {
	out := map[world.ChunkPos]uint64{}
	for p, c := range m {
		if c.Status() != StatusFull {
			panic("not full")
		}
		out[p] = fingerprint(c, true)
	}
	return out
}

func diff(t testing.TB, name string, want, got map[world.ChunkPos]uint64) {
	t.Helper()
	bad := 0
	for p, w := range want {
		if g, ok := got[p]; !ok || g != w {
			bad++
			if bad <= 3 {
				t.Errorf("%s: chunk %v differs", name, p)
			}
		}
	}
	if bad > 0 {
		t.Errorf("%s: %d/%d chunks differ", name, bad, len(want))
	}
}

// sealed fingerprints every in-memory chunk whose blocks can no longer
// change (initialize_light or later).
func sealed(p *Pipeline) map[world.ChunkPos]uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[world.ChunkPos]uint64{}
	for pos, e := range p.entries {
		if e.chunk != nil && e.status >= StatusInitializeLight {
			out[pos] = fingerprint(e.chunk, false)
		}
	}
	return out
}

var (
	refOnce   sync.Once
	refPrints map[world.ChunkPos]uint64
	refSealed map[world.ChunkPos]uint64
)

// center straddles chunk 0 and the region border at 8.
var (
	testCenter = world.ChunkPos{X: 4, Z: -1}
	testRadius = int32(4)
)

func reference(t testing.TB) (map[world.ChunkPos]uint64, map[world.ChunkPos]uint64) {
	refOnce.Do(func() {
		p := newPipe(t, 1, nil, synthOpts{runs: newRunCounter(t)}, 0)
		defer p.Close()
		refPrints = prints(getEach(t, p, square(testCenter.X, testCenter.Z, testRadius), StatusFull))
		refSealed = sealed(p)
	})
	return refPrints, refSealed
}

func TestReferenceSanity(t *testing.T) {
	want, _ := reference(t)
	if len(want) != 81 {
		t.Fatalf("%d chunks", len(want))
	}
	distinct := map[uint64]bool{}
	for _, v := range want {
		distinct[v] = true
	}
	if len(distinct) != len(want) {
		t.Errorf("only %d distinct chunks", len(distinct))
	}
	p := newPipe(t, 1, nil, synthOpts{}, 0)
	defer p.Close()
	c, err := p.Get(context.Background(), testCenter, StatusFull)
	if err != nil {
		t.Fatal(err)
	}
	// every status ran, light and entities exist somewhere
	if !c.LightReady {
		t.Error("light not computed")
	}
	st := p.Stats()
	for s := StatusStructureStarts; s <= StatusFull; s++ {
		if st.Steps[s] == 0 {
			t.Errorf("no %s steps", s)
		}
	}
	if st.FarReads != 0 || st.FarWrites != 0 {
		t.Errorf("synthetic stages leave their view: %+v", st)
	}
	t.Logf("one full chunk needs steps %v", st.Steps)
}

// TestDeterminism: identical output for 1..N workers and many request
// orders, across chunk and region borders.
func TestDeterminism(t *testing.T) {
	want, wantSealed := reference(t)
	type run struct {
		order   string
		workers int
		each    bool
	}
	var runs []run
	for name := range orders(testCenter, testRadius) {
		runs = append(runs, run{name, 1, false}, run{name, 8, false})
	}
	if !testing.Short() {
		runs = append(runs, run{"spiral", 3, true}, run{"random1", 5, true}, run{"farnear", 32, false})
	}
	ords := orders(testCenter, testRadius)
	for _, r := range runs {
		p := newPipe(t, r.workers, nil, synthOpts{runs: newRunCounter(t)}, 0)
		var got map[world.ChunkPos]*ProtoChunk
		if r.each {
			got = getEach(t, p, ords[r.order], StatusFull)
		} else {
			got = requestAll(t, p, ords[r.order], StatusFull)
		}
		label := fmt.Sprintf("%s/workers=%d/each=%v", r.order, r.workers, r.each)
		diff(t, label, want, prints(got))
		n := 0
		for pos, v := range sealed(p) {
			if ref, ok := wantSealed[pos]; ok {
				n++
				if ref != v {
					t.Errorf("%s: sealed chunk %v differs", label, pos)
				}
			}
		}
		if n < len(want) {
			t.Errorf("%s: only %d sealed chunks compared", label, n)
		}
		p.Close()
	}
}

// TestConcurrentRequesters: many goroutines requesting overlapping areas in
// different orders at once.
func TestConcurrentRequesters(t *testing.T) {
	want, _ := reference(t)
	p := newPipe(t, 8, nil, synthOpts{runs: newRunCounter(t)}, 0)
	defer p.Close()
	var wg sync.WaitGroup
	var mu sync.Mutex
	got := map[world.ChunkPos]uint64{}
	for g := range 16 {
		wg.Go(func() {
			ps := spiral(testCenter, testRadius)
			rnd := rand.New(rand.NewPCG(uint64(g), 7))
			rnd.Shuffle(len(ps), func(i, k int) { ps[i], ps[k] = ps[k], ps[i] })
			for _, pos := range ps[:30] {
				c, err := p.Get(context.Background(), pos, StatusFull)
				if err != nil {
					t.Error(err)
					return
				}
				f := fingerprint(c, true)
				mu.Lock()
				if old, ok := got[pos]; ok && old != f {
					t.Errorf("chunk %v changed between requesters", pos)
				}
				got[pos] = f
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	for pos, f := range got {
		if want[pos] != f {
			t.Errorf("chunk %v differs from reference", pos)
		}
	}
}

// TestIntermediateRequests: asking for lower statuses first (as a server
// warming up) and only later for full does not change the result.
func TestIntermediateRequests(t *testing.T) {
	want, _ := reference(t)
	p := newPipe(t, 4, nil, synthOpts{runs: newRunCounter(t)}, 0)
	defer p.Close()
	ps := spiral(testCenter, testRadius+3)
	for _, st := range []Status{StatusBiomes, StatusTerrain, StatusFeatures, StatusLight} {
		requestAll(t, p, ps[len(ps)/3:], st)
	}
	diff(t, "intermediate", want, prints(requestAll(t, p, square(testCenter.X, testCenter.Z, testRadius), StatusFull)))
}

// TestOrderSensitivity shows the synthetic features really depend on
// decoration order: another valid rank table gives a different world. This
// is what makes TestDeterminism meaningful.
func TestOrderSensitivity(t *testing.T) {
	want, _ := reference(t)
	saved := decorationRank
	defer func() { decorationRank = saved }()
	decorationRank = [9]int8{0, 1, 2, 3, 4, 5, 6, 7, 8}
	p := newPipe(t, 4, nil, synthOpts{}, 0)
	defer p.Close()
	got := prints(requestAll(t, p, square(testCenter.X, testCenter.Z, testRadius), StatusFull))
	n := 0
	for pos, v := range want {
		if got[pos] != v {
			n++
		}
	}
	if n == 0 {
		t.Fatal("synthetic features are order-independent; determinism tests prove nothing")
	}
	t.Logf("%d/%d chunks change with another decoration order", n, len(want))
}
