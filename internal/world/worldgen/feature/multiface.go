package feature

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// multifaceGrowth is the multiface_growth feature (glow lichen, sculk vein).
// Behaviour inferred from kremenmc.probe.feat_cave.Draws draw logs and
// cave_place goldens: the origin is tried with the shuffled valid faces, then
// the neighbour in each shuffled direction, trying the faces except the one
// facing back. A placed face spreads once with chance_of_spreading. Faces
// attach to full-cube or sturdy neighbours (leaves count); can_be_placed_on
// applies to the first face only. glow_lichen omits chance_of_spreading; the
// 0.5 default matches its probe cases.
type multifaceGrowth struct {
	block     data.StateID
	name      string
	on        blockSet
	valid     []int
	searchMax int
	chance    float32
}

// Direction indices follow dirOrder (down, up, north, south, west, east).
var mfDirOff = [6]Pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

func mfOpposite(d int) int { return d ^ 1 }

// shuffleFaces is a Fisher-Yates shuffle from the last element, matching the
// reference's nextInt(n), nextInt(n-1), ..., nextInt(2) draw sequence.
func shuffleFaces(r *Random, l []int) {
	for i := len(l); i > 1; i-- {
		j := int(r.NextIntn(int32(i)))
		l[i-1], l[j] = l[j], l[i-1]
	}
}

func parseMultiface(rg *registry, b json.RawMessage) (Feature, error) {
	var c struct {
		Block   string          `json:"block"`
		On      json.RawMessage `json:"can_be_placed_on"`
		Ceiling bool            `json:"can_place_on_ceiling"`
		Floor   bool            `json:"can_place_on_floor"`
		Wall    bool            `json:"can_place_on_wall"`
		Chance  *float32        `json:"chance_of_spreading"`
		Range   *int            `json:"search_range"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("multiface_growth: %w", err)
	}
	f := &multifaceGrowth{name: mfTrimNS(c.Block), searchMax: 10, chance: 0.5}
	st, ok := data.DefaultState(f.name)
	if !ok {
		return nil, fmt.Errorf("multiface_growth: unknown block %s", c.Block)
	}
	f.block = st
	if c.Chance != nil {
		f.chance = *c.Chance
	}
	if c.Range != nil {
		f.searchMax = *c.Range
	}
	var names []string
	var one string
	if json.Unmarshal(c.On, &one) == nil {
		names = []string{one}
	} else if err := json.Unmarshal(c.On, &names); err != nil {
		return nil, fmt.Errorf("multiface_growth: can_be_placed_on: %w", err)
	}
	on, err := rg.blocks(names)
	if err != nil {
		return nil, fmt.Errorf("multiface_growth: %w", err)
	}
	f.on = on
	if c.Ceiling {
		f.valid = append(f.valid, 1)
	}
	if c.Floor {
		f.valid = append(f.valid, 0)
	}
	if c.Wall {
		f.valid = append(f.valid, 2, 5, 3, 4)
	}
	return f, nil
}

func mfTrimNS(s string) string {
	if len(s) > 10 && s[:10] == "minecraft:" {
		return s[10:]
	}
	return s
}

func mfWaterSource(st data.StateID) bool { return fluidOf(st) == "water" }

// mfWaterBlock: a water source block itself, not a waterlogged block.
func mfWaterBlock(st data.StateID) bool { return fluidOf(st) == "water" && blockName(st) == "water" }

// veinCanReplace: the blocks a sculk vein spreads into, besides veins.
// Probe kremenmc.probe.feat_ow.Place, scenes cavezoo/cavezoo2 (first
// writes in 30 cases): snow layers (1, 3, 8), short grass, glow lichen,
// ferns, bushes, seagrass, vines, leaf litter, roots, light and resin
// clumps are replaced; fire, flowing water, lava, stairs, slabs, glass,
// leaves, fences, torches, poppies, carpets, cobwebs, trapdoors and moss
// carpets are not. Fitted as air, or #replaceable but fire holding no
// fluid or a water source. Soul fire (the probe's burnt out on deepslate)
// is taken as fire: the reference ancient city keeps its soul fire next to
// sculk, as it keeps a waterlogged stair (-512,-38,1267).
func veinCanReplace(st data.StateID) bool {
	if data.IsAir(st) {
		return true
	}
	if n := blockName(st); n == "fire" || n == "soul_fire" || !replaceableBlocks().has(st) {
		return false
	}
	f := fluidOf(st)
	return f == "" || f == "water"
}

func (f *multifaceGrowth) Place(c *placeCtx, r *Random, p Pos) bool {
	// The feature itself only grows into air or water (a snow layer origin
	// fails for sculk_vein, cavemix probe); its spreading uses canReplace.
	cur := c.lv.Block(p)
	if !data.IsAir(cur) && !mfWaterBlock(cur) {
		return false
	}
	dirs := append([]int(nil), f.valid...)
	shuffleFaces(r, dirs)
	if f.placeAt(c.lv, r, p, cur, dirs) {
		return true
	}
	for _, d := range dirs {
		ex := make([]int, 0, len(f.valid))
		for _, v := range f.valid {
			if v != mfOpposite(d) {
				ex = append(ex, v)
			}
		}
		shuffleFaces(r, ex)
		q := p
		// The reference only ever tried the first block of each direction
		// (24/24 probe cases, including walks that would have found a face
		// further on), whatever search_range says.
		for range min(f.searchMax, 1) {
			q = q.Add(mfDirOff[d])
			s := c.lv.Block(q)
			if !data.IsAir(s) && !mfWaterBlock(s) && blockName(s) != f.name {
				break
			}
			if f.placeAt(c.lv, r, q, s, ex) {
				return true
			}
		}
	}
	return false
}

func (f *multifaceGrowth) placeAt(lv *Level, r *Random, p Pos, cur data.StateID, dirs []int) bool {
	for _, d := range dirs {
		if !f.on.has(lv.Block(p.Add(mfDirOff[d]))) {
			continue
		}
		st, ok := f.withFace(lv, p, cur, d)
		if !ok {
			continue
		}
		lv.SetBlock(p, st)
		if r.NextFloat() < f.chance {
			f.spread(lv, r, p, st, d)
		}
		return true
	}
	return false
}

// withFace returns cur with face d added, if the face can attach there.
func (f *multifaceGrowth) withFace(lv *Level, p Pos, cur data.StateID, d int) (data.StateID, bool) {
	reg := data.Canonical()
	base := cur
	if blockName(cur) != f.name {
		if !f.canReplace(cur) {
			return 0, false
		}
		base = f.block
		if mfWaterSource(cur) {
			base, _ = reg.With(base, "waterlogged", "true")
		}
	} else if v, _ := reg.Value(cur, dirOrder[d]); v == "true" {
		return 0, false
	}
	if n := lv.Block(p.Add(mfDirOff[d])); data.Flags(n)&data.FlagFullCube == 0 && !faceSturdy(n, mfOpposite(d)) {
		return 0, false
	}
	st, ok := reg.With(base, dirOrder[d], "true")
	return st, ok
}

// spread grows one face from (p, face) toward the first shuffled direction
// that accepts it: the same block, the neighbour in the same plane, or
// around the supporting block's edge.
// canReplace: the blocks f places into besides its own block.
func (f *multifaceGrowth) canReplace(st data.StateID) bool {
	if f.name == "sculk_vein" {
		return veinCanReplace(st)
	}
	return data.IsAir(st) || mfWaterSource(st)
}

func (f *multifaceGrowth) spread(lv *Level, r *Random, p Pos, st data.StateID, face int) {
	all := []int{0, 1, 2, 3, 4, 5}
	shuffleFaces(r, all)
	for _, d := range all {
		if d>>1 == face>>1 {
			continue
		}
		cands := [3]struct {
			pos  Pos
			face int
		}{{p, d}, {p.Add(mfDirOff[d]), face}, {p.Add(mfDirOff[d]).Add(mfDirOff[face]), mfOpposite(d)}}
		for _, t := range cands {
			s := lv.Block(t.pos)
			if ns, ok := f.withFace(lv, t.pos, s, t.face); ok {
				lv.SetBlock(t.pos, ns)
				return
			}
		}
	}
}

func init() {
	featureTypes["multiface_growth"] = parseMultiface
}
