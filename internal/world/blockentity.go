package world

import (
	"sync"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

var beTypes = sync.OnceValue(func() map[string]int32 {
	m := map[string]int32{}
	p, err := wgdata.Load()
	if err != nil {
		return m
	}
	l, _ := p.Builtin("block_entity_type")
	for i, id := range l {
		m[id] = int32(i)
	}
	return m
})

// BlockEntityNetType is the minecraft:block_entity_type network ID for a
// block entity id ("minecraft:chest"), or -1 if unknown.
func BlockEntityNetType(id string) int32 {
	if n, ok := beTypes()[id]; ok {
		return n
	}
	return -1
}
