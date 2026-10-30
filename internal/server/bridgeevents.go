package server

import (
	"context"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/bridge"
	"github.com/levitskyyy/KremenMC/internal/entity/spawn"
	"github.com/levitskyyy/KremenMC/internal/item"
)

// Java plugin events fired from combat, respawn, teleport and spawning.
// Each returns quickly when no Java plugin listens.

func (s *Server) bridgeListens(event string) *bridge.Bridge {
	if b := s.javaBridge(); b != nil && b.HasListeners(event) {
		return b
	}
	return nil
}

// bridgeDamage fires EntityDamageEvent; false means cancelled.
func (w *World) bridgeDamage(t combatant, src DamageSource, amount *float32) bool {
	b := w.s.bridgeListens(bridge.EventEntityDamage)
	if b == nil {
		return true
	}
	f := map[string]any{"entity": entityMap(t), "cause": src.Type, "damage": float64(*amount)}
	if src.Causing != nil {
		f["damager"] = entityMap(src.Causing)
	}
	if src.Direct != nil {
		f["direct"] = entityMap(src.Direct)
	}
	cancelled, mut := b.FireEvent(context.Background(), bridge.Event{Name: bridge.EventEntityDamage, Fields: f})
	if cancelled {
		return false
	}
	if d, ok := mut["damage"].(float64); ok {
		*amount = float32(d)
	}
	return true
}

type deathOutcome struct {
	msg        api.Component
	show, keep bool
	clearDrops bool
}

// bridgeDeath fires PlayerDeathEvent before the inventory is dropped.
func (p *Player) bridgeDeath(msg api.Component, keep bool) deathOutcome {
	out := deathOutcome{msg: msg, show: true, keep: keep}
	b := p.s.bridgeListens(bridge.EventPlayerDeath)
	if b == nil {
		return out
	}
	var drops []any
	if !keep {
		p.inv.mu.Lock()
		for _, st := range p.inv.m.Items {
			if !st.IsEmpty() && st.EnchantmentLevel("minecraft:vanishing_curse") == 0 {
				drops = append(drops, map[string]any{"type": item.NameOf(st.Item), "count": int(st.Count)})
			}
		}
		p.inv.mu.Unlock()
	}
	p.mu.Lock()
	lvl := p.expLevel
	p.mu.Unlock()
	_, mut := b.FireEvent(context.Background(), bridge.Event{Name: bridge.EventPlayerDeath, Fields: map[string]any{
		"player": bridge.UUID(p.prof.UUID), "deathMessage": textJSON(msg), "keepInventory": keep,
		"drops": drops, "level": lvl,
	}})
	if j, ok := mut["deathMessage"].(string); ok {
		out.msg = componentFromJSON(j)
	}
	if h, ok := mut["hideMessage"].(bool); ok && h {
		out.show = false
	}
	if k, ok := mut["keepInventory"].(bool); ok {
		out.keep = k
	}
	if c, ok := mut["clearDrops"].(bool); ok {
		out.clearDrops = c
	}
	return out
}

// bridgeRespawn fires PlayerRespawnEvent and returns the respawn location.
func (p *Player) bridgeRespawn(at api.Location) api.Location {
	b := p.s.bridgeListens(bridge.EventPlayerRespawn)
	if b == nil {
		return at
	}
	_, mut := b.FireEvent(context.Background(), bridge.Event{Name: bridge.EventPlayerRespawn, Fields: map[string]any{
		"player": bridge.UUID(p.prof.UUID), "location": bridgeLoc(at).Map(),
	}})
	if m, ok := mut["location"].(map[string]any); ok {
		l := bridge.LocationFromMap(m)
		if l.World == "" || l.World == at.World {
			return api.Location{World: at.World, Pos: api.Vec3{X: l.X, Y: l.Y, Z: l.Z}, Yaw: l.Yaw, Pitch: l.Pitch}
		}
	}
	return at
}

var teleportCauses = map[api.TeleportCause]string{
	api.TeleportPlugin: "PLUGIN", api.TeleportCommand: "COMMAND", api.TeleportEnderPearl: "ENDER_PEARL",
	api.TeleportChorusFruit: "CONSUMABLE_EFFECT", api.TeleportNetherPortal: "NETHER_PORTAL", api.TeleportEndPortal: "END_PORTAL",
	api.TeleportEndGateway: "END_GATEWAY", api.TeleportSpectate: "SPECTATE", api.TeleportDismount: "DISMOUNT", api.TeleportExitBed: "EXIT_BED",
}

// bridgeTeleport fires PlayerTeleportEvent; false means cancelled.
func (p *Player) bridgeTeleport(to *api.Location, cause api.TeleportCause) bool {
	b := p.s.bridgeListens(bridge.EventPlayerTeleport)
	if b == nil {
		return true
	}
	c, ok := teleportCauses[cause]
	if !ok {
		c = "UNKNOWN"
	}
	cancelled, mut := b.FireEvent(context.Background(), bridge.Event{Name: bridge.EventPlayerTeleport, Fields: map[string]any{
		"player": bridge.UUID(p.prof.UUID), "from": bridgeLoc(p.Location()).Map(), "to": bridgeLoc(*to).Map(), "cause": c,
	}})
	if cancelled {
		return false
	}
	if m, ok := mut["to"].(map[string]any); ok {
		l := bridge.LocationFromMap(m)
		*to = api.Location{World: to.World, Pos: api.Vec3{X: l.X, Y: l.Y, Z: l.Z}, Yaw: l.Yaw, Pitch: l.Pitch}
	}
	return true
}

// bridgeSpawn fires CreatureSpawnEvent for a mob about to be added; false means cancelled.
func (w *World) bridgeSpawn(m *Mob, reason string) bool {
	b := w.s.bridgeListens(bridge.EventEntitySpawn)
	if b == nil {
		return true
	}
	cancelled, _ := b.FireEvent(context.Background(), bridge.Event{Name: bridge.EventEntitySpawn, Fields: map[string]any{
		"entity": entityMap(m), "reason": reason,
	}})
	return !cancelled
}

func bridgeSpawnReason(r spawn.Reason) string {
	switch r {
	case spawn.ReasonNatural:
		return "NATURAL"
	case spawn.ReasonChunkGeneration:
		return "CHUNK_GEN"
	case spawn.ReasonSpawner:
		return "SPAWNER"
	}
	return "DEFAULT"
}
