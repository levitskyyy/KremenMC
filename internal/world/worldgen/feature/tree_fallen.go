package feature

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// fallenTree is the fallen_tree feature, fitted to kremenmc.probe.feat_tree.Draws
// (60 isolated trees, draw for draw) and to the reads of
// kremenmc.probe.feat_region.Region in real terrain:
//   - a stump (the trunk state, axis y) at the origin, decorated first by
//     the stump decorators with the stump as the only trunk position;
//   - a direction nextInt(4) over north, east, south, west, the length
//     log_length, and a gap 2 + nextInt(2): length-2 logs from the gap on;
//   - the first log drops from origin y+1 while it is free and the block
//     below it is air; every log must be free (air or replaceable by
//     trees), otherwise none is placed and the stump stays;
//   - the logs lie along the direction (axis x or z) and get the log
//     decorators with the logs as trunk positions.
type fallenTree struct {
	trunk      StateProvider
	length     IntProvider
	stump, log []decorator
}

var horizontal4 = [4]Pos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}

func (f *fallenTree) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	cfg := &tree{trunk: f.trunk}
	if !validTreePos(lv, p) {
		return false
	}
	st := withAxis(f.trunk.State(lv, r, p), "y")
	lv.SetBlock(p, st)
	stump := &treeRun{cfg: cfg, lv: lv, logs: []Pos{p}}
	for _, d := range f.stump {
		d.decorate(stump, r)
	}
	dir := horizontal4[r.NextIntn(4)]
	n := f.length.Sample(r) - 2
	gap := 2 + int(r.NextIntn(2))
	q := Pos{p.X + dir.X*gap, p.Y + 1, p.Z + dir.Z*gap}
	for validTreePos(lv, q) && data.IsAir(lv.Block(q.Add(Pos{0, -1, 0}))) && q.Y > lv.MinY() {
		q.Y--
	}
	logs := make([]Pos, 0, max(n, 0))
	for i := range n {
		l := q.Add(Pos{dir.X * i, 0, dir.Z * i})
		if !validTreePos(lv, l) {
			return true
		}
		logs = append(logs, l)
	}
	axis := "x"
	if dir.X == 0 {
		axis = "z"
	}
	run := &treeRun{cfg: cfg, lv: lv}
	for _, l := range logs {
		lv.SetBlock(l, withAxis(f.trunk.State(lv, r, l), axis))
		run.logs = append(run.logs, l)
	}
	for _, d := range f.log {
		d.decorate(run, r)
	}
	return true
}

func withAxis(st data.StateID, axis string) data.StateID {
	if s, ok := data.Canonical().With(st, "axis", axis); ok {
		return s
	}
	return st
}

// attachedToLogs: the logs in trunk-decorator order, shuffled; per log a
// direction from the list, then with probability p a block from the
// provider on that side when it is air.
type attachedToLogs struct {
	p     float32
	state StateProvider
	dirs  []Pos
}

func (a attachedToLogs) decorate(t *treeRun, r *Random) {
	ls := javaHashOrder(t.logs)
	for i := len(ls); i > 1; i-- {
		j := int(r.NextIntn(int32(i)))
		ls[i-1], ls[j] = ls[j], ls[i-1]
	}
	for _, l := range ls {
		d := a.dirs[r.NextIntn(int32(len(a.dirs)))]
		if r.NextFloat() >= a.p {
			continue
		}
		q := l.Add(d)
		if !data.IsAir(t.lv.Block(q)) {
			continue
		}
		t.setDeco(q, a.state.State(t.lv, r, q))
	}
}

var dirNames = map[string]Pos{
	"down": {0, -1, 0}, "up": {0, 1, 0}, "north": {0, 0, -1}, "south": {0, 0, 1}, "west": {-1, 0, 0}, "east": {1, 0, 0},
}

func init() {
	decoratorTypes["attached_to_logs"] = func(rg *registry, b json.RawMessage) (decorator, error) {
		var p struct {
			P     float32         `json:"probability"`
			State json.RawMessage `json:"block_provider"`
			Dirs  []string        `json:"directions"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		a := attachedToLogs{p: p.P}
		for _, d := range p.Dirs {
			o, ok := dirNames[strings.TrimPrefix(d, "minecraft:")]
			if !ok {
				return nil, fmt.Errorf("attached_to_logs: direction %q", d)
			}
			a.dirs = append(a.dirs, o)
		}
		if len(a.dirs) == 0 {
			return nil, fmt.Errorf("attached_to_logs: no directions")
		}
		var err error
		a.state, err = rg.parseStateProvider(p.State)
		return a, err
	}
	featureTypes["fallen_tree"] = func(rg *registry, b json.RawMessage) (Feature, error) {
		var p struct {
			Trunk  json.RawMessage   `json:"trunk_provider"`
			Length json.RawMessage   `json:"log_length"`
			Stump  []json.RawMessage `json:"stump_decorators"`
			Log    []json.RawMessage `json:"log_decorators"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("fallen_tree: %w", err)
		}
		f := &fallenTree{}
		var err error
		if f.trunk, err = rg.parseStateProvider(p.Trunk); err != nil {
			return nil, err
		}
		if f.length, err = parseInt(p.Length); err != nil {
			return nil, err
		}
		for _, l := range [2]struct {
			src []json.RawMessage
			dst *[]decorator
		}{{p.Stump, &f.stump}, {p.Log, &f.log}} {
			for _, d := range l.src {
				dec, err := rg.parseDecorator(d)
				if err != nil {
					return nil, err
				}
				*l.dst = append(*l.dst, dec)
			}
		}
		return f, nil
	}
}
