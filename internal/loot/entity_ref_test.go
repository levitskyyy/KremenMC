package loot

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

type testEntity struct {
	typ   string
	pos   api.Vec3
	fire  bool
	baby  bool
	size  int
	color string
	main  item.Stack
}

func (e *testEntity) TypeName() string   { return e.typ }
func (e *testEntity) Position() api.Vec3 { return e.pos }
func (e *testEntity) Flag(n string) bool {
	switch n {
	case "is_on_fire":
		return e.fire
	case "is_baby":
		return e.baby
	}
	return false
}
func (e *testEntity) Equipment(s string) item.Stack {
	if s == "mainhand" {
		return e.main
	}
	return item.Stack{}
}
func (e *testEntity) Component(n string) (json.RawMessage, bool) {
	if n == "minecraft:sheep/color" && e.color != "" {
		return json.RawMessage(strconv.Quote(e.color)), true
	}
	return nil, false
}
func (e *testEntity) TypeSpecific(kind, field string) (any, bool) {
	switch {
	case kind == "cube_mob" && field == "size" && e.size > 0:
		return e.size, true
	case kind == "sheep" && field == "sheared" && e.color != "":
		return false, true
	case kind == "raider" && field == "is_captain":
		return false, true
	}
	return nil, false
}
func (e *testEntity) Vehicle() Entity { return nil }

type entityCase struct {
	id                    int
	table, typ, flags, wp string
	byPlayer              bool
	damage                string
	seed                  int64
}

func (c entityCase) line() string {
	bp := "0"
	if c.byPlayer {
		bp = "1"
	}
	return fmt.Sprintf("entity|%d|%s|%s|%s|%s|%s|%s|%d", c.id, c.table, c.typ, c.flags, c.wp, bp, c.damage, c.seed)
}

func entityCases(t testing.TB) []entityCase {
	pk, err := wgdata.Load()
	if err != nil {
		t.Fatal(err)
	}
	var cs []entityCase
	for _, id := range pk.Keys("loot_table") {
		rest, ok := strings.CutPrefix(id, "minecraft:entities/")
		if !ok {
			continue
		}
		typ := "minecraft:" + strings.SplitN(rest, "/", 2)[0]
		if typ == "minecraft:player" {
			continue
		}
		for _, fl := range []string{"-", "fire", "baby"} {
			for _, wp := range []string{"-", `minecraft:diamond_sword[enchantments={"minecraft:looting":3}]`} {
				for _, bp := range []bool{false, true} {
					cs = append(cs, entityCase{id: len(cs), table: id, typ: typ, flags: fl, wp: wp, byPlayer: bp, damage: "generic", seed: int64(len(cs))*104729 + 3})
				}
			}
		}
		cs = append(cs, entityCase{id: len(cs), table: id, typ: typ, flags: "-", wp: "-", byPlayer: false, damage: "lightning", seed: int64(len(cs)) + 11})
	}
	return cs
}

func TestEntityTablesMatchReference(t *testing.T) {
	cs := entityCases(t)
	var in strings.Builder
	for _, c := range cs {
		in.WriteString(c.line() + "\n")
	}
	inPath := filepath.Join(t.TempDir(), "loot_entities_in.txt")
	if os.Getenv("KREMENMC_REF") == "1" {
		if err := os.WriteFile(inPath, []byte(in.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden := reftest.Golden(t, "loot_entities_out.txt", "kremenmc.probe.dig.Loot", inPath, "digest")
	want := map[string]string{}
	full := map[int]string{}
	ents := map[int][]string{}
	sc := bufio.NewScanner(bytes.NewReader(golden))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		switch f[0] {
		case "digest":
			want[f[1]] = f[2] + " " + f[3]
		case "entity":
			id, _ := strconv.Atoi(f[1])
			full[id] = sc.Text()
		case "ent":
			id, _ := strconv.Atoi(f[1])
			ents[id] = f[2:]
		case "error":
			t.Errorf("probe: %s", sc.Text())
		}
	}
	got := map[string][]string{}
	for _, c := range cs {
		st := ents[c.id]
		if len(st) < 4 {
			t.Fatalf("no entity state for case %d", c.id)
		}
		e := &testEntity{typ: c.typ, pos: api.Vec3{X: 0.5, Y: 100, Z: 0.5}, fire: st[0] == "true", baby: st[1] == "true"}
		if st[2] != "-" {
			e.size, _ = strconv.Atoi(st[2])
		}
		if st[3] != "-" {
			e.color = st[3]
		}
		killer := &testEntity{typ: "minecraft:player", pos: api.Vec3{}, main: parseSpec(t, c.wp)}
		ctx := &Context{Rand: rng.NewXoroshiro(c.seed), This: e, Origin: e.pos, HasOrigin: true}
		ds := &DamageSource{Type: "minecraft:generic", Tags: func(string) bool { return false }}
		if c.byPlayer {
			ds = &DamageSource{Type: "minecraft:player_attack", Source: killer, Direct: killer, Tags: func(string) bool { return false }}
			ctx.Attacker, ctx.DirectAttacker, ctx.LastPlayer = killer, killer, killer
		}
		if c.damage == "lightning" {
			ds = &DamageSource{Type: "minecraft:lightning_bolt", Tags: func(tag string) bool { return tag == "minecraft:is_lightning" }}
		}
		ctx.Damage = ds
		res := Roll(c.table, ctx)
		line := fmt.Sprintf("entity %d %s %d", c.id, encodeStacks(res), ctx.Rand.NextLong())
		if w, ok := full[c.id]; ok && w != line {
			t.Errorf("%s\n got %s\nwant %s", c.line(), line, w)
		}
		got[c.table] = append(got[c.table], line)
	}
	bad := 0
	for _, d := range digest(got) {
		f := strings.Fields(d)
		if w := want[f[0]]; w != f[1]+" "+f[2] {
			bad++
			if bad <= 20 {
				t.Errorf("table %s: digest %s %s, reference %q", f[0], f[1], f[2], w)
			}
		}
	}
	t.Logf("%d rolls over %d tables, %d mismatching tables", len(cs), len(got), bad)
}
