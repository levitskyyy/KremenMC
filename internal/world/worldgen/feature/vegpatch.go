package feature

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// vegetationPatch is vegetation_patch (moss, pale moss) and, with water set,
// waterlogged_vegetation_patch (clay pools). Inferred from
// kremenmc.probe.feat_cave.Draws draw logs and cave_place goldens.
type vegetationPatch struct {
	water       bool
	replaceable blockSet
	ground      StateProvider
	veg         *Placed
	ceiling     bool
	depth       IntProvider
	extraBottom float32
	vertical    int
	vegChance   float32
	xz          IntProvider
	edgeChance  float32
}

func parseVegPatch(water bool) func(rg *registry, b json.RawMessage) (Feature, error) {
	return func(rg *registry, b json.RawMessage) (Feature, error) {
		var c struct {
			Depth       json.RawMessage `json:"depth"`
			ExtraBottom float32         `json:"extra_bottom_block_chance"`
			EdgeChance  float32         `json:"extra_edge_column_chance"`
			Ground      json.RawMessage `json:"ground_state"`
			Replaceable string          `json:"replaceable"`
			Surface     string          `json:"surface"`
			VegChance   float32         `json:"vegetation_chance"`
			Veg         json.RawMessage `json:"vegetation_feature"`
			Vertical    int             `json:"vertical_range"`
			XZ          json.RawMessage `json:"xz_radius"`
		}
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("vegetation_patch: %w", err)
		}
		f := &vegetationPatch{water: water, extraBottom: c.ExtraBottom, edgeChance: c.EdgeChance,
			ceiling: c.Surface == "ceiling", vegChance: c.VegChance, vertical: c.Vertical}
		var err error
		if f.replaceable, err = rg.tag(c.Replaceable); err != nil {
			return nil, fmt.Errorf("vegetation_patch: %w", err)
		}
		if f.ground, err = rg.parseStateProvider(c.Ground); err != nil {
			return nil, fmt.Errorf("vegetation_patch ground: %w", err)
		}
		if f.veg, err = rg.parsePlaced(c.Veg); err != nil {
			return nil, fmt.Errorf("vegetation_patch vegetation: %w", err)
		}
		if f.depth, err = parseInt(c.Depth); err != nil {
			return nil, fmt.Errorf("vegetation_patch depth: %w", err)
		}
		if f.xz, err = parseInt(c.XZ); err != nil {
			return nil, fmt.Errorf("vegetation_patch xz_radius: %w", err)
		}
		return f, nil
	}
}

func (f *vegetationPatch) Place(c *placeCtx, r *Random, p Pos) bool {
	rx := f.xz.Sample(r) + 1
	rz := f.xz.Sample(r) + 1
	ground := f.placeGround(c.lv, r, p, rx, rz)
	if f.water {
		ground = f.flood(c.lv, ground)
	}
	f.distribute(c, r, ground)
	return len(ground) > 0
}

// flood turns the ground positions enclosed by solid blocks on the four
// sides and below into water sources and keeps only those.
func (f *vegetationPatch) flood(lv *Level, ground []Pos) []Pos {
	var wet []Pos
	for _, g := range ground {
		if f.enclosed(lv, g) {
			wet = append(wet, g)
		}
	}
	water, _ := data.DefaultState("water")
	for _, g := range wet {
		lv.SetBlock(g, water)
	}
	return wet
}

func (f *vegetationPatch) enclosed(lv *Level, g Pos) bool {
	for _, d := range [...]Pos{{0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}, {0, -1, 0}} {
		if !isSolid(lv.Block(g.Add(d))) {
			return false
		}
	}
	return true
}

func (f *vegetationPatch) placeGround(lv *Level, r *Random, p Pos, rx, rz int) []Pos {
	in, out := Pos{0, -1, 0}, Pos{0, 1, 0}
	if f.ceiling {
		in, out = out, in
	}
	var ground []Pos
	for x := -rx; x <= rx; x++ {
		xe := x == -rx || x == rx
		for z := -rz; z <= rz; z++ {
			ze := z == -rz || z == rz
			if xe && ze {
				continue
			}
			if (xe || ze) && r.NextFloat() >= f.edgeChance {
				continue
			}
			q := p.Add(Pos{x, 0, z})
			for i := 0; i < f.vertical && data.IsAir(lv.Block(q)); i++ {
				q = q.Add(in)
			}
			for i := 0; i < f.vertical && !data.IsAir(lv.Block(q)); i++ {
				q = q.Add(out)
			}
			g := q.Add(in)
			if !data.IsAir(lv.Block(q)) || !f.replaceable.has(lv.Block(g)) {
				continue
			}
			if f.placeColumn(lv, r, g, in) {
				ground = append(ground, g)
			}
		}
	}
	return ground
}

// placeColumn replaces up to depth blocks from g inward with the ground
// state, plus one extra with extra_bottom_block_chance.
func (f *vegetationPatch) placeColumn(lv *Level, r *Random, g, in Pos) bool {
	depth := f.depth.Sample(r)
	q := g
	placed := false
	for i := 0; i < depth; i++ {
		cur := lv.Block(q)
		if !f.replaceable.has(cur) {
			break
		}
		placed = true
		st := f.ground.State(lv, r, q)
		if cur == st {
			break
		}
		lv.SetBlock(q, st)
		q = q.Add(in)
	}
	if placed && f.extraBottom > 0 && r.NextFloat() < f.extraBottom && f.replaceable.has(lv.Block(q)) {
		lv.SetBlock(q, f.ground.State(lv, r, q))
	}
	return placed
}

func (f *vegetationPatch) distribute(c *placeCtx, r *Random, ground []Pos) {
	out := Pos{0, 1, 0}
	if f.ceiling {
		out = Pos{0, -1, 0}
	}
	for _, g := range vpHashOrder(ground) {
		if f.vegChance > 0 && r.NextFloat() < f.vegChance {
			if !f.water {
				f.veg.place(c.lv, r, g.Add(out), c.top)
				continue
			}
			if f.veg.place(c.lv, r, g, c.top) {
				if st, ok := data.Canonical().With(c.lv.Block(g), "waterlogged", "true"); ok {
					c.lv.SetBlock(g, st)
				}
			}
		}
	}
}

// vpHashOrder is a Java HashSet<BlockPos> iteration order: hash
// (y + 31z)*31 + x spread by h ^ h>>>16, buckets ascending with insertion
// order inside a bucket, the table doubling from 16 at 3/4 load.
func vpHashOrder(ps []Pos) []Pos {
	n := 16
	for len(ps) > n*3/4 {
		n *= 2
	}
	bucket := func(p Pos) int {
		h := (int32(p.Y)+int32(p.Z)*31)*31 + int32(p.X)
		h ^= int32(uint32(h) >> 16)
		return int(h) & (n - 1)
	}
	out := slices.Clone(ps)
	slices.SortStableFunc(out, func(a, b Pos) int { return cmp.Compare(bucket(a), bucket(b)) })
	return out
}

func init() {
	featureTypes["vegetation_patch"] = parseVegPatch(false)
	featureTypes["waterlogged_vegetation_patch"] = parseVegPatch(true)
}
