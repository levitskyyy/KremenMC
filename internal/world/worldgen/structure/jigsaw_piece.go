package structure

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Junction is a reference JigsawJunction: a connection seen from one piece,
// at the other piece's jigsaw column.
type Junction struct {
	X, GroundY, Z int32
	DeltaY        int32
	Rigid         bool // projection of the piece on the other side
}

// JigsawPiece is a reference PoolElementStructurePiece (structure_piece
// "minecraft:jigsaw"): one pool element placed at a position and rotation.
type JigsawPiece struct {
	el        *poolElement
	X, Y, Z   int32
	Rot       Rotation
	GLD       int32 // ground_level_delta
	Junctions []Junction

	box   Box
	parts []*TemplatePiece // single: 1, list: one per element, feature: none
}

func newJigsawPiece(e *poolElement, x, y, z int32, rot Rotation) (*JigsawPiece, error) {
	p := &JigsawPiece{el: e, X: x, Y: y, Z: z, Rot: rot, GLD: e.groundLevelDelta()}
	var add func(e *poolElement) error
	add = func(e *poolElement) error {
		switch e.kind {
		case "single":
			t, err := NewTemplatePiece("minecraft:jigsaw", e.location, x, y, z, rot, MirrorNone, 0, 0)
			if err != nil {
				return err
			}
			t.Processors, t.ProcessorsName, t.ProcessorsJSON = e.processors, e.procName, e.procJSON
			p.parts = append(p.parts, t)
		case "list":
			for _, c := range e.list {
				if err := add(c); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := add(e); err != nil {
		return nil, err
	}
	switch {
	case e.kind == "feature":
		p.box = Box{x, y, z, x, y, z}
	case len(p.parts) == 0:
		return nil, nil
	default:
		// A list element's box covers all its templates (outpost towers:
		// watchtower 21 + watchtower_overgrown 23 tall -> 23).
		p.box = p.parts[0].Box()
		for _, t := range p.parts[1:] {
			p.box = p.box.Encapsulate(t.Box())
		}
	}
	return p, nil
}

// groundLevelDelta is the element's own delta: 1 for template elements, 0
// for features (reference starts).
func (e *poolElement) groundLevelDelta() int32 {
	if e.kind == "feature" || e.kind == "empty" {
		return 0
	}
	return 1
}

func (p *JigsawPiece) primary() *Template {
	if len(p.parts) == 0 {
		return nil
	}
	return p.parts[0].tmpl
}

func (p *JigsawPiece) move(dx, dy, dz int32) {
	p.X, p.Y, p.Z = p.X+dx, p.Y+dy, p.Z+dz
	for _, t := range p.parts {
		t.Move(dx, dy, dz)
	}
	p.box = Box{p.box.MinX + dx, p.box.MinY + dy, p.box.MinZ + dz, p.box.MaxX + dx, p.box.MaxY + dy, p.box.MaxZ + dz}
}

func (p *JigsawPiece) Type() string { return "minecraft:jigsaw" }
func (p *JigsawPiece) Box() Box     { return p.box }

// Rigid reports whether the element has the rigid projection.
func (p *JigsawPiece) Rigid() bool { return p.el.rigid }

// Location is the element's (first) template, or its feature.
func (p *JigsawPiece) Location() string {
	if p.el.kind == "feature" {
		return p.el.feature
	}
	return p.el.primary()
}

func projName(rigid bool) string {
	if rigid {
		return "rigid"
	}
	return "terrain_matching"
}

// Save writes the reference layout (GD is 0 in every reference start).
func (p *JigsawPiece) Save() nbt.Compound {
	js := make([]any, len(p.Junctions))
	for i, j := range p.Junctions {
		js[i] = nbt.Compound{"source_x": j.X, "source_ground_y": j.GroundY, "source_z": j.Z, "delta_y": j.DeltaY, "dest_proj": projName(j.Rigid)}
	}
	var el any
	_ = json.Unmarshal(p.el.raw, &el)
	return nbt.Compound{
		"id": "minecraft:jigsaw", "GD": int32(0), "O": int32(-1), "BB": p.box.IntArray(),
		"PosX": p.X, "PosY": p.Y, "PosZ": p.Z, "rotation": rotNames[p.Rot], "ground_level_delta": p.GLD,
		"junctions": nbt.List{Type: nbt.TagCompound, Items: js}, "pool_element": jsonToNBT(el),
	}
}

func loadJigsawPiece(c nbt.Compound) (Piece, error) {
	pk, err := wgdata.Load()
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(nbtToJSON(c["pool_element"]))
	if err != nil {
		return nil, err
	}
	e, err := parseElement(pk, raw)
	if err != nil {
		return nil, fmt.Errorf("structure: jigsaw piece: %w", err)
	}
	x, _ := c["PosX"].(int32)
	y, _ := c["PosY"].(int32)
	z, _ := c["PosZ"].(int32)
	var rot Rotation
	rs, _ := c["rotation"].(string)
	for i, n := range rotNames {
		if n == rs {
			rot = Rotation(i)
		}
	}
	// Reference: a reloaded piece's box is its element's box again (the
	// terrain_matching raise is lost).
	p, err := newJigsawPiece(e, x, y, z, rot)
	if err != nil || p == nil {
		return nil, fmt.Errorf("structure: jigsaw piece %v: %w", c["pool_element"], err)
	}
	p.GLD, _ = c["ground_level_delta"].(int32)
	for _, it := range compounds(c["junctions"]) {
		j := Junction{}
		j.X, _ = it["source_x"].(int32)
		j.GroundY, _ = it["source_ground_y"].(int32)
		j.Z, _ = it["source_z"].(int32)
		j.DeltaY, _ = it["delta_y"].(int32)
		j.Rigid = it["dest_proj"] != "terrain_matching"
		p.Junctions = append(p.Junctions, j)
	}
	return p, nil
}

func jsonToNBT(v any) any {
	switch t := v.(type) {
	case map[string]any:
		c := nbt.Compound{}
		for k, e := range t {
			c[k] = jsonToNBT(e)
		}
		return c
	case []any:
		l := nbt.List{Type: nbt.TagEnd}
		for _, e := range t {
			n := jsonToNBT(e)
			l.Items = append(l.Items, n)
			switch n.(type) {
			case nbt.Compound:
				l.Type = nbt.TagCompound
			case string:
				l.Type = nbt.TagString
			case int32:
				l.Type = nbt.TagInt
			case float32:
				l.Type = nbt.TagFloat
			case int8:
				l.Type = nbt.TagByte
			case nbt.List:
				l.Type = nbt.TagList
			}
		}
		return l
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1<<31 {
			return int32(t)
		}
		return float32(t)
	case bool:
		if t {
			return int8(1)
		}
		return int8(0)
	}
	return v
}

func nbtToJSON(v any) any {
	switch t := v.(type) {
	case nbt.Compound:
		m := map[string]any{}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			m[k] = nbtToJSON(t[k])
		}
		return m
	case nbt.List:
		l := make([]any, 0, len(t.Items))
		for _, e := range t.Items {
			l = append(l, nbtToJSON(e))
		}
		return l
	}
	return v
}

// Place writes the piece inside clip.
func (p *JigsawPiece) Place(lv Level, r *rng.WorldgenRandom, clip Box, cx, cz int32) error {
	return p.place(lv, r, clip, cx, cz)
}
