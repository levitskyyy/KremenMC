package server

import (
	"bufio"
	"bytes"
	"fmt"
	"hash/crc32"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/loot"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

var digSpecRe = regexp.MustCompile(`^([a-z_:]+)(?:\[enchantments=\{(.*)\}\])?$`)

func digStack(t testing.TB, spec string) item.Stack {
	if spec == "-" {
		return item.Stack{}
	}
	m := digSpecRe.FindStringSubmatch(spec)
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
			eid, ok := regsync.Index("minecraft:enchantment", strings.Trim(kv[:i], `"`))
			if !ok {
				t.Fatalf("enchantment in %s", spec)
			}
			lvl, _ := strconv.Atoi(kv[i+1:])
			es = append(es, item.Enchantment{ID: eid, Level: int32(lvl)})
		}
		st = st.Set(item.Enchantments, item.EncodeEnchantments(es))
	}
	return st
}

var digTarget = api.BlockPos{X: 1, Y: 100, Z: 0}

// traceEnv models the probe's world: one target block, a digging player
// and an observer, recording what each receives like the probe prints it.
type traceEnv struct {
	t          testing.TB
	st, orig   data.StateID
	in         digInputs
	gm         api.GameMode
	self, obs  []string
	drops, dmg int32
}

func (e *traceEnv) air(api.BlockPos) bool { return data.IsAir(e.st) }
func (e *traceEnv) progress(api.BlockPos) float32 {
	return destroyProgress(e.st, &e.in)
}
func (e *traceEnv) creative() bool { return e.gm == api.Creative }
func (e *traceEnv) restricted(api.BlockPos) bool {
	switch e.gm {
	case api.Spectator:
		return true
	case api.Adventure:
		return e.in.main.IsEmpty() || !canBreakAdventure(e.in.main, e.st)
	}
	return false
}
func (e *traceEnv) mayInteract(api.BlockPos) bool           { return true }
func (e *traceEnv) interactDenied(api.BlockPos, int32) bool { return false }
func (e *traceEnv) resync(pos api.BlockPos) {
	if pos == digTarget {
		e.self = append(e.self, " upd:"+e.blk())
	}
}
func (e *traceEnv) blk() string {
	if data.IsAir(e.st) {
		return "air"
	}
	return "blk"
}
func (e *traceEnv) sendProgress(_ api.BlockPos, j int32) {
	e.obs = append(e.obs, fmt.Sprintf(" d%d", j))
}
func (e *traceEnv) hitEvent(id int32, _ api.BlockPos, face int32) {
	e.self = append(e.self, fmt.Sprintf(" selfev%d", id))
	e.obs = append(e.obs, fmt.Sprintf(" ev%d:%s", id, e.evData(face)))
}
func (e *traceEnv) evData(d int32) string {
	if d == int32(e.orig) {
		return "st"
	}
	return strconv.Itoa(int(d))
}
func (e *traceEnv) destroyAndAck(pos api.BlockPos) {
	if !e.destroy(pos) {
		e.resync(pos)
	}
}
func (e *traceEnv) destroy(api.BlockPos) bool {
	b := data.BlockOf(e.st)
	if e.gm == api.Creative && !creativeCanDestroy(e.in.main) || e.restricted(digTarget) {
		return false
	}
	e.obs = append(e.obs, " ev2001:"+e.evData(int32(e.st)))
	st := e.st
	e.st = data.Air
	if e.gm == api.Creative {
		return true
	}
	if t, ok := stackTool(e.in.main); ok && b.Hardness != 0 && t.damagePerBlock > 0 {
		e.dmg += t.damagePerBlock
	}
	if correctForDrops(e.in.main, b) {
		ctx := &loot.Context{Rand: rng.NewXoroshiro(1), State: st, HasState: true, Tool: e.in.main, HasTool: true, HasOrigin: true}
		for _, s := range loot.Roll(loot.BlockTable(b), ctx) {
			e.drops += s.Count
		}
	}
	return true
}

func digTools() []string {
	return []string{"-", "minecraft:wooden_pickaxe", "minecraft:golden_pickaxe", "minecraft:diamond_pickaxe",
		`minecraft:netherite_pickaxe[enchantments={"minecraft:efficiency":5}]`, "minecraft:diamond_axe", "minecraft:diamond_shovel",
		"minecraft:diamond_hoe", "minecraft:shears", "minecraft:diamond_sword", "minecraft:mace", "minecraft:trident"}
}

type digCase struct{ line string }

func digCases(t testing.TB) []string {
	var out []string
	reg := data.Canonical()
	for ti, tool := range digTools() {
		for i := range reg.Blocks {
			b := &reg.Blocks[i]
			out = append(out, fmt.Sprintf("speed|b%d_%d|%s|%s|-|-|-|-|1|0", ti, i, data.StateString(b.DefaultState), tool))
		}
	}
	n := 0
	mod := func(state, tool, helmet, haste, fatigue, conduit, ground, water string) {
		out = append(out, fmt.Sprintf("speed|m%d|%s|%s|%s|%s|%s|%s|%s|%s", n, state, tool, helmet, haste, fatigue, conduit, ground, water))
		n++
	}
	for _, st := range []string{"minecraft:stone", "minecraft:obsidian", "minecraft:dirt", "minecraft:oak_log"} {
		for _, tool := range []string{"-", "minecraft:diamond_pickaxe", `minecraft:diamond_pickaxe[enchantments={"minecraft:efficiency":1}]`, `minecraft:iron_shovel[enchantments={"minecraft:efficiency":4}]`} {
			for _, a := range []string{"-", "0", "1", "2", "3", "5"} {
				mod(st, tool, "-", a, "-", "-", "1", "0")
				mod(st, tool, "-", "-", a, "-", "1", "0")
				mod(st, tool, "-", "-", "-", a, "1", "0")
			}
			mod(st, tool, "-", "1", "-", "2", "1", "0")
			mod(st, tool, "-", "2", "0", "-", "0", "0")
			mod(st, tool, "-", "-", "-", "-", "0", "0")
			mod(st, tool, "-", "-", "-", "-", "1", "1")
			mod(st, tool, `minecraft:diamond_helmet[enchantments={"minecraft:aqua_affinity":1}]`, "-", "-", "-", "1", "1")
			mod(st, tool, `minecraft:diamond_helmet[enchantments={"minecraft:aqua_affinity":1}]`, "-", "-", "-", "0", "1")
		}
	}
	seqs := []string{
		"minecraft:dirt|-|s|S T14 F T5",
		"minecraft:dirt|-|s|S T13 F T5",
		"minecraft:dirt|-|s|S T9 F T10",
		"minecraft:dirt|-|s|S T10 F",
		"minecraft:dirt|-|s|S F T20",
		"minecraft:dirt|-|s|S T9 F T6 S T10 F",
		"minecraft:dirt|-|s|S T5 A T5",
		"minecraft:dirt|-|s|S T5 S T3 F",
		"minecraft:dirt|-|s|T3 S T1 T1 T1 T1 T1 T1 T1 T1",
		"minecraft:dirt|-|c|S",
		"minecraft:dirt|minecraft:diamond_sword|c|S",
		"minecraft:dirt|minecraft:mace|c|S",
		"minecraft:dirt|minecraft:trident|c|S",
		"minecraft:dirt|minecraft:debug_stick|c|S",
		"minecraft:dirt|minecraft:diamond_pickaxe|c|S",
		"minecraft:dirt|-|a|S T20 F",
		"minecraft:dirt|minecraft:diamond_shovel|a|S T20 F",
		"minecraft:short_grass|-|s|S",
		"minecraft:stone|-|s|S T1 T1 T1 T1 T1 T1",
		"minecraft:stone|-|s|S T49 F",
		"minecraft:stone|minecraft:wooden_pickaxe|s|S T10 F",
		"minecraft:stone|minecraft:wooden_pickaxe|s|S T7 F T3",
		"minecraft:stone|minecraft:wooden_pickaxe|s|S T5 F T20",
		`minecraft:stone|minecraft:netherite_pickaxe[enchantments={"minecraft:efficiency":5}]|s|S`,
		"minecraft:obsidian|minecraft:diamond_pickaxe|s|S T300 F",
		"minecraft:oak_log|minecraft:diamond_axe|s|S T3 F",
		"minecraft:bedrock|minecraft:diamond_pickaxe|s|S T40 F",
		"minecraft:glass|-|s|S T9 F",
		"minecraft:sand|minecraft:diamond_shovel|s|S T1 F",
		"minecraft:dirt|-|s|S T3 T3 F T2 F T10",
		"minecraft:dirt|-|s|S T4 A T2 S T10 F",
		"minecraft:dirt|-|s|A F S F",
		"minecraft:stone|minecraft:wooden_pickaxe|s|S T2 T2 T2 T2 F",
		"minecraft:torch|-|s|S",
		"minecraft:dirt|-|s|S T3 C2 T5 F",
		"minecraft:stone|minecraft:wooden_pickaxe|s|S T2 C4 T4 C0 T4 F",
		"minecraft:dirt|-|s|C3 T2 S T2 C5 T12 F",
		"minecraft:dirt|-|s|S T16 C2 T2 F",
		"minecraft:dirt|-|c|C1 S",
	}
	for i, s := range seqs {
		face := ""
		if i%5 == 3 {
			face = "|" + strconv.Itoa(i%6)
		}
		out = append(out, fmt.Sprintf("seq|%d|%s%s", i, s, face))
	}
	return out
}

func f32hex(f float32) string { return strconv.FormatUint(uint64(math.Float32bits(f)), 16) }

func optAmp(s string) int32 {
	if s == "-" {
		return 0
	}
	n, _ := strconv.Atoi(s)
	return int32(n) + 1
}

func goSpeed(t testing.TB, f []string, stacks map[string]item.Stack) string {
	st := data.MustState(f[2])
	in := digInputs{main: stacks[f[3]], ground: f[8] == "1", water: f[9] == "1"}
	in.equip[0], in.equip[5] = in.main, stacks[f[4]]
	in.haste, in.fatigue, in.conduit = optAmp(f[5]), optAmp(f[6]), optAmp(f[7])
	b := data.BlockOf(st)
	return fmt.Sprintf("speed %s %s %v eye=%v", f[1], f32hex(destroyProgress(st, &in)), correctForDrops(in.main, b), in.water)
}

func goSeq(t testing.TB, f []string, stacks map[string]item.Stack) string {
	st := data.MustState(f[2])
	env := &traceEnv{t: t, st: st, orig: st, gm: map[string]api.GameMode{"s": api.Survival, "c": api.Creative, "a": api.Adventure}[f[4]]}
	env.in = digInputs{main: stacks[f[3]], ground: true}
	env.in.equip[0] = env.in.main
	face := int32(1)
	if len(f) > 6 && f[6] != "" {
		n, _ := strconv.Atoi(f[6])
		face = int32(n)
	}
	var d digMachine
	var sb strings.Builder
	sb.WriteString("seq " + f[1] + " g=true")
	for _, op := range strings.Fields(f[5]) {
		n := 1
		if op[0] == 'T' {
			n, _ = strconv.Atoi(op[1:])
		}
		for range n {
			switch op[0] {
			case 'S':
				d.action(env, digTarget, digStart, face)
			case 'F':
				d.action(env, digTarget, digFinish, face)
			case 'A':
				d.action(env, digTarget, digAbort, face)
			case 'C':
				f, _ := strconv.Atoi(op[1:])
				d.action(env, digTarget, digChangeDirection, int32(f))
			case 'T':
				d.tick(env)
			}
		}
		sb.WriteString(" " + op + "[" + env.blk())
		sb.WriteString(strings.Join(env.self, "") + strings.Join(env.obs, ""))
		sb.WriteString("]")
		env.self, env.obs = nil, nil
	}
	fmt.Fprintf(&sb, " drops=%d dmg=%d", env.drops, env.dmg)
	return sb.String()
}

// TestDigMatchesReference compares destroy progress (every block with a
// dozen tools, plus effects, water, air and enchantments), tool
// correctness, and the breaking state machine against the reference.
func TestDigMatchesReference(t *testing.T) {
	cases := digCases(t)
	inPath := filepath.Join(t.TempDir(), "dig_in.txt")
	if os.Getenv("KREMENMC_REF") == "1" {
		os.WriteFile(inPath, []byte(strings.Join(cases, "\n")+"\n"), 0o644)
	}
	golden := reftest.Golden(t, "dig_out.txt", "kremenmc.probe.dig.Dig", inPath, "digest")
	want := map[string]string{}
	digests := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(golden))
	sc.Buffer(nil, 1<<22)
	for sc.Scan() {
		l := sc.Text()
		f := strings.Fields(l)
		switch f[0] {
		case "speed", "seq":
			want[f[0]+"|"+f[1]] = l
		case "digest":
			digests[f[1]] = f[2] + " " + f[3]
		case "error":
			t.Errorf("probe: %s", l)
		}
	}
	stacks := map[string]item.Stack{}
	groups := map[string][]string{}
	var order []string
	nSpeed, nSeq := 0, 0
	for _, c := range cases {
		f := strings.Split(c, "|")
		for _, s := range []string{f[3], f[4]} {
			if _, ok := stacks[s]; !ok && (f[0] == "speed" || s == f[3]) {
				stacks[s] = digStack(t, s)
			}
		}
		var got string
		if f[0] == "speed" {
			got = goSpeed(t, f, stacks)
			nSpeed++
			if strings.HasPrefix(f[1], "b") {
				if _, ok := groups[f[3]]; !ok {
					order = append(order, f[3])
				}
				groups[f[3]] = append(groups[f[3]], got)
				continue
			}
		} else {
			got = goSeq(t, f, stacks)
			nSeq++
		}
		if w := want[f[0]+"|"+f[1]]; w != got {
			t.Errorf("%s\n got %s\nwant %s", c, got, w)
		}
	}
	for _, k := range order {
		ls := groups[k]
		d := fmt.Sprintf("%08x %d", crc32.ChecksumIEEE([]byte(strings.Join(ls, "\n"))), len(ls))
		if digests[k] != d {
			t.Errorf("speed digest for %s: %s, reference %s", k, d, digests[k])
		}
	}
	t.Logf("%d destroy-speed cases, %d breaking sequences", nSpeed, nSeq)
}
