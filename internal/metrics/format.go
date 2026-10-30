package metrics

import (
	"strconv"
	"strings"
	"time"

	"github.com/levitskyyy/KremenMC/api"
)

// MaxLines is the sidebar line limit.
const MaxLines = 15

const (
	accentTick  = "aqua"
	accentSys   = "light_purple"
	accentWorld = "green"
	accentNet   = "gold"
	accentMeta  = "dark_gray"

	colLabel = "gray"
	colUnit  = "dark_gray"
	colValue = "white"
)

// TPSColor grades a tick rate: green from 19.5, yellow from 15, red below.
func TPSColor(tps float64) string {
	switch {
	case tps >= 19.5:
		return "green"
	case tps >= 15:
		return "yellow"
	}
	return "red"
}

func msptColor(ms float64) string {
	switch {
	case ms <= 25:
		return "green"
	case ms <= 40:
		return "yellow"
	case ms <= 50:
		return "gold"
	}
	return "red"
}

func pctColor(p float64) string {
	switch {
	case p < 50:
		return "green"
	case p < 80:
		return "yellow"
	}
	return "red"
}

func val(s, color string) api.Component { return api.Colored(s, color) }
func unit(s string) api.Component       { return api.Colored(s, colUnit) }
func white(s string) api.Component      { return api.Colored(s, colValue) }

func line(accent, label string, parts ...api.Component) api.Component {
	extra := make([]api.Component, 0, len(parts)+2)
	extra = append(extra, api.Colored("▍", accent), api.Colored(label+" ", colLabel))
	return api.Component{Extra: append(extra, parts...)}
}

// Title is the sidebar heading.
func Title() api.Component {
	return api.Component{Extra: []api.Component{api.Colored("KremenMC", "gold").WithBold(true), api.Colored(" admin", "yellow")}}
}

// Lines renders st as at most MaxLines lines, grouped by section accent.
func Lines(st *api.ServerStats) []api.Component {
	ms := func(v float64) api.Component { return val(f1(v), msptColor(v)) }
	tps := func(v float64) api.Component { return val(f1(v), TPSColor(v)) }
	cpu := []api.Component{val(pct(st.CPU), pctColor(st.CPU)), unit(" proc")}
	if st.HasLoad {
		cpu = append(cpu, unit(" · load "), white(f2(st.Load[0])+" "+f2(st.Load[1])+" "+f2(st.Load[2])))
	}
	rss := mb(st.RSSBytes)
	if st.RSSEstimated {
		rss = "~" + rss
	}
	return []api.Component{
		line(accentTick, "TPS", tps(st.TPS1m), white(" "), tps(st.TPS5m), white(" "), tps(st.TPS15m), unit(" 1/5/15m")),
		line(accentTick, "MSPT", ms(st.MSPT), unit(" avg "), ms(st.MSPTP95), unit(" p95 "), ms(st.MSPTMax), unit(" max")),
		line(accentTick, "Tick", white(strconv.Itoa(st.TickWorkers)), unit(" wkr "),
			white(pct(st.WorkerBusy*100)), unit(" busy "),
			white("×"+f1(st.Parallelism)), unit(" · "), white(strconv.Itoa(st.Regions)), unit(" rgn")),
		line(accentSys, "CPU", cpu...),
		line(accentSys, "Cores", cores(st)...),
		line(accentSys, "Mem", unit("heap "), white(mb(st.HeapBytes)), unit(" rss "), white(rss), unit(" sys "), white(mb(st.SysBytes))),
		line(accentSys, "GC", white(dur(st.GCPauseP95)), unit(" p95 · "), white(count(float64(st.GCCount))), unit(" runs · "),
			white(count(float64(st.Goroutines))), unit(" gor")),
		line(accentWorld, "Chunks", white(count(float64(st.LoadedChunks))), unit(" loaded · "), white(f1(st.ChunksGenerated)), unit(" gen/s")),
		line(accentWorld, "IO", white(strconv.Itoa(st.IOWorkers)), unit(" wkr · "), white(strconv.Itoa(st.IOActive)), unit(" busy · "),
			val(strconv.Itoa(st.IOQueued), queueColor(st.IOQueued)), unit(" queued")),
		line(accentWorld, "World", white(count(float64(st.Entities))), unit(" entities · "),
			white(strconv.Itoa(st.Players)+"/"+strconv.Itoa(st.MaxPlayers)), unit(" players")),
		line(accentNet, "Net", unit("↓"), white(kb(st.NetIn)), unit(" ↑"), white(kb(st.NetOut)), unit(" KB/s")),
		line(accentNet, "Pkts", unit("↓"), white(count(st.PacketsIn)), unit(" ↑"), white(count(st.PacketsOut)), unit(" /s")),
		line(accentMeta, "Up", white(uptime(st.Uptime)), unit(" · "), white(st.GameVersion), unit(" ("+strconv.Itoa(int(st.Protocol))+") · "),
			white("KremenMC "+st.Version)),
	}
}

// BossTitle is the one-line summary shown in the boss bar.
func BossTitle(st *api.ServerStats) api.Component {
	limit := st.MemLimit
	if limit == 0 {
		limit = st.SysBytes
	}
	players := " players"
	if st.Players == 1 {
		players = " player"
	}
	sep := unit(" · ")
	return api.Component{Extra: []api.Component{
		api.Colored("TPS ", colLabel), val(f1(st.TPS), TPSColor(st.TPS)), sep,
		api.Colored("MSPT ", colLabel), val(f1(st.MSPT), msptColor(st.MSPT)), sep,
		api.Colored("CPU ", colLabel), val(pct(st.CPU), pctColor(st.CPU)), sep,
		api.Colored("RAM ", colLabel), white(strconv.FormatUint(st.HeapBytes>>20, 10) + "/" + strconv.FormatUint(limit>>20, 10)), unit(" MB"), sep,
		white(strconv.Itoa(st.Players)), api.Colored(players, colLabel),
	}}
}

const (
	maxCoreBars = 32
	barLevels   = "▁▂▃▄▅▆▇█"
)

// cores draws one bar glyph per core, a min/avg/max summary for large
// machines, or process usage in cores when per-core data is unavailable.
func cores(st *api.ServerStats) []api.Component {
	cs := st.CPUCores
	if len(cs) == 0 {
		used := st.CPU / 100 * float64(st.NumCPU)
		return []api.Component{white(f1(used)), unit("/" + strconv.Itoa(st.GOMAXPROCS) + " used · per-core n/a")}
	}
	if len(cs) > maxCoreBars {
		lo, hi, sum := cs[0], cs[0], 0.0
		for _, c := range cs {
			lo, hi, sum = min(lo, c), max(hi, c), sum+c
		}
		avg := sum / float64(len(cs))
		return []api.Component{
			unit("min "), val(pct(lo), pctColor(lo)), unit(" avg "), val(pct(avg), pctColor(avg)),
			unit(" max "), val(pct(hi), pctColor(hi)), unit(" ×" + strconv.Itoa(len(cs))),
		}
	}
	levels := []rune(barLevels)
	var out []api.Component
	var run strings.Builder
	color := ""
	for _, c := range cs {
		col := pctColor(c)
		if col != color && run.Len() > 0 {
			out = append(out, val(run.String(), color))
			run.Reset()
		}
		color = col
		run.WriteRune(levels[min(len(levels)-1, int(c/100*float64(len(levels))))])
	}
	return append(out, val(run.String(), color))
}

func queueColor(n int) string {
	switch {
	case n == 0:
		return "green"
	case n < 64:
		return "yellow"
	}
	return "red"
}

func f1(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }
func f2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

func pct(v float64) string { return strconv.FormatFloat(v, 'f', 0, 64) + "%" }

func mb(b uint64) string {
	if b >= 10<<30 {
		return strconv.FormatFloat(float64(b)/(1<<30), 'f', 1, 64) + "G"
	}
	return strconv.FormatUint(b>>20, 10) + "M"
}

func kb(bps float64) string {
	k := bps / 1024
	if k < 100 {
		return strconv.FormatFloat(k, 'f', 1, 64)
	}
	return strconv.FormatFloat(k, 'f', 0, 64)
}

func count(v float64) string {
	switch {
	case v < 1000:
		return strconv.FormatFloat(v, 'f', 0, 64)
	case v < 1e6:
		return strconv.FormatFloat(v/1e3, 'f', 1, 64) + "k"
	}
	return strconv.FormatFloat(v/1e6, 'f', 1, 64) + "M"
}

func dur(d time.Duration) string {
	switch {
	case d <= 0:
		return "0ms"
	case d < 100*time.Microsecond:
		return strconv.FormatInt(d.Microseconds(), 10) + "µs"
	case d < 10*time.Millisecond:
		return strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', 2, 64) + "ms"
	}
	return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
}

func uptime(d time.Duration) string {
	s := int64(d / time.Second)
	days, h, m, sec := s/86400, s/3600%24, s/60%60, s%60
	switch {
	case days > 0:
		return strconv.FormatInt(days, 10) + "d " + strconv.FormatInt(h, 10) + "h"
	case h > 0:
		return strconv.FormatInt(h, 10) + "h" + pad2(m) + "m"
	case m > 0:
		return strconv.FormatInt(m, 10) + "m" + pad2(sec) + "s"
	}
	return strconv.FormatInt(sec, 10) + "s"
}

func pad2(v int64) string {
	if v < 10 {
		return "0" + strconv.FormatInt(v, 10)
	}
	return strconv.FormatInt(v, 10)
}
