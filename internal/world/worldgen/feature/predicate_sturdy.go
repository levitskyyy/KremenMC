package feature

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// predicate_sturdy.txt is BlockState.isFaceSturdy (full support) per state,
// dumped by tools/refprobe kremenmc.probe.feat_ore.Sturdy: "block n mask..."
// with one mask per state in definition order, or one for all states. Mask
// bits follow dirOrder.
//
//go:embed predicate_sturdy.txt
var sturdyTxt string

var dirOrder = []string{"down", "up", "north", "south", "west", "east"}

var sturdyMasks = sync.OnceValues(func() ([]uint8, error) {
	reg := data.Canonical()
	n := 0
	for _, b := range reg.Blocks {
		n = max(n, int(b.MinState)+int(b.NumStates))
	}
	out := make([]uint8, n)
	for _, line := range strings.Split(sturdyTxt, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		b, ok := reg.BlockByName(f[0])
		if !ok {
			return nil, fmt.Errorf("sturdy: unknown block %s", f[0])
		}
		cnt, _ := strconv.Atoi(f[1])
		if cnt != int(b.NumStates) {
			return nil, fmt.Errorf("sturdy: %s has %d states, reference %d", f[0], b.NumStates, cnt)
		}
		for i := range cnt {
			v := f[2]
			if len(f) > 3 {
				v = f[2+i]
			}
			m, _ := strconv.Atoi(v)
			out[int(b.MinState)+i] = uint8(m)
		}
	}
	return out, nil
})

func faceSturdy(st data.StateID, dir int) bool {
	m, err := sturdyMasks()
	return err == nil && int(st) < len(m) && m[st]&(1<<dir) != 0
}

type predSturdy struct {
	off Pos
	dir int
}

func (m predSturdy) Test(lv *Level, p Pos) bool { return faceSturdy(lv.Block(p.Add(m.off)), m.dir) }

func init() {
	predicateTypes["has_sturdy_face"] = func(_ *registry, b json.RawMessage) (Predicate, error) {
		var p struct {
			Offset    []int  `json:"offset"`
			Direction string `json:"direction"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("has_sturdy_face: %w", err)
		}
		if _, err := sturdyMasks(); err != nil {
			return nil, err
		}
		pr := predSturdy{dir: -1}
		if len(p.Offset) == 3 {
			pr.off = Pos{p.Offset[0], p.Offset[1], p.Offset[2]}
		}
		for i, d := range dirOrder {
			if d == p.Direction {
				pr.dir = i
			}
		}
		if pr.dir < 0 {
			return nil, fmt.Errorf("has_sturdy_face: direction %q", p.Direction)
		}
		return pr, nil
	}
}
