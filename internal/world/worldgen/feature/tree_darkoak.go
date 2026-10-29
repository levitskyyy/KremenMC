package feature

import "encoding/json"

// darkOakTrunk is dark_oak_trunk_placer, fitted to kremenmc.probe.feat_tree.Draws
// (testdata/trees_placers.txt): a 2×2 trunk that leans 2 − nextInt(3)
// blocks in direction nextInt(4) (N, E, S, W), one block per level from
// h − nextInt(4); then each of the 12 positions around the 2×2 (x −1..2,
// z −1..2) gets a branch column when nextInt(3) == 0, nextInt(3)+2 logs
// high ending at y h−2.
type darkOakTrunk struct{ trunkHeight }

var square2 = [4]Pos{{0, 0, 0}, {1, 0, 0}, {0, 0, 1}, {1, 0, 1}}

func (d darkOakTrunk) place(t *treeRun, r *Random, h int, p Pos) []attachment {
	for _, o := range square2 {
		t.setDirt(r, p.Add(o).Add(Pos{0, -1, 0}))
	}
	dir := horizontals[r.NextIntn(4)]
	bend := h - int(r.NextIntn(4))
	lean := 2 - int(r.NextIntn(3))
	x, z := 0, 0
	for i := range h {
		if i >= bend && lean > 0 {
			x += dir.X
			z += dir.Z
			lean--
		}
		for _, o := range square2 {
			t.placeLog(r, p.Add(Pos{x + o.X, i, z + o.Z}))
		}
	}
	for dx := -1; dx <= 2; dx++ {
		for dz := -1; dz <= 2; dz++ {
			if dx >= 0 && dx <= 1 && dz >= 0 && dz <= 1 {
				continue
			}
			if r.NextIntn(3) != 0 {
				continue
			}
			n := int(r.NextIntn(3)) + 2
			for k := range n {
				t.placeLog(r, p.Add(Pos{dx, h - 2 - k, dz}))
			}
			t.branchTops = append(t.branchTops, p.Add(Pos{dx, h - 2, dz}))
		}
	}
	return []attachment{{pos: p.Add(Pos{x, h, z}), double: true}}
}

// darkOakFoliage is dark_oak_foliage_placer as configured (radius 0,
// offset 0), relative to the top 2×2's north-west log one block up:
//   - y+1: the 2×2 when nextBoolean();
//   - y: x, z in −2..3 with ax+az <= 2 (a = x−1 for x > 0, else −x);
//   - y−1: x, z in −3..4 unless both x and z are outside −2..2;
//   - y−2: x, z in −2..3;
//   - around every branch top: 5×5 without corners at its level, 3×3 above.
type darkOakFoliage struct{}

func (darkOakFoliage) foliageHeight(*Random, int) int { return 0 }
func (darkOakFoliage) foliageRadius(*Random, int) int { return 0 }

func (darkOakFoliage) place(t *treeRun, r *Random, _ int, a attachment, _, _ int) {
	c := a.pos
	if r.NextBool() {
		for _, o := range square2 {
			t.placeLeaf(r, c.Add(Pos{o.X, 1, o.Z}))
		}
	}
	da := func(v int) int {
		if v > 0 {
			return v - 1
		}
		return -v
	}
	for x := -2; x <= 3; x++ {
		for z := -2; z <= 3; z++ {
			if da(x)+da(z) <= 2 {
				t.placeLeaf(r, c.Add(Pos{x, 0, z}))
			}
		}
	}
	for x := -3; x <= 4; x++ {
		for z := -3; z <= 4; z++ {
			if (x < -2 || x > 2) && (z < -2 || z > 2) {
				continue
			}
			t.placeLeaf(r, c.Add(Pos{x, -1, z}))
		}
	}
	for x := -2; x <= 3; x++ {
		for z := -2; z <= 3; z++ {
			t.placeLeaf(r, c.Add(Pos{x, -2, z}))
		}
	}
	for _, b := range t.branchTops {
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				if abs(x) == 2 && abs(z) == 2 {
					continue
				}
				t.placeLeaf(r, b.Add(Pos{x, 0, z}))
			}
		}
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				t.placeLeaf(r, b.Add(Pos{x, 1, z}))
			}
		}
	}
}

func init() {
	trunkTypes["dark_oak_trunk_placer"] = func(b json.RawMessage) (trunkPlacer, error) {
		var d darkOakTrunk
		return d, json.Unmarshal(b, &d.trunkHeight)
	}
	foliageTypes["dark_oak_foliage_placer"] = func(json.RawMessage) (foliagePlacer, error) {
		return darkOakFoliage{}, nil
	}
}
