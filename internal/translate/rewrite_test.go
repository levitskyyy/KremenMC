package translate

import (
	"reflect"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/meta"
	"github.com/levitskyyy/KremenMC/internal/translate/pdef"
	"github.com/levitskyyy/KremenMC/internal/translate/v775"
	"github.com/levitskyyy/KremenMC/internal/translate/v776"
)

func canonEnum(t *testing.T, kind, name string) int32 {
	t.Helper()
	m, _ := meta.Load(packets.Protocol)
	i := m.Index(kind, name)
	if i < 0 {
		t.Fatalf("canonical %s %s missing", kind, name)
	}
	return i
}

func canonItem(t *testing.T, name string) int32 {
	t.Helper()
	i, ok := data.Canonical().ItemByName(name)
	if !ok {
		t.Fatalf("item %s", name)
	}
	return int32(i)
}

func TestKnownPacks(t *testing.T) {
	for _, p := range Default().Supported() {
		c := lookup(t, p)
		cb := &packets.ClientboundConfigSelectKnownPacks{Packs: []packets.ClientboundConfigSelectKnownPacksPack{{Namespace: "minecraft", ID: "core", Version: packets.Version}}}
		bodies := encode(t, c, packets.Configuration, cb)
		q := checkFrame(t, p, packets.Configuration, packets.Clientbound, bodies[0], "select_known_packs")
		packs := reflect.ValueOf(q).Elem().FieldByName("Packs")
		if packs.Len() != len(c.t.last().meta.Packs) || packs.Index(0).FieldByName("Version").String() != c.t.last().meta.Packs[0] {
			t.Errorf("%d: offered %+v", p, q)
		}
		reply := &packets.ServerboundConfigSelectKnownPacks{Packs: []packets.ServerboundConfigSelectKnownPacksPack{{Namespace: "minecraft", ID: "core", Version: c.t.last().meta.Packs[0]}}}
		id, _ := c.t.last().id(packets.Configuration, packets.Serverbound, "select_known_packs")
		var w proto.Writer
		w.VarInt(id)
		reply.Encode(&w)
		out, err := c.Decode(packets.Configuration, w.B)
		if err != nil || len(out) != 1 {
			t.Fatal(out, err)
		}
		if v := out[0].(*packets.ServerboundConfigSelectKnownPacks).Packs[0].Version; v != packets.Version {
			t.Errorf("%d: reply version %s", p, v)
		}
	}
}

func TestRegistryDataAndBiomes(t *testing.T) {
	c := lookup(t, 776)
	entries := []packets.ClientboundConfigRegistryDataEntry{
		{Key: "minecraft:badlands"}, {Key: "minecraft:dappled_forest"}, {Key: "minecraft:plains"},
		{Key: "plugin:custom", Value: proto.Some(proto.RawNBT{10, 0})}, {Key: "minecraft:desert"},
	}
	q := encodeOne(t, c, packets.Configuration, &packets.ClientboundConfigRegistryData{Registry: "minecraft:worldgen/biome", Entries: entries}, "registry_data").(*v776.ClientboundConfigRegistryData)
	var keys []string
	for _, e := range q.Entries {
		keys = append(keys, e.Key)
	}
	if !reflect.DeepEqual(keys, []string{"minecraft:badlands", "minecraft:plains", "plugin:custom", "minecraft:desert"}) {
		t.Fatalf("entries %v", keys)
	}
	if got := c.regIndex("worldgen/biome", 4); got != 3 {
		t.Errorf("desert index %d", got)
	}
	// a registry the client does not have is not sent
	if n := len(encode(t, c, packets.Configuration, &packets.ClientboundConfigRegistryData{Registry: "plugin:things"})); n != 0 {
		t.Errorf("unknown registry sent")
	}

	// chunk biomes follow the connection's biome indices: dappled_forest -> plains
	var sec proto.Writer
	sec.I16(0)
	sec.I16(0)
	sec.U8(0)
	sec.VarInt(0) // air
	sec.U8(1)
	sec.VarInt(2) // indirect palette of two biomes
	sec.VarInt(1) // dappled_forest
	sec.VarInt(4) // desert
	sec.U64(0)
	ch := &packets.ClientboundLevelChunkWithLight{ChunkData: sec.B}
	r := encodeOne(t, c, packets.Play, ch, "level_chunk_with_light").(*v776.ClientboundLevelChunkWithLight)
	rd := proto.Reader{B: r.ChunkData[4+2:]}
	bits, _ := rd.U8()
	n, _ := rd.VarInt()
	a, _ := rd.VarInt()
	b, _ := rd.VarInt()
	if bits != 1 || n != 2 || a != 1 || b != 3 {
		t.Errorf("biome palette bits=%d n=%d [%d %d]", bits, n, a, b)
	}
}

func TestTags(t *testing.T) {
	c := lookup(t, 775)
	canon := data.Canonical()
	stone, _ := canon.BlockByName("stone")
	poplar, _ := canon.BlockByName("poplar_planks")
	zombie, _ := canon.EntityByName("zombie")
	tags := &packets.ClientboundConfigUpdateTags{Tags: []packets.ClientboundConfigUpdateTagsTag{
		{TagType: "minecraft:block", Tags: []packets.Tag{{TagName: "minecraft:mineable/pickaxe", Entries: []int32{int32(stone.ID), int32(poplar.ID)}}}},
		{TagType: "minecraft:item", Tags: []packets.Tag{{TagName: "minecraft:planks", Entries: []int32{canonItem(t, "poplar_planks"), canonItem(t, "oak_planks")}}}},
		{TagType: "minecraft:entity_type", Tags: []packets.Tag{{TagName: "minecraft:undead", Entries: []int32{int32(zombie.ID)}}}},
		{TagType: "minecraft:game_event", Tags: []packets.Tag{{TagName: "minecraft:vibrations", Entries: []int32{1}}}},
	}}
	q := encodeOne(t, c, packets.Configuration, tags, "update_tags").(*v775.ClientboundConfigUpdateTags)
	tr := c.t.last().reg
	if len(q.Tags) != 3 {
		t.Fatalf("%d tag registries", len(q.Tags))
	}
	ts, _ := tr.BlockByName("stone")
	if e := q.Tags[0].Tags[0].Entries; len(e) != 1 || e[0] != int32(ts.ID) {
		t.Errorf("block tag %v", e)
	}
	if e := q.Tags[1].Tags[0].Entries; len(e) != 1 || tr.ItemName(data.ItemID(e[0])) != "oak_planks" {
		t.Errorf("item tag %v", e)
	}
	if e := q.Tags[2].Tags[0].Entries; len(e) != 1 || tr.Entity(data.EntityID(e[0])).Name != "zombie" {
		t.Errorf("entity tag %v", e)
	}
}

// canonSlot builds a diamond sword named "x" with damage 5, a bundle holding
// poplar planks and a 26.3-only component.
func canonSlot(t *testing.T, w *proto.Writer) {
	w.VarInt(1)
	w.VarInt(canonItem(t, "diamond_sword"))
	w.VarInt(4)
	w.VarInt(0)
	w.VarInt(canonEnum(t, meta.Component, "custom_name"))
	w.U8(8) // NBT string tag
	w.U16(1)
	w.U8('x')
	w.VarInt(canonEnum(t, meta.Component, "damage"))
	w.VarInt(5)
	w.VarInt(canonEnum(t, meta.Component, "mob_visibility"))
	w.VarInt(2)
	w.VarInt(5)
	w.F32(0.5)
	w.VarInt(canonEnum(t, meta.Component, "bundle_contents"))
	w.VarInt(1)
	w.VarInt(canonItem(t, "poplar_planks"))
	w.VarInt(3)
	w.VarInt(0)
	w.VarInt(0)
}

type slotView struct {
	item  string
	count int32
	comps []string
	inner []string
}

func readSlot(t *testing.T, c *conn, r *proto.Reader) slotView {
	t.Helper()
	tm := c.t.last().meta
	cases, _, _ := pdef.Compile(tm.Types).Cases("SlotComponent")
	cnt, _ := r.VarInt()
	if cnt == 0 {
		return slotView{}
	}
	id, _ := r.VarInt()
	v := slotView{item: c.t.last().reg.ItemName(data.ItemID(id)), count: cnt}
	add, _ := r.VarInt()
	rem, _ := r.VarInt()
	for range add {
		typ, _ := r.VarInt()
		name := tm.Enums[meta.Component][typ]
		v.comps = append(v.comps, name)
		if name == "bundle_contents" {
			n, _ := r.VarInt()
			for range n {
				iid, _ := r.VarInt()
				if c.t.maps.templateAsSlot {
					iid, _ = r.VarInt() // slot form: count first
				} else {
					r.VarInt()
				}
				r.VarInt()
				r.VarInt()
				v.inner = append(v.inner, c.t.last().reg.ItemName(data.ItemID(iid)))
			}
			continue
		}
		if err := pdef.Skip(cases[name], r); err != nil {
			t.Fatalf("component %s: %v", name, err)
		}
	}
	if rem != 0 {
		t.Fatalf("removals %d", rem)
	}
	return v
}

func TestSlotRewrite(t *testing.T) {
	for _, p := range Default().Supported() {
		c := lookup(t, p)
		var w proto.Writer
		canonSlot(t, &w)
		q := encodeOne(t, c, packets.Play, &packets.ClientboundContainerSetSlot{WindowID: 1, Slot: 36, Raw: w.B}, "container_set_slot")
		raw := reflect.ValueOf(q).Elem().FieldByName("Raw").Bytes()
		r := proto.Reader{B: raw}
		v := readSlot(t, c, &r)
		if r.Remaining() != 0 {
			t.Fatalf("%d: %d trailing bytes", p, r.Remaining())
		}
		want := slotView{item: "diamond_sword", count: 1, comps: []string{"custom_name", "damage", "bundle_contents"}, inner: []string{"stone"}}
		if !reflect.DeepEqual(v, want) {
			t.Errorf("%d: %+v", p, v)
		}

		// container contents: two slots plus the carried item
		w.B = w.B[:0]
		w.VarInt(2)
		canonSlot(t, &w)
		w.VarInt(0)
		w.VarInt(0)
		q = encodeOne(t, c, packets.Play, &packets.ClientboundContainerSetContent{Raw: w.B}, "container_set_content")
		r = proto.Reader{B: reflect.ValueOf(q).Elem().FieldByName("Raw").Bytes()}
		if n, _ := r.VarInt(); n != 2 {
			t.Fatal(n)
		}
		if v := readSlot(t, c, &r); v.item != "diamond_sword" {
			t.Errorf("%d: content %+v", p, v)
		}
		readSlot(t, c, &r)
		readSlot(t, c, &r)
		if r.Remaining() != 0 {
			t.Errorf("%d: content trailing %d", p, r.Remaining())
		}
	}
}

func TestCreativeSlotUp(t *testing.T) {
	c := lookup(t, 775)
	tr := c.t.last().reg
	sword, _ := tr.ItemByName("diamond_sword")
	tm := c.t.last().meta
	var w proto.Writer
	id, _ := c.t.last().id(packets.Play, packets.Serverbound, "set_creative_mode_slot")
	w.VarInt(id)
	w.I16(36)
	w.VarInt(1)
	w.VarInt(int32(sword))
	w.VarInt(1)
	w.VarInt(0)
	w.VarInt(tm.Index(meta.Component, "damage"))
	w.ByteArray([]byte{5})
	out, err := c.Decode(packets.Play, w.B)
	if err != nil || len(out) != 1 {
		t.Fatal(out, err)
	}
	q := out[0].(*packets.ServerboundSetCreativeModeSlot)
	r := proto.Reader{B: q.Raw}
	r.VarInt()
	if item, _ := r.VarInt(); item != canonItem(t, "diamond_sword") {
		t.Errorf("item %d", item)
	}
	r.VarInt()
	r.VarInt()
	if typ, _ := r.VarInt(); typ != canonEnum(t, meta.Component, "damage") {
		t.Errorf("component %d", typ)
	}
}

func TestEntityMetadata(t *testing.T) {
	for _, p := range Default().Supported() {
		c := lookup(t, p)
		canon := data.Canonical()
		itemType, _ := canon.EntityByName("item")
		encode(t, c, packets.Play, &packets.ClientboundAddEntity{EntityID: 40, Type: int32(itemType.ID)})
		var w proto.Writer
		w.U8(0)
		w.VarInt(canonEnum(t, meta.Metadata, "byte"))
		w.I8(0x20)
		w.U8(8)
		w.VarInt(canonEnum(t, meta.Metadata, "item_stack"))
		w.VarInt(2)
		w.VarInt(canonItem(t, "poplar_planks"))
		w.VarInt(0)
		w.VarInt(0)
		w.U8(9) // not an item entity field: dropped
		w.VarInt(canonEnum(t, meta.Metadata, "int"))
		w.VarInt(3)
		w.U8(0xff)
		q := encodeOne(t, c, packets.Play, &packets.ClientboundSetEntityData{EntityID: 40, Raw: w.B}, "set_entity_data")
		r := proto.Reader{B: reflect.ValueOf(q).Elem().FieldByName("Raw").Bytes()}
		tm := c.t.last().meta
		k, _ := r.U8()
		s, _ := r.VarInt()
		b, _ := r.I8()
		if k != 0 || tm.Enums[meta.Metadata][s] != "byte" || b != 0x20 {
			t.Fatalf("%d: first entry %d %d %d", p, k, s, b)
		}
		k, _ = r.U8()
		s, _ = r.VarInt()
		if k != 8 || tm.Enums[meta.Metadata][s] != "item_stack" {
			t.Fatalf("%d: second entry %d %d", p, k, s)
		}
		if v := readSlot(t, c, &r); v.item != "stone" || v.count != 2 {
			t.Errorf("%d: item %+v", p, v)
		}
		if end, _ := r.U8(); end != 0xff || r.Remaining() != 0 {
			t.Errorf("%d: entry 9 kept or bad terminator", p)
		}
	}
}

func TestParticles(t *testing.T) {
	poplar, _ := data.Canonical().DefaultState("poplar_planks")
	var w proto.Writer
	w.VarInt(canonEnum(t, meta.Particle, "block"))
	w.VarInt(int32(poplar))
	w.Bool(false)
	w.Bool(true)
	w.F64(1)
	w.F64(2)
	w.F64(3)
	w.F32(0.1)
	w.F32(0.2)
	w.F32(0.3)
	w.F32(0.5)
	w.F32(0.7)
	w.F32(0.6)
	w.VarInt(12)
	w.VarInt(0)
	for _, p := range Default().Supported() {
		c := lookup(t, p)
		q := encodeOne(t, c, packets.Play, &packets.ClientboundLevelParticles{Raw: w.B}, "level_particles")
		v := reflect.ValueOf(q).Elem()
		if v.FieldByName("Amount").Int() != 12 || v.FieldByName("Y").Float() != 2 || v.FieldByName("VelocityOffset").Float() != float64(float32(0.7)) || (v.FieldByName("AlwaysShow").IsValid() && !v.FieldByName("AlwaysShow").Bool()) {
			t.Errorf("%d: %+v", p, q)
		}
		r := proto.Reader{B: v.FieldByName("Raw").Bytes()}
		typ, _ := r.VarInt()
		st, _ := r.VarInt()
		if c.t.last().meta.Enums[meta.Particle][typ] != "block" || sname(c.t.last().reg, data.StateID(st)) != "stone" {
			t.Errorf("%d: particle %d state %d", p, typ, st)
		}
	}
	// particles the client lacks drop the packet
	w.B = w.B[:0]
	w.VarInt(canonEnum(t, meta.Particle, "red_poplar_leaves"))
	c := lookup(t, 776)
	if n := len(encode(t, c, packets.Play, &packets.ClientboundLevelParticles{Raw: w.B})); n != 0 {
		t.Errorf("unknown particle sent")
	}
}

func TestCommands(t *testing.T) {
	var w proto.Writer
	w.VarInt(4)
	w.U8(0) // root
	w.VarInt(1)
	w.VarInt(1)
	w.U8(1) // literal "tp"
	w.VarInt(2)
	w.VarInt(2)
	w.VarInt(3)
	w.String("tp")
	w.U8(2 | 4) // argument "target": entity, single
	w.VarInt(0)
	w.String("target")
	w.VarInt(6)
	w.U8(1)
	w.U8(2 | 4 | 0x10) // argument with a 26.3-only parser and suggestions
	w.VarInt(0)
	w.String("anim")
	w.VarInt(60) // minecraft:swing_animation
	w.String("minecraft:ask_server")
	w.VarInt(0)
	for _, p := range Default().Supported() {
		c := lookup(t, p)
		q := encodeOne(t, c, packets.Play, &packets.ClientboundCommands{Raw: w.B}, "commands")
		raw := reflect.ValueOf(q).Elem().FieldByName("Raw").Bytes()
		names, _ := ParserNames(p)
		r := proto.Reader{B: raw}
		r.VarInt()
		var parsers []string
		for range 4 {
			flags, _ := r.U8()
			k, _ := r.VarInt()
			for range k {
				r.VarInt()
			}
			if flags&3 != 0 {
				r.String(100)
			}
			if flags&3 == 2 {
				id, _ := r.VarInt()
				parsers = append(parsers, string(names[id]))
				switch names[id] {
				case "minecraft:entity":
					r.U8()
				case "brigadier:string":
					if mode, _ := r.VarInt(); mode != 2 {
						t.Errorf("%d: string mode %d", p, mode)
					}
				}
			}
			if flags&0x10 != 0 {
				r.String(100)
			}
		}
		if root, _ := r.VarInt(); root != 0 || r.Remaining() != 0 {
			t.Errorf("%d: tail", p)
		}
		if !reflect.DeepEqual(parsers, []string{"minecraft:entity", "brigadier:string"}) {
			t.Errorf("%d: parsers %v", p, parsers)
		}
	}
}

func TestSoundFallback(t *testing.T) {
	c := lookup(t, 776)
	cm := c.t.vers[0].meta
	var missing int32 = -1
	for i := range cm.Enums[meta.Sound] {
		if c.t.maps.sounds[i] < 0 {
			missing = int32(i)
			break
		}
	}
	snd := &packets.ClientboundSound{Sound: proto.Holder[packets.ItemSoundEvent]{ID: 0}, SoundCategory: 10, Volume: 1}
	q := encodeOne(t, c, packets.Play, snd, "sound").(*v776.ClientboundSound)
	if q.Sound.Value != nil || q.Sound.ID != 0 || q.SoundCategory != 10 {
		t.Errorf("known sound: %+v", q)
	}
	if missing >= 0 {
		snd.Sound.ID = missing
		q = encodeOne(t, c, packets.Play, snd, "sound").(*v776.ClientboundSound)
		if q.Sound.Value == nil || q.Sound.Value.SoundName != "minecraft:"+cm.Enums[meta.Sound][missing] {
			t.Errorf("missing sound not inlined: %+v", q.Sound)
		}
	}
}
