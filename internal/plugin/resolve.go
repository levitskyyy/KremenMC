package plugin

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

var (
	ErrMissingDependency = errors.New("missing dependency")
	ErrDependencyCycle   = errors.New("dependency cycle")
	ErrDuplicateID       = errors.New("duplicate plugin id")
)

// Resolution is the outcome of ordering a set of manifests.
type Resolution struct {
	// Order lists loadable plugin ids so that every plugin comes after its
	// depends, softdepends and the plugins that loadbefore it.
	Order []string
	// Failed maps plugin ids that cannot load to the reason.
	Failed map[string]error
}

type edge struct{ soft bool }

// Resolve orders plugins with a deterministic Kahn topological sort. Missing
// hard dependencies and hard cycles fail the plugins involved and everything
// that hard-depends on them. Cycles that include a soft edge are broken by
// ignoring soft edges into the lowest-id stuck plugin.
func Resolve(manifests []*Manifest) Resolution {
	res := Resolution{Failed: map[string]error{}}
	byID := make(map[string]*Manifest, len(manifests))
	for _, m := range manifests {
		if _, dup := byID[m.ID]; dup {
			res.Failed[m.ID] = fmt.Errorf("%w: %s", ErrDuplicateID, m.ID)
			continue
		}
		byID[m.ID] = m
	}
	for id := range res.Failed {
		delete(byID, id)
	}

	// in[x][y] means y must load before x.
	in := map[string]map[string]edge{}
	add := func(after, before string, soft bool) {
		if in[after] == nil {
			in[after] = map[string]edge{}
		}
		if e, ok := in[after][before]; ok && !e.soft {
			return
		}
		in[after][before] = edge{soft: soft}
	}
	for id, m := range byID {
		in[id] = in[id]
		for _, d := range m.Depends {
			add(id, d, false)
		}
		for _, d := range m.SoftDepends {
			if _, ok := byID[d]; ok {
				add(id, d, true)
			}
		}
		for _, d := range m.LoadBefore {
			if _, ok := byID[d]; ok {
				add(d, id, true)
			}
		}
	}

	// Propagate missing hard dependencies to a fixed point.
	for changed := true; changed; {
		changed = false
		for id := range byID {
			if _, failed := res.Failed[id]; failed {
				continue
			}
			for dep, e := range in[id] {
				if e.soft {
					continue
				}
				if _, ok := byID[dep]; !ok {
					res.Failed[id] = fmt.Errorf("%w: %s requires %s", ErrMissingDependency, id, dep)
					changed = true
					break
				}
				if err, failed := res.Failed[dep]; failed {
					res.Failed[id] = fmt.Errorf("%w: %s requires %s (%v)", ErrMissingDependency, id, dep, err)
					changed = true
					break
				}
			}
		}
	}

	for _, cycle := range hardCycles(byID, in, res.Failed) {
		err := fmt.Errorf("%w: %s", ErrDependencyCycle, strings.Join(append(cycle, cycle[0]), " -> "))
		for _, id := range cycle {
			res.Failed[id] = err
		}
	}
	// Plugins hard-depending on cycle members fail too.
	for changed := true; changed; {
		changed = false
		for id := range byID {
			if _, failed := res.Failed[id]; failed {
				continue
			}
			for dep, e := range in[id] {
				if err, failed := res.Failed[dep]; failed && !e.soft {
					res.Failed[id] = fmt.Errorf("%w: %s requires %s (%v)", ErrMissingDependency, id, dep, err)
					changed = true
					break
				}
			}
		}
	}

	pending := map[string]map[string]edge{}
	for id := range byID {
		if _, failed := res.Failed[id]; failed {
			continue
		}
		deps := map[string]edge{}
		for d, e := range in[id] {
			if _, ok := byID[d]; !ok {
				continue
			}
			if _, failed := res.Failed[d]; failed {
				continue
			}
			deps[d] = e
		}
		pending[id] = deps
	}
	for len(pending) > 0 {
		var ready []string
		for id, deps := range pending {
			if len(deps) == 0 {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 {
			// Only soft cycles remain: drop soft edges of the lowest id plugin
			// whose remaining edges are all soft.
			ids := sortedKeys(pending)
			for _, id := range ids {
				allSoft := true
				for _, e := range pending[id] {
					if !e.soft {
						allSoft = false
						break
					}
				}
				if allSoft {
					ready = []string{id}
					break
				}
			}
			if len(ready) == 0 {
				// Unreachable: hard cycles were removed above.
				for _, id := range ids {
					res.Failed[id] = fmt.Errorf("%w: unresolved %s", ErrDependencyCycle, id)
				}
				break
			}
		}
		slices.Sort(ready)
		next := ready[0]
		res.Order = append(res.Order, next)
		delete(pending, next)
		for _, deps := range pending {
			delete(deps, next)
		}
	}
	return res
}

// hardCycles returns the strongly connected components (size > 1) of the
// hard-dependency graph among non-failed plugins, each sorted by id.
func hardCycles(byID map[string]*Manifest, in map[string]map[string]edge, failed map[string]error) [][]string {
	var (
		index   = map[string]int{}
		low     = map[string]int{}
		onStack = map[string]bool{}
		stack   []string
		next    int
		out     [][]string
	)
	var strong func(v string)
	strong = func(v string) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range sortedKeys(in[v]) {
			if in[v][w].soft {
				continue
			}
			if _, ok := byID[w]; !ok {
				continue
			}
			if _, f := failed[w]; f {
				continue
			}
			if _, seen := index[w]; !seen {
				strong(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			if len(comp) > 1 {
				slices.Sort(comp)
				out = append(out, comp)
			}
		}
	}
	for _, id := range sortedKeys(byID) {
		if _, f := failed[id]; f {
			continue
		}
		if _, seen := index[id]; !seen {
			strong(id)
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
