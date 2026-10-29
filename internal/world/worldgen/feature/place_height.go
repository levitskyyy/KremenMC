package feature

import (
	"encoding/json"
	"fmt"
)

// biasedHeight is biased_to_bottom / very_biased_to_bottom: nested uniform
// draws, each bounded by the previous one (testdata/feat_heights.txt).
type biasedHeight struct {
	lo, hi anchor
	inner  int
	very   bool
}

func (b biasedHeight) Sample(r *Random, minY, height int) int {
	lo, hi := b.lo.resolve(minY, height), b.hi.resolve(minY, height)
	if hi-lo-b.inner+1 <= 0 {
		return lo
	}
	if !b.very {
		j := int(r.NextIntn(int32(hi - lo - b.inner + 1)))
		return int(r.NextIntn(int32(j+b.inner))) + lo
	}
	j := int(r.NextIntBetweenInclusive(int32(lo+b.inner), int32(hi)))
	k := int(r.NextIntBetweenInclusive(int32(lo), int32(j-1)))
	if k-1+b.inner <= lo {
		return lo
	}
	return int(r.NextIntBetweenInclusive(int32(lo), int32(k-1+b.inner)))
}

func init() {
	for name, very := range map[string]bool{"biased_to_bottom": false, "very_biased_to_bottom": true} {
		heightTypes[name] = func(b json.RawMessage) (HeightProvider, error) {
			var p struct {
				Min   json.RawMessage `json:"min_inclusive"`
				Max   json.RawMessage `json:"max_inclusive"`
				Inner *int            `json:"inner"`
			}
			if err := json.Unmarshal(b, &p); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			h := biasedHeight{inner: 1, very: very}
			if p.Inner != nil {
				h.inner = *p.Inner
			}
			var err error
			if h.lo, err = parseAnchor(p.Min); err != nil {
				return nil, err
			}
			h.hi, err = parseAnchor(p.Max)
			return h, err
		}
	}
}
