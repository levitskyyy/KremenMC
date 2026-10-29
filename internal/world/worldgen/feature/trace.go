package feature

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
)

// RunTraced is Run for comparisons with kremenmc.probe.feat_region.Region: every
// block write, and the block and heightmap reads of placed features whose
// key contains one of keep, go to w in the probe's line format.
func (s *Stage) RunTraced(sc *gen.StageContext, w io.Writer, keep []string) error {
	inner := &Level{rg: sc, sc: sc, st: s, wg: map[world.ChunkPos]*[2][256]int16{}}
	tr := &traceRegion{sc: sc, inner: inner, w: w}
	lv := &Level{rg: tr, st: s}
	present := s.presentBiomes(sc)
	r, decoSeed := sc.Decoration()
	origin := Pos{sc.MinBlockX(), sc.Dim().MinY, sc.MinBlockZ()}
	fmt.Fprintf(w, "C %d %d\n", sc.Pos().X, sc.Pos().Z)
	x0, z0 := sc.MinBlockX()-16, sc.MinBlockZ()-16
	minY, maxY := sc.Dim().MinY, sc.Dim().MinY+sc.Dim().Height-1
	before := make([]data.StateID, 0, 48*48*(maxY-minY+1))
	for y := minY; y <= maxY; y++ {
		for x := range 48 {
			for z := range 48 {
				before = append(before, sc.Block(x0+x, y, z0+z))
			}
		}
	}
	for step, l := range s.order {
		if s.StructureStep != nil {
			s.StructureStep(sc, step)
		}
		for i, pf := range l {
			want := false
			for b, ok := range present {
				if ok && s.inStep[b] != nil && s.inStep[b][step][i] {
					want = true
					break
				}
			}
			if !want {
				continue
			}
			fmt.Fprintf(w, "F %s\n", strings.TrimPrefix(pf.Key, ""))
			tr.keep = slices.ContainsFunc(keep, func(k string) bool { return strings.Contains(pf.Key, k) })
			r.SetFeatureSeed(decoSeed, int32(i), int32(step))
			pf.place(lv, r, origin, pf)
		}
	}
	fmt.Fprintln(w, "E")
	i := 0
	for y := minY; y <= maxY; y++ {
		for x := range 48 {
			for z := range 48 {
				if st := sc.Block(x0+x, y, z0+z); st != before[i] {
					fmt.Fprintf(w, "D %d %d %d %s\n", x0+x, y, z0+z, TraceState(st))
				}
				i++
			}
		}
	}
	return nil
}

type traceRegion struct {
	sc    *gen.StageContext
	inner *Level
	w     io.Writer
	keep  bool
}

// TraceState formats a state as the probe does: name, properties sorted.
func TraceState(s data.StateID) string {
	str := data.StateString(s)
	name, props, ok := strings.Cut(str, "[")
	if !ok {
		return str
	}
	ps := strings.Split(strings.TrimSuffix(props, "]"), ",")
	slices.Sort(ps)
	return name + "[" + strings.Join(ps, ",") + "]"
}

func (t *traceRegion) Dim() world.Dimension { return t.sc.Dim() }

func (t *traceRegion) Block(x, y, z int) data.StateID {
	s := t.sc.Block(x, y, z)
	if t.keep {
		fmt.Fprintf(t.w, "B %d %d %d %s\n", x, y, z, TraceState(s))
	}
	return s
}

func (t *traceRegion) SetBlock(x, y, z int, s data.StateID) bool {
	ok := t.sc.SetBlock(x, y, z, s)
	fmt.Fprintf(t.w, "S %d %d %d %s %v\n", x, y, z, TraceState(s), ok)
	return ok
}

func (t *traceRegion) Height(h world.Heightmap, x, z int) int {
	v := t.inner.Height(h, x, z)
	if t.keep {
		fmt.Fprintf(t.w, "H %s %d %d %d\n", h, x, z, v)
	}
	return v
}

func (t *traceRegion) Biome(x, y, z int) (data.BiomeID, bool) { return t.sc.Biome(x, y, z) }

func (t *traceRegion) SetBlockEntity(x, y, z int, id string, d nbt.Compound) bool {
	return t.sc.SetBlockEntity(x, y, z, id, d)
}
