package structure

import (
	"encoding/json"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/feature"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// featureLevel adapts a feature level to the piece Level interface for
// template features.
type featureLevel struct{ lv *feature.Level }

func (f featureLevel) MinY() int32 { return int32(f.lv.MinY()) }
func (f featureLevel) MaxY() int32 { return int32(f.lv.MaxY()) }
func (f featureLevel) Block(x, y, z int32) data.StateID {
	return f.lv.Block(feature.Pos{X: int(x), Y: int(y), Z: int(z)})
}
func (f featureLevel) SetBlock(x, y, z int32, s data.StateID) bool {
	return f.lv.SetBlock(feature.Pos{X: int(x), Y: int(y), Z: int(z)}, s)
}
func (f featureLevel) Height(hm string, x, z int32) int32 {
	h := world.WorldSurfaceWG
	for c := world.Heightmap(0); c <= world.MotionBlockingNoLeaves; c++ {
		if c.String() == strings.ToUpper(strings.TrimPrefix(hm, "minecraft:")) {
			h = c
		}
	}
	return int32(f.lv.Height(h, int(x), int(z)))
}
func (f featureLevel) Biome(x, y, z int32) data.BiomeID {
	return f.lv.Biome(feature.Pos{X: int(x), Y: int(y), Z: int(z)})
}
func (f featureLevel) SetBlockEntity(x, y, z int32, id string, d nbt.Compound) {
	f.lv.SetBlockEntity(feature.Pos{X: int(x), Y: int(y), Z: int(z)}, id, d)
}
func (featureLevel) AddEntity(nbt.Compound)        {}
func (featureLevel) MarkPostProcess(_, _, _ int32) {}

// placeTemplateFeature places tmpl centred on p (probe-fitted: the
// template's centre stays on p for every rotation).
func placeTemplateFeature(lv *feature.Level, tmpl string, procs json.RawMessage, p feature.Pos, rot int, r *feature.Random) bool {
	t, err := LoadTemplate(tmpl)
	if err != nil {
		return false
	}
	px, pz := t.SizeX/2, t.SizeZ/2
	ox, oy, oz := int32(p.X)-px, int32(p.Y), int32(p.Z)-pz
	piece, err := NewTemplatePiece("minecraft:template_feature", tmpl, ox, oy, oz, Rotation(rot), MirrorNone, px, pz)
	if err != nil {
		return false
	}
	if len(procs) > 0 && string(procs) != "null" {
		var key string
		if json.Unmarshal(procs, &key) == nil {
			piece.Processors, err = LoadProcessorList(key)
		} else {
			pk, perr := wgdata.Load()
			if perr != nil {
				return false
			}
			piece.Processors, err = ParseProcessorList(pk, procs)
		}
		if err != nil {
			return false
		}
	}
	if n := len(t.Palettes); n > 0 {
		piece.Palette = r.NextIntn(int32(n))
	}
	fl := featureLevel{lv}
	clip := Box{-1 << 30, -1 << 30, -1 << 30, 1 << 30, 1 << 30, 1 << 30}
	return piece.Place(fl, r, clip, 0, 0) == nil
}

func init() {
	feature.PlaceTemplate = placeTemplateFeature
}
