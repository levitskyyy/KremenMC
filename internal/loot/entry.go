package loot

import (
	"encoding/json"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/item"
)

// entry is a LootPoolEntryContainer.
type entry struct {
	kind      string
	conds     []*cond
	modifiers []*modifier
	weight    int32
	quality   int32

	item     int32    // item
	items    []int32  // tag
	expandT  bool     // tag
	table    *Table   // loot_table (inline or resolved lazily)
	tableID  string   // loot_table reference
	dynamic  string   // dynamic
	children []*entry // alternatives, group, sequence

	tagChoice int32 // index into items for an expanded tag choice, else -1
	parent    *entry
}

func parseEntry(raw json.RawMessage) (*entry, error) {
	var j struct {
		Condition json.RawMessage   `json:"condition"`
		Modifier  json.RawMessage   `json:"modifier"`
		Weight    *int32            `json:"weight"`
		Quality   int32             `json:"quality"`
		Name      string            `json:"name"`
		Items     string            `json:"items"`
		Expand    bool              `json:"expand"`
		Value     json.RawMessage   `json:"value"`
		Children  []json.RawMessage `json:"children"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	e := &entry{kind: typeOf(raw), weight: 1, quality: j.Quality, tagChoice: -1}
	if j.Weight != nil {
		e.weight = *j.Weight
	}
	var err error
	if e.conds, err = parseConds(j.Condition); err != nil {
		return nil, err
	}
	if e.modifiers, err = parseModifiers(j.Modifier); err != nil {
		return nil, err
	}
	switch e.kind {
	case "item":
		id, ok := item.IDOf(j.Name)
		if !ok {
			return nil, fmt.Errorf("unknown item %s", j.Name)
		}
		e.item = id
	case "tag":
		name := j.Items
		if name == "" {
			name = j.Name
		}
		names, err := resolveSet("item", json.RawMessage(`"#`+name+`"`))
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			if id, ok := item.IDOf(n); ok {
				e.items = append(e.items, id)
			}
		}
		e.expandT = j.Expand
	case "loot_table":
		var id string
		if json.Unmarshal(j.Value, &id) == nil {
			e.tableID = qualify(id)
		} else if e.table, err = Parse("inline", j.Value); err != nil {
			return nil, err
		}
	case "dynamic":
		e.dynamic = j.Name
	case "empty":
	case "alternatives", "group", "sequence":
		for _, c := range j.Children {
			ch, err := parseEntry(c)
			if err != nil {
				return nil, err
			}
			e.children = append(e.children, ch)
		}
	default:
		return nil, fmt.Errorf("unsupported entry %s", e.kind)
	}
	return e, nil
}

func (e *entry) weightFor(luck float32) int32 {
	return max(floorF(float32(e.weight)+float32(e.quality)*luck), 0)
}

// expand is LootPoolEntryContainer.expand: adds the choosable entries and
// reports whether this entry applied.
func (e *entry) expand(ctx *Context, add func(*entry)) bool {
	if !allConds(e.conds, ctx) {
		return false
	}
	switch e.kind {
	case "alternatives":
		for _, c := range e.children {
			if c.expand(ctx, add) {
				return true
			}
		}
		return false
	case "group":
		for _, c := range e.children {
			c.expand(ctx, add)
		}
		return true
	case "sequence":
		for _, c := range e.children {
			if !c.expand(ctx, add) {
				return false
			}
		}
		return true
	case "tag":
		if e.expandT {
			for i := range e.items {
				add(&entry{kind: "tagitem", weight: e.weight, quality: e.quality, parent: e, tagChoice: int32(i)})
			}
			return true
		}
	}
	add(e)
	return true
}

// create is LootPoolSingletonContainer.createItemStack.
func (e *entry) create(emit func(item.Stack), ctx *Context) {
	src := e
	if e.kind == "tagitem" {
		src = e.parent
	}
	out := decorate(src.modifiers, emit, ctx)
	switch e.kind {
	case "item":
		out(item.Stack{Item: e.item, Count: 1})
	case "tagitem":
		out(item.Stack{Item: src.items[e.tagChoice], Count: 1})
	case "tag":
		for _, id := range e.items {
			out(item.Stack{Item: id, Count: 1})
		}
	case "loot_table":
		t := e.table
		if t == nil {
			t = Get(e.tableID)
		}
		if t != nil {
			t.RollRaw(ctx, out)
		}
	case "dynamic":
		if ctx.Dynamic != nil {
			ctx.Dynamic(e.dynamic, out)
		}
	}
}
