package server

import (
	"math"
	"os"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/internal/config"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// TestLoad joins many fake clients that then walk in circles.
// Run with KREMENMC_LOAD_CLIENTS=200 go test -run TestLoad -v ./internal/server
func TestLoad(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("KREMENMC_LOAD_CLIENTS"))
	if n <= 0 {
		t.Skip("set KREMENMC_LOAD_CLIENTS to run")
	}
	vd := 8
	if v, err := strconv.Atoi(os.Getenv("KREMENMC_LOAD_VIEW")); err == nil {
		vd = v
	}
	s := startServer(t, func(p *config.ServerProperties) {
		p.ViewDistance = vd
		p.NetworkCompressionThreshold = 256
	})
	clients := make([]*fakeClient, n)
	positions := make([]*packets.ClientboundPlayerPosition, n)
	var wg sync.WaitGroup
	start := time.Now()
	sem := make(chan struct{}, 32)
	for i := range clients {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			c := dial(t, s.Addr(), "load"+strconv.Itoa(i))
			c.view = int8(vd)
			positions[i] = c.joinPlay(s.Addr())
			clients[i] = c
		}()
	}
	wg.Wait()
	joinAll := time.Since(start)
	lat := make([]time.Duration, n)
	for i, c := range clients {
		lat[i] = c.joined
	}
	slices.Sort(lat)
	t.Logf("%d clients joined in %v; join latency p50=%v p95=%v max=%v", n, joinAll.Round(time.Millisecond),
		lat[n/2].Round(time.Millisecond), lat[n*95/100].Round(time.Millisecond), lat[n-1].Round(time.Millisecond))

	stop := make(chan struct{})
	var mwg sync.WaitGroup
	for i, c := range clients {
		mwg.Add(1)
		go func() {
			defer mwg.Done()
			p := positions[i]
			tk := time.NewTicker(50 * time.Millisecond)
			defer tk.Stop()
			for step := 0; ; step++ {
				select {
				case <-stop:
					return
				case <-tk.C:
				}
				a := float64(step) * 0.05
				x, z := p.X+6*math.Cos(a+float64(i)), p.Z+6*math.Sin(a+float64(i))
				c.send(&packets.ServerboundMovePlayerPosRot{X: x, Y: p.Y, Z: z, Yaw: float32(a * 57), Flags: packets.MovementFlagsOnGround})
				c.send(&packets.ServerboundClientTickEnd{})
			}
		}()
		go func() {
			for range c.in {
			}
		}()
	}
	var samples []float64
	for range 10 {
		time.Sleep(time.Second)
		samples = append(samples, s.MSPT())
	}
	close(stop)
	mwg.Wait()
	var chunks int64
	for _, c := range clients {
		chunks += c.chunks.Load()
	}
	slices.Sort(samples)
	t.Logf("moving %d players for 10s: MSPT avg(100 ticks) min=%.2f median=%.2f max=%.2f, TPS=%.1f, chunks sent=%d, online=%d",
		n, samples[0], samples[len(samples)/2], samples[len(samples)-1], s.TPS(), chunks, len(s.Players()))
	if got := len(s.Players()); got != n {
		t.Fatalf("%d players online, want %d", got, n)
	}
}
