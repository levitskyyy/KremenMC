package structure

import (
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Capped processor (trail ruins archaeology), black box from probe
// kremenmc.probe.jigsaw.CapProbe (finalizeProcessing on synthetic block
// lists, 5 origins, indices and loot seeds all reproduced):
//   - it runs after every block went through the per-block processors, on
//     the whole processed list;
//   - the list is shuffled (Fisher-Yates from the top, nextInt(i+1)) with a
//     legacy positional random of the world seed at the template origin;
//   - in that order the delegate is applied until it has changed limit
//     blocks;
//   - an append_loot rule gives LootTableSeed = the block's rule random
//     (legacy, getSeed(pos)) next long after its predicate draws.

type cappedEntry struct {
	x, y, z int32
	state   data.StateID
	be      nbt.Compound
}

func (pl *ProcessorList) hasCapped() bool {
	if pl == nil {
		return false
	}
	for _, p := range pl.procs {
		if p.typ == "capped" {
			return true
		}
	}
	return false
}

// worldSeed is the seed of the level being decorated (0 outside a stage).
func worldSeed(lv Level) int64 {
	if s, ok := lv.(interface{ Seed() int64 }); ok {
		return s.Seed()
	}
	if sl, ok := lv.(*stageLevel); ok {
		return sl.sc.Seed()
	}
	return 0
}

func (pl *ProcessorList) finalizeCapped(lv Level, reg *data.Registry, ox, oy, oz int32, es []cappedEntry) {
	for _, p := range pl.procs {
		if p.typ != "capped" || p.delegate == nil || p.limit <= 0 || len(es) == 0 {
			continue
		}
		r := rng.NewLegacy(worldSeed(lv)).ForkPositional().At(ox, oy, oz)
		idx := make([]int, len(es))
		for i := range idx {
			idx[i] = i
		}
		for i := len(idx) - 1; i > 0; i-- {
			j := int(r.NextIntn(int32(i + 1)))
			idx[i], idx[j] = idx[j], idx[i]
		}
		n := int32(0)
		for _, i := range idx {
			if n >= p.limit {
				break
			}
			e := &es[i]
			if s, be, ok := p.delegate.applyRuleLoot(reg, lv, e.state, e.x, e.y, e.z); ok {
				e.state, e.be = s, be
				n++
			}
		}
	}
}

// applyRuleLoot applies a rule processor to one block; ok is false when no
// rule matched.
func (p *processor) applyRuleLoot(reg *data.Registry, lv Level, s data.StateID, x, y, z int32) (data.StateID, nbt.Compound, bool) {
	if p.typ != "rule" {
		return s, nil, false
	}
	r := rng.NewLegacy(rng.GetSeed(x, y, z))
	cur := lv.Block(x, y, z)
	for _, ru := range p.rules {
		if ru.input.test(reg, s, r) && ru.location.test(reg, cur, r) {
			var be nbt.Compound
			if ru.lootTable != "" {
				be = nbt.Compound{"LootTable": ru.lootTable, "LootTableSeed": r.NextLong()}
				if bl := reg.BlockOf(ru.output); bl != nil {
					be["id"] = "minecraft:" + bl.Name
					if bl.Name == "suspicious_gravel" || bl.Name == "suspicious_sand" {
						be["id"] = "minecraft:brushable_block"
					}
				}
			}
			return ru.output, be, true
		}
	}
	return s, nil, false
}
