package feature

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// Coral shapes and the data-driven decoration around them, inferred from
// draw logs of tools/refprobe kremenmc.probe.feat_ocean.Place (see
// testdata/feat_ocean_place.txt). In 26.3 coral_tree and coral_claw only
// lay out positions; each position runs the configured placed feature.

// coralDirs is the reference horizontal plane order.
var coralDirs = [4]Pos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}

var coralUp = Pos{0, 1, 0}

func shuffleDirs(r *Random, d []Pos) {
	for i := len(d); i > 1; i-- {
		j := r.NextIntn(int32(i))
		d[i-1], d[j] = d[j], d[i-1]
	}
}

type coralTree struct{ block *Placed }

func (t coralTree) Place(c *placeCtx, r *Random, p Pos) bool {
	h := int(r.NextIntn(3)) + 1
	for range h {
		if !t.block.place(c.lv, r, p, nil) {
			return true
		}
		p = p.Add(coralUp)
	}
	top := p.Add(Pos{0, -1, 0})
	n := int(r.NextIntn(3)) + 2
	dirs := coralDirs
	shuffleDirs(r, dirs[:])
	for _, d := range dirs[:n] {
		q := top
		ext := int(r.NextIntn(5))
		ok := true
		for range 2 {
			q = q.Add(d).Add(coralUp)
			if ok = t.block.place(c.lv, r, q, nil); !ok {
				break
			}
		}
		turn, turned := false, false
		for i := 0; ok && i < ext; i++ {
			q = q.Add(coralUp)
			if turn {
				q = q.Add(d)
				turn, turned = false, true
			} else {
				turned = false
			}
			if !t.block.place(c.lv, r, q, nil) {
				break
			}
			if !turned && r.NextFloat() < 0.25 {
				turn = true
			}
		}
	}
	return true
}

// overlay places every feature at the origin in order.
type overlay []*Placed

// overlay places every feature at the origin in order; the reference
// reports true even when nothing was placed.
func (o overlay) Place(c *placeCtx, r *Random, p Pos) bool {
	for _, f := range o {
		f.place(c.lv, r, p, nil)
	}
	return true
}

// randomBlock picks a default state from an ordered block list.
type randomBlock []data.StateID

func (b randomBlock) State(_ *Level, r *Random, _ Pos) data.StateID {
	return b[r.NextIntn(int32(len(b)))]
}

// rotatedState rotates the inner state's horizontal facing by the rotation
// that maps north onto dir.
type rotatedState struct {
	inner StateProvider
	turns int
}

var facingNames = [4]string{"north", "east", "south", "west"}

func (s rotatedState) State(lv *Level, r *Random, p Pos) data.StateID {
	st := s.inner.State(lv, r, p)
	reg := data.Canonical()
	f, ok := reg.Value(st, "facing")
	if !ok {
		return st
	}
	for i, n := range facingNames {
		if n == f {
			if out, ok := reg.With(st, "facing", facingNames[(i+s.turns)%4]); ok {
				return out
			}
		}
	}
	return st
}

// orderedBlocks resolves block ids and tags in data-pack order, first
// occurrence wins.
func (rg *registry) orderedBlocks(names []string, seen map[string]bool, out []data.StateID) ([]data.StateID, error) {
	for _, n := range names {
		if t, ok := strings.CutPrefix(n, "#"); ok {
			if !strings.Contains(t, ":") {
				t = "minecraft:" + t
			}
			ids, ok := rg.pack.Tag("block", t)
			if !ok {
				return nil, fmt.Errorf("block tag %s: missing", t)
			}
			var err error
			if out, err = rg.orderedBlocks(ids, seen, out); err != nil {
				return nil, err
			}
			continue
		}
		name := strings.TrimPrefix(n, "minecraft:")
		if seen[name] {
			continue
		}
		seen[name] = true
		st, ok := data.DefaultState(name)
		if !ok {
			return nil, fmt.Errorf("unknown block %s", n)
		}
		out = append(out, st)
	}
	return out, nil
}

func init() {
	featureTypes["coral_tree"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var p struct {
			Feature json.RawMessage `json:"feature"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("coral_tree: %w", err)
		}
		pf, err := rg.parsePlaced(p.Feature)
		return coralTree{pf}, err
	}
	featureTypes["coral_claw"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var p struct {
			Feature json.RawMessage `json:"feature"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("coral_claw: %w", err)
		}
		pf, err := rg.parsePlaced(p.Feature)
		return coralClaw{pf}, err
	}
	featureTypes["overlay"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var p struct {
			Features []json.RawMessage `json:"features"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("overlay: %w", err)
		}
		var o overlay
		for _, raw := range p.Features {
			pf, err := rg.parsePlaced(raw)
			if err != nil {
				return nil, err
			}
			o = append(o, pf)
		}
		return o, nil
	}
	providerTypes["random_block"] = func(rg *registry, b json.RawMessage) (StateProvider, error) {
		var p struct {
			Blocks json.RawMessage `json:"blocks"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("random_block: %w", err)
		}
		names, err := stringList(p.Blocks)
		if err != nil {
			return nil, fmt.Errorf("random_block: %w", err)
		}
		out, err := rg.orderedBlocks(names, map[string]bool{}, nil)
		if err != nil {
			return nil, fmt.Errorf("random_block: %w", err)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("random_block: empty")
		}
		return randomBlock(out), nil
	}
	providerTypes["rotated"] = func(rg *registry, b json.RawMessage) (StateProvider, error) {
		var p struct {
			Direction string          `json:"direction"`
			State     json.RawMessage `json:"state"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("rotated: %w", err)
		}
		in, err := rg.parseStateProvider(p.State)
		if err != nil {
			return nil, err
		}
		for i, n := range facingNames {
			if n == p.Direction {
				return rotatedState{in, i}, nil
			}
		}
		return nil, fmt.Errorf("rotated: direction %q", p.Direction)
	}
}

type coralClaw struct{ block *Placed }

func (t coralClaw) Place(c *placeCtx, r *Random, p Pos) bool {
	if !t.block.place(c.lv, r, p, nil) {
		return false
	}
	di := int(r.NextIntn(4))
	dir := coralDirs[di]
	n := int(r.NextIntn(2)) + 2
	arms := []Pos{dir, coralDirs[(di+1)%4], coralDirs[(di+3)%4]}
	shuffleDirs(r, arms)
	for _, d := range arms[:n] {
		q := p
		a := int(r.NextIntn(2))
		first, step, ext := d, d, 2
		if d != dir {
			if r.NextIntn(2) == 1 {
				step = coralUp
			}
			ext = 3
			first = d.Add(coralUp)
		}
		ext += int(r.NextIntn(3))
		// A failed lead block ends the lead; the arm then rises from the
		// last block placed.
		for i := 0; i <= a; i++ {
			mv := step
			if i == 0 {
				mv = first
			}
			if !t.block.place(c.lv, r, q.Add(mv), nil) {
				break
			}
			q = q.Add(mv)
		}
		rise := true
		for i := 0; i < ext; i++ {
			q = q.Add(dir)
			if rise {
				q = q.Add(coralUp)
			}
			if !t.block.place(c.lv, r, q, nil) {
				break
			}
			rise = r.NextFloat() < 0.25
		}
	}
	return true
}
