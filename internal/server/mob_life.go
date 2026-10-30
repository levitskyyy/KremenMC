package server

import (
	"math"
	"math/rand/v2"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/entity/spawn"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// mobLife is the data-driven per-mob state: finalizeSpawn results (baby,
// variants, colors, equipment drop chances), age and breeding, taming and
// shearing. Guarded by mu; inbox runs on the mob's tick.
type mobLife struct {
	mu         sync.Mutex
	inbox      []func()
	vals       map[string]any // finalizeSpawn attributes
	drop       [numSlots]float32
	leftHanded bool
	age        int32 // AgeableMob.age: < 0 baby, > 0 breeding cooldown
	love       int32
	loveBy     *Player
	sheared    bool
	color      int32 // sheep wool
	collar     int32
	tame       bool
	owner      api.UUID
}

// Synched data indices and serializer ids (26.3; tools/refprobe
// kremenmc.probe.mob.Mobs "meta" lines).
const (
	serByte, serVillager, serOptRef = 0, 18, 13

	metaMobFlags = 15
	ageBaby      = -24000
	breedCooldwn = 6000
	loveTicks    = 600
)

// holderMeta lists, per type, the data-driven variant registries sent in
// metadata: index, serializer, registry.
var holderMeta = map[string][][3]any{
	"wolf":            {{23, 25, "wolf_variant"}, {24, 26, "wolf_sound_variant"}},
	"cat":             {{20, 21, "cat_variant"}, {24, 22, "cat_sound_variant"}},
	"cow":             {{18, 23, "cow_variant"}, {19, 24, "cow_sound_variant"}},
	"pig":             {{19, 28, "pig_variant"}, {20, 29, "pig_sound_variant"}},
	"chicken":         {{18, 30, "chicken_variant"}, {19, 31, "chicken_sound_variant"}},
	"frog":            {{18, 27, "frog_variant"}},
	"zombie_nautilus": {{21, 32, "zombie_nautilus_variant"}},
}

// intMeta maps finalizeSpawn int attributes to VarInt metadata indices.
var intMeta = map[string]map[string]byte{
	"axolotl":      {"type": 18},
	"parrot":       {"type": 20},
	"rabbit":       {"type": 18},
	"fox":          {"type": 18},
	"llama":        {"strength": 20, "type": 21},
	"trader_llama": {"strength": 20, "type": 21},
}

var zombieLike = map[string]bool{"zombie": true, "husk": true, "drowned": true, "zombie_villager": true, "zombified_piglin": true}

var notAgeable = map[string]bool{"skeleton": true, "stray": true, "bogged": true, "wither_skeleton": true,
	"creeper": true, "spider": true, "cave_spider": true, "enderman": true, "witch": true, "slime": true,
	"magma_cube": true, "pillager": true, "vindicator": true, "evoker": true, "piglin_brute": true}

// babyIndex is the DATA_BABY_ID index of a type (0 when it has none).
func babyIndex(name string) byte {
	switch {
	case zombieLike[name]:
		return 16
	case name == "piglin":
		return 17
	case notAgeable[name]:
		return 0
	}
	return 16
}

func appendMeta(b []byte, idx byte, ser int32, v ...int32) []byte {
	b = append(b, idx)
	b = proto.AppendVarInt(b, ser)
	if ser == serByte || ser == serBool {
		return append(b, byte(v[0]))
	}
	for _, x := range v {
		b = proto.AppendVarInt(b, x)
	}
	return b
}

func boolI(v bool) int32 {
	if v {
		return 1
	}
	return 0
}

func dyeID(name string) int32 {
	for i, n := range dyeColors {
		if n == name {
			return int32(i)
		}
	}
	return 0
}

var dyeColors = [...]string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
	"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}

// lifeMeta is the full synched data of the life state (Add Entity metadata
// without the terminator). Caller holds l.mu.
func (m *Mob) lifeMeta(l *mobLife) []byte {
	name := m.typ.Name
	var b []byte
	if l.leftHanded {
		b = appendMeta(b, metaMobFlags, serByte, 2)
	}
	if i := babyIndex(name); i != 0 && l.age < 0 {
		b = appendMeta(b, i, serBool, 1)
	}
	for _, h := range holderMeta[name] {
		key := "variant"
		if strings.Contains(h[2].(string), "sound") {
			key = "sound_variant"
		}
		var id int32 = -1
		switch v := l.vals[key].(type) {
		case string:
			id, _ = regsync.Index(h[2].(string), v)
		case int32:
			id = v
		}
		if id >= 0 {
			b = appendMeta(b, byte(h[0].(int)), int32(h[1].(int)), id)
		}
	}
	for k, idx := range intMeta[name] {
		if v, ok := l.vals[k].(int32); ok {
			b = appendMeta(b, idx, serVarInt, v)
		}
	}
	switch name {
	case "sheep":
		b = appendMeta(b, 18, serByte, l.color|boolI(l.sheared)<<4)
	case "wolf":
		b = appendMeta(b, 21, serVarInt, l.collar)
	case "cat":
		b = appendMeta(b, 23, serVarInt, l.collar)
	case "horse":
		c, _ := l.vals["color"].(int32)
		mk, _ := l.vals["markings"].(int32)
		b = appendMeta(b, 19, serVarInt, c|mk<<8)
	case "goat":
		if s, _ := l.vals["screaming"].(bool); s {
			b = appendMeta(b, 18, serBool, 1)
		}
	case "panda":
		g1, _ := l.vals["main_gene"].(int32)
		g2, _ := l.vals["hidden_gene"].(int32)
		b = appendMeta(b, 21, serByte, g1)
		b = appendMeta(b, 22, serByte, g2)
	case "villager", "zombie_villager":
		idx := byte(19)
		if name == "zombie_villager" {
			idx = 20
		}
		t, _ := l.vals["villager_type"].(string)
		if t == "" {
			t = "plains"
		}
		tid, _ := item.BuiltinID("villager_type", t)
		pid, _ := item.BuiltinID("villager_profession", "none")
		b = appendMeta(b, idx, serVillager, tid, pid, 1)
	}
	if isTamable(name) {
		b = appendMeta(b, 18, serByte, boolI(l.tame)<<2)
		if l.tame {
			b = append(b, 19)
			b = proto.AppendVarInt(b, serOptRef)
			b = append(b, 1)
			b = append(b, l.owner[:]...)
		}
	}
	return b
}

func isTamable(name string) bool {
	return name == "wolf" || name == "cat" || name == "parrot" || name == "zombie_nautilus"
}

// syncLife sends the life metadata to viewers and keeps it for new ones.
// Caller holds l.mu; runs on the mob's tick (or before it is published).
func (m *Mob) syncLife(l *mobLife, push bool) {
	raw := m.lifeMeta(l)
	m.trk.SetMeta(entity.EndMeta(append([]byte(nil), raw...)))
	if push && len(raw) > 0 {
		m.trk.Push(encode(&packets.ClientboundSetEntityData{EntityID: m.e.ID, Raw: entity.EndMeta(raw)}))
	}
}

// applySpawnAttrs turns finalizeSpawn results into mob state: equipment
// with drop chances, baby age, variants and colors, and the metadata the
// client renders. Runs before the mob is published.
func (m *Mob) applySpawnAttrs(attrs map[string]any, leftHanded bool) {
	l := &m.life
	l.mu.Lock()
	defer l.mu.Unlock()
	l.vals, l.leftHanded, l.collar = attrs, leftHanded, 14
	for i := range l.drop {
		l.drop[i] = 0.085
	}
	if b, _ := attrs["baby"].(bool); b {
		l.age = ageBaby
		l.inbox = append(l.inbox, func() { m.SetAIAge(ageBaby) })
	}
	if c, ok := attrs["color"].(string); ok && m.typ.Name == "sheep" {
		l.color = dyeID(c)
	}
	geared := false
	for i, s := range slotNames {
		name, ok := attrs["equip."+s].(string)
		if !ok {
			continue
		}
		id, ok := item.IDOf("minecraft:" + name)
		if !ok {
			continue
		}
		m.liv.mu.Lock()
		m.setEquipment(i, item.Stack{Item: id, Count: 1})
		m.liv.mu.Unlock()
		if d, ok := attrs["drop."+s].(float32); ok {
			l.drop[i] = d
		}
		geared = true
	}
	if mh, ok := attrs["max_health"].(int32); ok && m.liv.attrs != nil {
		m.liv.mu.Lock()
		m.liv.attrs.SetBase(entity.AttrMaxHealth, float64(mh))
		m.liv.health = float32(mh)
		m.liv.mu.Unlock()
	}
	m.syncLife(l, false)
	if geared {
		m.trk.SetSpawnExtra(func() [][]byte {
			m.liv.mu.Lock()
			defer m.liv.mu.Unlock()
			var out [][]byte
			if m.liv.attrs != nil {
				out = append(out, encode(attrPacket(m.e.ID, m.liv.attrs, m.liv.attrs.Synced())))
			}
			eq := [6]item.Stack(m.liv.equip[:6])
			return append(out, encode(&packets.ClientboundSetEquipment{EntityID: m.e.ID, Raw: equipmentRaw(eq, []int{0, 1, 2, 3, 4, 5})}))
		})
	}
}

// later queues fn for the mob's tick.
func (m *Mob) later(fn func()) {
	m.life.mu.Lock()
	m.life.inbox = append(m.life.inbox, fn)
	m.life.mu.Unlock()
}

// IsBaby reports whether the mob is a baby (AgeableMob age < 0).
func (m *Mob) IsBaby() bool {
	m.life.mu.Lock()
	defer m.life.mu.Unlock()
	return m.life.age < 0
}

// tickLife is the per-tick part of AgeableMob/Animal: growing up, breeding
// cooldown, love-mode countdown with heart particles, and breeding with a
// nearby partner in love.
func (m *Mob) tickLife() {
	l := &m.life
	l.mu.Lock()
	inbox := l.inbox
	l.inbox = nil
	l.mu.Unlock()
	for _, fn := range inbox {
		fn()
	}
	l.mu.Lock()
	switch {
	case l.age < 0:
		l.age++
		if l.age == 0 {
			m.syncLife(l, true)
		}
	case l.age > 0:
		l.age--
	}
	if l.love > 0 {
		l.love--
		if l.love%10 == 0 {
			m.w.broadcastNear(m.feet(), &packets.ClientboundEntityEvent{EntityID: m.e.ID, EntityStatus: 18})
		}
	}
	l.mu.Unlock()
}

func init() {
	aiHooks.TemptItem = func(typ, it string) bool { return isMobFood(typ, it) }
	aiHooks.Breed = func(a, b *Mob) *Mob { return a.w.spawnOffspring(a, b, a.feet()) }
	aiHooks.AteGrass = func(m *Mob) {
		m.later(func() {
			m.life.mu.Lock()
			if m.life.sheared {
				m.life.sheared = false
				m.syncLife(&m.life, true)
			}
			m.life.mu.Unlock()
		})
	}
}

// spawnOffspring is Animal.spawnChildFromBreeding (the AI's BreedGoal
// calls it through aiHooks.Breed): a baby at the first parent inheriting
// a random parent's variant (sheep: that parent's wool), breeding
// cooldown on both parents, and 1-7 experience. Tick goroutine.
func (w *World) spawnOffspring(a, b *Mob, at api.Vec3) *Mob {
	for _, p := range [2]*Mob{a, b} {
		p.life.mu.Lock()
		p.life.love, p.life.age, p.life.loveBy = 0, breedCooldwn, nil
		p.life.mu.Unlock()
	}
	t := a.typ
	child := newMob(w, t, api.Location{World: w.name, Pos: at, Yaw: rand.Float32() * 360})
	vals := map[string]any{"baby": true}
	a.life.mu.Lock()
	for k, v := range a.life.vals {
		if strings.Contains(k, "variant") || k == "type" || k == "color" || k == "markings" || k == "villager_type" {
			vals[k] = v
		}
	}
	ca := a.life.color
	a.life.mu.Unlock()
	if rand.IntN(2) == 0 {
		b.life.mu.Lock()
		for k, v := range b.life.vals {
			if strings.Contains(k, "variant") || k == "type" {
				vals[k] = v
			}
		}
		ca = b.life.color
		b.life.mu.Unlock()
	}
	vals["color"] = dyeColors[ca&15]
	child.applySpawnAttrs(vals, rand.Float32() < 0.05)
	w.mobsMu.Lock()
	w.addMobLocked(child)
	w.mobsMu.Unlock()
	child.SetAIAge(ageBaby)
	w.dropXP(at, 1+rand.IntN(7))
	return child
}

// feed handles the food items of a mob: breeding (love mode) for adults,
// growth by 10% of the remaining baby time for babies. It reports whether
// the item was used.
func (m *Mob) feed(p *Player, it item.Stack) bool {
	name := item.NameOf(it.Item)
	if !isMobFood(m.typ.Name, name) {
		return false
	}
	l := &m.life
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.age < 0:
		l.age -= l.age / 10 // ageUp(-age/10 seconds)
		age := l.age
		m.later(func() { m.SetAIAge(age) })
		m.w.broadcastNear(m.feet(), &packets.ClientboundEntityEvent{EntityID: m.e.ID, EntityStatus: 18})
	case l.age == 0 && l.love == 0 && m.canFallInLove(l):
		l.love, l.loveBy = loveTicks, p
		m.later(func() { m.SetInLove(loveTicks) })
		m.w.broadcastNear(m.feet(), &packets.ClientboundEntityEvent{EntityID: m.e.ID, EntityStatus: 18})
	default:
		return false
	}
	return true
}

func (m *Mob) canFallInLove(l *mobLife) bool {
	if isTamable(m.typ.Name) && m.typ.Name != "parrot" {
		return l.tame
	}
	return m.typ.Name != "parrot"
}

// foodTag maps a mob to its breeding/tempt item tag (#minecraft:<x>_food).
func foodTag(mob string) string {
	switch mob {
	case "mooshroom":
		return "cow_food"
	case "trader_llama":
		return "llama_food"
	case "zombie_nautilus":
		return "nautilus_food"
	}
	return mob + "_food"
}

func isMobFood(mob, itemName string) bool { return inTag("item", foodTag(mob), itemName) }

// TemptItem reports whether a held item tempts mobs of a type (the
// TemptGoal of the AI reads it).
func TemptItem(mob string, it item.Stack) bool {
	return !it.IsEmpty() && isMobFood(mob, item.NameOf(it.Item))
}

// tickBaseXP is the death experience of a type before equipment bonuses
// (wiki, "Experience#Sources"; Mob.getBaseExperienceReward).
func baseXP(m *Mob) int {
	name := m.typ.Name
	switch name {
	case "blaze", "guardian", "elder_guardian", "evoker", "breeze":
		return 10
	case "ravager", "piglin_brute":
		return 20
	case "wither":
		return 50
	case "ender_dragon":
		return 500
	case "endermite":
		return 3
	case "villager", "wandering_trader", "iron_golem", "snow_golem", "bat", "armor_stand", "allay", "copper_golem":
		return 0
	case "slime", "magma_cube":
		return 1
	}
	if m.liv.attrs == nil {
		return 0
	}
	if isMonsterName(name) {
		return 5
	}
	return 1 + rand.IntN(3)
}

func isMonsterName(n string) bool {
	switch n {
	case "zombie", "husk", "drowned", "zombie_villager", "zombified_piglin", "skeleton", "stray", "bogged",
		"wither_skeleton", "creeper", "spider", "cave_spider", "enderman", "witch", "pillager", "vindicator",
		"phantom", "silverfish", "shulker", "ghast", "hoglin", "zoglin", "piglin", "vex", "warden", "creaking", "parched":
		return true
	}
	return false
}

// deathXP is getExperienceReward for a kill: base, plus 1-3 per
// equipment item with drop chance <= 1, x2.5 for baby zombies, 0 for baby
// animals.
func (m *Mob) deathXP() int {
	xp := baseXP(m)
	baby := m.IsBaby()
	if baby && !zombieLike[m.typ.Name] && !isMonsterName(m.typ.Name) {
		return 0
	}
	if xp >= 5 {
		m.life.mu.Lock()
		drop := m.life.drop
		m.life.mu.Unlock()
		m.liv.mu.Lock()
		for i, st := range m.liv.equip {
			if !st.IsEmpty() && drop[i] <= 1 {
				xp += 1 + rand.IntN(3)
			}
		}
		m.liv.mu.Unlock()
	}
	if baby && zombieLike[m.typ.Name] {
		xp = int(float64(xp) * 2.5)
	}
	return xp
}

// orbSizes are ExperienceOrb.getExperienceValue thresholds (wiki).
var orbSizes = [...]int{2477, 1237, 617, 307, 149, 73, 37, 17, 7, 3, 1}

// dropXP is ExperienceOrb.award: the amount split into orbs.
func (w *World) dropXP(at api.Vec3, amount int) {
	for amount > 0 {
		v := 1
		for _, s := range orbSizes {
			if amount >= s {
				v = s
				break
			}
		}
		amount -= v
		w.spawnOrb(at, v)
	}
}

func (w *World) spawnOrb(at api.Vec3, value int) {
	t, ok := data.Canonical().EntityByName("experience_orb")
	if !ok {
		return
	}
	o := newMob(w, t, api.Location{World: w.name, Pos: at, Yaw: rand.Float32() * 360})
	o.e.Vel = api.Vec3{X: (rand.Float64()*0.2 - 0.1) * 2, Y: rand.Float64() * 0.2 * 2, Z: (rand.Float64()*0.2 - 0.1) * 2}
	o.trk.SetMeta(entity.EndMeta(appendMeta(nil, 8, serVarInt, int32(value))))
	o.trk.Data = int32(value)
	age := 0
	o.onTick = func(o *Mob) {
		age++
		if age >= 6000 {
			o.Remove()
			return
		}
		if age < 2 {
			return
		}
		pos := o.feet()
		for _, p := range w.playerList() {
			if p.GameMode() == api.Spectator || p.IsDead() {
				continue
			}
			pp := p.Location().Pos
			if dx, dy, dz := pp.X-pos.X, pp.Y+0.5-pos.Y, pp.Z-pos.Z; dx*dx+dy*dy+dz*dz < 1.5*1.5 {
				p.giveXP(value)
				p.send(&packets.ClientboundTakeItemEntity{CollectedEntityID: o.e.ID, CollectorEntityID: p.e.ID, PickupItemCount: 1})
				o.Remove()
				return
			}
		}
	}
	w.mobsMu.Lock()
	w.addMobLocked(o)
	w.mobsMu.Unlock()
}

// giveXP is Player.giveExperiencePoints.
func (p *Player) giveXP(n int) {
	p.mu.Lock()
	lvl, prog := int32(p.expLevel), p.expProgress
	p.mu.Unlock()
	total := xpTotal(lvl, prog) + int32(n)
	for {
		need := xpTotal(lvl+1, 0) - xpTotal(lvl, 0)
		if total-xpTotal(lvl, 0) < need {
			break
		}
		lvl++
	}
	need := xpTotal(lvl+1, 0) - xpTotal(lvl, 0)
	old := int32(p.Level())
	p.SetExp(int(lvl), float32(total-xpTotal(lvl, 0))/float32(need))
	for l := old + 1; l <= lvl; l++ {
		p.levelUpSound(l)
	}
}

// dropEquipment is Mob.dropCustomDeathLoot: preserved items (chance > 1)
// always drop; others drop for a player kill with chance + 0.01 x looting,
// damaged to a random durability.
func (m *Mob) dropEquipment(killer *Player, looting int) []item.Stack {
	m.life.mu.Lock()
	drop := m.life.drop
	m.life.mu.Unlock()
	var out []item.Stack
	m.liv.mu.Lock()
	defer m.liv.mu.Unlock()
	for i, st := range m.liv.equip {
		if st.IsEmpty() {
			continue
		}
		c := drop[i]
		if c > 1 || (killer != nil && rand.Float32() < c+0.01*float32(looting)) {
			if c <= 1 && st.IsDamageable() && st.MaxDamage() > 1 {
				st = st.WithDamage(rand.Int32N(st.MaxDamage() - 1))
			}
			out = append(out, st)
			m.liv.equip[i] = item.Stack{}
		}
	}
	return out
}

// finalizeCommandSpawn runs finalizeSpawn (reason spawn egg/command) for a
// mob created through World.SpawnEntity, on its first tick, then resends
// metadata and equipment.
func (w *World) finalizeCommandSpawn(m *Mob) {
	ws := w.spawnState()
	st, ok := spawn.EntityByName("minecraft:" + m.typ.Name)
	if ws == nil || !ok || !st.Mob {
		return
	}
	pos := m.feet()
	sm := &spawn.Mob{Type: st, X: pos.X, Y: pos.Y, Z: pos.Z}
	l := &ws.lvl
	x, y, z := int(math.Floor(pos.X)), int(math.Floor(pos.Y)), int(math.Floor(pos.Z))
	ws.eng.FinalizeSpawn(l, ws.rnd, sm, l.DifficultyAt(x, y, z), spawn.ReasonSpawnItemUse, nil)
	m.applySpawnAttrs(sm.Attrs, sm.LeftHanded)
	m.life.mu.Lock()
	raw := m.lifeMeta(&m.life)
	m.life.mu.Unlock()
	if len(raw) > 0 {
		m.trk.Push(encode(&packets.ClientboundSetEntityData{EntityID: m.e.ID, Raw: entity.EndMeta(raw)}))
	}
	m.liv.mu.Lock()
	eq := [6]item.Stack(m.liv.equip[:6])
	m.liv.mu.Unlock()
	m.trk.Push(encode(&packets.ClientboundSetEquipment{EntityID: m.e.ID, Raw: equipmentRaw(eq, []int{0, 1, 2, 3, 4, 5})}))
}

// lootingOf is the Looting level of the killer's main hand.
func lootingOf(p *Player) int {
	if p == nil {
		return 0
	}
	for _, e := range p.Hand(api.MainHand).Enchantments() {
		if enchantmentName(e.ID) == "minecraft:looting" {
			return int(e.Level)
		}
	}
	return 0
}
