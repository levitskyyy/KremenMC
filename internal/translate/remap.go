package translate

import (
	"strings"

	"github.com/levitskyyy/KremenMC/internal/command"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/translate/meta"
	"github.com/levitskyyy/KremenMC/internal/translate/pdef"
)

// remaps are the direct canonical <-> target value tables of one target.
type remaps struct {
	states                     []int32 // canonical state -> target state
	items                      []int32 // canonical item -> target item (unmapped: stone)
	itemsUp                    []int32 // target item -> canonical item (unmapped: stone)
	itemKnown                  []bool  // canonical item exists in target
	blocks                     []int32 // canonical block -> target block, -1 if missing
	entities                   []int32 // canonical entity type -> target, -1 if missing
	biomes                     []int32 // canonical data biome -> target data biome
	srcStateBits, dstStateBits uint8
	stoneItem, stoneItemUp     int32

	components   []int32 // canonical component type -> target, -1 if missing or incompatible
	componentsUp []int32
	serializers  []int32 // canonical metadata serializer -> target, -1 if missing or incompatible
	particles    []int32 // canonical particle -> target, -1 if missing or incompatible
	sounds       []int32
	attributes   []int32
	parsers      []int32
	soundSources []int32
	// metaIndex maps canonical entity type -> canonical metadata index -> target index (-1 drop).
	metaIndex [][]int16

	src, dst         *pdef.Schema
	srcComp, dstComp map[string]*pdef.Node
	srcSer           []*pdef.Node
	serNames         []string
	srcPart          []*pdef.Node
	partNames        []string
	fallbackParticle int32
	templateAsSlot   bool // the target encodes item templates as slots (before 26.1)
	dstPartByID      []*pdef.Node
	parserProps      []*pdef.Node
	stringParser     int32
}

func lookupOr(m map[string]int32, k string, def int32) int32 {
	if v, ok := m[k]; ok {
		return v
	}
	return def
}

func bitsFor(n int) uint8 {
	b := uint8(0)
	for (1 << b) < n {
		b++
	}
	if b == 0 {
		b = 1
	}
	return b
}

func buildRemaps(canon, tgt *verInfo) *remaps {
	from, to := canon.reg, tgt.reg
	m := &remaps{
		srcStateBits: bitsFor(from.NumStates()),
		dstStateBits: bitsFor(to.NumStates()),
	}
	dr := data.NewRemap(from, to)
	stone, _ := to.DefaultState("stone")
	m.states = make([]int32, len(dr.States))
	for s, t := range dr.States {
		m.states[s] = int32(t)
		if t == data.Air && !from.IsAir(data.StateID(s)) {
			if _, ok := to.BlockByName(from.BlockOf(data.StateID(s)).Name); !ok && from.IsSolid(data.StateID(s)) {
				m.states[s] = int32(stone)
			}
		}
	}
	m.blocks = make([]int32, len(from.Blocks))
	for i := range from.Blocks {
		m.blocks[i] = -1
		if b, ok := to.BlockByName(from.Blocks[i].Name); ok {
			m.blocks[i] = int32(b.ID)
		}
	}
	si, _ := to.ItemByName("stone")
	m.stoneItem = int32(si)
	su, _ := from.ItemByName("stone")
	m.stoneItemUp = int32(su)
	m.items = make([]int32, len(from.Items))
	m.itemKnown = make([]bool, len(from.Items))
	for i, it := range from.Items {
		id, ok := to.ItemByName(it.Name)
		m.itemKnown[i] = ok
		if !ok {
			id = si
		}
		m.items[i] = int32(id)
	}
	m.itemsUp = make([]int32, len(to.Items))
	for i, it := range to.Items {
		id, ok := from.ItemByName(it.Name)
		if !ok {
			id = su
		}
		m.itemsUp[i] = int32(id)
	}
	m.entities = make([]int32, len(dr.Entities))
	for i, e := range dr.Entities {
		m.entities[i] = int32(e)
	}
	m.biomes = make([]int32, len(dr.Biomes))
	for i, b := range dr.Biomes {
		m.biomes[i] = int32(b)
	}

	cm, tm := canon.meta, tgt.meta
	m.src, m.dst = pdef.Compile(cm.Types), pdef.Compile(tm.Types)
	_, hasTemplate := tm.Types["ItemStackTemplate"]
	m.templateAsSlot = !hasTemplate
	m.sounds = meta.EnumMap(cm, tm, meta.Sound)
	m.soundSources = meta.EnumMap(cm, tm, meta.SoundSource)
	m.parsers, m.parserProps = parserMap(m.src, m.dst, tm)
	m.stringParser = max(tm.Index(meta.Parser, "brigadier:string"), 0)
	m.attributes = attributeMap(cm, tm)

	m.srcComp, _, _ = m.src.Cases("SlotComponent")
	m.dstComp, _, _ = m.dst.Cases("SlotComponent")
	m.components = compatibleEnum(cm, tm, meta.Component, m.srcComp, m.dstComp)
	m.componentsUp = compatibleEnum(tm, cm, meta.Component, m.dstComp, m.srcComp)

	srcSer, serNames, _ := m.src.Cases("entityMetadataEntry")
	dstSer, _, _ := m.dst.Cases("entityMetadataEntry")
	m.serializers = compatibleEnum(cm, tm, meta.Metadata, srcSer, dstSer)
	m.serNames = serNames
	m.srcSer = make([]*pdef.Node, len(serNames))
	for i, n := range serNames {
		m.srcSer[i] = srcSer[n]
	}

	srcPart, partNames, _ := m.src.Cases("Particle")
	dstPart, _, _ := m.dst.Cases("Particle")
	m.particles = compatibleEnum(cm, tm, meta.Particle, srcPart, dstPart)
	m.partNames = partNames
	m.srcPart = make([]*pdef.Node, len(partNames))
	for i, n := range partNames {
		m.srcPart[i] = srcPart[n]
	}

	for _, n := range tm.Enums[meta.Particle] {
		m.dstPartByID = append(m.dstPartByID, dstPart[n])
	}
	for _, n := range []string{"poof", "smoke", "cloud"} {
		if i := tm.Index(meta.Particle, n); i >= 0 && dstPart[n].Void() {
			m.fallbackParticle = i
			break
		}
	}

	m.metaIndex = make([][]int16, len(from.Entities))
	for i, e := range from.Entities {
		src := cm.MetadataKeys[e.Name]
		dst := tm.MetadataKeys[e.Name]
		if src == nil || dst == nil {
			continue
		}
		pos := make(map[string]int16, len(dst))
		for j, k := range dst {
			pos[k] = int16(j)
		}
		idx := make([]int16, len(src))
		for j, k := range src {
			idx[j] = lookupOr16(pos, k)
		}
		m.metaIndex[i] = idx
	}
	return m
}

func lookupOr16(m map[string]int16, k string) int16 {
	if v, ok := m[k]; ok {
		return v
	}
	return -1
}

// enumAlias pairs enum names that were renamed without a layout change.
var enumAlias = map[string]map[string]string{
	meta.Metadata: {"optional_living_entity_reference": "optional_uuid", "optional_uuid": "optional_living_entity_reference"},
}

// compatibleEnum maps enum positions by name, keeping only entries whose
// payload schemas are equal in both versions.
func compatibleEnum(from, to *meta.Meta, kind string, fromCases, toCases map[string]*pdef.Node) []int32 {
	src := from.Enums[kind]
	out := make([]int32, len(src))
	for i, n := range src {
		tn := n
		out[i] = to.Index(kind, n)
		if out[i] < 0 {
			if a, ok := enumAlias[kind][n]; ok {
				tn, out[i] = a, to.Index(kind, a)
			}
		}
		if out[i] < 0 {
			continue
		}
		a, b := fromCases[n], toCases[tn]
		if a == nil || b == nil || !pdef.Equal(a, b) {
			out[i] = -1
		}
	}
	return out
}

// attributeMap maps attribute IDs by name, ignoring the "generic."/"player."
// prefixes that 1.21.2 dropped.
func attributeMap(from, to *meta.Meta) []int32 {
	src := attributeList(from)
	dst := attributeList(to)
	idx := make(map[string]int32, len(dst))
	for i, n := range dst {
		idx[n] = int32(i)
	}
	out := make([]int32, len(src))
	for i, n := range src {
		out[i] = lookupOr(idx, n, -1)
	}
	return out
}

// attributeOverride replaces attribute lists that minecraft-data gets wrong.
var attributeOverride = map[int32]int32{
	777: 775, // canonical schema is patched from 26.1
	776: 775, // the 26.2 data branch dropped four attributes that still exist
}

func attributeList(m *meta.Meta) []string {
	if o, ok := attributeOverride[m.Protocol]; ok {
		if om, err := meta.Load(o); err == nil {
			m = om
		}
	}
	l := m.Enums[meta.Attribute]
	out := make([]string, len(l))
	for i, n := range l {
		n = strings.TrimPrefix(n, "generic.")
		n = strings.TrimPrefix(n, "player.")
		out[i] = n
	}
	return out
}

func (m *remaps) state(s int32) int32 {
	if s < 0 || int(s) >= len(m.states) {
		return 0
	}
	return m.states[s]
}

func (m *remaps) item(i int32) int32 {
	if i < 0 || int(i) >= len(m.items) {
		return m.stoneItem
	}
	return m.items[i]
}

func (m *remaps) itemUp(i int32) int32 {
	if i < 0 || int(i) >= len(m.itemsUp) {
		return m.stoneItemUp
	}
	return m.itemsUp[i]
}

func (m *remaps) entity(e int32) int32 {
	if e < 0 || int(e) >= len(m.entities) {
		return -1
	}
	return m.entities[e]
}

func (m *remaps) block(b int32) int32 {
	if b < 0 || int(b) >= len(m.blocks) {
		return -1
	}
	return m.blocks[b]
}

func at(t []int32, i int32) int32 {
	if i < 0 || int(i) >= len(t) {
		return -1
	}
	return t[i]
}

// parserAlias folds parser names that changed between versions.
var parserAlias = map[string]string{
	"minecraft:color": "minecraft:team_color",
	"minecraft:nbt":   "minecraft:nbt_compound_tag",
}

func parserName(n string) string {
	if a, ok := parserAlias[n]; ok {
		return a
	}
	return n
}

// parserMap maps the verified 26.3 parser registry (command.Parsers777) to
// the target. Canonical parsers without a known property schema have a nil
// node; target parsers with different properties map to -1.
func parserMap(src, dst *pdef.Schema, tm *meta.Meta) ([]int32, []*pdef.Node) {
	srcProps, _, _ := src.FindSwitch("command_node", "properties")
	dstProps, _, _ := dst.FindSwitch("command_node", "properties")
	norm := func(in map[string]*pdef.Node) map[string]*pdef.Node {
		out := make(map[string]*pdef.Node, len(in))
		for k, v := range in {
			out[parserName(k)] = v
		}
		return out
	}
	srcProps, dstProps = norm(srcProps), norm(dstProps)
	dstIdx := map[string]int32{}
	for i, n := range tm.Enums[meta.Parser] {
		dstIdx[parserName(n)] = int32(i)
	}
	ids := make([]int32, len(command.Parsers777))
	props := make([]*pdef.Node, len(command.Parsers777))
	for i, p := range command.Parsers777 {
		n := parserName(string(p))
		props[i] = srcProps[n]
		ids[i] = -1
		if t, ok := dstIdx[n]; ok && props[i] != nil && dstProps[n] != nil && pdef.Equal(props[i], dstProps[n]) {
			ids[i] = t
		}
	}
	return ids, props
}
