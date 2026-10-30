package feature

import (
	"encoding/json"
	"fmt"
)

// PlaceTemplate places a structure template for the template feature (and
// fossils). Package structure sets it from init(), since structure imports
// feature. tmpl is the template key, procs the processor list (inline JSON
// or a quoted registry key, may be nil), rot the rotation index (none,
// clockwise_90, clockwise_180, counterclockwise_90). It draws the palette
// from r and reports whether anything was placed.
var PlaceTemplate func(lv *Level, tmpl string, procs json.RawMessage, p Pos, rot int, r *Random) bool

// templateFeature is the 26.x template feature (desert well, sulfur
// springs). Fitted to kremenmc.probe.feat_ow.Place draw logs: a weighted
// template pick (nextInt of the total weight), a rotation (nextInt(4)), then
// the palette draw inside the placement.
type templateFeature struct {
	keys   []string
	weight []int
	total  int
	procs  json.RawMessage
}

func parseTemplateFeature(_ *registry, b json.RawMessage) (Feature, error) {
	var c struct {
		Templates []struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
			Weight int `json:"weight"`
		} `json:"templates"`
		Processors json.RawMessage `json:"processors"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	if PlaceTemplate == nil {
		return nil, unsupported("feature", "template")
	}
	f := &templateFeature{procs: c.Processors}
	for _, t := range c.Templates {
		f.keys = append(f.keys, t.Data.ID)
		f.weight = append(f.weight, t.Weight)
		f.total += t.Weight
	}
	if f.total <= 0 {
		return nil, fmt.Errorf("template: no templates")
	}
	return f, nil
}

func (f *templateFeature) Place(c *placeCtx, r *Random, p Pos) bool {
	w := int(r.NextIntn(int32(f.total)))
	key := f.keys[len(f.keys)-1]
	for i, v := range f.weight {
		if w < v {
			key = f.keys[i]
			break
		}
		w -= v
	}
	rot := int(r.NextIntn(4))
	return PlaceTemplate(c.lv, key, f.procs, p, rot, r)
}

func init() {
	featureTypes["template"] = parseTemplateFeature
}

// Region is the block access a Level reads and writes.
type Region = region

// NewLevel wraps a region for feature placement outside the pipeline
// (probe replays).
func NewLevel(rg Region) *Level { return &Level{rg: rg, st: &Stage{}} }
