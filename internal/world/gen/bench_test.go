package gen

import (
	"fmt"
	"runtime"
	"slices"
	"testing"
	"time"
)

const benchHeavy = 400

// BenchmarkPipeline generates a 7x7 full area (plus its dependency margin)
// with a CPU-heavy synthetic terrain stage at several worker counts.
func BenchmarkPipeline(b *testing.B) {
	ws := []int{1, 2, 4, 8, runtime.GOMAXPROCS(0)}
	slices.Sort(ws)
	for _, w := range slices.Compact(ws) {
		b.Run(fmt.Sprintf("workers=%d", w), func(b *testing.B) {
			var terrain, busy int64
			var total time.Duration
			for range b.N {
				p := newPipe(b, w, nil, synthOpts{heavy: benchHeavy}, 0)
				t0 := time.Now()
				requestAll(b, p, square(0, 0, 3), StatusFull)
				total += time.Since(t0)
				terrain = p.Stats().Steps[StatusTerrain]
				busy += p.Stats().BusyNanos
				p.Close()
			}
			b.ReportMetric(float64(terrain)*float64(b.N)/total.Seconds(), "terrain-chunks/s")
			b.ReportMetric(49*float64(b.N)/total.Seconds(), "full-chunks/s")
			b.ReportMetric(float64(busy)/float64(total), "busy-workers")
		})
	}
}
