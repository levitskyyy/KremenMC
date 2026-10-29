package metrics

import (
	"slices"
	"sync/atomic"
	"time"
)

// Window keeps the most recent per-second samples for rolling averages.
type Window struct {
	buf  []float64
	n, i int
}

func NewWindow(size int) *Window { return &Window{buf: make([]float64, size)} }

func (w *Window) Add(v float64) {
	w.buf[w.i] = v
	w.i = (w.i + 1) % len(w.buf)
	w.n = min(w.n+1, len(w.buf))
}

// Avg averages the newest min(last, available) samples; 0 when empty.
func (w *Window) Avg(last int) float64 {
	k := min(last, w.n)
	if k <= 0 {
		return 0
	}
	var sum float64
	j := w.i
	for range k {
		j--
		if j < 0 {
			j = len(w.buf) - 1
		}
		sum += w.buf[j]
	}
	return sum / float64(k)
}

// TickSpan is how many recent tick durations TickTimes keeps.
const TickSpan = 100

// TickTimes is a ring of recent tick durations with one writer (the tick
// loop) and any number of concurrent readers.
type TickTimes struct {
	ring [TickSpan]atomic.Int64
	n    atomic.Int64
}

func (t *TickTimes) Record(d time.Duration) {
	i := t.n.Load()
	t.ring[i%TickSpan].Store(int64(d))
	t.n.Store(i + 1)
}

// Stats returns the mean, 95th percentile and maximum of the recorded ticks.
func (t *TickTimes) Stats() (avg, p95, peak time.Duration) {
	n := int(min(t.n.Load(), TickSpan))
	if n == 0 {
		return 0, 0, 0
	}
	var d [TickSpan]time.Duration
	var sum time.Duration
	for i := range n {
		d[i] = time.Duration(t.ring[i].Load())
		sum += d[i]
	}
	s := d[:n]
	slices.Sort(s)
	return sum / time.Duration(n), s[(n*95+99)/100-1], s[n-1]
}
