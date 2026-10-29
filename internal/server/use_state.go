package server

import (
	"math"
	"math/rand/v2"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// useState is the tick-owned gameplay state behind item use: the item in
// use, food exhaustion, cooldowns, effects and what was last synced.
type useState struct {
	active    bool
	hand      api.Hand
	stack     item.Stack
	remaining int32

	hunger      item.Hunger
	lastHealth  float64
	lastFood    int
	lastSatZero bool
	synced      bool

	cooldowns map[string]int64 // group -> end tick
	effects   map[int32]*activeEffect

	equip     [6]item.Stack
	equipInit bool
	seen      map[int32]bool // player entities whose equipment p was sent

	prevGround bool
	usingFlag  bool
}

type activeEffect struct {
	id, amplifier, duration int32
	ambient, particles      bool
	icon                    bool
}

// Living entity flags (metadata index 8).
const (
	metaLivingFlags = 8
	livingUsing     = 0x01
	livingOffhand   = 0x02
)

// Sound categories (SoundSource ids).
const (
	soundNeutral = 6
	soundPlayers = 7
)

// tickGameplay runs once per tick in the player phase after packets.
func (p *Player) tickGameplay() {
	if !p.joined.Load() {
		return
	}
	u := &p.use
	if u.cooldowns == nil {
		u.cooldowns = map[string]int64{}
		u.effects = map[int32]*activeEffect{}
		u.seen = map[int32]bool{}
		u.hunger = item.NewHunger()
	}
	p.tickUsing()
	p.tickEffects()
	p.tickFood()
	p.tickCooldowns()
	p.pickupItems()
	p.syncInventory()
	p.syncEquipment()
	p.syncHealth()
}

func (p *Player) difficulty() int32 {
	if w := p.world.Load(); w != nil {
		return int32(w.Difficulty())
	}
	return item.Normal
}

func (p *Player) naturalRegen() bool {
	if w := p.world.Load(); w != nil {
		if v, ok := w.GameRule("naturalRegeneration"); ok && v == "false" {
			return false
		}
	}
	return true
}

// tickFood is FoodData.tick plus the peaceful regeneration of Player.aiStep.
func (p *Player) tickFood() {
	u := &p.use
	p.mu.Lock()
	u.hunger.Food, u.hunger.Saturation = int32(p.food), p.saturation
	health := float32(p.health)
	gm := p.gm
	p.mu.Unlock()
	if gm == api.Spectator {
		return
	}
	diff, regen := p.difficulty(), p.naturalRegen()
	if diff == item.Peaceful && regen {
		ticks := p.TicksLived()
		if health < 20 && ticks%20 == 0 {
			p.heal(1)
		}
		if u.hunger.NeedsFood() && ticks%10 == 0 {
			u.hunger.Food++
		}
	}
	heal, starve := u.hunger.Tick(diff, regen, float32(p.Health()), 20)
	p.mu.Lock()
	p.food, p.saturation = int(u.hunger.Food), u.hunger.Saturation
	p.mu.Unlock()
	if heal > 0 {
		p.heal(float64(heal))
	}
	if starve {
		p.Damage(1, api.DamageSource{Type: "minecraft:starve"})
	}
}

// exhaust is Player.causeFoodExhaustion.
func (p *Player) exhaust(x float32) {
	if gm := p.GameMode(); gm == api.Creative || gm == api.Spectator {
		return
	}
	p.use.hunger.AddExhaustion(x)
}

func (p *Player) heal(v float64) {
	p.mu.Lock()
	if p.health > 0 {
		p.health = min(p.health+v, 20)
	}
	p.mu.Unlock()
}

// syncHealth sends Set Health when health, food or the zero-ness of
// saturation changed (ServerPlayer.doTick).
func (p *Player) syncHealth() {
	u := &p.use
	p.mu.Lock()
	h, f, z := p.health, p.food, p.saturation == 0
	p.mu.Unlock()
	if u.synced && h == u.lastHealth && f == u.lastFood && z == u.lastSatZero {
		return
	}
	u.synced, u.lastHealth, u.lastFood, u.lastSatZero = true, h, f, z
	p.sendHealth()
}

// onMoved applies movement exhaustion and jump detection for an accepted
// move (ServerPlayer.checkMovementStatistics / jumpFromGround).
func (p *Player) onMoved(from, to api.Vec3, onGround bool) {
	u := &p.use
	dx, dy, dz := to.X-from.X, to.Y-from.Y, to.Z-from.Z
	if u.prevGround && !onGround && dy > 0 && !p.Flying() {
		if p.Sprinting() {
			p.exhaust(0.2)
		} else {
			p.exhaust(0.05)
		}
	}
	u.prevGround = onGround
	if p.Sprinting() && !p.Flying() {
		if cm := roundf(sqrt(dx*dx+dz*dz) * 100); cm > 0 {
			p.exhaust(0.1 * float32(cm) * 0.01)
		}
	}
}

func (p *Player) tickCooldowns() {
	now := p.s.CurrentTick()
	for g, end := range p.use.cooldowns {
		if now >= end {
			delete(p.use.cooldowns, g)
			p.send(&packets.ClientboundCooldown{CooldownGroup: g, CooldownTicks: 0})
		}
	}
}

func cooldownGroup(st item.Stack) string {
	if c, ok := st.UseCooldown(); ok && c.Group != "" {
		return c.Group
	}
	return item.NameOf(st.Item)
}

func (p *Player) onCooldown(st item.Stack) bool {
	end, ok := p.use.cooldowns[cooldownGroup(st)]
	return ok && p.s.CurrentTick() < end
}

func (p *Player) addCooldown(st item.Stack, ticks int32) {
	if ticks <= 0 {
		return
	}
	g := cooldownGroup(st)
	p.use.cooldowns[g] = p.s.CurrentTick() + int64(ticks)
	p.send(&packets.ClientboundCooldown{CooldownGroup: g, CooldownTicks: ticks})
}

func (p *Player) sendSound(sound, category int32, volume, pitch float32, includeSelf bool) {
	pos := p.Location().Pos
	raw := encode(&packets.ClientboundSound{
		Sound: proto.Holder[packets.ItemSoundEvent]{ID: sound}, SoundCategory: category,
		X: int32(pos.X * 8), Y: int32(pos.Y * 8), Z: int32(pos.Z * 8),
		Volume: volume, Pitch: pitch, Seed: rand.Int64(),
	})
	w := p.world.Load()
	for _, o := range w.playerList() {
		if o == p && !includeSelf {
			continue
		}
		if o.Location().Pos.DistanceSq(pos) <= 16*16*max(float64(volume), 1)*max(float64(volume), 1) {
			o.c.SendRaw(raw)
		}
	}
}

func (p *Player) playItemSound(s item.Sound, category int32, volume, pitch float32, includeSelf bool) {
	if s.ID >= 0 {
		p.sendSound(s.ID, category, volume, pitch, includeSelf)
	}
}

func soundID(name string) int32 {
	id, ok := item.BuiltinID("sound_event", name)
	if !ok {
		return -1
	}
	return id
}

func roundf(v float64) int64 { return int64(math.Floor(v + 0.5)) }
func sqrt(v float64) float64 { return math.Sqrt(v) }

// setUsingFlag broadcasts the living-entity "using item" flags to viewers.
func (p *Player) setUsingFlag(active bool, hand api.Hand) {
	var f byte
	if active {
		f = livingUsing
		if hand == api.OffHand {
			f |= livingOffhand
		}
	}
	b := []byte{metaLivingFlags, 0, f, 0xff}
	raw := encode(&packets.ClientboundSetEntityData{EntityID: p.e.ID, Raw: b})
	p.mu.Lock()
	p.extra = append(p.extra, raw)
	p.mu.Unlock()
	p.use.usingFlag = active
}

func triangle(mid, dev float32) float32 {
	return mid + dev*(rand.Float32()-rand.Float32())
}

func randBetween(a, b float32) float32 { return a + rand.Float32()*(b-a) }
