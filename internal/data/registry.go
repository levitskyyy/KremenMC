// Package data exposes block, item, entity and biome registries generated
// from minecraft-data. Per-state lookups are flat slices indexed by state ID.
package data

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data/gen"
)

type (
	StateID  uint32
	BlockID  uint16
	ItemID   int32
	EntityID int32
	BiomeID  int32
)

const (
	FlagAir uint8 = 1 << iota
	// FlagSolid matches vanilla blocksMotion(): used by MOTION_BLOCKING and physics.
	FlagSolid
	// FlagLiquid marks water and lava blocks.
	FlagLiquid
	// FlagFluid marks states whose fluid state is non-empty (liquids, waterlogged, kelp...).
	FlagFluid
	FlagLeaves
	FlagFullCube
	FlagTransparent
	FlagReplaceable
)

type Box struct{ MinX, MinY, MinZ, MaxX, MaxY, MaxZ float64 }

type Property struct {
	Name   string
	Values []string
	stride uint32
}

type Block struct {
	ID           BlockID
	Name         string
	MinState     StateID
	NumStates    uint32
	DefaultState StateID
	Hardness     float32 // -1 for unbreakable
	Resistance   float32
	Diggable     bool
	Props        []Property
}

type Item struct {
	ID        ItemID
	Name      string
	StackSize int
}

type EntityType struct {
	ID       EntityID
	Name     string
	Width    float32
	Height   float32
	Kind     string
	Category string
}

type Biome struct {
	ID            BiomeID
	Name          string
	Category      string
	Temperature   float32
	Precipitation bool
	Dimension     string
	Color         int32
}

// Registry is immutable after Load and safe for concurrent use.
type Registry struct {
	Version     string
	Protocol    int32
	DataVersion int32

	Blocks   []Block
	Items    []Item
	Entities []EntityType
	Biomes   []Biome

	stateBlock []BlockID
	stateFlags []uint8
	stateLight []uint8 // emission<<4 | filter
	stateShape []uint16
	shapes     [][]Box

	blockByName  map[string]BlockID
	itemByName   map[string]ItemID
	entityByName map[string]EntityID
	biomeByName  map[string]BiomeID
}

var (
	cacheMu sync.Mutex
	cache   = map[string]*Registry{}
)

// Load returns the registry for an embedded version, e.g. "26.3".
func Load(version string) (*Registry, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if r, ok := cache[version]; ok {
		return r, nil
	}
	raw, err := gen.FS.ReadFile(version + ".bin.gz")
	if err != nil {
		return nil, fmt.Errorf("data: version %s not embedded: %w", version, err)
	}
	r, err := parse(raw)
	if err != nil {
		return nil, fmt.Errorf("data: %s: %w", version, err)
	}
	cache[version] = r
	return r, nil
}

// Versions lists the embedded registry versions.
func Versions() []string {
	ents, _ := gen.FS.ReadDir(".")
	var out []string
	for _, e := range ents {
		if v, ok := strings.CutSuffix(e.Name(), ".bin.gz"); ok {
			out = append(out, v)
		}
	}
	return out
}

type reader struct {
	b   []byte
	off int
	err error
}

var errCorrupt = errors.New("corrupt registry blob")

func (r *reader) uv() int {
	if r.err != nil {
		return 0
	}
	v, n := binary.Uvarint(r.b[r.off:])
	if n <= 0 {
		r.err = errCorrupt
		return 0
	}
	r.off += n
	return int(v)
}

func (r *reader) u8() uint8 {
	if r.err != nil || r.off >= len(r.b) {
		r.err = errCorrupt
		return 0
	}
	r.off++
	return r.b[r.off-1]
}

func (r *reader) take(n int) []byte {
	if r.err != nil || n < 0 || r.off+n > len(r.b) {
		r.err = errCorrupt
		return nil
	}
	r.off += n
	return r.b[r.off-n : r.off]
}

func (r *reader) str() string { return string(r.take(r.uv())) }

func (r *reader) f32() float32 {
	p := r.take(4)
	if p == nil {
		return 0
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(p))
}

func (r *reader) f64() float64 {
	p := r.take(8)
	if p == nil {
		return 0
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(p))
}

func parse(gz []byte) (*Registry, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(raw, []byte("KRMCDAT1")) {
		return nil, errCorrupt
	}
	r := &reader{b: raw, off: 8}
	reg := &Registry{
		blockByName:  map[string]BlockID{},
		itemByName:   map[string]ItemID{},
		entityByName: map[string]EntityID{},
		biomeByName:  map[string]BiomeID{},
	}
	reg.Version = r.str()
	reg.Protocol = int32(r.uv())
	reg.DataVersion = int32(r.uv())

	coords := make([]float64, r.uv())
	for i := range coords {
		coords[i] = r.f64()
	}
	c := func() float64 {
		i := int(r.u8())
		if i >= len(coords) {
			r.err = errCorrupt
			return 0
		}
		return coords[i]
	}
	reg.shapes = make([][]Box, r.uv())
	for i := range reg.shapes {
		n := r.uv()
		if r.err != nil {
			return nil, r.err
		}
		boxes := make([]Box, n)
		for j := range boxes {
			boxes[j] = Box{c(), c(), c(), c(), c(), c()}
		}
		reg.shapes[i] = boxes
	}

	nBlocks, nStates := r.uv(), r.uv()
	if r.err != nil || nStates > 1<<24 || nBlocks > 1<<16 {
		return nil, errCorrupt
	}
	reg.Blocks = make([]Block, nBlocks)
	reg.stateBlock = make([]BlockID, nStates)
	reg.stateFlags = make([]uint8, nStates)
	reg.stateLight = make([]uint8, nStates)
	reg.stateShape = make([]uint16, nStates)
	next := 0
	for i := range reg.Blocks {
		b := &reg.Blocks[i]
		b.ID = BlockID(i)
		b.Name = r.str()
		b.MinState = StateID(next)
		b.DefaultState = StateID(next + r.uv())
		b.Hardness = r.f32()
		b.Resistance = r.f32()
		b.Diggable = r.u8() != 0
		b.Props = make([]Property, r.uv())
		for p := range b.Props {
			b.Props[p].Name = r.str()
			b.Props[p].Values = make([]string, r.uv())
			for v := range b.Props[p].Values {
				b.Props[p].Values[v] = r.str()
			}
		}
		n := uint32(1)
		for p := len(b.Props) - 1; p >= 0; p-- {
			b.Props[p].stride = n
			n *= uint32(len(b.Props[p].Values))
		}
		b.NumStates = n
		if r.err != nil || next+int(n) > nStates {
			return nil, errCorrupt
		}
		for s := next; s < next+int(n); s++ {
			reg.stateBlock[s] = b.ID
			reg.stateFlags[s] = r.u8()
			reg.stateLight[s] = r.u8()
			sh := r.uv()
			if sh >= len(reg.shapes) {
				return nil, errCorrupt
			}
			reg.stateShape[s] = uint16(sh)
		}
		next += int(n)
		reg.blockByName[b.Name] = b.ID
	}
	if next != nStates {
		return nil, errCorrupt
	}

	reg.Items = make([]Item, r.uv())
	for i := range reg.Items {
		reg.Items[i] = Item{ID: ItemID(i), Name: r.str(), StackSize: r.uv()}
		reg.itemByName[reg.Items[i].Name] = ItemID(i)
	}
	reg.Entities = make([]EntityType, r.uv())
	for i := range reg.Entities {
		e := EntityType{ID: EntityID(i), Name: r.str()}
		e.Width, e.Height = r.f32(), r.f32()
		e.Kind, e.Category = r.str(), r.str()
		reg.Entities[i] = e
		reg.entityByName[e.Name] = e.ID
	}
	reg.Biomes = make([]Biome, r.uv())
	for i := range reg.Biomes {
		b := Biome{ID: BiomeID(i), Name: r.str(), Category: r.str()}
		b.Temperature = r.f32()
		b.Precipitation = r.u8() != 0
		b.Dimension = r.str()
		b.Color = int32(r.uv())
		reg.Biomes[i] = b
		reg.biomeByName[b.Name] = b.ID
	}
	if r.err != nil {
		return nil, r.err
	}
	return reg, nil
}

func trimNS(name string) string {
	if n, ok := strings.CutPrefix(name, "minecraft:"); ok {
		return n
	}
	return name
}
