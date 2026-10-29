package gen

import (
	"fmt"
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// SyntheticStages is a stage set for pipeline tests and benchmarks (not
// vanilla generation), with the same data-flow shape as vanilla: starts
// read by references up to 8 chunks away, biomes read across borders by
// terrain, features that read neighbour blocks and heightmaps and write
// across borders (so their result depends on decoration order), chunk-local
// light and spawn reading neighbour biomes. All randomness is positional.

func mustState(name string) data.StateID {
	s, ok := data.Canonical().DefaultState(name)
	if !ok {
		panic("no block " + name)
	}
	return s
}

func mustBiome(name string) data.BiomeID {
	b, ok := data.Canonical().BiomeByName(name)
	if !ok {
		panic("no biome " + name)
	}
	return b
}

var (
	bStone    = mustState("stone")
	bDirt     = mustState("dirt")
	bGrass    = mustState("grass_block")
	bSand     = mustState("sand")
	bLog      = mustState("oak_log")
	bLeaves   = mustState("oak_leaves")
	bSnow     = mustState("snow_block")
	bOre      = mustState("iron_ore")
	bGlow     = mustState("glowstone")
	bBrick    = mustState("bricks")
	biomeList = []data.BiomeID{mustBiome("plains"), mustBiome("desert"), mustBiome("forest"), mustBiome("snowy_plains")}
	wool      = func() (w [9]data.StateID) {
		for i, c := range []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray", "light_gray"} {
			w[i] = mustState(c + "_wool")
		}
		return
	}()
)

type SyntheticOptions struct {
	// Heavy adds CPU work per terrain column (benchmarks).
	Heavy int
	// Hook runs at the start of every stage (gates, delays, failures).
	Hook func(ctx *StageContext) error
}

// SyntheticStages returns the synthetic stage set.
func SyntheticStages(o SyntheticOptions) []Stage {
	pos := func(ctx *StageContext) rng.Positional { return ctx.Positional(rng.AlgorithmXoroshiro) }
	wrap := func(s Status, f func(*StageContext) error) Stage {
		return StageFunc{S: s, F: func(ctx *StageContext) error {
			if o.Hook != nil {
				if err := o.Hook(ctx); err != nil {
					return err
				}
			}
			return f(ctx)
		}}
	}
	return []Stage{
		wrap(StatusStructureStarts, func(ctx *StageContext) error {
			p := ctx.Pos()
			r := pos(ctx).At(p.X, 1, p.Z)
			if r.NextIntn(6) == 0 {
				ctx.Chunk().Starts["test:tower"] = RawStart(nbt.Compound{
					"r": r.NextIntBetweenInclusive(0, 2),
					"h": r.NextIntBetweenInclusive(4, 20),
				})
			}
			return nil
		}),
		wrap(StatusStructureReferences, func(ctx *StageContext) error {
			p := ctx.Pos()
			for d := int32(0); d <= 8; d++ {
				forRing(p, d, func(n world.ChunkPos) {
					nc, st, ok := ctx.Neighbour(n.X, n.Z)
					if !ok || st < StatusStructureStarts {
						return
					}
					s, ok := nc.Starts["test:tower"]
					if !ok {
						return
					}
					if rad := s.Encode()["r"].(int32); cheb(n, p) <= rad {
						ctx.Chunk().AddRef("test:tower", n)
					}
				})
			}
			return nil
		}),
		wrap(StatusBiomes, func(ctx *StageContext) error {
			c := ctx.Chunk()
			pr := pos(ctx)
			nref := int32(len(c.Refs["test:tower"]))
			for si := range c.Sections {
				for cell := range 64 {
					cx, cy, cz := cell&3, cell>>4, cell>>2&3
					x := ctx.MinBlockX()>>2 + cx
					z := ctx.MinBlockZ()>>2 + cz
					// coarse 8x8-cell patches so biomes vary across chunk borders
					r := pr.At(int32(x>>3), int32(si/8), int32(z>>3))
					b := biomeList[(r.NextIntn(4)+nref)%4]
					c.Sections[si].SetBiome(cx, cy, cz, b)
				}
			}
			return nil
		}),
		wrap(StatusTerrain, func(ctx *StageContext) error {
			c := ctx.Chunk()
			pr := pos(ctx)
			dim := ctx.Dim()
			vals := make([]uint32, 4096)
			heights := [256]int{}
			for lz := range 16 {
				for lx := range 16 {
					x, z := ctx.MinBlockX()+lx, ctx.MinBlockZ()+lz
					h := 64 + int(8*valueNoise(pr, x, z, 24)+4*valueNoise(pr, x, z, 7))
					// read biomes across the border (allowed at ring 1)
					desert := 0
					for _, o := range [][2]int{{-6, 0}, {6, 0}, {0, -6}, {0, 6}} {
						if b, ok := ctx.Biome(x+o[0], 64, z+o[1]); ok && b == biomeList[1] {
							desert++
						}
					}
					h -= desert
					for range o.Heavy {
						h += int(valueNoise(pr, x+h, z, 3)) & 0
					}
					heights[lz<<4|lx] = h
				}
			}
			for si := range c.Sections {
				y0 := dim.MinY + si<<4
				for i := range 4096 {
					lx, ly, lz := i&15, i>>8, i>>4&15
					y := y0 + ly
					h := heights[lz<<4|lx]
					var s data.StateID
					switch {
					case y < h-3:
						s = bStone
					case y < h:
						s = bDirt
					case y == h:
						if c.Biome(lx, y, lz) == biomeList[1] {
							s = bSand
						} else {
							s = bGrass
						}
					default:
						s = data.Air
					}
					vals[i] = uint32(s)
				}
				c.Sections[si].SetAllBlocks(vals)
			}
			c.Finish()
			return nil
		}),
		wrap(StatusFeatures, func(ctx *StageContext) error {
			r, _ := ctx.Decoration()
			bx, bz := ctx.MinBlockX(), ctx.MinBlockZ()
			// trees: read heightmap (raised by earlier neighbour trees) and
			// check for space, write leaves up to 2 blocks across borders
			n := int(r.NextIntBetweenInclusive(1, 4))
			for range n {
				x, z := bx+int(r.NextIntn(16)), bz+int(r.NextIntn(16))
				y := ctx.Height(world.MotionBlocking, x, z)
				if g := ctx.Block(x, y-1, z); g != bGrass && g != bLeaves {
					continue
				}
				if ctx.Block(x, y+1, z) != data.Air {
					continue
				}
				th := 3 + int(r.NextIntn(3))
				for dy := range th {
					ctx.SetBlock(x, y+dy, z, bLog)
				}
				for dx := -2; dx <= 2; dx++ {
					for dz := -2; dz <= 2; dz++ {
						for dy := th - 1; dy <= th; dy++ {
							if ctx.Block(x+dx, y+dy, z+dz) == data.Air && r.NextIntn(5) != 0 {
								ctx.SetBlock(x+dx, y+dy, z+dz, bLeaves)
							}
						}
					}
				}
			}
			// ores replace stone, including the first column of neighbours
			for range 8 {
				x, z := bx-1+int(r.NextIntn(18)), bz-1+int(r.NextIntn(18))
				y := 20 + int(r.NextIntn(30))
				if ctx.Block(x, y, z) == bStone {
					ctx.SetBlock(x, y, z, bOre)
				}
			}
			// snow caps on whatever is on top now
			for range 6 {
				x, z := bx+int(r.NextIntn(16)), bz+int(r.NextIntn(16))
				y := ctx.Height(world.WorldSurface, x, z)
				if b, _ := ctx.Biome(x, y, z); b == biomeList[3] {
					ctx.SetBlock(x, y, z, bSnow)
				}
			}
			// glowstone lamp
			if r.NextIntn(3) == 0 {
				x, z := bx+int(r.NextIntn(16)), bz+int(r.NextIntn(16))
				ctx.SetBlock(x, ctx.Height(world.WorldSurface, x, z), z, bGlow)
			}
			// referenced structures place clipped to this chunk
			c := ctx.Chunk()
			for _, src := range c.Refs["test:tower"] {
				sp := world.PosFromKey(uint64(src))
				sc, _, ok := ctx.Neighbour(sp.X, sp.Z)
				if !ok {
					return fmt.Errorf("start %v out of range", sp)
				}
				h := int(sc.Starts["test:tower"].Encode()["h"].(int32))
				cx, cz := int(sp.X)<<4+8, int(sp.Z)<<4+8
				for y := 100; y < 100+h; y++ {
					for x := cx - 1; x <= cx+1; x++ {
						if x>>4 == int(ctx.Pos().X) && cz>>4 == int(ctx.Pos().Z) {
							ctx.SetBlock(x, y, cz, bBrick)
						}
					}
				}
			}
			return nil
		}),
		wrap(StatusInitializeLight, func(ctx *StageContext) error { return nil }),
		wrap(StatusLight, func(ctx *StageContext) error {
			world.NewLightEngine(ctx.Dim(), nil).LightChunk(ctx.Chunk().Chunk)
			return nil
		}),
		wrap(StatusSpawn, func(ctx *StageContext) error {
			p := ctx.Pos()
			r := pos(ctx).At(p.X, 7, p.Z)
			snowy := 0
			for dx := -1; dx <= 1; dx++ {
				for dz := -1; dz <= 1; dz++ {
					if b, ok := ctx.Biome(ctx.MinBlockX()+16*dx+8, 64, ctx.MinBlockZ()+16*dz+8); ok && b == biomeList[3] {
						snowy++
					}
				}
			}
			if r.NextIntn(3) == 0 || snowy > 4 {
				x, z := ctx.MinBlockX()+int(r.NextIntn(16)), ctx.MinBlockZ()+int(r.NextIntn(16))
				id := "minecraft:sheep"
				if snowy > 4 {
					id = "minecraft:polar_bear"
				}
				ctx.Chunk().Entities = append(ctx.Chunk().Entities, nbt.Compound{
					"id": id, "Pos": nbt.List{Type: nbt.TagDouble, Items: []any{float64(x) + 0.5, float64(ctx.Height(world.MotionBlocking, x, z)), float64(z) + 0.5}},
				})
			}
			return nil
		}),
	}
}

// valueNoise is smooth noise in [0,1) from positional corner randoms.
func valueNoise(pr rng.Positional, x, z, scale int) float64 {
	gx, gz := floorDiv(x, scale), floorDiv(z, scale)
	fx := float64(x-gx*scale) / float64(scale)
	fz := float64(z-gz*scale) / float64(scale)
	at := func(i, k int) float64 { return pr.At(int32(i), int32(scale), int32(k)).NextDouble() }
	sx, sz := fx*fx*(3-2*fx), fz*fz*(3-2*fz)
	a := at(gx, gz) + (at(gx+1, gz)-at(gx, gz))*sx
	b := at(gx, gz+1) + (at(gx+1, gz+1)-at(gx, gz+1))*sx
	return a + (b-a)*sz
}

func floorDiv(a, b int) int { return int(math.Floor(float64(a) / float64(b))) }
