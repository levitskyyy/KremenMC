package generator

import (
	"math"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// Terrain is an original noise generator: continental/detail/ridge height
// fields, temperature-humidity biomes, rivers, spaghetti and cavern caves,
// ore blobs and trees. Output depends only on (seed, chunk position), so
// chunks can be generated concurrently in any order.
type Terrain struct {
	Seed     int64
	SeaLevel int

	continent, detail, ridge, river *Octaves
	temp, humid, jitter             *Octaves
	caveA, caveB, cavern            *Octaves
	surface                         *Octaves

	b blocks
}

type blocks struct {
	stone, deepslate, bedrock, water, lava, air, caveAir       uint32
	grass, snowyGrass, dirt, coarseDirt, podzol, sand, redSand uint32
	sandstone, gravel, terracotta, snowBlock, snow, ice, clay  uint32
	shortGrass, dandelion, poppy, deadBush, cactus             uint32
	bands                                                      []uint32
	ores                                                       []ore
	trees                                                      [numTreeKinds]treeKind
}

type ore struct {
	normal, deep   uint32
	minY, maxY     int
	perChunk, size int
}

type treeKind struct{ log, leaves uint32 }

const (
	treeOak = iota
	treeBirch
	treeSpruce
	treeJungle
	treeAcacia
	treeDarkOak
	treeCherry
	numTreeKinds
)

type biomeKind uint8

const (
	bPlains biomeKind = iota
	bForest
	bBirchForest
	bDarkForest
	bFlowerForest
	bTaiga
	bSnowyTaiga
	bSnowyPlains
	bSavanna
	bJungle
	bSwamp
	bDesert
	bBadlands
	bCherry
	bStonyPeaks
	bSnowySlopes
	bBeach
	bSnowyBeach
	bRiver
	bFrozenRiver
	bOcean
	bColdOcean
	bFrozenOcean
	bLukewarmOcean
	bWarmOcean
	bDeepOcean
	numBiomeKinds
)

var biomeNames = [numBiomeKinds]string{
	"plains", "forest", "birch_forest", "dark_forest", "flower_forest", "taiga", "snowy_taiga",
	"snowy_plains", "savanna", "jungle", "swamp", "desert", "badlands", "cherry_grove",
	"stony_peaks", "snowy_slopes", "beach", "snowy_beach", "river", "frozen_river", "ocean",
	"cold_ocean", "frozen_ocean", "lukewarm_ocean", "warm_ocean", "deep_ocean",
}

var biomeIDs = func() (ids [numBiomeKinds]data.BiomeID) {
	reg := data.Canonical()
	for i, n := range biomeNames {
		ids[i], _ = reg.BiomeByName(n)
	}
	return
}()

func st(s string) uint32 { return uint32(data.MustState(s)) }

var defaultBlocks = sync.OnceValue(func() blocks {
	b := blocks{
		stone: st("stone"), deepslate: st("deepslate[axis=y]"), bedrock: st("bedrock"),
		water: st("water[level=0]"), lava: st("lava[level=0]"), air: st("air"), caveAir: st("cave_air"),
		grass: st("grass_block[snowy=false]"), snowyGrass: st("grass_block[snowy=true]"),
		dirt: st("dirt"), coarseDirt: st("coarse_dirt"), podzol: st("podzol[snowy=false]"),
		sand: st("sand"), redSand: st("red_sand"), sandstone: st("sandstone"), gravel: st("gravel"),
		terracotta: st("terracotta"), snowBlock: st("snow_block"), snow: st("snow[layers=1]"),
		ice: st("ice"), clay: st("clay"), shortGrass: st("short_grass"), dandelion: st("dandelion"),
		poppy: st("poppy"), deadBush: st("dead_bush"), cactus: st("cactus[age=0]"),
	}
	for _, n := range []string{"terracotta", "orange_terracotta", "yellow_terracotta", "terracotta",
		"red_terracotta", "white_terracotta", "light_gray_terracotta", "brown_terracotta", "orange_terracotta"} {
		b.bands = append(b.bands, st(n))
	}
	b.ores = []ore{
		{st("coal_ore"), st("deepslate_coal_ore"), 0, 192, 18, 14},
		{st("iron_ore"), st("deepslate_iron_ore"), -64, 72, 12, 9},
		{st("copper_ore"), st("deepslate_copper_ore"), -16, 112, 8, 10},
		{st("gold_ore"), st("deepslate_gold_ore"), -64, 32, 4, 8},
		{st("redstone_ore[lit=false]"), st("deepslate_redstone_ore[lit=false]"), -64, 16, 6, 7},
		{st("lapis_ore"), st("deepslate_lapis_ore"), -64, 64, 2, 6},
		{st("diamond_ore"), st("deepslate_diamond_ore"), -64, 16, 3, 5},
	}
	for i, n := range [numTreeKinds]string{"oak", "birch", "spruce", "jungle", "acacia", "dark_oak", "cherry"} {
		b.trees[i] = treeKind{st(n + "_log[axis=y]"), st(n + "_leaves[distance=1,persistent=true,waterlogged=false]")}
	}
	return b
})

func NewTerrain(seed int64) *Terrain {
	s := uint64(seed)
	t := &Terrain{Seed: seed, SeaLevel: 63, b: defaultBlocks()}
	t.continent = NewOctaves(hash(s, 1), 5)
	t.detail = NewOctaves(hash(s, 2), 4)
	t.ridge = NewOctaves(hash(s, 3), 4)
	t.river = NewOctaves(hash(s, 4), 3)
	t.temp = NewOctaves(hash(s, 5), 3)
	t.humid = NewOctaves(hash(s, 6), 3)
	t.jitter = NewOctaves(hash(s, 7), 2)
	t.caveA = NewOctaves(hash(s, 8), 2)
	t.caveB = NewOctaves(hash(s, 9), 2)
	t.cavern = NewOctaves(hash(s, 10), 3)
	t.surface = NewOctaves(hash(s, 11), 2)
	return t
}

type column struct {
	height int // y of the top solid block
	biome  biomeKind
}

func smoothstep(e0, e1, x float64) float64 {
	t := math.Max(0, math.Min(1, (x-e0)/(e1-e0)))
	return t * t * (3 - 2*t)
}

// sample computes height and biome for world column x,z; pure.
func (t *Terrain) sample(x, z int) column {
	fx, fz := float64(x), float64(z)
	c := t.continent.At2(fx/1400, fz/1400)*1.6 + 0.12
	d := t.detail.At2(fx/110, fz/110)
	r := 1 - math.Abs(t.ridge.At2(fx/600, fz/600)*1.8)
	land := smoothstep(-0.05, 0.35, c)
	h := float64(t.SeaLevel) + c*42 + d*(5+9*land)
	h += r * r * r * 110 * smoothstep(0.15, 0.6, c)

	rv := math.Abs(t.river.At2(fx/520, fz/520))
	riverW := 0.04
	isRiver := false
	if rv < riverW && h > float64(t.SeaLevel)-2 {
		k := smoothstep(0, riverW, rv)
		target := float64(t.SeaLevel) - 3
		if h < float64(t.SeaLevel)+25 {
			h = target + (h-target)*k
			isRiver = k < 0.6
		}
	}
	height := int(math.Floor(h))
	temp := t.temp.At2(fx/900, fz/900)*1.8 + t.jitter.At2(fx/40, fz/40)*0.06
	hum := t.humid.At2(fx/750, fz/750)*1.8 + t.jitter.At2(fz/40, fx/40)*0.06
	return column{height: height, biome: t.pickBiome(height, temp, hum, isRiver, c)}
}

func (t *Terrain) pickBiome(h int, temp, hum float64, river bool, cont float64) biomeKind {
	sea := t.SeaLevel
	switch {
	case h < sea-1:
		if river {
			if temp < -0.45 {
				return bFrozenRiver
			}
			return bRiver
		}
		deep := h < sea-18
		switch {
		case temp < -0.45:
			return bFrozenOcean
		case temp < -0.15:
			return bColdOcean
		case temp > 0.5:
			return bWarmOcean
		case temp > 0.25:
			return bLukewarmOcean
		case deep:
			return bDeepOcean
		}
		return bOcean
	case river && h <= sea+1:
		if temp < -0.45 {
			return bFrozenRiver
		}
		return bRiver
	case h <= sea+2 && cont < 0.2:
		if temp < -0.45 {
			return bSnowyBeach
		}
		if temp > 0.5 && hum < -0.2 {
			return bDesert
		}
		return bBeach
	case h > sea+75:
		if temp < 0.1 {
			return bSnowySlopes
		}
		return bStonyPeaks
	}
	switch {
	case temp < -0.45:
		if hum > 0.1 {
			return bSnowyTaiga
		}
		return bSnowyPlains
	case temp < -0.15:
		if hum > -0.2 {
			return bTaiga
		}
		return bPlains
	case temp < 0.3:
		switch {
		case hum > 0.45:
			if h < sea+5 {
				return bSwamp
			}
			return bDarkForest
		case hum > 0.15:
			return bForest
		case hum > -0.05:
			return bBirchForest
		case hum > -0.2 && h > sea+30:
			return bCherry
		case hum < -0.45:
			return bFlowerForest
		}
		return bPlains
	default:
		switch {
		case hum > 0.35:
			return bJungle
		case hum < -0.35:
			if temp > 0.6 {
				return bBadlands
			}
			return bDesert
		case hum < 0:
			return bSavanna
		}
		return bPlains
	}
}

func (k biomeKind) snowy() bool {
	switch k {
	case bSnowyPlains, bSnowyTaiga, bSnowySlopes, bSnowyBeach, bFrozenRiver, bFrozenOcean:
		return true
	}
	return false
}

func (k biomeKind) ocean() bool { return k >= bOcean }

var bufPool = sync.Pool{New: func() any { return new(genBuf) }}

type genBuf struct {
	vals []uint32 // sections*4096
	cols [256]column
	cave []float64
}

func (t *Terrain) Generate(c *world.Chunk) {
	g := bufPool.Get().(*genBuf)
	defer bufPool.Put(g)
	minY, height := c.Dim.MinY, c.Dim.Height
	n := len(c.Sections) * 4096
	if cap(g.vals) < n {
		g.vals = make([]uint32, n)
	}
	vals := g.vals[:n]
	clear(vals)
	bx, bz := int(c.X)<<4, int(c.Z)<<4
	for i := range g.cols {
		g.cols[i] = t.sample(bx+i&15, bz+i>>4)
	}
	set := func(x, y, z int, v uint32) {
		if y < minY || y >= minY+height {
			return
		}
		ry := y - minY
		vals[(ry>>4)*4096+(ry&15)<<8|z<<4|x] = v
	}
	get := func(x, y, z int) uint32 {
		if y < minY || y >= minY+height {
			return t.b.air
		}
		ry := y - minY
		return vals[(ry>>4)*4096+(ry&15)<<8|z<<4|x]
	}

	t.fillColumns(c, g, set)
	t.carveCaves(c, g, get, set)
	t.placeOres(c, get, set)
	t.decorate(c, g, get, set)

	for si := range c.Sections {
		c.Sections[si].Blocks.SetAll(vals[si*4096 : (si+1)*4096])
	}
	t.fillBiomes(c, g)
	c.Finish()
}

func (t *Terrain) fillColumns(c *world.Chunk, g *genBuf, set func(x, y, z int, v uint32)) {
	b := &t.b
	minY := c.Dim.MinY
	top := minY + c.Dim.Height - 1
	bx, bz := int(c.X)<<4, int(c.Z)<<4
	for i, col := range g.cols {
		x, z := i&15, i>>4
		wx, wz := bx+x, bz+z
		h := min(col.height, top)
		depth := 3 + int(t.surface.At2(float64(wx)/12, float64(wz)/12)*2+1.5)
		topB, fill := t.surfaceBlocks(col, h)
		for y := minY; y <= h; y++ {
			v := b.stone
			switch {
			case y < minY+5 && int(hash(uint64(t.Seed), int64(wx), int64(y), int64(wz))%5) >= y-minY:
				v = b.bedrock
			case y < 0 || (y < 8 && int(hash(uint64(t.Seed)^1, int64(wx), int64(y), int64(wz))%8) >= y):
				v = b.deepslate
			case y == h:
				v = topB
			case y > h-depth:
				v = fill
			}
			if col.biome == bBadlands && y > h-depth-12 && y < h && y > t.SeaLevel {
				v = b.bands[(y+int(t.surface.At2(float64(wx)/60, float64(wz)/60)*4)+64)%len(b.bands)]
			}
			set(x, y, z, v)
		}
		for y := h + 1; y <= t.SeaLevel && y <= top; y++ {
			v := b.water
			if y == t.SeaLevel && col.biome.snowy() {
				v = b.ice
			}
			set(x, y, z, v)
		}
	}
}

func (t *Terrain) surfaceBlocks(col column, h int) (top, fill uint32) {
	b := &t.b
	under := h < t.SeaLevel
	switch col.biome {
	case bDesert, bBeach, bWarmOcean, bLukewarmOcean:
		return b.sand, b.sandstone
	case bSnowyBeach:
		return b.sand, b.sand
	case bBadlands:
		return b.redSand, b.terracotta
	case bStonyPeaks:
		return b.stone, b.stone
	case bSnowySlopes:
		return b.snowBlock, b.snowBlock
	case bOcean, bColdOcean, bFrozenOcean, bDeepOcean:
		if h < t.SeaLevel-20 {
			return b.gravel, b.gravel
		}
		return b.sand, b.sand
	case bRiver, bFrozenRiver:
		if under {
			return b.sand, b.dirt
		}
	case bSwamp:
		if under {
			return b.clay, b.dirt
		}
	case bTaiga, bSnowyTaiga:
		if !under && (h*7)%5 == 0 {
			return b.podzol, b.dirt
		}
	}
	if under {
		return b.dirt, b.dirt
	}
	if col.biome.snowy() {
		return b.snowyGrass, b.dirt
	}
	return b.grass, b.dirt
}

// carveCaves samples two noise fields on a 4-block lattice and carves where
// both are near zero (tunnels) or a third field is high (caverns).
func (t *Terrain) carveCaves(c *world.Chunk, g *genBuf, get func(x, y, z int) uint32, set func(x, y, z int, v uint32)) {
	b := &t.b
	minY := c.Dim.MinY
	maxH := minY
	for _, col := range g.cols {
		maxH = max(maxH, col.height)
	}
	ny := (maxH-minY)/4 + 2
	need := 5 * 5 * ny * 3
	if cap(g.cave) < need {
		g.cave = make([]float64, need)
	}
	f := g.cave[:need]
	bx, bz := int(c.X)<<4, int(c.Z)<<4
	idx := func(ix, iy, iz, k int) int { return ((iy*5+iz)*5+ix)*3 + k }
	for iy := 0; iy < ny; iy++ {
		y := float64(minY + iy*4)
		for iz := range 5 {
			z := float64(bz + iz*4)
			for ix := range 5 {
				x := float64(bx + ix*4)
				f[idx(ix, iy, iz, 0)] = t.caveA.At3(x/70, y/45, z/70)
				f[idx(ix, iy, iz, 1)] = t.caveB.At3(x/70, y/45, z/70)
				f[idx(ix, iy, iz, 2)] = t.cavern.At3(x/110, y/55, z/110)
			}
		}
	}
	for i, col := range g.cols {
		x, z := i&15, i>>4
		ix, fx := x>>2, float64(x&3)/4
		iz, fz := z>>2, float64(z&3)/4
		limit := col.height - 7
		if col.biome.ocean() || col.biome == bRiver || col.biome == bFrozenRiver {
			limit = col.height - 12
		}
		for y := minY + 5; y <= limit; y++ {
			iy, fy := (y-minY)>>2, float64((y-minY)&3)/4
			var v [3]float64
			for k := range 3 {
				c00 := lerp(fx, f[idx(ix, iy, iz, k)], f[idx(ix+1, iy, iz, k)])
				c10 := lerp(fx, f[idx(ix, iy+1, iz, k)], f[idx(ix+1, iy+1, iz, k)])
				c01 := lerp(fx, f[idx(ix, iy, iz+1, k)], f[idx(ix+1, iy, iz+1, k)])
				c11 := lerp(fx, f[idx(ix, iy+1, iz+1, k)], f[idx(ix+1, iy+1, iz+1, k)])
				v[k] = lerp(fz, lerp(fy, c00, c10), lerp(fy, c01, c11))
			}
			tunnel := v[0]*v[0]+v[1]*v[1] < 0.0035
			cavern := y < 40 && v[2] > 0.42-float64(40-y)*0.002
			if !tunnel && !cavern {
				continue
			}
			if cur := get(x, y, z); cur == b.bedrock || cur == b.water {
				continue
			}
			if y < minY+10 {
				set(x, y, z, b.lava)
			} else {
				set(x, y, z, b.caveAir)
			}
		}
	}
}

func (t *Terrain) placeOres(c *world.Chunk, get func(x, y, z int) uint32, set func(x, y, z int, v uint32)) {
	b := &t.b
	r := rng{hash(uint64(t.Seed), int64(c.X), int64(c.Z), 0x0E5)}
	minY, maxY := c.Dim.MinY, c.Dim.MaxY()
	for _, o := range b.ores {
		lo, hi := max(o.minY, minY), min(o.maxY, maxY)
		if lo > hi {
			continue
		}
		for range o.perChunk {
			x, y, z := r.intn(16), r.between(lo, hi), r.intn(16)
			for range o.size {
				if x >= 0 && x < 16 && z >= 0 && z < 16 {
					switch get(x, y, z) {
					case b.stone:
						set(x, y, z, o.normal)
					case b.deepslate:
						set(x, y, z, o.deep)
					}
				}
				switch r.intn(3) {
				case 0:
					x += r.intn(3) - 1
				case 1:
					y += r.intn(3) - 1
				default:
					z += r.intn(3) - 1
				}
			}
		}
	}
}

func (k biomeKind) trees() (kind int, perChunk int) {
	switch k {
	case bForest, bFlowerForest:
		return treeOak, 8
	case bBirchForest:
		return treeBirch, 9
	case bDarkForest:
		return treeDarkOak, 14
	case bTaiga, bSnowyTaiga:
		return treeSpruce, 8
	case bJungle:
		return treeJungle, 12
	case bSavanna:
		return treeAcacia, 1
	case bSwamp:
		return treeOak, 2
	case bCherry:
		return treeCherry, 4
	case bPlains, bSnowyPlains:
		return treeOak, 0
	}
	return -1, 0
}

// decorate places vegetation and trees. Trees rooted in neighbor chunks are
// replayed so their overhang lands here too.
func (t *Terrain) decorate(c *world.Chunk, g *genBuf, get func(x, y, z int) uint32, set func(x, y, z int, v uint32)) {
	b := &t.b
	bx, bz := int(c.X)<<4, int(c.Z)<<4
	r := rng{hash(uint64(t.Seed), int64(c.X), int64(c.Z), 0xF10)}
	for i, col := range g.cols {
		x, z := i&15, i>>4
		h := col.height
		if h < t.SeaLevel || h+1 > c.Dim.MaxY() {
			continue
		}
		ground := get(x, h, z)
		roll := r.intn(100)
		switch {
		case col.biome.snowy() && ground != b.water && ground != b.ice:
			if get(x, h+1, z) == b.air {
				set(x, h+1, z, b.snow)
			}
		case col.biome == bDesert && ground == b.sand:
			if roll < 1 {
				for y := h + 1; y <= h+1+r.intn(3); y++ {
					set(x, y, z, b.cactus)
				}
			} else if roll < 3 {
				set(x, h+1, z, b.deadBush)
			}
		case ground == b.grass:
			switch {
			case roll < 12:
				set(x, h+1, z, b.shortGrass)
			case roll < 13 || (col.biome == bFlowerForest && roll < 22):
				set(x, h+1, z, b.dandelion)
			case roll < 14 || (col.biome == bFlowerForest && roll < 30):
				set(x, h+1, z, b.poppy)
			}
		}
	}

	for dz := -1; dz <= 1; dz++ {
		for dx := -1; dx <= 1; dx++ {
			ncx, ncz := int64(c.X)+int64(dx), int64(c.Z)+int64(dz)
			tr := rng{hash(uint64(t.Seed), ncx, ncz, 0x7EE)}
			base := t.sample(int(ncx)<<4+8, int(ncz)<<4+8)
			kind, count := base.biome.trees()
			if base.biome == bPlains && tr.intn(4) == 0 {
				count = 1
			}
			for range count {
				lx, lz := tr.intn(16), tr.intn(16)
				size := tr.next()
				if kind < 0 {
					continue
				}
				wx, wz := int(ncx)<<4+lx, int(ncz)<<4+lz
				col := t.sample(wx, wz)
				if k, _ := col.biome.trees(); k != kind && !(col.biome == bPlains && kind == treeOak) {
					continue
				}
				if col.height < t.SeaLevel || col.height+20 > c.Dim.MaxY() {
					continue
				}
				if col.biome.ocean() || col.biome == bBeach || col.biome == bRiver || col.biome == bStonyPeaks {
					continue
				}
				t.tree(kind, size, wx-bx, col.height+1, wz-bz, get, set)
			}
		}
	}
}

// tree draws a tree with its base at chunk-relative x,z (may lie outside 0..15);
// only blocks inside the chunk are written.
func (t *Terrain) tree(kind int, size uint64, x, y, z int, get func(x, y, z int) uint32, set func(x, y, z int, v uint32)) {
	b := &t.b
	tk := b.trees[kind]
	in := func(x, z int) bool { return x >= 0 && x < 16 && z >= 0 && z < 16 }
	leaf := func(x, y, z int) {
		if in(x, z) {
			if v := get(x, y, z); v == b.air || v == b.shortGrass || v == b.snow {
				set(x, y, z, tk.leaves)
			}
		}
	}
	log := func(x, y, z int) {
		if in(x, z) {
			if v := get(x, y, z); v == b.air || v == tk.leaves || v == b.shortGrass || v == b.snow {
				set(x, y, z, tk.log)
			}
		}
	}
	if in(x, z) {
		g := get(x, y-1, z)
		if g != b.grass && g != b.snowyGrass && g != b.podzol && g != b.dirt {
			return
		}
		set(x, y-1, z, b.dirt)
	}
	trunk := 4 + int(size%3)
	switch kind {
	case treeSpruce:
		trunk = 6 + int(size%4)
		for i := 0; i < trunk; i++ {
			log(x, y+i, z)
		}
		for i := 2; i <= trunk+1; i++ {
			rad := (trunk + 1 - i) % 3
			if i == trunk+1 {
				rad = 0
			}
			for dz := -rad; dz <= rad; dz++ {
				for dx := -rad; dx <= rad; dx++ {
					if abs(dx)+abs(dz) <= rad+1 && (dx != 0 || dz != 0 || i >= trunk) {
						leaf(x+dx, y+i, z+dz)
					}
				}
			}
		}
	case treeJungle:
		trunk = 8 + int(size%6)
		for i := 0; i < trunk; i++ {
			log(x, y+i, z)
		}
		blob(x, y+trunk-1, z, 3, 2, leaf)
	case treeAcacia:
		dir := int(size % 4)
		ox, oz := []int{1, -1, 0, 0}[dir], []int{0, 0, 1, -1}[dir]
		cx, cz := x, z
		for i := 0; i < trunk; i++ {
			if i >= trunk-2 {
				cx, cz = cx+ox, cz+oz
			}
			log(cx, y+i, cz)
		}
		blob(cx, y+trunk, cz, 3, 1, leaf)
	case treeDarkOak, treeCherry:
		trunk = 5 + int(size%2)
		for i := 0; i < trunk; i++ {
			log(x, y+i, z)
			log(x+1, y+i, z)
			log(x, y+i, z+1)
			log(x+1, y+i, z+1)
		}
		blob(x, y+trunk-1, z, 3, 2, leaf)
	default:
		if kind == treeBirch {
			trunk++
		}
		for i := 0; i < trunk; i++ {
			log(x, y+i, z)
		}
		blob(x, y+trunk-2, z, 2, 2, leaf)
		for dz := -1; dz <= 1; dz++ {
			for dx := -1; dx <= 1; dx++ {
				if abs(dx)+abs(dz) <= 1 {
					leaf(x+dx, y+trunk, z+dz)
				}
			}
		}
	}
}

func blob(x, y, z, r, h int, leaf func(x, y, z int)) {
	for dy := 0; dy < h; dy++ {
		rr := r - dy/2
		for dz := -rr; dz <= rr; dz++ {
			for dx := -rr; dx <= rr; dx++ {
				if dx*dx+dz*dz <= rr*rr+1 {
					leaf(x+dx, y+dy, z+dz)
				}
			}
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (t *Terrain) fillBiomes(c *world.Chunk, g *genBuf) {
	var cells [16]data.BiomeID
	for cz := range 4 {
		for cx := range 4 {
			cells[cz<<2|cx] = biomeIDs[g.cols[(cz*4+2)<<4|(cx*4+2)].biome]
		}
	}
	var vals [64]uint32
	for i := range vals {
		vals[i] = uint32(cells[i&15])
	}
	for si := range c.Sections {
		c.Sections[si].Biomes.SetAll(vals[:])
	}
}
