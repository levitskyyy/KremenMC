package feature

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// speleothem is the 26.x speleothem feature (pointed dripstone, sulfur
// spikes). Fitted to kremenmc.probe.feat_ow.Place draw logs: the tip points
// away from the one non-empty neighbour (above or below; both: nextBoolean,
// true is down), a patch of base blocks grows around the block behind the
// tip, and the column is two blocks long when nextFloat < 0.2.
type speleothem struct {
	base, pointed data.StateID
	replace       blockSet
}

type speleothemConfig struct {
	Base    string          `json:"base_block"`
	Pointed string          `json:"pointed_block"`
	Replace json.RawMessage `json:"replaceable_blocks"`
}

func (rg *registry) parseSpeleothemBlocks(c speleothemConfig) (base, pointed data.StateID, rep blockSet, err error) {
	var ok bool
	if base, ok = data.DefaultState(strings.TrimPrefix(c.Base, "minecraft:")); !ok {
		return 0, 0, nil, fmt.Errorf("speleothem: unknown block %s", c.Base)
	}
	if pointed, ok = data.DefaultState(strings.TrimPrefix(c.Pointed, "minecraft:")); !ok {
		return 0, 0, nil, fmt.Errorf("speleothem: unknown block %s", c.Pointed)
	}
	var names []string
	var one string
	if json.Unmarshal(c.Replace, &one) == nil {
		names = []string{one}
	} else if err := json.Unmarshal(c.Replace, &names); err != nil {
		return 0, 0, nil, fmt.Errorf("speleothem: replaceable_blocks: %w", err)
	}
	rep, err = rg.blocks(names)
	return base, pointed, rep, err
}

func parseSpeleothem(rg *registry, b json.RawMessage) (Feature, error) {
	var c speleothemConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("speleothem: %w", err)
	}
	base, pointed, rep, err := rg.parseSpeleothemBlocks(c)
	if err != nil {
		return nil, err
	}
	return speleothem{base, pointed, rep}, nil
}

// spEmptyOrWater: the neighbour test of the tip direction.
func spEmptyOrWater(st data.StateID) bool {
	return data.IsAir(st) || fluidOf(st) == "water" || fluidOf(st) == "flowing_water"
}

func (f speleothem) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	upFree := spEmptyOrWater(lv.Block(p.Add(Pos{0, 1, 0})))
	downFree := spEmptyOrWater(lv.Block(p.Add(Pos{0, -1, 0})))
	var dir int
	switch {
	case !upFree && !downFree:
		dir = 1
		if r.NextBool() {
			dir = -1
		}
	case !downFree:
		dir = 1
	case !upFree:
		dir = -1
	default:
		return false
	}
	base := p.Add(Pos{0, -dir, 0})
	// The block behind the tip must be a base (base block or replaceable):
	// a pointed dripstone there fails without draws (region trace, seed
	// 8266389061079214669 chunk -39,-39).
	if st := lv.Block(base); st != f.base && !f.replace.has(st) {
		return false
	}
	spPatch(lv, r, base, f.base, f.replace)
	h := 1
	if r.NextFloat() < 0.2 {
		h = 2
	}
	spColumn(lv, p, dir, h, f.pointed)
	return true
}

func spPlaceBase(lv *Level, p Pos, base data.StateID, rep blockSet) {
	if rep.has(lv.Block(p)) {
		lv.SetBlock(p, base)
	}
}

// spHorizontal is north, east, south, west.
var spHorizontal = [4]Pos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}

func spPatch(lv *Level, r *Random, base Pos, st data.StateID, rep blockSet) {
	spPlaceBase(lv, base, st, rep)
	for _, d := range spHorizontal {
		if r.NextFloat() >= 0.7 {
			continue
		}
		q := base.Add(d)
		spPlaceBase(lv, q, st, rep)
		for range 2 {
			if r.NextFloat() >= 0.5 {
				break
			}
			q = q.Add(mfDirOff[r.NextIntn(6)])
			spPlaceBase(lv, q, st, rep)
		}
	}
}

// spColumn writes a pointed column of h blocks from p along dir (1 up, -1
// down): base, middle, frustum, tip from the base side.
func spColumn(lv *Level, p Pos, dir, h int, pointed data.StateID) {
	reg := data.Canonical()
	vd := "up"
	if dir < 0 {
		vd = "down"
	}
	for i := range h {
		q := p.Add(Pos{0, dir * i, 0})
		th := "middle"
		switch {
		case i == h-1:
			th = "tip"
		case i == h-2:
			th = "frustum"
		case i == 0:
			th = "base"
		}
		st, _ := reg.With(pointed, "thickness", th)
		st, _ = reg.With(st, "vertical_direction", vd)
		wl := "false"
		if f := fluidOf(lv.Block(q)); f == "water" || f == "flowing_water" {
			wl = "true"
		}
		st, _ = reg.With(st, "waterlogged", wl)
		lv.SetBlock(q, st)
	}
}

func init() {
	featureTypes["speleothem"] = parseSpeleothem
}
