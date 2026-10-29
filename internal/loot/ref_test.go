package loot

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

var refTools = []string{
	"-",
	"minecraft:wooden_pickaxe",
	"minecraft:diamond_pickaxe",
	`minecraft:diamond_pickaxe[enchantments={"minecraft:silk_touch":1}]`,
	`minecraft:diamond_pickaxe[enchantments={"minecraft:fortune":1}]`,
	`minecraft:diamond_pickaxe[enchantments={"minecraft:fortune":3}]`,
	"minecraft:shears",
	`minecraft:diamond_axe[enchantments={"minecraft:fortune":2}]`,
	"minecraft:diamond_shovel",
	`minecraft:netherite_hoe[enchantments={"minecraft:fortune":3,"minecraft:silk_touch":1}]`,
}

var specRe = regexp.MustCompile(`^([a-z_:]+)(?:\[enchantments=\{(.*)\}\])?$`)

// parseSpec builds the stack the probe's ItemParser builds for spec.
func parseSpec(t testing.TB, spec string) item.Stack {
	if spec == "-" {
		return item.Stack{}
	}
	m := specRe.FindStringSubmatch(spec)
	if m == nil {
		t.Fatalf("bad spec %s", spec)
	}
	id, ok := item.IDOf(m[1])
	if !ok {
		t.Fatalf("unknown item %s", m[1])
	}
	st := item.Stack{Item: id, Count: 1}
	if m[2] != "" {
		var es []item.Enchantment
		for _, kv := range strings.Split(m[2], ",") {
			i := strings.LastIndex(kv, ":")
			name, v := strings.Trim(kv[:i], `"`), kv[i+1:]
			eid, ok := regsync.Index("minecraft:enchantment", name)
			if !ok {
				t.Fatalf("enchantment %s", name)
			}
			lvl, _ := strconv.Atoi(v)
			es = append(es, item.Enchantment{ID: eid, Level: int32(lvl)})
		}
		st = st.Set(item.Enchantments, item.EncodeEnchantments(es))
	}
	return st
}

type blockCase struct {
	id        int
	table     string
	state     data.StateID
	tool      string
	explosion float32
	seed      int64
}

func blockCases(t testing.TB) []blockCase {
	var cs []blockCase
	reg := data.Canonical()
	for i := range reg.Blocks {
		b := &reg.Blocks[i]
		tid := BlockTable(b)
		if tid == "" {
			continue
		}
		states := []data.StateID{b.DefaultState}
		if len(b.Props) > 0 {
			vals := make([]int, len(b.Props))
			for j := range b.Props {
				vals[j] = len(b.Props[j].Values) - 1
			}
			if s := b.StateAt(vals); s != b.DefaultState {
				states = append(states, s)
			}
		}
		for si, st := range states {
			for ti, tool := range refTools {
				seed := int64(len(cs))*7919 + 17
				cs = append(cs, blockCase{id: len(cs), table: tid, state: st, tool: tool, seed: seed})
				if ti%3 == 0 && si == 0 {
					cs = append(cs, blockCase{id: len(cs), table: tid, state: st, tool: tool, explosion: float32(1 + ti%5), seed: seed + 1})
				}
			}
		}
	}
	return cs
}

func (c blockCase) line() string {
	ex := "-"
	if c.explosion > 0 {
		ex = strconv.FormatFloat(float64(c.explosion), 'f', 1, 32)
	}
	return fmt.Sprintf("block|%d|%s|%s|%s|%s|%d", c.id, c.table, data.StateString(c.state), c.tool, ex, c.seed)
}

func encodeStacks(stacks []item.Stack) string {
	if len(stacks) == 0 {
		return "-"
	}
	parts := make([]string, len(stacks))
	for i, s := range stacks {
		parts[i] = hex.EncodeToString(item.AppendStack(nil, s))
	}
	return strings.Join(parts, ",")
}

// digest groups result lines by table into crc32 digests, the form the
// golden keeps (full lines with KREMENMC_LOOT_FULL=1 when debugging).
func digest(lines map[string][]string) []string {
	var out []string
	for tbl, ls := range lines {
		out = append(out, fmt.Sprintf("%s %08x %d", tbl, crc32.ChecksumIEEE([]byte(strings.Join(ls, "\n"))), len(ls)))
	}
	return out
}

func TestBlockTablesMatchReference(t *testing.T) {
	cs := blockCases(t)
	var in strings.Builder
	for _, c := range cs {
		in.WriteString(c.line() + "\n")
	}
	inPath := filepath.Join(t.TempDir(), "loot_blocks_in.txt")
	if os.Getenv("KREMENMC_REF") == "1" {
		if err := os.WriteFile(inPath, []byte(in.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden := reftest.Golden(t, "loot_blocks_out.txt", "kremenmc.probe.dig.Loot", inPath, "digest")
	want := map[string]string{}
	full := map[int]string{}
	sc := bufio.NewScanner(bytes.NewReader(golden))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		switch {
		case len(f) == 4 && f[0] == "digest":
			want[f[1]] = f[2] + " " + f[3]
		case len(f) >= 4 && f[0] == "block":
			id, _ := strconv.Atoi(f[1])
			full[id] = sc.Text()
		case f[0] == "error":
			t.Errorf("probe: %s", sc.Text())
		}
	}
	tools := map[string]item.Stack{}
	for _, s := range refTools {
		tools[s] = parseSpec(t, s)
	}
	got := map[string][]string{}
	for _, c := range cs {
		ctx := &Context{
			Rand: rng.NewXoroshiro(c.seed), Origin: api.Vec3{X: 1.5, Y: 100.5, Z: 0.5}, HasOrigin: true,
			State: c.state, HasState: true, Tool: tools[c.tool], HasTool: true,
		}
		if c.explosion > 0 {
			ctx.ExplosionRadius, ctx.HasExplosion = c.explosion, true
		}
		res := Roll(c.table, ctx)
		line := fmt.Sprintf("block %d %s %d", c.id, encodeStacks(res), ctx.Rand.NextLong())
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
	if len(want) == 0 {
		t.Fatal("empty golden")
	}
	t.Logf("%d rolls over %d tables, %d mismatching tables", len(cs), len(got), bad)
}
