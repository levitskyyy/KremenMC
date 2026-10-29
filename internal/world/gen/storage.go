package gen

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// Codec converts proto-chunks to and from a lossless NBT form. Block and
// biome values are runtime registry ids, so the format is only valid for
// the build that wrote it; the Anvil proto-chunk format (internal/world/
// anvil) is the durable one.
type Codec struct {
	Dim world.Dimension
	// DecodeStart rebuilds a structure start; nil keeps RawStart.
	DecodeStart func(id string, c nbt.Compound) (StructureStart, error)
}

func (cd Codec) Encode(c *ProtoChunk) nbt.Compound {
	root := nbt.Compound{
		"xPos":          c.X,
		"zPos":          c.Z,
		"minY":          int32(c.Dim.MinY),
		"height":        int32(c.Dim.Height),
		"Status":        c.Status().ID(),
		"InhabitedTime": c.InhabitedTime,
		"lightReady":    c.LightReady,
	}
	vals := make([]uint32, 4096)
	secs := make([]nbt.Compound, len(c.Sections))
	for i := range c.Sections {
		s := &c.Sections[i]
		secs[i] = nbt.Compound{
			"blocks": encodeContainer(&s.Blocks, vals),
			"biomes": encodeContainer(&s.Biomes, vals[:64]),
		}
	}
	root["sections"] = secs
	light := make([]nbt.Compound, len(c.SkyLight))
	for i := range c.SkyLight {
		l := nbt.Compound{}
		if b := c.SkyLight[i].Bytes(); b != nil {
			l["sky"] = append([]byte(nil), b...)
		}
		if b := c.BlockLight[i].Bytes(); b != nil {
			l["block"] = append([]byte(nil), b...)
		}
		light[i] = l
	}
	root["light"] = light
	bes := make([]nbt.Compound, len(c.BlockEntities))
	for i, be := range c.BlockEntities {
		e := nbt.Compound{"x": int32(be.X), "y": be.Y, "z": int32(be.Z), "net": be.NetType, "id": be.ID}
		if be.Data != nil {
			e["data"] = cloneCompound(be.Data)
		}
		bes[i] = e
	}
	root["blockEntities"] = bes
	if c.Extra != nil {
		root["extra"] = cloneCompound(c.Extra)
	}
	starts := nbt.Compound{}
	for _, id := range c.SortedStarts() {
		starts[id] = c.Starts[id].Encode()
	}
	root["starts"] = starts
	refs := nbt.Compound{}
	for _, id := range c.SortedRefs() {
		refs[id] = append([]int64(nil), c.Refs[id]...)
	}
	root["refs"] = refs
	ents := make([]nbt.Compound, len(c.Entities))
	for i := range c.Entities {
		ents[i] = cloneCompound(c.Entities[i])
	}
	root["entities"] = ents
	post := nbt.Compound{}
	for i, l := range c.PostProcessing {
		if len(l) == 0 {
			continue
		}
		v := make([]int32, len(l))
		for k, x := range l {
			v[k] = int32(x)
		}
		post[strconv.Itoa(i)] = v
	}
	root["post"] = post
	root["blockTicks"] = encodeTicks(c.BlockTicks)
	root["fluidTicks"] = encodeTicks(c.FluidTicks)
	if c.Data != nil {
		root["data"] = cloneCompound(c.Data)
	}
	return root
}

func encodeContainer(c *world.Container, vals []uint32) nbt.Compound {
	vals = c.Values(vals)
	var pal []int32
	idx := map[uint32]int{}
	for _, v := range vals {
		if _, ok := idx[v]; !ok {
			idx[v] = len(pal)
			pal = append(pal, int32(v))
		}
	}
	out := nbt.Compound{"palette": pal}
	if len(pal) == 1 {
		return out
	}
	if len(pal) <= 256 {
		b := make([]byte, len(vals))
		for i, v := range vals {
			b[i] = byte(idx[v])
		}
		out["data"] = b
		return out
	}
	d := make([]int32, len(vals))
	for i, v := range vals {
		d[i] = int32(v)
	}
	out["direct"] = d
	return out
}

func decodeContainer(c *world.Container, v any, vals []uint32) error {
	m, ok := v.(nbt.Compound)
	if !ok {
		return errors.New("container missing")
	}
	pal, _ := m["palette"].([]int32)
	if d, ok := m["direct"].([]int32); ok && len(d) == len(vals) {
		for i, x := range d {
			vals[i] = uint32(x)
		}
	} else if len(pal) == 1 {
		for i := range vals {
			vals[i] = uint32(pal[0])
		}
	} else if b, ok := m["data"].([]byte); ok && len(b) == len(vals) {
		for i, x := range b {
			if int(x) >= len(pal) {
				return errors.New("palette index out of range")
			}
			vals[i] = uint32(pal[x])
		}
	} else {
		return errors.New("bad container")
	}
	c.SetAll(vals)
	return nil
}

func encodeTicks(ts []Tick) []nbt.Compound {
	out := make([]nbt.Compound, len(ts))
	for i, t := range ts {
		out[i] = nbt.Compound{"x": t.X, "y": t.Y, "z": t.Z, "i": t.Type, "t": t.Delay, "p": t.Priority}
	}
	return out
}

func compounds(v any) []nbt.Compound {
	switch x := v.(type) {
	case []nbt.Compound:
		return x
	case nbt.List:
		out := make([]nbt.Compound, 0, len(x.Items))
		for _, it := range x.Items {
			if c, ok := it.(nbt.Compound); ok {
				out = append(out, c)
			}
		}
		return out
	}
	return nil
}

func i32(v any) int32 {
	x, _ := v.(int32)
	return x
}

func decodeTicks(v any) []Tick {
	var out []Tick
	for _, c := range compounds(v) {
		id, _ := c["i"].(string)
		out = append(out, Tick{X: i32(c["x"]), Y: i32(c["y"]), Z: i32(c["z"]), Type: id, Delay: i32(c["t"]), Priority: i32(c["p"])})
	}
	return out
}

func (cd Codec) Decode(root nbt.Compound) (*ProtoChunk, error) {
	x, ok1 := root["xPos"].(int32)
	z, ok2 := root["zPos"].(int32)
	if !ok1 || !ok2 {
		return nil, errors.New("gen: proto chunk missing position")
	}
	dim := world.Dimension{MinY: int(i32(root["minY"])), Height: int(i32(root["height"])), HasSkylight: cd.Dim.HasSkylight}
	if dim.MinY != cd.Dim.MinY || dim.Height != cd.Dim.Height {
		return nil, fmt.Errorf("gen: chunk %d,%d has bounds %+v", x, z, dim)
	}
	name, _ := root["Status"].(string)
	st, ok := ParseStatus(name)
	if !ok {
		return nil, fmt.Errorf("gen: chunk %d,%d: unknown status %q", x, z, name)
	}
	c := NewProtoChunk(world.ChunkPos{X: x, Z: z}, cd.Dim)
	c.SetStatus(st)
	secs := compounds(root["sections"])
	if len(secs) != len(c.Sections) {
		return nil, fmt.Errorf("gen: chunk %d,%d: %d sections", x, z, len(secs))
	}
	vals := make([]uint32, 4096)
	for i := range secs {
		s := &c.Sections[i]
		if err := decodeContainer(&s.Blocks, secs[i]["blocks"], vals); err != nil {
			return nil, fmt.Errorf("gen: chunk %d,%d section %d blocks: %w", x, z, i, err)
		}
		if err := decodeContainer(&s.Biomes, secs[i]["biomes"], vals[:64]); err != nil {
			return nil, fmt.Errorf("gen: chunk %d,%d section %d biomes: %w", x, z, i, err)
		}
	}
	for i, l := range compounds(root["light"]) {
		if i >= len(c.SkyLight) {
			break
		}
		if b, ok := l["sky"].([]byte); ok {
			c.SkyLight[i], _ = world.NibblesFrom(b)
		}
		if b, ok := l["block"].([]byte); ok {
			c.BlockLight[i], _ = world.NibblesFrom(b)
		}
	}
	c.LightReady = root["lightReady"] == int8(1) || root["lightReady"] == true
	for _, e := range compounds(root["blockEntities"]) {
		id, _ := e["id"].(string)
		d, _ := e["data"].(nbt.Compound)
		c.BlockEntities = append(c.BlockEntities, world.BlockEntity{
			X: uint8(i32(e["x"])), Y: i32(e["y"]), Z: uint8(i32(e["z"])), NetType: i32(e["net"]), ID: id, Data: d,
		})
	}
	c.InhabitedTime, _ = root["InhabitedTime"].(int64)
	c.Extra, _ = root["extra"].(nbt.Compound)
	if starts, ok := root["starts"].(nbt.Compound); ok {
		for id, v := range starts {
			sc, _ := v.(nbt.Compound)
			var s StructureStart = RawStart(sc)
			if cd.DecodeStart != nil {
				var err error
				if s, err = cd.DecodeStart(id, sc); err != nil {
					return nil, fmt.Errorf("gen: chunk %d,%d start %s: %w", x, z, id, err)
				}
			}
			c.Starts[id] = s
		}
	}
	if refs, ok := root["refs"].(nbt.Compound); ok {
		for id, v := range refs {
			l, _ := v.([]int64)
			c.Refs[id] = l
		}
	}
	c.Entities = compounds(root["entities"])
	if post, ok := root["post"].(nbt.Compound); ok {
		for k, v := range post {
			i, err := strconv.Atoi(k)
			l, _ := v.([]int32)
			if err != nil || i < 0 || i >= len(c.PostProcessing) {
				continue
			}
			s := make([]int16, len(l))
			for n, x := range l {
				s[n] = int16(x)
			}
			c.PostProcessing[i] = s
		}
	}
	c.BlockTicks = decodeTicks(root["blockTicks"])
	c.FluidTicks = decodeTicks(root["fluidTicks"])
	c.Data, _ = root["data"].(nbt.Compound)
	for i := range c.Sections {
		c.Sections[i].Recount()
	}
	c.RecalcHeightmaps()
	return c, nil
}

// Marshal encodes c to bytes.
func (cd Codec) Marshal(c *ProtoChunk) ([]byte, error) {
	return nbt.Marshal(nil, "", cd.Encode(c), false)
}

// Unmarshal decodes bytes produced by Marshal.
func (cd Codec) Unmarshal(b []byte) (*ProtoChunk, error) {
	_, root, _, err := nbt.Unmarshal(b, false)
	if err != nil {
		return nil, fmt.Errorf("gen: decode: %w", err)
	}
	return cd.Decode(root)
}

// MemStorage keeps encoded chunks in memory. Chunks go through the codec,
// so a pipeline reopened on the same MemStorage sees exactly what a disk
// round trip would give.
type MemStorage struct {
	Codec
	mu sync.Mutex
	m  map[world.ChunkPos][]byte
}

func NewMemStorage(dim world.Dimension) *MemStorage {
	return &MemStorage{Codec: Codec{Dim: dim}, m: map[world.ChunkPos][]byte{}}
}

func (s *MemStorage) LoadProto(p world.ChunkPos) (*ProtoChunk, error) {
	s.mu.Lock()
	b, ok := s.m[p]
	s.mu.Unlock()
	if !ok {
		return nil, nil
	}
	return s.Unmarshal(b)
}

func (s *MemStorage) SaveProto(c *ProtoChunk) error {
	b, err := s.Marshal(c)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.m[c.Pos()] = b
	s.mu.Unlock()
	return nil
}

// Len is the number of stored chunks.
func (s *MemStorage) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}

// FileStorage keeps one file per chunk under Dir, written atomically.
type FileStorage struct {
	Codec
	Dir string
}

func NewFileStorage(dir string, dim world.Dimension) (*FileStorage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("gen: storage: %w", err)
	}
	return &FileStorage{Codec: Codec{Dim: dim}, Dir: dir}, nil
}

func (s *FileStorage) path(p world.ChunkPos) string {
	return filepath.Join(s.Dir, fmt.Sprintf("c.%d.%d.nbt", p.X, p.Z))
}

func (s *FileStorage) LoadProto(p world.ChunkPos) (*ProtoChunk, error) {
	b, err := os.ReadFile(s.path(p))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("gen: storage: %w", err)
	}
	return s.Unmarshal(b)
}

func (s *FileStorage) SaveProto(c *ProtoChunk) error {
	b, err := s.Marshal(c)
	if err != nil {
		return err
	}
	path := s.path(c.Pos())
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("gen: storage: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("gen: storage: %w", err)
	}
	return nil
}
