package loot

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// randomEnch is one enchantment enchant_randomly can choose.
type randomEnch struct {
	id       int32
	maxLevel int32
	items    map[int32]bool // supported item ids
}

// parseEnchantRandomly reads enchant_randomly's options (all enchantments
// when absent) and only_compatible (default true).
func parseEnchantRandomly(m *modifier, raw json.RawMessage) error {
	var j struct {
		Options        json.RawMessage `json:"options"`
		OnlyCompatible *bool           `json:"only_compatible"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return err
	}
	pk, err := wgdata.Load()
	if err != nil {
		return err
	}
	var names []string
	if len(j.Options) == 0 {
		names = pk.Keys("enchantment")
	} else {
		var refs []string
		if err := json.Unmarshal(j.Options, &refs); err != nil {
			var one string
			if err := json.Unmarshal(j.Options, &one); err != nil {
				return fmt.Errorf("enchant_randomly options: %w", err)
			}
			refs = []string{one}
		}
		seen := map[string]bool{}
		for _, r := range refs {
			expandTag(pk, "enchantment", r, seen, &names)
		}
	}
	m.compatOnly = j.OnlyCompatible == nil || *j.OnlyCompatible
	for _, n := range names {
		e, err := loadRandomEnch(pk, n)
		if err != nil {
			return err
		}
		m.enchs = append(m.enchs, e)
	}
	return nil
}

func loadRandomEnch(pk *wgdata.Pack, name string) (randomEnch, error) {
	name = qualify(name)
	raw, ok := pk.JSON("enchantment", name)
	if !ok {
		return randomEnch{}, fmt.Errorf("unknown enchantment %s", name)
	}
	var j struct {
		MaxLevel int32           `json:"max_level"`
		Items    json.RawMessage `json:"supported_items"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return randomEnch{}, fmt.Errorf("enchantment %s: %w", name, err)
	}
	id, ok := regsync.Index("minecraft:enchantment", name)
	if !ok {
		return randomEnch{}, fmt.Errorf("enchantment %s: no network id", name)
	}
	var refs []string
	if err := json.Unmarshal(j.Items, &refs); err != nil {
		var one string
		_ = json.Unmarshal(j.Items, &one)
		refs = []string{one}
	}
	var items []string
	seen := map[string]bool{}
	for _, r := range refs {
		expandTag(pk, "item", r, seen, &items)
	}
	e := randomEnch{id: id, maxLevel: max(j.MaxLevel, 1), items: map[int32]bool{}}
	for _, it := range items {
		if iid, ok := item.IDOf(it); ok {
			e.items[iid] = true
		}
	}
	return e, nil
}

// expandTag appends ref ("#tag" or an id) resolved against registry reg.
func expandTag(pk *wgdata.Pack, reg, ref string, seen map[string]bool, out *[]string) {
	ref = strings.TrimSpace(ref)
	if ref == "" || seen[ref] {
		return
	}
	seen[ref] = true
	if !strings.HasPrefix(ref, "#") {
		*out = append(*out, qualify(ref))
		return
	}
	raw, ok := pk.TagJSON(reg, qualify(ref[1:]))
	if !ok {
		return
	}
	var j struct {
		Values []json.RawMessage `json:"values"`
	}
	if json.Unmarshal(raw, &j) != nil {
		return
	}
	for _, v := range j.Values {
		var s string
		if json.Unmarshal(v, &s) != nil {
			var o struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(v, &o) != nil {
				continue
			}
			s = o.ID
		}
		expandTag(pk, reg, s, seen, out)
	}
}

var bookID, enchantedBookID = func() (int32, int32) {
	a, _ := item.IDOf("minecraft:book")
	b, _ := item.IDOf("minecraft:enchanted_book")
	return a, b
}()

// enchantRandomly picks one compatible enchantment and a level in
// [1, max_level]; a book becomes an enchanted book. The draw order is an
// assumption (one index, then one level).
func enchantRandomly(s item.Stack, ctx *Context, m *modifier) item.Stack {
	book := s.Item == bookID
	var pool []randomEnch
	for _, e := range m.enchs {
		if !m.compatOnly || book || e.items[s.Item] {
			pool = append(pool, e)
		}
	}
	if len(pool) == 0 || ctx.Rand == nil {
		return s
	}
	e := pool[ctx.Rand.NextIntn(int32(len(pool)))]
	lvl := int32(1)
	if e.maxLevel > 1 {
		lvl = 1 + ctx.Rand.NextIntn(e.maxLevel)
	}
	if book {
		out := item.Stack{Item: enchantedBookID, Count: s.Count}
		return setComp(out, item.StoredEnchantments, item.EncodeEnchantments([]item.Enchantment{{ID: e.id, Level: lvl}}))
	}
	es := append(s.Enchantments(), item.Enchantment{ID: e.id, Level: lvl})
	return setComp(s, item.Enchantments, item.EncodeEnchantments(es))
}
