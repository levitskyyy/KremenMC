package server

import (
	"testing"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

func TestBlockSoundTypes(t *testing.T) {
	for _, c := range []struct {
		state, place string
		vol, pitch   float32
	}{
		{"minecraft:stone", "minecraft:block.stone.place", 1, 1},
		{"minecraft:oak_planks", "minecraft:block.wood.place", 1, 1},
		{"minecraft:white_wool", "minecraft:block.wool.place", 1, 1},
		{"minecraft:grass_block[snowy=false]", "minecraft:block.grass.place", 1, 1},
	} {
		bs := blockSoundOf(data.MustState(c.state))
		if bs.place != soundID(c.place) || bs.vol != c.vol || bs.pitch != c.pitch {
			t.Errorf("%s: place %d (want %d) vol %v pitch %v", c.state, bs.place, soundID(c.place), bs.vol, bs.pitch)
		}
	}
}

func TestPlayerHurtSoundByEffects(t *testing.T) {
	for typ, want := range map[string]string{
		"minecraft:generic":          "entity.player.hurt",
		"minecraft:drown":            "entity.player.hurt_drown",
		"minecraft:on_fire":          "entity.player.hurt_on_fire",
		"minecraft:freeze":           "entity.player.hurt_freeze",
		"minecraft:sweet_berry_bush": "entity.player.hurt_sweet_berry_bush",
	} {
		if got := playerHurtSound(damageTypeOf(typ)); got != want {
			t.Errorf("%s: %s, want %s", typ, got, want)
		}
	}
}

// TestMobHurtSound matches the reference observation for a zombie hit:
// a positional sound, hostile category, volume 1, pitch 1±0.2 (baby +0.5),
// heard by everyone including the attacker; the death sound replaces it.
func TestMobHurtSound(t *testing.T) {
	s := startServer(t, nil)
	c := dial(t, s.Addr(), "Listener")
	pos := c.joinPlay(s.Addr())
	w := s.defaultWorld()
	hurtID, deathID := soundID("minecraft:entity.zombie.hurt"), soundID("minecraft:entity.zombie.death")
	e, err := w.SpawnEntity("minecraft:zombie", api.Location{Pos: api.Vec3{X: pos.X + 2, Y: pos.Y, Z: pos.Z}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	z := e.(*Mob)
	z.Hurt(EntityDamage("minecraft:player_attack", mustPlayer(t, s, "Listener")), 1)
	h := expect(c, func(p *packets.ClientboundSound) bool { return p.Sound.ID == hurtID })
	if h.SoundCategory != soundHostile || h.Volume != 1 || h.Pitch < 0.8 || h.Pitch > 1.2 {
		t.Fatalf("hurt sound %+v", h)
	}
	z.liv.mu.Lock()
	z.liv.cooldown = 0
	z.liv.mu.Unlock()
	z.Hurt(EntityDamage("minecraft:player_attack", mustPlayer(t, s, "Listener")), 100)
	expect(c, func(p *packets.ClientboundSound) bool { return p.Sound.ID == deathID })
}

// TestPlayerSounds: a watcher hears the builder's place sound (stone:
// volume 1, pitch 0.8, blocks), footsteps (0.15, players) and hurt sound;
// the builder hears neither its own place nor step sounds.
func TestPlayerSounds(t *testing.T) {
	s := startServer(t, nil)
	a := dial(t, s.Addr(), "Builder")
	pos := a.joinPlay(s.Addr())
	b := dial(t, s.Addr(), "Watcher")
	b.joinPlay(s.Addr())

	stone, _ := data.Canonical().ItemByName("stone")
	slot := proto.AppendVarInt(nil, 1)
	slot = proto.AppendVarInt(slot, int32(stone))
	slot = append(slot, 0, 0)
	a.send(&packets.ServerboundSetCreativeModeSlot{Slot: 36, Raw: slot})
	a.send(&packets.ServerboundSetCarriedItem{SlotID: 0})
	target := proto.BlockPos{X: int32(pos.X) + 2, Y: int32(pos.Y) - 1, Z: int32(pos.Z)}
	a.send(&packets.ServerboundUseItemOn{Location: target, Direction: 1, CursorX: 0.5, CursorY: 1, CursorZ: 0.5, Sequence: 7})
	place := soundID("minecraft:block.stone.place")
	pl := expect(b, func(p *packets.ClientboundSound) bool { return p.Sound.ID == place })
	if pl.Volume != 1 || pl.Pitch != 0.8 || pl.SoundCategory != soundBlocks {
		t.Fatalf("place sound %+v", pl)
	}
	w := s.defaultWorld()
	floor := blockSoundOf(w.w.Block(int(pos.X), int(pos.Y)-1, int(pos.Z))).step
	for i := 1; i <= 12; i++ {
		a.send(&packets.ServerboundMovePlayerPos{X: pos.X, Y: pos.Y, Z: pos.Z - float64(i)*0.2, Flags: packets.MovementFlagsOnGround})
	}
	st := expect(b, func(p *packets.ClientboundSound) bool { return p.Sound.ID == floor })
	if st.Volume != 0.15 || st.SoundCategory != soundPlayers {
		t.Fatalf("step sound %+v", st)
	}
	mustPlayer(t, s, "Watcher").(*Player).SetGameMode(api.Survival)
	pb := mustPlayer(t, s, "Watcher").(*Player)
	pb.Hurt(NewDamage("minecraft:drown"), 1)
	hurtID := soundID("minecraft:entity.player.hurt_drown")
	expect(a, func(p *packets.ClientboundSound) bool { return p.Sound.ID == hurtID && p.Pitch == 1 })
	a.send(&packets.ServerboundChatCommand{Command: "nosuchcommand"})
	for {
		pk := expect[packets.Packet](a, nil)
		if snd, ok := pk.(*packets.ClientboundSound); ok && (snd.Sound.ID == place || snd.Sound.ID == floor) {
			t.Fatalf("builder heard its own sound %+v", snd)
		}
		if _, ok := pk.(*packets.ClientboundSystemChat); ok {
			break
		}
	}
}
