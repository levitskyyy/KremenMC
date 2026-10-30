package feature

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/carver"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// endSpikeDef is one obsidian pillar of the main end island.
type endSpikeDef struct {
	x, z, radius, height int
	guarded              bool
}

// endSpikesFor is EndSpikeFeature.getSpikesForLevel, fitted black-box with
// kremenmc.probe.feat_nether.Spikes (10 seeds, testdata/end_spikes.txt,
// TestEndSpikesForSeed): the indices 0..9 shuffled like
// Collections.shuffle by a legacy random seeded with the low 16 bits of the
// first nextLong of a legacy random seeded with the world seed; spike i
// stands at floor(42·cos θ), floor(42·sin θ) with θ = 2(-π +
// 0.31415927f·i) through the Mth sine table; shuffled index j gives radius
// 2 + j/3, height 76 + 3j, and iron bars for j = 1, 2.
func endSpikesFor(seed int64) [10]endSpikeDef {
	var l [10]int
	for i := range l {
		l[i] = i
	}
	r := rng.NewLegacy(rng.NewLegacy(seed).NextLong() & 0xffff)
	for i := 10; i > 1; i-- {
		j := int(r.NextIntn(int32(i)))
		l[i-1], l[j] = l[j], l[i-1]
	}
	var out [10]endSpikeDef
	for i, j := range l {
		th := 2.0 * (-math.Pi + float64(float32(0.31415927))*float64(i))
		out[i] = endSpikeDef{
			x:       int(math.Floor(42.0 * float64(carver.Cos(th)))),
			z:       int(math.Floor(42.0 * float64(carver.Sin(th)))),
			radius:  2 + j/3,
			height:  76 + 3*j,
			guarded: j == 1 || j == 2,
		}
	}
	return out
}

// endSpike is end_spike. Placement, from the probe's write log and the
// reference end (TestDimMeasure the_end): a spike is placed by the chunk
// holding its centre; over x, z within the radius and y from the bottom of
// the world to height+10, cells with dx²+dz² <= r²+1 below the height become
// obsidian, other cells above y 65 become air; guarded spikes get a 5×5×4
// iron bar cage (sides and top) from the height up; then one nextFloat
// (crystal yaw ×360), an end crystal at the centre one above the height,
// bedrock at the height and fire above it.
type endSpike struct {
	fixed        []endSpikeDef
	invulnerable bool
}

var (
	obsidianState, _ = data.DefaultState("obsidian")
	fireState, _     = data.DefaultState("fire")
	airState, _      = data.DefaultState("air")
)

func (f *endSpike) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	spikes := f.fixed
	if len(spikes) == 0 {
		if lv.sc == nil {
			return false
		}
		all := endSpikesFor(lv.sc.Seed())
		spikes = all[:]
	}
	placed := false
	for _, s := range spikes {
		if s.x>>4 == p.X>>4 && s.z>>4 == p.Z>>4 {
			f.placeSpike(lv, r, s)
			placed = true
		}
	}
	return placed
}

func (f *endSpike) placeSpike(lv *Level, r *Random, s endSpikeDef) {
	rad := s.radius
	for y := lv.MinY(); y <= s.height+10; y++ {
		for dz := -rad; dz <= rad; dz++ {
			for dx := -rad; dx <= rad; dx++ {
				q := Pos{s.x + dx, y, s.z + dz}
				if dx*dx+dz*dz <= rad*rad+1 && y < s.height {
					lv.SetBlock(q, obsidianState)
				} else if y > 65 {
					lv.SetBlock(q, airState)
				}
			}
		}
	}
	if s.guarded {
		cage := func(dx, dy, dz int) bool {
			return dx >= -2 && dx <= 2 && dz >= -2 && dz <= 2 && dy >= 0 && dy <= 3 &&
				(dx == -2 || dx == 2 || dz == -2 || dz == 2 || dy == 3)
		}
		for dx := -2; dx <= 2; dx++ {
			for dz := -2; dz <= 2; dz++ {
				for dy := 0; dy <= 3; dy++ {
					if cage(dx, dy, dz) {
						lv.SetBlock(Pos{s.x + dx, s.height + dy, s.z + dz}, ironBars(
							cage(dx, dy, dz-1), cage(dx, dy, dz+1), cage(dx-1, dy, dz), cage(dx+1, dy, dz)))
					}
				}
			}
		}
	}
	yaw := r.NextFloat() * 360
	lv.addEntity(Pos{s.x, s.height + 1, s.z}, nbt.Compound{
		"id":           "minecraft:end_crystal",
		"Pos":          nbt.List{Type: nbt.TagDouble, Items: []any{float64(s.x) + 0.5, float64(s.height + 1), float64(s.z) + 0.5}},
		"Motion":       nbt.List{Type: nbt.TagDouble, Items: []any{0.0, 0.0, 0.0}},
		"Rotation":     nbt.List{Type: nbt.TagFloat, Items: []any{yaw, float32(0)}},
		"ShowBottom":   int8(1),
		"Invulnerable": boolByte(f.invulnerable),
		"UUID":         genUUID(s.x, s.height+1, s.z, 7),
	})
	lv.SetBlock(Pos{s.x, s.height, s.z}, bedrockState)
	lv.SetBlock(Pos{s.x, s.height + 1, s.z}, fireState)
}

func ironBars(north, south, west, east bool) data.StateID {
	st, _ := data.Canonical().StateFrom("iron_bars", func(k string) (string, bool) {
		switch k {
		case "north":
			return fmt.Sprint(north), true
		case "south":
			return fmt.Sprint(south), true
		case "west":
			return fmt.Sprint(west), true
		case "east":
			return fmt.Sprint(east), true
		}
		return "", false
	})
	return st
}

func boolByte(b bool) int8 {
	if b {
		return 1
	}
	return 0
}

func init() {
	featureTypes["end_spike"] = func(_ *registry, b json.RawMessage) (Feature, error) {
		var raw struct {
			Spikes []struct {
				X       int  `json:"centerX"`
				Z       int  `json:"centerZ"`
				Radius  int  `json:"radius"`
				Height  int  `json:"height"`
				Guarded bool `json:"guarded"`
			} `json:"spikes"`
			Invulnerable bool            `json:"crystal_invulnerable"`
			Beam         json.RawMessage `json:"crystal_beam_target"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("end_spike: %w", err)
		}
		if len(raw.Beam) > 0 {
			return nil, unsupported("feature", "end_spike (crystal_beam_target)")
		}
		f := &endSpike{invulnerable: raw.Invulnerable}
		for _, s := range raw.Spikes {
			f.fixed = append(f.fixed, endSpikeDef{s.X, s.Z, s.Radius, s.Height, s.Guarded})
		}
		return f, nil
	}
}
