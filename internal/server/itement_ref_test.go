package server

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// probeArena is the probe's block layout: stone below y=99, the floor block
// at y=99, optional water at y=100..101, air elsewhere.
type probeArena struct {
	floor data.StateID
	water bool
}

func (a probeArena) Block(x, y, z int) data.StateID {
	if x < -4 || x > 5 || z < -4 || z > 4 {
		return data.Air
	}
	switch {
	case y < 99 && y >= 96:
		return data.MustState("minecraft:stone")
	case y == 99:
		return a.floor
	case a.water && (y == 100 || y == 101):
		return data.MustState("minecraft:water")
	}
	return data.Air
}

func testItem(t testing.TB, id int32, pos, vel api.Vec3, n int32) *itemEnt {
	w := &World{name: "t"}
	stone, _ := item.IDOf("stone")
	it := w.newItemEntity(pos, vel, item.Stack{Item: stone, Count: n})
	it.m.e.ID = id
	return it
}

func pf(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(err)
	}
	return v
}

func itemCases() []string {
	var cs []string
	add := func(format string, a ...any) { cs = append(cs, fmt.Sprintf(format, a...)) }
	n := 0
	for _, fl := range []string{"minecraft:stone", "minecraft:ice", "minecraft:blue_ice", "minecraft:soul_sand", "minecraft:honey_block"} {
		add("traj|%d|%s|0|0.5|101|0.5|0.08|0.2|-0.05|60", n, fl)
		n++
		add("traj|%d|%s|0|0.3|100|0.7|0.1|0|0.02|40", n, fl)
		n++
	}
	add("traj|%d|minecraft:stone|1|0.5|101.5|0.5|0.05|0.2|0.01|80", n)
	n++
	add("traj|%d|minecraft:stone|1|0.5|103|0.5|0|0|0|80", n)
	n++
	add("traj|%d|minecraft:stone|0|0.5|100|0.5|0|0|0|20", n)
	n++
	for _, m := range [][3]string{{"5", "7", "0.3"}, {"7", "5", "0.3"}, {"5", "5", "0.3"}, {"64", "5", "0.3"}, {"60", "10", "0.3"}, {"3", "4", "0.7"}, {"3", "4", "1.2"}} {
		add("merge|%d|%s|%s|%s|90", n, m[0], m[1], m[2])
		n++
	}
	for _, seed := range []int64{42, 7, -9, 123456789} {
		add("pop|%d|%d|3", n, seed)
		n++
	}
	return cs
}

// TestItemEntityMatchesReference replays item entity motion, merging and
// popResource spawning against the reference.
func TestItemEntityMatchesReference(t *testing.T) {
	cases := itemCases()
	inPath := filepath.Join(t.TempDir(), "item_in.txt")
	if os.Getenv("KREMENMC_REF") == "1" {
		os.WriteFile(inPath, []byte(strings.Join(cases, "\n")+"\n"), 0o644)
	}
	golden := reftest.Golden(t, "itement_out.txt", "kremenmc.probe.dig.ItemEnt", inPath)
	want := map[string][]string{}
	sc := bufio.NewScanner(bytes.NewReader(golden))
	sc.Buffer(nil, 1<<22)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) > 1 {
			want[f[0]+"|"+f[1]] = f
		}
		if f[0] == "error" {
			t.Errorf("probe: %s", sc.Text())
		}
	}
	for _, c := range cases {
		f := strings.Split(c, "|")
		w := want[f[0]+"|"+f[1]]
		if w == nil {
			t.Errorf("no reference for %s", c)
			continue
		}
		switch f[0] {
		case "traj":
			id, _ := strconv.Atoi(strings.TrimPrefix(w[2], "id="))
			arena := probeArena{floor: data.MustState(f[2]), water: f[3] == "1"}
			it := testItem(t, int32(id), api.Vec3{X: pf(f[4]), Y: pf(f[5]), Z: pf(f[6])}, api.Vec3{X: pf(f[7]), Y: pf(f[8]), Z: pf(f[9])}, 1)
			for i, step := range w[3:] {
				it.step(arena, -64, func() []*Mob { return nil })
				v := strings.Split(step, ",")
				e := it.m.e
				got := [6]float64{e.Pos.X, e.Pos.Y, e.Pos.Z, e.Vel.X, e.Vel.Y, e.Vel.Z}
				ok := (v[6] == "1") == e.OnGround
				for k := range 6 {
					ok = ok && got[k] == pf(v[k])
				}
				if !ok {
					t.Errorf("%s tick %d: got %v ground=%v, want %s", c, i+1, got, e.OnGround, step)
					break
				}
			}
		case "merge":
			ca, _ := strconv.Atoi(f[2])
			cb, _ := strconv.Atoi(f[3])
			ids := strings.Split(strings.TrimPrefix(w[2], "ids="), ",")
			ia, _ := strconv.Atoi(ids[0])
			ib, _ := strconv.Atoi(ids[1])
			a := testItem(t, int32(ia), api.Vec3{X: 0.5, Y: 100, Z: 0.5}, api.Vec3{}, int32(ca))
			b := testItem(t, int32(ib), api.Vec3{X: 0.5 + pf(f[4]), Y: 100, Z: 0.5}, api.Vec3{}, int32(cb))
			a.delay, b.delay = 10, 30
			arena := probeArena{floor: data.MustState("minecraft:stone")}
			all := func() []*Mob { return []*Mob{a.m, b.m} }
			var out []string
			n, _ := strconv.Atoi(f[5])
			for range n {
				if !a.m.removed.Load() {
					a.step(arena, -64, all)
				}
				if !b.m.removed.Load() {
					b.step(arena, -64, all)
				}
				show := func(x *itemEnt) string {
					if x.m.removed.Load() {
						return "x"
					}
					return fmt.Sprintf("%d:%d", x.stack.Count, x.age)
				}
				out = append(out, show(a)+"/"+show(b))
				if a.m.removed.Load() || b.m.removed.Load() {
					s := a
					if a.m.removed.Load() {
						s = b
					}
					p := s.m.e.Pos
					out = append(out, "pos="+jd(p.X)+","+jd(p.Y)+","+jd(p.Z), fmt.Sprintf("delay=%d", s.delay))
					break
				}
			}
			if got, ref := strings.Join(out, " "), strings.Join(w[3:], " "); got != ref {
				t.Errorf("%s\n got %s\nwant %s", c, got, ref)
			}
		case "pop":
			seed, _ := strconv.ParseInt(f[2], 10, 64)
			r := rng.NewLegacy(seed)
			at := popResourceOffset(r, api.BlockPos{X: 1, Y: 100, Z: 0})
			v := strings.Split(w[2], ",")
			got := [3]float64{at.X, at.Y, at.Z}
			ok := w[4] == fmt.Sprintf("delay=%d", itemPickupDefault) && w[5] == fmt.Sprintf("next=%d", r.NextLong())
			for k := range 3 {
				ok = ok && got[k] == pf(v[k])
			}
			// The toss velocity comes from the entity's own random.
			ok = ok && pf(v[4]) == 0.2 && max(pf(v[3]), -pf(v[3]), pf(v[5]), -pf(v[5])) <= 0.1
			if !ok {
				t.Errorf("%s: got %v, want %v", c, got, w[2:])
			}
		}
	}
}

// jd formats like Java's Double.toString for the values the merge case
// prints (positions with short decimal forms).
func jd(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}
