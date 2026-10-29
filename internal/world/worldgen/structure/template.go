package structure

import (
	"fmt"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// TemplateBlock is one block of a structure template, relative to the
// template origin.
type TemplateBlock struct {
	X, Y, Z int32
	State   data.StateID
	NBT     nbt.Compound // block entity data without x/y/z, or nil
}

// Template is a decoded structure NBT file (data/<ns>/structure/*.nbt).
type Template struct {
	SizeX, SizeY, SizeZ int32
	// Palettes holds every palette's blocks (most templates have one;
	// shipwrecks and others pick one at random).
	Palettes [][]TemplateBlock
	Entities []nbt.Compound
}

var (
	tmplMu    sync.Mutex
	tmplCache = map[string]*Template{}
)

// LoadTemplate returns the embedded template key ("minecraft:igloo/top").
// Templates are immutable and shared.
func LoadTemplate(key string) (*Template, error) {
	key = qualify(key)
	tmplMu.Lock()
	t, ok := tmplCache[key]
	tmplMu.Unlock()
	if ok {
		return t, nil
	}
	pk, err := wgdata.Load()
	if err != nil {
		return nil, fmt.Errorf("structure: %w", err)
	}
	b, ok := pk.Template(key)
	if !ok {
		return nil, fmt.Errorf("structure: template %s missing", key)
	}
	_, root, _, err := nbt.Unmarshal(b, false)
	if err != nil {
		return nil, fmt.Errorf("structure: template %s: %w", key, err)
	}
	t, err = decodeTemplate(root)
	if err != nil {
		return nil, fmt.Errorf("structure: template %s: %w", key, err)
	}
	tmplMu.Lock()
	tmplCache[key] = t
	tmplMu.Unlock()
	return t, nil
}

func qualify(k string) string {
	for i := 0; i < len(k); i++ {
		if k[i] == ':' {
			return k
		}
	}
	return "minecraft:" + k
}

func intList(v any) []int32 {
	switch l := v.(type) {
	case nbt.List:
		out := make([]int32, 0, len(l.Items))
		for _, e := range l.Items {
			if n, ok := e.(int32); ok {
				out = append(out, n)
			}
		}
		return out
	case []int32:
		return l
	}
	return nil
}

func compounds(v any) []nbt.Compound {
	l, ok := v.(nbt.List)
	if !ok {
		return nil
	}
	out := make([]nbt.Compound, 0, len(l.Items))
	for _, e := range l.Items {
		if c, ok := e.(nbt.Compound); ok {
			out = append(out, c)
		}
	}
	return out
}

func decodePalette(entries []nbt.Compound) []data.StateID {
	reg := data.Canonical()
	out := make([]data.StateID, len(entries))
	for i, e := range entries {
		// 26.3 templates use id/properties; older files Name/Properties.
		name, _ := e["id"].(string)
		if name == "" {
			name, _ = e["Name"].(string)
		}
		props, _ := e["properties"].(nbt.Compound)
		if props == nil {
			props, _ = e["Properties"].(nbt.Compound)
		}
		s, ok := reg.StateFrom(trimNS(name), func(k string) (string, bool) {
			v, ok := props[k].(string)
			return v, ok
		})
		if !ok {
			s, _ = reg.DefaultState("air")
		}
		out[i] = s
	}
	return out
}

func decodeTemplate(root nbt.Compound) (*Template, error) {
	size := intList(root["size"])
	if len(size) != 3 {
		return nil, fmt.Errorf("bad size")
	}
	t := &Template{SizeX: size[0], SizeY: size[1], SizeZ: size[2]}
	var pals [][]data.StateID
	if p, ok := root["palette"]; ok {
		pals = append(pals, decodePalette(compounds(p)))
	} else if ps, ok := root["palettes"].(nbt.List); ok {
		for _, e := range ps.Items {
			l, _ := e.(nbt.List)
			pals = append(pals, decodePalette(compounds(l)))
		}
	}
	blocks := compounds(root["blocks"])
	for _, pal := range pals {
		out := make([]TemplateBlock, 0, len(blocks))
		for _, b := range blocks {
			pos := intList(b["pos"])
			si, _ := b["state"].(int32)
			if len(pos) != 3 || int(si) >= len(pal) {
				continue
			}
			tb := TemplateBlock{X: pos[0], Y: pos[1], Z: pos[2], State: pal[si]}
			if c, ok := b["nbt"].(nbt.Compound); ok {
				tb.NBT = c
			}
			out = append(out, tb)
		}
		t.Palettes = append(t.Palettes, out)
	}
	t.Entities = compounds(root["entities"])
	return t, nil
}

// TransformPos maps a template-relative position through mirror then
// rotation about pivot (template-relative), as StructurePlaceSettings does.
func TransformPos(x, z int32, m Mirror, rot Rotation, px, pz int32) (int32, int32) {
	switch m {
	case MirrorLeftRight:
		z = -z
	case MirrorFrontBack:
		x = -x
	}
	switch rot {
	case Rot90:
		return px + pz - z, pz - px + x
	case Rot180:
		return px + px - x, pz + pz - z
	case Rot270:
		return px - pz + z, px + pz - x
	}
	return x, z
}

// TemplateBox is the world box of t placed at origin with m, rot, pivot.
func TemplateBox(t *Template, ox, oy, oz int32, m Mirror, rot Rotation, px, pz int32) Box {
	x0, z0 := TransformPos(0, 0, m, rot, px, pz)
	x1, z1 := TransformPos(t.SizeX-1, t.SizeZ-1, m, rot, px, pz)
	return Box{ox + min(x0, x1), oy, oz + min(z0, z1), ox + max(x0, x1), oy + t.SizeY - 1, oz + max(z0, z1)}
}
