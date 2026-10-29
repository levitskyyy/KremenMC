package density

import (
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/noise"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Compile compiles a density function against this state. Compilation is
// single-goroutine; the resulting sampler is immutable.
func (s *State) Compile(n *Node) (Sampler, error) {
	c := &compiler{s: s, refs: map[string]Sampler{}, nodes: map[*Node]Sampler{}}
	return c.compile(n)
}

type compiler struct {
	s     *State
	refs  map[string]Sampler
	nodes map[*Node]Sampler
	err   error
}

func (c *compiler) fail(err error) Sampler {
	if c.err == nil {
		c.err = err
	}
	return constS{}
}

func (c *compiler) compile(n *Node) (Sampler, error) {
	out := c.node(n)
	return out, c.err
}

func isConst(n *Node) bool { return n.Kind == KConstant }

// resolve follows references to the underlying node.
func (c *compiler) resolve(n *Node) *Node {
	for n.Kind == KRef {
		m, err := c.s.Function(n.Ref)
		if err != nil {
			c.fail(err)
			return &Node{Kind: KConstant}
		}
		n = m
	}
	return n
}

func (c *compiler) node(n *Node) Sampler {
	if n.Kind == KRef {
		if v, ok := c.refs[n.Ref]; ok {
			return v
		}
		v := c.node(c.resolve(n))
		c.refs[n.Ref] = v
		return v
	}
	if v, ok := c.nodes[n]; ok {
		return v
	}
	v := c.build(n)
	c.nodes[n] = v
	return v
}

func (c *compiler) noiseOf(n *Node) noise.Noise {
	if n.NoiseP != nil {
		c.fail(fmt.Errorf("density: inline noise parameters are not supported"))
		return noise.NewStack(nil)
	}
	var v noise.Noise
	var err error
	if c.s.Legacy && (n.Noise == "minecraft:nether/temperature" || n.Noise == "minecraft:nether/vegetation") {
		v, err = c.s.legacyBiomeNoise(n.Noise)
	} else {
		v, err = c.s.Noise(n.Noise)
	}
	if err != nil {
		c.fail(err)
		return noise.NewStack(nil)
	}
	return v
}

func (c *compiler) build(n *Node) Sampler {
	a := func(i int) Sampler { return c.node(n.Args[i]) }
	arg := func(i int) *Node { return c.resolve(n.Args[i]) }
	switch n.Kind {
	case KConstant:
		return constS{n.Value}
	case KBlendAlpha:
		return hookS{KBlendAlpha, 1}
	case KBlendOffset, KBeardifier:
		return hookS{n.Kind, 0}
	case KNoise:
		nz := c.noiseOf(n)
		sx, sy, sz := arg(0), arg(1), arg(2)
		if isConst(sx) && sx.Value == 0 && isConst(sy) && sy.Value == 0 && isConst(sz) && sz.Value == 0 {
			return noiseS{nz, n.XZ, n.Y}
		}
		return shiftedS{a(0), a(1), a(2), nz, n.XZ, n.Y}
	case KShiftA:
		return constBinS{KMul, noiseS{c.noiseOf(n), 0.25, 0}, 4}
	case KShift:
		return constBinS{KMul, noiseS{c.noiseOf(n), 0.25, 0.25}, 4}
	case KShiftB:
		return shiftBS{c.noiseOf(n)}
	case KEndIslands:
		return newEndIslands(c.s.Seed)
	case KDistanceToPoint:
		return distanceS{n.I[0], n.I[1], n.I[2], n.Metric}
	case KGradient:
		g := gradientS{axis: n.Axis, tiling: n.Tiling, from: n.I[0], lo: min(n.I[0], n.I[1]), hi: max(n.I[0], n.I[1]),
			rng: n.I[1] - n.I[0], fromValue: n.F[0]}
		g.factor = (n.F[1] - n.F[0]) / float32(n.I[1]-n.I[0])
		return g
	case KAbs, KSquare, KCube, KSqrt, KReciprocal, KNegate, KSqueeze, KLog, KSign:
		return unaryS{kind: n.Kind, in: a(0)}
	case KHalfNegative:
		return unaryS{n.Kind, a(0), 0.5}
	case KQuarterNegative:
		return unaryS{n.Kind, a(0), 0.25}
	case KFloor, KRound, KCeil, KTruncate:
		r := roundS{kind: n.Kind, in: a(0)}
		if len(n.Args) > 1 {
			r.mult = a(1)
		}
		return r
	case KAdd, KSub, KMul, KDiv, KMin, KMax:
		return c.binary(n)
	case KPow:
		b, e := arg(0), arg(1)
		switch {
		case isConst(e) && e.Value == 2:
			return unaryS{kind: KSquare, in: a(0)}
		case isConst(e) && e.Value == 0.5:
			return unaryS{kind: KSqrt, in: a(0)}
		case isConst(b):
			return powS{exp: a(1), cBase: float64(b.Value), mode: 1}
		case isConst(e):
			return powS{base: a(0), cExp: float64(e.Value), mode: 2}
		}
		return powS{base: a(0), exp: a(1)}
	case KSpline:
		return c.spline(n.Spline)
	case KLerp:
		al, f, s := arg(0), arg(1), arg(2)
		switch {
		case isConst(f):
			return lerpS{a: a(0), s: a(2), cf: f.Value, mode: 1}
		case isConst(s):
			return lerpS{a: a(0), f: a(1), cs: s.Value, mode: 2}
		}
		_ = al
		return lerpS{a: a(0), f: a(1), s: a(2)}
	case KClamp:
		return clampS{a(0), n.F[0], n.F[1]}
	case KRangeChoice:
		return rangeS{a(0), n.F[0], n.F[1], a(1), a(2)}
	case KIntervalSelect:
		fs := make([]Sampler, len(n.Args)-1)
		for i := range fs {
			fs[i] = a(i + 1)
		}
		return intervalS{a(0), n.Thresh, fs}
	case KCache:
		id := c.s.nextID
		c.s.nextID++
		return cacheS{id, a(0)}
	case KBlendDensity:
		return blendDensityS{a(0)}
	case KInterpolated:
		id := c.s.nextID
		c.s.nextID++
		return interpS{id, a(0), n.I[0], n.I[1], 1 / float32(n.I[0]), 1 / float32(n.I[1])}
	case KSlice:
		return sliceS{a(0), n.Axis, n.I[0]}
	case KFindTopSurface:
		return topSurfaceS{a(0), a(1), n.I[0], n.I[1]}
	case KOldBlendedNoise:
		var r rng.Source
		if c.s.Legacy {
			r = rng.NewLegacy(c.s.Seed)
		} else {
			r = c.s.pos.FromHashOf(noise.BlendedNoiseSeed)
		}
		b := n.Blend.NewBlended(r)
		lxz, ly, mxz, my := b.Scales()
		alpha := clampS{constBinS{KAdd, noiseS{b.Main, mxz, my}, 0.5}, 0, 1}
		return lerpS{a: alpha, f: noiseS{b.MinLimit, lxz, ly}, s: noiseS{b.MaxLimit, lxz, ly}}
	}
	return c.fail(fmt.Errorf("density: cannot compile kind %d", n.Kind))
}

func (c *compiler) binary(n *Node) Sampler {
	l, r := c.resolve(n.Args[0]), c.resolve(n.Args[1])
	switch {
	case isConst(l) && (n.Kind == KSub || n.Kind == KDiv):
		return constBinS{n.Kind, c.node(n.Args[1]), l.Value}
	case isConst(l):
		return constBinS{n.Kind, c.node(n.Args[1]), l.Value}
	case isConst(r):
		switch n.Kind {
		case KSub:
			return constBinS{KAdd, c.node(n.Args[0]), -r.Value}
		case KDiv:
			return constBinS{KMul, c.node(n.Args[0]), 1 / r.Value}
		}
		return constBinS{n.Kind, c.node(n.Args[0]), r.Value}
	}
	b := binS{kind: n.Kind, l: c.node(n.Args[0]), r: c.node(n.Args[1])}
	switch n.Kind {
	case KMin:
		b.rb = negInf
	case KMax:
		b.rb = posInf
	}
	return b
}

var negInf, posInf = float32(-1) / zero32, float32(1) / zero32

var zero32 float32

func (c *compiler) spline(sp *Spline) *splineS {
	if sp.Const {
		return &splineS{value: sp.Value}
	}
	out := &splineS{coord: c.node(sp.Coordinate), locations: sp.Locations, derivatives: sp.Derivatives}
	for _, v := range sp.Values {
		out.values = append(out.values, c.spline(v))
	}
	return out
}

var newEndIslands = func(seed int64) Sampler {
	r := rng.NewLegacy(seed)
	r.Consume(17292)
	return endIslandS{noise.NewSimplexOpt(r, true)}
}
