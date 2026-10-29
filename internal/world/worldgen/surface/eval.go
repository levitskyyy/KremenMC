package surface

import (
	"fmt"
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/density"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/noise"
)

type ruleEval interface {
	// apply returns the material for the context position or noState.
	apply(c *Ctx) data.StateID
}

type condEval interface {
	test(c *Ctx) bool
}

type compiler struct {
	s     *System
	reg   Registry
	rules map[*Rule]ruleEval
	conds map[*Condition]condEval
	slots int
}

func (c *compiler) rule(r *Rule) (ruleEval, error) {
	if e, ok := c.rules[r]; ok {
		return e, nil
	}
	var e ruleEval
	switch r.Kind {
	case rBlock:
		id, err := c.s.stateID(r.State)
		if err != nil {
			return nil, err
		}
		e = blockRule(id)
	case rBandlands:
		e = bandRule{}
	case rSequence:
		seq := make(seqRule, 0, len(r.Sequence))
		for _, sub := range r.Sequence {
			se, err := c.rule(sub)
			if err != nil {
				return nil, err
			}
			seq = append(seq, se)
		}
		if len(seq) == 1 {
			e = seq[0]
		} else {
			e = seq
		}
	case rCondition:
		ce, err := c.cond(r.If)
		if err != nil {
			return nil, err
		}
		te, err := c.rule(r.Then)
		if err != nil {
			return nil, err
		}
		e = &condRule{ce, te}
	case rOreVein:
		oe, err := c.ore(r.Ore)
		if err != nil {
			return nil, err
		}
		e = oe
	default:
		return nil, fmt.Errorf("surface: rule kind %d", r.Kind)
	}
	c.rules[r] = e
	return e, nil
}

func (c *compiler) slot() int {
	c.slots++
	return c.slots - 1
}

func (c *compiler) cond(k *Condition) (condEval, error) {
	if e, ok := c.conds[k]; ok {
		return e, nil
	}
	s := c.s
	var e condEval
	switch k.Kind {
	case cBiome:
		set := make([]bool, len(s.reg.Biomes))
		for _, b := range k.Biomes {
			id, ok := s.reg.BiomeByName(b)
			if !ok {
				return nil, fmt.Errorf("surface: unknown biome %s", b)
			}
			set[id] = true
		}
		e = &biomeCond{set: set, slot: c.slot()}
	case cNoiseThreshold:
		n, err := s.state.Noise(k.Noise)
		if err != nil {
			return nil, err
		}
		e = &noiseCond{n: n, min: k.Min, max: k.Max, is3d: k.Is3D, slot: c.slot()}
	case cVerticalGradient:
		e = &gradientCond{
			trueY:  k.TrueAt.resolve(s.MinY, s.Height),
			falseY: k.FalseAt.resolve(s.MinY, s.Height),
			pos:    newPosRandom(s.pos.FromHashOf(k.RandomName).ForkPositional()),
		}
	case cYAbove:
		e = &yAboveCond{y: k.Anchor.resolve(s.MinY, s.Height), mul: k.SurfaceDepthMultiplier, addStone: k.AddStoneDepth}
	case cWater:
		e = &waterCond{offset: k.Offset, mul: k.SurfaceDepthMultiplier, addStone: k.AddStoneDepth}
	case cTemperature:
		e = &tempCond{slot: c.slot()}
	case cSteep:
		e = steepCond{}
	case cHole:
		e = holeCond{}
	case cAbovePreliminarySurface:
		e = abovePrelimCond{}
	case cNot:
		in, err := c.cond(k.Invert)
		if err != nil {
			return nil, err
		}
		e = notCond{in}
	case cStoneDepth:
		e = &stoneDepthCond{offset: k.Offset, addSurface: k.AddSurfaceDepth, secondary: k.SecondaryDepthRange, ceiling: k.Ceiling}
	default:
		return nil, fmt.Errorf("surface: condition kind %d", k.Kind)
	}
	c.conds[k] = e
	return e, nil
}

func (c *compiler) ore(o *OreVein) (ruleEval, error) {
	s := c.s
	fn := func(raw []byte) (density.Sampler, error) {
		n, err := density.Parse(raw)
		if err != nil {
			return nil, err
		}
		return s.state.Compile(n)
	}
	e := &oreRule{pos: newPosRandom(s.pos.FromHashOf(oreRandomName).ForkPositional()), rawChance: o.RawOreChance}
	var err error
	if e.density, err = fn(o.Density); err != nil {
		return nil, err
	}
	if e.richness, err = fn(o.Richness); err != nil {
		return nil, err
	}
	if e.gap, err = fn(o.FillerGap); err != nil {
		return nil, err
	}
	if e.ore, err = s.stateID(o.OreBlock); err != nil {
		return nil, err
	}
	if e.raw, err = s.stateID(o.RawOreBlock); err != nil {
		return nil, err
	}
	if e.filler, err = s.stateID(o.FillerBlock); err != nil {
		return nil, err
	}
	return e, nil
}

const oreRandomName = "minecraft:ore"

type blockRule data.StateID

func (b blockRule) apply(*Ctx) data.StateID { return data.StateID(b) }

type bandRule struct{}

func (bandRule) apply(c *Ctx) data.StateID { return c.s.Band(c.x, c.y, c.z) }

type seqRule []ruleEval

func (s seqRule) apply(c *Ctx) data.StateID {
	for _, r := range s {
		if v := r.apply(c); v != noState {
			return v
		}
	}
	return noState
}

type condRule struct {
	cond condEval
	then ruleEval
}

func (r *condRule) apply(c *Ctx) data.StateID {
	if r.cond.test(c) {
		return r.then.apply(c)
	}
	return noState
}

type oreRule struct {
	density, richness, gap density.Sampler
	pos                    posRandom
	ore, raw, filler       data.StateID
	rawChance              float32
}

func (o *oreRule) apply(c *Ctx) data.StateID {
	d := o.density.Value(&c.d, c.x, c.y, c.z)
	if d <= 0 {
		return noState
	}
	r := o.pos.at(c.x, c.y, c.z)
	if r.nextFloat() > d {
		return noState
	}
	if r.nextFloat() < o.richness.Value(&c.d, c.x, c.y, c.z) && o.gap.Value(&c.d, c.x, c.y, c.z) < 0 {
		if r.nextFloat() < o.rawChance {
			return o.raw
		}
		return o.ore
	}
	return o.filler
}

type biomeCond struct {
	set  []bool
	slot int
}

func (b *biomeCond) test(c *Ctx) bool {
	if v, ok := c.memoY(b.slot); ok {
		return v
	}
	id := c.biome()
	v := int(id) < len(b.set) && b.set[id]
	c.setMemoY(b.slot, v)
	return v
}

type noiseCond struct {
	n        noise.Noise
	min, max float64
	is3d     bool
	slot     int
}

func (n *noiseCond) test(c *Ctx) bool {
	if n.is3d {
		v := float64(n.n.Get(float64(c.x), float64(c.y), float64(c.z)))
		return v >= n.min && v <= n.max
	}
	if v, ok := c.memoXZ(n.slot); ok {
		return v
	}
	v := float64(n.n.Get(float64(c.x), 0, float64(c.z)))
	r := v >= n.min && v <= n.max
	c.setMemoXZ(n.slot, r)
	return r
}

type gradientCond struct {
	trueY, falseY int32
	pos           posRandom
}

func (g *gradientCond) test(c *Ctx) bool {
	if c.y <= g.trueY {
		return true
	}
	if c.y >= g.falseY {
		return false
	}
	t := (float64(c.y) - float64(g.trueY)) / (float64(g.falseY) - float64(g.trueY))
	chance := 1 + t*(0-1)
	r := g.pos.at(c.x, c.y, c.z)
	return float64(r.nextFloat()) < chance
}

type yAboveCond struct {
	y, mul   int32
	addStone bool
}

func (k *yAboveCond) test(c *Ctx) bool {
	y := c.y
	if k.addStone {
		y += c.above
	}
	return y >= k.y+c.depth*k.mul
}

type waterCond struct {
	offset, mul int32
	addStone    bool
}

func (k *waterCond) test(c *Ctx) bool {
	if c.water == math.MinInt32 {
		return true
	}
	y := c.y
	if k.addStone {
		y += c.above
	}
	return y >= c.water+k.offset+c.depth*k.mul
}

type tempCond struct{ slot int }

func (k *tempCond) test(c *Ctx) bool {
	if v, ok := c.memoY(k.slot); ok {
		return v
	}
	id := c.biome()
	v := int(id) < len(c.s.climates) && c.s.climates[id].coldEnoughToSnow(c.x, c.y, c.z, c.s.SeaLevel)
	c.setMemoY(k.slot, v)
	return v
}

type steepCond struct{}

func (steepCond) test(c *Ctx) bool { return c.gz >= 4 || c.gx <= -4 }

type holeCond struct{}

func (holeCond) test(c *Ctx) bool { return c.depth <= 0 }

type abovePrelimCond struct{}

func (abovePrelimCond) test(c *Ctx) bool { return c.y >= c.minSurface }

type notCond struct{ in condEval }

func (n notCond) test(c *Ctx) bool { return !n.in.test(c) }

type stoneDepthCond struct {
	offset     int32
	addSurface bool
	secondary  int32
	ceiling    bool
}

func (k *stoneDepthCond) test(c *Ctx) bool {
	d := c.above
	if k.ceiling {
		d = c.below
	}
	lim := 1 + k.offset
	if k.addSurface {
		lim += c.depth
	}
	if k.secondary != 0 {
		t := (c.secondary() + 1) / 2
		lim += int32(t * float64(k.secondary))
	}
	return d <= lim
}
