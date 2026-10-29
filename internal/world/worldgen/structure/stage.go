package structure

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Probe answers terrain-only questions at structure_starts time, before the
// chunk has terrain: the biome source and the noise height.
type Probe interface {
	// Biome is the biome source's biome at block x,y,z.
	Biome(x, y, z int32) data.BiomeID
	// Height is the terrain height (first non-matching block above) at
	// block x,z for "WORLD_SURFACE_WG" or "OCEAN_FLOOR_WG".
	Height(x, z int32, heightmap string) int32
}

// TypeFactory builds a Type from its worldgen/structure JSON.
type TypeFactory func(key string, raw json.RawMessage) (Type, error)

var typeFactories = map[string]TypeFactory{}

// RegisterType registers a structure type ("minecraft:igloo") from init().
func RegisterType(name string, f TypeFactory) { typeFactories[name] = f }

var stepNames = []string{"raw_generation", "lakes", "local_modifications", "underground_structures",
	"surface_structures", "strongholds", "underground_ores", "underground_decoration", "fluid_springs",
	"vegetal_decoration", "top_layer_modification"}

func stepIndex(s string) int {
	s = trimNS(s)
	for i, n := range stepNames {
		if n == s {
			return i
		}
	}
	return 4
}

type structDef struct {
	key    string
	typ    Type
	biomes []bool
	step   int
	index  int // index among this step's structures (decoration seed)
}

// Stages runs the structure statuses of one dimension. It is immutable
// after NewStages, apart from a decoded-start cache, and safe for
// concurrent chunks.
type Stages struct {
	seed  int64
	probe Probe
	sets  []*Set
	all   map[string]*Set // every set by key (exclusion zones)
	defs  map[string]*structDef
	steps [][]*structDef

	decoded sync.Map // startKey -> *Start

	// Features places feature_pool_element features (set before use).
	Features FeaturePlacer
}

type startKey struct {
	pos int64
	id  string
}

// NewStages loads every structure set whose structures kremenmc implements.
func NewStages(seed int64, probe Probe) (*Stages, error) {
	sets, err := LoadSets()
	if err != nil {
		return nil, err
	}
	pk, err := wgdata.Load()
	if err != nil {
		return nil, fmt.Errorf("structure: %w", err)
	}
	s := &Stages{seed: seed, probe: probe, all: map[string]*Set{}, defs: map[string]*structDef{}, steps: make([][]*structDef, len(stepNames))}
	for _, set := range sets {
		s.all[set.Key] = set
	}
	reg := data.Canonical()
	nb := len(reg.Biomes)
	var keys []string
	for _, k := range pk.Keys("worldgen/structure") {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	stepPos := make([]int, len(stepNames))
	for _, k := range keys {
		b, _ := pk.JSON("worldgen/structure", k)
		var head struct {
			Type   string          `json:"type"`
			Biomes json.RawMessage `json:"biomes"`
			Step   string          `json:"step"`
		}
		if err := json.Unmarshal(b, &head); err != nil {
			return nil, fmt.Errorf("structure: %s: %w", k, err)
		}
		index := stepPos[stepIndex(head.Step)]
		stepPos[stepIndex(head.Step)]++
		f, ok := typeFactories[qualify(head.Type)]
		if !ok {
			continue
		}
		t, err := f(qualify(k), b)
		if err != nil {
			return nil, fmt.Errorf("structure: %s: %w", k, err)
		}
		if t == nil {
			continue
		}
		d := &structDef{key: qualify(k), typ: t, step: stepIndex(head.Step), index: index, biomes: make([]bool, nb)}
		for _, name := range biomeList(pk, head.Biomes) {
			if id, ok := reg.BiomeByName(trimNS(name)); ok && int(id) < nb {
				d.biomes[id] = true
			}
		}
		s.defs[d.key] = d
	}
	// Decoration index: the position among ALL data pack structures of the
	// step in key order (loot seeds of reference buried treasures, index 0
	// of underground_structures, and a shipwreck, index 35 of
	// surface_structures; xoroshiro random, see stage PlaceStep).
	for _, k := range keys {
		if d, ok := s.defs[qualify(k)]; ok {
			s.steps[d.step] = append(s.steps[d.step], d)
		}
	}
	for _, set := range sets {
		if set.Placement.Type != "random_spread" {
			continue
		}
		ok := true
		for _, ws := range set.Structures {
			if s.defs[qualify(ws.Structure)] == nil {
				ok = false
			}
		}
		if ok && len(set.Structures) > 0 {
			s.sets = append(s.sets, set)
		}
	}
	return s, nil
}

func biomeList(pk *wgdata.Pack, raw json.RawMessage) []string {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		if strings.HasPrefix(one, "#") {
			l, _ := pk.Tag("worldgen/biome", one[1:])
			return l
		}
		return []string{one}
	}
	var many []string
	_ = json.Unmarshal(raw, &many)
	return many
}

// Supported lists the structures that generate.
func (s *Stages) Supported() []string {
	var out []string
	for _, set := range s.sets {
		for _, ws := range set.Structures {
			out = append(out, qualify(ws.Structure))
		}
	}
	sort.Strings(out)
	return out
}

// StartsStage is the structure_starts status.
func (s *Stages) StartsStage() gen.Stage {
	return gen.StageFunc{S: gen.StatusStructureStarts, F: s.runStarts}
}

// ReferencesStage is the structure_references status.
func (s *Stages) ReferencesStage() gen.Stage {
	return gen.StageFunc{S: gen.StatusStructureReferences, F: s.runRefs}
}

func (s *Stages) runStarts(sc *gen.StageContext) error {
	pos := sc.Pos()
	sts, err := s.StartsAt(pos.X, pos.Z)
	if err != nil {
		return err
	}
	for _, st := range sts {
		sc.Chunk().Starts[st.ID] = st
	}
	return nil
}

// StartsAt is every structure start of chunk cx,cz, one per structure set
// in set key order (the reference world of seed 0 holds a mineshaft and an
// ocean_ruin_warm start in chunk 23,-39).
func (s *Stages) StartsAt(cx, cz int32) ([]*Start, error) {
	var out []*Start
	for _, set := range s.sets {
		st, err := s.setStart(set, cx, cz)
		if err != nil {
			return nil, err
		}
		if st != nil {
			out = append(out, st)
		}
	}
	return out, nil
}

func largeFeature(seed int64, cx, cz int32) *rng.WorldgenRandom {
	r := rng.NewWorldgenRandom(rng.NewLegacy(0))
	r.SetLargeFeatureSeed(seed, cx, cz)
	return r
}

// StartAt is the structure start of chunk cx,cz, or nil. It is a pure
// function of the seed and the chunk.
func (s *Stages) StartAt(cx, cz int32) (*Start, error) {
	for _, set := range s.sets {
		st, err := s.setStart(set, cx, cz)
		if err != nil || st != nil {
			return st, err
		}
	}
	return nil, nil
}

func (s *Stages) setStart(set *Set, cx, cz int32) (*Start, error) {
	if !set.Placement.IsSpreadChunk(s.seed, cx, cz) {
		return nil, nil
	}
	if ex := set.Placement.Exclusion; ex != nil {
		if o := s.all[qualify(ex.OtherSet)]; o != nil && InExclusionRange(&o.Placement, s.seed, cx, cz, ex.ChunkCount) {
			return nil, nil
		}
	}
	// Multi-structure sets (shipwreck + shipwreck_beached, ocean ruins)
	// have disjoint biome lists: each structure is tried in list order
	// with a fresh start random; the weighted selection is not modelled.
	for _, ws := range set.Structures {
		st, err := s.tryStructure(ws, cx, cz)
		if err != nil || st != nil {
			return st, err
		}
	}
	return nil, nil
}

func (s *Stages) tryStructure(ws WeightedStructure, cx, cz int32) (*Start, error) {
	{
		r := largeFeature(s.seed, cx, cz)
		d := s.defs[qualify(ws.Structure)]
		ctx := &GenContext{
			Seed: s.seed, ChunkX: cx, ChunkZ: cz, Random: r,
			Height: func(x, z int32, hm string) int32 { return s.probe.Height(x, z, hm) },
			Biome: func(qx, qy, qz int32) data.BiomeID {
				return s.probe.Biome(qx<<2, qy<<2, qz<<2)
			},
			BiomeOK: func(x, y, z int32) bool {
				b := s.probe.Biome(x, y, z)
				return int(b) < len(d.biomes) && d.biomes[b]
			},
		}
		pieces, err := d.typ.Generate(ctx)
		if err != nil {
			return nil, fmt.Errorf("structure %s at %d,%d: %w", d.key, cx, cz, err)
		}
		if len(pieces) == 0 {
			return nil, nil
		}
		return &Start{ID: d.key, ChunkX: cx, ChunkZ: cz, Pieces: pieces}, nil
	}
}

// startOf returns the decoded start of id held by chunk src.
func (s *Stages) startOf(src *gen.ProtoChunk, id string) *Start {
	v := src.Starts[id]
	if v == nil {
		return nil
	}
	if st, ok := v.(*Start); ok {
		return st
	}
	k := startKey{int64(world.ChunkPos{X: src.X, Z: src.Z}.Key()), id}
	if c, ok := s.decoded.Load(k); ok {
		return c.(*Start)
	}
	st, err := DecodeStart(v.Encode())
	if err != nil || len(st.Pieces) == 0 {
		st = nil
	}
	s.decoded.Store(k, st)
	return st
}

func (s *Stages) runRefs(sc *gen.StageContext) error {
	pc := sc.Chunk()
	pos := sc.Pos()
	cb := ChunkBox(pos.X, pos.Z, int32(sc.Dim().MinY), int32(sc.Dim().Height))
	for dz := int32(-8); dz <= 8; dz++ {
		for dx := int32(-8); dx <= 8; dx++ {
			n, _, ok := sc.Neighbour(pos.X+dx, pos.Z+dz)
			if !ok {
				continue
			}
			for _, id := range n.SortedStarts() {
				st := s.startOf(n, id)
				if st == nil {
					continue
				}
				if st.Box().Intersects(cb) {
					pc.AddRef(id, world.ChunkPos{X: n.X, Z: n.Z})
				}
			}
		}
	}
	return nil
}

// PlaceStep places, before the features of decoration step step, every
// referenced start of that step clipped to the decorated chunk.
func (s *Stages) PlaceStep(sc *gen.StageContext, step int) {
	if step >= len(s.steps) || len(s.steps[step]) == 0 {
		return
	}
	pc := sc.Chunk()
	pos := sc.Pos()
	clip := ChunkBox(pos.X, pos.Z, int32(sc.Dim().MinY), int32(sc.Dim().Height))
	lv := &stageLevel{sc: sc, fp: s.Features}
	_, decoSeed := sc.Decoration()
	for _, d := range s.steps[step] {
		// One random per structure and chunk, shared by all its starts
		// (two-start mineshaft chunks of the reference world match only so,
		// starts in refOrder).
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(0))
		r.SetFeatureSeed(decoSeed, int32(d.index), int32(step))
		for _, k := range refOrder(pc.Refs[d.key]) {
			sp := world.PosFromKey(uint64(k))
			n, _, ok := sc.Neighbour(sp.X, sp.Z)
			if !ok {
				continue
			}
			st := s.startOf(n, d.key)
			if st == nil {
				continue
			}
			for _, p := range st.Pieces {
				if !p.Box().Intersects(clip) {
					continue
				}
				if err := p.Place(lv, r, clip, pos.X, pos.Z); err != nil {
					sc.Logf("structure %s piece %s: %v", d.key, p.Type(), err)
				}
			}
		}
	}
}

// DecodeStart inverts Start.Encode for pieces with a registered loader.
func DecodeStart(c nbt.Compound) (*Start, error) {
	st := &Start{}
	st.ID, _ = c["id"].(string)
	st.ChunkX, _ = c["ChunkX"].(int32)
	st.ChunkZ, _ = c["ChunkZ"].(int32)
	st.References, _ = c["references"].(int32)
	for _, pcmp := range compounds(c["Children"]) {
		id, _ := pcmp["id"].(string)
		l, ok := pieceLoaders[qualify(id)]
		if !ok {
			return nil, fmt.Errorf("structure: no loader for piece %s", id)
		}
		p, err := l(pcmp)
		if err != nil {
			return nil, err
		}
		st.Pieces = append(st.Pieces, p)
	}
	return st, nil
}

// stageLevel adapts a features StageContext to Level.
type stageLevel struct {
	sc *gen.StageContext
	fp FeaturePlacer
}

func (l *stageLevel) MinY() int32 { return int32(l.sc.Dim().MinY) }
func (l *stageLevel) MaxY() int32 { return int32(l.sc.Dim().MinY + l.sc.Dim().Height) }
func (l *stageLevel) Block(x, y, z int32) data.StateID {
	return l.sc.Block(int(x), int(y), int(z))
}
func (l *stageLevel) SetBlock(x, y, z int32, s data.StateID) bool {
	return l.sc.SetBlock(int(x), int(y), int(z), s)
}
func (l *stageLevel) Height(hm string, x, z int32) int32 {
	h := world.WorldSurfaceWG
	switch hm {
	case "OCEAN_FLOOR_WG":
		h = world.OceanFloorWG
	case "OCEAN_FLOOR":
		h = world.OceanFloor
	case "WORLD_SURFACE":
		h = world.WorldSurface
	}
	return int32(l.sc.Height(h, int(x), int(z)))
}
func (l *stageLevel) Biome(x, y, z int32) data.BiomeID {
	b, _ := l.sc.Biome(int(x), int(y), int(z))
	return b
}
func (l *stageLevel) SetBlockEntity(x, y, z int32, id string, c nbt.Compound) {
	l.sc.SetBlockEntity(int(x), int(y), int(z), id, c)
}
func (l *stageLevel) AddEntity(e nbt.Compound) {
	pc := l.sc.Chunk()
	pc.Entities = append(pc.Entities, e)
}
func (l *stageLevel) MarkPostProcess(x, y, z int32) {
	if l.sc.InWriteZone(int(x), int(z)) && x>>4 == l.sc.Pos().X && z>>4 == l.sc.Pos().Z {
		l.sc.Chunk().MarkPostProcess(int(x), int(y), int(z))
	}
}
