package metrics

import "bytes"

// cpuTimes is one /proc/stat cpuN line reduced to busy and total jiffies.
type cpuTimes struct{ busy, total uint64 }

// parseProcStat appends per-core times from /proc/stat content, skipping the
// aggregate "cpu " line. Idle and iowait count as not busy.
func parseProcStat(b []byte, dst []cpuTimes) []cpuTimes {
	for len(b) > 0 {
		line := b
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			line, b = b[:i], b[i+1:]
		} else {
			b = nil
		}
		if len(line) < 4 || line[0] != 'c' || line[1] != 'p' || line[2] != 'u' || line[3] < '0' || line[3] > '9' {
			continue
		}
		var t cpuTimes
		var fb [12][]byte
		field := 0
		for _, f := range fields(line, fb[:0]) {
			if field == 0 {
				field++
				continue
			}
			v, ok := parseUint(f)
			if !ok {
				break
			}
			// user nice system idle iowait irq softirq steal; guest time
			// is already included in user/nice.
			if field <= 8 {
				t.total += v
				if field != 4 && field != 5 {
					t.busy += v
				}
			}
			field++
		}
		if field > 4 {
			dst = append(dst, t)
		}
	}
	return dst
}

// coreUsage converts two /proc/stat samples into busy percentages.
func coreUsage(prev, cur []cpuTimes, dst []float64) []float64 {
	if len(prev) != len(cur) {
		return dst
	}
	for i := range cur {
		dt := cur[i].total - prev[i].total
		db := cur[i].busy - prev[i].busy
		if cur[i].total < prev[i].total || cur[i].busy < prev[i].busy || dt == 0 {
			dst = append(dst, 0)
			continue
		}
		dst = append(dst, clampPct(float64(db)/float64(dt)*100))
	}
	return dst
}

// parseLoadAvg reads the first three fields of /proc/loadavg.
func parseLoadAvg(b []byte) (l [3]float64, ok bool) {
	var fb [3][]byte
	fs := fields(b, fb[:0])
	if len(fs) < 3 {
		return l, false
	}
	for i := range 3 {
		v, ok := parseFixed(fs[i])
		if !ok {
			return l, false
		}
		l[i] = v
	}
	return l, true
}

// parseStatmRSS returns the resident page count from /proc/self/statm.
func parseStatmRSS(b []byte) (uint64, bool) {
	var fb [2][]byte
	fs := fields(b, fb[:0])
	if len(fs) < 2 {
		return 0, false
	}
	return parseUint(fs[1])
}

// fields splits b on whitespace into dst, up to cap(dst) fields.
func fields(b []byte, dst [][]byte) [][]byte {
	for len(dst) < cap(dst) {
		for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\n') {
			b = b[1:]
		}
		if len(b) == 0 {
			break
		}
		i := 0
		for i < len(b) && b[i] != ' ' && b[i] != '\t' && b[i] != '\n' {
			i++
		}
		dst = append(dst, b[:i])
		b = b[i:]
	}
	return dst
}

func parseUint(b []byte) (uint64, bool) {
	if len(b) == 0 {
		return 0, false
	}
	var v uint64
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + uint64(c-'0')
	}
	return v, true
}

// parseFixed parses a non-negative decimal like "1.25".
func parseFixed(b []byte) (float64, bool) {
	i := bytes.IndexByte(b, '.')
	if i < 0 {
		v, ok := parseUint(b)
		return float64(v), ok
	}
	whole, ok := parseUint(b[:i])
	if !ok {
		return 0, false
	}
	frac := b[i+1:]
	if len(frac) == 0 {
		return float64(whole), true
	}
	f, ok := parseUint(frac)
	if !ok {
		return 0, false
	}
	div := 1.0
	for range frac {
		div *= 10
	}
	return float64(whole) + float64(f)/div, true
}
