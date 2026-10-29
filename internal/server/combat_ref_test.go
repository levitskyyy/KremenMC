package server

import (
	"bufio"
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/reftest"
)

// TestCombatMatchesReference replays testdata/combat.script (damage
// pipeline and melee scenarios) and compares health, absorption, victim
// knockback and armor durability with the reference (combat.golden).
func TestCombatMatchesReference(t *testing.T) {
	scriptPath, _ := filepath.Abs(filepath.Join("testdata", "combat.script"))
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	golden := reftest.Golden(t, "combat.golden", "kremenmc.probe.combat.Hits", scriptPath)
	want := map[string][]map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(golden))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 || f[0] != "res" {
			continue
		}
		kv := map[string]string{"#": f[2]}
		for _, x := range f[3:] {
			k, v, _ := strings.Cut(x, "=")
			kv[k] = v
		}
		want[f[1]] = append(want[f[1]], kv)
	}

	s := startServer(t, nil)
	ca := dial(t, s.Addr(), "Attacker")
	ca.joinPlay(s.Addr())
	cv := dial(t, s.Addr(), "Victim")
	cv.joinPlay(s.Addr())
	for _, c := range []*fakeClient{ca, cv} {
		go func() {
			for range c.in {
			}
		}()
	}
	w := s.defaultWorld()
	att := mustPlayer(t, s, "Attacker").(*Player)
	vic := mustPlayer(t, s, "Victim").(*Player)
	waitFor(t, func() bool { return att.liv.attrs != nil && vic.liv.attrs != nil })

	var extras []combatant
	extraCombatants = func(*World) []combatant { return extras }
	defer func() { extraCombatants = nil }()

	for line := range strings.Lines(string(script)) {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != "run" {
			continue
		}
		name := f[1]
		t.Run(name, func(t *testing.T) {
			exp := want[name]
			if len(exp) == 0 {
				t.Skip("no reference result")
			}
			kv := map[string]string{}
			var actions []string
			for _, x := range f[2:] {
				k, v, _ := strings.Cut(x, "=")
				switch k {
				case "hurt", "attack", "fall", "inv":
					actions = append(actions, x)
				default:
					kv[k] = v
				}
			}
			w.SetDifficulty(map[string]api.Difficulty{"peaceful": api.DifficultyPeaceful, "easy": api.DifficultyEasy, "normal": api.DifficultyNormal, "hard": api.DifficultyHard}[or(kv["diff"], "normal")])
			ap := vec(or(kv["apos"], "0.5,101,0.5"))
			resetPlayer(att, ap, float32(num(or(kv["ayaw"], "0"))))
			setHand(att, kv["weapon"])
			att.liv.mu.Lock()
			att.liv.ticker = int32(num(or(kv["cooldown"], "100")))
			att.liv.refreshEquipment(att.gear())
			effects(&att.liv, kv["aeffect"])
			att.liv.mu.Unlock()
			if _, ok := kv["crit"]; ok {
				att.mu.Lock()
				att.e.FallDistance, att.e.OnGround = 1, false
				att.mu.Unlock()
			}
			if _, ok := kv["sprint"]; ok {
				att.mu.Lock()
				att.sprinting = true
				att.mu.Unlock()
			}
			vp := vec(or(kv["vpos"], "0.5,101,2.5"))
			vyaw := float32(num(or(kv["vyaw"], "180")))
			var v combatant
			resetPlayer(vic, api.Vec3{X: 1000, Y: 101, Z: 1000}, 0)
			if kv["victim"] == "player" {
				resetPlayer(vic, vp, vyaw)
				v = vic
			} else {
				v = detachedMob(t, w, or(kv["victim"], "minecraft:zombie"), vp, vyaw)
			}
			extras = nil
			if e := kv["extra"]; e != "" {
				for _, p := range strings.Split(e, ";") {
					extras = append(extras, detachedMob(t, w, "minecraft:zombie", vec(p), 180))
				}
			}
			if v == combatant(vic) {
				extras = append(extras, att)
			} else {
				extras = append(extras, v)
			}
			l := v.lv()
			if h := kv["vhealth"]; h != "" {
				l.mu.Lock()
				v.setCurHealth(float32(num(h)))
				l.mu.Unlock()
			}
			if a := kv["armor"]; a != "" {
				for _, p := range strings.Split(a, ",") {
					slot, it, _ := strings.Cut(p, "/")
					v.setEquipment(map[string]int{"head": slotHead, "chest": slotChest, "legs": slotLegs, "feet": slotFeet, "mainhand": slotMain}[slot], stackSpec(it))
				}
			}
			l.mu.Lock()
			l.refreshEquipment(v.gear())
			effects(l, kv["veffect"])
			if a := kv["vabs"]; a != "" {
				l.absorb = min(float32(num(a)), float32(l.attrs.Value(entity.AttrMaxAbsorption)))
			}
			l.mu.Unlock()
			if _, ok := kv["block"]; ok {
				sh := stackSpec("minecraft:shield")
				v.setEquipment(slotOff, sh)
				l.mu.Lock()
				l.block = shieldState{stack: sh, slot: slotOff, used: 10}
				l.mu.Unlock()
			}
			check(t, "start", v, exp[0])
			attacked := false
			for i, act := range actions {
				k, val, _ := strings.Cut(act, "=")
				ret := ""
				switch k {
				case "hurt":
					q := strings.Split(val, "/")
					src := NewDamage(q[0])
					if len(q) > 2 {
						switch q[2] {
						case "player":
							src = EntityDamage(q[0], att)
						case "zombie":
							src = EntityDamage(q[0], detachedMob(t, w, "minecraft:zombie", ap, 0))
						}
					}
					ret = strconv.FormatBool(hurt(v, src, float32(num(q[1]))))
				case "attack":
					done := make(chan struct{})
					first := !attacked
					attacked = true
					att.later(func() {
						if first {
							att.liv.mu.Lock()
							att.liv.ticker = int32(num(or(kv["cooldown"], "100")))
							att.liv.mu.Unlock()
						}
						att.attackEntity(v)
						close(done)
					})
					<-done
				case "fall":
					d := fallDamage(v, num(val), 1)
					ok := d > 0 && hurt(v, NewDamage("minecraft:fall"), d)
					ret = strconv.FormatBool(ok)
				case "inv":
					l.mu.Lock()
					l.cooldown = int32(num(val))
					l.mu.Unlock()
				}
				if i+1 >= len(exp) {
					t.Fatalf("reference has no result for %s", act)
				}
				e := exp[i+1]
				if r, ok := e["ret"]; ok && ret != "" && r != ret {
					t.Errorf("%s: hurt returned %s, reference %s", act, ret, r)
				}
				check(t, act, v, e)
			}
			for i, x := range extras[:len(extras)-1] {
				if e := find(exp, "extra"+strconv.Itoa(i)); e != nil {
					check(t, "extra"+strconv.Itoa(i), x, e)
				}
			}
		})
	}
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func num(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func vec(s string) api.Vec3 {
	f := strings.Split(s, ",")
	return api.Vec3{X: num(f[0]), Y: num(f[1]), Z: num(f[2])}
}

func find(exp []map[string]string, action string) map[string]string {
	for _, e := range exp {
		if e["#"] == action {
			return e
		}
	}
	return nil
}

func stackSpec(spec string) item.Stack {
	parts := strings.Split(spec, "+")
	id, _ := item.IDOf(parts[0])
	st := item.Stack{Item: id, Count: 1}
	var es []item.Enchantment
	for _, p := range parts[1:] {
		n, lvl, _ := strings.Cut(p, "@")
		eid, _ := regsync.Index("minecraft:enchantment", n)
		l, _ := strconv.Atoi(lvl)
		es = append(es, item.Enchantment{ID: eid, Level: int32(l)})
	}
	if len(es) > 0 {
		st = st.Set(item.Enchantments, item.EncodeEnchantments(es))
	}
	return st
}

func setHand(p *Player, spec string) {
	st := item.Stack{}
	if spec != "" {
		st = stackSpec(spec)
	}
	p.setHand(api.MainHand, st)
}

func effects(l *living, spec string) {
	if spec == "" {
		return
	}
	for _, x := range strings.Split(spec, ",") {
		n, amp, _ := strings.Cut(x, "@")
		id, _ := item.BuiltinID("mob_effect", n)
		a, _ := strconv.Atoi(amp)
		l.setEffect(id, int32(a), true)
	}
}

func resetPlayer(p *Player, pos api.Vec3, yaw float32) {
	p.SetGameMode(api.Survival)
	p.inv.mu.Lock()
	for i := range p.inv.m.Items {
		p.inv.m.Items[i] = item.Stack{}
	}
	p.inv.mu.Unlock()
	p.mu.Lock()
	p.e.Pos, p.e.Prev, p.e.Yaw, p.e.HeadYaw, p.e.Pitch = pos, pos, yaw, yaw, 0
	p.e.OnGround, p.e.FallDistance, p.e.Vel = true, 0, api.Vec3{}
	p.sprinting = false
	p.health = 20
	p.mu.Unlock()
	l := &p.liv
	l.mu.Lock()
	for id := range l.fx {
		l.setEffect(id, 0, false)
	}
	l.refreshEquipment(p.gear())
	l.dead, l.absorb, l.cooldown, l.hurtTime, l.lastHurt, l.block, l.shieldOff = false, 0, 0, 0, 0, shieldState{}, 0
	l.mu.Unlock()
}

func detachedMob(t *testing.T, w *World, kind string, pos api.Vec3, yaw float32) *Mob {
	et, ok := data.Canonical().EntityByName(strings.TrimPrefix(kind, "minecraft:"))
	if !ok {
		t.Fatalf("unknown entity %s", kind)
	}
	m := newMob(w, et, api.Location{World: w.name, Pos: pos, Yaw: yaw})
	m.e.OnGround = true
	return m
}

func check(t *testing.T, act string, v combatant, e map[string]string) {
	t.Helper()
	l := v.lv()
	l.mu.Lock()
	hp, abs := v.curHealth(), l.absorb
	l.mu.Unlock()
	if w := float32(num(e["hp"])); math.Abs(float64(hp-w)) > 1e-4 {
		t.Errorf("%s: health %v, reference %v", act, hp, w)
	}
	if w := float32(num(e["abs"])); math.Abs(float64(abs-w)) > 1e-4 {
		t.Errorf("%s: absorption %v, reference %v", act, abs, w)
	}
	if !v.isPlayer() {
		got, want := v.vel(), vec(e["vel"])
		if math.Abs(got.X-want.X) > 1e-6 || math.Abs(got.Y-want.Y) > 1e-6 || math.Abs(got.Z-want.Z) > 1e-6 {
			t.Errorf("%s: velocity %v, reference %v", act, got, want)
		}
	}
	if d, ok := e["dur"]; ok {
		eq := v.gear()
		for _, p := range strings.Split(d, ",") {
			slot, n, _ := strings.Cut(p, ":")
			i := map[string]int{"head": slotHead, "chest": slotChest, "legs": slotLegs, "feet": slotFeet, "offhand": slotOff}[slot]
			if got := eq[i].Damage(); strconv.Itoa(int(got)) != n {
				t.Errorf("%s: %s durability damage %d, reference %s", act, slot, got, n)
			}
		}
	}
}

// TestProjectilesMatchReference fires arrows, tridents and snowballs at a
// detached zombie and compares damage and knockback with proj.golden.
func TestProjectilesMatchReference(t *testing.T) {
	scriptPath, _ := filepath.Abs(filepath.Join("testdata", "proj.script"))
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	golden := reftest.Golden(t, "proj.golden", "kremenmc.probe.combat.Proj", scriptPath)
	want := map[string][]map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(golden))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 || f[0] != "shot" {
			continue
		}
		kv := map[string]string{}
		for _, x := range f[3:] {
			k, v, _ := strings.Cut(x, "=")
			kv[k] = v
		}
		want[f[1]] = append(want[f[1]], kv)
	}
	s := startServer(t, nil)
	w := s.defaultWorld()
	var extras []combatant
	extraCombatants = func(*World) []combatant { return extras }
	defer func() { extraCombatants = nil }()
	for line := range strings.Lines(string(script)) {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "shot" {
			continue
		}
		name, kind := f[1], f[2]
		kv := map[string]string{}
		for _, x := range f[3:] {
			k, v, _ := strings.Cut(x, "=")
			kv[k] = v
		}
		target := kv["target"]
		if target == "" || strings.HasPrefix(target, "player") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			exp := want[name]
			tt, tp, _ := strings.Cut(target, "@")
			seen := map[float32]bool{}
			for i := range exp {
				z := detachedMob(t, w, tt, vec(tp), 180)
				extras = []combatant{z}
				et, _ := data.Canonical().EntityByName(kind)
				m := newMob(w, et, api.Location{World: w.name, Pos: vec(kv["pos"])})
				m.e.Vel = vec(kv["vel"])
				pr := &projectile{baseDmg: 2, pickup: pickupAllowed, weapon: stackSpec(or(kv["weapon"], "minecraft:bow"))}
				if _, ok := kv["crit"]; ok {
					pr.crit = true
				}
				if kind == "trident" {
					pr.stack = stackSpec("minecraft:trident")
				}
				pr.left = true
				m.proj = pr
				for k := 0; k < 40 && m.IsValid() && z.Health() == 20; k++ {
					m.tickProjectile()
				}
				dmg := 20 - z.Health()
				e := exp[min(i, len(exp)-1)]
				if pr.crit {
					seen[float32(math.Round(dmg*1000)/1000)] = true
					continue
				}
				if w := num(e["dmg"]); math.Abs(dmg-w) > 1e-4 {
					t.Errorf("damage %v, reference %v", dmg, w)
				}
				if tv, ok := e["tvel"]; ok {
					got, want := z.vel(), vec(tv)
					if math.Abs(got.X-want.X) > 1e-6 || math.Abs(got.Y-want.Y) > 1e-6 || math.Abs(got.Z-want.Z) > 1e-6 {
						t.Errorf("target velocity %v, reference %v", got, want)
					}
				}
			}
			if len(seen) > 0 {
				ref := map[float32]bool{}
				for _, e := range exp {
					ref[float32(math.Round(num(e["dmg"])*1000)/1000)] = true
				}
				for d := range seen {
					if !ref[d] {
						t.Errorf("crit damage %v never seen in the reference %v", d, ref)
					}
				}
			}
		})
	}
}
