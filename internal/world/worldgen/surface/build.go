package surface

import (
	"fmt"
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// Build applies the material pass to chunk c (blocks as left by the noise
// fill): per column, x-major then z, the eroded badlands extension, the
// material rule for every default-block position from the top down, then
// the frozen ocean extension. Biome lookups go through b, which must cover
// the chunk and its 8 neighbours. It is the reference
// MaterialSystem.buildSurface.
func (s *System) Build(ctx *Ctx, c *world.Chunk, b Biomes) error {
	return s.build(ctx, c, b, s.rule)
}

// Extensions runs Build without the material rule: only the hardcoded
// extensions, which is what the reference produces for an empty rule.
func (s *System) Extensions(ctx *Ctx, c *world.Chunk, b Biomes) error {
	return s.build(ctx, c, b, nil)
}

func (s *System) build(ctx *Ctx, c *world.Chunk, b Biomes, rule ruleEval) error {
	// The chunk is the dimension's height (nether and end chunks are 256
	// tall over 128-block noise settings); the pass covers the whole chunk
	// like the reference column, anchors resolve against the generation
	// range, which is the settings range for every vanilla dimension.
	if int32(c.Dim.MinY) > s.MinY || int32(c.Dim.MinY+c.Dim.Height) < s.MinY+s.Height {
		return fmt.Errorf("surface: chunk height %d+%d does not cover %s (%d+%d)", c.Dim.MinY, c.Dim.Height, s.Settings, s.MinY, s.Height)
	}
	ctx.minY, ctx.height = int32(c.Dim.MinY), int32(c.Dim.Height)
	ctx.reset(s, b)
	n := len(c.Sections)
	if cap(ctx.buf) < n*4096 {
		ctx.buf = make([]uint32, n*4096)
		ctx.sectionChanged = make([]bool, n)
	}
	buf := ctx.buf[:n*4096]
	changed := ctx.sectionChanged[:n]
	for i := range c.Sections {
		c.Sections[i].Blocks.Values(buf[i*4096 : (i+1)*4096])
		changed[i] = false
	}
	s.buildBuffer(ctx, c.X, c.Z, buf, changed, rule)
	any := false
	c.Lock()
	for i := range c.Sections {
		if changed[i] {
			c.Sections[i].SetAllBlocks(buf[i*4096 : (i+1)*4096])
			any = true
		}
	}
	c.Unlock()
	if any {
		c.RecalcHeightmaps()
		c.MarkDirty()
	}
	return nil
}

func (s *System) isStone(st data.StateID) bool {
	return !s.reg.IsAir(st) && !s.reg.HasFluid(st)
}

// buildBuffer runs the pass over blocks in (ry<<8|z<<4|x) order.
func (s *System) buildBuffer(ctx *Ctx, cx, cz int32, buf []uint32, changed []bool, rule ruleEval) {
	h := ctx.height
	at := func(x, ry, z int32) data.StateID { return data.StateID(buf[ry<<8|z<<4|x]) }
	for col := range int32(256) {
		x, z := col&15, col>>4
		top := int32(0)
		for ry := h - 1; ry >= 0; ry-- {
			if !s.reg.IsAir(at(x, ry, z)) {
				top = ry + 1
				break
			}
		}
		ctx.heights[col] = top
	}
	height := func(x, z int32) int32 { return ctx.heights[max(0, min(15, z))<<4|max(0, min(15, x))] }
	bx, bz := cx<<4, cz<<4
	bc := &bufColumn{s: s, ctx: ctx, buf: buf, changed: changed}
	for x := range int32(16) {
		for z := range int32(16) {
			wx, wz := bx+x, bz+z
			bc.x, bc.z = x, z
			start := ctx.heights[z<<4|x] + ctx.minY
			b := ctx.biomeAt(wx, start, wz)
			if b == s.erodedBadlands {
				s.erodedBadlandsExtension(bc, wx, wz, start, ctx.minY)
				start = ctx.heights[z<<4|x] + ctx.minY
			}
			gx := height(x+1, z) - height(x-1, z)
			gz := height(x, z+1) - height(x, z-1)
			ctx.updateXZ(wx, wz, gx, gz)
			above, water := int32(0), int32(math.MinInt32)
			nextCeil := int32(math.MaxInt32)
			for ry := start - ctx.minY - 1; ry >= 0; ry-- {
				st := at(x, ry, z)
				y := ry + ctx.minY
				if s.reg.IsAir(st) {
					above, water = 0, math.MinInt32
					continue
				}
				if s.reg.HasFluid(st) {
					if water == math.MinInt32 {
						water = y + 1
					}
					continue
				}
				if nextCeil >= y {
					nextCeil = math.MinInt32
					for ly := ry - 1; ly >= -1; ly-- {
						if ly < 0 || !s.isStone(at(x, ly, z)) {
							nextCeil = ly + 1 + ctx.minY
							break
						}
					}
				}
				above++
				ctx.updateY(above, y-nextCeil+1, water, y)
				if st == s.DefaultBlock && rule != nil {
					r := rule.apply(ctx)
					if ctx.onEval != nil {
						ctx.onEval(ctx, r)
					}
					if r != noState {
						buf[ry<<8|z<<4|x] = uint32(r)
						changed[ry>>4] = true
					}
				}
			}
			if b == s.frozenOcean || b == s.deepFrozen {
				s.frozenOceanExtension(ctx.minSurface, b, bc, wx, wz, start)
			}
		}
	}
}

// bufColumn is one column of the build buffer; writes keep the
// WORLD_SURFACE_WG heights current, as the reference proto-chunk does.
type bufColumn struct {
	s       *System
	ctx     *Ctx
	buf     []uint32
	changed []bool
	x, z    int32
}

func (b *bufColumn) get(y int32) data.StateID {
	ry := y - b.ctx.minY
	if ry < 0 || ry >= b.ctx.height {
		return b.s.sAir
	}
	return data.StateID(b.buf[ry<<8|b.z<<4|b.x])
}

func (b *bufColumn) set(y int32, st data.StateID) {
	ry := y - b.ctx.minY
	if ry < 0 || ry >= b.ctx.height {
		return
	}
	b.buf[ry<<8|b.z<<4|b.x] = uint32(st)
	b.changed[ry>>4] = true
	h := &b.ctx.heights[b.z<<4|b.x]
	if !b.s.reg.IsAir(st) && ry >= *h {
		*h = ry + 1
	}
}

// TopMaterial is the reference MaterialSystem.topMaterial, used by carvers
// to re-top dirt uncovered below a carved grass block: the material rule at
// absolute x,y,z of chunk c as if the block were the top of a one-block
// floor (stone depth 1 above and below, water above when underFluid), with
// the column's surface depth and the chunk's current WORLD_SURFACE heights
// for steepness. ok is false when no rule matches.
func (s *System) TopMaterial(ctx *Ctx, c *world.Chunk, b Biomes, x, y, z int32, underFluid bool) (data.StateID, bool) {
	if ctx.s != s || ctx.biomes != b || ctx.topX != c.X || ctx.topZ != c.Z || !ctx.topOK {
		ctx.reset(s, b)
		ctx.topX, ctx.topZ, ctx.topOK = c.X, c.Z, true
	}
	ctx.minY, ctx.height = int32(c.Dim.MinY), int32(c.Dim.Height)
	lx, lz := int(x&15), int(z&15)
	h := func(x, z int) int32 {
		return int32(c.Height(world.WorldSurface, max(0, min(15, x)), max(0, min(15, z))))
	}
	ctx.updateXZ(x, z, h(lx+1, lz)-h(lx-1, lz), h(lx, lz+1)-h(lx, lz-1))
	water := int32(math.MinInt32)
	if underFluid {
		water = y + 1
	}
	ctx.updateY(1, 1, water, y)
	r := s.rule.apply(ctx)
	return r, r != noState
}
