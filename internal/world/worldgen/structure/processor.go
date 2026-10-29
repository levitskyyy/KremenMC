package structure

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Processors implemented from the data-pack format and the wiki; their
// random draws are not verified against the reference. Per-block randomness
// uses a legacy random seeded from the world position (GetSeed), so the
// result does not depend on which chunk places the block.

type blockPred struct {
	typ   string
	block string
	state data.StateID
	tag   map[string]bool
	prob  float32
}

func (p *blockPred) test(reg *data.Registry, s data.StateID, r *rng.Legacy) bool {
	b := reg.BlockOf(s)
	name := ""
	if b != nil {
		name = b.Name
	}
	switch p.typ {
	case "always_true", "":
		return true
	case "block_match":
		return name == p.block
	case "blockstate_match":
		return s == p.state
	case "tag_match":
		return p.tag[name]
	case "random_block_match":
		return name == p.block && r.NextFloat() < p.prob
	case "random_blockstate_match":
		return s == p.state && r.NextFloat() < p.prob
	}
	return false
}

type predJSON struct {
	Type  string          `json:"predicate_type"`
	Block string          `json:"block"`
	State json.RawMessage `json:"block_state"`
	Tag   string          `json:"tag"`
	Prob  float32         `json:"probability"`
}

func stateFromJSON(raw json.RawMessage) (data.StateID, error) {
	var j struct {
		Name       string            `json:"Name"`
		Properties map[string]string `json:"Properties"`
		ID         string            `json:"id"`
		Props      map[string]string `json:"properties"`
	}
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		j.Name = s
	} else if err := json.Unmarshal(raw, &j); err != nil {
		return 0, err
	}
	if j.ID != "" {
		j.Name, j.Properties = j.ID, j.Props
	}
	st, ok := data.Canonical().StateFrom(trimNS(j.Name), func(k string) (string, bool) {
		v, ok := j.Properties[k]
		return v, ok
	})
	if !ok {
		return 0, fmt.Errorf("unknown block %s", j.Name)
	}
	return st, nil
}

func blockTag(pk *wgdata.Pack, tag string) map[string]bool {
	l, _ := pk.Tag("block", strings.TrimPrefix(tag, "#"))
	m := map[string]bool{}
	for _, n := range l {
		m[trimNS(n)] = true
	}
	return m
}

func parsePred(pk *wgdata.Pack, j predJSON) (*blockPred, error) {
	p := &blockPred{typ: trimNS(j.Type), block: trimNS(j.Block), prob: j.Prob}
	if len(j.State) > 0 {
		s, err := stateFromJSON(j.State)
		if err != nil {
			return nil, err
		}
		p.state = s
	}
	if j.Tag != "" {
		p.tag = blockTag(pk, j.Tag)
	}
	return p, nil
}

type rule struct {
	input, location *blockPred
	output          data.StateID
	lootTable       string
}

type processor struct {
	typ       string
	rules     []rule
	integrity float32
	rottable  map[string]bool
	protected map[string]bool
	heightmap string
	offset    int32
	delegate  *processor
}

// ProcessorList is a worldgen/processor_list entry.
type ProcessorList struct {
	procs []*processor
}

var (
	plMu    sync.Mutex
	plCache = map[string]*ProcessorList{}
)

// LoadProcessorList returns the named processor list.
func LoadProcessorList(name string) (*ProcessorList, error) {
	name = qualify(name)
	plMu.Lock()
	defer plMu.Unlock()
	if pl, ok := plCache[name]; ok {
		return pl, nil
	}
	pk, err := wgdata.Load()
	if err != nil {
		return nil, err
	}
	b, ok := pk.JSON("worldgen/processor_list", name)
	if !ok {
		return nil, fmt.Errorf("structure: processor list %s missing", name)
	}
	pl, err := ParseProcessorList(pk, b)
	if err != nil {
		return nil, fmt.Errorf("structure: processor list %s: %w", name, err)
	}
	plCache[name] = pl
	return pl, nil
}

// ParseProcessorList parses {"processors": [...]} (or a bare list).
func ParseProcessorList(pk *wgdata.Pack, b []byte) (*ProcessorList, error) {
	var raw []json.RawMessage
	var obj struct {
		Processors []json.RawMessage `json:"processors"`
	}
	if json.Unmarshal(b, &obj) == nil && obj.Processors != nil {
		raw = obj.Processors
	} else if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	pl := &ProcessorList{}
	for _, r := range raw {
		p, err := parseProcessor(pk, r)
		if err != nil {
			return nil, err
		}
		pl.procs = append(pl.procs, p)
	}
	return pl, nil
}

func parseProcessor(pk *wgdata.Pack, b []byte) (*processor, error) {
	var j struct {
		Type      string          `json:"processor_type"`
		Integrity float32         `json:"integrity"`
		Rottable  json.RawMessage `json:"rottable_blocks"`
		Value     string          `json:"value"`
		Heightmap string          `json:"heightmap"`
		Offset    int32           `json:"offset"`
		Delegate  json.RawMessage `json:"delegate"`
		Rules     []struct {
			Input    predJSON        `json:"input_predicate"`
			Location predJSON        `json:"location_predicate"`
			Output   json.RawMessage `json:"output_state"`
			BEMod    struct {
				Type      string `json:"type"`
				LootTable string `json:"loot_table"`
			} `json:"block_entity_modifier"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, err
	}
	p := &processor{typ: trimNS(j.Type), integrity: j.Integrity, heightmap: j.Heightmap, offset: j.Offset}
	switch p.typ {
	case "rule":
		for _, r := range j.Rules {
			in, err := parsePred(pk, r.Input)
			if err != nil {
				return nil, err
			}
			loc, err := parsePred(pk, r.Location)
			if err != nil {
				return nil, err
			}
			out, err := stateFromJSON(r.Output)
			if err != nil {
				return nil, err
			}
			p.rules = append(p.rules, rule{in, loc, out, r.BEMod.LootTable})
		}
	case "block_rot":
		if len(j.Rottable) > 0 {
			var s string
			if json.Unmarshal(j.Rottable, &s) == nil {
				p.rottable = blockTag(pk, s)
			} else {
				var l []string
				_ = json.Unmarshal(j.Rottable, &l)
				p.rottable = map[string]bool{}
				for _, n := range l {
					p.rottable[trimNS(n)] = true
				}
			}
		}
	case "protected_blocks":
		p.protected = blockTag(pk, j.Value)
	case "capped":
		if len(j.Delegate) > 0 {
			d, err := parseProcessor(pk, j.Delegate)
			if err != nil {
				return nil, err
			}
			p.delegate = d
		}
	case "gravity", "nop", "block_ignore", "jigsaw_replacement", "lava_submerged_block", "block_age", "blackstone_replace":
	default:
		return nil, fmt.Errorf("unknown processor %s", j.Type)
	}
	return p, nil
}

// Apply runs the list on one template block about to be written at wx,wy,wz.
// keep is false when the block must be skipped.
func (pl *ProcessorList) Apply(lv Level, b TemplateBlock, s data.StateID, be nbt.Compound, wx, wy, wz int32, piece *TemplatePiece) (data.StateID, nbt.Compound, bool) {
	reg := data.Canonical()
	for _, p := range pl.procs {
		var keep bool
		s, be, wy, keep = p.apply(reg, lv, s, be, wx, wy, wz)
		if !keep {
			return s, be, false
		}
	}
	return s, be, true
}

func (p *processor) apply(reg *data.Registry, lv Level, s data.StateID, be nbt.Compound, wx, wy, wz int32) (data.StateID, nbt.Compound, int32, bool) {
	switch p.typ {
	case "rule":
		r := rng.NewLegacy(rng.GetSeed(wx, wy, wz))
		cur := lv.Block(wx, wy, wz)
		for _, ru := range p.rules {
			if ru.input.test(reg, s, r) && ru.location.test(reg, cur, r) {
				s = ru.output
				if ru.lootTable != "" {
					be = nbt.Compound{"LootTable": ru.lootTable}
					if bl := reg.BlockOf(s); bl != nil {
						be["id"] = "minecraft:" + bl.Name
					}
				}
				break
			}
		}
	case "block_rot":
		bl := reg.BlockOf(s)
		if p.rottable != nil && (bl == nil || !p.rottable[bl.Name]) {
			break
		}
		r := rng.NewLegacy(rng.GetSeed(wx, wy, wz))
		if r.NextFloat() > p.integrity {
			return s, be, wy, false
		}
	case "protected_blocks":
		if bl := reg.BlockOf(lv.Block(wx, wy, wz)); bl != nil && p.protected[bl.Name] {
			return s, be, wy, false
		}
	}
	// gravity (jigsaw terrain-matching pieces) and capped are not applied
	// yet: they pass blocks through unchanged.
	return s, be, wy, true
}
