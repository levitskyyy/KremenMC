package metrics

import (
	"math"
	"testing"
)

const procStat1 = `cpu  1000 0 500 8000 100 0 0 0 0 0
cpu0 500 0 250 4000 50 0 0 0 0 0
cpu1 500 0 250 4000 50 0 0 0 0 0
intr 12345 0 0
ctxt 999
`

const procStat2 = `cpu  1700 0 800 8300 100 0 0 0 0 0
cpu0 1100 0 500 4100 50 0 0 0 0 0
cpu1 600 0 300 4200 50 0 0 0 0 0
`

func TestParseProcStat(t *testing.T) {
	a := parseProcStat([]byte(procStat1), nil)
	b := parseProcStat([]byte(procStat2), nil)
	if len(a) != 2 || len(b) != 2 {
		t.Fatalf("cores %d %d", len(a), len(b))
	}
	if a[0] != (cpuTimes{busy: 750, total: 4800}) {
		t.Fatalf("cpu0 %+v", a[0])
	}
	got := coreUsage(a, b, nil)
	// cpu0: busy +850 of +950; cpu1: busy +150 of +350
	want := []float64{850.0 / 950 * 100, 150.0 / 350 * 100}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Fatalf("core %d = %v, want %v", i, got[i], want[i])
		}
	}
	if got := coreUsage(a, b[:1], nil); len(got) != 0 {
		t.Fatalf("mismatched core count gave %v", got)
	}
	if got := coreUsage(b, a, nil); got[0] != 0 {
		t.Fatalf("counter reset gave %v", got)
	}
}

func TestParseLoadAvgAndStatm(t *testing.T) {
	l, ok := parseLoadAvg([]byte("0.52 1.25 3.00 2/1234 5678\n"))
	if !ok || l != [3]float64{0.52, 1.25, 3} {
		t.Fatalf("loadavg %v %v", l, ok)
	}
	if _, ok := parseLoadAvg([]byte("0.5 x")); ok {
		t.Fatal("short loadavg accepted")
	}
	rss, ok := parseStatmRSS([]byte("123456 7890 345 12 0 4567 0\n"))
	if !ok || rss != 7890 {
		t.Fatalf("statm %d %v", rss, ok)
	}
}

func TestHistQuantile(t *testing.T) {
	buckets := []float64{0, 1, 2, 3, math.Inf(1)}
	tests := []struct {
		counts []float64
		q      float64
		want   float64
	}{
		{[]float64{0, 0, 0, 0}, 0.95, 0},
		{[]float64{90, 5, 5, 0}, 0.95, 2},
		{[]float64{100, 0, 0, 0}, 0.95, 1},
		{[]float64{0, 0, 0, 10}, 0.95, 3},
	}
	for _, tt := range tests {
		if got := histQuantile(tt.counts, buckets, tt.q); got != tt.want {
			t.Errorf("%v q%.2f = %v, want %v", tt.counts, tt.q, got, tt.want)
		}
	}
}
