package server

import (
	"math/rand/v2"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// Entity events for a broken equipped item, by inventory slot.
const (
	eventBreakMainHand = 47
	eventBreakOffHand  = 48
)

// onMineBlock is ItemStack.mineBlock for the main hand: tools lose
// damage_per_block durability on blocks that take time to break.
func (p *Player) onMineBlock(hardness float32) {
	if hardness == 0 || p.GameMode() == api.Creative {
		return
	}
	st := p.Hand(api.MainHand)
	v, ok := st.Component(item.Tool)
	if !ok {
		return
	}
	if dpb := toolDamagePerBlock(v); dpb > 0 {
		p.hurtHeld(api.MainHand, dpb)
	}
}

// toolDamagePerBlock reads damage_per_block from an encoded tool component.
func toolDamagePerBlock(v []byte) int32 {
	r := proto.Reader{B: v}
	n, err := r.VarInt()
	if err != nil {
		return 0
	}
	for range n {
		if err := item.SkipHolderSet(&r); err != nil {
			return 0
		}
		for _, size := range []int{4, 1} {
			if ok, _ := r.Bool(); ok {
				r.Raw(size)
			}
		}
	}
	if _, err := r.F32(); err != nil {
		return 0
	}
	d, _ := r.VarInt()
	return d
}

// hurtHeld is ItemStack.hurtAndBreak for a hand: unbreaking may absorb each
// point; at max damage the item breaks with the break entity event.
func (p *Player) hurtHeld(h api.Hand, amount int32) {
	st := p.Hand(h)
	if !st.IsDamageable() || amount <= 0 {
		return
	}
	if lvl := st.EnchantmentLevel("minecraft:unbreaking"); lvl > 0 {
		kept := int32(0)
		for range amount {
			if rand.IntN(int(lvl)+1) == 0 {
				kept++
			}
		}
		amount = kept
		if amount == 0 {
			return
		}
	}
	d := st.Damage() + amount
	if d >= st.MaxDamage() {
		p.setHand(h, item.Stack{})
		ev := int8(eventBreakMainHand)
		if h == api.OffHand {
			ev = eventBreakOffHand
		}
		raw := encode(&packets.ClientboundEntityEvent{EntityID: p.e.ID, EntityStatus: ev})
		p.c.SendRaw(raw)
		p.mu.Lock()
		p.extra = append(p.extra, raw)
		p.mu.Unlock()
		return
	}
	p.setHand(h, st.WithDamage(d))
}
