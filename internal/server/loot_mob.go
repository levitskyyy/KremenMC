package server

import (
	"encoding/json"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/loot"
)

func init() { entityLoot = mobDeathLoot }

// mobDeathLoot is LivingEntity.dropFromLootTable for the mob's default
// entities/<type> table.
func mobDeathLoot(m *Mob, src DamageSource, killer *Player) []item.Stack {
	t := loot.Get("minecraft:entities/" + m.typ.Name)
	if t == nil {
		return nil
	}
	ctx := &loot.Context{
		Origin: m.feet(), HasOrigin: true,
		This:           lootMob{m},
		Attacker:       lootEntityOf(src.Causing),
		DirectAttacker: lootEntityOf(src.Direct),
		Damage: &loot.DamageSource{
			Type:   src.Type,
			Tags:   func(tag string) bool { return inTag("damage_type", tag, src.Type) },
			Source: lootEntityOf(src.Causing),
			Direct: lootEntityOf(src.Direct),
		},
	}
	if killer != nil {
		ctx.LastPlayer = lootPlayer{killer}
		if v, ok := killer.AttributeValue("minecraft:luck"); ok {
			ctx.Luck = float32(v)
		}
	}
	return m.w.RollLoot(t, ctx)
}

func lootEntityOf(e api.Entity) loot.Entity {
	switch v := orNil(e).(type) {
	case *Player:
		return lootPlayer{v}
	case *Mob:
		return lootMob{v}
	}
	return nil
}

// lootMob adapts a Mob for loot predicates.
type lootMob struct{ m *Mob }

func (l lootMob) TypeName() string   { return "minecraft:" + l.m.typ.Name }
func (l lootMob) Position() api.Vec3 { return l.m.feet() }
func (l lootMob) Flag(name string) bool {
	if name == "is_on_fire" {
		return l.m.fireTicks() > 0
	}
	return false
}

func (l lootMob) Equipment(slot string) item.Stack {
	i := -1
	switch slot {
	case "mainhand":
		i = slotMain
	case "offhand":
		i = slotOff
	case "feet":
		i = slotFeet
	case "legs":
		i = slotLegs
	case "chest":
		i = slotChest
	case "head":
		i = slotHead
	}
	if i < 0 {
		return item.Stack{}
	}
	l.m.liv.mu.Lock()
	defer l.m.liv.mu.Unlock()
	return l.m.liv.equip[i]
}

func (l lootMob) Component(string) (json.RawMessage, bool) { return nil, false }
func (l lootMob) TypeSpecific(string, string) (any, bool)  { return nil, false }
func (l lootMob) Vehicle() loot.Entity                     { return nil }
