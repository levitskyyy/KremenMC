package world

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/reftest"
)

type emptyGen struct{}

func (emptyGen) Generate(c *Chunk) { c.Finish() }

// TestFluidScenes replays testdata/fluid_scenes.txt (regenerate the golden
// with KREMENMC_REF=1) and compares the block states tick by tick.
func TestFluidScenes(t *testing.T) {
	script, err := os.ReadFile("testdata/fluid_scenes.txt")
	if err != nil {
		t.Fatal(err)
	}
	gold := reftest.Golden(t, "fluid_scenes.golden", "kremenmc.probe.fluid.Scenes", mustAbs(t, "testdata/fluid_scenes.txt"))
	want := sceneLines(gold)
	got := sceneLines(runFluidScript(t, script))
	for name, w := range want {
		g := got[name]
		bad := 0
		var first []string
		gi := map[string]bool{}
		for _, l := range g {
			gi[l] = true
		}
		wi := map[string]bool{}
		for _, l := range w {
			wi[l] = true
			if !gi[l] {
				bad++
				if len(first) < 6 {
					first = append(first, "missing "+l)
				}
			}
		}
		for _, l := range g {
			if !wi[l] {
				bad++
				if len(first) < 12 {
					first = append(first, "extra   "+l)
				}
			}
		}
		if bad > 0 {
			t.Errorf("scene %s: %d/%d lines differ\n  %s", name, bad, len(w), strings.Join(first, "\n  "))
		} else {
			t.Logf("scene %s: %d lines identical", name, len(w))
		}
	}
}

func mustAbs(t *testing.T, p string) string {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd + "/" + p
}

// sceneLines groups "t ..." lines by scene.
func sceneLines(b []byte) map[string][]string {
	out := map[string][]string{}
	cur := ""
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "scene "):
			cur = l[6:]
		case strings.HasPrefix(l, "t "):
			out[cur] = append(out[cur], l)
		}
	}
	return out
}

func runFluidScript(t *testing.T, script []byte) []byte {
	var out bytes.Buffer
	var w *World
	var box [6]int
	var prev map[[3]int]data.StateID
	base := int64(0)
	// Scenes are replayed 40 blocks east of where the reference ran them:
	// at x=128 and x=256 they would straddle a region border, where a
	// change forwarded to the neighbouring region shows up one tick late
	// (its scheduled ticks still count from the original tick). Set
	// FLUID_SHIFT=0 to see that.
	shift := 40
	if v := os.Getenv("FLUID_SHIFT"); v != "" {
		shift, _ = strconv.Atoi(v)
	}
	defer func() {
		if w != nil {
			w.Close()
		}
	}()
	snapshot := func(tick int64, diff bool) {
		for y := box[1]; y <= box[4]; y++ {
			for z := box[2]; z <= box[5]; z++ {
				for x := box[0]; x <= box[3]; x++ {
					s := w.Block(x+shift, y, z)
					p := [3]int{x, y, z}
					if diff && prev[p] != s {
						fmt.Fprintf(&out, "t %d %d %d %d %s\n", tick, x, y, z, strings.TrimPrefix(data.StateString(s), "minecraft:"))
					}
					prev[p] = s
				}
			}
		}
	}
	for line := range strings.Lines(string(script)) {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Fields(line)
		switch f[0] {
		case "scene":
			if w != nil {
				w.Close()
			}
			cfg := Config{Generator: emptyGen{}, Workers: 2, TickWorkers: 2}
			if f[2] == "nether" {
				cfg.Dim, cfg.Ultrawarm = Nether, true
			}
			w, _ = New(cfg)
			for i := range box {
				box[i], _ = strconv.Atoi(f[3+i])
			}
			w.AddTicket(ChunkAt((box[0]+box[3])/2+shift, (box[2]+box[5])/2), 3, TicketForced)
			waitFor(t, func() bool { return w.Loaded() >= 49 })
			w.DoTick()
			base = w.Tick()
			prev = map[[3]int]data.StateID{}
			fmt.Fprintf(&out, "scene %s\n", f[1])
		case "cmd":
			a := make([]int, 0, 6)
			for _, v := range f[2:] {
				if n, err := strconv.Atoi(v); err == nil {
					a = append(a, n)
				}
			}
			st, ok := data.ParseState(f[len(f)-1])
			if !ok {
				t.Fatalf("bad state in %q", line)
			}
			if f[1] == "setblock" {
				a = append(a, a...)
			}
			for x := a[0]; x <= a[3]; x++ {
				for z := a[2]; z <= a[5]; z++ {
					for y := a[1]; y <= a[4]; y++ {
						w.SetBlock(x+shift, y, z, st)
					}
				}
			}
		case "run":
			n, _ := strconv.Atoi(f[1])
			flushInbox(w)
			snapshot(0, false)
			for range n {
				w.DoTick()
				snapshot(w.Tick()-base, true)
			}
		}
	}
	return out.Bytes()
}

// flushInbox applies queued changes now, between ticks, like commands run
// on the reference between ticks.
func flushInbox(w *World) {
	w.regMu.RLock()
	rs := make([]*Region, 0, len(w.regions))
	for _, r := range w.regions {
		rs = append(rs, r)
	}
	w.regMu.RUnlock()
	t := w.ctxPool.Get().(*TickContext)
	defer w.ctxPool.Put(t)
	for again := true; again; {
		again = false
		for _, r := range rs {
			r.inboxMu.Lock()
			msgs := r.inbox
			r.inbox = nil
			r.inboxMu.Unlock()
			t.r = r
			for _, m := range msgs {
				again = true
				t.at = m.at
				m.fn(t)
			}
		}
	}
	t.r, t.at = nil, 0
}
