package feature

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/carver"
)

// ruleTest is a structure-processor style rule test (predicate_type) as
// used by ore targets. Draws only for the random_* tests.
type ruleTest interface {
	test(st data.StateID, p Pos, r *Random) bool
}

type ruleAlways struct{}

func (ruleAlways) test(data.StateID, Pos, *Random) bool { return true }

type ruleBlocks struct{ set blockSet }

func (m ruleBlocks) test(st data.StateID, _ Pos, _ *Random) bool { return m.set.has(st) }

type ruleExactState struct{ st data.StateID }

func (m ruleExactState) test(st data.StateID, _ Pos, _ *Random) bool { return st == m.st }

type ruleRandomBlocks struct {
	set    blockSet
	chance float32
}

func (m ruleRandomBlocks) test(st data.StateID, _ Pos, r *Random) bool {
	return m.set.has(st) && r.NextFloat() < m.chance
}

type ruleRandomState struct {
	st     data.StateID
	chance float32
}

func (m ruleRandomState) test(st data.StateID, _ Pos, r *Random) bool {
	return st == m.st && r.NextFloat() < m.chance
}

type ruleHeight struct{ min, max int }

func (m ruleHeight) test(_ data.StateID, p Pos, _ *Random) bool { return p.Y >= m.min && p.Y <= m.max }

type ruleNot struct{ r ruleTest }

func (m ruleNot) test(st data.StateID, p Pos, r *Random) bool { return !m.r.test(st, p, r) }

type ruleAll []ruleTest

func (m ruleAll) test(st data.StateID, p Pos, r *Random) bool {
	for _, t := range m {
		if !t.test(st, p, r) {
			return false
		}
	}
	return true
}

type ruleAny []ruleTest

func (m ruleAny) test(st data.StateID, p Pos, r *Random) bool {
	for _, t := range m {
		if t.test(st, p, r) {
			return true
		}
	}
	return false
}

func (rg *registry) parseRuleTest(b json.RawMessage) (ruleTest, error) {
	var p struct {
		Type        string            `json:"predicate_type"`
		Tag         string            `json:"tag"`
		Block       string            `json:"block"`
		State       json.RawMessage   `json:"block_state"`
		Probability float32           `json:"probability"`
		Min         int               `json:"min_inclusive"`
		Max         int               `json:"max_inclusive"`
		Rule        json.RawMessage   `json:"rule"`
		Rules       []json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("rule test: %w", err)
	}
	list := func() ([]ruleTest, error) {
		var out []ruleTest
		for _, s := range p.Rules {
			t, err := rg.parseRuleTest(s)
			if err != nil {
				return nil, err
			}
			out = append(out, t)
		}
		return out, nil
	}
	switch p.Type {
	case "minecraft:always_true":
		return ruleAlways{}, nil
	case "minecraft:tag_match":
		s, err := rg.tag(p.Tag)
		return ruleBlocks{s}, err
	case "minecraft:block_match":
		s, err := rg.blocks([]string{p.Block})
		return ruleBlocks{s}, err
	case "minecraft:random_block_match":
		s, err := rg.blocks([]string{p.Block})
		return ruleRandomBlocks{s, p.Probability}, err
	case "minecraft:blockstate_match":
		st, err := parseBlockState(p.State)
		return ruleExactState{st}, err
	case "minecraft:random_blockstate_match":
		st, err := parseBlockState(p.State)
		return ruleRandomState{st, p.Probability}, err
	case "minecraft:height_match":
		return ruleHeight{p.Min, p.Max}, nil
	case "minecraft:not":
		t, err := rg.parseRuleTest(p.Rule)
		return ruleNot{t}, err
	case "minecraft:all_of":
		l, err := list()
		return ruleAll(l), err
	case "minecraft:any_of":
		l, err := list()
		return ruleAny(l), err
	}
	return nil, unsupported("rule_test", p.Type)
}

type oreTarget struct {
	test  ruleTest
	state data.StateID
}

type oreConfig struct {
	size    int
	discard float32
	targets []oreTarget
}

func (rg *registry) parseOreConfig(b json.RawMessage) (oreConfig, error) {
	var p struct {
		Size    int     `json:"size"`
		Discard float32 `json:"discard_chance_on_air_exposure"`
		Targets []struct {
			State  json.RawMessage `json:"state"`
			Target json.RawMessage `json:"target"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return oreConfig{}, fmt.Errorf("ore: %w", err)
	}
	c := oreConfig{size: p.Size, discard: p.Discard}
	for _, t := range p.Targets {
		st, err := parseBlockState(t.State)
		if err != nil {
			return c, err
		}
		rt, err := rg.parseRuleTest(t.Target)
		if err != nil {
			return c, err
		}
		c.targets = append(c.targets, oreTarget{rt, st})
	}
	return c, nil
}

// canWrite is WorldGenRegion.ensureCanWrite for the column and height.
func (lv *Level) canWrite(p Pos) bool {
	if p.Y < lv.MinY() || p.Y >= lv.MaxY() {
		return false
	}
	return lv.sc == nil || lv.sc.InWriteZone(p.X, p.Z)
}

func (lv *Level) touchesAir(p Pos) bool {
	for _, d := range [6]Pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		if data.IsAir(lv.Block(p.Add(d))) {
			return true
		}
	}
	return false
}

// tryPlace writes the first target whose rule matches at p. A rule match
// draws the air-exposure float when the discard chance is strictly
// between 0 and 1, whether or not p touches air.
func (c *oreConfig) tryPlace(lv *Level, r *Random, p Pos) bool {
	st := lv.Block(p)
	for _, t := range c.targets {
		if !t.test.test(st, p, r) {
			continue
		}
		keep := true
		switch {
		case c.discard <= 0:
		case c.discard >= 1:
			keep = !lv.touchesAir(p)
		default:
			keep = r.NextFloat() >= c.discard || !lv.touchesAir(p)
		}
		if keep {
			lv.SetBlock(p, t.state)
			return true
		}
	}
	return false
}

type oreFeature struct{ oreConfig }

func (o oreFeature) Place(c *placeCtx, r *Random, p Pos) bool {
	size := float32(o.size)
	ang := r.NextFloat() * math.Pi
	g := size / 8
	ext := int(math.Ceil(float64((size/16*2 + 1) / 2)))
	sin, cos := float64(carver.Sin(float64(ang))*g), float64(carver.Cos(float64(ang))*g)
	x1, x2 := float64(p.X)+sin, float64(p.X)-sin
	z1, z2 := float64(p.Z)+cos, float64(p.Z)-cos
	y1 := float64(p.Y + int(r.NextIntn(3)) - 2)
	y2 := float64(p.Y + int(r.NextIntn(3)) - 2)
	cg := int(math.Ceil(float64(g)))
	ox, oy, oz := p.X-cg-ext, p.Y-2-ext, p.Z-cg-ext
	w, h := 2*(cg+ext), 2*(2+ext)
	for x := ox; x <= ox+w; x++ {
		for z := oz; z <= oz+w; z++ {
			if oy <= c.lv.Height(world.OceanFloorWG, x, z) {
				return o.blob(c.lv, r, x1, x2, y1, y2, z1, z2, ox, oy, oz, w, h)
			}
		}
	}
	return false
}

func (o oreFeature) blob(lv *Level, r *Random, x1, x2, y1, y2, z1, z2 float64, ox, oy, oz, w, h int) bool {
	n := o.size
	sp := make([][4]float64, n)
	for i := range n {
		t := float32(i) / float32(n)
		cx := x1 + float64(t)*(x2-x1)
		cy := y1 + float64(t)*(y2-y1)
		cz := z1 + float64(t)*(z2-z1)
		rr := r.NextDouble() * float64(n) / 16
		d := (float64(carver.Sin(float64(math.Pi*t))+1)*rr + 1) / 2
		sp[i] = [4]float64{cx, cy, cz, d}
	}
	for i := 0; i < n-1; i++ {
		if sp[i][3] <= 0 {
			continue
		}
		for j := i + 1; j < n; j++ {
			if sp[j][3] <= 0 {
				continue
			}
			dx, dy, dz := sp[i][0]-sp[j][0], sp[i][1]-sp[j][1], sp[i][2]-sp[j][2]
			dr := sp[i][3] - sp[j][3]
			if dr*dr > dx*dx+dy*dy+dz*dz {
				if dr > 0 {
					sp[j][3] = -1
				} else {
					sp[i][3] = -1
				}
			}
		}
	}
	// The index aliases across rows at the inclusive far edges, as the
	// reference's does; size covers the largest alias.
	done := make([]bool, w*h*w+h*w+w+1)
	count := 0
	for _, s := range sp {
		d := s[3]
		if d < 0 {
			continue
		}
		lo := [3]int{max(floor(s[0]-d), ox), max(floor(s[1]-d), oy), max(floor(s[2]-d), oz)}
		hi := [3]int{max(floor(s[0]+d), lo[0]), max(floor(s[1]+d), lo[1]), max(floor(s[2]+d), lo[2])}
		for x := lo[0]; x <= hi[0]; x++ {
			ex := (float64(x) + 0.5 - s[0]) / d
			if ex*ex >= 1 {
				continue
			}
			for y := lo[1]; y <= hi[1]; y++ {
				ey := (float64(y) + 0.5 - s[1]) / d
				if ex*ex+ey*ey >= 1 {
					continue
				}
				for z := lo[2]; z <= hi[2]; z++ {
					ez := (float64(z) + 0.5 - s[2]) / d
					if ex*ex+ey*ey+ez*ez >= 1 {
						continue
					}
					q := Pos{x, y, z}
					if y < lv.MinY() || y >= lv.MaxY() {
						continue
					}
					idx := x - ox + (y-oy)*w + (z-oz)*w*h
					if done[idx] {
						continue
					}
					done[idx] = true
					if !lv.canWrite(q) {
						continue
					}
					if o.tryPlace(lv, r, q) {
						count++
					}
				}
			}
		}
	}
	return count > 0
}

func floor(v float64) int { return int(math.Floor(v)) }

func init() {
	featureTypes["ore"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		c, err := rg.parseOreConfig(b)
		return oreFeature{c}, err
	}
}
