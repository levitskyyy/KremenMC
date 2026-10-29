package spawn

import (
	_ "embed"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// The refdata files are probe outputs captured from the reference server
// (see tools/refprobe/java/kremenmc/probe/spawn and tables_test.go). They are
// embedded verbatim and parsed once at init.

//go:embed refdata/static.txt
var refStatic string

//go:embed refdata/entities.txt
var refEntities string

//go:embed refdata/blocks.txt
var refBlocks string

// Placement is the vanilla SpawnPlacementType of an entity type.
type Placement uint8

const (
	PlacementNone Placement = iota // no_restrictions
	PlacementInWater
	PlacementInLava
	PlacementOnGround
)

// Heightmap names the heightmap kinds the spawn engine reads.
type Heightmap uint8

const (
	HeightmapWorldSurface Heightmap = iota
	HeightmapMotionBlocking
	HeightmapMotionBlockingNoLeaves
	HeightmapOceanFloor
)

var heightmapNames = [...]string{"WORLD_SURFACE", "MOTION_BLOCKING", "MOTION_BLOCKING_NO_LEAVES", "OCEAN_FLOOR"}

func (h Heightmap) String() string { return heightmapNames[h] }

// EntityType holds reference-extracted spawn properties of one entity type.
// Kind is the registry ID (protocol 777 numbering).
type EntityType struct {
	Kind          int32
	Name          string
	Category      Category
	Summonable    bool
	FarFromPlayer bool // canSpawnFarFromPlayer
	FireImmune    bool
	Peaceful      bool // isAllowedInPeaceful
	Width, Height float32
	Registered    bool // has a SpawnPlacements entry
	Placement     Placement
	Heightmap     Heightmap

	Mob           bool
	ClusterSize   int    // getMaxSpawnClusterSize
	GroupLimit    int    // smallest n with isMaxGroupSizeReached(n), 0 if never
	FarAway       bool   // removeWhenFarAway for a fresh, unnamed instance
	CustomPersist bool   // requiresCustomPersistence for a fresh instance
	Chain         string // reference class chain, most derived first
	// Declarer maps spawn-related method names to the most derived
	// reference class declaring them (signature-level reflection).
	Declarer map[string]string

	danger     int    // index into dangerSets
	spawnMask  uint32 // bit c set when blocks of spawn class c accept this type
	classFlags uint32
}

const (
	classMonster uint32 = 1 << iota
	classAnimal
	classWaterAnimal
	classAbstractFish
	classPathfinder
	classAgeable
	classAbstractHorse
	classAgeableWaterCreature
)

var classFlagNames = map[string]uint32{
	"Monster": classMonster, "Animal": classAnimal, "WaterAnimal": classWaterAnimal,
	"AbstractFish": classAbstractFish, "PathfinderMob": classPathfinder, "AgeableMob": classAgeable,
	"AbstractHorse": classAbstractHorse, "AgeableWaterCreature": classAgeableWaterCreature,
}

func (t *EntityType) is(f uint32) bool { return t.classFlags&f != 0 }

// SpawnAABB is EntityType.getSpawnAABB(x, y, z).
func (t *EntityType) SpawnAABB(x, y, z float64) AABB {
	hw := float64(t.Width) / 2
	return AABB{x - hw, y, z - hw, x + hw, y + float64(t.Height), z + hw}
}

type stateRange struct{ lo, hi data.StateID }

var (
	entityTypes  []*EntityType
	entityByName = map[string]*EntityType{}
	dangerSets   [][]stateRange
	blockTable   []blockProps
	blockFirst   = map[string]data.StateID{}
	blockCount   = map[string]uint32{}
	blockNames   []string // by state ID
)

// blockProps packs per-state properties: bits 0-3 conductor, full collision
// cube, signal source, air; 4-7 light emission; 8-10 fluid kind; 11-14 fluid
// amount; 15 fluid source; 16-23 spawn class; 24-27 light dampening; 28
// liquid block; 29 pathfindable on land.
type blockProps uint32

const (
	bpConductor blockProps = 1 << iota
	bpFullCollision
	bpSignal
	bpAir
	bpPathLand blockProps = 1 << 29
)

// Fluid kinds.
const (
	FluidNone uint8 = iota
	FluidWater
	FluidFlowingWater
	FluidLava
	FluidFlowingLava
)

func (p blockProps) light() int      { return int(p>>4) & 15 }
func (p blockProps) fluid() uint8    { return uint8(p>>8) & 7 }
func (p blockProps) amount() int     { return int(p>>11) & 15 }
func (p blockProps) source() bool    { return p&(1<<15) != 0 }
func (p blockProps) spawnClass() int { return int(p>>16) & 0xff }
func (p blockProps) lightBlock() int { return int(p>>24) & 15 }
func (p blockProps) liquid() bool    { return p&(1<<28) != 0 }
func (p blockProps) isWater() bool   { f := p.fluid(); return f == FluidWater || f == FluidFlowingWater }
func (p blockProps) isLava() bool    { f := p.fluid(); return f == FluidLava || f == FluidFlowingLava }

// EntityByName returns the entity type for a namespaced key.
func EntityByName(name string) (*EntityType, bool) {
	t, ok := entityByName[qualify(name)]
	return t, ok
}

// EntityByKind returns the entity type for a registry ID.
func EntityByKind(k int32) *EntityType {
	if k < 0 || int(k) >= len(entityTypes) {
		return nil
	}
	return entityTypes[k]
}

// EntityTypes lists all entity types in registry order.
func EntityTypes() []*EntityType { return entityTypes }

func qualify(k string) string {
	if strings.IndexByte(k, ':') < 0 {
		return "minecraft:" + k
	}
	return k
}

func props(s data.StateID) blockProps {
	if int(s) < len(blockTable) {
		return blockTable[s]
	}
	return bpAir
}

// BlockName returns the block key of a state.
func BlockName(s data.StateID) string {
	if int(s) < len(blockNames) {
		return blockNames[s]
	}
	return "minecraft:air"
}

// StateRange returns the first state ID and state count of a block.
func StateRange(block string) (data.StateID, uint32, bool) {
	first, ok := blockFirst[qualify(block)]
	return first, blockCount[qualify(block)], ok
}

// Parsed during package variable initialization so init funcs in other
// files can use the tables.
var _ = func() bool {
	if err := parseTables(); err != nil {
		panic(fmt.Errorf("spawn: embedded tables: %w", err))
	}
	return true
}()

func kv(fields []string) map[string]string {
	m := make(map[string]string, len(fields))
	for _, f := range fields {
		if k, v, ok := strings.Cut(f, "="); ok {
			m[k] = v
		}
	}
	return m
}

func hexFloat(s string) (float32, error) {
	u, err := strconv.ParseUint(strings.TrimPrefix(s, "f"), 16, 32)
	return math.Float32frombits(uint32(u)), err
}

func hexDouble(s string) (float64, error) {
	u, err := strconv.ParseUint(strings.TrimPrefix(s, "d"), 16, 64)
	return math.Float64frombits(u), err
}

func parseTables() error {
	for _, line := range strings.Split(refStatic, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "entity" {
			continue
		}
		id, _ := strconv.Atoi(f[1])
		m := kv(f[3:])
		cat, ok := CategoryByName(m["cat"])
		if !ok {
			return fmt.Errorf("category %q", m["cat"])
		}
		w, err := hexFloat(m["w"])
		if err != nil {
			return err
		}
		h, err := hexFloat(m["h"])
		if err != nil {
			return err
		}
		t := &EntityType{
			Kind: int32(id), Name: f[2], Category: cat,
			Summonable: m["summon"] == "true", FarFromPlayer: m["far"] == "true", FireImmune: m["fireImmune"] == "true",
			Peaceful: m["peaceful"] == "true", Width: w, Height: h, Registered: m["registered"] == "true",
		}
		switch m["placement"] {
		case "in_water":
			t.Placement = PlacementInWater
		case "in_lava":
			t.Placement = PlacementInLava
		case "on_ground":
			t.Placement = PlacementOnGround
		}
		for i, n := range heightmapNames {
			if m["heightmap"] == n {
				t.Heightmap = Heightmap(i)
			}
		}
		if id != len(entityTypes) {
			return fmt.Errorf("entity ids not dense at %d", id)
		}
		entityTypes = append(entityTypes, t)
		entityByName[t.Name] = t
	}
	for _, line := range strings.Split(refEntities, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "mob":
			t := entityByName[f[1]]
			if t == nil {
				return fmt.Errorf("mob %s unknown", f[1])
			}
			m := kv(f[2:])
			t.Mob = true
			t.ClusterSize, _ = strconv.Atoi(m["cluster"])
			if g := m["groupReached"]; g != "none" {
				first, _, _ := strings.Cut(g, ",")
				t.GroupLimit, _ = strconv.Atoi(first)
			}
			t.FarAway = m["farAway0"] == "true"
			t.CustomPersist = m["customPersist"] == "true"
			t.Chain = m["chain"]
			for _, c := range strings.Split(t.Chain, ">") {
				t.classFlags |= classFlagNames[c]
			}
		case "overrides":
			t := entityByName[f[1]]
			if t == nil {
				return fmt.Errorf("overrides %s unknown", f[1])
			}
			t.Declarer = map[string]string{}
			for _, o := range f[2:] {
				if k, v, ok := strings.Cut(o, "@"); ok {
					t.Declarer[k] = v
				}
			}
		case "danger":
			m := kv(f[2:])
			for _, n := range strings.Split(m["types"], ",") {
				if t := entityByName[n]; t != nil {
					t.danger = len(dangerSets)
				}
			}
			dangerSets = append(dangerSets, nil)
		case "dangerstates":
			var rs []stateRange
			for _, r := range f[1:] {
				lo, hi, _ := strings.Cut(r, "-")
				a, _ := strconv.Atoi(lo)
				b, _ := strconv.Atoi(hi)
				rs = append(rs, stateRange{data.StateID(a), data.StateID(b)})
			}
			dangerSets[len(dangerSets)-1] = rs
		}
	}
	for _, line := range strings.Split(refBlocks, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "spawnclass":
			c, _ := strconv.Atoi(f[1])
			for _, n := range f[2:] {
				if t := entityByName[n]; t != nil {
					t.spawnMask |= 1 << c
				}
			}
		case "b":
			first, _ := strconv.Atoi(f[2])
			n, _ := strconv.Atoi(f[3])
			bar := slicesIndex(f, "|")
			if bar < 0 || first != len(blockTable) {
				return fmt.Errorf("block %s malformed or out of order", f[1])
			}
			var pal []blockProps
			for _, p := range f[5:bar] {
				bp, err := parseBlockProps(p)
				if err != nil {
					return fmt.Errorf("block %s: %w", f[1], err)
				}
				pal = append(pal, bp)
			}
			for _, r := range f[bar+1:] {
				i, c, _ := strings.Cut(r, "*")
				ii, _ := strconv.Atoi(i)
				cc, _ := strconv.Atoi(c)
				for range cc {
					blockTable = append(blockTable, pal[ii])
					blockNames = append(blockNames, f[1])
				}
			}
			if len(blockTable) != first+n {
				return fmt.Errorf("block %s: state count", f[1])
			}
			blockFirst[f[1]] = data.StateID(first)
			blockCount[f[1]] = uint32(n)
		}
	}
	if len(entityTypes) == 0 || len(blockTable) == 0 {
		return fmt.Errorf("empty")
	}
	return nil
}

func slicesIndex(f []string, s string) int {
	for i, v := range f {
		if v == s {
			return i
		}
	}
	return -1
}

func parseBlockProps(s string) (blockProps, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 5 || len(parts[0]) != 6 {
		return 0, fmt.Errorf("props %q", s)
	}
	var p blockProps
	for i := range 4 {
		if parts[0][i] == '1' {
			p |= 1 << i
		}
	}
	if parts[0][4] == '1' {
		p |= 1 << 28
	}
	if parts[0][5] == '1' {
		p |= bpPathLand
	}
	light, _ := strconv.Atoi(parts[1])
	p |= blockProps(light) << 4
	damp, _ := strconv.Atoi(parts[2])
	p |= blockProps(damp) << 24
	if parts[3] != "-" {
		fl := strings.Split(parts[3], ":")
		var kind uint8
		switch fl[0] {
		case "water":
			kind = FluidWater
		case "flowing_water":
			kind = FluidFlowingWater
		case "lava":
			kind = FluidLava
		case "flowing_lava":
			kind = FluidFlowingLava
		default:
			return 0, fmt.Errorf("fluid %q", fl[0])
		}
		amt, _ := strconv.Atoi(fl[1])
		p |= blockProps(kind)<<8 | blockProps(amt)<<11
		if fl[2] == "1" {
			p |= 1 << 15
		}
	}
	cls, _ := strconv.Atoi(parts[4])
	p |= blockProps(cls) << 16
	return p, nil
}

func isDangerous(t *EntityType, s data.StateID) bool {
	for _, r := range dangerSets[t.danger] {
		if s >= r.lo && s <= r.hi {
			return true
		}
	}
	return false
}

// validSpawnOn is BlockState.isValidSpawn(level, pos, type) for the block
// below a spawn position.
func validSpawnOn(s data.StateID, t *EntityType) bool {
	return t.spawnMask&(1<<props(s).spawnClass()) != 0
}
