package server

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/bridge"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

func (h BridgeHost) entity(f bridge.Fields) api.Entity {
	u, ok := f.UUID("uuid")
	if !ok {
		return nil
	}
	if p, ok := h.s.Player(api.UUID(u)); ok {
		return p
	}
	for _, w := range h.s.worldList() {
		if e, ok := w.Entity(api.UUID(u)); ok {
			return e
		}
	}
	return nil
}

func entityMap(e api.Entity) map[string]any {
	if e == nil {
		return nil
	}
	l := e.Location()
	m := map[string]any{
		"uuid": bridge.UUID(e.UUID()), "type": string(e.Type()), "world": l.World,
		"x": l.Pos.X, "y": l.Pos.Y, "z": l.Pos.Z, "yaw": float64(l.Yaw), "pitch": float64(l.Pitch),
		"valid": e.IsValid(),
	}
	if x, ok := e.(interface{ EntityID() int32 }); ok {
		m["entityId"] = int(x.EntityID())
	}
	if c, ok := e.(combatant); ok && c.lv().attrs != nil {
		m["living"] = true
		m["health"] = float64(c.curHealth())
		c.lv().mu.Lock()
		m["maxHealth"] = c.lv().attrs.Value(entity.AttrMaxHealth)
		c.lv().mu.Unlock()
	}
	return m
}

func (h BridgeHost) worldOf(f bridge.Fields) (*World, error) {
	w := h.s.world(f.String("world"))
	if w == nil {
		return nil, fmt.Errorf("unknown world %q", f.String("world"))
	}
	return w, nil
}

// worldQuery answers the queries of protocol_world.go; ok is false for other kinds.
func (h BridgeHost) worldQuery(kind string, f bridge.Fields) (any, bool, error) {
	switch kind {
	case bridge.QueryEntity:
		return entityMap(h.entity(f)), true, nil
	case bridge.QueryEntities:
		w, err := h.worldOf(f)
		if err != nil {
			return nil, true, err
		}
		var es []api.Entity
		if f.Has("radius") {
			es = w.NearbyEntities(api.Vec3{X: f.Float("x"), Y: f.Float("y"), Z: f.Float("z")}, f.Float("radius"))
		} else {
			es = w.Entities()
		}
		out := make([]any, 0, len(es))
		for _, e := range es {
			out = append(out, entityMap(e))
		}
		return out, true, nil
	case bridge.QueryAttribute:
		c, ok := h.entity(f).(combatant)
		if !ok {
			return map[string]any{"has": false}, true, nil
		}
		return attributeMap(c.lv(), f.String("name")), true, nil
	case bridge.QueryEffects:
		return h.effects(h.entity(f)), true, nil
	case bridge.QueryList:
		return h.listEntries(f.String("list")), true, nil
	case bridge.QueryListState:
		return map[string]any{"whitelist": h.s.wl.Enabled()}, true, nil
	case bridge.QueryHighest:
		w, err := h.worldOf(f)
		if err != nil {
			return nil, true, err
		}
		return w.HighestBlockY(int(f.Int("x")), int(f.Int("z"))), true, nil
	case bridge.QueryChunkLoaded:
		w, err := h.worldOf(f)
		if err != nil {
			return nil, true, err
		}
		return w.IsChunkLoaded(int(f.Int("x")), int(f.Int("z"))), true, nil
	case bridge.QueryLoadedChunks:
		w, err := h.worldOf(f)
		if err != nil {
			return nil, true, err
		}
		cs := w.LoadedChunks()
		out := make([]any, len(cs))
		for i, c := range cs {
			out[i] = map[string]any{"x": c.X, "z": c.Z}
		}
		return out, true, nil
	case bridge.QuerySpawnEntity:
		w, err := h.worldOf(f)
		if err != nil {
			return nil, true, err
		}
		at := api.Location{World: w.name, Pos: api.Vec3{X: f.Float("x"), Y: f.Float("y"), Z: f.Float("z")},
			Yaw: float32(f.Float("yaw")), Pitch: float32(f.Float("pitch"))}
		t, ok := data.Canonical().EntityByName(f.String("type"))
		if !ok || t.Name == "player" {
			return nil, true, fmt.Errorf("cannot spawn %q", f.String("type"))
		}
		m := newMob(w, t, at)
		w.mobsMu.Lock()
		w.addMobLocked(m)
		w.mobsMu.Unlock()
		return entityMap(m), true, nil
	case bridge.QueryDropItem:
		w, err := h.worldOf(f)
		if err != nil {
			return nil, true, err
		}
		st := stackFromAPI(api.ItemStack{Type: api.Key(f.String("item")), Count: int32(max(1, f.Int("count")))})
		if st.IsEmpty() {
			return nil, true, errors.New("empty item stack")
		}
		pos := api.Vec3{X: f.Float("x"), Y: f.Float("y"), Z: f.Float("z")}
		var e api.Entity
		if f.Bool("naturally") {
			e = w.SpawnItem(pos, st, itemPickupDefault)
		} else {
			e = w.spawnItem(pos, api.Vec3{}, st, int64(itemPickupDefault)).m
		}
		return entityMap(e), true, nil
	}
	return nil, false, nil
}

func attributeMap(l *living, name string) map[string]any {
	a, ok := entity.AttrByName(name)
	l.mu.Lock()
	defer l.mu.Unlock()
	if !ok || l.attrs == nil || !l.attrs.Has(a) {
		return map[string]any{"has": false}
	}
	var mods []any
	for _, m := range l.attrs.Modifiers(a) {
		mods = append(mods, map[string]any{"id": m.ID, "amount": m.Amount, "op": int(m.Op)})
	}
	return map[string]any{
		"has": true, "base": l.attrs.Base(a), "value": l.attrs.Value(a),
		"default": entity.AttrInfoOf(a).Default, "modifiers": mods,
	}
}

// noteEffectEnd records when a player's effect ends so other goroutines
// can read effect durations; players keep the end tick (-1 infinite) in
// living.fxLeft, which only mobs count down.
func (p *Player) noteEffectEnd(id, duration int32) {
	end := int32(-1)
	if duration >= 0 {
		end = int32(p.s.CurrentTick()) + duration
	}
	p.liv.mu.Lock()
	if _, ok := p.liv.fx[id]; ok {
		p.liv.fxLeft[id] = end
	}
	p.liv.mu.Unlock()
}

func (h BridgeHost) effects(e api.Entity) []any {
	c, ok := e.(combatant)
	if !ok {
		return nil
	}
	l := c.lv()
	now := int32(h.s.CurrentTick())
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]any, 0, len(l.fx))
	for id, amp := range l.fx {
		d := l.fxLeft[id]
		if c.isPlayer() && d >= 0 {
			d = max(0, d-now)
		}
		out = append(out, map[string]any{"type": effectName(id), "amplifier": int(amp), "duration": int(d),
			"ambient": false, "particles": true, "icon": true})
	}
	return out
}

func (h BridgeHost) listEntries(which string) []any {
	var out []any
	switch which {
	case "ops":
		h.s.ops.mu.RLock()
		for u, e := range h.s.ops.ops {
			out = append(out, map[string]any{"target": bridge.UUID(u), "name": e.Name})
		}
		h.s.ops.mu.RUnlock()
	case "whitelist":
		for _, p := range h.s.wl.Entries() {
			out = append(out, map[string]any{"target": bridge.UUID(p.UUID), "name": p.Name})
		}
	case "profile", "ip":
		l := h.s.bans
		if which == "ip" {
			l = h.s.ipBans
		}
		for _, e := range l.Entries() {
			if !e.Expires.IsZero() && time.Now().After(e.Expires) {
				continue
			}
			m := map[string]any{"target": e.Target, "name": e.Name, "reason": e.Reason, "source": e.Source,
				"created": e.Created.UnixMilli(), "expires": int64(0)}
			if !e.Expires.IsZero() {
				m["expires"] = e.Expires.UnixMilli()
			}
			out = append(out, m)
		}
	}
	return out
}

// worldAction applies the actions of protocol_world.go; it reports whether kind was one of them.
func (h BridgeHost) worldAction(kind string, f bridge.Fields) bool {
	switch kind {
	case bridge.ActionAttributeBase, bridge.ActionAttributeMod:
		c, ok := h.entity(f).(combatant)
		a, ok2 := entity.AttrByName(f.String("name"))
		if !ok || !ok2 {
			return true
		}
		l := c.lv()
		l.mu.Lock()
		if l.attrs != nil && l.attrs.Has(a) {
			switch {
			case kind == bridge.ActionAttributeBase:
				l.attrs.SetBase(a, f.Float("value"))
			case f.Bool("remove"):
				l.attrs.RemoveModifier(a, f.String("id"))
			default:
				l.attrs.SetModifier(a, entity.Modifier{ID: f.String("id"), Amount: f.Float("amount"), Op: entity.Op(f.Int("op"))})
			}
		}
		l.mu.Unlock()
	case bridge.ActionEffectAdd:
		h.addEffect(h.entity(f), f)
	case bridge.ActionEffectRemove:
		h.removeEffect(h.entity(f), f.String("type"))
	case bridge.ActionOp:
		if u, ok := f.UUID("uuid"); ok {
			h.s.setOp(api.UUID(u), f.String("name"), f.Bool("value"))
		}
	case bridge.ActionWhitelist:
		if u, ok := f.UUID("uuid"); ok {
			if f.Bool("value") {
				h.s.wl.Add(api.GameProfile{UUID: api.UUID(u), Name: f.String("name")})
			} else {
				h.s.wl.Remove(api.UUID(u))
			}
		}
	case bridge.ActionWhitelistOn:
		h.s.wl.SetEnabled(f.Bool("value"))
	case bridge.ActionBan, bridge.ActionPardon:
		l := h.s.bans
		if f.String("list") == "ip" {
			l = h.s.ipBans
		}
		target := strings.TrimSpace(f.String("target"))
		if target == "" {
			return true
		}
		if kind == bridge.ActionPardon {
			l.Pardon(target)
			return true
		}
		e := api.BanEntry{Target: target, Name: f.String("name"), Reason: f.String("reason"), Source: f.String("source")}
		if ms := f.Int("expires"); ms > 0 {
			e.Expires = time.UnixMilli(ms)
		}
		l.Ban(e)
	case bridge.ActionEntityRemove:
		if m, ok := h.entity(f).(*Mob); ok {
			m.Remove()
		}
	case bridge.ActionParticle:
		h.particle(f)
	case bridge.ActionBossBar:
		h.bossBar(f)
	default:
		return false
	}
	return true
}

func (h BridgeHost) addEffect(e api.Entity, f bridge.Fields) {
	id, ok := item.BuiltinID("mob_effect", strings.TrimPrefix(f.String("type"), "minecraft:"))
	if !ok {
		return
	}
	ef := item.Effect{ID: id, Amplifier: int32(f.Int("amplifier")), Duration: int32(f.Int("duration")),
		Ambient: f.Bool("ambient"), ShowParticles: f.Bool("particles"), ShowIcon: f.Bool("icon")}
	switch v := e.(type) {
	case *Mob:
		v.liv.mu.Lock()
		v.liv.setEffect(id, 0, false)
		v.liv.mu.Unlock()
		v.addEffect(ef)
	case *Player:
		v.tasks.add(pending{t: h.s.sched.newTask("bridge", false), run: func(*task) {
			v.removeEffect(id)
			v.addEffect(ef)
		}})
	}
}

func (h BridgeHost) removeEffect(e api.Entity, typ string) {
	var ids []int32
	if typ != "" {
		id, ok := item.BuiltinID("mob_effect", strings.TrimPrefix(typ, "minecraft:"))
		if !ok {
			return
		}
		ids = []int32{id}
	}
	switch v := e.(type) {
	case *Mob:
		v.liv.mu.Lock()
		if typ == "" {
			for id := range v.liv.fx {
				ids = append(ids, id)
			}
		}
		for _, id := range ids {
			v.liv.setEffect(id, 0, false)
		}
		v.liv.mu.Unlock()
	case *Player:
		v.tasks.add(pending{t: h.s.sched.newTask("bridge", false), run: func(*task) {
			if typ == "" {
				for id := range v.use.effects {
					ids = append(ids, id)
				}
			}
			for _, id := range ids {
				v.removeEffect(id)
			}
		}})
	}
}

// particleData lists particle types whose options follow the type id
// (ParticleTypes with a codec); particles not listed carry no options.
var particleData = map[string]bool{
	"dust": true, "dust_color_transition": true, "block": true, "block_marker": true, "falling_dust": true,
	"dust_pillar": true, "block_crumble": true, "entity_effect": true, "tinted_leaves": true, "effect": true,
	"instant_effect": true, "dragon_breath": true, "flash": true, "sculk_charge": true, "shriek": true,
	"item": true, "vibration": true, "trail": true,
}

// appendParticle writes a Particle (type id and options); false if the type is
// unknown or its options are not supported.
func appendParticle(w *proto.Writer, name string, d bridge.Fields) bool {
	name = strings.TrimPrefix(name, "minecraft:")
	id, ok := item.BuiltinID("particle_type", name)
	if !ok {
		return false
	}
	w.VarInt(id)
	if !particleData[name] {
		return true
	}
	switch name {
	case "dust":
		w.I32(int32(d.Int("color")))
		w.F32(float32(max(0.01, d.Float("scale"))))
	case "dust_color_transition":
		w.I32(int32(d.Int("color")))
		w.I32(int32(d.Int("toColor")))
		w.F32(float32(max(0.01, d.Float("scale"))))
	case "block", "block_marker", "falling_dust", "dust_pillar", "block_crumble":
		st, ok := data.ParseState(d.String("block"))
		if !ok {
			st = data.Air
		}
		w.VarInt(int32(st))
	case "entity_effect", "tinted_leaves", "flash":
		w.I32(int32(d.Int("color")))
	case "effect", "instant_effect":
		w.I32(int32(d.Int("color")))
		w.F32(float32(d.Float("power")))
	case "dragon_breath":
		w.F32(float32(d.Float("power")))
	case "sculk_charge":
		w.F32(float32(d.Float("roll")))
	case "shriek":
		w.VarInt(int32(d.Int("delay")))
	default:
		return false
	}
	return true
}

func (h BridgeHost) particle(f bridge.Fields) {
	var w proto.Writer
	w.Bool(f.Bool("force"))
	w.Bool(false)
	w.F64(f.Float("x"))
	w.F64(f.Float("y"))
	w.F64(f.Float("z"))
	w.F32(float32(f.Float("dx")))
	w.F32(float32(f.Float("dy")))
	w.F32(float32(f.Float("dz")))
	w.F32(float32(f.Float("speed")))
	w.I32(int32(max(0, f.Int("count"))))
	if !appendParticle(&w, f.String("name"), f.Map("data")) {
		h.s.log.Debug("bridge particle not supported", "name", f.String("name"))
		return
	}
	raw := encode(&packets.ClientboundLevelParticles{Raw: w.Bytes()})
	if p, ok := h.player(f); ok {
		p.c.SendRaw(raw)
		return
	}
	w2 := h.s.world(f.String("world"))
	if w2 == nil {
		return
	}
	pos := api.Vec3{X: f.Float("x"), Y: f.Float("y"), Z: f.Float("z")}
	r := 32.0
	if f.Bool("force") {
		r = 512
	}
	for _, p := range w2.playerList() {
		if p.Location().Pos.DistanceSq(pos) <= r*r {
			p.c.SendRaw(raw)
		}
	}
}

func (h BridgeHost) bossBar(f bridge.Fields) {
	p, ok := h.player(f)
	id, ok2 := f.UUID("id")
	if !ok || !ok2 {
		return
	}
	pk := &packets.ClientboundBossEvent{EntityUUID: id, Title: toText(componentFromJSON(f.String("title"))),
		Health: float32(min(1, max(0, f.Float("progress")))), Color: int32(f.Int("color")),
		Dividers: int32(f.Int("overlay")), Flags: uint8(f.Int("flags"))}
	switch f.String("op") {
	case "add":
		pk.Action = 0
	case "remove":
		pk.Action = 1
	case "progress":
		pk.Action = 2
	case "title":
		pk.Action = 3
	case "style":
		pk.Action = 4
	case "flags":
		pk.Action = 5
	default:
		return
	}
	p.send(pk)
}
