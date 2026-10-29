package structure

import (
	"encoding/json"
	"math"
	"sort"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// FeaturePlacer places a placed feature by registry key (the features
// stage); set on Stages so feature_pool_element pieces can place theirs.
type FeaturePlacer interface {
	PlaceFeature(sc *gen.StageContext, key string, r *rng.WorldgenRandom, x, y, z int32) bool
}

// place writes the piece's element inside clip. Rules inferred from block
// compares of the reference village chunks:
//   - a terrain_matching block goes to WORLD_SURFACE_WG (frozen at the end
//     of terrain) - 1 + its template y in its own column, after the
//     element's processors ran at the unprojected position;
//   - air of legacy elements is not placed;
//   - a jigsaw block becomes its final_state; structure_void is not placed.
func (p *JigsawPiece) place(lv Level, r *rng.WorldgenRandom, clip Box, cx, cz int32) error {
	if p.el.kind == "feature" {
		if sl, ok := lv.(*stageLevel); ok && sl.fp != nil && clip.Contains(p.X, p.Y, p.Z) {
			if debugFeatureHook != nil {
				debugFeatureHook(p, r, lv)
			}
			sl.fp.PlaceFeature(sl.sc, p.el.feature, r, p.X, p.Y, p.Z)
		}
		return nil
	}
	els := []*poolElement{p.el}
	if p.el.kind == "list" {
		els = p.el.list
	}
	for i, t := range p.parts {
		p.placeTemplate(lv, r, t, els[min(i, len(els)-1)], clip)
	}
	return nil
}

func (p *JigsawPiece) placeTemplate(lv Level, r *rng.WorldgenRandom, t *TemplatePiece, el *poolElement, clip Box) {
	tm := t.tmpl
	if len(tm.Palettes) == 0 {
		return
	}
	reg := data.Canonical()
	for _, b := range tm.Palettes[0] {
		x, z := TransformPos(b.X, b.Z, MirrorNone, p.Rot, 0, 0)
		wx, wy, wz := p.X+x, p.Y+b.Y, p.Z+z
		fy := wy
		if !p.el.rigid {
			fy = wgSurface(lv, wx, wz) - 1 + b.Y
		}
		if !clip.Contains(wx, fy, wz) || fy < lv.MinY() || fy >= lv.MaxY() {
			continue
		}
		bl := reg.BlockOf(b.State)
		if bl == nil {
			continue
		}
		state, beNBT := b.State, b.NBT
		switch bl.Name {
		case "structure_block", "structure_void":
			continue
		case "jigsaw":
			fs, _ := b.NBT["final_state"].(string)
			if fs == "" {
				fs = "minecraft:air"
			}
			s, ok := reg.ParseState(fs)
			if !ok {
				continue
			}
			if fb := reg.BlockOf(s); fb != nil && fb.Name == "structure_void" {
				continue
			}
			state, beNBT = s, nil
		}
		if el.legacy && reg.IsAir(state) {
			continue
		}
		state = TransformState(state, MirrorNone, p.Rot)
		if t.Processors != nil {
			var keep bool
			state, beNBT, keep = t.Processors.Apply(lv, b, state, beNBT, wx, wy, wz, t)
			if !keep {
				continue
			}
		}
		// Observed (plains streets): the element's processors see the
		// unprojected position; terrain_matching moves the block after.
		wy = fy
		state = waterlog(reg, lv.Block(wx, wy, wz), state)
		lv.SetBlock(wx, wy, wz, state)
		if beNBT != nil {
			if id, _ := beNBT["id"].(string); id != "" {
				d := nbt.Compound{}
				for k, v := range beNBT {
					switch k {
					case "id", "x", "y", "z":
					default:
						d[k] = v
					}
				}
				// A container with a LootTable gets LootTableSeed = the
				// placement random's next long (reference outpost chest
				// -1383,91,-134: first draw of step 4 index 27).
				if _, ok := d["LootTable"]; ok && r != nil {
					if _, ok := d["LootTableSeed"]; !ok {
						d["LootTableSeed"] = r.NextLong()
					}
				}
				lv.SetBlockEntity(wx, wy, wz, id, d)
			}
		}
	}
	p.placeEntities(lv, t, clip)
}

// placeEntities adds the template's entities (villagers, golems, cats...)
// with rotated positions. NOT verified against the reference.
func (p *JigsawPiece) placeEntities(lv Level, t *TemplatePiece, clip Box) {
	for _, e := range t.tmpl.Entities {
		pos := doubles(e["pos"])
		src, _ := e["nbt"].(nbt.Compound)
		if len(pos) != 3 || src == nil {
			continue
		}
		x, z := transformVec(pos[0], pos[2], p.Rot, 0, 0)
		wx, wy, wz := float64(p.X)+x, float64(p.Y)+pos[1], float64(p.Z)+z
		bx, by, bz := int32(math.Floor(wx)), int32(math.Floor(wy)), int32(math.Floor(wz))
		if !clip.Contains(bx, by, bz) {
			continue
		}
		ent := nbt.Compound{}
		for k, v := range src {
			ent[k] = v
		}
		delete(ent, "UUID")
		ent["Pos"] = nbt.List{Type: nbt.TagDouble, Items: []any{wx, wy, wz}}
		if rl, ok := src["Rotation"].(nbt.List); ok && len(rl.Items) == 2 {
			yaw, _ := rl.Items[0].(float32)
			ent["Rotation"] = nbt.List{Type: nbt.TagFloat, Items: []any{yaw + 90*float32(p.Rot), rl.Items[1]}}
		}
		h := uint64(rng.GetSeed(bx, by, bz))
		id, _ := src["id"].(string)
		for i := 0; i < len(id); i++ {
			h = h*31 + uint64(id[i])
		}
		ent["UUID"] = []int32{int32(h >> 32), int32(h), int32(h>>16) ^ 0x4000, int32(h>>48) ^ -0x80000000}
		lv.AddEntity(ent)
	}
}

// PoolFeatures lists the placed features of every feature_pool_element in
// the data pack's template pools, sorted.
func PoolFeatures() ([]string, error) {
	pk, err := wgdata.Load()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if t["element_type"] == "minecraft:feature_pool_element" {
				if f, ok := t["feature"].(string); ok {
					seen[qualify(f)] = true
				}
			}
			for _, e := range t {
				walk(e)
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	for _, k := range pk.Keys("worldgen/template_pool") {
		b, _ := pk.JSON("worldgen/template_pool", k)
		var v any
		if json.Unmarshal(b, &v) == nil {
			walk(v)
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// wgSurface is WORLD_SURFACE_WG as frozen at the end of the terrain status
// (the features stage's WorldGenRegion semantics; gen.AnvilWGKey).
func wgSurface(lv Level, x, z int32) int32 {
	sl, ok := lv.(*stageLevel)
	if !ok {
		return lv.Height("WORLD_SURFACE_WG", x, z)
	}
	pc, _, ok := sl.sc.Neighbour(x>>4, z>>4)
	if !ok {
		return lv.Height("WORLD_SURFACE_WG", x, z)
	}
	wg, _ := pc.Data[gen.AnvilWGKey].(nbt.Compound)
	l, _ := wg[world.WorldSurfaceWG.String()].([]int64)
	bits := uint(world.HeightmapBits(sl.sc.Dim().Height))
	per := 64 / bits
	i := uint(z&15)<<4 | uint(x&15)
	if uint(len(l))*per < 256 {
		return lv.Height("WORLD_SURFACE_WG", x, z)
	}
	return int32(uint64(l[i/per])>>(i%per*bits)&(1<<bits-1)) + lv.MinY()
}

var debugFeatureHook func(p *JigsawPiece, r *rng.WorldgenRandom, lv Level)
