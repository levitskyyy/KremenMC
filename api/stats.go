package api

import "time"

// ServerStats is a point-in-time sample of server health. The server takes
// one per second; rates are per second over the last sample interval.
type ServerStats struct {
	Sampled     time.Time
	Uptime      time.Duration
	Version     string // engine version
	GameVersion string
	Protocol    int32

	// TPS is the rate over the last 20 ticks; TPS1m/5m/15m are rolling
	// averages of the per-second rate.
	TPS                  float64
	TPS1m, TPS5m, TPS15m float64
	// MSPT statistics over the last 100 ticks, in milliseconds.
	MSPT, MSPTP95, MSPTMax float64

	// CPU is process CPU time as a percentage of all logical CPUs.
	CPU        float64
	NumCPU     int
	GOMAXPROCS int
	// CPUCores is system-wide busy percent per logical CPU; nil when the
	// platform does not expose it without cgo (darwin).
	CPUCores []float64
	// Load is the 1/5/15 minute system load average; HasLoad is false when
	// unavailable.
	Load    [3]float64
	HasLoad bool

	HeapBytes uint64 // live and not-yet-swept heap objects
	RSSBytes  uint64
	// RSSEstimated is set when RSS is derived from Go runtime accounting
	// rather than read from the OS.
	RSSEstimated bool
	SysBytes     uint64 // memory mapped by the Go runtime
	MemLimit     uint64 // GOMEMLIMIT; 0 when unlimited
	GCPauseP95   time.Duration
	GCCount      uint64
	Goroutines   int

	TickWorkers int
	// WorkerBusy is the fraction of tick worker capacity spent ticking
	// regions during checkerboard phases; Parallelism is the average number
	// of workers running concurrently in those phases.
	WorkerBusy  float64
	Parallelism float64
	Regions     int

	IOWorkers, IOQueued, IOActive int
	ChunksGenerated               float64 // per second
	LoadedChunks                  int
	Entities                      int
	Players, MaxPlayers           int

	NetIn, NetOut         float64 // bytes per second on the wire
	PacketsIn, PacketsOut float64
}

// StatsProvider is implemented by servers that sample ServerStats.
type StatsProvider interface {
	Stats() ServerStats
}
