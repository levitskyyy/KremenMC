package metrics

import (
	"runtime"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/api"
)

func TestProcSample(t *testing.T) {
	p := NewProc()
	var st api.ServerStats
	t0 := time.Now()
	p.Sample(t0, &st)
	if st.CPU != 0 {
		t.Fatalf("first sample has CPU %v", st.CPU)
	}
	deadline := time.Now().Add(50 * time.Millisecond)
	x := 0
	for time.Now().Before(deadline) {
		x++
	}
	runtime.GC()
	st = api.ServerStats{}
	p.Sample(time.Now(), &st)
	if st.CPU <= 0 || st.CPU > 100 {
		t.Fatalf("CPU %v after busy loop (%d)", st.CPU, x)
	}
	if st.HeapBytes == 0 || st.SysBytes < st.HeapBytes || st.RSSBytes == 0 {
		t.Fatalf("memory heap=%d sys=%d rss=%d", st.HeapBytes, st.SysBytes, st.RSSBytes)
	}
	if st.GCCount == 0 || st.Goroutines == 0 || st.GOMAXPROCS != runtime.GOMAXPROCS(0) || st.NumCPU != runtime.NumCPU() {
		t.Fatalf("runtime gc=%d gor=%d procs=%d cpu=%d", st.GCCount, st.Goroutines, st.GOMAXPROCS, st.NumCPU)
	}
	switch runtime.GOOS {
	case "linux":
		if len(st.CPUCores) != runtime.NumCPU() || !st.HasLoad || st.RSSEstimated {
			t.Fatalf("linux cores=%d load=%v est=%v", len(st.CPUCores), st.HasLoad, st.RSSEstimated)
		}
	case "darwin":
		if st.CPUCores != nil || !st.HasLoad || !st.RSSEstimated {
			t.Fatalf("darwin cores=%v load=%v est=%v", st.CPUCores, st.HasLoad, st.RSSEstimated)
		}
	}
}

func TestWindow(t *testing.T) {
	w := NewWindow(4)
	if w.Avg(10) != 0 {
		t.Fatal("empty window")
	}
	for _, v := range []float64{1, 2, 3, 4, 5, 6} {
		w.Add(v)
	}
	if got := w.Avg(2); got != 5.5 {
		t.Fatalf("avg2 %v", got)
	}
	if got := w.Avg(100); got != 4.5 {
		t.Fatalf("avg all %v", got)
	}
}

func TestTickTimes(t *testing.T) {
	var tt TickTimes
	if a, p, m := tt.Stats(); a != 0 || p != 0 || m != 0 {
		t.Fatal("empty ring")
	}
	for i := 1; i <= 10; i++ {
		tt.Record(time.Duration(i) * time.Millisecond)
	}
	a, p, m := tt.Stats()
	if a != 5500*time.Microsecond || p != 10*time.Millisecond || m != 10*time.Millisecond {
		t.Fatalf("avg %v p95 %v max %v", a, p, m)
	}
	for i := 1; i <= 300; i++ {
		tt.Record(time.Duration(i%100+1) * time.Millisecond)
	}
	a, p, m = tt.Stats()
	if a != 50500*time.Microsecond || p != 95*time.Millisecond || m != 100*time.Millisecond {
		t.Fatalf("wrapped avg %v p95 %v max %v", a, p, m)
	}
}

func BenchmarkProcSample(b *testing.B) {
	p := NewProc()
	var st api.ServerStats
	now := time.Now()
	b.ReportAllocs()
	for b.Loop() {
		now = now.Add(time.Second)
		st = api.ServerStats{}
		p.Sample(now, &st)
	}
}
