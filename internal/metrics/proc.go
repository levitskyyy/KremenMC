// Package metrics samples process and runtime statistics and formats server
// stats for display. Sampling is meant to run once per second from a single
// goroutine.
package metrics

import (
	"math"
	"runtime"
	"runtime/metrics"
	"time"

	"github.com/levitskyyy/KremenMC/api"
)

const (
	mHeap = iota
	mTotal
	mReleased
	mGCCycles
	mGoroutines
	mGomaxprocs
	mMemLimit
	mPauses
	numSamples
)

var sampleNames = [numSamples]string{
	mHeap:       "/memory/classes/heap/objects:bytes",
	mTotal:      "/memory/classes/total:bytes",
	mReleased:   "/memory/classes/heap/released:bytes",
	mGCCycles:   "/gc/cycles/total:gc-cycles",
	mGoroutines: "/sched/goroutines:goroutines",
	mGomaxprocs: "/sched/gomaxprocs:threads",
	mMemLimit:   "/gc/gomemlimit:bytes",
	mPauses:     "/sched/pauses/total/gc:seconds",
}

// pauseDecay ages the GC pause histogram so its p95 covers roughly the
// last minute.
var pauseDecay = math.Exp(-1.0 / 60)

// Proc samples process CPU, memory, GC and platform CPU/load figures. It is
// not safe for concurrent use.
type Proc struct {
	samples  [numSamples]metrics.Sample
	pause    []float64
	prevHist []uint64
	lastCPU  time.Duration
	lastWall time.Time
	plat     platform
}

func NewProc() *Proc {
	p := &Proc{}
	for i, n := range sampleNames {
		p.samples[i].Name = n
	}
	return p
}

// Sample fills the process fields of st.
func (p *Proc) Sample(now time.Time, st *api.ServerStats) {
	metrics.Read(p.samples[:])
	st.HeapBytes = p.u64(mHeap)
	st.SysBytes = p.u64(mTotal)
	st.GCCount = p.u64(mGCCycles)
	st.Goroutines = int(p.u64(mGoroutines))
	st.GOMAXPROCS = int(p.u64(mGomaxprocs))
	if l := p.u64(mMemLimit); l < math.MaxInt64 {
		st.MemLimit = l
	}
	if s := p.samples[mPauses]; s.Value.Kind() == metrics.KindFloat64Histogram {
		st.GCPauseP95 = p.pauseP95(s.Value.Float64Histogram())
	}
	st.NumCPU = runtime.NumCPU()

	if cpu, ok := cpuTime(); ok {
		if !p.lastWall.IsZero() {
			if wall := now.Sub(p.lastWall); wall > 0 {
				st.CPU = clampPct(float64(cpu-p.lastCPU) / float64(wall) / float64(st.NumCPU) * 100)
			}
		}
		p.lastCPU = cpu
	}
	p.lastWall = now

	p.plat.sample(st)
	if st.RSSBytes == 0 {
		st.RSSBytes = st.SysBytes - min(st.SysBytes, p.u64(mReleased))
		st.RSSEstimated = true
	}
}

func (p *Proc) u64(i int) uint64 {
	if v := p.samples[i].Value; v.Kind() == metrics.KindUint64 {
		return v.Uint64()
	}
	return 0
}

func (p *Proc) pauseP95(h *metrics.Float64Histogram) time.Duration {
	if len(p.pause) != len(h.Counts) {
		p.pause = make([]float64, len(h.Counts))
		p.prevHist = make([]uint64, len(h.Counts))
		copy(p.prevHist, h.Counts)
		for i, c := range h.Counts {
			p.pause[i] = float64(c)
		}
	} else {
		for i, c := range h.Counts {
			p.pause[i] = p.pause[i]*pauseDecay + float64(c-p.prevHist[i])
			p.prevHist[i] = c
		}
	}
	return time.Duration(histQuantile(p.pause, h.Buckets, 0.95) * float64(time.Second))
}

// histQuantile returns the upper bound of the bucket holding quantile q.
// buckets has len(counts)+1 boundaries.
func histQuantile(counts, buckets []float64, q float64) float64 {
	var total float64
	for _, c := range counts {
		total += c
	}
	if total < 0.5 {
		return 0
	}
	target := total * q
	var acc float64
	for i, c := range counts {
		acc += c
		if acc >= target {
			if hi := buckets[i+1]; !math.IsInf(hi, 1) {
				return hi
			}
			return max(buckets[i], 0)
		}
	}
	return 0
}

func clampPct(v float64) float64 { return min(max(v, 0), 100) }
