package item

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
)

func TestHungerEat(t *testing.T) {
	tests := []struct {
		food  int32
		sat   float32
		f     FoodInfo
		wfood int32
		wsat  float32
	}{
		{10, 2, FoodInfo{4, 2.4, false}, 14, 4.4},
		{18, 18, FoodInfo{4, 2.4, false}, 20, 20},
		{0, 0, FoodInfo{20, 30, false}, 20, 20},
	}
	for _, tc := range tests {
		h := Hunger{Food: tc.food, Saturation: tc.sat}
		h.Eat(tc.f)
		if h.Food != tc.wfood || h.Saturation != tc.wsat {
			t.Errorf("eat %+v: got %d/%v, want %d/%v", tc, h.Food, h.Saturation, tc.wfood, tc.wsat)
		}
	}
}

func fmtF(f float32) string {
	s := strconv.FormatFloat(float64(f), 'f', -1, 32)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

func TestReferenceHunger(t *testing.T) {
	r := rand.New(rand.NewPCG(20, 26))
	type hcase struct {
		diff            int32
		health, sat, ex float32
		food            int32
		n               int
	}
	exs := []float32{0, 0, 0.005, 0.1, 0.5, 2, 4.5}
	var cases []hcase
	for range 400 {
		food := int32(r.IntN(21))
		c := hcase{diff: int32(1 + r.IntN(3)), health: float32(1+r.IntN(40)) / 2, food: food, sat: float32(r.IntN(int(food)*2+1)) / 2, ex: exs[r.IntN(len(exs))], n: 100 + r.IntN(150)}
		cases = append(cases, c)
	}
	in, _ := filepath.Abs(filepath.Join("testdata", "hunger_in.txt"))
	if os.Getenv("KREMENMC_REF") == "1" {
		var b bytes.Buffer
		for i, c := range cases {
			fmt.Fprintf(&b, "tick %d %d %s %d %s %s %d\n", i, c.diff, fmtF(c.health), c.food, fmtF(c.sat), fmtF(c.ex), c.n)
		}
		if err := os.WriteFile(in, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden := string(reftest.Golden(t, "hunger_out.txt", "kremenmc.probe.item.Hunger", in))
	want := map[string]string{}
	for _, l := range strings.Split(golden, "\n") {
		f := strings.SplitN(l, " ", 3)
		if len(f) == 3 && f[0] == "tick" {
			want[f[1]] = f[2]
		}
	}
	bad := 0
	for i, c := range cases {
		h := Hunger{Food: c.food, Saturation: c.sat}
		health := c.health
		var out []string
		starves := 0
		// The reference harness player is never ticked, so only its first
		// starvation hit lands; compare up to the second one.
		for range c.n {
			h.AddExhaustion(c.ex)
			heal, starve := h.Tick(c.diff, true, health, 20)
			if heal > 0 && health > 0 {
				health = min(health+heal, 20)
			}
			if starve {
				if starves++; starves == 2 {
					break
				}
				health = max(health-1, 0)
			}
			out = append(out, fmtF(health)+"/"+strconv.Itoa(int(h.Food))+"/"+fmtF(h.Saturation))
		}
		w := strings.Fields(want[strconv.Itoa(i)])
		if len(w) > len(out) {
			w = w[:len(out)]
		}
		if got := strings.Join(out, " "); got != strings.Join(w, " ") {
			g := strings.Fields(got)
			k := 0
			for k < len(g) && k < len(w) && g[k] == w[k] {
				k++
			}
			if bad++; bad <= 3 {
				t.Errorf("case %d %+v: first diff at tick %d: got %v want %v (prev %v)", i, c, k, g[k], w[min(k, len(w)-1)], g[max(k-1, 0)])
			}
		}
	}
	t.Logf("%d hunger cases, %d mismatched", len(cases), bad)
}
