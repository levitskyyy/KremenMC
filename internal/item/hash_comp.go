package item

import (
	"errors"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

var errNoHash = errors.New("item: component not hashable")

// hp reads wire values for hashing; the first error sticks.
type hp struct {
	r   proto.Reader
	err error
	d   int
}

func (p *hp) fail(err error) {
	if p.err == nil {
		p.err = err
	}
}

func (p *hp) vi() int32 {
	v, err := p.r.VarInt()
	p.fail(err)
	return v
}

func (p *hp) i32() int32 {
	v, err := p.r.I32()
	p.fail(err)
	return v
}

func (p *hp) i64() int64 {
	v, err := p.r.I64()
	p.fail(err)
	return v
}

func (p *hp) f32() float32 {
	v, err := p.r.F32()
	p.fail(err)
	return v
}

func (p *hp) f64() float64 {
	v, err := p.r.F64()
	p.fail(err)
	return v
}

func (p *hp) b() bool {
	v, err := p.r.Bool()
	p.fail(err)
	return v
}

func (p *hp) s() string {
	v, err := p.r.String(maxString)
	p.fail(err)
	return v
}

func (p *hp) n() int {
	n, err := count(&p.r, maxList)
	p.fail(err)
	if err != nil {
		return 0
	}
	return n
}

func (p *hp) uuid() [16]byte {
	b, err := p.r.Raw(16)
	p.fail(err)
	var u [16]byte
	copy(u[:], b)
	return u
}

func (p *hp) list(f func() uint32) uint32 {
	n := p.n()
	es := make([]uint32, 0, n)
	for range n {
		if p.err != nil {
			break
		}
		es = append(es, f())
	}
	return hashList(es)
}

// optional runs f when the presence flag is set.
func (p *hp) opt(f func()) {
	if p.b() {
		f()
	}
}

var (
	regOnce  sync.Once
	regNames map[string][]string
	regMu    sync.Mutex
)

// regName names entry id of a registry (static or synchronized).
func regName(reg string, id int32) string {
	regOnce.Do(func() {
		regNames = map[string][]string{}
		for _, r := range regsync.Registries() {
			regNames[r.Name] = r.Entries
		}
	})
	regMu.Lock()
	es, ok := regNames[reg]
	if !ok {
		switch reg {
		case "minecraft:item":
			for _, it := range data.Canonical().Items {
				es = append(es, "minecraft:"+it.Name)
			}
		default:
			if p, err := wgdata.Load(); err == nil {
				es, _ = p.Builtin(reg)
			}
		}
		regNames[reg] = es
	}
	regMu.Unlock()
	if id < 0 || int(id) >= len(es) {
		return ""
	}
	return es[id]
}

func (p *hp) ref(reg string) uint32 {
	id := p.vi()
	n := regName(reg, id)
	if n == "" {
		p.fail(errNoHash)
	}
	return HashString(n)
}

// holder reads a registry reference or an inline value hashed by direct.
func (p *hp) holder(reg string, direct func() uint32) uint32 {
	id := p.vi()
	if id == 0 {
		if direct == nil {
			p.fail(errNoHash)
			return 0
		}
		return direct()
	}
	n := regName(reg, id-1)
	if n == "" {
		p.fail(errNoHash)
	}
	return HashString(n)
}

func (p *hp) holderSet(reg string) uint32 {
	n := p.vi()
	if n == 0 {
		return HashString("#" + p.s())
	}
	if n < 0 || int(n) > p.r.Remaining()+1 {
		p.fail(ErrMalformed)
		return 0
	}
	es := make([]uint32, 0, n-1)
	for range n - 1 {
		es = append(es, p.ref(reg))
	}
	if len(es) == 1 {
		return es[0]
	}
	return hashList(es)
}

func (p *hp) sound() uint32 {
	return p.holder("minecraft:sound_event", func() uint32 {
		m := &mapB{}
		m.put("sound_id", HashString(p.s()))
		p.opt(func() { m.put("range", HashFloat(p.f32())) })
		return m.sum()
	})
}

func (p *hp) enumName(names []string) uint32 {
	v := p.vi()
	if v < 0 || int(v) >= len(names) {
		p.fail(errNoHash)
		return 0
	}
	return HashString(names[v])
}

var (
	dyeNames       = []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray", "light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}
	rarityNames    = []string{"common", "uncommon", "rare", "epic"}
	opNames        = []string{"add_value", "add_multiplied_base", "add_multiplied_total"}
	slotGroupNames = []string{"any", "mainhand", "offhand", "hand", "feet", "legs", "chest", "head", "armor", "body", "saddle"}
	equipSlotNames = []string{"mainhand", "feet", "legs", "chest", "head", "offhand", "body", "saddle"}
	animNames      = []string{"none", "eat", "drink", "block", "bow", "spear", "crossbow", "spyglass", "toot_horn", "brush", "bundle", "trident"}
	swingNames     = []string{"none", "whack", "stab"}
	shapeNames     = []string{"small_ball", "large_ball", "star", "creeper", "burst"}
	foxNames       = []string{"red", "snow"}
	salmonNames    = []string{"small", "medium", "large"}
	parrotNames    = []string{"red_blue", "blue", "green", "yellow_blue", "gray"}
	fishOrder      = []string{"kob", "flopper", "sunstreak", "stripey", "snooper", "glitter", "dasher", "blockfish", "brinely", "betty", "spotty", "clayfish"}
	mooshroomNames = []string{"red", "brown"}
	rabbitNames    = []string{"brown", "white", "black", "white_splotched", "gold", "salt", "evil"}
	horseNames     = []string{"white", "creamy", "chestnut", "brown", "black", "gray", "dark_brown"}
	llamaNames     = []string{"creamy", "white", "brown", "gray"}
	axolotlNames   = []string{"lucy", "wild", "gold", "cyan", "blue"}
	mapPostNames   = []string{"lock", "scale"}
)

// nbtHash hashes one network NBT value the way its codec form hashes.
// text applies the text component codec's boolean and list conventions.
func (p *hp) nbtHash(text bool) uint32 {
	t, err := p.r.U8()
	p.fail(err)
	if t == nbt.TagEnd {
		return HashEmpty()
	}
	return p.nbtPayload(t, text, "")
}

var textBools = map[string]bool{"bold": true, "italic": true, "underlined": true, "strikethrough": true, "obfuscated": true, "interpret": true}

func (p *hp) nbtString() string {
	n, err := p.r.U16()
	p.fail(err)
	b, err := p.r.Raw(int(n))
	p.fail(err)
	return decodeMUTF8(b)
}

func (p *hp) nbtPayload(t byte, text bool, key string) uint32 {
	if p.d++; p.d > 512 {
		p.fail(errDepth)
		return 0
	}
	defer func() { p.d-- }()
	r := &p.r
	switch t {
	case nbt.TagByte:
		v, err := r.I8()
		p.fail(err)
		if text && textBools[key] {
			return HashBool(v != 0)
		}
		return HashByte(v)
	case nbt.TagShort:
		v, err := r.I16()
		p.fail(err)
		return HashShort(v)
	case nbt.TagInt:
		return HashInt(p.i32())
	case nbt.TagLong:
		return HashLong(p.i64())
	case nbt.TagFloat:
		return HashFloat(p.f32())
	case nbt.TagDouble:
		return HashDouble(p.f64())
	case nbt.TagString:
		return HashString(p.nbtString())
	case nbt.TagByteArray:
		n := p.i32()
		b, err := r.Raw(int(max(n, 0)))
		p.fail(err)
		return hashBytes(b)
	case nbt.TagIntArray, nbt.TagLongArray:
		n := p.i32()
		if n < 0 || int(n) > r.Remaining() {
			p.fail(ErrMalformed)
			return 0
		}
		if t == nbt.TagIntArray {
			v := make([]int32, n)
			for i := range v {
				v[i] = p.i32()
			}
			return hashInts(v)
		}
		v := make([]int64, n)
		for i := range v {
			v[i] = p.i64()
		}
		return hashLongs(v)
	case nbt.TagList:
		et, err := r.U8()
		p.fail(err)
		n := p.i32()
		if n < 0 || int(n) > r.Remaining() {
			p.fail(ErrMalformed)
			return 0
		}
		es := make([]uint32, 0, n)
		for range n {
			if p.err != nil {
				break
			}
			if et == nbt.TagCompound {
				es = append(es, p.nbtCompound(text, true))
			} else {
				es = append(es, p.nbtPayload(et, text, key))
			}
		}
		return hashList(es)
	case nbt.TagCompound:
		return p.nbtCompound(text, false)
	}
	p.fail(ErrMalformed)
	return 0
}

// nbtCompound hashes a compound; inside lists a compound whose only key is
// "" is the wrapper NbtOps uses for heterogeneous lists and is unwrapped.
func (p *hp) nbtCompound(text, inList bool) uint32 {
	var es []kv
	var only uint32
	single := true
	for p.err == nil {
		t, err := p.r.U8()
		p.fail(err)
		if t == nbt.TagEnd || err != nil {
			break
		}
		k := p.nbtString()
		v := p.nbtPayload(t, text, k)
		if k != "" || len(es) > 0 {
			single = false
		}
		only = v
		es = append(es, kv{HashString(k), v})
	}
	if inList && single && len(es) == 1 {
		return only
	}
	return hashMap(es)
}

func decodeMUTF8(b []byte) string {
	for _, c := range b {
		if c == 0xc0 || c >= 0xed {
			return decodeMUTF8Slow(b)
		}
	}
	return string(b)
}

func decodeMUTF8Slow(b []byte) string {
	var u []uint16
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c < 0x80:
			u = append(u, uint16(c))
			i++
		case c&0xe0 == 0xc0 && i+1 < len(b):
			u = append(u, uint16(c&0x1f)<<6|uint16(b[i+1]&0x3f))
			i += 2
		case i+2 < len(b):
			u = append(u, uint16(c&0x0f)<<12|uint16(b[i+1]&0x3f)<<6|uint16(b[i+2]&0x3f))
			i += 3
		default:
			i = len(b)
		}
	}
	rs := make([]rune, 0, len(u))
	for i := 0; i < len(u); i++ {
		c := rune(u[i])
		if c >= 0xd800 && c < 0xdc00 && i+1 < len(u) {
			rs = append(rs, ((c-0xd800)<<10|(rune(u[i+1])-0xdc00))+0x10000)
			i++
			continue
		}
		rs = append(rs, c)
	}
	return string(rs)
}

func (p *hp) text() uint32 { return p.nbtHash(true) }

func (p *hp) effectDetails(m *mapB) {
	if a := p.vi(); a != 0 {
		m.put("amplifier", HashByte(int8(a)))
	}
	if d := p.vi(); d != 0 {
		m.put("duration", HashInt(d))
	}
	ambient, particles, icon := p.b(), p.b(), p.b()
	if ambient {
		m.put("ambient", HashBool(true))
	}
	if !particles {
		m.put("show_particles", HashBool(false))
	}
	m.put("show_icon", HashBool(icon))
	p.opt(func() {
		h := &mapB{}
		p.effectDetails(h)
		m.put("hidden_effect", h.sum())
	})
}

func (p *hp) mobEffect() uint32 {
	m := &mapB{}
	m.put("id", p.ref("minecraft:mob_effect"))
	p.effectDetails(m)
	return m.sum()
}

func (p *hp) consumeEffect() uint32 {
	k := p.vi()
	m := &mapB{}
	m.put("type", HashString(regName("minecraft:consume_effect_type", k)))
	switch k {
	case 0:
		m.put("effects", p.list(p.mobEffect))
		if pr := p.f32(); pr != 1 {
			m.put("probability", HashFloat(pr))
		}
	case 1:
		m.put("effects", p.holderSet("minecraft:mob_effect"))
	case 2:
	case 3:
		if d := p.f32(); d != 16 {
			m.put("diameter", HashFloat(d))
		}
		if !p.b() {
			p.fail(errNoHash)
		}
	case 4:
		m.put("sound", p.sound())
	default:
		p.fail(ErrMalformed)
	}
	return m.sum()
}

func (p *hp) patchMap() uint32 {
	add, rem := p.n(), p.n()
	es := make([]kv, 0, add+rem)
	for range add {
		if p.err != nil {
			break
		}
		t, err := typeOf(&p.r)
		p.fail(err)
		if err != nil {
			break
		}
		f := hashers[t]
		if f == nil {
			p.fail(errNoHash)
			break
		}
		es = append(es, kv{HashString(t.String()), f(p)})
	}
	for range rem {
		t, err := typeOf(&p.r)
		p.fail(err)
		es = append(es, kv{HashString("!" + t.String()), hashEmptyMap})
	}
	return hashMap(es)
}

func (p *hp) template() uint32 {
	m := &mapB{}
	m.put("id", p.ref("minecraft:item"))
	if c := p.vi(); c != 1 {
		m.put("count", HashInt(c))
	}
	if pm := p.patchMap(); pm != hashEmptyMap {
		m.put("components", pm)
	}
	return m.sum()
}

func (p *hp) firework() uint32 {
	m := &mapB{}
	m.put("shape", p.enumName(shapeNames))
	ints := func() []int32 {
		n := p.n()
		v := make([]int32, 0, n)
		for range n {
			v = append(v, p.i32())
		}
		return v
	}
	if c := ints(); len(c) > 0 {
		m.put("colors", intList(c))
	}
	if c := ints(); len(c) > 0 {
		m.put("fade_colors", intList(c))
	}
	if p.b() {
		m.put("has_trail", HashBool(true))
	}
	if p.b() {
		m.put("has_twinkle", HashBool(true))
	}
	return m.sum()
}

func (p *hp) enchantments() uint32 {
	n := p.n()
	es := make([]kv, 0, n)
	for range n {
		k := p.ref("minecraft:enchantment")
		es = append(es, kv{k, HashInt(p.vi())})
	}
	return hashMap(es)
}

func (p *hp) resolvable(float bool) uint32 {
	if p.b() {
		if float {
			return HashFloat(p.f32())
		}
		return HashInt(p.i32())
	}
	return HashString(p.s())
}

func (p *hp) kineticCond() uint32 {
	m := &mapB{}
	m.put("max_duration_ticks", HashInt(p.vi()))
	if v := p.f32(); v != 0 {
		m.put("min_speed", HashFloat(v))
	}
	if v := p.f32(); v != 0 {
		m.put("min_relative_speed", HashFloat(v))
	}
	return m.sum()
}

func (p *hp) filterable(read func() uint32) uint32 {
	m := &mapB{}
	m.put("raw", read())
	p.opt(func() { m.put("filtered", read()) })
	return m.sum()
}

func intList(v []int32) uint32 {
	es := make([]uint32, len(v))
	for i, x := range v {
		es[i] = HashInt(x)
	}
	return hashList(es)
}

func fromNBTCompoundWithID(p *hp, reg string) uint32 {
	id := p.ref(reg)
	t, err := p.r.U8()
	p.fail(err)
	if t != nbt.TagCompound {
		p.fail(errNoHash)
		return 0
	}
	var es []kv
	for p.err == nil {
		t, err := p.r.U8()
		p.fail(err)
		if t == nbt.TagEnd || err != nil {
			break
		}
		k := p.nbtString()
		es = append(es, kv{HashString(k), p.nbtPayload(t, false, k)})
	}
	es = append(es, kv{HashString("id"), id})
	return hashMap(es)
}

func intHash(p *hp) uint32  { return HashInt(p.vi()) }
func unitHash(*hp) uint32   { return hashEmptyMap }
func textHash(p *hp) uint32 { return p.text() }
func idHash(p *hp) uint32   { return HashString(p.s()) }
func dyeHash(p *hp) uint32  { return p.enumName(dyeNames) }
func nbtHashF(p *hp) uint32 { return p.nbtHash(false) }
func f32Hash(p *hp) uint32  { return HashFloat(p.f32()) }

func refHash(reg string) func(*hp) uint32 {
	return func(p *hp) uint32 { return p.ref(reg) }
}

func holderRefHash(reg string) func(*hp) uint32 {
	return func(p *hp) uint32 { return p.holder(reg, nil) }
}

func enumHash(names []string) func(*hp) uint32 {
	return func(p *hp) uint32 { return p.enumName(names) }
}

var hashers [NumTypes]func(*hp) uint32

func init() {
	h := &hashers
	h[CustomData] = nbtHashF
	h[MaxStackSize] = intHash
	h[MaxDamage] = intHash
	h[Damage] = intHash
	h[Unbreakable] = unitHash
	h[UseEffects] = func(p *hp) uint32 {
		m := &mapB{}
		if p.b() {
			m.put("can_sprint", HashBool(true))
		}
		if !p.b() {
			m.put("interact_vibrations", HashBool(false))
		}
		if v := p.f32(); v != 0.2 {
			m.put("speed_multiplier", HashFloat(v))
		}
		return m.sum()
	}
	h[CustomName] = textHash
	h[MinimumAttackCharge] = f32Hash
	h[DamageType] = refHash("minecraft:damage_type")
	h[ItemName] = textHash
	h[ItemModel] = idHash
	h[Lore] = func(p *hp) uint32 { return p.list(p.text) }
	h[Rarity] = enumHash(rarityNames)
	h[Enchantments] = func(p *hp) uint32 { return p.enchantments() }
	h[AttributeModifiers] = func(p *hp) uint32 {
		return p.list(func() uint32 {
			m := &mapB{}
			m.put("type", p.ref("minecraft:attribute"))
			m.put("id", HashString(p.s()))
			m.put("amount", HashDouble(p.f64()))
			m.put("operation", p.enumName(opNames))
			slot := p.vi()
			if slot < 0 || int(slot) >= len(slotGroupNames) {
				p.fail(errNoHash)
				return 0
			}
			if slot != 0 {
				m.put("slot", HashString(slotGroupNames[slot]))
			}
			switch p.vi() {
			case 0:
			case 1:
				m.put("display", (&mapB{}).put("type", HashString("hidden")).sum())
			case 2:
				m.put("display", (&mapB{}).put("type", HashString("override")).put("value", p.text()).sum())
			default:
				p.fail(ErrMalformed)
			}
			return m.sum()
		})
	}
	h[CanPlaceOn] = blockPredicates
	h[CanBreak] = blockPredicates
	h[CustomModelData] = func(p *hp) uint32 {
		m := &mapB{}
		if l := p.list(func() uint32 { return HashFloat(p.f32()) }); l != hashList(nil) {
			m.put("floats", l)
		}
		if l := p.list(func() uint32 { return HashBool(p.b()) }); l != hashList(nil) {
			m.put("flags", l)
		}
		if l := p.list(func() uint32 { return HashString(p.s()) }); l != hashList(nil) {
			m.put("strings", l)
		}
		if l := p.list(func() uint32 { return HashInt(p.i32()) }); l != hashList(nil) {
			m.put("colors", l)
		}
		return m.sum()
	}
	h[TooltipDisplay] = func(p *hp) uint32 {
		m := &mapB{}
		if p.b() {
			m.put("hide_tooltip", HashBool(true))
		}
		if l := p.list(func() uint32 { return p.ref("minecraft:data_component_type") }); l != hashList(nil) {
			m.put("hidden_components", l)
		}
		return m.sum()
	}
	h[RepairCost] = intHash
	h[CreativeSlotLock] = unitHash
	h[EnchantmentGlintOverride] = func(p *hp) uint32 { return HashBool(p.b()) }
	h[IntangibleProjectile] = func(p *hp) uint32 { p.nbtHash(false); return hashEmptyMap }
	h[Food] = func(p *hp) uint32 {
		m := &mapB{}
		m.put("nutrition", HashInt(p.vi()))
		m.put("saturation", HashFloat(p.f32()))
		if p.b() {
			m.put("can_always_eat", HashBool(true))
		}
		return m.sum()
	}
	h[Consumable] = func(p *hp) uint32 {
		m := &mapB{}
		if v := p.f32(); v != 1.6 {
			m.put("consume_seconds", HashFloat(v))
		}
		if a := p.enumName(animNames); a != HashString("eat") {
			m.put("animation", a)
		}
		if s := p.sound(); s != HashString("minecraft:entity.generic.eat") {
			m.put("sound", s)
		}
		if !p.b() {
			m.put("has_consume_particles", HashBool(false))
		}
		if l := p.list(p.consumeEffect); l != hashList(nil) {
			m.put("on_consume_effects", l)
		}
		return m.sum()
	}
	h[UseRemainder] = func(p *hp) uint32 { return p.template() }
	h[UseCooldown] = func(p *hp) uint32 {
		m := &mapB{}
		m.put("seconds", HashFloat(p.f32()))
		p.opt(func() { m.put("cooldown_group", HashString(p.s())) })
		return m.sum()
	}
	h[DamageResistant] = func(p *hp) uint32 {
		return (&mapB{}).put("types", p.holderSet("minecraft:damage_type")).sum()
	}
	h[Tool] = func(p *hp) uint32 {
		m := &mapB{}
		rules := p.list(func() uint32 {
			r := &mapB{}
			r.put("blocks", p.holderSet("minecraft:block"))
			p.opt(func() { r.put("speed", HashFloat(p.f32())) })
			p.opt(func() { r.put("correct_for_drops", HashBool(p.b())) })
			return r.sum()
		})
		m.put("rules", rules)
		if v := p.f32(); v != 1 {
			m.put("default_mining_speed", HashFloat(v))
		}
		if v := p.vi(); v != 1 {
			m.put("damage_per_block", HashInt(v))
		}
		if !p.b() {
			m.put("can_destroy_blocks_in_creative", HashBool(false))
		}
		return m.sum()
	}
	h[Weapon] = func(p *hp) uint32 {
		m := &mapB{}
		if v := p.vi(); v != 1 {
			m.put("item_damage_per_attack", HashInt(v))
		}
		if v := p.f32(); v != 0 {
			m.put("disable_blocking_for_seconds", HashFloat(v))
		}
		return m.sum()
	}
	h[AttackRange] = func(p *hp) uint32 {
		m := &mapB{}
		names := []string{"min_reach", "max_reach", "min_creative_reach", "max_creative_reach", "hitbox_margin", "mob_factor"}
		defs := []float32{0, 3, 0, 5, 0.3, 1}
		for i, n := range names {
			if v := p.f32(); v != defs[i] {
				m.put(n, HashFloat(v))
			}
		}
		return m.sum()
	}
	h[Enchantable] = func(p *hp) uint32 { return (&mapB{}).put("value", HashInt(p.vi())).sum() }
	h[Equippable] = func(p *hp) uint32 {
		m := &mapB{}
		m.put("slot", p.enumName(equipSlotNames))
		if s := p.sound(); s != HashString("minecraft:item.armor.equip_generic") {
			m.put("equip_sound", s)
		}
		p.opt(func() { m.put("asset_id", HashString(p.s())) })
		p.opt(func() { m.put("camera_overlay", HashString(p.s())) })
		p.opt(func() { m.put("allowed_entities", p.holderSet("minecraft:entity_type")) })
		for _, f := range []struct {
			n string
			d bool
		}{{"dispensable", true}, {"swappable", true}, {"damage_on_hurt", true}, {"equip_on_interact", false}, {"can_be_sheared", false}} {
			if v := p.b(); v != f.d {
				m.put(f.n, HashBool(v))
			}
		}
		if s := p.sound(); s != HashString("minecraft:item.shears.snip") {
			m.put("shearing_sound", s)
		}
		return m.sum()
	}
	h[Repairable] = func(p *hp) uint32 {
		return (&mapB{}).put("items", p.holderSet("minecraft:item")).sum()
	}
	h[Glider] = unitHash
	h[TooltipStyle] = idHash
	h[DeathProtection] = func(p *hp) uint32 {
		m := &mapB{}
		if l := p.list(p.consumeEffect); l != hashList(nil) {
			m.put("death_effects", l)
		}
		return m.sum()
	}
	h[BlocksAttacks] = func(p *hp) uint32 {
		m := &mapB{}
		if v := p.f32(); v != 0 {
			m.put("block_delay_seconds", HashFloat(v))
		}
		if v := p.f32(); v != 1 {
			m.put("disable_cooldown_scale", HashFloat(v))
		}
		red := p.list(func() uint32 {
			r := &mapB{}
			if v := p.f32(); v != 90 {
				r.put("horizontal_blocking_angle", HashFloat(v))
			}
			p.opt(func() { r.put("type", p.holderSet("minecraft:damage_type")) })
			r.put("base", HashFloat(p.f32()))
			r.put("factor", HashFloat(p.f32()))
			return r.sum()
		})
		defRed := hashList([]uint32{(&mapB{}).put("base", HashFloat(0)).put("factor", HashFloat(1)).sum()})
		if red != defRed {
			m.put("damage_reductions", red)
		}
		id := &mapB{}
		id.put("threshold", HashFloat(p.f32()))
		id.put("base", HashFloat(p.f32()))
		id.put("factor", HashFloat(p.f32()))
		if v := id.sum(); v != (&mapB{}).put("threshold", HashFloat(1)).put("base", HashFloat(0)).put("factor", HashFloat(1)).sum() {
			m.put("item_damage", v)
		}
		p.opt(func() { m.put("bypassed_by", p.holderSet("minecraft:damage_type")) })
		p.opt(func() { m.put("block_sound", p.sound()) })
		p.opt(func() { m.put("disabled_sound", p.sound()) })
		return m.sum()
	}
	h[PiercingWeapon] = func(p *hp) uint32 {
		m := &mapB{}
		if !p.b() {
			m.put("deals_knockback", HashBool(false))
		}
		if p.b() {
			m.put("dismounts", HashBool(true))
		}
		p.opt(func() { m.put("sound", p.sound()) })
		p.opt(func() { m.put("hit_sound", p.sound()) })
		return m.sum()
	}
	h[KineticWeapon] = func(p *hp) uint32 {
		m := &mapB{}
		if v := p.vi(); v != 10 {
			m.put("contact_cooldown_ticks", HashInt(v))
		}
		if v := p.vi(); v != 0 {
			m.put("delay_ticks", HashInt(v))
		}
		p.opt(func() { m.put("dismount_conditions", p.kineticCond()) })
		p.opt(func() { m.put("knockback_conditions", p.kineticCond()) })
		p.opt(func() { m.put("damage_conditions", p.kineticCond()) })
		if v := p.f32(); v != 0 {
			m.put("forward_movement", HashFloat(v))
		}
		if v := p.f32(); v != 1 {
			m.put("damage_multiplier", HashFloat(v))
		}
		p.opt(func() { m.put("sound", p.sound()) })
		p.opt(func() { m.put("hit_sound", p.sound()) })
		return m.sum()
	}
	swing := func(p *hp) uint32 {
		m := &mapB{}
		if t := p.enumName(swingNames); t != HashString("whack") {
			m.put("type", t)
		}
		if d := p.vi(); d != 6 {
			m.put("duration", HashInt(d))
		}
		return m.sum()
	}
	h[AttackAnimation] = swing
	h[InteractAnimation] = swing
	h[AdditionalTradeCost] = intHash
	h[BlockTransformer] = refHash("minecraft:block_transformer")
	h[VillagerFood] = func(p *hp) uint32 { return (&mapB{}).put("nutrition", HashInt(p.vi())).sum() }
	h[StoredEnchantments] = func(p *hp) uint32 { return p.enchantments() }
	h[Dye] = dyeHash
	h[DyedColor] = func(p *hp) uint32 { return HashInt(p.i32()) }
	h[MapId] = intHash
	h[MapDecorations] = nbtHashF
	h[MapPostProcessing] = enumHash(mapPostNames)
	h[ChargedProjectiles] = func(p *hp) uint32 { return p.list(p.template) }
	h[BundleContents] = func(p *hp) uint32 { return p.list(p.template) }
	h[PotionContents] = func(p *hp) uint32 {
		m := &mapB{}
		p.opt(func() { m.put("potion", p.ref("minecraft:potion")) })
		p.opt(func() { m.put("custom_color", HashInt(p.i32())) })
		if l := p.list(p.mobEffect); l != hashList(nil) {
			m.put("custom_effects", l)
		}
		p.opt(func() { m.put("custom_name", HashString(p.s())) })
		return m.sum()
	}
	h[PotionDurationScale] = f32Hash
	h[SuspiciousStewEffects] = func(p *hp) uint32 {
		return p.list(func() uint32 {
			m := &mapB{}
			m.put("id", p.ref("minecraft:mob_effect"))
			if d := p.vi(); d != 160 {
				m.put("duration", HashInt(d))
			}
			return m.sum()
		})
	}
	h[WritableBookContent] = func(p *hp) uint32 {
		m := &mapB{}
		if l := p.list(func() uint32 { return p.filterable(func() uint32 { return HashString(p.s()) }) }); l != hashList(nil) {
			m.put("pages", l)
		}
		return m.sum()
	}
	h[WrittenBookContent] = func(p *hp) uint32 {
		m := &mapB{}
		m.put("title", p.filterable(func() uint32 { return HashString(p.s()) }))
		m.put("author", HashString(p.s()))
		if g := p.vi(); g != 0 {
			m.put("generation", HashInt(g))
		}
		if l := p.list(func() uint32 { return p.filterable(p.text) }); l != hashList(nil) {
			m.put("pages", l)
		}
		if p.b() {
			m.put("resolved", HashBool(true))
		}
		return m.sum()
	}
	h[Trim] = func(p *hp) uint32 {
		m := &mapB{}
		m.put("material", p.holder("minecraft:trim_material", nil))
		m.put("pattern", p.holder("minecraft:trim_pattern", nil))
		return m.sum()
	}
	h[DebugStickState] = nbtHashF
	h[EntityData] = func(p *hp) uint32 { return fromNBTCompoundWithID(p, "minecraft:entity_type") }
	h[BucketEntityData] = nbtHashF
	h[BlockEntityData] = func(p *hp) uint32 { return fromNBTCompoundWithID(p, "minecraft:block_entity_type") }
	h[Instrument] = holderRefHash("minecraft:instrument")
	h[ProvidesTrimMaterial] = holderRefHash("minecraft:trim_material")
	h[OminousBottleAmplifier] = intHash
	h[JukeboxPlayable] = holderRefHash("minecraft:jukebox_song")
	h[ProvidesBannerPatterns] = func(p *hp) uint32 { return p.holderSet("minecraft:banner_pattern") }
	h[Recipes] = nbtHashF
	h[LodestoneTracker] = func(p *hp) uint32 {
		m := &mapB{}
		p.opt(func() {
			t := &mapB{}
			t.put("dimension", HashString(p.s()))
			x, y, z := unpackPos(p.i64())
			t.put("pos", hashInts([]int32{x, y, z}))
			m.put("target", t.sum())
		})
		if !p.b() {
			m.put("tracked", HashBool(false))
		}
		return m.sum()
	}
	h[FireworkExplosion] = func(p *hp) uint32 { return p.firework() }
	h[Fireworks] = func(p *hp) uint32 {
		m := &mapB{}
		if d := p.vi(); d != 0 {
			m.put("flight_duration", HashByte(int8(d)))
		}
		if l := p.list(p.firework); l != hashList(nil) {
			m.put("explosions", l)
		}
		return m.sum()
	}
	h[NoteBlockSound] = idHash
	h[Profile] = func(p *hp) uint32 {
		m := &mapB{}
		props := func() {
			if l := p.list(func() uint32 {
				e := &mapB{}
				e.put("name", HashString(p.s()))
				e.put("value", HashString(p.s()))
				p.opt(func() { e.put("signature", HashString(p.s())) })
				return e.sum()
			}); l != hashList(nil) {
				m.put("properties", l)
			}
		}
		uuid := func() {
			u := p.uuid()
			m.put("id", hashInts([]int32{int32(be32(u[0:])), int32(be32(u[4:])), int32(be32(u[8:])), int32(be32(u[12:]))}))
		}
		if p.b() {
			uuid()
			m.put("name", HashString(p.s()))
			props()
		} else {
			p.opt(func() { m.put("name", HashString(p.s())) })
			p.opt(uuid)
			props()
		}
		p.opt(func() { m.put("texture", HashString(p.s())) })
		p.opt(func() { m.put("cape", HashString(p.s())) })
		p.opt(func() { m.put("elytra", HashString(p.s())) })
		p.opt(func() {
			if p.b() {
				m.put("model", HashString("slim"))
			} else {
				m.put("model", HashString("wide"))
			}
		})
		return m.sum()
	}
	h[BannerPatterns] = func(p *hp) uint32 {
		return p.list(func() uint32 {
			m := &mapB{}
			m.put("pattern", p.holder("minecraft:banner_pattern", nil))
			m.put("color", p.enumName(dyeNames))
			return m.sum()
		})
	}
	h[BaseColor] = dyeHash
	h[PotDecorations] = func(p *hp) uint32 {
		m := &mapB{}
		for _, side := range []string{"back", "left", "right", "front"} {
			p.opt(func() { m.put(side, p.template()) })
		}
		return m.sum()
	}
	h[Container] = func(p *hp) uint32 {
		n := p.n()
		es := make([]uint32, 0, n)
		for i := range n {
			p.opt(func() {
				es = append(es, (&mapB{}).put("slot", HashInt(int32(i))).put("item", p.template()).sum())
			})
		}
		return hashList(es)
	}
	h[BlockState] = func(p *hp) uint32 {
		n := p.n()
		es := make([]kv, 0, n)
		for range n {
			k := HashString(p.s())
			es = append(es, kv{k, HashString(p.s())})
		}
		return hashMap(es)
	}
	h[Bees] = func(p *hp) uint32 {
		return p.list(func() uint32 {
			m := &mapB{}
			m.put("entity_data", fromNBTCompoundWithID(p, "minecraft:entity_type"))
			m.put("ticks_in_hive", HashInt(p.vi()))
			m.put("min_ticks_in_hive", HashInt(p.vi()))
			return m.sum()
		})
	}
	h[SulfurCubeContent] = func(p *hp) uint32 { return p.template() }
	h[Lock] = nbtHashF
	h[ContainerLoot] = nbtHashF
	h[BreakSound] = func(p *hp) uint32 { return p.sound() }
	h[Compostable] = func(p *hp) uint32 { return (&mapB{}).put("layers", p.resolvable(false)).sum() }
	h[CookingFuel] = func(p *hp) uint32 {
		return (&mapB{}).put("burn_time", p.resolvable(false)).put("speed_multiplier", p.resolvable(true)).sum()
	}
	h[BrewingFuel] = func(p *hp) uint32 {
		return (&mapB{}).put("uses", p.resolvable(false)).put("speed_multiplier", p.resolvable(true)).sum()
	}
	h[MobVisibility] = func(p *hp) uint32 {
		m := &mapB{}
		m.put("targeting_entity_types", p.holderSet("minecraft:entity_type"))
		m.put("visibility", HashFloat(p.f32()))
		return m.sum()
	}
	h[VillagerVariant] = refHash("minecraft:villager_type")
	h[WolfVariant] = refHash("minecraft:wolf_variant")
	h[WolfSoundVariant] = refHash("minecraft:wolf_sound_variant")
	h[FoxVariant] = enumHash(foxNames)
	h[SalmonSize] = enumHash(salmonNames)
	h[ParrotVariant] = enumHash(parrotNames)
	h[TropicalFishPattern] = func(p *hp) uint32 {
		v := p.vi()
		i := (v>>8)*2 + v&1
		if v&^0x701 != 0 || i < 0 || int(i) >= len(fishOrder) {
			p.fail(errNoHash)
			return 0
		}
		return HashString(fishOrder[i])
	}
	h[MooshroomVariant] = enumHash(mooshroomNames)
	h[RabbitVariant] = func(p *hp) uint32 {
		v := p.vi()
		if v == 99 {
			return HashString("evil")
		}
		if v < 0 || v > 5 {
			p.fail(errNoHash)
			return 0
		}
		return HashString(rabbitNames[v])
	}
	h[PigVariant] = refHash("minecraft:pig_variant")
	h[PigSoundVariant] = refHash("minecraft:pig_sound_variant")
	h[CowVariant] = refHash("minecraft:cow_variant")
	h[CowSoundVariant] = refHash("minecraft:cow_sound_variant")
	h[ChickenVariant] = refHash("minecraft:chicken_variant")
	h[ChickenSoundVariant] = refHash("minecraft:chicken_sound_variant")
	h[ZombieNautilusVariant] = refHash("minecraft:zombie_nautilus_variant")
	h[FrogVariant] = refHash("minecraft:frog_variant")
	h[HorseVariant] = enumHash(horseNames)
	h[PaintingVariant] = holderRefHash("minecraft:painting_variant")
	h[LlamaVariant] = enumHash(llamaNames)
	h[AxolotlVariant] = enumHash(axolotlNames)
	h[CatVariant] = refHash("minecraft:cat_variant")
	h[CatSoundVariant] = refHash("minecraft:cat_sound_variant")
	for _, t := range []Type{WolfCollar, TropicalFishBaseColor, TropicalFishPatternColor, CatCollar, SheepColor, ShulkerColor, CushionColor} {
		h[t] = dyeHash
	}
	h[ProvidesPotteryPattern] = refHash("minecraft:decorated_pot_pattern")
	sign := func(p *hp) uint32 {
		m := &mapB{}
		msgs := make([]uint32, 4)
		for i := range msgs {
			msgs[i] = p.text()
		}
		m.put("messages", hashList(msgs))
		p.opt(func() {
			f := make([]uint32, 4)
			for i := range f {
				f[i] = p.text()
			}
			m.put("filtered_messages", hashList(f))
		})
		m.put("color", p.enumName(dyeNames))
		m.put("has_glowing_text", HashBool(p.b()))
		return m.sum()
	}
	h[SignTextFront] = sign
	h[SignTextBack] = sign
	h[Waxed] = unitHash
}

func be32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func unpackPos(v int64) (x, y, z int32) {
	x = int32(v >> 38)
	y = int32(v << 52 >> 52)
	z = int32(v << 26 >> 38)
	return
}

// HashComponent returns the vanilla hash of an encoded component value.
func HashComponent(t Type, v []byte) (int32, error) {
	if t < 0 || t >= NumTypes || hashers[t] == nil {
		return 0, errNoHash
	}
	p := &hp{r: proto.Reader{B: v}}
	h := hashers[t](p)
	if p.err != nil {
		return 0, p.err
	}
	if p.r.Remaining() != 0 {
		return 0, ErrMalformed
	}
	return int32(h), nil
}

func blockPredicates(p *hp) uint32 {
	n := p.n()
	es := make([]uint32, 0, n)
	for range n {
		m := &mapB{}
		p.opt(func() { m.put("blocks", p.holderSet("minecraft:block")) })
		p.opt(func() {
			k := p.n()
			st := make([]kv, 0, k)
			for range k {
				name := HashString(p.s())
				var v uint32
				if p.b() {
					v = HashString(p.s())
				} else {
					r := &mapB{}
					p.opt(func() { r.put("min", HashString(p.s())) })
					p.opt(func() { r.put("max", HashString(p.s())) })
					v = r.sum()
				}
				st = append(st, kv{name, v})
			}
			m.put("state", hashMap(st))
		})
		p.opt(func() { p.fail(errNoHash) })
		if p.n() != 0 || p.n() != 0 {
			p.fail(errNoHash)
		}
		es = append(es, m.sum())
	}
	if len(es) == 1 {
		return es[0]
	}
	return hashList(es)
}
