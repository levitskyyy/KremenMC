package feature_test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/tools/refbench/bench"
	"github.com/levitskyyy/KremenMC/tools/refbench/compare"
	"github.com/levitskyyy/KremenMC/tools/refbench/gens"
	"github.com/levitskyyy/KremenMC/tools/refbench/refgen"
)

// TestTreeMeasure prints, per tree-related block name, the reference count,
// kremenmc count and position matches over full border chunks (TREEMEASURE=seed).
func TestTreeMeasure(t *testing.T) {
	s := os.Getenv("TREEMEASURE")
	if s == "" {
		t.Skip()
	}
	seed, _ := strconv.ParseInt(s, 10, 64)
	ref, err := reftest.Open()
	if err != nil {
		t.Fatal(err)
	}
	o := bench.Options{Ref: ref, Seed: seed, Dim: "overworld", Spec: "border", Stage: "full", Gen: "features", Structures: true}
	rs, pos, err := bench.Reference(o)
	if os.Getenv("TREECHUNKS") != "" {
		fmt.Println("chunks", pos)
	}
	if err != nil {
		t.Fatal(err)
	}
	f, err := gens.Lookup("features")
	if err != nil {
		t.Fatal(err)
	}
	dimName := refgen.DimName(refgen.DimID("overworld"))
	dim, err := gens.Dimension(dimName)
	if err != nil {
		t.Fatal(err)
	}
	gpos := pos
	if os.Getenv("TREERING") != "" {
		// Also decorate the ring around the compared chunks, as the
		// reference world has it.
		seen := map[compare.Pos]bool{}
		for _, p := range pos {
			seen[p] = true
		}
		gpos = append([]compare.Pos(nil), pos...)
		for _, p := range pos {
			for dx := int32(-1); dx <= 1; dx++ {
				for dz := int32(-1); dz <= 1; dz++ {
					q := compare.Pos{X: p.X + dx, Z: p.Z + dz}
					if !seen[q] {
						seen[q] = true
						gpos = append(gpos, q)
					}
				}
			}
		}
	}
	got, err := bench.Generate(context.Background(), f(seed, dimName), dim, gpos, "full", 0)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`_log$|_leaves$|^minecraft:vine$|^minecraft:cocoa$|mushroom|^minecraft:podzol$|_wood$`)
	type st struct{ ref, got, match int }
	m := map[string]*st{}
	name := func(b string) string { n, _, _ := strings.Cut(b, "["); return n }
	get := func(n string) *st {
		if m[n] == nil {
			m[n] = &st{}
		}
		return m[n]
	}
	for _, p := range pos {
		rc, gc := rs[p], got[p]
		if rc == nil || gc == nil {
			continue
		}
		for sy := rc.MinSection; sy < rc.MinSection+len(rc.Sections); sy++ {
			a, b := rc.Section(sy), gc.Section(sy)
			for i := range 4096 {
				x, y, z := i&15, i>>8, (i>>4)&15
				var an, bn string
				if a != nil {
					an = name(a.Block(x, y, z))
				}
				if b != nil {
					bn = name(b.Block(x, y, z))
				}
				if c := os.Getenv("TREECOL"); c != "" && slices.Contains(strings.Split(c, ";"), fmt.Sprintf("%d,%d", int(p.X)*16+x, int(p.Z)*16+z)) && (an != bn || an != "minecraft:air") {
					fmt.Printf("col %d,%d y=%d ref=%s got=%s\n", int(p.X)*16+x, int(p.Z)*16+z, sy*16+y, a.Block(x, y, z), b.Block(x, y, z))
				}
				if w := os.Getenv("TREEPOS"); w != "" && (an == w || bn == w) {
					fmt.Printf("pos %d %d %d ref=%s got=%s\n", int(p.X)*16+x, sy*16+y, int(p.Z)*16+z, a.Block(x, y, z), b.Block(x, y, z))
				}
				if re.MatchString(an) {
					get(an).ref++
					if an == bn {
						get(an).match++
					}
				}
				if re.MatchString(bn) {
					get(bn).got++
				}
			}
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		v := m[k]
		fmt.Printf("%-36s ref %6d got %6d match %6d\n", k, v.ref, v.got, v.match)
	}
}
