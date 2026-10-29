package structure

import (
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// EnableJigsaw wires a jigsaw structure for tests that compare it with the
// reference before it is enabled in generation.
func EnableJigsaw(keys ...string) {
	for _, k := range keys {
		jigsawEnabled[k] = true
	}
}

type JigsawInfo struct {
	X, Y, Z                   int32
	Front, Name, Target, Pool string
}

func TemplateJigsaws(loc string) ([]JigsawInfo, error) {
	t, err := LoadTemplate(loc)
	if err != nil {
		return nil, err
	}
	var out []JigsawInfo
	for _, j := range templateJigsaws(t) {
		out = append(out, JigsawInfo{j.x, j.y, j.z, j.front, j.name, j.target, j.pool})
	}
	return out, nil
}

// JigsawDraws turns on the assembly draw log and returns a getter.
func JigsawDraws() func() []string {
	jigDrawLog = []string{}
	return func() []string { return jigDrawLog }
}

func JigsawTrace() []string { return jigTrace }

func (s *Stages) ProbeHeight(x, z int32) int32 { return s.probe.Height(x, z, "WORLD_SURFACE_WG") }

func TemplateSize(loc string) [3]int32 {
	t, _ := LoadTemplate(loc)
	return [3]int32{t.SizeX, t.SizeY, t.SizeZ}
}

func PoolMaxHeight(name string) (int32, error) {
	a, err := NewAssembler()
	if err != nil {
		return 0, err
	}
	p, err := a.pool(name)
	if err != nil {
		return 0, err
	}
	var m int32
	for _, e := range p.elements {
		if e.primary() == "" {
			continue
		}
		t, err := LoadTemplate(e.primary())
		if err != nil {
			return 0, err
		}
		m = max(m, t.SizeY)
	}
	return m, nil
}

func RawJigsaws(loc string) []string {
	t, err := LoadTemplate(loc)
	if err != nil {
		return []string{err.Error()}
	}
	reg := data.Canonical()
	var out []string
	for _, b := range t.Palettes[0] {
		bl := reg.BlockOf(b.State)
		if bl == nil || bl.Name != "jigsaw" {
			continue
		}
		out = append(out, fmt.Sprintf("%d,%d,%d %s %v", b.X, b.Y, b.Z, reg.StateString(b.State), b.NBT))
	}
	return out
}

// SetStructureIndex overrides a structure's decoration index (tests).
func (s *Stages) SetStructureIndex(key string, i int) { s.defs[key].index = i }

// StructureIndex is a structure's decoration index.
func (s *Stages) StructureIndex(key string) int { return s.defs[key].index }

func SetFeatureHook(f func(x, y, z int32, r *rng.WorldgenRandom, block func(x, y, z int32) data.StateID)) {
	if f == nil {
		debugFeatureHook = nil
		return
	}
	debugFeatureHook = func(p *JigsawPiece, r *rng.WorldgenRandom, lv Level) { f(p.X, p.Y, p.Z, r, lv.Block) }
}

// TemplateBlockEntities lists a template's block entity NBT.
func TemplateBlockEntities(loc string) []string {
	t, err := LoadTemplate(loc)
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	reg := data.Canonical()
	for _, b := range t.Palettes[0] {
		if b.NBT != nil && reg.BlockOf(b.State).Name != "jigsaw" {
			out = append(out, fmt.Sprintf("%d,%d,%d %s %v", b.X, b.Y, b.Z, reg.StateString(b.State), b.NBT))
		}
	}
	return out
}

func (p *JigsawPiece) Primary() *Template { return p.primary() }

// TemplateStates lists the state of every block of a template's first
// palette (tests).
func TemplateStates(loc string) []string {
	t, err := LoadTemplate(loc)
	if err != nil {
		return []string{err.Error()}
	}
	reg := data.Canonical()
	var out []string
	for _, b := range t.Palettes[0] {
		out = append(out, reg.StateString(b.State))
	}
	return out
}
