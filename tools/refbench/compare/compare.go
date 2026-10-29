package compare

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/world/anvil"
)

// Options select what is compared.
type Options struct {
	Blocks     bool
	Biomes     bool
	Heightmaps []string // vanilla type names; empty compares none
	// BlockEntities and Entities compare those lists (by position and id,
	// plus data when Data is set).
	BlockEntities bool
	Entities      bool
	Data          bool
	MaxMismatches int // first N mismatch coordinates kept (default 50)
	TopPairs      int // confusion pairs kept (default 40)
}

// FullOptions compares everything saved for full chunks.
func FullOptions() Options {
	return Options{Blocks: true, Biomes: true, BlockEntities: true, Entities: true, Data: true,
		Heightmaps: []string{"WORLD_SURFACE", "OCEAN_FLOOR", "MOTION_BLOCKING", "MOTION_BLOCKING_NO_LEAVES"}}
}

// StageOptions compares what a proto-chunk holds after a stage.
func StageOptions(stage string) Options {
	if stage == "biomes" {
		return Options{Biomes: true}
	}
	return Options{Blocks: true, Biomes: true, Heightmaps: []string{"WORLD_SURFACE_WG", "OCEAN_FLOOR_WG"}}
}

type Counter struct {
	Total int `json:"total"`
	Match int `json:"match"`
}

func (c *Counter) add(ok bool) {
	c.Total++
	if ok {
		c.Match++
	}
}

func (c Counter) Pct() float64 {
	if c.Total == 0 {
		return 100
	}
	return float64(c.Match) * 100 / float64(c.Total)
}

func (c Counter) Equal() bool { return c.Match == c.Total }

type HeightStat struct {
	Counter
	MaxAbs  int     `json:"maxAbs"`
	MeanAbs float64 `json:"meanAbs"`
	sumAbs  int
}

type Pair struct {
	Ref   string `json:"ref"`
	Got   string `json:"got"`
	Count int    `json:"count"`
}

type Mismatch struct {
	Kind string `json:"kind"` // block, biome, heightmap:<type>, block_entity, entity
	X    int    `json:"x"`
	Y    int    `json:"y"`
	Z    int    `json:"z"`
	Ref  string `json:"ref"`
	Got  string `json:"got"`
}

type SectionReport struct {
	Y      int     `json:"y"`
	Blocks Counter `json:"blocks"`
	Biomes Counter `json:"biomes"`
}

type ChunkReport struct {
	X          int32                  `json:"x"`
	Z          int32                  `json:"z"`
	RefStatus  string                 `json:"refStatus"`
	GotStatus  string                 `json:"gotStatus"`
	Blocks     Counter                `json:"blocks"`
	BlockNames Counter                `json:"blockNames"`
	Solid      Counter                `json:"blocksNotBothAir"`
	Biomes     Counter                `json:"biomes"`
	Heightmaps map[string]*HeightStat `json:"heightmaps,omitempty"`
	BlockEnts  Counter                `json:"blockEntities"`
	Entities   Counter                `json:"entities"`
	Sections   []SectionReport        `json:"sections,omitempty"` // mismatching sections only
}

type Report struct {
	Meta           map[string]string      `json:"meta"`
	Chunks         []ChunkReport          `json:"chunks"`
	MissingRef     []Pos                  `json:"missingRef,omitempty"`
	MissingGot     []Pos                  `json:"missingGot,omitempty"`
	Blocks         Counter                `json:"blocks"`
	BlockNames     Counter                `json:"blockNames"`
	Solid          Counter                `json:"blocksNotBothAir"` // positions where either side is not air
	Biomes         Counter                `json:"biomes"`
	Heightmaps     map[string]*HeightStat `json:"heightmaps,omitempty"`
	BlockEnts      Counter                `json:"blockEntities"`
	Entities       Counter                `json:"entities"`
	Confusion      []Pair                 `json:"confusion"`         // block names, ref -> got
	PropConfusion  []Pair                 `json:"propertyConfusion"` // same block name, different state
	BiomeConfusion []Pair                 `json:"biomeConfusion"`
	First          []Mismatch             `json:"firstMismatches"`
	opts           Options
}

// Equal reports whether every compared item matched and no chunk is missing.
func (r *Report) Equal() bool {
	if len(r.MissingRef)+len(r.MissingGot) > 0 {
		return false
	}
	ok := r.Blocks.Equal() && r.Biomes.Equal() && r.BlockEnts.Equal() && r.Entities.Equal()
	for _, h := range r.Heightmaps {
		ok = ok && h.Equal()
	}
	return ok
}

func blockName(s string) string {
	n, _, _ := strings.Cut(s, "[")
	return n
}

type pairKey struct{ ref, got string }

// Compare diffs got against ref for the chunks in order (all of ref when
// order is nil).
func Compare(ref, got Set, order []Pos, o Options) *Report {
	if o.MaxMismatches <= 0 {
		o.MaxMismatches = 50
	}
	if o.TopPairs <= 0 {
		o.TopPairs = 40
		if n, err := strconv.Atoi(os.Getenv("KREMENMC_COMPARE_PAIRS")); err == nil && n > 0 {
			o.TopPairs = n
		}
	}
	if order == nil {
		order = slices.SortedFunc(maps.Keys(ref), func(a, b Pos) int {
			return cmp.Or(cmp.Compare(a.Z, b.Z), cmp.Compare(a.X, b.X))
		})
	}
	r := &Report{Meta: map[string]string{}, Heightmaps: map[string]*HeightStat{}, opts: o}
	conf, prop, bconf := map[pairKey]int{}, map[pairKey]int{}, map[pairKey]int{}
	miss := func(m Mismatch) {
		if len(r.First) < o.MaxMismatches {
			r.First = append(r.First, m)
		}
	}
	for _, p := range order {
		rc, gc := ref[p], got[p]
		if rc == nil {
			r.MissingRef = append(r.MissingRef, p)
			continue
		}
		if gc == nil {
			r.MissingGot = append(r.MissingGot, p)
			continue
		}
		cr := ChunkReport{X: p.X, Z: p.Z, RefStatus: rc.Status, GotStatus: gc.Status, Heightmaps: map[string]*HeightStat{}}
		bx, bz := int(p.X)*16, int(p.Z)*16
		minSec, maxSec := rc.MinSection, rc.MinSection+len(rc.Sections)-1
		for sy := minSec; sy <= maxSec; sy++ {
			rs, gs := rc.Section(sy), gc.Section(sy)
			sr := SectionReport{Y: sy}
			if o.Blocks {
				for y := range 16 {
					for z := range 16 {
						for x := range 16 {
							a, b := secBlock(rs, x, y, z), secBlock(gs, x, y, z)
							eq := a == b
							sr.Blocks.add(eq)
							cr.Blocks.add(eq)
							if a != "minecraft:air" || b != "minecraft:air" {
								cr.Solid.add(eq)
							}
							if eq {
								cr.BlockNames.add(true)
								continue
							}
							an, bn := blockName(a), blockName(b)
							cr.BlockNames.add(an == bn)
							if an == bn {
								prop[pairKey{a, b}]++
							} else {
								conf[pairKey{an, bn}]++
							}
							miss(Mismatch{Kind: "block", X: bx + x, Y: sy*16 + y, Z: bz + z, Ref: a, Got: b})
						}
					}
				}
			}
			if o.Biomes {
				for qy := range 4 {
					for qz := range 4 {
						for qx := range 4 {
							a, b := secBiome(rs, qx, qy, qz), secBiome(gs, qx, qy, qz)
							eq := a == b
							sr.Biomes.add(eq)
							cr.Biomes.add(eq)
							if !eq {
								bconf[pairKey{a, b}]++
								miss(Mismatch{Kind: "biome", X: bx + qx*4, Y: sy*16 + qy*4, Z: bz + qz*4, Ref: a, Got: b})
							}
						}
					}
				}
			}
			if !sr.Blocks.Equal() || !sr.Biomes.Equal() {
				cr.Sections = append(cr.Sections, sr)
			}
		}
		for _, name := range o.Heightmaps {
			hs := &HeightStat{}
			a, b := rc.Heightmaps[name], gc.Heightmaps[name]
			for i := range 256 {
				var av, gv int32 = math.MinInt32, math.MinInt32
				if len(a) == 256 {
					av = a[i]
				}
				if len(b) == 256 {
					gv = b[i]
				}
				eq := av == gv
				hs.add(eq)
				if !eq {
					d := 1 << 20
					if av != math.MinInt32 && gv != math.MinInt32 {
						d = int(av - gv)
						if d < 0 {
							d = -d
						}
					}
					hs.MaxAbs = max(hs.MaxAbs, d)
					hs.sumAbs += d
					miss(Mismatch{Kind: "heightmap:" + name, X: bx + i&15, Z: bz + i>>4, Ref: fmt.Sprint(av), Got: fmt.Sprint(gv)})
				}
			}
			cr.Heightmaps[name] = hs
			t := r.Heightmaps[name]
			if t == nil {
				t = &HeightStat{}
				r.Heightmaps[name] = t
			}
			t.Total += hs.Total
			t.Match += hs.Match
			t.MaxAbs = max(t.MaxAbs, hs.MaxAbs)
			t.sumAbs += hs.sumAbs
		}
		if o.BlockEntities {
			compareList(&cr.BlockEnts, beKeys(rc.BlockEntities, o.Data), beKeys(gc.BlockEntities, o.Data), "block_entity", miss)
		}
		if o.Entities {
			compareList(&cr.Entities, entKeys(rc.Entities, o.Data), entKeys(gc.Entities, o.Data), "entity", miss)
		}
		r.Blocks.Total += cr.Blocks.Total
		r.Blocks.Match += cr.Blocks.Match
		r.Solid.Total += cr.Solid.Total
		r.Solid.Match += cr.Solid.Match
		r.BlockNames.Total += cr.BlockNames.Total
		r.BlockNames.Match += cr.BlockNames.Match
		r.Biomes.Total += cr.Biomes.Total
		r.Biomes.Match += cr.Biomes.Match
		r.BlockEnts.Total += cr.BlockEnts.Total
		r.BlockEnts.Match += cr.BlockEnts.Match
		r.Entities.Total += cr.Entities.Total
		r.Entities.Match += cr.Entities.Match
		r.Chunks = append(r.Chunks, cr)
	}
	for _, h := range r.Heightmaps {
		if d := h.Total - h.Match; d > 0 {
			h.MeanAbs = float64(h.sumAbs) / float64(d)
		}
	}
	for _, c := range r.Chunks {
		for _, h := range c.Heightmaps {
			if d := h.Total - h.Match; d > 0 {
				h.MeanAbs = float64(h.sumAbs) / float64(d)
			}
		}
	}
	r.Confusion = topPairs(conf, o.TopPairs)
	r.PropConfusion = topPairs(prop, o.TopPairs)
	r.BiomeConfusion = topPairs(bconf, o.TopPairs)
	return r
}

func secBlock(s *anvil.RawSection, x, y, z int) string {
	if s != nil {
		if b := s.Block(x, y, z); b != "" {
			return b
		}
	}
	return "minecraft:air"
}

func secBiome(s *anvil.RawSection, qx, qy, qz int) string {
	if s == nil {
		return ""
	}
	return s.Biome(qx, qy, qz)
}

type item struct {
	key     string // position and id
	data    string // canonical data rendering when compared
	desc    string
	x, y, z int
}

func beKeys(l []BlockEntity, withData bool) []item {
	out := make([]item, len(l))
	for i, b := range l {
		out[i] = item{key: fmt.Sprintf("%d,%d,%d %s", b.X, b.Y, b.Z, b.ID), desc: b.ID, x: b.X, y: b.Y, z: b.Z}
		if withData {
			out[i].data = render(b.Data)
		}
	}
	return out
}

func entKeys(l []Entity, withData bool) []item {
	out := make([]item, len(l))
	for i, e := range l {
		out[i] = item{key: fmt.Sprintf("%.4f,%.4f,%.4f %s", e.Pos[0], e.Pos[1], e.Pos[2], e.ID), desc: e.ID,
			x: int(math.Floor(e.Pos[0])), y: int(math.Floor(e.Pos[1])), z: int(math.Floor(e.Pos[2]))}
		if withData {
			out[i].data = render(e.Data)
		}
	}
	return out
}

// compareList matches items by key (multiset); Total counts the union.
func compareList(c *Counter, ref, got []item, kind string, miss func(Mismatch)) {
	left := map[string][]item{}
	for _, g := range got {
		left[g.key] = append(left[g.key], g)
	}
	for _, a := range ref {
		gs := left[a.key]
		if len(gs) == 0 {
			c.add(false)
			miss(Mismatch{Kind: kind, X: a.x, Y: a.y, Z: a.z, Ref: a.desc, Got: "(none)"})
			continue
		}
		b := gs[0]
		left[a.key] = gs[1:]
		ok := a.data == b.data
		c.add(ok)
		if !ok {
			miss(Mismatch{Kind: kind + "_data", X: a.x, Y: a.y, Z: a.z, Ref: a.data, Got: b.data})
		}
	}
	for _, gs := range left {
		for _, b := range gs {
			c.add(false)
			miss(Mismatch{Kind: kind, X: b.x, Y: b.y, Z: b.z, Ref: "(none)", Got: b.desc})
		}
	}
}

// render prints NBT deterministically (fmt sorts map keys).
func render(v any) string { return fmt.Sprintf("%v", v) }

func topPairs(m map[pairKey]int, n int) []Pair {
	out := make([]Pair, 0, len(m))
	for k, c := range m {
		out = append(out, Pair{Ref: k.ref, Got: k.got, Count: c})
	}
	slices.SortFunc(out, func(a, b Pair) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Ref, b.Ref), cmp.Compare(a.Got, b.Got))
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// WriteJSON writes the report as indented JSON.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Print writes a human summary.
func (r *Report) Print(w io.Writer) {
	keys := slices.Sorted(maps.Keys(r.Meta))
	var meta []string
	for _, k := range keys {
		meta = append(meta, k+"="+r.Meta[k])
	}
	fmt.Fprintf(w, "compare %s\n", strings.Join(meta, " "))
	fmt.Fprintf(w, "chunks: %d compared", len(r.Chunks))
	if len(r.MissingRef) > 0 {
		fmt.Fprintf(w, ", %d missing in reference", len(r.MissingRef))
	}
	if len(r.MissingGot) > 0 {
		fmt.Fprintf(w, ", %d missing in kremenmc output", len(r.MissingGot))
	}
	fmt.Fprintln(w)
	line := func(name string, c Counter) {
		if c.Total > 0 {
			fmt.Fprintf(w, "  %-28s %7.3f%%  (%d / %d)\n", name, c.Pct(), c.Match, c.Total)
		}
	}
	line("blocks (full state)", r.Blocks)
	line("blocks (name only)", r.BlockNames)
	line("blocks (not both air)", r.Solid)
	line("biomes (quart cells)", r.Biomes)
	for _, k := range slices.Sorted(maps.Keys(r.Heightmaps)) {
		h := r.Heightmaps[k]
		line("heightmap "+k, h.Counter)
		if !h.Equal() {
			fmt.Fprintf(w, "  %-28s max |d| %d, mean |d| %.1f\n", "", h.MaxAbs, h.MeanAbs)
		}
	}
	if r.opts.BlockEntities {
		line("block entities", r.BlockEnts)
	}
	if r.opts.Entities {
		line("entities", r.Entities)
	}
	pairs := func(title string, ps []Pair, n int) {
		if len(ps) == 0 {
			return
		}
		fmt.Fprintf(w, "%s:\n", title)
		for _, p := range ps[:min(n, len(ps))] {
			fmt.Fprintf(w, "  %9d  %s -> %s\n", p.Count, p.Ref, p.Got)
		}
	}
	pairs("block confusion (reference -> compared)", r.Confusion, 20)
	pairs("property-only confusion", r.PropConfusion, 10)
	pairs("biome confusion", r.BiomeConfusion, 15)
	var worst []ChunkReport
	for _, c := range r.Chunks {
		if !c.Blocks.Equal() || !c.Biomes.Equal() {
			worst = append(worst, c)
		}
	}
	if len(worst) > 0 {
		slices.SortFunc(worst, func(a, b ChunkReport) int {
			return cmp.Compare(a.Blocks.Pct()+a.Biomes.Pct(), b.Blocks.Pct()+b.Biomes.Pct())
		})
		fmt.Fprintf(w, "per chunk (worst %d of %d mismatching):\n", min(10, len(worst)), len(worst))
		for _, c := range worst[:min(10, len(worst))] {
			fmt.Fprintf(w, "  chunk %d,%d  blocks %.2f%%  biomes %.2f%%  status %s/%s", c.X, c.Z, c.Blocks.Pct(), c.Biomes.Pct(), c.RefStatus, c.GotStatus)
			var bad []string
			for _, s := range c.Sections {
				bad = append(bad, fmt.Sprintf("%d:%.0f%%", s.Y, s.Blocks.Pct()))
			}
			if len(bad) > 8 {
				bad = append(bad[:8], "...")
			}
			fmt.Fprintf(w, "  sections %s\n", strings.Join(bad, " "))
		}
	}
	if len(r.First) > 0 {
		fmt.Fprintf(w, "first %d mismatches:\n", min(20, len(r.First)))
		for _, m := range r.First[:min(20, len(r.First))] {
			fmt.Fprintf(w, "  %-22s %d %d %d  %s -> %s\n", m.Kind, m.X, m.Y, m.Z, m.Ref, m.Got)
		}
	}
}
