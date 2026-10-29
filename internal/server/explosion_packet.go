package server

import (
	"sync"

	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto"
)

// explodeTail encodes the explode packet fields after the knockback:
// explosion particle (explosion or explosion_emitter), sound
// (entity.generic.explode), the default block particle list (poof 0.5/1,
// smoke 1/1, weight 1 each) and play_sound = true.
var explodeTails = sync.OnceValue(func() (t [2][]byte) {
	pid := func(n string) int32 { id, _ := item.BuiltinID("particle_type", n); return id }
	for i, main := range [2]string{"explosion_emitter", "explosion"} {
		var w proto.Writer
		w.VarInt(pid(main))
		w.VarInt(soundID("minecraft:entity.generic.explode") + 1)
		w.VarInt(2)
		w.VarInt(pid("poof"))
		w.F32(0.5)
		w.F32(1)
		w.VarInt(1)
		w.VarInt(pid("smoke"))
		w.F32(1)
		w.F32(1)
		w.VarInt(1)
		w.Bool(true)
		t[i] = w.B
	}
	return t
})

func explodeTail(small bool) []byte {
	if small {
		return explodeTails()[1]
	}
	return explodeTails()[0]
}
