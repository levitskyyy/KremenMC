package generator

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
)

type Layer struct {
	State  data.StateID
	Height int
}

// Flat stacks layers upward from the dimension's MinY.
type Flat struct {
	Layers []Layer
	Biome  data.BiomeID
}

// DefaultFlat is bedrock, two dirt and grass in plains.
func DefaultFlat() *Flat {
	f, _ := ParseFlat("minecraft:bedrock,2*minecraft:dirt,minecraft:grass_block", "plains")
	return f
}

// ParseFlat parses a vanilla-style layer list: "minecraft:bedrock,3*minecraft:stone[...]"
// bottom to top.
func ParseFlat(spec, biome string) (*Flat, error) {
	reg := data.Canonical()
	b, ok := reg.BiomeByName(biome)
	if !ok {
		return nil, fmt.Errorf("generator: unknown biome %q", biome)
	}
	f := &Flat{Biome: b}
	depth := 0
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n := 1
		if cnt, rest, ok := strings.Cut(part, "*"); ok && !strings.Contains(cnt, "[") {
			v, err := strconv.Atoi(cnt)
			if err != nil || v < 1 {
				return nil, fmt.Errorf("generator: bad layer count %q", part)
			}
			n, part = v, rest
		}
		st, ok := reg.ParseState(part)
		if !ok {
			return nil, fmt.Errorf("generator: unknown block %q", part)
		}
		f.Layers = append(f.Layers, Layer{State: st, Height: n})
		depth += n
	}
	if depth > 4064 {
		return nil, fmt.Errorf("generator: flat layers too tall (%d)", depth)
	}
	return f, nil
}

func (f *Flat) Generate(c *world.Chunk) {
	vals := make([]uint32, 4096)
	var bio [64]uint32
	for i := range bio {
		bio[i] = uint32(f.Biome)
	}
	for si := range c.Sections {
		s := &c.Sections[si]
		s.Biomes.SetAll(bio[:])
		lo := si << 4
		var single data.StateID
		uniform := true
		for ly := range 16 {
			st := f.at(lo + ly)
			if ly == 0 {
				single = st
			} else if st != single {
				uniform = false
			}
			for i := ly << 8; i < (ly+1)<<8; i++ {
				vals[i] = uint32(st)
			}
		}
		if uniform {
			s.Blocks.Fill(uint32(single))
		} else {
			s.Blocks.SetAll(vals)
		}
	}
	c.Finish()
}

// at returns the state at height h above MinY.
func (f *Flat) at(h int) data.StateID {
	for _, l := range f.Layers {
		if h < l.Height {
			return l.State
		}
		h -= l.Height
	}
	return data.Air
}
