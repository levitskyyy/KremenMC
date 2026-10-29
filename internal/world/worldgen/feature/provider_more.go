package feature

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/noise"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// noiseState is the noise provider: the normal noise at the scaled
// position, mapped from [-1, 1] onto the state list.
type noiseState struct {
	noise  noise.Noise
	scale  float64
	states []data.StateID
}

func pickByNoise(states []data.StateID, v float64) data.StateID {
	d := min(max((1+v)/2, 0), 0.9999)
	return states[int(d*float64(len(states)))]
}

func (n *noiseState) State(_ *Level, _ *Random, p Pos) data.StateID {
	return pickByNoise(n.states, float64(n.noise.Get(float64(p.X)*n.scale, float64(p.Y)*n.scale, float64(p.Z)*n.scale)))
}

// randomizedInt is randomized_int: the source state with an int property
// set from an int provider (source drawn first).
type randomizedInt struct {
	src    StateProvider
	prop   string
	values IntProvider
}

func (ri randomizedInt) State(lv *Level, r *Random, p Pos) data.StateID {
	st := ri.src.State(lv, r, p)
	v := ri.values.Sample(r)
	if out, ok := data.Canonical().With(st, ri.prop, strconv.Itoa(v)); ok {
		return out
	}
	return st
}

func parseStates(l []json.RawMessage) ([]data.StateID, error) {
	var out []data.StateID
	for _, s := range l {
		st, err := parseBlockState(s)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

func init() {
	providerTypes["noise"] = func(_ *registry, b json.RawMessage) (StateProvider, error) {
		var p struct {
			Noise  noise.NormalParams `json:"noise"`
			Seed   int64              `json:"seed"`
			Scale  float64            `json:"scale"`
			States []json.RawMessage  `json:"states"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("noise provider: %w", err)
		}
		nn, err := noise.NewNormalNoise(p.Noise)
		if err != nil {
			return nil, err
		}
		n := &noiseState{noise: nn.Create(rng.NewWorldgenRandom(rng.NewLegacy(p.Seed))), scale: p.Scale}
		n.states, err = parseStates(p.States)
		return n, err
	}
	providerTypes["randomized_int"] = func(rg *registry, b json.RawMessage) (StateProvider, error) {
		var p struct {
			Property string          `json:"property"`
			Source   json.RawMessage `json:"source"`
			Values   json.RawMessage `json:"values"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("randomized_int: %w", err)
		}
		src, err := rg.parseStateProvider(p.Source)
		if err != nil {
			return nil, err
		}
		v, err := parseInt(p.Values)
		return randomizedInt{src, p.Property, v}, err
	}
}
