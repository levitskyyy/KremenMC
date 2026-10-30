package feature

import (
	"encoding/json"
	"fmt"
)

// predVolume is the volume_match block predicate: every position of the
// box min..max (inclusive, relative) must match. Probe-verified with
// kremenmc.probe.feat_ow.Place (testdata/ow_template.txt.gz).
type predVolume struct {
	match    Predicate
	min, max Pos
}

func (v predVolume) Test(lv *Level, p Pos) bool {
	for y := v.min.Y; y <= v.max.Y; y++ {
		for z := v.min.Z; z <= v.max.Z; z++ {
			for x := v.min.X; x <= v.max.X; x++ {
				if !v.match.Test(lv, p.Add(Pos{x, y, z})) {
					return false
				}
			}
		}
	}
	return true
}

func init() {
	predicateTypes["volume_match"] = func(rg *registry, b json.RawMessage) (Predicate, error) {
		var c struct {
			Match json.RawMessage `json:"match"`
			Min   []int           `json:"min"`
			Max   []int           `json:"max"`
		}
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("volume_match: %w", err)
		}
		if len(c.Min) != 3 || len(c.Max) != 3 {
			return nil, fmt.Errorf("volume_match: min/max")
		}
		m, err := rg.parsePredicate(c.Match)
		if err != nil {
			return nil, err
		}
		return predVolume{m, Pos{c.Min[0], c.Min[1], c.Min[2]}, Pos{c.Max[0], c.Max[1], c.Max[2]}}, nil
	}
}
