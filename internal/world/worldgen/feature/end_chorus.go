package feature

import (
	"encoding/json"
	"strconv"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// chorusPlant is chorus_plant. From kremenmc.probe.feat_nether.Place draw,
// read and write logs (30 end scenes):
//   - The origin must be air on end stone (supports_chorus_plant); it gets
//     a plant and grows a tree from it at depth 0 with the origin as root.
//   - grow(pos, depth): a stem of nextInt(4)+1 blocks (+1 at depth 0) above
//     pos; each stem block needs its four horizontal neighbours air, else
//     the whole call ends there (no branches, no flower). Below depth 4 it
//     draws nextInt(4) branches (+1 at depth 0), each a direction drawn as
//     nextInt(4) over north, east, south, west from the stem top; a branch
//     block needs |dx|, |dz| < 8 from the root, itself and the block below
//     it air and its three other horizontal neighbours air, and then grows
//     at depth+1. When no branch was placed the stem top becomes a
//     chorus_flower of age 5.
//   - Every new plant block is written with its connections at that time
//     (a neighbour that is a chorus plant or flower, end stone below), and
//     the block it grew from is rewritten with fresh connections.
type chorusPlant struct{}

var (
	chorusFlower5, _ = data.Canonical().ParseState("chorus_flower[age=5]")
	chorusBlocks     = sync.OnceValue(func() [3]data.BlockID {
		reg := data.Canonical()
		p, _ := reg.BlockByName("chorus_plant")
		f, _ := reg.BlockByName("chorus_flower")
		e, _ := reg.BlockByName("end_stone")
		return [3]data.BlockID{p.ID, f.ID, e.ID}
	})
)

const chorusSpread = 8

func (chorusPlant) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	if !data.IsAir(lv.Block(p)) || !isChorusSupport(lv.Block(p.Add(Pos{0, -1, 0}))) {
		return false
	}
	setChorusPlant(lv, p)
	growChorus(lv, r, p, p, 0)
	return true
}

func isChorusSupport(s data.StateID) bool {
	return data.Canonical().BlockID(s) == chorusBlocks()[2]
}

func isChorus(s data.StateID) bool {
	b := data.Canonical().BlockID(s)
	return b == chorusBlocks()[0] || b == chorusBlocks()[1]
}

func setChorusPlant(lv *Level, p Pos) {
	conn := func(d Pos) string {
		s := lv.Block(p.Add(d))
		return strconv.FormatBool(isChorus(s) || (d.Y < 0 && isChorusSupport(s)))
	}
	st, _ := data.Canonical().StateFrom("chorus_plant", func(k string) (string, bool) {
		switch k {
		case "north":
			return conn(Pos{0, 0, -1}), true
		case "east":
			return conn(Pos{1, 0, 0}), true
		case "south":
			return conn(Pos{0, 0, 1}), true
		case "west":
			return conn(Pos{-1, 0, 0}), true
		case "up":
			return conn(Pos{0, 1, 0}), true
		case "down":
			return conn(Pos{0, -1, 0}), true
		}
		return "", false
	})
	lv.SetBlock(p, st)
}

// chorusSidesAir reports whether the horizontal neighbours of p, except
// the one at skip (index into horizontals, -1 for none), are air.
func chorusSidesAir(lv *Level, p Pos, skip int) bool {
	for i, d := range horizontals {
		if i != skip && !data.IsAir(lv.Block(p.Add(d))) {
			return false
		}
	}
	return true
}

func growChorus(lv *Level, r *Random, pos, root Pos, depth int) {
	n := int(r.NextIntn(4)) + 1
	if depth == 0 {
		n++
	}
	top := pos
	for i := 1; i <= n; i++ {
		q := pos.Add(Pos{0, i, 0})
		if !chorusSidesAir(lv, q, -1) {
			return
		}
		setChorusPlant(lv, q)
		setChorusPlant(lv, q.Add(Pos{0, -1, 0}))
		top = q
	}
	branched := false
	if depth < 4 {
		k := int(r.NextIntn(4))
		if depth == 0 {
			k++
		}
		for range k {
			di := int(r.NextIntn(4))
			b := top.Add(horizontals[di])
			if abs(b.X-root.X) < chorusSpread && abs(b.Z-root.Z) < chorusSpread &&
				data.IsAir(lv.Block(b)) && data.IsAir(lv.Block(b.Add(Pos{0, -1, 0}))) &&
				chorusSidesAir(lv, b, (di+2)%4) {
				branched = true
				setChorusPlant(lv, b)
				setChorusPlant(lv, top)
				growChorus(lv, r, b, root, depth+1)
			}
		}
	}
	if !branched {
		lv.SetBlock(top, chorusFlower5)
	}
}

func init() {
	featureTypes["chorus_plant"] = func(*registry, json.RawMessage) (Feature, error) { return chorusPlant{}, nil }
}
