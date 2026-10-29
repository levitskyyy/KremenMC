package feature

import (
	"encoding/json"
	"strconv"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// paleMoss is the pale_moss tree decorator, fitted to
// kremenmc.probe.feat_tree.Writes (52 isolated pale oaks, writes and reads
// logged): the trunk positions are shuffled (Fisher-Yates from the end);
// with ground_probability, pale_moss_patch is placed above the first
// shuffled position on the lowest level of the list. Then every trunk
// position (decorator order) and every leaf draws a float against
// trunk_probability / leaves_probability, and an air block below it starts a
// hanging moss column.
type paleMoss struct {
	ground, trunk, leaves float32
	patch                 Feature
}

func (d paleMoss) decorate(t *treeRun, r *Random) {
	ps := t.trunkPositions()
	if len(ps) == 0 {
		return
	}
	sh := shuffledCopy(r, ps)
	if r.NextFloat() < d.ground {
		low := ps[0].Y
		for _, p := range sh {
			if p.Y == low {
				d.patch.Place(&placeCtx{lv: t.lv}, r, p.Add(Pos{0, 1, 0}))
				break
			}
		}
	}
	for _, p := range ps {
		if r.NextFloat() < d.trunk {
			d.hang(t, r, p.Add(Pos{0, -1, 0}))
		}
	}
	for _, l := range javaHashOrder(t.leaves) {
		if r.NextFloat() < d.leaves {
			d.hang(t, r, l.Add(Pos{0, -1, 0}))
		}
	}
}

// hang grows pale_hanging_moss down from an air block p: while the block
// below is air a float >= 0.5 extends it; the last block is the tip.
func (paleMoss) hang(t *treeRun, r *Random, p Pos) {
	if !data.IsAir(t.lv.Block(p)) {
		return
	}
	reg := data.Canonical()
	st, _ := data.DefaultState("pale_hanging_moss")
	mid, _ := reg.With(st, "tip", "false")
	tip, _ := reg.With(st, "tip", "true")
	for {
		b := p.Add(Pos{0, -1, 0})
		if !data.IsAir(t.lv.Block(b)) || r.NextFloat() < 0.5 {
			t.setDeco(p, tip)
			return
		}
		t.setDeco(p, mid)
		p = b
	}
}

// paleMossWired gates pale_moss (and so every pale oak) out of generation:
// its draws and blocks match the reference except the pale_moss_carpet wall
// sides and second layer set by the patch's simple_block (TestPaleOakAt).
var paleMossWired = false

func shuffledCopy(r *Random, ps []Pos) []Pos {
	s := append([]Pos(nil), ps...)
	for i := len(s); i > 1; i-- {
		j := int(r.NextIntn(int32(i)))
		s[i-1], s[j] = s[j], s[i-1]
	}
	return s
}

// creakingHeart is the creaking_heart decorator: with probability p, the
// first shuffled trunk position whose six neighbours (down, up, north,
// south, west, east) are all logs becomes a dormant natural creaking heart.
type creakingHeart struct{ p float32 }

var sixDirs = [6]Pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

func (d creakingHeart) decorate(t *treeRun, r *Random) {
	if r.NextFloat() >= d.p {
		return
	}
	_, logs := treeTags()
	for _, p := range shuffledCopy(r, t.trunkPositions()) {
		ok := true
		for _, o := range sixDirs {
			if !logs.has(t.lv.Block(p.Add(o))) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		reg := data.Canonical()
		st, _ := data.DefaultState("creaking_heart")
		for _, kv := range [][2]string{{"axis", "y"}, {"creaking_heart_state", "dormant"}, {"natural", "true"}} {
			st, _ = reg.With(st, kv[0], kv[1])
		}
		t.setDeco(p, st)
		return
	}
}

func registerPaleUnverified() {
	decoratorTypes["pale_moss"] = func(rg *registry, b json.RawMessage) (decorator, error) {
		if !paleMossWired {
			return nil, unsupported("tree_decorator", "pale_moss")
		}
		var p struct {
			Ground float32 `json:"ground_probability"`
			Trunk  float32 `json:"trunk_probability"`
			Leaves float32 `json:"leaves_probability"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		patch, err := rg.parseFeature(json.RawMessage(strconv.Quote("minecraft:pale_moss_patch")))
		if err != nil {
			return nil, err
		}
		return paleMoss{p.Ground, p.Trunk, p.Leaves, patch}, nil
	}
	decoratorTypes["creaking_heart"] = func(_ *registry, b json.RawMessage) (decorator, error) {
		var p struct {
			P float32 `json:"probability"`
		}
		return creakingHeart{p.P}, json.Unmarshal(b, &p)
	}
}
