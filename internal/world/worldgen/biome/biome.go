// Package biome implements the reference 26.3 biome sources (multi_noise
// with the overworld and nether presets, the_end, fixed, checkerboard) and
// the per-chunk biome fill of the BIOMES status.
//
// Sources are immutable and safe for concurrent use. The multi-noise
// lookup's per-thread memory (see climate.Cursor) is passed explicitly; a
// chunk fill uses one fresh Cursor, which makes results independent of
// thread scheduling and chunk order.
package biome

import (
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/climate"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/mth"
)

// Source picks the biome of a quart (4x4x4) cell.
type Source interface {
	// Biome returns the biome key at quart coordinates. ctx and cur are
	// per-goroutine (either may be nil).
	Biome(ctx *density.Ctx, cur *climate.Cursor, qx, qy, qz int32) string
}

// MultiNoise is the reference MultiNoiseBiomeSource: the climate target
// point looked up in a parameter list.
type MultiNoise struct {
	Router *density.Router
	List   *climate.List[string]
}

func (m *MultiNoise) Biome(ctx *density.Ctx, cur *climate.Cursor, qx, qy, qz int32) string {
	t := m.Router.ClimateTarget(ctx, qx, qy, qz)
	if cur == nil {
		return m.List.Find(t)
	}
	return m.List.FindWith(t, cur)
}

// TheEnd is the reference TheEndBiomeSource.
type TheEnd struct {
	erosion density.Sampler
}

func (e *TheEnd) Biome(ctx *density.Ctx, _ *climate.Cursor, qx, qy, qz int32) string {
	sx, sz := qx>>2, qz>>2
	if int64(sx)*int64(sx)+int64(sz)*int64(sz) <= 4096 {
		return "minecraft:the_end"
	}
	h := e.erosion.Value(ctx, (sx*2+1)*8, qy<<2, (sz*2+1)*8)
	switch {
	case h > 0.25:
		return "minecraft:end_highlands"
	case h >= -0.0625:
		return "minecraft:end_midlands"
	case h < -0.21875:
		return "minecraft:small_end_islands"
	}
	return "minecraft:end_barrens"
}

// Fixed always returns one biome.
type Fixed string

func (f Fixed) Biome(*density.Ctx, *climate.Cursor, int32, int32, int32) string { return string(f) }

// Checkerboard alternates biomes in squares of 2^Scale chunks.
type Checkerboard struct {
	Biomes []string
	Scale  int32
}

func (c Checkerboard) Biome(_ *density.Ctx, _ *climate.Cursor, qx, _, qz int32) string {
	s := c.Scale + 2
	return c.Biomes[mth.PositiveModulo((qx>>s)+(qz>>s), int32(len(c.Biomes)))]
}

// Preset loads a multi-noise parameter list preset ("overworld", "nether").
func Preset(name string) (*climate.List[string], error) {
	reg, err := density.Vanilla()
	if err != nil {
		return nil, err
	}
	p, ok := reg.(interface{ Report(string) ([]byte, bool) })
	if !ok {
		return nil, fmt.Errorf("biome: registry has no reports")
	}
	b, ok := p.Report("biome_parameters/minecraft/" + name + ".json")
	if !ok {
		return nil, fmt.Errorf("biome: unknown preset %s", name)
	}
	es, err := climate.ParseReport(b)
	if err != nil {
		return nil, err
	}
	return climate.NewList(es), nil
}

// ForDimension builds the vanilla biome source of a dimension
// ("overworld", "the_nether", "the_end") for a world seed.
func ForDimension(dim string, seed int64) (Source, error) {
	switch dim {
	case "overworld", "minecraft:overworld":
		return multiNoise("minecraft:overworld", "overworld", seed)
	case "the_nether", "minecraft:the_nether":
		return multiNoise("minecraft:nether", "nether", seed)
	case "the_end", "minecraft:the_end":
		r, err := density.CachedRouter("minecraft:end", seed)
		if err != nil {
			return nil, err
		}
		return &TheEnd{erosion: r.Fields["erosion"]}, nil
	}
	return nil, fmt.Errorf("biome: unknown dimension %q", dim)
}

func multiNoise(settings, preset string, seed int64) (*MultiNoise, error) {
	r, err := density.CachedRouter(settings, seed)
	if err != nil {
		return nil, err
	}
	l, err := Preset(preset)
	if err != nil {
		return nil, err
	}
	return &MultiNoise{Router: r, List: l}, nil
}

// FillChunk computes the biomes of chunk (cx, cz) for sections minSection
// up to minSection+sections-1, calling set with chunk-local quart
// coordinates (qx, qz in 0..3, qy from 0 at the bottom section). Sections
// are visited bottom-up and quarts x-major, then y, then z, all through one
// fresh Cursor.
func FillChunk(src Source, cx, cz int32, minSection, sections int32, set func(qx, qy, qz int32, b string)) {
	var cur climate.Cursor
	ctx := &density.Ctx{}
	for s := range sections {
		qy0 := (minSection + s) << 2
		for x := range int32(4) {
			for y := range int32(4) {
				for z := range int32(4) {
					set(x, s<<2|y, z, src.Biome(ctx, &cur, cx<<2|x, qy0+y, cz<<2|z))
				}
			}
		}
	}
}
