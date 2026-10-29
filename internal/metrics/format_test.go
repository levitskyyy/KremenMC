package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/api"
)

func sampleStats() api.ServerStats {
	return api.ServerStats{
		Uptime: 3*time.Hour + 12*time.Minute, Version: "0.1.0", GameVersion: "26.3", Protocol: 777,
		TPS: 19.8, TPS1m: 20, TPS5m: 19.7, TPS15m: 14.2, MSPT: 3.21, MSPTP95: 5.1, MSPTMax: 61,
		CPU: 18.4, NumCPU: 8, GOMAXPROCS: 8, CPUCores: []float64{5, 30, 60, 95, 0, 100, 12, 49},
		Load: [3]float64{2.1, 1.8, 1.5}, HasLoad: true,
		HeapBytes: 412 << 20, RSSBytes: 530 << 20, SysBytes: 2048 << 20, GCPauseP95: 420 * time.Microsecond, GCCount: 128, Goroutines: 142,
		TickWorkers: 8, WorkerBusy: 0.23, Parallelism: 1.84, Regions: 12,
		IOWorkers: 4, ChunksGenerated: 12, LoadedChunks: 1234, Entities: 85, Players: 12, MaxPlayers: 100,
		NetIn: 12.3 * 1024, NetOut: 240.1 * 1024, PacketsIn: 180, PacketsOut: 2400,
	}
}

func TestLines(t *testing.T) {
	st := sampleStats()
	lines := Lines(&st)
	if len(lines) > MaxLines {
		t.Fatalf("%d lines", len(lines))
	}
	var plain []string
	for _, l := range lines {
		plain = append(plain, l.Plain())
	}
	want := []string{
		"▍TPS 20.0 19.7 14.2 1/5/15m",
		"▍MSPT 3.2 avg 5.1 p95 61.0 max",
		"▍Tick 8 wkr 23% busy ×1.8 · 12 rgn",
		"▍CPU 18% proc · load 2.10 1.80 1.50",
		"▍Cores ▁▃▅█▁█▁▄",
		"▍Mem heap 412M rss 530M sys 2048M",
		"▍GC 0.42ms p95 · 128 runs · 142 gor",
		"▍Chunks 1.2k loaded · 12.0 gen/s",
		"▍IO 4 wkr · 0 busy · 0 queued",
		"▍World 85 entities · 12/100 players",
		"▍Net ↓12.3 ↑240 KB/s",
		"▍Pkts ↓180 ↑2.4k /s",
		"▍Up 3h12m · 26.3 (777) · kremenmc 0.1.0",
	}
	if strings.Join(plain, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(plain, "\n"), strings.Join(want, "\n"))
	}
	tps := lines[0].Extra
	if tps[2].Color != "green" || tps[4].Color != "green" || tps[6].Color != "red" {
		t.Fatalf("tps colours %q %q %q", tps[2].Color, tps[4].Color, tps[6].Color)
	}
}

func TestCoresFallbacks(t *testing.T) {
	st := sampleStats()
	st.CPUCores, st.CPU, st.NumCPU, st.GOMAXPROCS = nil, 25, 8, 8
	if got := line("", "", cores(&st)...).Plain(); got != "▍ 2.0/8 used · per-core n/a" {
		t.Fatalf("no per-core: %q", got)
	}
	st.CPUCores = make([]float64, 64)
	st.CPUCores[3] = 64
	if got := line("", "", cores(&st)...).Plain(); got != "▍ min 0% avg 1% max 64% ×64" {
		t.Fatalf("summary: %q", got)
	}
}

func TestBossTitle(t *testing.T) {
	st := sampleStats()
	if got := BossTitle(&st).Plain(); got != "TPS 19.8 · MSPT 3.2 · CPU 18% · RAM 412/2048 MB · 12 players" {
		t.Fatalf("%q", got)
	}
	st.MemLimit = 1024 << 20
	st.Players = 1
	if got := BossTitle(&st).Plain(); !strings.HasSuffix(got, "RAM 412/1024 MB · 1 player") {
		t.Fatalf("%q", got)
	}
}

func TestTPSColor(t *testing.T) {
	for _, tt := range []struct {
		tps  float64
		want string
	}{{20, "green"}, {19.5, "green"}, {19.49, "yellow"}, {15, "yellow"}, {14.9, "red"}, {0, "red"}} {
		if got := TPSColor(tt.tps); got != tt.want {
			t.Errorf("TPSColor(%v) = %s, want %s", tt.tps, got, tt.want)
		}
	}
}

func TestFormatting(t *testing.T) {
	tests := []struct{ got, want string }{
		{uptime(42 * time.Second), "42s"},
		{uptime(12*time.Minute + 5*time.Second), "12m05s"},
		{uptime(3*time.Hour + 2*time.Minute), "3h02m"},
		{uptime(50 * time.Hour), "2d 2h"},
		{dur(85 * time.Microsecond), "85µs"},
		{dur(4200 * time.Microsecond), "4.20ms"},
		{dur(31 * time.Millisecond), "31ms"},
		{mb(11 << 30), "11.0G"},
		{count(999), "999"},
		{count(1_500_000), "1.5M"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}
