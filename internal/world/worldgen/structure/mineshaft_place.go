package structure

import (
	"strconv"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Mineshaft block placement, inferred from black-box probes:
// kremenmc.probe.legacy.MinePlace (one piece on a stone or air scene) and
// kremenmc.probe.legacy.MineScene (whole starts on explicit scenes through a
// proxy level that logs every draw, read and write with its call path).
// See docs/vanilla-parity.md "Mineshaft".

// local maps a piece-local position to world x,z (the legacy frame also
// inferred from the swamp huts).
func (p *msPiece) local(lx, lz int32) (int32, int32) {
	b := p.box
	switch p.dir {
	case msNorth:
		return b.MinX + lx, b.MaxZ - lz
	case msSouth:
		return b.MinX + lx, b.MinZ + lz
	case msEast:
		return b.MinX + lz, b.MinZ + lx
	default:
		return b.MaxX - lz, b.MinZ + lx
	}
}

// msMaterials are the wood states of a mineshaft type.
type msMaterials struct {
	planks, log, fence, chain data.StateID
}

type msPlacer struct {
	p       *msPiece
	lv      Level
	clip    Box
	reg     *data.Registry
	r       *rng.WorldgenRandom
	mat     msMaterials
	spawner data.StateID
	// persistSpider records a placed spawner on the piece (vanilla's
	// in-memory hasPlacedSpider; probe replays only, never in the pipeline).
	persistSpider bool
	trace         func(label, val string)
}

func newMsPlacer(p *msPiece, lv Level, r *rng.WorldgenRandom, clip Box) *msPlacer {
	reg := data.Canonical()
	m := &msPlacer{p: p, lv: lv, clip: clip, reg: reg, r: r}
	wood := "oak"
	if p.mesa {
		wood = "dark_oak"
	}
	m.mat = msMaterials{
		planks: m.state("minecraft:" + wood + "_planks"),
		log:    m.state("minecraft:" + wood + "_log"),
		fence:  m.state("minecraft:" + wood + "_fence"),
		chain:  m.state("minecraft:iron_chain"),
	}
	m.spawner = m.state("minecraft:spawner")
	return m
}

func (m *msPlacer) float(label string) float32 {
	v := m.r.NextFloat()
	if m.trace != nil {
		m.trace(label, strconv.FormatFloat(float64(v), 'g', -1, 32))
	}
	return v
}

func (m *msPlacer) intn(n int32, label string) int32 {
	v := m.r.NextIntn(n)
	if m.trace != nil {
		m.trace(label, strconv.Itoa(int(v)))
	}
	return v
}

func (m *msPlacer) boolean(label string) bool {
	v := m.r.NextBool()
	if m.trace != nil {
		m.trace(label, strconv.FormatBool(v))
	}
	return v
}

func (m *msPlacer) long(label string) int64 {
	v := m.r.NextLong()
	if m.trace != nil {
		m.trace(label, strconv.FormatInt(v, 10))
	}
	return v
}

func (m *msPlacer) state(s string) data.StateID {
	id, _ := m.reg.ParseState(s)
	return id
}

// w maps a piece-local position to world coordinates.
func (m *msPlacer) w(lx, h, lz int32) (int32, int32, int32) {
	x, z := m.p.local(lx, lz)
	return x, m.p.box.MinY + h, z
}

func (m *msPlacer) get(lx, h, lz int32) data.StateID {
	x, y, z := m.w(lx, h, lz)
	return m.lv.Block(x, y, z)
}

// inClip reports whether the piece-local position is inside the clip box.
func (m *msPlacer) inClip(lx, h, lz int32) bool {
	x, y, z := m.w(lx, h, lz)
	return m.clip.Contains(x, y, z)
}

// canBeReplaced: the type's planks, log and fence and iron chains are never
// overwritten by placeBlock (MineScene fam1: every other block tried is).
func (m *msPlacer) canBeReplaced(s data.StateID) bool {
	return s != m.mat.planks && s != m.mat.log && m.reg.BlockOf(s) != m.reg.BlockOf(m.mat.chain) && m.reg.BlockOf(s) != m.reg.BlockOf(m.mat.fence)
}

// placeAt is placeBlock in world coordinates.
func (m *msPlacer) placeAt(x, y, z int32, s data.StateID) {
	if !m.clip.Contains(x, y, z) {
		return
	}
	old := m.lv.Block(x, y, z)
	if !m.canBeReplaced(old) {
		return
	}
	if old == m.spawner {
		if r, ok := m.lv.(interface{ RemoveBlockEntity(x, y, z int32) }); ok {
			r.RemoveBlockEntity(x, y, z)
		}
	}
	m.lv.SetBlock(x, y, z, s)
}

func (m *msPlacer) place(lx, h, lz int32, s data.StateID) {
	x, y, z := m.w(lx, h, lz)
	m.placeAt(x, y, z, s)
}

// box is generateBox in piece-local coordinates (h, then lx, then lz).
func (m *msPlacer) box(lx0, h0, lz0, lx1, h1, lz1 int32, s data.StateID) {
	for h := h0; h <= h1; h++ {
		for lx := lx0; lx <= lx1; lx++ {
			for lz := lz0; lz <= lz1; lz++ {
				m.place(lx, h, lz, s)
			}
		}
	}
}

// maybeBox draws for every position, inside the clip or not.
func (m *msPlacer) maybeBox(lx0, h0, lz0, lx1, h1, lz1 int32, p float32, s data.StateID) {
	for h := h0; h <= h1; h++ {
		for lx := lx0; lx <= lx1; lx++ {
			for lz := lz0; lz <= lz1; lz++ {
				if m.float("maybeBox") <= p {
					m.place(lx, h, lz, s)
				}
			}
		}
	}
}

// interior: below the OCEAN_FLOOR_WG height of the column.
func (m *msPlacer) interior(x, y, z int32) bool {
	return y < m.lv.Height("OCEAN_FLOOR_WG", x, z)
}

// face reports whether s has a full face toward dir (0 down, 1 up, 2
// north, 3 south, 4 west, 5 east) from its collision shape; leaves have no
// support faces (MineScene fam1: floors of leaves get planks, of top slabs
// do not).
func (m *msPlacer) face(s data.StateID, dir int) bool {
	if m.reg.IsLeaves(s) {
		return false
	}
	for _, b := range m.reg.Collision(s) {
		full := false
		switch dir {
		case 0:
			full = b.MinY == 0 && b.MinX == 0 && b.MaxX == 1 && b.MinZ == 0 && b.MaxZ == 1
		case 1:
			full = b.MaxY == 1 && b.MinX == 0 && b.MaxX == 1 && b.MinZ == 0 && b.MaxZ == 1
		case 2:
			full = b.MinZ == 0 && b.MinX == 0 && b.MaxX == 1 && b.MinY == 0 && b.MaxY == 1
		case 3:
			full = b.MaxZ == 1 && b.MinX == 0 && b.MaxX == 1 && b.MinY == 0 && b.MaxY == 1
		case 4:
			full = b.MinX == 0 && b.MinZ == 0 && b.MaxZ == 1 && b.MinY == 0 && b.MaxY == 1
		default:
			full = b.MaxX == 1 && b.MinZ == 0 && b.MaxZ == 1 && b.MinY == 0 && b.MaxY == 1
		}
		if full {
			return true
		}
	}
	return false
}

// hangsBelow: the block's bottom face supports a chain at its centre
// (MineScene fam2: full blocks, glass, spawners, fences, chains, chests and
// bottom slabs do; leaves, top slabs, torches, cobwebs and rails do not).
func (m *msPlacer) hangsBelow(s data.StateID) bool {
	if m.reg.IsLeaves(s) {
		return false
	}
	if m.reg.BlockOf(s).Name == "snow" {
		// A snow layer holds a chain although its lowest layer has no
		// collision (MineScene fam5): its shape, not its collision, counts.
		return true
	}
	for _, b := range m.reg.Collision(s) {
		if b.MinY == 0 && b.MinX <= 7.0/16 && b.MaxX >= 9.0/16 && b.MinZ <= 7.0/16 && b.MaxZ >= 9.0/16 {
			return true
		}
	}
	return false
}

// solidRender: an opaque full cube (rails are not drawn over glass, ice or
// spawners; MineScene fam1).
func (m *msPlacer) solidRender(s data.StateID) bool {
	return m.reg.IsFullCube(s) && !m.reg.Transparent(s)
}

// setPlanks puts planks at a world floor position whose top face is not
// sturdy (direct write, protected blocks included).
func (m *msPlacer) setPlanks(x, y, z int32) {
	if !m.clip.Contains(x, y, z) || !m.interior(x, y, z) {
		return
	}
	if !m.face(m.lv.Block(x, y, z), 1) {
		m.lv.SetBlock(x, y, z, m.mat.planks)
	}
}

// liquid: blocks that stop a piece (water, lava and bubble columns at any
// level; waterlogged blocks, seagrass and kelp do not; MineScene liq2).
func (m *msPlacer) liquid(s data.StateID) bool {
	switch m.reg.BlockOf(s).Name {
	case "water", "lava", "bubble_column":
		return true
	}
	return false
}

// invalid: a piece places nothing in a chunk when the biome (zoomed, as
// WorldGenRegion.getBiome) at the centre of its box grown by one and
// clipped to the chunk is mineshaft_blocking, or when a liquid lies on the
// outer faces of its box grown by one and clipped to the chunk (MineScene
// liq1-3: every position of the grown shell rejects, positions inside the
// box do not, and a clipped side's face is the last column in the chunk).
func (m *msPlacer) invalid() bool {
	b, c := m.p.box, m.clip
	x0, x1 := max(b.MinX-1, c.MinX), min(b.MaxX+1, c.MaxX)
	y0, y1 := max(b.MinY-1, c.MinY), min(b.MaxY+1, c.MaxY)
	z0, z1 := max(b.MinZ-1, c.MinZ), min(b.MaxZ+1, c.MaxZ)
	// The biome is read at the centre of the same grown, clipped box
	// ((min+max)/2 truncated; 877/877 MineScene reads).
	cx, cy, cz := (x0+x1)/2, (y0+y1)/2, (z0+z1)/2
	biome := m.lv.Biome(cx, cy, cz)
	if z, ok := m.lv.(interface {
		ZoomedBiome(x, y, z int32) data.BiomeID
	}); ok {
		biome = z.ZoomedBiome(cx, cy, cz)
	}
	if msBlocking()[biome] {
		return true
	}
	for y := y0; y <= y1; y++ {
		for z := z0; z <= z1; z++ {
			for x := x0; x <= x1; x++ {
				if x != x0 && x != x1 && y != y0 && y != y1 && z != z0 && z != z1 {
					x = x1 - 1
					continue
				}
				if m.liquid(m.lv.Block(x, y, z)) {
					return true
				}
			}
		}
	}
	return false
}

func (m *msPlacer) run() {
	if m.invalid() {
		return
	}
	switch m.p.kind {
	case msRoom:
		m.placeRoom()
	case msCorridor:
		m.placeCorridor()
	case msCrossing:
		m.placeCrossing()
	case msStairs:
		m.placeStairs()
	}
}

// stairs carve profile: [min h, max h] by distance from the entrance.
var msStairsCarve = [9][2]int32{{5, 7}, {5, 7}, {4, 7}, {3, 6}, {2, 5}, {1, 4}, {1, 3}, {0, 2}, {0, 2}}

func (m *msPlacer) placeStairs() {
	air := m.state("minecraft:cave_air")
	for lz, hr := range msStairsCarve {
		m.box(0, hr[0], int32(lz), 2, hr[1], int32(lz), air)
	}
}

var msFacing = [4]string{"north", "east", "south", "west"}

// outward is the horizontal direction of local -lx (side 0) or +lx (side 2).
func (m *msPlacer) outward(side int32) string {
	ns := m.p.dir == msNorth || m.p.dir == msSouth
	switch {
	case ns && side == 0:
		return "west"
	case ns:
		return "east"
	case side == 0:
		return "north"
	}
	return "south"
}

func (m *msPlacer) placeCorridor() {
	p := m.p
	air := m.state("minecraft:cave_air")
	l := p.sections * 5
	m.box(0, 0, 0, 2, 1, l-1, air)
	m.maybeBox(0, 2, 0, 2, 2, l-1, 0.8, air)
	web := m.state("minecraft:cobweb")
	if p.spider {
		m.maybeBox(0, 0, 0, 2, 1, l-1, 0.6, web)
	}
	spawned := p.placedSpider
	if !m.persistSpider {
		// Vanilla keeps hasPlacedSpider on the in-memory piece, so which
		// chunk gets the spawner depends on decoration order. kremenmc gives it
		// to the chunk holding the first section's middle (1, 0, 2); other
		// chunks act as if it were placed (no spawner draws).
		x, y, z := m.w(1, 0, 2)
		if !m.clip.Contains(x, y, z) {
			spawned = true
		}
	}
	for i := int32(0); i < p.sections; i++ {
		z := 2 + 5*i
		m.support(z)
		for _, c := range [8][2]int32{{0, z - 1}, {2, z - 1}, {0, z + 1}, {2, z + 1}, {0, z - 2}, {2, z - 2}, {0, z + 2}, {2, z + 2}} {
			chance := float32(0.1)
			if c[1] == z-2 || c[1] == z+2 {
				chance = 0.05
			}
			m.cobweb(c[0], 2, c[1], chance, web)
		}
		if m.intn(100, "chest") == 0 {
			m.chest(2, z-1)
		}
		if m.intn(100, "chest") == 0 {
			m.chest(0, z+1)
		}
		if p.spider && !spawned {
			lz := z - 1 + m.intn(3, "spawner")
			x, y, wz := m.w(1, 0, lz)
			if m.clip.Contains(x, y, wz) && m.interior(x, y, wz) {
				spawned = true
				if m.persistSpider {
					p.placedSpider = true
				}
				m.lv.SetBlock(x, y, wz, m.spawner)
				m.lv.SetBlockEntity(x, y, wz, "minecraft:mob_spawner", nbt.Compound{
					"SpawnData": nbt.Compound{"entity": nbt.Compound{"id": "minecraft:cave_spider"}}})
			}
		}
	}
	for lx := int32(0); lx < 3; lx++ {
		for lz := int32(0); lz < l; lz++ {
			x, y, z := m.w(lx, -1, lz)
			m.setPlanks(x, y, z)
		}
	}
	ends := []int32{2}
	if p.sections > 1 {
		ends = append(ends, 2+5*(p.sections-1))
	}
	for _, z := range ends {
		for _, lx := range []int32{0, 2} {
			x, y, wz := m.w(lx, -1, z)
			if m.clip.Contains(x, y, wz) && m.lv.Block(x, y, wz) == m.mat.planks {
				m.pillarOrChain(x, wz)
			}
		}
	}
	if p.rails {
		rail := m.state("minecraft:rail[shape=north_south]")
		if p.dir == msEast || p.dir == msWest {
			rail = m.state("minecraft:rail[shape=east_west]")
		}
		for lz := int32(0); lz < l; lz++ {
			x, y, z := m.w(1, 0, lz)
			if !m.clip.Contains(x, y, z) || !m.solidRender(m.lv.Block(x, y-1, z)) || !m.interior(x, y-1, z) {
				continue
			}
			if m.float("rail") < 0.7 {
				m.placeAt(x, y, z, rail)
			}
		}
	}
}

// support: fences at h0..1 on both sides (connected outward), the planks
// beam at h2 (sides only when nextInt(4) is 0), and two 0.05 wall torches
// under the beam's middle. Only under a ceiling with no air above the beam.
func (m *msPlacer) support(z int32) {
	if !m.inClip(0, 0, z) || !m.inClip(2, 0, z) {
		return
	}
	for lx := int32(0); lx < 3; lx++ {
		if m.reg.IsAir(m.get(lx, 3, z)) {
			return
		}
	}
	for _, lx := range []int32{0, 2} {
		f, _ := m.reg.With(m.mat.fence, m.outward(lx), "true")
		for h := int32(0); h < 2; h++ {
			m.place(lx, h, z, f)
			if m.inClip(lx, h, z) {
				x, y, wz := m.w(lx, h, z)
				m.lv.MarkPostProcess(x, y, wz)
			}
		}
	}
	if m.intn(4, "support") == 0 {
		m.place(0, 2, z, m.mat.planks)
		m.place(2, 2, z, m.mat.planks)
	} else {
		m.box(0, 2, z, 2, 2, z, m.mat.planks)
		fwd := msFacing[m.p.dir]
		back := msFacing[(m.p.dir+2)%4]
		m.maybeBlock(1, 2, z-1, 0.05, "torch", m.state("minecraft:wall_torch[facing="+back+"]"))
		m.maybeBlock(1, 2, z+1, 0.05, "torch", m.state("minecraft:wall_torch[facing="+fwd+"]"))
	}
}

// maybeBlock draws for positions outside the clip too (MineScene s1).
func (m *msPlacer) maybeBlock(lx, h, lz int32, p float32, label string, s data.StateID) {
	if m.float(label) < p {
		m.place(lx, h, lz, s)
	}
}

// cobweb: inside the clip and below the surface, a draw, then at least two
// sturdy neighbours (down, up, north, south, west, east; neighbours outside
// the clip are skipped).
func (m *msPlacer) cobweb(lx, h, lz int32, chance float32, web data.StateID) {
	x, y, z := m.w(lx, h, lz)
	if !m.clip.Contains(x, y, z) || !m.interior(x, y, z) {
		return
	}
	if m.float("web") >= chance {
		return
	}
	n := 0
	// The neighbour's face toward the web: up for the block below, ...
	for _, d := range [6][4]int32{{0, -1, 0, 1}, {0, 1, 0, 0}, {0, 0, -1, 3}, {0, 0, 1, 2}, {-1, 0, 0, 5}, {1, 0, 0, 4}} {
		nx, ny, nz := x+d[0], y+d[1], z+d[2]
		if !m.clip.Contains(nx, ny, nz) {
			continue
		}
		if m.face(m.lv.Block(nx, ny, nz), int(d[3])) {
			n++
			if n >= 2 {
				m.placeAt(x, y, z, web)
				return
			}
		}
	}
}

// passable: blocks the pillar and chain searches go through (MineScene
// fam4/fam5, 68 block types: air, water at any level, bubble columns, glow
// lichen, seagrass and tall seagrass pass; lava, kelp, vines, grass,
// cobwebs, carpets, rails, torches, chains, fences and the rest stop).
func (m *msPlacer) passable(s data.StateID) bool {
	if m.reg.IsAir(s) {
		return true
	}
	switch m.reg.BlockOf(s).Name {
	case "water", "bubble_column", "glow_lichen", "seagrass", "tall_seagrass":
		return true
	}
	return false
}

// pillarOrChain: from an end support's floor planks, search down and up
// one step at a time, down first; down covers 21 blocks below the floor
// (above the world bottom), up 51 blocks from h0 (MineScene fam3). Each
// search ends at its first non-air block: down, a block with a sturdy top
// gets logs up to the floor; up, a block that can hold a chain gets a fence
// at h0 and chains up to it. A search that ends on an unusable block leaves
// the other one running.
func (m *msPlacer) pillarOrChain(x, z int32) {
	floor := m.p.box.MinY - 1
	h0 := m.p.box.MinY
	minY := m.lv.MinY()
	downOK, upOK := true, true
	for i := int32(0); downOK || upOK; i++ {
		if down := floor - 1 - i; downOK {
			if i >= 21 || down <= minY {
				downOK = false
			} else if s := m.lv.Block(x, down, z); !m.passable(s) {
				if m.face(s, 1) {
					for y := down + 1; y < floor; y++ {
						m.lv.SetBlock(x, y, z, m.mat.log)
					}
					return
				}
				downOK = false
			}
		}
		if up := h0 + i; upOK {
			if i > 50 || up >= m.lv.MaxY() {
				upOK = false
			} else if s := m.lv.Block(x, up, z); !m.passable(s) {
				if m.hangsBelow(s) {
					m.lv.SetBlock(x, h0, z, m.mat.fence)
					for y := h0 + 1; y < up; y++ {
						m.lv.SetBlock(x, y, z, m.mat.chain)
					}
					return
				}
				upOK = false
			}
		}
	}
}

// placeCrossing: a plus-shaped carve on each floor, the 3x3 centre between
// the floors of a two-floored crossing, plank pillars at the four inner
// corners under a non-air ceiling, and floor planks.
func (m *msPlacer) placeCrossing() {
	b := m.p.box
	air := m.state("minecraft:cave_air")
	wbox := func(x0, y0, z0, x1, y1, z1 int32, s data.StateID) {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				for z := z0; z <= z1; z++ {
					m.placeAt(x, y, z, s)
				}
			}
		}
	}
	if m.p.twoFloored {
		wbox(b.MinX+1, b.MinY, b.MinZ, b.MaxX-1, b.MinY+2, b.MaxZ, air)
		wbox(b.MinX, b.MinY, b.MinZ+1, b.MaxX, b.MinY+2, b.MaxZ-1, air)
		wbox(b.MinX+1, b.MaxY-2, b.MinZ, b.MaxX-1, b.MaxY, b.MaxZ, air)
		wbox(b.MinX, b.MaxY-2, b.MinZ+1, b.MaxX, b.MaxY, b.MaxZ-1, air)
		wbox(b.MinX+1, b.MinY+3, b.MinZ+1, b.MaxX-1, b.MinY+3, b.MaxZ-1, air)
	} else {
		wbox(b.MinX+1, b.MinY, b.MinZ, b.MaxX-1, b.MaxY, b.MaxZ, air)
		wbox(b.MinX, b.MinY, b.MinZ+1, b.MaxX, b.MaxY, b.MaxZ-1, air)
	}
	for _, c := range [4][2]int32{{b.MinX + 1, b.MinZ + 1}, {b.MinX + 1, b.MaxZ - 1}, {b.MaxX - 1, b.MinZ + 1}, {b.MaxX - 1, b.MaxZ - 1}} {
		if !m.reg.IsAir(m.lv.Block(c[0], b.MaxY+1, c[1])) {
			wbox(c[0], b.MinY, c[1], c[0], b.MaxY, c[1], m.mat.planks)
		}
	}
	for x := b.MinX; x <= b.MaxX; x++ {
		for z := b.MinZ; z <= b.MaxZ; z++ {
			m.setPlanks(x, b.MinY-1, z)
		}
	}
}

// placeRoom: cave_air at h1..3 over the whole box, the entrance boxes, and
// an upper half ellipsoid from h4 to the top: (dx²+dy²+dz² <= 1.05 with
// dx = (x-(minX+w/2))/(w/2), dy = (y-(minY+4))/(maxY-minY-3)); fitted
// exactly to 2 probe rooms.
func (m *msPlacer) placeRoom() {
	b := m.p.box
	air := m.state("minecraft:cave_air")
	for y := b.MinY + 1; y <= min(b.MinY+3, b.MaxY); y++ {
		for x := b.MinX; x <= b.MaxX; x++ {
			for z := b.MinZ; z <= b.MaxZ; z++ {
				m.placeAt(x, y, z, air)
			}
		}
	}
	for _, e := range m.p.entrances {
		for y := e.MinY; y <= e.MaxY; y++ {
			for x := e.MinX; x <= e.MaxX; x++ {
				for z := e.MinZ; z <= e.MaxZ; z++ {
					m.placeAt(x, y, z, air)
				}
			}
		}
	}
	w, d := float64(b.MaxX-b.MinX+1), float64(b.MaxZ-b.MinZ+1)
	y0 := b.MinY + 4
	h := float64(b.MaxY - y0 + 1)
	for y := y0; y <= b.MaxY; y++ {
		dy := float64(y-y0) / h
		for x := b.MinX; x <= b.MaxX; x++ {
			dx := (float64(x) - (float64(b.MinX) + w/2)) / (w / 2)
			for z := b.MinZ; z <= b.MaxZ; z++ {
				dz := (float64(z) - (float64(b.MinZ) + d/2)) / (d / 2)
				if dx*dx+dy*dy+dz*dz < 1.05 {
					m.placeAt(x, y, z, air)
				}
			}
		}
	}
}

// chest: a chest minecart on a rail at floor level, when the spot is air
// and the block below is not (5 probe cases).
func (m *msPlacer) chest(lx, lz int32) {
	x, y, z := m.w(lx, 0, lz)
	if !m.clip.Contains(x, y, z) || !m.reg.IsAir(m.lv.Block(x, y, z)) || m.reg.IsAir(m.lv.Block(x, y-1, z)) {
		return
	}
	along := m.boolean("chestRail")
	ns := m.p.dir == msNorth || m.p.dir == msSouth
	shape := "east_west"
	if along == ns {
		shape = "north_south"
	}
	m.placeAt(x, y, z, m.state("minecraft:rail[shape="+shape+"]"))
	m.lv.AddEntity(nbt.Compound{
		"id":            "minecraft:chest_minecart",
		"Pos":           nbt.List{Type: nbt.TagDouble, Items: []any{float64(x) + 0.5, float64(y) + 0.5, float64(z) + 0.5}},
		"Rotation":      nbt.List{Type: nbt.TagFloat, Items: []any{float32(0), float32(0)}},
		"LootTable":     "minecraft:chests/abandoned_mineshaft",
		"LootTableSeed": m.long("loot"),
	})
}
