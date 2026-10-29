package spawn

import (
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// ChunkGenSeed seeds the chunk-generation spawn random the way the
// reference does for the spawn status: a WorldgenRandom over a legacy LCG
// with setDecorationSeed(levelSeed, minBlockX, minBlockZ). See doc.go for
// the verification status.
func ChunkGenSeed(levelSeed int64, cx, cz int) rng.Source {
	w := rng.NewWorldgenRandom(rng.NewLegacy(0))
	w.SetDecorationSeed(levelSeed, int32(cx*16), int32(cz*16))
	return w
}

// SpawnMobsForChunkGeneration is NaturalSpawner.spawnMobsForChunkGeneration:
// creature-category packs placed while a chunk is generated. r is the
// seeded generation random; lr is the accessor random
// (ServerLevelAccessor.getRandom, the WorldGenRegion random during
// generation) used by spawn predicates and finalizeSpawn. (bx, by, bz) is
// the position whose spawn settings and creature probability apply.
func (e *Engine) SpawnMobsForChunkGeneration(l Level, r, lr rng.Source, bx, by, bz, cx, cz int, sink Sink) {
	biome := l.Biome(bx, by, bz)
	settings := e.Data.Settings(biome)
	list := settings.Mobs(Creature)
	if list.IsEmpty() {
		return
	}
	prob := DefaultCreatureWorldGenProbability
	if b := e.Data.Biomes[biome]; b != nil {
		prob = b.WorldGenProbability
	}
	x0, z0 := cx*16, cz*16
	for r.NextFloat() < prob {
		d := list.Random(r)
		if d == nil {
			continue
		}
		count := int(d.Count.Sample(r))
		var group any
		x := x0 + int(r.NextIntn(16))
		z := z0 + int(r.NextIntn(16))
		sx, sz := x, z
		for range count {
			spawned := false
			for tries := 0; !spawned && tries < 4; tries++ {
				y := e.topNonCollidingY(l, d.Type, x, z)
				if d.Type.Summonable && e.PlacementOK(l, d.Type, x, y, z) {
					w := float64(d.Type.Width)
					px := clampd(float64(x), float64(x0)+w, float64(x0)+16-w)
					pz := clampd(float64(z), float64(z0)+w, float64(z0)+16-w)
					if l.NoCollision(d.Type.SpawnAABB(px, float64(y), pz)) &&
						e.CheckSpawnRules(l, d.Type, ReasonChunkGeneration, blockPos(px), y, blockPos(pz), lr) && d.Type.Mob {
						m := &Mob{Type: d.Type, X: px, Y: float64(y), Z: pz, Reason: ReasonChunkGeneration}
						m.Yaw = r.NextFloat() * 360
						mx, my, mz := m.BlockPos()
						if e.mobCheckSpawnRules(l, m.Type, mx, my, mz) && e.checkSpawnObstruction(l, m.Type, m.BoundingBox(), mx, my, mz) {
							group = e.FinalizeSpawn(l, lr, m, l.DifficultyAt(mx, my, mz), ReasonChunkGeneration, group)
							sink.AddMob(m)
							spawned = true
						}
					}
				}
				x += int(r.NextIntn(5) - r.NextIntn(5))
				z += int(r.NextIntn(5) - r.NextIntn(5))
				for x < x0 || x >= x0+16 || z < z0 || z >= z0+16 {
					x = sx + int(r.NextIntn(5)-r.NextIntn(5))
					z = sz + int(r.NextIntn(5)-r.NextIntn(5))
				}
			}
		}
	}
}

func clampd(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// topNonCollidingY is NaturalSpawner.getTopNonCollidingPos followed by the
// placement type's adjustSpawnPosition.
func (e *Engine) topNonCollidingY(l Level, t *EntityType, x, z int) int {
	y := l.Height(t.Heightmap, x, z)
	if l.Dimension().HasCeiling {
		minY := l.Dimension().MinY
		for {
			y--
			if props(l.BlockState(x, y, z))&bpAir != 0 {
				break
			}
		}
		for {
			y--
			if props(l.BlockState(x, y, z))&bpAir == 0 || y <= minY {
				break
			}
		}
	}
	if t.Placement == PlacementOnGround && props(l.BlockState(x, y-1, z))&bpPathLand != 0 {
		return y - 1
	}
	return y
}
