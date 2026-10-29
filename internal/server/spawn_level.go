package server

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/entity/spawn"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// spawnLevel is the spawn.Level view of a World for one spawn pass. It is
// owned by the tick goroutine and rebuilt by begin() every tick; chunk reads
// take the chunk read lock, so it is safe against IO and plugin goroutines.
type spawnLevel struct {
	w        *World
	dim      *spawn.Dimension
	seaLevel int
	zoomSeed int64

	players   []spawn.Player
	pchunks   []world.ChunkPos // every player in the world, spectators included
	simDist   int
	darken    int
	dayTime   int64
	diff      spawn.Difficulty
	thunder   bool
	spawnPos  [3]int
	spawnMobs bool

	lastKey   uint64
	lastChunk *world.Chunk
	haveLast  bool

	ents map[uint64][]*spawnEnt
}

// spawnEnt is an entity in the per-pass spatial index.
type spawnEnt struct {
	box       spawn.AABB
	typ       *spawn.EntityType // nil for players
	mob       *Mob
	spectator bool
	blocks    bool // Entity.blocksBuilding
	hard      bool // canBeCollidedWith
}

var biomeKeys = sync.OnceValue(func() []string {
	bs := data.Canonical().Biomes
	out := make([]string, len(bs))
	for i := range bs {
		out[i] = "minecraft:" + bs[i].Name
	}
	return out
})

// obfuscateSeed is BiomeManager.obfuscateSeed: the first 8 bytes (little
// endian) of SHA-256 over the little-endian seed.
func obfuscateSeed(seed int64) int64 {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(seed))
	h := sha256.Sum256(b[:])
	return int64(binary.LittleEndian.Uint64(h[:8]))
}

func (l *spawnLevel) chunk(cx, cz int) *world.Chunk {
	k := world.ChunkPos{X: int32(cx), Z: int32(cz)}.Key()
	if l.haveLast && k == l.lastKey {
		return l.lastChunk
	}
	c := l.w.w.Chunk(world.PosFromKey(k))
	l.lastKey, l.lastChunk, l.haveLast = k, c, true
	return c
}

func (l *spawnLevel) loaded(cx, cz int) bool { return l.chunk(cx, cz) != nil }

// Block implements entity.BlockSource for collision checks.
func (l *spawnLevel) Block(x, y, z int) data.StateID { return l.BlockState(x, y, z) }

func (l *spawnLevel) Dimension() *spawn.Dimension { return l.dim }
func (l *spawnLevel) SeaLevel() int               { return l.seaLevel }

func (l *spawnLevel) BlockState(x, y, z int) data.StateID {
	c := l.chunk(x>>4, z>>4)
	if c == nil {
		return data.Air
	}
	c.RLock()
	s := c.Block(x&15, y, z&15)
	c.RUnlock()
	return s
}

func (l *spawnLevel) light(sky bool, x, y, z int) int {
	si := (y-l.dim.MinY)>>4 + 1
	if si < 0 {
		return 0
	}
	c := l.chunk(x>>4, z>>4)
	if c == nil || si >= len(c.SkyLight) {
		if sky && l.dim.HasSkyLight {
			return 15
		}
		return 0
	}
	li := (y&15)<<8 | (z&15)<<4 | x&15
	c.RLock()
	var v uint8
	if sky {
		v = c.SkyLight[si].Get(li)
	} else {
		v = c.BlockLight[si].Get(li)
	}
	c.RUnlock()
	return int(v)
}

func (l *spawnLevel) SkyLight(x, y, z int) int   { return l.light(true, x, y, z) }
func (l *spawnLevel) BlockLight(x, y, z int) int { return l.light(false, x, y, z) }
func (l *spawnLevel) SkyDarken() int             { return l.darken }

var spawnHeightmaps = [...]world.Heightmap{
	spawn.HeightmapWorldSurface:           world.WorldSurface,
	spawn.HeightmapMotionBlocking:         world.MotionBlocking,
	spawn.HeightmapMotionBlockingNoLeaves: world.MotionBlockingNoLeaves,
	spawn.HeightmapOceanFloor:             world.OceanFloor,
}

func (l *spawnLevel) Height(h spawn.Heightmap, x, z int) int {
	c := l.chunk(x>>4, z>>4)
	if c == nil {
		return l.dim.MinY
	}
	c.RLock()
	y := c.Height(spawnHeightmaps[h], x&15, z&15)
	c.RUnlock()
	return y
}

// Biome is BiomeManager.getBiome: the fuzzy zoom picks a quart cell, whose
// y is clamped to the chunk. A cell in an unloaded neighbour falls back to
// the block's own chunk.
func (l *spawnLevel) Biome(x, y, z int) string {
	qx, qy, qz := zoomQuart(l.zoomSeed, int32(x), int32(y), int32(z))
	minQ := l.dim.MinY >> 2
	qy = min(max(qy, int32(minQ)), int32(minQ+l.dim.Height>>2-1))
	c := l.chunk(int(qx>>2), int(qz>>2))
	if c == nil {
		if c = l.chunk(x>>4, z>>4); c == nil {
			return "minecraft:plains"
		}
		qx, qz = int32(x>>2), int32(z>>2)
	}
	c.RLock()
	b := c.Biome(int(qx&3)<<2, int(qy)<<2, int(qz&3)<<2)
	c.RUnlock()
	if keys := biomeKeys(); int(b) < len(keys) {
		return keys[b]
	}
	return "minecraft:plains"
}

func (l *spawnLevel) Players() []spawn.Player { return l.players }

func (l *spawnLevel) SharedSpawn() (int, int, int) {
	return l.spawnPos[0], l.spawnPos[1], l.spawnPos[2]
}

// chebyshev returns the smallest chessboard distance from (cx, cz) to any
// player chunk.
func (l *spawnLevel) chebyshev(cx, cz int) int {
	best := math.MaxInt
	for _, p := range l.pchunks {
		d := max(absInt(cx-int(p.X)), absInt(cz-int(p.Z)))
		best = min(best, d)
	}
	return best
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// entityTicking mirrors the ENTITY_TICKING full-chunk status: loaded and
// strictly inside the simulation distance of a player ticket.
func (l *spawnLevel) entityTicking(cx, cz int) bool {
	return l.chebyshev(cx, cz) < l.simDist && l.loaded(cx, cz)
}

// blockTicking mirrors BLOCK_TICKING (the chunk list natural spawning and
// block entities iterate).
func (l *spawnLevel) blockTicking(cx, cz int) bool {
	return l.chebyshev(cx, cz) <= l.simDist && l.loaded(cx, cz)
}

func (l *spawnLevel) CanSpawnEntitiesInChunk(cx, cz int) bool { return l.entityTicking(cx, cz) }

func toEntityBox(b spawn.AABB) entity.AABB {
	return entity.AABB{MinX: b.MinX, MinY: b.MinY, MinZ: b.MinZ, MaxX: b.MaxX, MaxY: b.MaxY, MaxZ: b.MaxZ}
}

func fromEntityBox(b entity.AABB) spawn.AABB {
	return spawn.AABB{MinX: b.MinX, MinY: b.MinY, MinZ: b.MinZ, MaxX: b.MaxX, MaxY: b.MaxY, MaxZ: b.MaxZ}
}

func boxOverlap(a, b spawn.AABB) bool {
	return a.MinX < b.MaxX && a.MaxX > b.MinX && a.MinY < b.MaxY && a.MaxY > b.MinY && a.MinZ < b.MaxZ && a.MaxZ > b.MinZ
}

// eachEnt visits indexed entities whose chunk lies within two blocks of box
// (entity sections are queried with the same margin).
func (l *spawnLevel) eachEnt(box spawn.AABB, fn func(*spawnEnt) bool) {
	x0, x1 := int(math.Floor(box.MinX-2))>>4, int(math.Floor(box.MaxX+2))>>4
	z0, z1 := int(math.Floor(box.MinZ-2))>>4, int(math.Floor(box.MaxZ+2))>>4
	for cx := x0; cx <= x1; cx++ {
		for cz := z0; cz <= z1; cz++ {
			for _, e := range l.ents[world.ChunkPos{X: int32(cx), Z: int32(cz)}.Key()] {
				if !fn(e) {
					return
				}
			}
		}
	}
}

func (l *spawnLevel) NoCollision(box spawn.AABB) bool {
	if entity.Colliding(l, toEntityBox(box)) {
		return false
	}
	if box.MaxX-box.MinX < 1e-7 && box.MaxY-box.MinY < 1e-7 && box.MaxZ-box.MinZ < 1e-7 {
		return true
	}
	g := spawn.AABB{MinX: box.MinX - 1e-7, MinY: box.MinY - 1e-7, MinZ: box.MinZ - 1e-7, MaxX: box.MaxX + 1e-7, MaxY: box.MaxY + 1e-7, MaxZ: box.MaxZ + 1e-7}
	free := true
	l.eachEnt(g, func(e *spawnEnt) bool {
		if e.hard && !e.spectator && boxOverlap(e.box, g) {
			free = false
		}
		return free
	})
	return free
}

func (l *spawnLevel) IsUnobstructed(box spawn.AABB) bool {
	free := true
	l.eachEnt(box, func(e *spawnEnt) bool {
		if e.blocks && !e.spectator && boxOverlap(e.box, box) {
			free = false
		}
		return free
	})
	return free
}

func (l *spawnLevel) Structures(int, int, int) []spawn.StructureHit { return nil }

func (l *spawnLevel) Difficulty() spawn.Difficulty { return l.diff }

// DifficultyAt is getCurrentDifficultyAt: inhabited time and moon only
// count when the chunk is loaded.
func (l *spawnLevel) DifficultyAt(x, _, z int) spawn.DifficultyInstance {
	var inhabited int64
	var moon float32
	if c := l.chunk(x>>4, z>>4); c != nil {
		c.RLock()
		inhabited = c.InhabitedTime
		c.RUnlock()
		moon = l.MoonBrightness()
	}
	return spawn.NewDifficultyInstance(l.diff, l.dayTime, inhabited, moon)
}

func (l *spawnLevel) Thundering() bool { return l.thunder }
func (l *spawnLevel) Seed() int64      { return l.w.seed }

func moonPhase(dayTime int64) int {
	return int(((dayTime/24000)%8 + 8) % 8)
}

var (
	moonBrightness = [8]float32{1, 0.75, 0.5, 0.25, 0, 0.25, 0.5, 0.75}
	slimeChance    = [8]float32{0.5, 0.375, 0.25, 0.125, 0, 0.125, 0.25, 0.375}
)

func (l *spawnLevel) MoonBrightness() float32 { return moonBrightness[moonPhase(l.dayTime)] }

func (l *spawnLevel) SurfaceSlimeChance(int, int, int) float32 {
	return slimeChance[moonPhase(l.dayTime)]
}

// skyLightLevel samples the minecraft:day timeline track
// gameplay/sky_light_level (linear keyframes, period 24000).
func skyLightLevel(dayTime int64) float32 {
	const night = 0.26666668
	t := int(((dayTime % 24000) + 24000) % 24000)
	lerp := func(t0, t1 int, v0, v1 float32, t int) float32 {
		a := float32(t-t0) / float32(t1-t0)
		return v0 + a*(v1-v0)
	}
	switch {
	case t < 133:
		return lerp(22330, 24133, night, 1, t+24000)
	case t < 11867:
		return 1
	case t < 13670:
		return lerp(11867, 13670, 1, night, t)
	case t < 22330:
		return night
	default:
		return lerp(22330, 24133, night, 1, t)
	}
}

// skyDarken is Level.updateSkyBrightness for the overworld. Clear, full
// rain and full thunder match the reference at every tick of the day
// (testdata/spawn_skydarken.txt); partial thunder levels during weather
// fades are approximate, which this server never produces.
func skyDarken(dayTime int64, rain, thunder float32) int {
	l := skyLightLevel(dayTime)
	k := (1 - rain*5/16) * (1 - thunder*rain*5/16)
	g := (l - 0.26666668) / (1 - 0.26666668)
	return int((1 - g*k) * 11)
}

// zoomQuart is the quart cell BiomeManager.getBiome reads for a block.
func zoomQuart(seed int64, x, y, z int32) (int32, int32, int32) {
	i, j, k := x-2, y-2, z-2
	l, m, n := i>>2, j>>2, k>>2
	d, e, f := float64(i&3)/4, float64(j&3)/4, float64(k&3)/4
	best, bestD := 0, 0.0
	for o := range 8 {
		p, q, r := l, m, n
		g, h, s := d, e, f
		if o&4 != 0 {
			p, g = l+1, d-1
		}
		if o&2 != 0 {
			q, h = m+1, e-1
		}
		if o&1 != 0 {
			r, s = n+1, f-1
		}
		if dd := fiddledDistance(seed, p, q, r, g, h, s); o == 0 || bestD > dd {
			best, bestD = o, dd
		}
	}
	if best&4 != 0 {
		l++
	}
	if best&2 != 0 {
		m++
	}
	if best&1 != 0 {
		n++
	}
	return l, m, n
}

func lcgNext(a, b int64) int64 { return a*(a*6364136223846793005+1442695040888963407) + b }

func fiddle(s int64) float64 { return (float64(((s>>24)%1024+1024)%1024)/1024 - 0.5) * 0.9 }

func fiddledDistance(seed int64, x, y, z int32, xf, yf, zf float64) float64 {
	m := lcgNext(seed, int64(x))
	m = lcgNext(m, int64(y))
	m = lcgNext(m, int64(z))
	m = lcgNext(m, int64(x))
	m = lcgNext(m, int64(y))
	m = lcgNext(m, int64(z))
	d := fiddle(m)
	m = lcgNext(m, seed)
	e := fiddle(m)
	m = lcgNext(m, seed)
	f := fiddle(m)
	zz, yy, xx := zf+f, yf+e, xf+d
	return zz*zz + yy*yy + xx*xx
}
