package density

import (
	"sync"
	"testing"
)

func TestConcurrentRouter(t *testing.T) {
	r, err := CachedRouter("minecraft:overworld", 12345)
	if err != nil {
		t.Fatal(err)
	}
	fd := r.Fields["final_density"]
	const n = 1500
	type res struct {
		d  float32
		ct [6]int64
	}
	pos := func(i int) (int32, int32, int32) { return int32(i*37 - 20000), int32(i%384 - 64), int32(30000 - i*53) }
	sample := func(c *Ctx, i int) res {
		x, y, z := pos(i)
		tp := r.ClimateTarget(c, x>>2, y>>2, z>>2)
		return res{fd.Value(c, x, y, z), [6]int64{tp.Temperature, tp.Humidity, tp.Continentalness, tp.Erosion, tp.Depth, tp.Weirdness}}
	}
	want := make([]res, n)
	for i := range want {
		want[i] = sample(nil, i)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	bad := 0
	for g := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := &Ctx{}
			for k := range n {
				i := (k*7 + g*101) % n
				if sample(c, i) != want[i] {
					mu.Lock()
					bad++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if bad > 0 {
		t.Fatalf("%d concurrent samples differ from sequential", bad)
	}
}

var sinkF float32

func BenchmarkClimateTarget(b *testing.B) {
	r, err := CachedRouter("minecraft:overworld", 12345)
	if err != nil {
		b.Fatal(err)
	}
	c := &Ctx{}
	b.ReportAllocs()
	i := int32(0)
	for b.Loop() {
		i++
		t := r.ClimateTarget(c, i, 16, -i)
		sinkF += float32(t.Depth)
	}
}

func BenchmarkFinalDensityPoint(b *testing.B) {
	r, err := CachedRouter("minecraft:overworld", 12345)
	if err != nil {
		b.Fatal(err)
	}
	fd := r.Fields["final_density"]
	c := &Ctx{}
	b.ReportAllocs()
	i := int32(0)
	for b.Loop() {
		i++
		sinkF += fd.Value(c, i*3, 40, -i*5)
	}
}
