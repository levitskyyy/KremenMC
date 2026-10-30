package anvil

import (
	"fmt"
	"math/bits"
	"slices"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// DataVersion written to saved chunks (Java Edition 26.3).
const DataVersion = 5023

var handledKeys = map[string]bool{
	"DataVersion": true, "xPos": true, "zPos": true, "yPos": true,
	"Status": true, "status": true, "sections": true, "block_entities": true,
	"Heightmaps": true, "isLightOn": true, "LastUpdate": true, "InhabitedTime": true,
	"block_ticks": true, "fluid_ticks": true, genEntitiesKey: true,
}

// genEntitiesKey keeps world.Chunk.GenEntities (generated entities not yet
// added to the live world) in a full chunk; vanilla ignores it.
const genEntitiesKey = "kremenmc:gen_entities"

// LoadStats reports lossy conversions while decoding a chunk.
type LoadStats struct {
	UnknownBlocks int
	UnknownBiomes int
}

// DecodeChunk converts chunk NBT into a Chunk. It returns nil, nil for
// proto-chunks (status other than full), which callers should regenerate.
func DecodeChunk(root nbt.Compound, dim world.Dimension) (*world.Chunk, LoadStats, error) {
	status, _ := root["Status"].(string)
	if status == "" {
		status, _ = root["status"].(string)
	}
	if status != "" && status != "minecraft:full" && status != "full" {
		return nil, LoadStats{}, nil
	}
	return decodeChunk(root, dim, false)
}

// decodeChunk decodes the parts shared by full and proto chunks. Proto
// chunks keep block entities' keepPacked flag.
func decodeChunk(root nbt.Compound, dim world.Dimension, proto bool) (*world.Chunk, LoadStats, error) {
	var st LoadStats
	x, ok1 := root["xPos"].(int32)
	z, ok2 := root["zPos"].(int32)
	if !ok1 || !ok2 {
		return nil, st, fmt.Errorf("anvil: chunk missing xPos/zPos")
	}
	reg := data.Canonical()
	plains, _ := reg.BiomeByName("plains")
	c := world.NewChunk(x, z, dim, plains)
	minSec := dim.MinSection()
	lightOn := asBool(root["isLightOn"])
	var skyPresent, blockPresent []bool
	skyPresent = make([]bool, len(c.SkyLight))
	blockPresent = make([]bool, len(c.BlockLight))

	vals := make([]uint32, 4096)
	for _, it := range listItems(root["sections"]) {
		sec, ok := it.(nbt.Compound)
		if !ok {
			continue
		}
		y := int(asInt(sec["Y"]))
		li := y - minSec + 1
		if li < 0 || li >= len(c.SkyLight) {
			continue
		}
		// All-zero arrays decode to the zero value, as kremenmc keeps them.
		if b, ok := sec["SkyLight"].([]byte); ok {
			if n, ok := world.NibblesFrom(b); ok {
				if n.IsZero() {
					n = world.Nibbles{}
				}
				c.SkyLight[li], skyPresent[li] = n, true
			}
		}
		if b, ok := sec["BlockLight"].([]byte); ok {
			if n, ok := world.NibblesFrom(b); ok {
				if n.IsZero() {
					n = world.Nibbles{}
				}
				c.BlockLight[li], blockPresent[li] = n, true
			}
		}
		si := y - minSec
		if si < 0 || si >= len(c.Sections) {
			continue
		}
		s := &c.Sections[si]
		if bs, ok := sec["block_states"].(nbt.Compound); ok {
			pal := listItems(bs["palette"])
			ids := make([]uint32, len(pal))
			for i, p := range pal {
				id, ok := paletteState(reg, p)
				if !ok {
					st.UnknownBlocks++
				}
				ids[i] = uint32(id)
			}
			if err := loadPaletted(&s.Blocks, ids, bs["data"], 4, vals); err != nil {
				return nil, st, fmt.Errorf("anvil: chunk %d,%d section %d blocks: %w", x, z, y, err)
			}
		}
		if bm, ok := sec["biomes"].(nbt.Compound); ok {
			pal := listItems(bm["palette"])
			ids := make([]uint32, len(pal))
			for i, p := range pal {
				name, _ := p.(string)
				id, ok := reg.BiomeByName(name)
				if !ok {
					st.UnknownBiomes++
					id = plains
				}
				ids[i] = uint32(id)
			}
			if err := loadPaletted(&s.Biomes, ids, bm["data"], 0, vals[:64]); err != nil {
				return nil, st, fmt.Errorf("anvil: chunk %d,%d section %d biomes: %w", x, z, y, err)
			}
		}
	}
	for i := range c.Sections {
		c.Sections[i].Recount()
	}
	c.RecalcHeightmaps()

	if lightOn {
		// vanilla omits sky arrays that equal the bottom layer of the section above
		top := len(c.SkyLight) - 1
		if dim.HasSkylight {
			if !skyPresent[top] {
				c.SkyLight[top] = world.FullNibbles()
			}
			for i := top - 1; i >= 0; i-- {
				if !skyPresent[i] {
					c.SkyLight[i] = extendDown(c.SkyLight[i+1])
				}
			}
		}
		c.LightReady = true
	}

	for _, it := range listItems(root["block_entities"]) {
		be, ok := it.(nbt.Compound)
		if !ok {
			continue
		}
		id, _ := be["id"].(string)
		bx, by, bz := asInt(be["x"]), asInt(be["y"]), asInt(be["z"])
		d := nbt.Compound{}
		for k, v := range be {
			switch k {
			case "id", "x", "y", "z":
			case "keepPacked":
				if proto {
					d[k] = v
				}
			default:
				d[k] = v
			}
		}
		c.BlockEntities = append(c.BlockEntities, world.BlockEntity{
			X: uint8(bx & 15), Z: uint8(bz & 15), Y: by, NetType: world.BlockEntityNetType(id), ID: id, Data: d,
		})
	}
	c.InhabitedTime = asInt64(root["InhabitedTime"])
	if !proto {
		c.PendingTicks = pendingFromDisk(root)
		for _, it := range listItems(root[genEntitiesKey]) {
			if e, ok := it.(nbt.Compound); ok {
				c.GenEntities = append(c.GenEntities, e)
			}
		}
	}
	for k, v := range root {
		if !handledKeys[k] {
			if c.Extra == nil {
				c.Extra = nbt.Compound{}
			}
			c.Extra[k] = v
		}
	}
	return c, st, nil
}

func extendDown(above world.Nibbles) world.Nibbles {
	if above.IsFull() || above.IsZero() {
		return above
	}
	var n world.Nibbles
	for i := range 256 {
		v := above.Get(i)
		for y := range 16 {
			n.Set(y<<8|i, v)
		}
	}
	return n
}

// paletteState accepts both pre-26.3 {Name, Properties} and 26.3 {id, properties},
// bare strings and {"": name} entries.
func paletteState(reg *data.Registry, p any) (data.StateID, bool) {
	switch e := p.(type) {
	case string:
		return reg.DefaultState(e)
	case nbt.Compound:
		name, ok := e["id"].(string)
		if !ok {
			if name, ok = e["Name"].(string); !ok {
				name, _ = e[""].(string)
			}
		}
		props, _ := e["properties"].(nbt.Compound)
		if props == nil {
			props, _ = e["Properties"].(nbt.Compound)
		}
		return reg.StateFrom(name, func(k string) (string, bool) {
			v, ok := props[k].(string)
			return v, ok
		})
	}
	return 0, false
}

// loadPaletted unpacks disk data (min width minBits, no-span layout) into c.
func loadPaletted(c *world.Container, pal []uint32, raw any, minBits int, vals []uint32) error {
	if len(pal) == 0 {
		return nil
	}
	if len(pal) == 1 {
		c.Fill(pal[0])
		return nil
	}
	longs, _ := raw.([]int64)
	b := max(minBits, bits.Len(uint(len(pal)-1)))
	need := (len(vals) + 64/b - 1) / (64 / b)
	if len(longs) < need {
		return fmt.Errorf("data has %d longs, need %d", len(longs), need)
	}
	u := make([]uint64, len(longs))
	for i, l := range longs {
		u[i] = uint64(l)
	}
	world.UnpackIndices(vals, u, uint8(b))
	for i, v := range vals {
		if int(v) >= len(pal) {
			return fmt.Errorf("palette index %d out of %d", v, len(pal))
		}
		vals[i] = pal[v]
	}
	c.SetAll(vals)
	return nil
}

// EncodeChunk converts a Chunk into vanilla 26.3 chunk NBT. The caller must
// own the chunk or hold its read lock.
func EncodeChunk(c *world.Chunk, gameTime int64) nbt.Compound {
	return encodeChunk(c, gameTime, "minecraft:full")
}

func encodeChunk(c *world.Chunk, gameTime int64, status string) nbt.Compound {
	full := status == "minecraft:full"
	reg := data.Canonical()
	root := nbt.Compound{}
	for k, v := range c.Extra {
		root[k] = v
	}
	minSec := c.Dim.MinSection()
	root["DataVersion"] = int32(DataVersion)
	root["xPos"] = c.X
	root["zPos"] = c.Z
	root["yPos"] = int32(minSec)
	root["Status"] = status
	root["LastUpdate"] = gameTime
	root["InhabitedTime"] = c.InhabitedTime
	if c.LightReady {
		root["isLightOn"] = int8(1)
	} else if full {
		root["isLightOn"] = int8(0)
	}

	vals := make([]uint32, 4096)
	var nameBuf []byte
	sections := make([]nbt.Compound, 0, len(c.SkyLight))
	for li := range c.SkyLight {
		y := li - 1 + minSec
		sec := nbt.Compound{"Y": int8(y)}
		// Proto chunks keep partial light (initialize_light writes sky light
		// before the chunk is lit).
		if c.LightReady || !full {
			if c.Dim.HasSkylight && skyNeeded(c, li, full) {
				sec["SkyLight"] = append([]byte(nil), skyBytes(c.SkyLight[li])...)
			}
			if !c.BlockLight[li].IsZero() {
				sec["BlockLight"] = append([]byte(nil), c.BlockLight[li].Bytes()...)
			}
		}
		si := li - 1
		if si >= 0 && si < len(c.Sections) {
			s := &c.Sections[si]
			pal, idx := diskPalette(&s.Blocks, vals)
			// Like vanilla 26.3: default states are bare names; a list mixing
			// them with {id, properties} wraps them as {"": name}.
			entries := make([]any, len(pal))
			allDefault := true
			for i, id := range pal {
				st := data.StateID(id)
				b := reg.BlockOf(st)
				if b == nil {
					entries[i] = "minecraft:air"
					continue
				}
				nameBuf = append(append(nameBuf[:0], "minecraft:"...), b.Name...)
				if st == b.DefaultState || len(b.Props) == 0 {
					entries[i] = string(nameBuf)
					continue
				}
				allDefault = false
				props := nbt.Compound{}
				for p := range b.Props {
					props[b.Props[p].Name] = b.Props[p].Values[b.ValueIndex(st, p)]
				}
				entries[i] = nbt.Compound{"id": string(nameBuf), "properties": props}
			}
			palList := nbt.List{Type: nbt.TagString, Items: entries}
			if !allDefault {
				for i, e := range entries {
					if name, ok := e.(string); ok {
						entries[i] = nbt.Compound{"": name}
					}
				}
				palList.Type = nbt.TagCompound
			}
			bs := nbt.Compound{"palette": palList}
			if len(pal) > 1 {
				bs["data"] = packLongs(idx, uint8(max(4, bits.Len(uint(len(pal)-1)))))
			}
			sec["block_states"] = bs

			bpal, bidx := diskPalette(&s.Biomes, vals[:64])
			bentries := make([]any, len(bpal))
			for i, id := range bpal {
				bentries[i] = "minecraft:" + reg.BiomeName(data.BiomeID(id))
			}
			bm := nbt.Compound{"palette": nbt.List{Type: nbt.TagString, Items: bentries}}
			if len(bpal) > 1 {
				bm["data"] = packLongs(bidx, uint8(bits.Len(uint(len(bpal)-1))))
			}
			sec["biomes"] = bm
		} else if len(sec) == 1 {
			continue
		}
		sections = append(sections, sec)
	}
	root["sections"] = sections

	hm := nbt.Compound{}
	var tmp []uint64
	for _, h := range heightmapsFor(status) {
		src := h
		switch h {
		case world.WorldSurfaceWG:
			src = world.WorldSurface
		case world.OceanFloorWG:
			src = world.OceanFloor
		}
		tmp = c.PackHeightmap(tmp, src)
		l := make([]int64, len(tmp))
		for i, v := range tmp {
			l[i] = int64(v)
		}
		hm[h.String()] = l
	}
	root["Heightmaps"] = hm

	bes := make([]nbt.Compound, 0, len(c.BlockEntities))
	for _, be := range c.BlockEntities {
		e := nbt.Compound{}
		for k, v := range be.Data {
			e[k] = v
		}
		e["id"] = be.ID
		e["x"] = c.X<<4 + int32(be.X)
		e["y"] = be.Y
		e["z"] = c.Z<<4 + int32(be.Z)
		bes = append(bes, e)
	}
	root["block_entities"] = bes
	if full {
		root["block_ticks"], root["fluid_ticks"] = pendingToDisk(append(slices.Clip(c.PendingTicks), c.SavedTicks...))
		if len(c.GenEntities) > 0 {
			root[genEntitiesKey] = slices.Clone(c.GenEntities)
		}
	}
	return root
}

// pendingFromDisk reads the block_ticks and fluid_ticks of a full chunk;
// the delays are relative to the save, as vanilla writes them.
func pendingFromDisk(root nbt.Compound) []world.PendingTick {
	var out []world.PendingTick
	for _, k := range [...]string{"block_ticks", "fluid_ticks"} {
		for _, t := range decodeTicks(root[k]) {
			out = append(out, world.PendingTick{X: t.X, Y: t.Y, Z: t.Z, Type: t.Type, Delay: t.Delay, Priority: t.Priority, Fluid: k == "fluid_ticks"})
		}
	}
	return out
}

func pendingToDisk(ts []world.PendingTick) (blocks, fluids nbt.List) {
	var b, f []Tick
	for _, t := range ts {
		v := Tick{X: t.X, Y: t.Y, Z: t.Z, Type: t.Type, Delay: t.Delay, Priority: t.Priority}
		if t.Fluid {
			f = append(f, v)
		} else {
			b = append(b, v)
		}
	}
	return encodeTicks(b), encodeTicks(f)
}

// skyNeeded reports whether a sky light array must be written. Lit full
// chunks omit arrays the reader can derive from the section above (as
// DecodeChunk does), so an all-zero array below lit sections is written.
func skyNeeded(c *world.Chunk, li int, full bool) bool {
	n := c.SkyLight[li]
	if !full || !c.LightReady {
		return !n.IsZero()
	}
	above := world.FullNibbles()
	if li+1 < len(c.SkyLight) {
		above = c.SkyLight[li+1]
	}
	return !nibblesEqual(n, extendDown(above))
}

func nibblesEqual(a, b world.Nibbles) bool {
	if a.IsZero() || b.IsZero() {
		return a.IsZero() == b.IsZero()
	}
	if a.IsFull() || b.IsFull() {
		for i := range 4096 {
			if a.Get(i) != b.Get(i) {
				return false
			}
		}
		return true
	}
	return string(a.Bytes()) == string(b.Bytes())
}

func skyBytes(n world.Nibbles) []byte {
	if b := n.Bytes(); b != nil {
		return b
	}
	return make([]byte, 2048)
}

// diskPalette returns a compact palette and per-entry indices (into vals).
func diskPalette(c *world.Container, vals []uint32) ([]uint32, []uint32) {
	if v, ok := c.Single(); ok {
		return []uint32{v}, nil
	}
	c.Values(vals)
	var pal []uint32
	idx := map[uint32]uint32{}
	for i, v := range vals {
		j, ok := idx[v]
		if !ok {
			j = uint32(len(pal))
			idx[v] = j
			pal = append(pal, v)
		}
		vals[i] = j
	}
	return pal, vals
}

func packLongs(idx []uint32, b uint8) []int64 {
	u := world.PackIndices(nil, idx, b)
	out := make([]int64, len(u))
	for i, v := range u {
		out[i] = int64(v)
	}
	return out
}

func listItems(v any) []any {
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

func asInt(v any) int32 {
	switch n := v.(type) {
	case int8:
		return int32(n)
	case int16:
		return int32(n)
	case int32:
		return n
	case int64:
		return int32(n)
	}
	return 0
}

func asInt64(v any) int64 {
	if n, ok := v.(int64); ok {
		return n
	}
	return int64(asInt(v))
}

func asBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case int8:
		return b != 0
	}
	return false
}
