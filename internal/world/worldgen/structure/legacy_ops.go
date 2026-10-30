package structure

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Recorded legacy pieces (desert pyramid, jungle temple): the piece's
// block writes in order, in piece-local coordinates, recorded from a
// full-clip run of the reference piece facing north (O=2) through the
// black-box probe kremenmc.probe.temple.TScene and classified by the
// reference call path (TestLegacyOpsGen). Other orientations use the
// legacy frame (swamp hut): north (minX+x, maxZ-z), south (minX+x, minZ+z),
// east (minX+z, minZ+x), west (maxX-z, minZ+x), states mirrored/rotated as
// the swamp hut's. Op kinds:
//
//	S x y z state     place (inside the clip)
//	C x y z state     place unless the block there is air
//	F x y z state     fill the column downward from y (fillsDown)
//	BOOL              region random nextBoolean (drawn in every chunk)
//	B x y z s0 s1     place s0 when the last BOOL was false, else s1
//	R x y z           region random nextFloat (every position, every chunk):
//	                  < 0.33 sandstone, else sand (collapsed roof)
//	K x y z state     chest: placed unless already a chest; LootTableSeed =
//	                  placement random nextLong at its write
//	P x y z           potential suspicious sand position
type legacyOp struct {
	kind    byte
	x, y, z int32
	s0, s1  data.StateID
}

func loadLegacyOps(name string, gz []byte) ([]legacyOp, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("%s ops: %w", name, err)
	}
	reg := data.Canonical()
	var out []legacyOp
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		o := legacyOp{kind: f[0][0]}
		if f[0] == "BOOL" {
			o.kind = 'b'
		}
		if len(f) >= 4 {
			fmt.Sscan(f[1], &o.x)
			fmt.Sscan(f[2], &o.y)
			fmt.Sscan(f[3], &o.z)
		}
		for i, st := range f[min(len(f), 4):] {
			s, ok := reg.ParseState(st)
			if !ok {
				return nil, fmt.Errorf("%s ops: bad state %s", name, st)
			}
			if i == 0 {
				o.s0 = s
			} else {
				o.s1 = s
			}
		}
		out = append(out, o)
	}
	return out, sc.Err()
}

// legacyEnabled lists the recorded legacy structures wired into
// generation (only those verified 1:1 against the reference).
var legacyEnabled = map[string]bool{"minecraft:desert_pyramid": true, "minecraft:jungle_pyramid": true,
	"minecraft:ocean_ruin_cold": true, "minecraft:ocean_ruin_warm": true, "minecraft:stronghold": true}

// legacyFrame maps piece-local coordinates to the world for a saved
// orientation O (0 south, 1 west, 2 north, 3 east).
type legacyFrame struct {
	box Box
	o   int32
	m   Mirror
	rot Rotation
}

func newLegacyFrame(b Box, o int32) legacyFrame {
	f := legacyFrame{box: b, o: o}
	switch o {
	case 0:
		f.m = MirrorLeftRight
	case 3:
		f.rot = Rot90
	case 1:
		f.m, f.rot = MirrorLeftRight, Rot90
	}
	return f
}

func (f legacyFrame) world(x, y, z int32) (int32, int32, int32) {
	b := f.box
	wy := b.MinY + y
	switch f.o {
	case 2:
		return b.MinX + x, wy, b.MaxZ - z
	case 0:
		return b.MinX + x, wy, b.MinZ + z
	case 3:
		return b.MinX + z, wy, b.MinZ + x
	default:
		return b.MaxX - z, wy, b.MinZ + x
	}
}

func (f legacyFrame) state(s data.StateID) data.StateID { return TransformState(s, f.m, f.rot) }

// regionRandom is WorldGenRegion.getRandom of the decorated chunk.
func regionRandom(lv Level, cx, cz int32) rng.Source {
	if r, ok := lv.(interface{ RegionRandom() rng.Source }); ok {
		return r.RegionRandom()
	}
	var seed int64
	if s, ok := lv.(interface{ Seed() int64 }); ok {
		seed = s.Seed()
	}
	f := rng.NewXoroshiro(seed).ForkPositional().FromHashOf("minecraft:worldgen_region_random").ForkPositional()
	return f.At(cx<<4, 0, cz<<4)
}

func levelSeed(lv Level) int64 {
	if s, ok := lv.(interface{ Seed() int64 }); ok {
		return s.Seed()
	}
	return 0
}

// decoIndex is a structure's decoration step and index among all data pack
// structures of that step in key order (as NewStages assigns them).
var decoIndex = sync.OnceValue(func() map[string][2]int32 {
	out := map[string][2]int32{}
	pk, err := wgdata.Load()
	if err != nil {
		return out
	}
	keys := append([]string(nil), pk.Keys("worldgen/structure")...)
	sort.Strings(keys)
	pos := map[int]int32{}
	for _, k := range keys {
		b, _ := pk.JSON("worldgen/structure", k)
		var h struct {
			Step string `json:"step"`
		}
		_ = json.Unmarshal(b, &h)
		st := stepIndex(h.Step)
		out[qualify(k)] = [2]int32{int32(st), pos[st]}
		pos[st]++
	}
	return out
})

// placementRandom is the per-chunk structure placement random of chunk
// cx,cz (stage.go PlaceStep).
func placementRandom(seed int64, key string, cx, cz int32) *rng.WorldgenRandom {
	si := decoIndex()[key]
	r := rng.NewWorldgenRandom(rng.NewXoroshiro(0))
	d := r.SetDecorationSeed(seed, cx<<4, cz<<4)
	r.SetFeatureSeed(d, si[1], si[0])
	return r
}

var replaceableTag = sync.OnceValue(func() map[string]bool {
	pk, err := wgdata.Load()
	if err != nil {
		return nil
	}
	return blockTag(pk, "replaceable")
})

// fillsDown: fillColumnDown passes air and replaceable blocks holding a
// fluid (water, lava, bubble columns, seagrass, tall seagrass); kelp,
// waterlogged blocks, plants, snow and fire stop it (TScene scrambled
// scenes dpx1-3, 3 x 4 chunks).
func fillsDown(reg *data.Registry, s data.StateID) bool {
	if reg.IsAir(s) {
		return true
	}
	b := reg.BlockOf(s)
	return b != nil && reg.HasFluid(s) && replaceableTag()[b.Name]
}

// reorientChest is the chest facing rule of legacy pieces, fitted on the
// black-box kremenmc.probe.temple.Reorient (StructurePiece.reorient on 600
// random neighbour sets: result and read order all identical). Neighbours
// are read north, east, south, west: a chest returns the input unchanged;
// exactly one opaque full cube faces the chest away from it; otherwise,
// starting from the input facing (north), an opaque neighbour turns it to
// the opposite side, then clockwise, then to the opposite again.
func reorientChest(lv Level, reg *data.Registry, x, y, z int32, in string) string {
	type dir struct {
		name   string
		dx, dz int32
	}
	dirs := [4]dir{{"north", 0, -1}, {"east", 1, 0}, {"south", 0, 1}, {"west", -1, 0}}
	at := map[string]dir{}
	for _, d := range dirs {
		at[d.name] = d
	}
	opp := map[string]string{"north": "south", "south": "north", "east": "west", "west": "east"}
	cw := map[string]string{"north": "east", "east": "south", "south": "west", "west": "north"}
	solid := func(d dir) (bool, bool) {
		s := lv.Block(x+d.dx, y, z+d.dz)
		if b := reg.BlockOf(s); b != nil && b.Name == "chest" {
			return false, true
		}
		return reg.IsFullCube(s) && !reg.Transparent(s), false
	}
	found := ""
	for _, d := range dirs {
		sol, chest := solid(d)
		if chest {
			return in
		}
		if sol {
			if found != "" {
				found = ""
				goto turn
			}
			found = d.name
		}
	}
	if found != "" {
		return opp[found]
	}
turn:
	f := in
	for step := 0; step < 3; step++ {
		if sol, _ := solid(at[f]); sol {
			if step == 1 {
				f = cw[f]
			} else {
				f = opp[f]
			}
		}
	}
	return f
}
