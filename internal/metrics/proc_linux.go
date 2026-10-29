package metrics

import (
	"io"
	"os"

	"github.com/levitskyyy/KremenMC/api"
)

type platform struct {
	buf        []byte
	prev, cur  []cpuTimes
	pageSize   uint64
	unreadable bool
}

func (pl *platform) sample(st *api.ServerStats) {
	if b, ok := pl.read("/proc/stat"); ok {
		pl.cur = parseProcStat(b, pl.cur[:0])
		if len(pl.prev) == len(pl.cur) && len(pl.cur) > 0 {
			st.CPUCores = coreUsage(pl.prev, pl.cur, make([]float64, 0, len(pl.cur)))
		}
		pl.prev, pl.cur = pl.cur, pl.prev
	}
	if b, ok := pl.read("/proc/loadavg"); ok {
		st.Load, st.HasLoad = parseLoadAvg(b)
	}
	if b, ok := pl.read("/proc/self/statm"); ok {
		if pages, ok := parseStatmRSS(b); ok {
			if pl.pageSize == 0 {
				pl.pageSize = uint64(os.Getpagesize())
			}
			st.RSSBytes = pages * pl.pageSize
		}
	}
}

// read loads a small procfs file into the reused buffer.
func (pl *platform) read(path string) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if pl.buf == nil {
		pl.buf = make([]byte, 0, 8<<10)
	}
	b := pl.buf[:0]
	for {
		if len(b) == cap(b) {
			b = append(b, 0)[:len(b)]
		}
		n, err := f.Read(b[len(b):cap(b)])
		b = b[:len(b)+n]
		if err == io.EOF || (err == nil && n == 0) {
			break
		}
		if err != nil {
			return nil, false
		}
	}
	pl.buf = b
	return b, true
}
