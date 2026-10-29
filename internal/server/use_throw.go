package server

import (
	"strings"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/item"
)

var throwables = map[string]string{
	"minecraft:snowball":    "snowball",
	"minecraft:egg":         "egg",
	"minecraft:blue_egg":    "egg",
	"minecraft:brown_egg":   "egg",
	"minecraft:ender_pearl": "ender_pearl",
}

// launch spawns a projectile at the player's eyes (−0.1) along the look
// direction (Projectile.shootFromRotation).
func (p *Player) launch(kind string, speed, inaccuracy float64, pr *projectile) *Mob {
	w := p.world.Load()
	eye := p.EyeLocation()
	at := eye.Pos
	at.Y -= 0.1
	return w.spawnProjectile(kind, p, at, eye.Yaw, eye.Pitch, speed, inaccuracy, pr)
}

// throwItem handles snowballs, eggs and ender pearls; it reports whether
// st is throwable.
func (p *Player) throwItem(st item.Stack) (item.Stack, bool) {
	kind, ok := throwables[item.NameOf(st.Item)]
	if !ok {
		return st, false
	}
	p.launch(kind, 1.5, 1, &projectile{stack: st.WithCount(1)})
	if p.GameMode() != api.Creative {
		st.Count--
	}
	return st, true
}

// bowPower is BowItem.getPowerForTime.
func bowPower(charged int32) float64 {
	f := float64(charged) / 20
	f = (f*f + f*2) / 3
	return min(f, 1)
}

// releaseBow shoots an arrow with vanilla's bow power curve.
func (p *Player) releaseBow(charged int32) {
	f := bowPower(charged)
	if f < 0.1 {
		return
	}
	bow := p.Hand(api.MainHand)
	if item.NameOf(bow.Item) != "minecraft:bow" {
		bow = p.Hand(api.OffHand)
	}
	creative := p.GameMode() == api.Creative
	infinite := bow.EnchantmentLevel("minecraft:infinity") > 0
	ammo := item.Stack{}
	if id, ok := item.IDOf("minecraft:arrow"); ok {
		ammo = item.Stack{Item: id, Count: 1}
	}
	inv := p.Inventory()
	if !creative && !inv.Contains("minecraft:arrow", 1) {
		return
	}
	pickup := pickupAllowed
	if creative || infinite {
		pickup = pickupCreative
	} else {
		inv.RemoveItem(api.ItemStack{Type: "minecraft:arrow", Count: 1})
	}
	pr := &projectile{baseDmg: 2, crit: f == 1, pickup: pickup, weapon: bow, stack: ammo,
		fire: bow.EnchantmentLevel("minecraft:flame") > 0}
	p.launch("arrow", f*3, 1, pr)
	p.hurtHeld(handOf(p, bow), 1)
	p.sendSound(soundID("minecraft:entity.arrow.shoot"), soundPlayers, 1, 1/(randBetween(0, 0.4)+1.2)+float32(f)*0.5, true)
}

// releaseTrident throws a held trident after at least 10 ticks of charge.
func (p *Player) releaseTrident(charged int32, hand api.Hand) {
	if charged < 10 {
		return
	}
	st := p.Hand(hand)
	if item.NameOf(st.Item) != "minecraft:trident" || st.EnchantmentLevel("minecraft:riptide") > 0 {
		return
	}
	creative := p.GameMode() == api.Creative
	pr := &projectile{stack: st.WithCount(1), pickup: pickupAllowed}
	if creative {
		pr.pickup = pickupCreative
	}
	p.launch("trident", 2.5, 1, pr)
	p.hurtHeld(hand, 1)
	if !creative {
		p.setHand(hand, item.Stack{})
	}
	p.sendSound(soundID("minecraft:item.trident.throw"), soundPlayers, 1, 1, true)
}

func handOf(p *Player, st item.Stack) api.Hand {
	if item.Matches(p.Hand(api.OffHand), st) && !item.Matches(p.Hand(api.MainHand), st) {
		return api.OffHand
	}
	return api.MainHand
}

// spawnEgg spawns the egg's mob on the clicked face; it reports whether the
// held item is a spawn egg.
func (p *Player) spawnEgg(w *World, it api.ItemStack, at api.BlockPos, hand api.Hand) bool {
	name := string(it.Type)
	if !strings.HasSuffix(name, "_spawn_egg") {
		return false
	}
	kind := strings.TrimSuffix(name, "_spawn_egg")
	loc := api.Location{World: w.name, Pos: api.Vec3{X: float64(at.X) + 0.5, Y: float64(at.Y), Z: float64(at.Z) + 0.5}, Yaw: p.Location().Yaw + 180}
	if _, err := w.SpawnEntity(api.Key(kind), loc, nil); err == nil && p.GameMode() != api.Creative {
		it.Count--
		if it.Count <= 0 {
			it = api.ItemStack{}
		}
		p.Inventory().SetItemInHand(hand, it)
	}
	return true
}
