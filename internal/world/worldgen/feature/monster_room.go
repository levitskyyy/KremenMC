package feature

import (
	"encoding/json"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
)

// monsterRoom is monster_room (dungeons): a cobblestone box with a mossy
// floor, 1..5 two-high openings at floor level, up to two loot chests
// against walls and a spawner in the centre. Draw order and validity rules
// are inferred from the feat_env probe (tunnel scenes).
type monsterRoom struct{}

var dungeonMobs = [...]string{"minecraft:skeleton", "minecraft:zombie", "minecraft:zombie", "minecraft:spider"}

func (monsterRoom) Place(c *placeCtx, r *Random, p Pos) bool {
	lv := c.lv
	rx := int(r.NextIntn(2)) + 2
	rz := int(r.NextIntn(2)) + 2
	x0, x1 := -rx-1, rx+1
	z0, z1 := -rz-1, rz+1
	open := 0
	for x := x0; x <= x1; x++ {
		for y := -1; y <= 4; y++ {
			for z := z0; z <= z1; z++ {
				q := p.Add(Pos{x, y, z})
				solid := isSolid(lv.Block(q))
				if (y == -1 || y == 4) && !solid {
					return false
				}
				if (x == x0 || x == x1 || z == z0 || z == z1) && y == 0 &&
					data.IsAir(lv.Block(q)) && data.IsAir(lv.Block(q.Add(Pos{0, 1, 0}))) {
					open++
				}
			}
		}
	}
	if open < 1 || open > 5 {
		return false
	}
	caveAir, _ := data.DefaultState("cave_air")
	cobble, _ := data.DefaultState("cobblestone")
	mossy, _ := data.DefaultState("mossy_cobblestone")
	chest, _ := data.DefaultState("chest")
	for x := x0; x <= x1; x++ {
		for y := 3; y >= -1; y-- {
			for z := z0; z <= z1; z++ {
				q := p.Add(Pos{x, y, z})
				st := lv.Block(q)
				if x != x0 && x != x1 && y != -1 && z != z0 && z != z1 {
					if blockName(st) != "chest" && blockName(st) != "spawner" {
						lv.SetBlock(q, caveAir)
					}
					continue
				}
				if y >= 0 && !isSolid(lv.Block(q.Add(Pos{0, -1, 0}))) {
					lv.SetBlock(q, caveAir)
					continue
				}
				if !isSolid(st) || blockName(st) == "chest" {
					continue
				}
				if y == -1 && r.NextIntn(4) != 0 {
					lv.SetBlock(q, mossy)
				} else {
					lv.SetBlock(q, cobble)
				}
			}
		}
	}
	for range 2 {
		for range 3 {
			q := p.Add(Pos{int(r.NextIntn(int32(rx*2+1))) - rx, 0, int(r.NextIntn(int32(rz*2+1))) - rz})
			if !data.IsAir(lv.Block(q)) {
				continue
			}
			n := 0
			for _, d := range horizontal {
				if isSolid(lv.Block(q.Add(d))) {
					n++
				}
			}
			if n != 1 {
				continue
			}
			lv.SetBlock(q, chestFacing(lv, q, chest))
			lv.SetBlockEntity(q, "minecraft:chest", nbt.Compound{
				"LootTable": "minecraft:chests/simple_dungeon", "LootTableSeed": r.NextLong(),
			})
			break
		}
	}
	spawner, _ := data.DefaultState("spawner")
	lv.SetBlock(p, spawner)
	mob := dungeonMobs[r.NextIntn(int32(len(dungeonMobs)))]
	// Fields and types as the reference saves a fresh dungeon spawner.
	lv.SetBlockEntity(p, "minecraft:mob_spawner", nbt.Compound{
		"SpawnData":           nbt.Compound{"entity": nbt.Compound{"id": mob}},
		"SpawnPotentials":     nbt.List{},
		"Delay":               int16(20),
		"MinSpawnDelay":       int16(200),
		"MaxSpawnDelay":       int16(800),
		"SpawnCount":          int16(4),
		"MaxNearbyEntities":   int16(6),
		"RequiredPlayerRange": int16(16),
		"SpawnRange":          int16(4),
	})
	return true
}

// horizontal is north, east, south, west.
var horizontal = [4]Pos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}

var chestAway = [4]string{"south", "west", "north", "east"} // opposite of horizontal[i]

// chestFacing faces the chest away from its single full-cube neighbour,
// north when there is none or several.
func chestFacing(lv *Level, q Pos, st data.StateID) data.StateID {
	face := -1
	for i, d := range horizontal {
		f := data.Flags(lv.Block(q.Add(d)))
		if f&data.FlagFullCube != 0 && f&data.FlagTransparent == 0 {
			if face >= 0 {
				return st
			}
			face = i
		}
	}
	if face < 0 {
		return st
	}
	s, _ := data.With(st, "facing", chestAway[face])
	return s
}

func init() {
	featureTypes["monster_room"] = func(*registry, json.RawMessage) (Feature, error) { return monsterRoom{}, nil }
}
