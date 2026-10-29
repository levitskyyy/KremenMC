package gen

import (
	"fmt"
	"slices"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
)

// anvilDataKey holds ProtoChunk.Data in the chunk NBT; vanilla ignores it.
const anvilDataKey = "kremenmc:data"

// AnvilWGKey is the ProtoChunk.Data key for frozen *_WG heightmaps
// (anvil.PackWG). Vanilla stops updating them once terrain is done, so a
// terrain stage that wants vanilla-identical files sets it when it
// finishes; when absent they are recomputed from blocks on save. Loading
// restores it from the stored *_WG heightmaps when it was set at save.
const AnvilWGKey = "anvil:wg_heightmaps"

// wgFrozenKey marks, inside the saved kremenmc:data, that the stored *_WG
// heightmaps are the frozen AnvilWGKey ones rather than recomputed.
const wgFrozenKey = "kremenmc:wg_frozen"

// AnvilStorage persists proto-chunks in vanilla 26.3 region files (the
// durable format, readable by the official server), sharing the files with
// world.Storage via the same *anvil.Storage. The pipeline writes each chunk
// from one goroutine at a time, which is all the region layer needs.
type AnvilStorage struct {
	St *anvil.Storage
	// DecodeStart rebuilds a structure start; nil keeps RawStart.
	DecodeStart func(id string, c nbt.Compound) (StructureStart, error)
}

// NewAnvilStorage opens region files in dir (e.g. <world>/region).
func NewAnvilStorage(dir string, dim world.Dimension) *AnvilStorage {
	return &AnvilStorage{St: anvil.NewStorage(dir, dim)}
}

func (s *AnvilStorage) Close() error { return s.St.Close() }

func (s *AnvilStorage) LoadProto(pos world.ChunkPos) (*ProtoChunk, error) {
	p, err := s.St.LoadProto(pos)
	if err != nil || p == nil {
		return nil, err
	}
	st, ok := ParseStatus(p.Status)
	if !ok {
		return nil, fmt.Errorf("gen: chunk %d,%d: unknown status %q", pos.X, pos.Z, p.Status)
	}
	c := &ProtoChunk{
		Chunk:          p.Chunk,
		Starts:         map[string]StructureStart{},
		Refs:           map[string][]int64{},
		Entities:       p.Entities,
		PostProcessing: p.PostProcessing,
	}
	c.SetStatus(st)
	for id, sc := range p.Starts {
		var start StructureStart = RawStart(sc)
		if s.DecodeStart != nil {
			if start, err = s.DecodeStart(id, sc); err != nil {
				return nil, fmt.Errorf("gen: chunk %d,%d start %s: %w", pos.X, pos.Z, id, err)
			}
		}
		c.Starts[id] = start
	}
	for id, l := range p.References {
		l = slices.Clone(l)
		slices.Sort(l)
		c.Refs[id] = slices.Compact(l)
	}
	c.BlockTicks = fromAnvilTicks(p.BlockTicks)
	c.FluidTicks = fromAnvilTicks(p.FluidTicks)
	if d, ok := c.Extra[anvilDataKey].(nbt.Compound); ok {
		c.Data = d
		delete(c.Extra, anvilDataKey)
	}
	if len(c.Extra) == 0 {
		c.Extra = nil
	}
	// A chunk evicted between terrain and features must come back with the
	// heightmaps frozen at terrain; recomputing them from blocks would make
	// features depend on whether the chunk was evicted in between.
	if _, frozen := c.Data[wgFrozenKey]; frozen {
		delete(c.Data, wgFrozenKey)
		if len(p.WG) == 2 {
			wg := make(nbt.Compound, 2)
			for k, l := range p.WG {
				wg[k] = l
			}
			c.Data[AnvilWGKey] = wg
		}
	}
	return c, nil
}

func (s *AnvilStorage) SaveProto(c *ProtoChunk) error {
	p := &anvil.Proto{
		Chunk:          c.Chunk,
		Status:         c.Status().ID(),
		Starts:         make(map[string]nbt.Compound, len(c.Starts)),
		References:     c.Refs,
		Entities:       c.Entities,
		PostProcessing: c.PostProcessing,
		BlockTicks:     toAnvilTicks(c.BlockTicks),
		FluidTicks:     toAnvilTicks(c.FluidTicks),
	}
	for id, st := range c.Starts {
		p.Starts[id] = st.Encode()
	}
	data := c.Data
	if wg, ok := data[AnvilWGKey].(nbt.Compound); ok {
		p.WG = map[string][]int64{}
		for k, v := range wg {
			if l, ok := v.([]int64); ok {
				p.WG[k] = l
			}
		}
		data = withoutKey(data, AnvilWGKey)
		data[wgFrozenKey] = int8(1)
	}
	if len(data) > 0 {
		p.Extra = nbt.Compound{anvilDataKey: data}
	}
	return s.St.SaveProto(p)
}

// withoutKey returns m without key k (m itself is not modified).
func withoutKey(m nbt.Compound, k string) nbt.Compound {
	out := make(nbt.Compound, len(m))
	for kk, v := range m {
		if kk != k {
			out[kk] = v
		}
	}
	return out
}

func fromAnvilTicks(ts []anvil.Tick) []Tick {
	if len(ts) == 0 {
		return nil
	}
	out := make([]Tick, len(ts))
	for i, t := range ts {
		out[i] = Tick{X: t.X, Y: t.Y, Z: t.Z, Type: t.Type, Delay: t.Delay, Priority: t.Priority}
	}
	return out
}

func toAnvilTicks(ts []Tick) []anvil.Tick {
	out := make([]anvil.Tick, len(ts))
	for i, t := range ts {
		out[i] = anvil.Tick{X: t.X, Y: t.Y, Z: t.Z, Type: t.Type, Delay: t.Delay, Priority: t.Priority}
	}
	return out
}
