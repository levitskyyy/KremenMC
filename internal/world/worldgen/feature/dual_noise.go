package feature

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/noise"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// dualNoiseState is the dual_noise state provider (meadow flowers). No
// random draws. Fitted to kremenmc.probe.feat_drip.Fn grids (5 varieties,
// 53,361 positions each, all exact): with slow = the slow noise at p,
// k = clamp(min + (int)((slow+1)/2·(max-min+1)), min, max) candidates, the
// i-th the state at the slow noise of p + (54545i, 0, 34234i); the noise at
// p picks among them. A value v picks index (int)((v+1)/2·n) clamped. Both
// noises come from Legacy(seed).
type dualNoiseState struct {
	noise, slow      noise.Noise
	scale, slowScale float64
	min, max         int
	states           []data.StateID
}

func dnIndex(n int, v float32) int {
	return min(max(int((1+v)/2*float32(n)), 0), n-1)
}

func (d *dualNoiseState) State(_ *Level, _ *Random, p Pos) data.StateID {
	slowAt := func(x, y, z int) float32 {
		return float32(d.slow.Get(float64(x)*d.slowScale, float64(y)*d.slowScale, float64(z)*d.slowScale))
	}
	k := d.min + int((slowAt(p.X, p.Y, p.Z)+1)/2*float32(d.max-d.min+1))
	k = min(max(k, d.min), d.max)
	var buf [16]data.StateID
	l := buf[:0]
	for i := range k {
		l = append(l, d.states[dnIndex(len(d.states), slowAt(p.X+i*54545, p.Y, p.Z+i*34234))])
	}
	v := float32(d.noise.Get(float64(p.X)*d.scale, float64(p.Y)*d.scale, float64(p.Z)*d.scale))
	return l[dnIndex(len(l), v)]
}

func parseDualNoise(_ *registry, b json.RawMessage) (StateProvider, error) {
	var c struct {
		Noise     noise.NormalParams `json:"noise"`
		Slow      noise.NormalParams `json:"slow_noise"`
		Seed      int64              `json:"seed"`
		Scale     float64            `json:"scale"`
		SlowScale float64            `json:"slow_scale"`
		Variety   [2]int             `json:"variety"`
		States    []json.RawMessage  `json:"states"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("dual_noise: %w", err)
	}
	n1, err := noise.NewNormalNoise(c.Noise)
	if err != nil {
		return nil, fmt.Errorf("dual_noise: %w", err)
	}
	n2, err := noise.NewNormalNoise(c.Slow)
	if err != nil {
		return nil, fmt.Errorf("dual_noise: %w", err)
	}
	d := &dualNoiseState{
		noise: n1.Create(rng.NewWorldgenRandom(rng.NewLegacy(c.Seed))), slow: n2.Create(rng.NewWorldgenRandom(rng.NewLegacy(c.Seed))),
		scale: c.Scale, slowScale: c.SlowScale, min: c.Variety[0], max: c.Variety[1],
	}
	for _, s := range c.States {
		st, err := parseBlockState(s)
		if err != nil {
			return nil, fmt.Errorf("dual_noise: %w", err)
		}
		d.states = append(d.states, st)
	}
	if len(d.states) == 0 || d.min < 1 || d.max > 16 {
		return nil, fmt.Errorf("dual_noise: bad states/variety")
	}
	return d, nil
}

func init() {
	providerTypes["dual_noise"] = parseDualNoise
}
