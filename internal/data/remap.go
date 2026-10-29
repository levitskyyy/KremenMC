package data

// Remap holds flat ID translation tables from one registry version to another,
// matched by name (and property values for block states). Entries missing in
// the target fall back to the block's default state, air, or -1.
type Remap struct {
	States   []StateID
	Items    []ItemID
	Entities []EntityID
	Biomes   []BiomeID
	// Missing counts source entries without an exact target match.
	MissingStates, MissingItems, MissingEntities, MissingBiomes int
}

func NewRemap(from, to *Registry) *Remap {
	m := &Remap{
		States:   make([]StateID, from.NumStates()),
		Items:    make([]ItemID, len(from.Items)),
		Entities: make([]EntityID, len(from.Entities)),
		Biomes:   make([]BiomeID, len(from.Biomes)),
	}
	vals := make([]int, 0, 8)
	for bi := range from.Blocks {
		fb := &from.Blocks[bi]
		tb, ok := to.BlockByName(fb.Name)
		if !ok {
			for s := range fb.NumStates {
				m.States[fb.MinState+StateID(s)] = Air
			}
			m.MissingStates += int(fb.NumStates)
			continue
		}
		// map each source property/value index to the target's
		propMap := make([]int, len(fb.Props))
		valMap := make([][]int, len(fb.Props))
		for p := range fb.Props {
			propMap[p] = tb.PropIndex(fb.Props[p].Name)
			valMap[p] = make([]int, len(fb.Props[p].Values))
			for v, name := range fb.Props[p].Values {
				valMap[p][v] = -1
				if propMap[p] >= 0 {
					valMap[p][v] = tb.Props[propMap[p]].ValueIndex(name)
				}
			}
		}
		def := make([]int, len(tb.Props))
		for p := range tb.Props {
			def[p] = tb.ValueIndex(tb.DefaultState, p)
		}
		for s := range fb.NumStates {
			src := fb.MinState + StateID(s)
			vals = append(vals[:0], def...)
			exact := len(fb.Props) == len(tb.Props)
			for p := range fb.Props {
				vi := fb.ValueIndex(src, p)
				if propMap[p] < 0 || valMap[p][vi] < 0 {
					exact = false
					continue
				}
				vals[propMap[p]] = valMap[p][vi]
			}
			if !exact {
				m.MissingStates++
			}
			m.States[src] = tb.StateAt(vals)
		}
	}
	for i, it := range from.Items {
		id, ok := to.ItemByName(it.Name)
		if !ok {
			id = 0
			m.MissingItems++
		}
		m.Items[i] = id
	}
	for i, e := range from.Entities {
		t, ok := to.EntityByName(e.Name)
		m.Entities[i] = -1
		if ok {
			m.Entities[i] = t.ID
		} else {
			m.MissingEntities++
		}
	}
	for i, b := range from.Biomes {
		id, ok := to.BiomeByName(b.Name)
		if !ok {
			id = 0
			if p, ok := to.BiomeByName("plains"); ok {
				id = p
			}
			m.MissingBiomes++
		}
		m.Biomes[i] = id
	}
	return m
}

// State translates s, mapping out-of-range IDs to air.
func (m *Remap) State(s StateID) StateID {
	if int(s) >= len(m.States) {
		return Air
	}
	return m.States[s]
}
