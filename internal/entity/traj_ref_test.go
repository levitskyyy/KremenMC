package entity

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/reftest"
)

// Cases whose reference behaviour is not modelled exactly yet (documented
// in docs/vanilla-parity.md); they are still run and reported with -v.
var trajApprox = map[string]bool{
	"zombie_slime": true, // slime bounce height
	"zombie_lava":  true, // partial-submersion lava rule
	"item_lava":    true, // items burn in lava
}

type trajTick struct {
	pos, vel api.Vec3
	ground   bool
	removed  bool
}

func parseTraj(t *testing.T, b []byte) (map[string][]trajTick, []string) {
	out := map[string][]trajTick{}
	var order []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 10 || f[0] != "case" {
			continue
		}
		var v [7]float64
		for i := range v {
			x, err := strconv.ParseFloat(f[3+i], 64)
			if err != nil {
				t.Fatal(err)
			}
			v[i] = x
		}
		if _, ok := out[f[1]]; !ok {
			order = append(order, f[1])
		}
		out[f[1]] = append(out[f[1]], trajTick{
			pos: api.Vec3{X: v[0], Y: v[1], Z: v[2]}, vel: api.Vec3{X: v[3], Y: v[4], Z: v[5]},
			ground: v[6] == 1, removed: len(f) > 10 && f[10] == "removed",
		})
	}
	return out, order
}

func trajWorld(t *testing.T, script string) (blocks, map[string][]string) {
	src := blocks{}
	cases := map[string][]string{}
	for line := range strings.Lines(script) {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch {
		case f[0] == "setup" && f[1] == "fill":
			var c [6]int
			for i := range c {
				c[i], _ = strconv.Atoi(f[2+i])
			}
			src.fill(mustState(strings.TrimPrefix(f[8], "minecraft:")), c[0], c[1], c[2], c[3], c[4], c[5])
		case f[0] == "case":
			cases[f[1]] = f
		}
	}
	return src, cases
}

func TestTrajectoriesMatchReference(t *testing.T) {
	scriptPath, _ := filepath.Abs(filepath.Join("testdata", "traj.script"))
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	ref, order := parseTraj(t, reftest.Golden(t, "traj.golden", "kremenmc.probe.combat.Traj", scriptPath))
	src, cases := trajWorld(t, string(script))
	for _, name := range order {
		t.Run(name, func(t *testing.T) {
			want := ref[name]
			f := cases[name]
			typ := f[2]
			if i := strings.IndexByte(typ, ':'); i >= 0 && !strings.HasPrefix(typ, "minecraft:") {
				typ = typ[:i]
			}
			et, ok := data.Canonical().EntityByName(strings.TrimPrefix(typ, "minecraft:"))
			if !ok {
				t.Fatalf("unknown type %s", typ)
			}
			var bad string
			for phase := range 4 {
				bad = runTraj(et, want, src, int32(phase))
				if bad == "" {
					return
				}
			}
			if trajApprox[name] {
				t.Logf("approximate: %s", bad)
				return
			}
			t.Error(bad)
		})
	}
}

func runTraj(et *data.EntityType, want []trajTick, src blocks, phase int32) string {
	e := New(et)
	e.ID = phase
	e.Pos, e.Vel = want[0].pos, want[0].vel
	if e.Kind == KindLiving {
		e.Vel = want[0].vel
	}
	for i := 1; i < len(want); i++ {
		w := want[i]
		var removed bool
		if e.Kind == KindThrown || e.Kind == KindArrow {
			e.Prev = e.Pos
			e.Age++
			e.updateFluids(src)
			h := e.StepProjectile(src, nil)
			removed = e.Kind == KindThrown && h.Kind != HitNone
		} else {
			e.Tick(src)
			if et.Name == "falling_block" && e.OnGround {
				removed = true
			}
		}
		if !close(e.Pos, w.pos) || !close(e.Vel, w.vel) || e.OnGround != w.ground && e.Kind != KindThrown && e.Kind != KindArrow || removed != w.removed {
			return fmt.Sprintf("tick %d: got pos %v vel %v ground %v removed %v; want pos %v vel %v ground %v removed %v",
				i, e.Pos, e.Vel, e.OnGround, removed, w.pos, w.vel, w.ground, w.removed)
		}
		if removed {
			break
		}
	}
	return ""
}

func close(a, b api.Vec3) bool {
	const tol = 1e-9
	return math.Abs(a.X-b.X) <= tol && math.Abs(a.Y-b.Y) <= tol && math.Abs(a.Z-b.Z) <= tol
}
