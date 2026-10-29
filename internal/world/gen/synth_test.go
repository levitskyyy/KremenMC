package gen

import (
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// runCounter asserts that no (chunk, status) step ever runs twice, across
// pipelines sharing it (restart tests).
type runCounter struct {
	mu sync.Mutex
	m  map[[3]int32]int
	t  testing.TB
}

func newRunCounter(t testing.TB) *runCounter { return &runCounter{m: map[[3]int32]int{}, t: t} }

func (r *runCounter) hit(ctx *StageContext) {
	if r == nil {
		return
	}
	k := [3]int32{ctx.Pos().X, ctx.Pos().Z, int32(ctx.Target())}
	r.mu.Lock()
	r.m[k]++
	n := r.m[k]
	r.mu.Unlock()
	if n > 1 {
		r.t.Errorf("step %s ran %d times for %v", ctx.Target(), n, ctx.Pos())
	}
}

func (r *runCounter) count(s Status) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for k := range r.m {
		if Status(k[2]) == s {
			n++
		}
	}
	return n
}

type synthOpts struct {
	runs  *runCounter
	heavy int
	hook  func(ctx *StageContext) error
}

func synthStages(o synthOpts) []Stage {
	return SyntheticStages(SyntheticOptions{Heavy: o.heavy, Hook: func(ctx *StageContext) error {
		o.runs.hit(ctx)
		if o.hook != nil {
			return o.hook(ctx)
		}
		return nil
	}})
}

// fingerprint hashes everything generation produces for a chunk.
func fingerprint(c *ProtoChunk, withStatus bool) uint64 {
	h := fnv.New64a()
	w := func(v ...any) { fmt.Fprint(h, v...) }
	if withStatus {
		w("st", c.Status())
	}
	vals := make([]uint32, 4096)
	for i := range c.Sections {
		w("s", i, c.Sections[i].Blocks.Values(vals))
		w("b", c.Sections[i].Biomes.Values(vals[:64]))
	}
	for _, hm := range []world.Heightmap{world.WorldSurface, world.OceanFloor, world.MotionBlocking, world.MotionBlockingNoLeaves} {
		for i := range 256 {
			w(c.Height(hm, i&15, i>>4), ",")
		}
	}
	if withStatus {
		for i := range c.SkyLight {
			h.Write(c.SkyLight[i].Bytes())
			h.Write(c.BlockLight[i].Bytes())
		}
		for _, e := range c.Entities {
			w(canon(e))
		}
	}
	for _, id := range c.SortedStarts() {
		w(id, canon(c.Starts[id].Encode()))
	}
	for _, id := range c.SortedRefs() {
		w(id, c.Refs[id])
	}
	return h.Sum64()
}

func canon(v any) string {
	switch x := v.(type) {
	case nbt.Compound:
		ks := make([]string, 0, len(x))
		for k := range x {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		s := "{"
		for _, k := range ks {
			s += k + ":" + canon(x[k]) + ","
		}
		return s + "}"
	case RawStart:
		return canon(nbt.Compound(x))
	}
	return fmt.Sprint(v)
}
