package structure

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

var rotNames = [4]string{"NONE", "CLOCKWISE_90", "CLOCKWISE_180", "COUNTERCLOCKWISE_90"}
var mirrorNames = [3]string{"NONE", "LEFT_RIGHT", "FRONT_BACK"}

// TemplatePiece places one structure template: vanilla
// TemplateStructurePiece. Origin is the world position of the template's
// (0,0,0) before rotation about Pivot.
type TemplatePiece struct {
	PieceType      string
	Template       string
	OX, OY, OZ     int32
	Rot            Rotation
	Mirror         Mirror
	PX, PZ         int32
	Palette        int32
	Processors     *ProcessorList
	ProcessorsName string
	ProcessorsJSON string // inline processor list, persisted verbatim
	IgnoreAir      bool
	Depth          int32

	tmpl *Template
	box  Box
}

// NewTemplatePiece loads the template and computes the box.
func NewTemplatePiece(pieceType, tmpl string, ox, oy, oz int32, rot Rotation, m Mirror, px, pz int32) (*TemplatePiece, error) {
	t, err := LoadTemplate(tmpl)
	if err != nil {
		return nil, err
	}
	p := &TemplatePiece{PieceType: pieceType, Template: qualify(tmpl), OX: ox, OY: oy, OZ: oz, Rot: rot, Mirror: m, PX: px, PZ: pz, tmpl: t}
	p.box = TemplateBox(t, ox, oy, oz, m, rot, px, pz)
	return p, nil
}

func (p *TemplatePiece) Type() string { return p.PieceType }
func (p *TemplatePiece) Box() Box     { return p.box }

// Move shifts the piece by dx,dy,dz.
func (p *TemplatePiece) Move(dx, dy, dz int32) {
	p.OX += dx
	p.OY += dy
	p.OZ += dz
	p.box = TemplateBox(p.tmpl, p.OX, p.OY, p.OZ, p.Mirror, p.Rot, p.PX, p.PZ)
}

func (p *TemplatePiece) Save() nbt.Compound {
	c := nbt.Compound{
		"id": p.PieceType, "GD": p.Depth, "O": int32(-1), "BB": p.box.IntArray(),
		"Template": p.Template, "TPX": p.OX, "TPY": p.OY, "TPZ": p.OZ,
		"Rot": rotNames[p.Rot], "Mirror": mirrorNames[p.Mirror],
		"PX": p.PX, "PZ": p.PZ, "Palette": p.Palette,
	}
	if p.IgnoreAir {
		c["IgnoreAir"] = int8(1)
	}
	if p.ProcessorsName != "" {
		c["processors"] = p.ProcessorsName
	}
	if p.ProcessorsJSON != "" {
		c["processors_json"] = p.ProcessorsJSON
	}
	return c
}

func loadTemplatePiece(c nbt.Compound) (Piece, error) {
	id, _ := c["id"].(string)
	tmpl, _ := c["Template"].(string)
	ox, _ := c["TPX"].(int32)
	oy, _ := c["TPY"].(int32)
	oz, _ := c["TPZ"].(int32)
	px, _ := c["PX"].(int32)
	pz, _ := c["PZ"].(int32)
	var rot Rotation
	var m Mirror
	rs, _ := c["Rot"].(string)
	for i, n := range rotNames {
		if n == rs {
			rot = Rotation(i)
		}
	}
	ms, _ := c["Mirror"].(string)
	for i, n := range mirrorNames {
		if n == ms {
			m = Mirror(i)
		}
	}
	p, err := NewTemplatePiece(id, tmpl, ox, oy, oz, rot, m, px, pz)
	if err != nil {
		return nil, err
	}
	p.Palette, _ = c["Palette"].(int32)
	p.Depth, _ = c["GD"].(int32)
	ia, _ := c["IgnoreAir"].(int8)
	p.IgnoreAir = ia != 0
	if name, ok := c["processors"].(string); ok && name != "" {
		pl, err := LoadProcessorList(name)
		if err != nil {
			return nil, err
		}
		p.Processors, p.ProcessorsName = pl, name
	}
	if js, ok := c["processors_json"].(string); ok && js != "" {
		pk, err := wgdata.Load()
		if err != nil {
			return nil, err
		}
		pl, err := ParseProcessorList(pk, []byte(js))
		if err != nil {
			return nil, err
		}
		p.Processors, p.ProcessorsJSON = pl, js
	}
	return p, nil
}

func (p *TemplatePiece) Place(lv Level, r *rng.WorldgenRandom, clip Box, cx, cz int32) error {
	t := p.tmpl
	if len(t.Palettes) == 0 {
		return nil
	}
	reg := data.Canonical()
	pi := int(p.Palette) % len(t.Palettes)
	if len(t.Palettes) > 1 {
		// Inferred from one reference shipwreck (palette 2 of 8 at
		// 848,51,-560, 102/103 template blocks matching): a legacy random
		// seeded with the origin's position seed.
		pi = int(rng.NewLegacy(rng.GetSeed(p.OX, p.OY, p.OZ)).NextIntn(int32(len(t.Palettes))))
	}
	pal := t.Palettes[pi]
	var markers []marker
	for _, b := range pal {
		x, z := TransformPos(b.X, b.Z, p.Mirror, p.Rot, p.PX, p.PZ)
		wx, wy, wz := p.OX+x, p.OY+b.Y, p.OZ+z
		if !clip.Contains(wx, wy, wz) || wy < lv.MinY() || wy >= lv.MaxY() {
			continue
		}
		bl := reg.BlockOf(b.State)
		if bl == nil {
			continue
		}
		state := b.State
		beNBT := b.NBT
		switch bl.Name {
		case "structure_block":
			if mode, _ := b.NBT["mode"].(string); mode == "DATA" {
				md, _ := b.NBT["metadata"].(string)
				markers = append(markers, marker{wx, wy, wz, md})
			}
			continue
		case "structure_void":
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
			state, beNBT = s, nil
		case "air":
			if p.IgnoreAir {
				continue
			}
		}
		state = TransformState(state, p.Mirror, p.Rot)
		if p.Processors != nil {
			var keep bool
			state, beNBT, keep = p.Processors.Apply(lv, b, state, beNBT, wx, wy, wz, p)
			if !keep {
				continue
			}
		}
		if reg.IsAir(state) && p.IgnoreAir {
			continue
		}
		state = waterlog(reg, lv.Block(wx, wy, wz), state)
		lv.SetBlock(wx, wy, wz, state)
		if beNBT != nil {
			id, _ := beNBT["id"].(string)
			if id != "" {
				d := nbt.Compound{}
				for k, v := range beNBT {
					switch k {
					case "id", "x", "y", "z":
					default:
						d[k] = v
					}
				}
				lv.SetBlockEntity(wx, wy, wz, id, d)
			}
		}
	}
	// Data markers: the chest below a loot marker gets the structure's loot
	// table and a seed from the placement random.
	for _, m := range markers {
		lt, ok := markerLoot[p.PieceType][m.meta]
		if !ok || !clip.Contains(m.x, m.y-1, m.z) {
			continue
		}
		if bl := reg.BlockOf(lv.Block(m.x, m.y-1, m.z)); bl == nil || bl.Name != "chest" {
			continue
		}
		lv.SetBlockEntity(m.x, m.y-1, m.z, "minecraft:chest", nbt.Compound{
			"LootTable": lt, "LootTableSeed": r.NextLong(),
		})
	}
	return nil
}

type marker struct {
	x, y, z int32
	meta    string
}

// markerLoot maps a piece type's DATA marker metadata to a loot table.
var markerLoot = map[string]map[string]string{
	"minecraft:shipwreck": {
		"map_chest":      "minecraft:chests/shipwreck_map",
		"treasure_chest": "minecraft:chests/shipwreck_treasure",
		"supply_chest":   "minecraft:chests/shipwreck_supply",
	},
	"minecraft:igloo": {"chest": "minecraft:chests/igloo_chest"},
	"minecraft:iglu":  {"chest": "minecraft:chests/igloo_chest"},
}

func waterlog(reg *data.Registry, old, s data.StateID) data.StateID {
	ob := reg.BlockOf(old)
	if ob == nil || ob.Name != "water" || old != ob.DefaultState {
		return s
	}
	b := reg.BlockOf(s)
	for i := range b.Props {
		if b.Props[i].Name == "waterlogged" {
			if vi := b.Props[i].ValueIndex("true"); vi >= 0 {
				return b.WithIndex(s, i, vi)
			}
		}
	}
	return s
}

// TemplatesUnder lists the embedded templates under a path prefix, sorted.
func TemplatesUnder(prefix string) []string { return templatesUnder(prefix) }

func templatesUnder(prefix string) []string {
	pk, err := wgdata.Load()
	if err != nil {
		return nil
	}
	dir := "data/minecraft/structure/" + prefix
	var out []string
	for _, p := range pk.Paths() {
		if strings.HasPrefix(p, dir) && strings.HasSuffix(p, ".nbt") {
			out = append(out, "minecraft:"+strings.TrimSuffix(strings.TrimPrefix(p, "data/minecraft/structure/"), ".nbt"))
		}
	}
	sort.Strings(out)
	return out
}

// singleType is a one-template structure (igloo top, shipwreck): random
// rotation, template from a list, placed on a heightmap at the chunk.
// Implemented from the wiki description; draw order and offsets are not
// verified against the reference.
type singleType struct {
	step      int
	piece     string
	templates []string
	heightmap string
	dy        int32
	px, pz    int32
	ignoreAir bool
}

func (t *singleType) Step() int { return t.step }

func (t *singleType) Generate(ctx *GenContext) ([]Piece, error) {
	if len(t.templates) == 0 || ctx.Height == nil {
		return nil, nil
	}
	r := ctx.Random
	rot := Rotation(r.NextIntn(4))
	tmpl := t.templates[0]
	if len(t.templates) > 1 {
		tmpl = t.templates[r.NextIntn(int32(len(t.templates)))]
	}
	// Origin at the chunk's min corner, rotated about the type's pivot
	// (shipwreck pivot 4,15 inferred from reference starts), then set on
	// the heightmap minimum of the box corners.
	x0, z0 := ctx.ChunkX<<4, ctx.ChunkZ<<4
	p, err := NewTemplatePiece(t.piece, tmpl, x0, 0, z0, rot, MirrorNone, t.px, t.pz)
	if err != nil {
		return nil, err
	}
	b := p.Box()
	h := int32(1 << 30)
	for _, c := range [][2]int32{{b.MinX, b.MinZ}, {b.MaxX, b.MinZ}, {b.MinX, b.MaxZ}, {b.MaxX, b.MaxZ}} {
		h = min(h, ctx.Height(c[0], c[1], t.heightmap))
	}
	p.Move(0, h+t.dy, 0)
	cxm, czm := (b.MinX+b.MaxX)/2, (b.MinZ+b.MaxZ)/2
	if ctx.BiomeOK != nil && !ctx.BiomeOK(cxm, h, czm) {
		return nil, nil
	}
	p.IgnoreAir = t.ignoreAir
	return []Piece{p}, nil
}

func init() {
	// Starts saved by older kremenmc builds (top only, piece id igloo).
	RegisterPiece("minecraft:igloo", loadTemplatePiece)
	RegisterPiece("minecraft:shipwreck", loadTemplatePiece)
	RegisterType("minecraft:shipwreck", func(key string, raw json.RawMessage) (Type, error) {
		var j struct {
			Beached bool `json:"is_beached"`
		}
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, err
		}
		// Template order inferred from the reference starts of seed
		// 8266389061079214669 (4/4 consistent): intact variants
		// with_mast, upsidedown, sideways, rightsideup (full, front, back),
		// then the same ten degraded. The beached subset is a guess.
		var ts []string
		for _, deg := range []string{"", "_degraded"} {
			for _, base := range []string{"with_mast", "upsidedown_full", "upsidedown_fronthalf", "upsidedown_backhalf",
				"sideways_full", "sideways_fronthalf", "sideways_backhalf", "rightsideup_full", "rightsideup_fronthalf", "rightsideup_backhalf"} {
				if j.Beached && !strings.HasPrefix(base, "rightsideup") && base != "with_mast" {
					continue
				}
				ts = append(ts, "minecraft:shipwreck/"+base+deg)
			}
		}
		if len(ts) == 0 {
			return nil, fmt.Errorf("no shipwreck templates")
		}
		hm := "OCEAN_FLOOR_WG"
		if j.Beached {
			hm = "WORLD_SURFACE_WG"
		}
		return &singleType{step: 4, piece: "minecraft:shipwreck", templates: ts, heightmap: hm, dy: 1, px: 4, pz: 15, ignoreAir: true}, nil
	})
}
