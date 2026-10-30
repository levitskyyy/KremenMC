package server

import (
	"bytes"
	"encoding/json"

	"github.com/levitskyyy/KremenMC/internal/bridge"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/proto"
)

// Item stacks on the bridge wire (layout in internal/bridge/inv.go). Decoded
// components carry their raw bytes in "o" so Java can send untouched values
// back byte for byte.

const enchantmentReg = "minecraft:enchantment"

func itemToWire(s item.Stack) any {
	if s.IsEmpty() {
		return nil
	}
	c := map[string]any{}
	o := map[string]any{}
	var rm []any
	s.Patch.Each(func(t item.Type, v []byte, removed bool) {
		name := t.String()
		if removed {
			rm = append(rm, name)
			return
		}
		if d, ok := decodeComponent(t, v); ok {
			c[name] = d
			o[name] = bytes.Clone(v)
			return
		}
		c[name] = bytes.Clone(v)
	})
	m := map[string]any{"id": item.NameOf(s.Item), "count": int64(s.Count), "c": c, "o": o}
	if len(rm) > 0 {
		m["rm"] = rm
	}
	return m
}

func itemFromWire(v any) item.Stack {
	f, ok := v.(map[string]any)
	if !ok {
		return item.Stack{}
	}
	m := bridge.Fields(f)
	id, ok := item.IDOf(m.String("id"))
	n := int32(m.Int("count"))
	if !ok || id == 0 || n <= 0 {
		return item.Stack{}
	}
	s := item.Stack{Item: id, Count: min(n, 99)}
	for name, val := range m.Map("c") {
		t, ok := item.TypeByName(name)
		if !ok {
			continue
		}
		b, ok := val.([]byte)
		if !ok {
			if b, ok = encodeComponent(t, val); !ok {
				continue
			}
		}
		r := proto.Reader{B: b}
		if item.SkipValue(t, &r) != nil || r.Remaining() != 0 {
			continue
		}
		s = s.Set(t, b)
	}
	for _, x := range m.List("rm") {
		if name, ok := x.(string); ok {
			if t, ok := item.TypeByName(name); ok {
				s = s.Remove(t)
			}
		}
	}
	return s
}

func itemsToWire(items []item.Stack) []any {
	out := make([]any, len(items))
	for i, s := range items {
		out[i] = itemToWire(s)
	}
	return out
}

type rdr struct {
	r   proto.Reader
	err error
}

func (d *rdr) vi() int32 {
	v, err := d.r.VarInt()
	d.fail(err)
	return v
}
func (d *rdr) b() bool { v, err := d.r.Bool(); d.fail(err); return v }
func (d *rdr) s() string {
	v, err := d.r.String(1 << 15)
	d.fail(err)
	return v
}
func (d *rdr) n() int {
	v := d.vi()
	if v < 0 || v > 4096 {
		d.fail(item.ErrMalformed)
		return 0
	}
	return int(v)
}
func (d *rdr) fail(err error) {
	if d.err == nil && err != nil {
		d.err = err
	}
}
func (d *rdr) ok() bool { return d.err == nil && d.r.Remaining() == 0 }

func textToJSON(v []byte) (string, bool) {
	var t proto.Text
	if err := t.Decode(&proto.Reader{B: v}); err != nil {
		return "", false
	}
	b, err := json.Marshal(t)
	return string(b), err == nil
}

func textFromJSONBytes(s string) ([]byte, bool) {
	var t proto.Text
	if err := json.Unmarshal([]byte(s), &t); err != nil {
		t = proto.Plain(s)
	}
	return []byte(t.NBT()), true
}

func decodeComponent(t item.Type, v []byte) (any, bool) {
	d := &rdr{r: proto.Reader{B: v}}
	switch t {
	case item.CustomName, item.ItemName:
		return textToJSON(v)
	case item.Lore:
		n := d.n()
		out := make([]any, 0, n)
		for range n {
			start := d.r.Off
			var tx proto.Text
			if err := tx.Decode(&d.r); err != nil {
				return nil, false
			}
			j, ok := textToJSON(d.r.B[start:d.r.Off])
			if !ok {
				return nil, false
			}
			out = append(out, j)
		}
		return out, d.ok()
	case item.Enchantments, item.StoredEnchantments:
		es, err := item.DecodeEnchantments(v)
		if err != nil {
			return nil, false
		}
		out := map[string]any{}
		for _, e := range es {
			name := regName(enchantmentReg, e.ID)
			if name == "" {
				return nil, false
			}
			out[name] = int64(e.Level)
		}
		return out, true
	case item.Damage, item.MaxDamage, item.MaxStackSize, item.RepairCost:
		n := d.vi()
		return int64(n), d.ok()
	case item.DyedColor:
		n, err := d.r.I32()
		d.fail(err)
		return int64(n), d.ok()
	case item.Unbreakable, item.Glider:
		return true, len(v) == 0
	case item.EnchantmentGlintOverride:
		b := d.b()
		return b, d.ok()
	case item.ItemModel:
		s := d.s()
		return s, d.ok()
	case item.TooltipDisplay:
		hide := d.b()
		n := d.n()
		hidden := make([]any, 0, n)
		for range n {
			id := d.vi()
			if id < 0 || id >= int32(item.NumTypes) {
				return nil, false
			}
			hidden = append(hidden, item.Type(id).String())
		}
		return map[string]any{"hide": hide, "hidden": hidden}, d.ok()
	case item.CustomModelData:
		var floats, flags, strs, colors []any
		for range d.n() {
			f, err := d.r.F32()
			d.fail(err)
			floats = append(floats, float64(f))
		}
		for range d.n() {
			flags = append(flags, d.b())
		}
		for range d.n() {
			strs = append(strs, d.s())
		}
		for range d.n() {
			c, err := d.r.I32()
			d.fail(err)
			colors = append(colors, int64(c))
		}
		return map[string]any{"floats": orEmpty(floats), "flags": orEmpty(flags), "strings": orEmpty(strs), "colors": orEmpty(colors)}, d.ok()
	case item.Profile:
		return decodeProfile(d)
	}
	return nil, false
}

func orEmpty(l []any) []any {
	if l == nil {
		return []any{}
	}
	return l
}

func decodeProfile(d *rdr) (any, bool) {
	out := map[string]any{}
	if d.b() {
		u, err := d.r.UUID()
		d.fail(err)
		out["uuid"] = bridge.UUID(u)
		out["name"] = d.s()
	} else {
		if d.b() {
			out["name"] = d.s()
		}
		if d.b() {
			u, err := d.r.UUID()
			d.fail(err)
			out["uuid"] = bridge.UUID(u)
		}
	}
	var props []any
	for range d.n() {
		p := map[string]any{"name": d.s(), "value": d.s()}
		if d.b() {
			p["signature"] = d.s()
		}
		props = append(props, p)
	}
	out["properties"] = orEmpty(props)
	// skin patch: body, cape, elytra, model; only the empty patch is decoded
	for range 4 {
		if d.b() {
			return nil, false
		}
	}
	return out, d.ok()
}

func encodeComponent(t item.Type, v any) ([]byte, bool) {
	w := proto.Writer{}
	f, _ := v.(map[string]any)
	switch t {
	case item.CustomName, item.ItemName:
		s, ok := v.(string)
		if !ok {
			return nil, false
		}
		return textFromJSONBytes(s)
	case item.Lore:
		l, ok := v.([]any)
		if !ok {
			return nil, false
		}
		w.VarInt(int32(len(l)))
		for _, x := range l {
			s, _ := x.(string)
			b, _ := textFromJSONBytes(s)
			w.Raw(b)
		}
	case item.Enchantments, item.StoredEnchantments:
		if f == nil {
			return nil, false
		}
		var es []item.Enchantment
		for k, lv := range f {
			id, ok := regsync.Index(enchantmentReg, k)
			if !ok {
				continue
			}
			es = append(es, item.Enchantment{ID: id, Level: int32(bridge.ArgInt([]any{lv}, 0))})
		}
		sortEnchants(es)
		return item.EncodeEnchantments(es), true
	case item.Damage, item.MaxDamage, item.MaxStackSize, item.RepairCost:
		w.VarInt(int32(bridge.ArgInt([]any{v}, 0)))
	case item.DyedColor:
		w.I32(int32(bridge.ArgInt([]any{v}, 0)))
	case item.Unbreakable, item.Glider:
		return []byte{}, true
	case item.EnchantmentGlintOverride:
		w.Bool(bridge.ArgBool([]any{v}, 0))
	case item.ItemModel:
		w.String(bridge.ArgString([]any{v}, 0))
	case item.TooltipDisplay:
		m := bridge.Fields(f)
		w.Bool(m.Bool("hide"))
		var ids []int32
		for _, x := range m.List("hidden") {
			if n, ok := x.(string); ok {
				if tt, ok := item.TypeByName(n); ok {
					ids = append(ids, int32(tt))
				}
			}
		}
		w.VarInt(int32(len(ids)))
		for _, id := range ids {
			w.VarInt(id)
		}
	case item.CustomModelData:
		m := bridge.Fields(f)
		fl := m.List("floats")
		w.VarInt(int32(len(fl)))
		for _, x := range fl {
			w.F32(float32(bridge.ArgFloat([]any{x}, 0)))
		}
		bl := m.List("flags")
		w.VarInt(int32(len(bl)))
		for _, x := range bl {
			w.Bool(bridge.ArgBool([]any{x}, 0))
		}
		sl := m.List("strings")
		w.VarInt(int32(len(sl)))
		for _, x := range sl {
			w.String(bridge.ArgString([]any{x}, 0))
		}
		cl := m.List("colors")
		w.VarInt(int32(len(cl)))
		for _, x := range cl {
			w.I32(int32(bridge.ArgInt([]any{x}, 0)))
		}
	case item.Profile:
		m := bridge.Fields(f)
		u, hasU := m.UUID("uuid")
		name := m.String("name")
		if hasU && name != "" && len(name) <= 16 {
			w.Bool(true)
			w.UUID(u)
			w.String(name)
		} else {
			w.Bool(false)
			w.Bool(name != "")
			if name != "" {
				w.String(name)
			}
			w.Bool(hasU)
			if hasU {
				w.UUID(u)
			}
		}
		props := m.List("properties")
		w.VarInt(int32(len(props)))
		for _, x := range props {
			p := bridge.Fields(bridge.ArgMap([]any{x}, 0))
			w.String(p.String("name"))
			w.String(p.String("value"))
			sig := p.String("signature")
			w.Bool(sig != "")
			if sig != "" {
				w.String(sig)
			}
		}
		w.Raw([]byte{0, 0, 0, 0})
	default:
		return nil, false
	}
	return w.B, true
}

func sortEnchants(es []item.Enchantment) {
	for i := 1; i < len(es); i++ {
		for j := i; j > 0 && regName(enchantmentReg, es[j].ID) < regName(enchantmentReg, es[j-1].ID); j-- {
			es[j], es[j-1] = es[j-1], es[j]
		}
	}
}
