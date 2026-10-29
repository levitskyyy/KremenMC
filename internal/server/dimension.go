package server

import (
	"fmt"
	"path/filepath"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/world"
)

type dimKind uint8

const (
	dimOverworld dimKind = iota
	dimNether
	dimEnd
)

func dimOf(s string) dimKind {
	switch s {
	case "the_nether", "minecraft:the_nether":
		return dimNether
	case "the_end", "minecraft:the_end":
		return dimEnd
	}
	return dimOverworld
}

func dimOfGen(s string) dimKind { return dimOf(s) }

func (d dimKind) key() api.Key {
	switch d {
	case dimNether:
		return "minecraft:the_nether"
	case dimEnd:
		return "minecraft:the_end"
	}
	return "minecraft:overworld"
}

func (d dimKind) genName() string {
	switch d {
	case dimNether:
		return "the_nether"
	case dimEnd:
		return "the_end"
	}
	return "overworld"
}

func (d dimKind) bounds() world.Dimension {
	switch d {
	case dimNether:
		return world.Nether
	case dimEnd:
		return world.End
	}
	return world.Overworld
}

func (d dimKind) api() api.Dimension {
	switch d {
	case dimNether:
		return api.Nether
	case dimEnd:
		return api.End
	}
	return api.Overworld
}

// seaLevel is the noise settings' sea_level of the dimension.
func (d dimKind) seaLevel() int32 {
	switch d {
	case dimNether:
		return 32
	case dimEnd:
		return 0
	}
	return 63
}

// logicalHeight is the dimension type's logical_height (portal placement).
func (d dimKind) logicalHeight() int {
	if d == dimNether {
		return 128
	}
	return d.bounds().Height
}

// coordScale is the dimension type's coordinate_scale.
func (d dimKind) coordScale() float64 {
	if d == dimNether {
		return 8
	}
	return 1
}

// endSpawn is ServerLevel.END_SPAWN_POINT.
var endSpawn = api.BlockPos{X: 100, Y: 50, Z: 0}

func (d dimKind) defaultSpawn(name string) api.Location {
	if d == dimEnd {
		return api.Location{World: name, Pos: api.Vec3{X: 100.5, Y: 49, Z: 0.5}, Yaw: 90}
	}
	return api.Location{World: name, Pos: api.Vec3{X: 0.5, Y: 64, Z: 0.5}}
}

// OpenDimensions opens the nether (DIM-1) and the end (DIM1) of the
// overworld ow, Bukkit-style named <name>_nether and <name>_the_end.
func (s *Server) OpenDimensions(ow *World, workers, tickProcs int) error {
	for _, d := range [...]struct {
		dim    dimKind
		suffix string
		dir    string
	}{{dimNether, "_nether", "DIM-1"}, {dimEnd, "_the_end", "DIM1"}} {
		if s.worldByKey(d.dim.key()) != nil {
			continue
		}
		w, err := OpenWorld(s, WorldConfig{
			Name: ow.name + d.suffix, Dir: filepath.Join(ow.dir, d.dir), Seed: ow.seed, Flat: ow.flat,
			Workers: workers, TickProcs: tickProcs, Dim: d.dim.genName(),
		})
		if err != nil {
			return fmt.Errorf("dimension %s: %w", d.dim.key(), err)
		}
		s.AddWorld(w)
	}
	return nil
}

func (s *Server) worldByKey(k api.Key) *World {
	for _, w := range s.worldList() {
		if w.key == k {
			return w
		}
	}
	return nil
}

func (s *Server) dimWorld(d dimKind) *World {
	if d == dimOverworld {
		return s.defaultWorld()
	}
	return s.worldByKey(d.key())
}

func (p *Player) dimensionKey() string {
	if w := p.world.Load(); w != nil && w.dim != dimOverworld {
		return string(w.key)
	}
	return "minecraft:overworld"
}

// changeDimension moves the player to another world with a Respawn packet
// that keeps attributes and metadata; runs on the tick goroutine.
func (p *Player) changeDimension(dst *World, to api.Location) {
	src := p.world.Load()
	to.World = dst.name
	if src == dst {
		p.teleport(to)
		return
	}
	if src != nil && p.ticket != 0 {
		src.w.RemoveTicket(p.ticket)
	}
	p.ticket = 0
	p.sentMu.Lock()
	clear(p.sent)
	p.sentMu.Unlock()
	p.hasCenter, p.complete, p.unacked = false, false, 0
	clear(p.tracking)
	gm := p.GameMode()
	p.send(&packets.ClientboundRespawn{WorldState: packets.SpawnInfo{
		Dimension: dst.dimID, Name: string(dst.key), HashedSeed: dst.hashedSeed(),
		Gamemode: int32(gm), PreviousGamemode: -1, IsFlat: dst.flat, SeaLevel: dst.dim.seaLevel(),
		PortalCooldown: int32(p.portal.cooldown),
	}, CopyMetadata: 3})
	p.world.Store(dst)
	p.send(&packets.ClientboundChangeDifficulty{Difficulty: int32(dst.Difficulty())})
	p.sendAbilities()
	p.teleport(to)
	p.send(dst.timePacket())
	p.send(dst.spawnPacket())
	if dst.Storming() {
		p.send(&packets.ClientboundGameEvent{Reason: 2})
		p.send(&packets.ClientboundGameEvent{Reason: 7, GameMode: 1})
	}
	p.send(&packets.ClientboundGameEvent{Reason: 13})
	p.send(p.inventoryPacket())
	p.sendHealth()
	p.sendLoadedData()
}

// respawnWorld is where a dead player respawns: the respawn point's world,
// else the overworld. Switching worlds drops the old view first.
func (p *Player) respawnWorld() *World {
	src := p.world.Load()
	dst := p.s.defaultWorld()
	if r, ok := p.RespawnLocation(); ok && r.World != "" {
		if rw := p.s.world(r.World); rw != nil {
			dst = rw
		}
	}
	if dst == nil {
		dst = src
	}
	// The client drops its whole level on Respawn, so chunks are resent
	// even when the world stays the same.
	if src != dst && src != nil && p.ticket != 0 {
		src.w.RemoveTicket(p.ticket)
		p.ticket = 0
	}
	p.sentMu.Lock()
	clear(p.sent)
	p.sentMu.Unlock()
	p.hasCenter, p.complete, p.unacked = false, false, 0
	clear(p.tracking)
	p.world.Store(dst)
	return dst
}
