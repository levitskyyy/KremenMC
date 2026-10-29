package anvil

import (
	"errors"
	"fmt"
	"math/bits"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
)

// RawChunk is chunk NBT decoded by name, without mapping states or biomes to
// kremenmc registry IDs, so any vanilla chunk (proto-chunks included) can be
// inspected losslessly.
type RawChunk struct {
	X, Z        int32
	DataVersion int32
	Status      string // e.g. "minecraft:full", "minecraft:terrain"
	MinSection  int    // yPos
	Sections    []RawSection
	// Heightmaps by vanilla type name; 256 absolute y values (first free
	// block above the column), index z*16+x.
	Heightmaps map[string][]int32
	Root       nbt.Compound
}

// RawSection holds one 16^3 section. Block states are normalized strings
// "minecraft:name[k=v,...]" with all properties, sorted by key.
type RawSection struct {
	Y            int
	BlockPalette []string
	Blocks       []uint16 // 4096 palette indices, (y*16+z)*16+x; nil when the palette has one entry
	BiomePalette []string
	Biomes       []uint8 // 64 palette indices, (y*4+z)*4+x; nil when the palette has one entry
}

// Block returns the normalized state at section-local coordinates, "" when
// the section has no block data.
func (s *RawSection) Block(x, y, z int) string {
	if len(s.BlockPalette) == 0 {
		return ""
	}
	if s.Blocks == nil {
		return s.BlockPalette[0]
	}
	return s.BlockPalette[s.Blocks[(y<<8)|(z<<4)|x]]
}

// Biome returns the biome name at section-local quart coordinates.
func (s *RawSection) Biome(qx, qy, qz int) string {
	if len(s.BiomePalette) == 0 {
		return ""
	}
	if s.Biomes == nil {
		return s.BiomePalette[0]
	}
	return s.BiomePalette[s.Biomes[(qy<<4)|(qz<<2)|qx]]
}

// Section returns the section with the given section Y, or nil.
func (c *RawChunk) Section(sy int) *RawSection {
	i := sy - c.MinSection
	if i >= 0 && i < len(c.Sections) && c.Sections[i].Y == sy {
		return &c.Sections[i]
	}
	for j := range c.Sections {
		if c.Sections[j].Y == sy {
			return &c.Sections[j]
		}
	}
	return nil
}

// Block returns the state at chunk-local x,z and absolute y ("" if absent).
func (c *RawChunk) Block(x, y, z int) string {
	s := c.Section(y >> 4)
	if s == nil {
		return ""
	}
	return s.Block(x, y&15, z)
}

// DecodeRaw decodes vanilla chunk NBT (26.3 and the 1.18+ layout).
func DecodeRaw(root nbt.Compound) (*RawChunk, error) {
	x, ok1 := root["xPos"].(int32)
	z, ok2 := root["zPos"].(int32)
	if !ok1 || !ok2 {
		return nil, errors.New("anvil: chunk missing xPos/zPos")
	}
	c := &RawChunk{X: x, Z: z, DataVersion: asInt(root["DataVersion"]), MinSection: int(asInt(root["yPos"])), Root: root}
	c.Status, _ = root["Status"].(string)
	if c.Status == "" {
		c.Status, _ = root["status"].(string)
	}
	if c.Status != "" && !strings.Contains(c.Status, ":") {
		c.Status = "minecraft:" + c.Status
	}
	vals := make([]uint32, 4096)
	for _, it := range listItems(root["sections"]) {
		sec, ok := it.(nbt.Compound)
		if !ok {
			continue
		}
		rs := RawSection{Y: int(asInt(sec["Y"]))}
		if bs, ok := sec["block_states"].(nbt.Compound); ok {
			pal := listItems(bs["palette"])
			rs.BlockPalette = make([]string, len(pal))
			for i, p := range pal {
				rs.BlockPalette[i] = RawStateString(p)
			}
			idx, err := unpackRaw(len(pal), bs["data"], 4, vals)
			if err != nil {
				return nil, fmt.Errorf("anvil: chunk %d,%d section %d blocks: %w", x, z, rs.Y, err)
			}
			if idx != nil {
				rs.Blocks = make([]uint16, 4096)
				for i, v := range idx {
					rs.Blocks[i] = uint16(v)
				}
			}
		}
		if bm, ok := sec["biomes"].(nbt.Compound); ok {
			pal := listItems(bm["palette"])
			rs.BiomePalette = make([]string, len(pal))
			for i, p := range pal {
				rs.BiomePalette[i], _ = p.(string)
			}
			idx, err := unpackRaw(len(pal), bm["data"], 0, vals[:64])
			if err != nil {
				return nil, fmt.Errorf("anvil: chunk %d,%d section %d biomes: %w", x, z, rs.Y, err)
			}
			if idx != nil {
				rs.Biomes = make([]uint8, 64)
				for i, v := range idx {
					rs.Biomes[i] = uint8(v)
				}
			}
		}
		c.Sections = append(c.Sections, rs)
	}
	sort.Slice(c.Sections, func(i, j int) bool { return c.Sections[i].Y < c.Sections[j].Y })
	if hm, ok := root["Heightmaps"].(nbt.Compound); ok {
		c.Heightmaps = map[string][]int32{}
		minY := int32(c.MinSection * 16)
		for name, v := range hm {
			longs, _ := v.([]int64)
			hs, err := unpackHeightmap(longs)
			if err != nil {
				return nil, fmt.Errorf("anvil: chunk %d,%d heightmap %s: %w", x, z, name, err)
			}
			for i := range hs {
				hs[i] += minY
			}
			c.Heightmaps[name] = hs
		}
	}
	return c, nil
}

// RawStateString normalizes a palette entry ({Name, Properties} or the 26.3
// {id, properties} form, or a bare string) to "minecraft:name[k=v,...]".
func RawStateString(p any) string {
	var name string
	var props nbt.Compound
	switch e := p.(type) {
	case string:
		name = e
	case nbt.Compound:
		var ok bool
		if name, ok = e["Name"].(string); !ok {
			if name, ok = e["id"].(string); !ok {
				name, _ = e[""].(string)
			}
		}
		if props, ok = e["Properties"].(nbt.Compound); !ok {
			props, _ = e["properties"].(nbt.Compound)
		}
	}
	if len(props) == 0 {
		return defaultStateString(name)
	}
	return StateString(name, func(yield func(k, v string) bool) {
		for k, v := range props {
			s, _ := v.(string)
			if !yield(k, s) {
				return
			}
		}
	})
}

var defaultStates sync.Map // name -> normalized default state

// defaultStateString expands a bare block name: 26.3 writes default states
// without properties. Defaults come from the canonical kremenmc registry; unknown
// blocks stay bare.
func defaultStateString(name string) string {
	if v, ok := defaultStates.Load(name); ok {
		return v.(string)
	}
	out := StateString(name, nil)
	reg := data.Canonical()
	if st, ok := reg.DefaultState(name); ok {
		full := reg.StateString(st)
		if _, props, ok := strings.Cut(strings.TrimSuffix(full, "]"), "["); ok {
			out = StateString(name, func(yield func(k, v string) bool) {
				for kv := range strings.SplitSeq(props, ",") {
					k, v, _ := strings.Cut(kv, "=")
					if !yield(k, v) {
						return
					}
				}
			})
		}
	}
	defaultStates.Store(name, out)
	return out
}

// StateString builds the normalized form from a name and its properties in
// any order.
func StateString(name string, props func(yield func(k, v string) bool)) string {
	if name != "" && !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	var kv []string
	if props != nil {
		for k, v := range props {
			kv = append(kv, k+"="+v)
		}
	}
	if len(kv) == 0 {
		return name
	}
	slices.Sort(kv)
	return name + "[" + strings.Join(kv, ",") + "]"
}

// unpackRaw returns palette indices, or nil for single-entry palettes.
func unpackRaw(palLen int, raw any, minBits int, vals []uint32) ([]uint32, error) {
	if palLen <= 1 {
		return nil, nil
	}
	longs, _ := raw.([]int64)
	b := max(minBits, bits.Len(uint(palLen-1)))
	per := 64 / b
	if need := (len(vals) + per - 1) / per; len(longs) < need {
		return nil, fmt.Errorf("data has %d longs, need %d", len(longs), need)
	}
	mask := uint64(1)<<b - 1
	for i := range vals {
		v := uint32(uint64(longs[i/per]) >> (uint(i%per) * uint(b)) & mask)
		if int(v) >= palLen {
			return nil, fmt.Errorf("palette index %d out of %d", v, palLen)
		}
		vals[i] = v
	}
	return vals, nil
}

// unpackHeightmap infers the entry width from the array length (vanilla
// uses ceil(log2(height+1)) bits without spanning longs).
func unpackHeightmap(longs []int64) ([]int32, error) {
	for b := 1; b <= 32; b++ {
		per := 64 / b
		if (256+per-1)/per != len(longs) {
			continue
		}
		out := make([]int32, 256)
		mask := uint64(1)<<b - 1
		for i := range out {
			out[i] = int32(uint64(longs[i/per]) >> (uint(i%per) * uint(b)) & mask)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%d longs fit no entry width", len(longs))
}

// RawEntity is one entity from an entities/ region chunk.
type RawEntity struct {
	ID   string
	Pos  [3]float64
	Root nbt.Compound
}

// DecodeEntities decodes an entities/ region chunk ({Position, Entities}).
func DecodeEntities(root nbt.Compound) (x, z int32, ents []RawEntity, err error) {
	pos, ok := root["Position"].([]int32)
	if !ok || len(pos) != 2 {
		return 0, 0, nil, errors.New("anvil: entity chunk missing Position")
	}
	for _, it := range listItems(root["Entities"]) {
		e, ok := it.(nbt.Compound)
		if !ok {
			continue
		}
		re := RawEntity{Root: e}
		re.ID, _ = e["id"].(string)
		for i, v := range listItems(e["Pos"]) {
			if f, ok := v.(float64); ok && i < 3 {
				re.Pos[i] = f
			}
		}
		ents = append(ents, re)
	}
	return pos[0], pos[1], ents, nil
}

// ReadRawNBT reads one chunk's NBT from the region files in dir without
// writing to them. It returns nil, nil when the chunk is absent.
func ReadRawNBT(dir string, x, z int32) (nbt.Compound, error) {
	r, err := OpenRegionReadOnly(dir, x>>5, z>>5)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := r.Read(int(x&31), int(z&31))
	if err != nil || b == nil {
		return nil, err
	}
	_, root, _, err := nbt.Unmarshal(b, false)
	if err != nil {
		return nil, fmt.Errorf("anvil: chunk %d,%d: %w", x, z, err)
	}
	return root, nil
}

// ListChunks returns the coordinates of every chunk stored in the region
// files of dir.
func ListChunks(dir string) ([][2]int32, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out [][2]int32
	for _, e := range ents {
		var rx, rz int32
		if n, _ := fmt.Sscanf(e.Name(), "r.%d.%d.mca", &rx, &rz); n != 2 || e.Name() != fmt.Sprintf("r.%d.%d.mca", rx, rz) {
			continue
		}
		r, err := OpenRegionReadOnly(dir, rx, rz)
		if err != nil {
			return nil, err
		}
		for lz := range 32 {
			for lx := range 32 {
				if r.Has(lx, lz) {
					out = append(out, [2]int32{rx<<5 + int32(lx), rz<<5 + int32(lz)})
				}
			}
		}
		r.Close()
	}
	return out, nil
}
