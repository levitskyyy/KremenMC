// Package density implements the reference 26.3 density functions
// (net.minecraft.world.level.levelgen.densityfunction): JSON parsing of all
// 44 function types, compilation into sampler trees with the reference's
// canonical rewrites, float32 sampling (single points and volumes), the
// per-seed random state and the noise router / climate sampler.
//
// Compiled samplers are immutable and safe for concurrent use; per-call
// mutable state (caches, blending/structure hooks) lives in a Ctx value
// owned by one goroutine.
package density

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/noise"
)

// Kind is a density function type.
type Kind uint8

const (
	KConstant Kind = iota
	KBlendAlpha
	KBlendOffset
	KBeardifier
	KNoise
	KEndIslands
	KDistanceToPoint
	KGradient
	KShiftA
	KShiftB
	KShift
	KAbs
	KSquare
	KCube
	KSqrt
	KHalfNegative
	KQuarterNegative
	KReciprocal
	KNegate
	KSqueeze
	KLog
	KSign
	KFloor
	KRound
	KCeil
	KTruncate
	KAdd
	KSub
	KMul
	KDiv
	KMin
	KMax
	KPow
	KSpline
	KLerp
	KClamp
	KRangeChoice
	KIntervalSelect
	KCache
	KBlendDensity
	KInterpolated
	KSlice
	KFindTopSurface
	KOldBlendedNoise
	KRef // reference to a registry entry (HolderHolder)
)

// TypeNames are the registry ids in protocol order (Kind order).
var TypeNames = [...]string{
	"constant", "blend_alpha", "blend_offset", "beardifier", "noise", "end_outer_islands", "distance_to_point", "gradient",
	"shift_a", "shift_b", "shift", "abs", "square", "cube", "sqrt", "half_negative", "quarter_negative", "reciprocal",
	"negate", "squeeze", "log", "sign", "floor", "round", "ceil", "truncate", "add", "sub", "mul", "div", "min", "max",
	"pow", "spline", "lerp", "clamp", "range_choice", "interval_select", "cache", "blend_density", "interpolated",
	"slice", "find_top_surface", "old_blended_noise",
}

var kindByName = func() map[string]Kind {
	m := map[string]Kind{}
	for i, n := range TypeNames {
		m["minecraft:"+n] = Kind(i)
	}
	return m
}()

// Axis of gradients and slices.
type Axis uint8

const (
	AxisX Axis = iota
	AxisY
	AxisZ
)

// Tiling of a gradient.
type Tiling uint8

const (
	TileClamp Tiling = iota
	TileRepeat
	TileMirrored
)

// Metric of distance_to_point.
type Metric uint8

const (
	MetricEuclidean Metric = iota
	MetricEuclideanSquared
	MetricManhattan
	MetricChebyshev
)

// Node is a parsed density function. Fields are used per Kind:
// Args holds child functions in the reference record order.
type Node struct {
	Kind   Kind
	Value  float32 // constant
	Args   []*Node
	Noise  string              // noise key (noise, shift*)
	NoiseP *noise.NormalParams // inline noise parameters
	XZ, Y  float64             // noise scales
	I      [4]int32            // gradient coords, slice coordinate, cell sizes, point, find_top bounds
	F      [4]float32          // gradient values, clamp/range bounds
	Axis   Axis
	Tiling Tiling
	Metric Metric
	Thresh []float32
	Spline *Spline
	Blend  noise.BlendedParams
	Ref    string
}

// Spline is a cubic spline: either a constant or a multipoint over a
// coordinate function.
type Spline struct {
	Const       bool
	Value       float32
	Coordinate  *Node
	Locations   []float32
	Values      []*Spline
	Derivatives []float32
}

func qualify(id string) string {
	if strings.Contains(id, ":") {
		return id
	}
	return "minecraft:" + id
}

// Parse decodes a density function JSON value. String values are registry
// references.
func Parse(b []byte) (*Node, error) {
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("density: %w", err)
	}
	return parse(raw)
}

func parse(v any) (*Node, error) {
	switch t := v.(type) {
	case float64:
		return &Node{Kind: KConstant, Value: float32(t)}, nil
	case string:
		return &Node{Kind: KRef, Ref: qualify(t)}, nil
	case map[string]any:
		return parseObj(t)
	}
	return nil, fmt.Errorf("density: unexpected %T", v)
}

func parseObj(m map[string]any) (*Node, error) {
	ts, _ := m["type"].(string)
	k, ok := kindByName[qualify(ts)]
	if !ok {
		return nil, fmt.Errorf("density: unknown type %q", ts)
	}
	n := &Node{Kind: k}
	var err error
	child := func(name string) *Node {
		if err != nil {
			return nil
		}
		v, ok := m[name]
		if !ok {
			err = fmt.Errorf("density: %s: missing %s", ts, name)
			return nil
		}
		var c *Node
		c, err = parse(v)
		return c
	}
	optChild := func(name string) *Node {
		if _, ok := m[name]; !ok {
			return &Node{Kind: KConstant}
		}
		return child(name)
	}
	num := func(name string) float64 {
		f, ok := m[name].(float64)
		if !ok && err == nil {
			err = fmt.Errorf("density: %s: missing number %s", ts, name)
		}
		return f
	}
	noiseRef := func() {
		switch t := m["noise"].(type) {
		case string:
			n.Noise = qualify(t)
		case map[string]any:
			b, _ := json.Marshal(t)
			var p noise.NormalParams
			if e := json.Unmarshal(b, &p); e != nil {
				err = e
			}
			n.NoiseP = &p
		default:
			err = fmt.Errorf("density: %s: bad noise", ts)
		}
	}
	axis := func(name string) Axis {
		switch m[name] {
		case "x":
			return AxisX
		case "z":
			return AxisZ
		}
		return AxisY
	}
	switch k {
	case KConstant:
		n.Value = float32(num("argument"))
	case KBlendAlpha, KBlendOffset, KBeardifier, KEndIslands:
	case KNoise:
		noiseRef()
		n.XZ, n.Y = num("xz_scale"), num("y_scale")
		n.Args = []*Node{optChild("shift_x"), optChild("shift_y"), optChild("shift_z")}
	case KShiftA, KShiftB, KShift:
		noiseRef()
	case KDistanceToPoint:
		pt, _ := m["point"].([]any)
		for i := 0; i < 3 && i < len(pt); i++ {
			f, _ := pt[i].(float64)
			n.I[i] = int32(f)
		}
		switch m["metric"] {
		case "euclidean_squared":
			n.Metric = MetricEuclideanSquared
		case "manhattan":
			n.Metric = MetricManhattan
		case "chebyshev":
			n.Metric = MetricChebyshev
		}
	case KGradient:
		n.Axis = axis("axis")
		switch m["tiling"] {
		case "repeat":
			n.Tiling = TileRepeat
		case "mirrored_repeat":
			n.Tiling = TileMirrored
		}
		n.I[0], n.I[1] = int32(num("from_coordinate")), int32(num("to_coordinate"))
		n.F[0], n.F[1] = float32(num("from_value")), float32(num("to_value"))
	case KAbs, KSquare, KCube, KSqrt, KHalfNegative, KQuarterNegative, KReciprocal, KNegate, KSqueeze, KLog, KSign,
		KCache, KBlendDensity:
		n.Args = []*Node{child("input")}
	case KFloor, KRound, KCeil, KTruncate:
		n.Args = []*Node{child("input")}
		if _, ok := m["multiple"]; ok {
			n.Args = append(n.Args, child("multiple"))
		}
	case KAdd, KSub, KMul, KDiv, KMin, KMax:
		n.Args = []*Node{child("left"), child("right")}
	case KPow:
		n.Args = []*Node{child("base"), child("exponent")}
	case KSpline:
		n.Spline, err = parseSpline(m["spline"])
	case KLerp:
		n.Args = []*Node{child("alpha"), child("first"), child("second")}
	case KClamp:
		n.Args = []*Node{child("input")}
		n.F[0], n.F[1] = float32(num("min")), float32(num("max"))
	case KRangeChoice:
		n.Args = []*Node{child("input"), child("when_in_range"), child("when_out_of_range")}
		n.F[0], n.F[1] = float32(num("min_inclusive")), float32(num("max_exclusive"))
	case KIntervalSelect:
		n.Args = []*Node{child("input")}
		th, _ := m["thresholds"].([]any)
		for _, x := range th {
			f, _ := x.(float64)
			n.Thresh = append(n.Thresh, float32(f))
		}
		fs, _ := m["functions"].([]any)
		for _, x := range fs {
			if err == nil {
				var c *Node
				c, err = parse(x)
				n.Args = append(n.Args, c)
			}
		}
	case KInterpolated:
		n.Args = []*Node{child("input")}
		n.I[0], n.I[1] = int32(num("cell_size_xz")), int32(num("cell_size_y"))
	case KSlice:
		n.Axis = axis("axis")
		n.I[0] = int32(num("coordinate"))
		n.Args = []*Node{child("input")}
	case KFindTopSurface:
		n.Args = []*Node{child("density"), child("upper_bound")}
		n.I[0], n.I[1] = int32(num("lower_bound")), int32(num("cell_height"))
	case KOldBlendedNoise:
		n.Blend = noise.BlendedParams{XZScale: num("xz_scale"), YScale: num("y_scale"), XZFactor: num("xz_factor"),
			YFactor: num("y_factor"), SmearScaleMultiplier: num("smear_scale_multiplier")}
	}
	return n, err
}

func parseSpline(v any) (*Spline, error) {
	if f, ok := v.(float64); ok {
		return &Spline{Const: true, Value: float32(f)}, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("density: bad spline %T", v)
	}
	c, err := parse(m["coordinate"])
	if err != nil {
		return nil, err
	}
	s := &Spline{Coordinate: c}
	pts, _ := m["points"].([]any)
	for _, p := range pts {
		pm, _ := p.(map[string]any)
		loc, _ := pm["location"].(float64)
		der, _ := pm["derivative"].(float64)
		val, err := parseSpline(pm["value"])
		if err != nil {
			return nil, err
		}
		s.Locations = append(s.Locations, float32(loc))
		s.Derivatives = append(s.Derivatives, float32(der))
		s.Values = append(s.Values, val)
	}
	return s, nil
}
