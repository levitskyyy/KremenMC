package translate

import (
	"reflect"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/meta"
	"github.com/levitskyyy/KremenMC/internal/translate/v774"
)

func TestSetTime774(t *testing.T) {
	c := lookup(t, 774)
	encode(t, c, packets.Configuration, &packets.ClientboundConfigRegistryData{Registry: "minecraft:world_clock",
		Entries: []packets.ClientboundConfigRegistryDataEntry{{Key: "minecraft:the_end"}, {Key: "minecraft:overworld"}}})
	p := &packets.ClientboundSetTime{Age: 100, ClockUpdates: []packets.ClientboundSetTimeClockUpdate{{ID: 0, TotalTicks: 5}, {ID: 1, TotalTicks: 6000, Rate: 1}}}
	q := encodeOne(t, c, packets.Play, p, "set_time").(*v774.ClientboundSetTime)
	if q.Age != 100 || q.Time != 6000 || !q.TickDayTime {
		t.Errorf("%+v", q)
	}
	q = encodeOne(t, c, packets.Play, &packets.ClientboundSetTime{Age: 101}, "set_time").(*v774.ClientboundSetTime)
	if q.Time != 6000 {
		t.Errorf("day time not kept: %+v", q)
	}
}

func TestInteract774(t *testing.T) {
	c := lookup(t, 774)
	id, _ := c.t.last().id(packets.Play, packets.Serverbound, "interact")
	dec := func(p *v774.ServerboundInteract) []packets.Packet {
		var w proto.Writer
		w.VarInt(id)
		p.Encode(&w)
		out, err := c.Decode(packets.Play, w.B)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := dec(&v774.ServerboundInteract{Target: 7, Mouse: 1}); len(out) != 1 || !reflect.DeepEqual(out[0], &packets.ServerboundAttack{EntityID: 7}) {
		t.Errorf("attack: %#v", out)
	}
	out := dec(&v774.ServerboundInteract{Target: 7, Mouse: 2, X: 0.5, Y: 1, Hand: 1, Sneaking: true})
	if len(out) != 1 {
		t.Fatal(out)
	}
	if q := out[0].(*packets.ServerboundInteract); q.Target != 7 || q.Hand != 1 || !q.Sneaking || q.Location.Y < 0.99 || q.Location.Y > 1.01 {
		t.Errorf("interact at: %+v", q)
	}
	if out := dec(&v774.ServerboundInteract{Target: 7, Mouse: 0}); len(out) != 0 {
		t.Errorf("plain interact duplicated: %#v", out)
	}
}

func TestMetadataIndexRemap(t *testing.T) {
	c := lookup(t, 774)
	sheep, _ := data.Canonical().EntityByName("sheep")
	encode(t, c, packets.Play, &packets.ClientboundAddEntity{EntityID: 50, Type: int32(sheep.ID)})
	keys := c.t.vers[0].meta.MetadataKeys["sheep"]
	idx := func(l []string, k string) int {
		for i, n := range l {
			if n == k {
				return i
			}
		}
		t.Fatalf("no %s", k)
		return -1
	}
	var w proto.Writer
	w.U8(uint8(idx(keys, "age_locked")))
	w.VarInt(canonEnum(t, meta.Metadata, "boolean"))
	w.Bool(true)
	w.U8(uint8(idx(keys, "wool")))
	w.VarInt(canonEnum(t, meta.Metadata, "byte"))
	w.I8(0x13)
	w.U8(0xff)
	q := encodeOne(t, c, packets.Play, &packets.ClientboundSetEntityData{EntityID: 50, Raw: w.B}, "set_entity_data").(*v774.ClientboundSetEntityData)
	want := []byte{byte(idx(c.t.last().meta.MetadataKeys["sheep"], "wool")), byte(c.t.last().meta.Index(meta.Metadata, "byte")), 0x13, 0xff}
	if !reflect.DeepEqual(q.Raw, want) {
		t.Errorf("got % x, want % x", q.Raw, want)
	}
}
