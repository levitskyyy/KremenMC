// Package compare diffs chunk data by name: blocks (normalized state
// strings), biomes per quart cell, heightmaps, block entities and entities.
// Reference chunks come from vanilla region files or stage-oracle dumps;
// kremenmc chunks are converted from world.Chunk. Everything is usable from Go
// tests.
package compare

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
)

// Chunk is the comparable form of one chunk.
type Chunk struct {
	*anvil.RawChunk
	BlockEntities []BlockEntity
	Entities      []Entity
}

type BlockEntity struct {
	X, Y, Z int // absolute
	ID      string
	Data    nbt.Compound // without id/x/y/z/keepPacked
}

type Entity struct {
	ID   string
	Pos  [3]float64
	Data nbt.Compound // without UUID, Pos
}

type Pos struct{ X, Z int32 }

func (p Pos) String() string { return fmt.Sprintf("%d,%d", p.X, p.Z) }

// Set is a chunk collection keyed by position.
type Set map[Pos]*Chunk

// FromRaw wraps a decoded vanilla chunk, extracting its block entities.
func FromRaw(rc *anvil.RawChunk) *Chunk {
	c := &Chunk{RawChunk: rc}
	if rc.Root == nil {
		return c
	}
	{
		for _, it := range Items(rc.Root["block_entities"]) {
			be, ok := it.(nbt.Compound)
			if !ok {
				continue
			}
			b := BlockEntity{ID: str(be["id"]), X: toInt(be["x"]), Y: toInt(be["y"]), Z: toInt(be["z"]), Data: nbt.Compound{}}
			for k, v := range be {
				switch k {
				case "id", "x", "y", "z", "keepPacked":
				default:
					b.Data[k] = v
				}
			}
			c.BlockEntities = append(c.BlockEntities, b)
		}
	}
	return c
}

func str(v any) string { s, _ := v.(string); return s }

func toInt(v any) int {
	switch n := v.(type) {
	case int8:
		return int(n)
	case int16:
		return int(n)
	case int32:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}

// AddEntities attaches entities from a decoded entities/ region chunk.
func (c *Chunk) AddEntities(ents []anvil.RawEntity) {
	for _, e := range ents {
		d := nbt.Compound{}
		for k, v := range e.Root {
			switch k {
			case "UUID", "Pos", "id":
			default:
				d[k] = v
			}
		}
		c.Entities = append(c.Entities, Entity{ID: e.ID, Pos: e.Pos, Data: d})
	}
}

var stateNames sync.Map // data.StateID -> string

func stateName(s data.StateID) string {
	if v, ok := stateNames.Load(s); ok {
		return v.(string)
	}
	reg := data.Canonical()
	raw := reg.StateString(s)
	name, props, _ := strings.Cut(strings.TrimSuffix(raw, "]"), "[")
	n := anvil.StateString(name, func(yield func(k, v string) bool) {
		if props == "" {
			return
		}
		for kv := range strings.SplitSeq(props, ",") {
			k, v, _ := strings.Cut(kv, "=")
			if !yield(k, v) {
				return
			}
		}
	})
	stateNames.Store(s, n)
	return n
}

// Heightmaps kremenmc tracks, and the worldgen variants derived from the same
// predicates (WORLD_SURFACE_WG = non-air, OCEAN_FLOOR_WG = motion-blocking).
var kremenmcHeightmaps = map[string]world.Heightmap{
	"WORLD_SURFACE":             world.WorldSurface,
	"OCEAN_FLOOR":               world.OceanFloor,
	"MOTION_BLOCKING":           world.MotionBlocking,
	"MOTION_BLOCKING_NO_LEAVES": world.MotionBlockingNoLeaves,
	"WORLD_SURFACE_WG":          world.WorldSurface,
	"OCEAN_FLOOR_WG":            world.OceanFloor,
}

// FromWorld converts a kremenmc chunk. The caller must own it or hold its read lock.
func FromWorld(c *world.Chunk, status string) *Chunk {
	reg := data.Canonical()
	minSec := c.Dim.MinSection()
	rc := &anvil.RawChunk{X: c.X, Z: c.Z, DataVersion: anvil.DataVersion, Status: status, MinSection: minSec, Heightmaps: map[string][]int32{}}
	for i := range c.Sections {
		s := &c.Sections[i]
		rs := anvil.RawSection{Y: minSec + i}
		idx := map[data.StateID]uint16{}
		blocks := make([]uint16, 4096)
		for y := range 16 {
			for z := range 16 {
				for x := range 16 {
					st := s.Block(x, y, z)
					j, ok := idx[st]
					if !ok {
						j = uint16(len(rs.BlockPalette))
						idx[st] = j
						rs.BlockPalette = append(rs.BlockPalette, stateName(st))
					}
					blocks[y<<8|z<<4|x] = j
				}
			}
		}
		if len(rs.BlockPalette) > 1 {
			rs.Blocks = blocks
		}
		bidx := map[data.BiomeID]uint8{}
		biomes := make([]uint8, 64)
		for qy := range 4 {
			for qz := range 4 {
				for qx := range 4 {
					b := s.Biome(qx, qy, qz)
					j, ok := bidx[b]
					if !ok {
						j = uint8(len(rs.BiomePalette))
						bidx[b] = j
						rs.BiomePalette = append(rs.BiomePalette, "minecraft:"+reg.BiomeName(b))
					}
					biomes[qy<<4|qz<<2|qx] = j
				}
			}
		}
		if len(rs.BiomePalette) > 1 {
			rs.Biomes = biomes
		}
		rc.Sections = append(rc.Sections, rs)
	}
	for name, h := range kremenmcHeightmaps {
		v := make([]int32, 256)
		for z := range 16 {
			for x := range 16 {
				v[z<<4|x] = int32(c.Height(h, x, z))
			}
		}
		rc.Heightmaps[name] = v
	}
	out := &Chunk{RawChunk: rc}
	for _, be := range c.BlockEntities {
		out.BlockEntities = append(out.BlockEntities, BlockEntity{
			X: int(c.X)*16 + int(be.X), Y: int(be.Y), Z: int(c.Z)*16 + int(be.Z), ID: be.ID, Data: be.Data,
		})
	}
	return out
}

// LoadRegion reads chunks from a vanilla world dimension directory (holding
// region/ and entities/). Missing chunks are reported in the error.
func LoadRegion(dimDir string, ps []Pos) (Set, error) {
	set := Set{}
	var missing []Pos
	ents := map[Pos][]anvil.RawEntity{}
	for _, p := range ps {
		root, err := anvil.ReadRawNBT(filepath.Join(dimDir, "entities"), p.X, p.Z)
		if err != nil {
			return nil, err
		}
		if root != nil {
			x, z, es, err := anvil.DecodeEntities(root)
			if err != nil {
				return nil, err
			}
			ents[Pos{x, z}] = es
		}
	}
	for _, p := range ps {
		root, err := anvil.ReadRawNBT(filepath.Join(dimDir, "region"), p.X, p.Z)
		if err != nil {
			return nil, err
		}
		if root == nil {
			missing = append(missing, p)
			continue
		}
		rc, err := anvil.DecodeRaw(root)
		if err != nil {
			return nil, err
		}
		c := FromRaw(rc)
		c.AddEntities(ents[p])
		set[p] = c
	}
	if len(missing) > 0 {
		return set, fmt.Errorf("compare: %d chunks missing from %s (first %v); run refbench gen first", len(missing), dimDir, missing[0])
	}
	return set, nil
}

// FromRawList builds a set from decoded chunks (e.g. a stage dump).
func FromRawList(rcs []*anvil.RawChunk) Set {
	set := Set{}
	for _, rc := range rcs {
		set[Pos{rc.X, rc.Z}] = FromRaw(rc)
	}
	return set
}

// Canon renders NBT canonically with types (sorted keys), so two values are
// NBT-equal exactly when their renderings are equal.
func Canon(v any) string {
	var b strings.Builder
	canon(&b, v)
	return b.String()
}

func canon(b *strings.Builder, v any) {
	switch x := v.(type) {
	case nbt.Compound:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		b.WriteByte('{')
		for _, k := range keys {
			fmt.Fprintf(b, "%q:", k)
			canon(b, x[k])
			b.WriteByte(',')
		}
		b.WriteByte('}')
	case []nbt.Compound:
		if len(x) == 0 {
			b.WriteString("L0[")
		} else {
			b.WriteString("L10[")
		}
		for _, c := range x {
			canon(b, c)
			b.WriteByte(',')
		}
		b.WriteByte(']')
	case nbt.List:
		t := x.Type
		if len(x.Items) == 0 {
			t = 0
		}
		fmt.Fprintf(b, "L%d[", t)
		for _, it := range x.Items {
			canon(b, it)
			b.WriteByte(',')
		}
		b.WriteByte(']')
	case string:
		fmt.Fprintf(b, "%q", x)
	default:
		fmt.Fprintf(b, "%T(%v)", v, v)
	}
}

// Items returns the elements of an NBT list in either decoded form.
func Items(v any) []any {
	switch l := v.(type) {
	case nbt.List:
		return l.Items
	case []nbt.Compound:
		out := make([]any, len(l))
		for i := range l {
			out[i] = l[i]
		}
		return out
	}
	return nil
}
