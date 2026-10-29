package server

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// kremenmcPatchKey stores an item's full component patch in wire form next to
// the vanilla "components" compound, so components without an NBT mapping
// here survive a round trip. Vanilla ignores the extra key.
const kremenmcPatchKey = "kremenmc:patch"

func (s *Server) playerDataPath(id api.UUID) string {
	w := s.defaultWorld()
	if w == nil {
		return ""
	}
	return filepath.Join(w.dir, "playerdata", id.String()+".dat")
}

// playerSnapshot is everything written to playerdata, captured on the tick
// goroutine.
type playerSnapshot struct {
	c nbt.Compound
}

// snapshotData builds the vanilla Player.dat compound; it runs in the
// player phase or on the tick goroutine.
func (p *Player) snapshotData() playerSnapshot {
	p.mu.Lock()
	pos, vel := p.e.Pos, p.e.Vel
	yaw, pitch := p.e.Yaw, p.e.Pitch
	ground := p.e.OnGround
	fall := p.e.FallDistance
	health := float32(p.health)
	food, sat := int32(p.food), p.saturation
	lvl, prog := int32(p.expLevel), p.expProgress
	gm := p.gm
	flying, mayfly := p.flying, p.allowFlight
	p.mu.Unlock()
	u := &p.use
	c := nbt.Compound{
		"DataVersion":         data.Canonical().DataVersion,
		"Pos":                 nbt.List{Type: nbt.TagDouble, Items: []any{pos.X, pos.Y, pos.Z}},
		"Motion":              nbt.List{Type: nbt.TagDouble, Items: []any{vel.X, vel.Y, vel.Z}},
		"Rotation":            nbt.List{Type: nbt.TagFloat, Items: []any{yaw, pitch}},
		"OnGround":            ground,
		"fall_distance":       fall,
		"Fire":                int16(-20),
		"Air":                 int16(300),
		"Invulnerable":        false,
		"PortalCooldown":      int32(0),
		"UUID":                uuidInts(p.prof.UUID),
		"Health":              health,
		"AbsorptionAmount":    float32(0),
		"HurtTime":            int16(0),
		"HurtByTimestamp":     int32(0),
		"DeathTime":           int16(0),
		"FallFlying":          false,
		"foodLevel":           food,
		"foodSaturationLevel": sat,
		"foodExhaustionLevel": u.hunger.Exhaustion,
		"foodTickTimer":       int32(0),
		"XpLevel":             lvl,
		"XpP":                 prog,
		"XpTotal":             xpTotal(lvl, prog),
		"XpSeed":              int32(0),
		"Score":               int32(0),
		"playerGameType":      int32(gm),
		"SelectedItemSlot":    p.held.Load(),
		"Dimension":           p.dimensionKey(),
		"seenCredits":         false,
		"abilities": nbt.Compound{
			"invulnerable": gm == api.Creative || gm == api.Spectator, "flying": flying, "mayfly": mayfly,
			"instabuild": gm == api.Creative, "mayBuild": gm != api.Adventure && gm != api.Spectator,
			"flySpeed": float32(0.05), "walkSpeed": float32(0.1),
		},
	}
	var effects []nbt.Compound
	for _, e := range u.effects {
		if name, ok := builtinName("mob_effect", e.id); ok {
			effects = append(effects, nbt.Compound{
				"id": name, "amplifier": int8(e.amplifier), "duration": e.duration,
				"ambient": e.ambient, "show_particles": e.particles, "show_icon": e.icon,
			})
		}
	}
	if len(effects) > 0 {
		c["active_effects"] = effects
	}
	inv := []nbt.Compound{}
	equip := nbt.Compound{}
	p.inv.mu.Lock()
	for i := range 36 {
		if st := p.inv.get(i); !st.IsEmpty() {
			t := stackNBT(st)
			t["Slot"] = int8(i)
			inv = append(inv, t)
		}
	}
	for i, slot := range [...]string{"feet", "legs", "chest", "head"} {
		if st := p.inv.get(invArmorFeet + i); !st.IsEmpty() {
			equip[slot] = stackNBT(st)
		}
	}
	if st := p.inv.get(invOffhand); !st.IsEmpty() {
		equip["offhand"] = stackNBT(st)
	}
	p.inv.mu.Unlock()
	c["Inventory"] = inv
	if len(equip) > 0 {
		c["equipment"] = equip
	}
	c["EnderItems"] = []nbt.Compound{}
	return playerSnapshot{c: c}
}

func uuidInts(u api.UUID) []int32 {
	out := make([]int32, 4)
	for i := range out {
		out[i] = int32(uint32(u[i*4])<<24 | uint32(u[i*4+1])<<16 | uint32(u[i*4+2])<<8 | uint32(u[i*4+3]))
	}
	return out
}

// xpTotal is the total experience of a level and progress
// (Player.getXpNeededForNextLevel summed).
func xpTotal(level int32, prog float32) int32 {
	need := func(l int32) int32 {
		switch {
		case l >= 30:
			return 112 + (l-30)*9
		case l >= 15:
			return 37 + (l-15)*5
		}
		return 7 + l*2
	}
	t := int32(0)
	for l := range level {
		t += need(l)
	}
	return t + int32(prog*float32(need(level)))
}

// builtinName resolves a code-defined registry id to its name.
func builtinName(reg string, id int32) (string, bool) {
	pk, err := wgdata.Load()
	if err != nil {
		return "", false
	}
	es, _ := pk.Builtin(reg)
	if id < 0 || int(id) >= len(es) {
		return "", false
	}
	return es[id], true
}

// stackNBT is the ItemStack codec form: id, count and components, plus the
// wire patch under kremenmc:patch.
func stackNBT(st item.Stack) nbt.Compound {
	c := nbt.Compound{"id": item.NameOf(st.Item), "count": st.Count}
	comps := nbt.Compound{}
	st.Patch.Each(func(t item.Type, v []byte, removed bool) {
		name := t.String()
		if removed {
			comps["!"+name] = nbt.Compound{}
			return
		}
		if x, ok := componentNBT(t, v); ok {
			comps[name] = x
		}
	})
	if len(comps) > 0 {
		c["components"] = comps
	}
	if st.Patch.Len() > 0 {
		c[kremenmcPatchKey] = item.AppendPatch(nil, st.Patch)
	}
	return c
}

// componentNBT converts the components whose codec form is simple; others
// are carried by kremenmc:patch only.
func componentNBT(t item.Type, v []byte) (any, bool) {
	r := proto.Reader{B: v}
	switch t {
	case item.Damage, item.MaxDamage, item.MaxStackSize, item.RepairCost:
		n, err := r.VarInt()
		return n, err == nil
	case item.Unbreakable:
		return nbt.Compound{}, true
	case item.Enchantments, item.StoredEnchantments:
		es, err := item.DecodeEnchantments(v)
		if err != nil {
			return nil, false
		}
		m := nbt.Compound{}
		for _, e := range es {
			if n := enchantmentName(e.ID); n != "" {
				m[n] = e.Level
			}
		}
		return m, true
	}
	return nil, false
}

func enchantmentName(id int32) string {
	for _, r := range regsync.Registries() {
		if r.Name == "minecraft:enchantment" && id >= 0 && int(id) < len(r.Entries) {
			return r.Entries[id]
		}
	}
	return ""
}

// stackFromNBT reads a stack written by stackNBT; vanilla-written stacks
// keep the simple components componentNBT knows.
func stackFromNBT(c nbt.Compound) (item.Stack, bool) {
	name, _ := c["id"].(string)
	id, ok := item.IDOf(name)
	if !ok || id == 0 {
		return item.Stack{}, false
	}
	n := int32(1)
	switch x := c["count"].(type) {
	case int32:
		n = x
	case int8:
		n = int32(x)
	}
	st := item.Stack{Item: id, Count: n}
	if raw, ok := c[kremenmcPatchKey].([]byte); ok {
		r := proto.Reader{B: raw}
		if p, err := item.ReadPatch(&r); err == nil {
			st.Patch = p
			return st, true
		}
	}
	comps, _ := c["components"].(nbt.Compound)
	for k, v := range comps {
		t, ok := item.TypeByName(k)
		if !ok {
			continue
		}
		switch t {
		case item.Damage, item.MaxDamage, item.MaxStackSize, item.RepairCost:
			if x, ok := v.(int32); ok {
				st = st.Set(t, item.EncodeVarInt(x))
			}
		case item.Unbreakable:
			st = st.Set(t, nil)
		case item.Enchantments, item.StoredEnchantments:
			m, _ := v.(nbt.Compound)
			var es []item.Enchantment
			for en, lv := range m {
				if eid, ok := regsync.Index("minecraft:enchantment", en); ok {
					if l, ok := lv.(int32); ok {
						es = append(es, item.Enchantment{ID: eid, Level: l})
					}
				}
			}
			st = st.Set(t, item.EncodeEnchantments(es))
		}
	}
	return st, true
}

// savePlayerData writes the player's playerdata file now.
func (p *Player) savePlayerData() error {
	path := p.s.playerDataPath(p.prof.UUID)
	if path == "" {
		return nil
	}
	return writePlayerData(path, p.snapshotData())
}

func writePlayerData(path string, snap playerSnapshot) error {
	raw, err := nbt.Marshal(nil, "", snap.c, false)
	if err != nil {
		return fmt.Errorf("playerdata: %w", err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(raw)
	if err := zw.Close(); err != nil {
		return fmt.Errorf("playerdata: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("playerdata: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("playerdata: %w", err)
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("playerdata: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("playerdata: %w", err)
	}
	old := strings.TrimSuffix(path, ".dat") + ".dat_old"
	if _, err := os.Stat(path); err == nil {
		os.Remove(old)
		os.Rename(path, old)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("playerdata: %w", err)
	}
	return nil
}

func readPlayerData(path string) (nbt.Compound, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("playerdata: %w", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("playerdata: %w", err)
	}
	_, c, _, err := nbt.Unmarshal(raw, false)
	if err != nil {
		return nil, fmt.Errorf("playerdata: %w", err)
	}
	return c, nil
}

// loadPlayerData applies a saved playerdata file before the Login packet;
// it returns the saved location.
func (s *Server) loadPlayerData(p *Player) (api.Location, bool) {
	path := s.playerDataPath(p.prof.UUID)
	if path == "" {
		return api.Location{}, false
	}
	c, err := readPlayerData(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			s.log.Error("load playerdata", "player", p.prof.Name, "err", err)
			if c2, err2 := readPlayerData(strings.TrimSuffix(path, ".dat") + ".dat_old"); err2 == nil {
				c = c2
			}
		}
		if c == nil {
			return api.Location{}, false
		}
	}
	w := s.defaultWorld()
	if d, ok := c["Dimension"].(string); ok {
		if dw := s.worldByKey(api.Key(d)); dw != nil {
			w = dw
		}
	}
	loc := api.Location{World: w.name}
	if l, ok := c["Pos"].(nbt.List); ok && len(l.Items) == 3 {
		loc.Pos.X, _ = l.Items[0].(float64)
		loc.Pos.Y, _ = l.Items[1].(float64)
		loc.Pos.Z, _ = l.Items[2].(float64)
	}
	if l, ok := c["Rotation"].(nbt.List); ok && len(l.Items) == 2 {
		loc.Yaw, _ = l.Items[0].(float32)
		loc.Pitch, _ = l.Items[1].(float32)
	}
	p.mu.Lock()
	if v, ok := c["Health"].(float32); ok {
		p.health = float64(v)
	}
	if v, ok := c["foodLevel"].(int32); ok {
		p.food = int(v)
	}
	if v, ok := c["foodSaturationLevel"].(float32); ok {
		p.saturation = v
	}
	if v, ok := c["XpLevel"].(int32); ok {
		p.expLevel = int(v)
	}
	if v, ok := c["XpP"].(float32); ok {
		p.expProgress = v
	}
	if v, ok := c["playerGameType"].(int32); ok && v >= 0 && v <= 3 {
		p.gm = api.GameMode(v)
		p.allowFlight = p.gm == api.Creative || p.gm == api.Spectator
	}
	if ab, ok := c["abilities"].(nbt.Compound); ok {
		if v, ok := ab["flying"].(int8); ok {
			p.flying = v != 0 && p.allowFlight
		}
	}
	if p.gm == api.Spectator {
		p.flying = true
	}
	if v, ok := c["fall_distance"].(float64); ok {
		p.e.FallDistance = v
	}
	if v, ok := c["OnGround"].(int8); ok {
		p.e.OnGround = v != 0
	}
	p.mu.Unlock()
	if v, ok := c["SelectedItemSlot"].(int32); ok && v >= 0 && v < 9 {
		p.held.Store(v)
	}
	u := &p.use
	if u.cooldowns == nil {
		u.cooldowns = map[string]int64{}
		u.effects = map[int32]*activeEffect{}
		u.seen = map[int32]bool{}
		u.hunger = item.NewHunger()
	}
	if v, ok := c["foodExhaustionLevel"].(float32); ok {
		u.hunger.Exhaustion = v
	}
	u.hunger.Food, u.hunger.Saturation = int32(p.food), p.saturation
	for _, e := range compounds(c["active_effects"]) {
		name, _ := e["id"].(string)
		id, ok := item.BuiltinID("mob_effect", strings.TrimPrefix(name, "minecraft:"))
		if !ok {
			continue
		}
		ae := &activeEffect{id: id}
		if v, ok := e["amplifier"].(int8); ok {
			ae.amplifier = int32(v)
		}
		ae.duration, _ = e["duration"].(int32)
		ae.ambient = byteBool(e["ambient"])
		ae.particles = byteBool(e["show_particles"])
		ae.icon = byteBool(e["show_icon"])
		u.effects[id] = ae
	}
	p.inv.mu.Lock()
	for _, t := range compounds(c["Inventory"]) {
		slot, ok := t["Slot"].(int8)
		if !ok || slot < 0 || slot >= 36 {
			continue
		}
		if st, ok := stackFromNBT(t); ok {
			p.inv.set(int(slot), st)
		}
	}
	if eq, ok := c["equipment"].(nbt.Compound); ok {
		for i, slot := range [...]string{"feet", "legs", "chest", "head"} {
			if t, ok := eq[slot].(nbt.Compound); ok {
				if st, ok := stackFromNBT(t); ok {
					p.inv.set(invArmorFeet+i, st)
				}
			}
		}
		if t, ok := eq["offhand"].(nbt.Compound); ok {
			if st, ok := stackFromNBT(t); ok {
				p.inv.set(invOffhand, st)
			}
		}
	}
	p.inv.mu.Unlock()
	p.dataLoaded = true
	return loc, true
}

// sendLoadedData sends the loaded state the join sequence does not:
// effects and experience.
func (p *Player) sendLoadedData() {
	if !p.dataLoaded {
		return
	}
	for _, e := range p.use.effects {
		var flags uint8
		if e.ambient {
			flags |= 1
		}
		if e.particles {
			flags |= 2
		}
		if e.icon {
			flags |= 4
		}
		p.send(&packets.ClientboundUpdateMobEffect{EntityID: p.e.ID, EffectID: e.id, Amplifier: e.amplifier, Duration: e.duration, Flags: flags})
	}
	p.mu.Lock()
	lvl, prog := p.expLevel, p.expProgress
	p.mu.Unlock()
	p.send(&packets.ClientboundSetExperience{ExperienceBar: prog, Level: int32(lvl), TotalExperience: xpTotal(int32(lvl), prog)})
}

func compounds(v any) []nbt.Compound {
	switch x := v.(type) {
	case []nbt.Compound:
		return x
	case nbt.List:
		out := make([]nbt.Compound, 0, len(x.Items))
		for _, it := range x.Items {
			if c, ok := it.(nbt.Compound); ok {
				out = append(out, c)
			}
		}
		return out
	}
	return nil
}

func byteBool(v any) bool {
	switch x := v.(type) {
	case int8:
		return x != 0
	case bool:
		return x
	}
	return false
}

// dataTick writes requested autosaves: the snapshot is taken here, in the
// player phase, and written off the tick goroutine.
func (p *Player) dataTick() {
	if !p.saveReq.Swap(false) || !p.joined.Load() {
		return
	}
	path := p.s.playerDataPath(p.prof.UUID)
	if path == "" {
		return
	}
	snap := p.snapshotData()
	go func() {
		if err := writePlayerData(path, snap); err != nil {
			p.s.log.Error("autosave playerdata", "player", p.prof.Name, "err", err)
		}
	}()
}

// requestPlayerSaves asks every online player to autosave on its next
// tick.
func (s *Server) requestPlayerSaves() {
	for _, p := range s.onlinePlayers() {
		p.saveReq.Store(true)
	}
}
