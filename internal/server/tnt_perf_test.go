package server

import (
	"os"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
)

// TestTNTPerf blows up a TNT cube buried in stone and reports tick times;
// run with KREMENMC_TNTPERF=1.
func TestTNTPerf(t *testing.T) {
	if os.Getenv("KREMENMC_TNTPERF") == "" {
		t.Skip()
	}
	s := startServer(t, nil)
	c := dial(t, s.Addr(), "Bomber")
	pos := c.joinPlay(s.Addr())
	w := s.defaultWorld()
	tnt := data.MustState("minecraft:tnt")
	const n = 10
	bx, by, bz := int(pos.X)+8, int(pos.Y)+12, int(pos.Z)+8
	done := make(chan struct{})
	stone := data.MustState("minecraft:stone")
	s.Scheduler().Global().Run("t", func(api.Task) {
		for x := -8; x < n+8; x++ {
			for y := -8; y < n+8; y++ {
				for z := -8; z < n+8; z++ {
					w.w.SetBlock(bx+x, by+y, bz+z, stone)
				}
			}
		}
		for x := 0; x < n; x++ {
			for y := 0; y < n; y++ {
				for z := 0; z < n; z++ {
					w.w.SetBlock(bx+x, by+y, bz+z, tnt)
				}
			}
		}
		w.primeTNT(api.BlockPos{X: bx, Y: by, Z: bz}, nil)
		close(done)
	})
	<-done
	start := time.Now()
	var worst time.Duration
	last := time.Now()
	for i := 0; i < 400; i++ {
		ch := make(chan struct{})
		s.Scheduler().Global().Run("t", func(api.Task) { close(ch) })
		<-ch
		if d := time.Since(last); d > worst {
			worst = d
		}
		last = time.Now()
	}
	t.Logf("ents=%d 400 ticks in %v, worst tick gap %v", len(w.Entities()), time.Since(start), worst)
}
