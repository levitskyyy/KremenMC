package translate

import (
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/pdef"
)

// knownPacks offers the client its own vanilla pack versions in place of the
// canonical one; the reply is mapped back in rewriteSB.
func (c *conn) knownPacks(q *packets.ClientboundConfigSelectKnownPacks) packets.Packet {
	out := &packets.ClientboundConfigSelectKnownPacks{}
	for _, p := range q.Packs {
		if p.Namespace == "minecraft" && p.ID == "core" && p.Version == packets.Version {
			for _, v := range c.t.last().meta.Packs {
				p.Version = v
				out.Packs = append(out.Packs, p)
			}
			continue
		}
		out.Packs = append(out.Packs, p)
	}
	return out
}

// registryData removes entries the client cannot load from its own packs and
// records how the remaining indices shift.
func (c *conn) registryData(q *packets.ClientboundConfigRegistryData) packets.Packet {
	reg := trimNS(q.Registry)
	tm := c.t.last().meta
	tab := make([]int32, len(q.Entries))
	names := make([]string, len(q.Entries))
	for i, e := range q.Entries {
		names[i] = trimNS(e.Key)
	}
	out := &packets.ClientboundConfigRegistryData{Registry: q.Registry}
	var known map[string]bool
	if len(tm.Registries) > 0 {
		tnames, ok := tm.Registries["minecraft:"+reg]
		if !ok {
			for i := range tab {
				tab[i] = -1
			}
			c.regs[reg] = &syncedReg{toTarget: tab, names: names}
			return nil
		}
		known = make(map[string]bool, len(tnames))
		for _, n := range tnames {
			known[trimNS(n)] = true
		}
	}
	plains := int32(0)
	for i, e := range q.Entries {
		if known != nil && !e.Value.Ok && !known[trimNS(e.Key)] {
			tab[i] = -1
			continue
		}
		if trimNS(e.Key) == "plains" {
			plains = int32(len(out.Entries))
		}
		tab[i] = int32(len(out.Entries))
		out.Entries = append(out.Entries, e)
	}
	c.regs[reg] = &syncedReg{toTarget: tab, names: names, n: len(out.Entries)}
	if reg == "worldgen/biome" {
		c.biomes = container{
			size: 64, minBits: 1, maxBits: 3,
			srcGlobal: bitsFor(len(q.Entries)), dstGlobal: bitsFor(len(out.Entries)),
			table: tab, fallback: plains,
		}
	}
	return out
}

// tagGroup remaps the tags of one registry. Static registries without a
// table are dropped (ok false).
func (c *conn) tagGroup(tagType string, tags []packets.Tag) ([]packets.Tag, bool) {
	m := c.t.maps
	var f func(int32) int32
	switch reg := trimNS(tagType); reg {
	case "block":
		f = m.block
	case "item":
		f = func(i int32) int32 {
			if i < 0 || int(i) >= len(m.itemKnown) || !m.itemKnown[i] {
				return -1
			}
			return m.items[i]
		}
	case "entity_type":
		f = m.entity
	case "fluid":
		f = func(i int32) int32 { return i }
	default:
		if r := c.regs[reg]; r != nil {
			f = func(i int32) int32 { return at(r.toTarget, i) }
		}
	}
	if f == nil {
		return nil, false
	}
	out := make([]packets.Tag, len(tags))
	for i, tag := range tags {
		e := make([]int32, 0, len(tag.Entries))
		for _, v := range tag.Entries {
			if tv := f(v); tv >= 0 {
				e = append(e, tv)
			}
		}
		out[i] = packets.Tag{TagName: tag.TagName, Entries: e}
	}
	return out, true
}

func (c *conn) configTags(q *packets.ClientboundConfigUpdateTags) packets.Packet {
	out := &packets.ClientboundConfigUpdateTags{}
	for _, t := range q.Tags {
		if tags, ok := c.tagGroup(t.TagType, t.Tags); ok {
			out.Tags = append(out.Tags, packets.ClientboundConfigUpdateTagsTag{TagType: t.TagType, Tags: tags})
		}
	}
	return out
}

func (c *conn) playTags(q *packets.ClientboundUpdateTags) packets.Packet {
	out := &packets.ClientboundUpdateTags{}
	for _, t := range q.Tags {
		if tags, ok := c.tagGroup(t.TagType, t.Tags); ok {
			out.Tags = append(out.Tags, packets.ClientboundUpdateTagsTag{TagType: t.TagType, Tags: tags})
		}
	}
	return out
}

// biomeDefaults is the biome container used before the connection has seen
// the biome registry: it assumes the server sends the canonical data order.
type biomeDefaults struct {
	container
}

func newBiomeDefaults(canon, tgt *verInfo) biomeDefaults {
	names := tgt.meta.Registries["minecraft:worldgen/biome"]
	known := make(map[string]bool, len(names))
	for _, n := range names {
		known[trimNS(n)] = true
	}
	if len(names) == 0 {
		for _, b := range tgt.reg.Biomes {
			known[b.Name] = true
		}
	}
	src := canon.reg.Biomes
	tab := make([]int32, len(src))
	n, plains := int32(0), int32(0)
	for i, b := range src {
		if !known[b.Name] {
			tab[i] = -1
			continue
		}
		if b.Name == "plains" {
			plains = n
		}
		tab[i] = n
		n++
	}
	return biomeDefaults{container{
		size: 64, minBits: 1, maxBits: 3,
		srcGlobal: bitsFor(len(src)), dstGlobal: bitsFor(int(n)),
		table: tab, fallback: plains,
	}}
}

// commands rewrites argument parser IDs of the command tree; arguments
// whose parser the client lacks become greedy strings.
func (c *conn) commands(raw []byte, w *proto.Writer) error {
	m := c.t.maps
	r := proto.Reader{B: raw}
	n, err := r.VarInt()
	if err != nil {
		return err
	}
	if n < 0 || int(n) > r.Remaining() {
		return errCount
	}
	w.VarInt(n)
	for range n {
		flags, err := r.U8()
		if err != nil {
			return err
		}
		w.U8(flags)
		k, err := r.VarInt()
		if err != nil {
			return err
		}
		if k < 0 || int(k) > r.Remaining() {
			return errCount
		}
		w.VarInt(k)
		for range k {
			v, err := r.VarInt()
			if err != nil {
				return err
			}
			w.VarInt(v)
		}
		if flags&0x08 != 0 {
			v, err := r.VarInt()
			if err != nil {
				return err
			}
			w.VarInt(v)
		}
		typ := flags & 3
		if typ == 1 || typ == 2 {
			name, err := r.String(32767)
			if err != nil {
				return err
			}
			w.String(name)
		}
		if typ == 2 {
			p, err := r.VarInt()
			if err != nil {
				return err
			}
			if p < 0 || int(p) >= len(m.parserProps) {
				return errUnknownValue
			}
			start := r.Off
			// parsers missing from the schema are newer than it and take no properties
			if props := m.parserProps[p]; props != nil {
				if err := pdef.Skip(props, &r); err != nil {
					return err
				}
			}
			if tp := m.parsers[p]; tp >= 0 {
				w.VarInt(tp)
				w.Raw(r.B[start:r.Off])
			} else {
				w.VarInt(m.stringParser)
				w.VarInt(2)
			}
		}
		if flags&0x10 != 0 {
			s, err := r.String(32767)
			if err != nil {
				return err
			}
			w.String(s)
		}
	}
	w.Raw(r.B[r.Off:])
	return nil
}
