package server

import (
	"encoding/json"
	"math"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// toolRule is one entry of the minecraft:tool component.
type toolRule struct {
	blocks           item.HolderSet
	speed            float32
	hasSpeed         bool
	correct, hasCorr bool
}

type toolInfo struct {
	rules          []toolRule
	defaultSpeed   float32
	damagePerBlock int32
	creativeBreak  bool
}

func decodeTool(v []byte) (toolInfo, bool) {
	r := proto.Reader{B: v}
	n, err := r.VarInt()
	if err != nil || n < 0 || int(n) > len(v) {
		return toolInfo{}, false
	}
	t := toolInfo{rules: make([]toolRule, 0, n)}
	for range n {
		var tr toolRule
		k, err := r.VarInt()
		if err != nil {
			return toolInfo{}, false
		}
		if k == 0 {
			if tr.blocks.Tag, err = r.String(32767); err != nil {
				return toolInfo{}, false
			}
		} else {
			tr.blocks.IDs = make([]int32, 0, k-1)
			for range k - 1 {
				id, err := r.VarInt()
				if err != nil {
					return toolInfo{}, false
				}
				tr.blocks.IDs = append(tr.blocks.IDs, id)
			}
		}
		if ok, _ := r.Bool(); ok {
			tr.speed, _ = r.F32()
			tr.hasSpeed = true
		}
		if ok, _ := r.Bool(); ok {
			tr.correct, _ = r.Bool()
			tr.hasCorr = true
		}
		t.rules = append(t.rules, tr)
	}
	var e1, e2, e3 error
	t.defaultSpeed, e1 = r.F32()
	t.damagePerBlock, e2 = r.VarInt()
	t.creativeBreak, e3 = r.Bool()
	if e1 != nil || e2 != nil || e3 != nil {
		return toolInfo{}, false
	}
	return t, true
}

func stackTool(st item.Stack) (toolInfo, bool) {
	if st.IsEmpty() {
		return toolInfo{}, false
	}
	v, ok := st.Component(item.Tool)
	if !ok {
		return toolInfo{}, false
	}
	return decodeTool(v)
}

var (
	blockTagOnce sync.Once
	blockTags    map[string]map[int32]bool
)

func blockInSet(h item.HolderSet, id data.BlockID) bool {
	if h.Tag == "" {
		for _, x := range h.IDs {
			if x == int32(id) {
				return true
			}
		}
		return false
	}
	blockTagOnce.Do(func() {
		blockTags = map[string]map[int32]bool{}
		for _, g := range regsync.Tags() {
			if g.Registry != "minecraft:block" {
				continue
			}
			for _, t := range g.Tags {
				m := make(map[int32]bool, len(t.IDs))
				for _, x := range t.IDs {
					m[x] = true
				}
				blockTags[t.Name] = m
			}
		}
	})
	name := strings.TrimPrefix(h.Tag, "#")
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	return blockTags[name][int32(id)]
}

// toolSpeed is ItemStack.getDestroySpeed.
func toolSpeed(st item.Stack, b *data.Block) float32 {
	t, ok := stackTool(st)
	if !ok {
		return 1
	}
	for _, r := range t.rules {
		if r.hasSpeed && blockInSet(r.blocks, b.ID) {
			return r.speed
		}
	}
	return t.defaultSpeed
}

// toolCorrect is ItemStack.isCorrectToolForDrops.
func toolCorrect(st item.Stack, b *data.Block) bool {
	t, ok := stackTool(st)
	if !ok {
		return false
	}
	for _, r := range t.rules {
		if r.hasCorr && blockInSet(r.blocks, b.ID) {
			return r.correct
		}
	}
	return false
}

func requiresTool(b *data.Block) bool {
	return int(b.ID) < len(blockNeedsTool) && blockNeedsTool[b.ID]
}

// correctForDrops is Player.hasCorrectToolForDrops.
func correctForDrops(main item.Stack, b *data.Block) bool {
	return !requiresTool(b) || toolCorrect(main, b)
}

// digInputs is everything Player.getDestroySpeed reads.
type digInputs struct {
	main    item.Stack
	equip   [6]item.Stack // mainhand, offhand, feet, legs, chest, head
	haste   int32         // amplifier+1, 0 when absent
	conduit int32
	fatigue int32
	ground  bool
	water   bool
}

// Attribute ids in the player's mining attributes table.
const (
	attrMiningEfficiency = iota
	attrBlockBreakSpeed
	attrSubmergedMining
)

var miningAttrs = [...]struct {
	name     string
	base     float64
	min, max float64
}{
	{"minecraft:mining_efficiency", 0, 0, 1024},
	{"minecraft:block_break_speed", 1, 0, 1024},
	{"minecraft:submerged_mining_speed", 0.2, 0, 20},
}

// attribute evaluates a mining attribute with the modifiers that enchantments
// on equipped items grant (AttributeInstance.calculateValue).
func (in *digInputs) attribute(a int) float64 {
	var add, mulBase float64
	mulTotal := []float64(nil)
	for slot, st := range in.equip {
		if st.IsEmpty() {
			continue
		}
		for _, e := range st.Enchantments() {
			ench := enchantmentByID(e.ID)
			if ench == nil || !ench.inSlot(slot) {
				continue
			}
			for _, m := range ench.attrs {
				if m.attr != a {
					continue
				}
				v := m.amount.eval(e.Level)
				switch m.op {
				case "add_value":
					add += v
				case "add_multiplied_base":
					mulBase += v
				case "add_multiplied_total":
					mulTotal = append(mulTotal, v)
				}
			}
		}
	}
	d := miningAttrs[a]
	v := d.base + add
	v += v * mulBase
	for _, m := range mulTotal {
		v *= 1 + m
	}
	return min(max(v, d.min), d.max)
}

// destroySpeed is Player.getDestroySpeed.
func destroySpeed(b *data.Block, in *digInputs) float32 {
	f := toolSpeed(in.main, b)
	if f > 1 {
		f += float32(in.attribute(attrMiningEfficiency))
	}
	if amp := max(in.haste, in.conduit); amp > 0 {
		f *= 1 + float32(amp)*0.2
	}
	if in.fatigue > 0 {
		f *= float32(math.Pow(0.3, float64(in.fatigue)))
	}
	f *= float32(in.attribute(attrBlockBreakSpeed))
	if in.water {
		f *= float32(in.attribute(attrSubmergedMining))
	}
	if !in.ground {
		f /= 5
	}
	return f
}

// destroyProgress is BlockState.getDestroyProgress: the fraction of the block
// broken per tick.
func destroyProgress(st data.StateID, in *digInputs) float32 {
	b := data.BlockOf(st)
	if b == nil {
		return 0
	}
	h := b.Hardness
	if h == -1 {
		return 0
	}
	div := float32(100)
	if correctForDrops(in.main, b) {
		div = 30
	}
	return destroySpeed(b, in) / h / div
}

// Enchantment attribute effects, loaded from the embedded data pack.

type levelValue struct {
	kind                  string
	base, perLevel, added float64
	min, max              float64
	inner                 *levelValue
	values                []float64
	fallback              *levelValue
}

func parseLevelValue(raw json.RawMessage) *levelValue {
	var c float64
	if json.Unmarshal(raw, &c) == nil {
		return &levelValue{kind: "constant", base: c}
	}
	var o struct {
		Type     string          `json:"type"`
		Base     float64         `json:"base"`
		PerLevel float64         `json:"per_level_above_first"`
		Added    float64         `json:"added"`
		Min      float64         `json:"min"`
		Max      float64         `json:"max"`
		Value    json.RawMessage `json:"value"`
		Values   []float64       `json:"values"`
		Fallback json.RawMessage `json:"fallback"`
	}
	if json.Unmarshal(raw, &o) != nil {
		return &levelValue{kind: "constant"}
	}
	v := &levelValue{kind: strings.TrimPrefix(o.Type, "minecraft:"), base: o.Base, perLevel: o.PerLevel, added: o.Added, min: o.Min, max: o.Max, values: o.Values}
	if o.Value != nil {
		v.inner = parseLevelValue(o.Value)
	}
	if o.Fallback != nil {
		v.fallback = parseLevelValue(o.Fallback)
	}
	return v
}

// eval is LevelBasedValue.calculate (float math as in the reference).
func (v *levelValue) eval(level int32) float64 {
	return float64(v.evalF(level))
}

func (v *levelValue) evalF(level int32) float32 {
	switch v.kind {
	case "constant":
		return float32(v.base)
	case "linear":
		return float32(v.base) + float32(v.perLevel)*float32(level-1)
	case "levels_squared":
		return float32(level*level) + float32(v.added)
	case "clamped":
		return min(max(v.inner.evalF(level), float32(v.min)), float32(v.max))
	case "lookup":
		if int(level) <= len(v.values) && level > 0 {
			return float32(v.values[level-1])
		}
		if v.fallback != nil {
			return v.fallback.evalF(level)
		}
	}
	return 0
}

type enchAttr struct {
	attr   int
	op     string
	amount *levelValue
}

type enchantment struct {
	name  string
	slots []string
	attrs []enchAttr
	// per-effect-component raw JSON, for the loot engine's enchanted bonuses
}

var (
	enchOnce sync.Once
	enchByID map[int32]*enchantment
)

func enchantmentByID(id int32) *enchantment {
	enchOnce.Do(func() {
		enchByID = map[int32]*enchantment{}
		pk, err := wgdata.Load()
		if err != nil {
			return
		}
		for _, name := range pk.Keys("enchantment") {
			idx, ok := regsync.Index("minecraft:enchantment", name)
			if !ok {
				continue
			}
			raw, _ := pk.JSON("enchantment", name)
			var j struct {
				Slots   []string `json:"slots"`
				Effects struct {
					Attributes []struct {
						Attribute string          `json:"attribute"`
						Operation string          `json:"operation"`
						Amount    json.RawMessage `json:"amount"`
					} `json:"minecraft:attributes"`
				} `json:"effects"`
			}
			if json.Unmarshal(raw, &j) != nil {
				continue
			}
			e := &enchantment{name: name, slots: j.Slots}
			for _, a := range j.Effects.Attributes {
				for i, m := range miningAttrs {
					if m.name == a.Attribute {
						e.attrs = append(e.attrs, enchAttr{attr: i, op: a.Operation, amount: parseLevelValue(a.Amount)})
					}
				}
			}
			enchByID[idx] = e
		}
	})
	return enchByID[id]
}

// inSlot reports whether equipment slot (0 mainhand, 1 offhand, 2 feet ..
// 5 head) is in one of the enchantment's slot groups.
func (e *enchantment) inSlot(slot int) bool {
	for _, g := range e.slots {
		switch g {
		case "any":
			return true
		case "mainhand":
			if slot == 0 {
				return true
			}
		case "offhand":
			if slot == 1 {
				return true
			}
		case "hand":
			if slot <= 1 {
				return true
			}
		case "feet":
			if slot == 2 {
				return true
			}
		case "legs":
			if slot == 3 {
				return true
			}
		case "chest":
			if slot == 4 {
				return true
			}
		case "head":
			if slot == 5 {
				return true
			}
		case "armor":
			if slot >= 2 {
				return true
			}
		}
	}
	return false
}

// fluidHeightAt is the water surface height inside block (x,y,z), or 0.
func waterSurface(w interface {
	Block(x, y, z int) data.StateID
}, x, y, z int) float64 {
	st := w.Block(x, y, z)
	if data.Flags(st)&data.FlagFluid == 0 {
		return 0
	}
	b := data.BlockOf(st)
	if b == nil || b.Name == "lava" {
		return 0
	}
	if data.Flags(w.Block(x, y+1, z))&data.FlagFluid != 0 {
		if ab := data.BlockOf(w.Block(x, y+1, z)); ab != nil && ab.Name != "lava" {
			return 1
		}
	}
	if b.Name != "water" {
		return 8.0 / 9
	}
	lv, _ := data.Canonical().Value(st, "level")
	n := 0
	if len(lv) > 0 {
		n = int(lv[0] - '0')
		if len(lv) > 1 {
			n = 8
		}
	}
	if n >= 8 {
		return 8.0 / 9
	}
	return float64(8-n) / 9
}

func eyeInWater(w interface {
	Block(x, y, z int) data.StateID
}, eyeX, eyeY, eyeZ float64) bool {
	x, y, z := int(math.Floor(eyeX)), int(math.Floor(eyeY)), int(math.Floor(eyeZ))
	h := waterSurface(w, x, y, z)
	return h > 0 && eyeY < float64(y)+h
}
