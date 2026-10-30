package anvil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// Proto is a chunk at any generation status with the generation state
// vanilla 26.3 persists. Chunk.Extra keeps every root key not decoded here
// (e.g. UpgradeData, blending_data, below_zero_retrogen, carving masks) so
// it is written back unchanged.
type Proto struct {
	Chunk  *world.Chunk
	Status string // "minecraft:terrain", ...
	// Starts are structures.starts by structure id (vanilla writes
	// {id:"INVALID"} for checked positions without a start).
	Starts map[string]nbt.Compound
	// References are structures.References: packed ChunkPos per structure id.
	References map[string][]int64
	// Entities are stored inline in proto-chunks (full chunks keep them in
	// the entities/ region files).
	Entities []nbt.Compound
	// PostProcessing holds, per section from the bottom, packed
	// section-relative positions x | y<<4 | z<<8.
	PostProcessing [][]int16
	BlockTicks     []Tick
	FluidTicks     []Tick
	// WG holds packed WORLD_SURFACE_WG / OCEAN_FLOOR_WG heightmaps to write
	// as is. Vanilla freezes them after terrain (feature writes, also from
	// neighbours, do not update them), so they cannot be recomputed from
	// blocks later. DecodeProto fills it from disk; missing entries are
	// computed from the current blocks.
	WG map[string][]int64
	// Extra are root keys written in addition to (and over) Chunk.Extra.
	Extra nbt.Compound
}

// Tick is a scheduled block or fluid tick ({i, x, y, z, t, p}).
type Tick struct {
	X, Y, Z  int32 // absolute
	Type     string
	Delay    int32
	Priority int32
}

var protoKeys = []string{"structures", "PostProcessing", "block_ticks", "fluid_ticks", "entities"}

// StatusID normalizes "terrain" to "minecraft:terrain".
func StatusID(s string) string {
	if s == "" {
		return "minecraft:empty"
	}
	if !strings.Contains(s, ":") {
		return "minecraft:" + s
	}
	return s
}

// heightmapsFor lists the heightmaps vanilla 26.3 saves for a status: none
// before terrain, the worldgen and final ones on proto-chunks from terrain
// on, the final four on full chunks.
func heightmapsFor(status string) []world.Heightmap {
	final := []world.Heightmap{world.WorldSurface, world.OceanFloor, world.MotionBlocking, world.MotionBlockingNoLeaves}
	switch StatusID(status) {
	case "minecraft:full":
		return final
	case "minecraft:empty", "minecraft:structure_starts", "minecraft:structure_references", "minecraft:biomes":
		return nil
	}
	return append([]world.Heightmap{world.WorldSurfaceWG, world.OceanFloorWG}, final...)
}

// DecodeProto decodes chunk NBT of any status.
func DecodeProto(root nbt.Compound, dim world.Dimension) (*Proto, LoadStats, error) {
	status, _ := root["Status"].(string)
	if status == "" {
		status, _ = root["status"].(string)
	}
	c, st, err := decodeChunk(root, dim, true)
	if err != nil {
		return nil, st, err
	}
	p := &Proto{Chunk: c, Status: StatusID(status), Starts: map[string]nbt.Compound{}, References: map[string][]int64{},
		PostProcessing: make([][]int16, dim.Sections())}
	for _, k := range protoKeys {
		delete(c.Extra, k)
	}
	if len(c.Extra) == 0 {
		c.Extra = nil
	}
	if s, ok := root["structures"].(nbt.Compound); ok {
		if starts, ok := s["starts"].(nbt.Compound); ok {
			for id, v := range starts {
				if sc, ok := v.(nbt.Compound); ok {
					p.Starts[id] = sc
				}
			}
		}
		if refs, ok := s["References"].(nbt.Compound); ok {
			for id, v := range refs {
				l, ok := v.([]int64)
				if !ok {
					return nil, st, fmt.Errorf("anvil: chunk %d,%d: references %s are %T", c.X, c.Z, id, v)
				}
				p.References[id] = l
			}
		}
	}
	for _, it := range listItems(root["entities"]) {
		if e, ok := it.(nbt.Compound); ok {
			p.Entities = append(p.Entities, e)
		}
	}
	for i, it := range listItems(root["PostProcessing"]) {
		l, ok := it.(nbt.List)
		if !ok || i >= len(p.PostProcessing) {
			continue
		}
		for _, v := range l.Items {
			if n, ok := v.(int16); ok {
				p.PostProcessing[i] = append(p.PostProcessing[i], n)
			}
		}
	}
	p.BlockTicks = decodeTicks(root["block_ticks"])
	p.FluidTicks = decodeTicks(root["fluid_ticks"])
	if hm, ok := root["Heightmaps"].(nbt.Compound); ok {
		for _, h := range []world.Heightmap{world.WorldSurfaceWG, world.OceanFloorWG} {
			if l, ok := hm[h.String()].([]int64); ok {
				if p.WG == nil {
					p.WG = map[string][]int64{}
				}
				p.WG[h.String()] = l
			}
		}
	}
	return p, st, nil
}

func decodeTicks(v any) []Tick {
	var out []Tick
	for _, it := range listItems(v) {
		c, ok := it.(nbt.Compound)
		if !ok {
			continue
		}
		id, _ := c["i"].(string)
		out = append(out, Tick{X: asInt(c["x"]), Y: asInt(c["y"]), Z: asInt(c["z"]), Type: id, Delay: asInt(c["t"]), Priority: asInt(c["p"])})
	}
	return out
}

func encodeTicks(ts []Tick) nbt.List {
	items := make([]any, len(ts))
	for i, t := range ts {
		items[i] = nbt.Compound{"i": t.Type, "p": t.Priority, "t": t.Delay, "x": t.X, "y": t.Y, "z": t.Z}
	}
	return nbt.List{Type: nbt.TagCompound, Items: items}
}

// EncodeProto encodes a chunk of any status in the vanilla 26.3 layout. The
// caller must own the chunk or hold its read lock.
func EncodeProto(p *Proto, gameTime int64) nbt.Compound {
	status := StatusID(p.Status)
	root := encodeChunk(p.Chunk, gameTime, status)
	for k, v := range p.Extra {
		root[k] = v
	}
	if hm, ok := root["Heightmaps"].(nbt.Compound); ok {
		// Before terrain vanilla saves the _WG maps (all zero) only on the
		// proto-chunks that created them; keep what was decoded.
		early := heightmapsFor(status) == nil
		for name, l := range p.WG {
			if _, want := hm[name]; want || early {
				hm[name] = l
			}
		}
	}
	starts := nbt.Compound{}
	for id, s := range p.Starts {
		starts[id] = s
	}
	refs := nbt.Compound{}
	for id, l := range p.References {
		refs[id] = l
	}
	root["structures"] = nbt.Compound{"starts": starts, "References": refs}
	n := max(len(p.PostProcessing), p.Chunk.Dim.Sections())
	post := make([]any, n)
	for i := range post {
		var l []int16
		if i < len(p.PostProcessing) {
			l = p.PostProcessing[i]
		}
		items := make([]any, len(l))
		for j, v := range l {
			items[j] = v
		}
		post[i] = nbt.List{Type: nbt.TagShort, Items: items}
	}
	root["PostProcessing"] = nbt.List{Type: nbt.TagList, Items: post}
	root["block_ticks"] = encodeTicks(p.BlockTicks)
	root["fluid_ticks"] = encodeTicks(p.FluidTicks)
	if status != "minecraft:full" {
		ents := make([]any, len(p.Entities))
		for i, e := range p.Entities {
			ents[i] = e
		}
		root["entities"] = nbt.List{Type: nbt.TagCompound, Items: ents}
	}
	return root
}

// LoadNBT reads a chunk's raw NBT; nil, nil when absent.
func (s *Storage) LoadNBT(p world.ChunkPos) (nbt.Compound, error) {
	rx, rz := p.X>>5, p.Z>>5
	r, err := s.acquire(rx, rz, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer s.release(rx, rz)
	raw, err := r.Read(int(p.X&31), int(p.Z&31))
	if err != nil || raw == nil {
		return nil, err
	}
	_, root, _, err := nbt.Unmarshal(raw, false)
	if err != nil {
		return nil, fmt.Errorf("anvil: chunk %d,%d: %w", p.X, p.Z, err)
	}
	return root, nil
}

// SaveNBT writes a chunk's raw NBT. Callers must not write the same chunk
// concurrently; different chunks (even in one region) are fine.
func (s *Storage) SaveNBT(p world.ChunkPos, root nbt.Compound) error {
	if s.ReadOnly {
		return ErrReadOnly
	}
	buf, err := nbt.Marshal(nil, "", root, false)
	if err != nil {
		return fmt.Errorf("anvil: encode chunk %d,%d: %w", p.X, p.Z, err)
	}
	rx, rz := p.X>>5, p.Z>>5
	r, err := s.acquire(rx, rz, true)
	if err != nil {
		return err
	}
	defer s.release(rx, rz)
	return r.Write(int(p.X&31), int(p.Z&31), buf, s.Compression)
}

// LoadProto reads and decodes a chunk of any status; nil, nil when absent.
// Entities of full chunks come from the entities/ region files.
func (s *Storage) LoadProto(p world.ChunkPos) (*Proto, error) {
	root, err := s.LoadNBT(p)
	if err != nil || root == nil {
		return nil, err
	}
	pr, _, err := DecodeProto(root, s.Dim)
	if err != nil {
		return nil, err
	}
	if pr.Chunk.X != p.X || pr.Chunk.Z != p.Z {
		return nil, fmt.Errorf("anvil: chunk at %d,%d claims %d,%d", p.X, p.Z, pr.Chunk.X, pr.Chunk.Z)
	}
	if pr.Status == "minecraft:full" {
		if pr.Entities, err = s.LoadEntities(p); err != nil {
			return nil, err
		}
	}
	return pr, nil
}

// SaveProto encodes and writes a chunk of any status; a full chunk's
// entities go to the entities/ region files like vanilla.
func (s *Storage) SaveProto(p *Proto) error {
	var t int64
	if s.GameTime != nil {
		t = s.GameTime()
	}
	if StatusID(p.Status) == "minecraft:full" {
		if err := s.SaveEntities(p.Chunk.Pos(), p.Entities); err != nil {
			return err
		}
	}
	return s.SaveNBT(p.Chunk.Pos(), EncodeProto(p, t))
}

// entities returns the storage for the entities/ directory next to Dir.
func (s *Storage) entities() *Storage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ents == nil {
		dir := s.EntitiesDir
		if dir == "" {
			dir = filepath.Join(filepath.Dir(s.Dir), "entities")
		}
		s.ents = NewStorage(dir, s.Dim)
		s.ents.ReadOnly = s.ReadOnly
		s.ents.Compression = s.Compression
	}
	return s.ents
}

// LoadEntities reads a chunk's entities from the entities/ region files.
func (s *Storage) LoadEntities(p world.ChunkPos) ([]nbt.Compound, error) {
	root, err := s.entities().LoadNBT(p)
	if err != nil || root == nil {
		return nil, err
	}
	var out []nbt.Compound
	for _, it := range listItems(root["Entities"]) {
		if e, ok := it.(nbt.Compound); ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// SaveEntities writes a chunk's entities in the vanilla entities/ layout
// ({DataVersion, Position, Entities}); an empty list removes the record.
func (s *Storage) SaveEntities(p world.ChunkPos, ents []nbt.Compound) error {
	es := s.entities()
	if len(ents) == 0 {
		r, err := es.acquire(p.X>>5, p.Z>>5, false)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		defer es.release(p.X>>5, p.Z>>5)
		return r.Delete(int(p.X&31), int(p.Z&31))
	}
	items := make([]any, len(ents))
	for i, e := range ents {
		items[i] = e
	}
	return es.SaveNBT(p, nbt.Compound{
		"DataVersion": int32(DataVersion),
		"Position":    []int32{p.X, p.Z},
		"Entities":    nbt.List{Type: nbt.TagCompound, Items: items},
	})
}

// PackWG packs the chunk's current WORLD_SURFACE_WG and OCEAN_FLOOR_WG
// heightmaps (same predicates as WORLD_SURFACE and OCEAN_FLOOR), for
// snapshotting them when terrain completes.
func PackWG(c *world.Chunk) nbt.Compound {
	out := nbt.Compound{}
	for h, src := range map[world.Heightmap]world.Heightmap{world.WorldSurfaceWG: world.WorldSurface, world.OceanFloorWG: world.OceanFloor} {
		u := c.PackHeightmap(nil, src)
		l := make([]int64, len(u))
		for i, v := range u {
			l[i] = int64(v)
		}
		out[h.String()] = l
	}
	return out
}
