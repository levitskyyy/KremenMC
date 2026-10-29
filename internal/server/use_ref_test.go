package server

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

type useCase struct {
	id                     int
	creative               bool
	main, off, head, chest string // "<count> <item>" or "-"
	food                   int
	sat, health            float32
	hand                   int
}

func (c useCase) line() string {
	gm := "s"
	if c.creative {
		gm = "c"
	}
	return fmt.Sprintf("%d|%s|%s|%s|%s|%s|%d|%s|%s|%d|60", c.id, gm, c.main, c.off, c.head, c.chest, c.food,
		strconv.FormatFloat(float64(c.sat), 'f', 1, 32), strconv.FormatFloat(float64(c.health), 'f', 1, 32), c.hand)
}

func useCases() []useCase {
	var cs []useCase
	add := func(c useCase) {
		c.id = len(cs)
		cs = append(cs, c)
	}
	var consumables, armor []string
	for _, it := range data.Canonical().Items {
		id, _ := item.IDOf(it.Name)
		d := item.Defaults(id)
		if d.Has(item.Consumable) {
			consumables = append(consumables, "minecraft:"+it.Name)
		}
		if w, ok := d.Wire(item.Equippable); ok {
			if e, err := item.DecodeEquippable(w); err == nil && e.Swappable && e.Slot >= item.SlotFeet && e.Slot <= item.SlotHead {
				armor = append(armor, "minecraft:"+it.Name)
			}
		}
	}
	for _, n := range consumables {
		add(useCase{main: "1 " + n, off: "-", head: "-", chest: "-", food: 10, health: 10})
		add(useCase{main: "2 " + n, off: "-", head: "-", chest: "-", food: 20, sat: 5, health: 20})
		add(useCase{creative: true, main: "3 " + n, off: "-", head: "-", chest: "-", food: 10, health: 10})
	}
	for _, s := range []string{
		`1 minecraft:potion[potion_contents={potion:"minecraft:healing"}]`,
		`1 minecraft:potion[potion_contents={potion:"minecraft:strong_regeneration"}]`,
		`1 minecraft:potion[potion_contents={potion:"minecraft:long_swiftness",custom_effects:[{id:"minecraft:haste",amplifier:1,duration:300}]}]`,
		`1 minecraft:suspicious_stew[suspicious_stew_effects=[{id:"minecraft:night_vision",duration:100}]]`,
		`4 minecraft:cookie[use_cooldown={seconds:2.5,cooldown_group:"kremenmc:snack"}]`,
		`2 minecraft:stone[consumable={consume_seconds:0}]`,
	} {
		add(useCase{main: s, off: "-", head: "-", chest: "-", food: 10, health: 10})
	}
	add(useCase{main: "-", off: "1 minecraft:apple", head: "-", chest: "-", food: 10, health: 10, hand: 1})
	for i, n := range armor {
		worn := "-"
		if i%2 == 0 {
			worn = "1 minecraft:leather_helmet"
		}
		e, _ := item.Defaults(mustID(n)).Wire(item.Equippable)
		info, _ := item.DecodeEquippable(e)
		c := useCase{main: "1 " + n, off: "-", head: "-", chest: "-", food: 20, health: 20}
		switch info.Slot {
		case item.SlotHead:
			c.head = worn
		case item.SlotChest:
			c.chest = strings.Replace(worn, "helmet", "chestplate", 1)
		}
		add(c)
		c.main = "2 " + n
		add(c)
		c.creative = true
		add(c)
	}
	add(useCase{main: "1 minecraft:diamond_helmet", off: "-", head: `1 minecraft:iron_helmet[enchantments={"minecraft:binding_curse":1}]`, chest: "-", food: 20, health: 20})
	add(useCase{creative: true, main: "1 minecraft:diamond_helmet", off: "-", head: `1 minecraft:iron_helmet[enchantments={"minecraft:binding_curse":1}]`, chest: "-", food: 20, health: 20})
	add(useCase{main: "1 minecraft:diamond_helmet", off: "-", head: "1 minecraft:diamond_helmet", chest: "-", food: 20, health: 20})
	add(useCase{main: "1 minecraft:shield", off: "-", head: "-", chest: "-", food: 20, health: 20})
	add(useCase{main: "1 minecraft:stone", off: "-", head: "-", chest: "-", food: 20, health: 20})
	return cs
}

func mustID(n string) int32 {
	id, _ := item.IDOf(n)
	return id
}

type useWant map[string]string

func parseUse(line string) (int, useWant) {
	f := strings.Fields(line)
	id, _ := strconv.Atoi(f[1])
	w := useWant{}
	for _, kv := range f[2:] {
		k, v, _ := strings.Cut(kv, "=")
		w[k] = v
	}
	return id, w
}

// TestReferenceItemUse replays item-use cases against the reference
// handler's results (kremenmc.probe.item.Use): hands, armor, food, health,
// effects, extra inventory items, cooldowns and full resyncs.
func TestReferenceItemUse(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	cases := useCases()
	in, _ := filepath.Abs(filepath.Join("testdata", "use_in.txt"))
	if os.Getenv("KREMENMC_REF") == "1" {
		var b bytes.Buffer
		for _, c := range cases {
			b.WriteString(c.line() + "\n")
		}
		os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(in, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden := reftest.Golden(t, "use_out.txt", "kremenmc.probe.item.Use", in)
	want := map[int]useWant{}
	for _, l := range strings.Split(string(golden), "\n") {
		if strings.HasPrefix(l, "use ") {
			id, w := parseUse(l)
			var cds []string
			content := false
			for _, x := range strings.Split(w["packets"], ",") {
				switch {
				case x == "content":
					content = true
				case strings.HasPrefix(x, "cooldown:"):
					cds = append(cds, x)
				}
			}
			w["content"], w["cooldown"] = strconv.FormatBool(content), strings.Join(cds, ",")
			want[id] = w
		} else if strings.HasPrefix(l, "error") {
			t.Errorf("probe: %s", l)
		}
	}
	stacks := parseStackSpecs(t, golden, cases)
	s := startServer(t, survival)
	var mu sync.Mutex
	got := map[int]useWant{}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for _, c := range cases {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			r := runUseCase(t, s, c, stacks)
			mu.Lock()
			got[c.id] = r
			mu.Unlock()
		}()
	}
	wg.Wait()
	bad := 0
	for _, c := range cases {
		w, g := want[c.id], got[c.id]
		var diffs []string
		for _, k := range []string{"main", "off", "head", "chest", "food", "inv", "cooldown", "content", "effects", "health"} {
			// The reference player is not ticked while we are: effects
			// that act over time and random consume effects are only
			// comparable by presence.
			ticking := strings.Contains(w["effects"], "regeneration") || strings.Contains(w["effects"], "poison") || strings.Contains(w["effects"], "wither")
			if k == "health" && ticking || k == "effects" && randomEffects(c.main) {
				continue
			}
			if !useFieldEqual(k, g[k], w[k]) {
				diffs = append(diffs, fmt.Sprintf("%s: got %s want %s", k, g[k], w[k]))
			}
		}
		if len(diffs) > 0 {
			if bad++; bad <= 15 {
				t.Errorf("case %d (%s): %s", c.id, c.line(), strings.Join(diffs, "; "))
			}
		}
	}
	t.Logf("%d use cases, %d mismatched", len(cases), bad)
}

func randomEffects(spec string) bool {
	for _, n := range []string{"chicken", "rotten_flesh", "poisonous_potato"} {
		if strings.HasSuffix(spec, "minecraft:"+n) {
			return true
		}
	}
	return false
}

func useFieldEqual(k, got, want string) bool {
	switch k {
	case "effects":
		return effectNames(got) == effectNames(want)
	}
	return got == want
}

func effectNames(s string) string {
	if s == "-" || s == "" {
		return "-"
	}
	var out []string
	for _, e := range strings.Split(s, ",") {
		p := strings.Split(e, ":")
		out = append(out, strings.Join(p[:len(p)-1], ":"))
	}
	return strings.Join(out, ",")
}

// parseStackSpecs resolves the stack specs of the cases through the
// reference results: the probe prints every stack it built in hex.
func parseStackSpecs(t *testing.T, golden []byte, cases []useCase) map[string]item.Stack {
	m := map[string]item.Stack{}
	for _, l := range strings.Split(strings.TrimSpace(string(golden)), "\n") {
		rest, ok := strings.CutPrefix(l, "stackspec ")
		if !ok {
			continue
		}
		h, spec, _ := strings.Cut(rest, "\t")
		raw, _ := hex.DecodeString(h)
		st, err := item.DecodeStack(raw)
		if err != nil {
			t.Fatal(err)
		}
		m[spec] = st
	}
	for _, c := range cases {
		for _, s := range []string{c.main, c.off, c.head, c.chest} {
			if _, ok := m[s]; !ok && s != "-" {
				t.Fatalf("stack spec %q missing from the golden", s)
			}
		}
	}
	return m
}

func runUseCase(t *testing.T, s *Server, c useCase, stacks map[string]item.Stack) useWant {
	name := fmt.Sprintf("u%d", c.id)
	cl := dial(t, s.Addr(), name)
	cl.joinPlay(s.Addr())
	defer cl.nc.Close()
	pp, _ := s.PlayerByName(name)
	p := pp.(*Player)
	ready := make(chan struct{})
	gm := api.Survival
	if c.creative {
		gm = api.Creative
	}
	p.SetGameMode(gm)
	p.Scheduler().Run("test", func(api.Task) {
		in := p.inv
		in.mu.Lock()
		in.set(0, stacks[c.main])
		in.set(invOffhand, stacks[c.off])
		in.set(39, stacks[c.head])
		in.set(38, stacks[c.chest])
		in.mu.Unlock()
		p.mu.Lock()
		p.food, p.saturation, p.health = c.food, c.sat, float64(c.health)
		p.mu.Unlock()
		close(ready)
	})
	<-ready
	content := 0
	var cooldowns []string
	var cmu sync.Mutex
	done := make(chan useWant, 1)
	pong, used := make(chan struct{}), make(chan struct{})
	start := s.CurrentTick() + 2
	go func() {
		for {
			pk, ok := <-cl.in
			if !ok {
				return
			}
			cmu.Lock()
			switch m := pk.(type) {
			case *packets.ClientboundContainerSetContent:
				if s.CurrentTick() >= start {
					content++
				}
			case *packets.ClientboundPongResponse:
				switch m.Payload {
				case 1:
					close(used)
				case 777:
					close(pong)
				}
			case *packets.ClientboundCooldown:
				if m.CooldownTicks > 0 {
					cooldowns = append(cooldowns, fmt.Sprintf("cooldown:%s:%d", m.CooldownGroup, m.CooldownTicks))
				}
			}
			cmu.Unlock()
		}
	}()
	time.Sleep(150 * time.Millisecond)
	cl.send(&packets.ServerboundUseItem{Hand: int32(c.hand), Sequence: 1})
	cl.send(&packets.ServerboundPingRequest{Payload: 1})
	select {
	case <-used:
	case <-time.After(15 * time.Second):
		t.Errorf("case %d: use not processed", c.id)
	}
	p.Scheduler().RunRepeating("test", 1, 1, func(task api.Task) {
		if p.use.active && p.use.remaining < 1000 {
			return
		}
		task.Cancel()
		in := p.inv
		in.mu.Lock()
		w := useWant{
			"main": hexStack(in.get(0)), "off": hexStack(in.get(invOffhand)),
			"head": hexStack(in.get(39)), "chest": hexStack(in.get(38)),
		}
		var inv []string
		for i := 1; i < 36; i++ {
			if st := in.get(i); !st.IsEmpty() {
				inv = append(inv, fmt.Sprintf("%d:%s", i, hexStack(st)))
			}
		}
		in.mu.Unlock()
		w["inv"] = strings.Join(inv, ",")
		if w["inv"] == "" {
			w["inv"] = "-"
		}
		p.mu.Lock()
		w["food"] = fmt.Sprintf("%d/%s", p.food, javaFloat(p.saturation))
		w["health"] = javaFloat(float32(p.health))
		p.mu.Unlock()
		var es []string
		for id, e := range p.use.effects {
			n := effectKey(id)
			es = append(es, fmt.Sprintf("%s:%d:%d", n, e.amplifier, e.duration))
		}
		slices.Sort(es)
		w["effects"] = strings.Join(es, ",")
		if w["effects"] == "" {
			w["effects"] = "-"
		}
		done <- w
	})
	var w useWant
	select {
	case w = <-done:
	case <-time.After(15 * time.Second):
		t.Errorf("case %d: use did not finish", c.id)
		return useWant{}
	}
	// Packets are ordered: once the pong arrives everything sent before it has.
	cl.send(&packets.ServerboundPingRequest{Payload: 777})
	select {
	case <-pong:
	case <-time.After(15 * time.Second):
		t.Errorf("case %d: no pong", c.id)
	}
	cmu.Lock()
	w["content"] = strconv.FormatBool(content > 0)
	w["cooldown"] = strings.Join(cooldowns, ",")
	cmu.Unlock()
	return w
}

func effectKey(id int32) string {
	if p, err := wgdata.Load(); err == nil {
		if es, _ := p.Builtin("mob_effect"); int(id) < len(es) {
			return es[id]
		}
	}
	return strconv.Itoa(int(id))
}

func hexStack(s item.Stack) string { return hex.EncodeToString(item.AppendStack(nil, s)) }

// javaFloat formats like Java's Float.toString for the values we compare.
func javaFloat(f float32) string {
	s := strconv.FormatFloat(float64(f), 'f', -1, 32)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}
