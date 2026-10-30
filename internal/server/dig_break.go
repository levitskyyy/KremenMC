package server

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/bridge"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/loot"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// worldLoot is a world's random state for drops: the level random used by
// popResource and item entity velocities, and the loot random sequences.
type worldLoot struct {
	mu   sync.Mutex
	rand *rng.Legacy
	seqs map[string]rng.Source
}

var worldLoots sync.Map // *World -> *worldLoot

func (w *World) lootState() *worldLoot {
	if v, ok := worldLoots.Load(w); ok {
		return v.(*worldLoot)
	}
	v, _ := worldLoots.LoadOrStore(w, &worldLoot{rand: rng.NewLegacy(time.Now().UnixNano()), seqs: map[string]rng.Source{}})
	return v.(*worldLoot)
}

// RollLoot rolls t with ctx. Without ctx.Rand it draws from the world's
// random sequence for the table (RandomSequences), else the level random.
func (w *World) RollLoot(t *loot.Table, ctx *loot.Context) []item.Stack {
	if t == nil {
		return nil
	}
	if ctx.World == nil {
		ctx.World = w.w
	}
	if ctx.Rand != nil {
		return t.Roll(ctx)
	}
	r := w.lootState()
	r.mu.Lock()
	defer r.mu.Unlock()
	if t.Sequence != "" {
		src, ok := r.seqs[t.Sequence]
		if !ok {
			src = loot.SequenceSource(w.seed, t.Sequence)
			r.seqs[t.Sequence] = src
		}
		ctx.Rand = src
	} else {
		ctx.Rand = r.rand
	}
	out := t.Roll(ctx)
	ctx.Rand = nil
	return out
}

// BlockLoot is Block.getDrops: the loot of state at pos broken with tool by
// breaker (nil for none); explosion > 0 adds the explosion radius.
func (w *World) BlockLoot(pos api.BlockPos, st data.StateID, tool item.Stack, breaker loot.Entity, explosion float32) []item.Stack {
	id := loot.BlockTable(data.BlockOf(st))
	if id == "" {
		return nil
	}
	ctx := &loot.Context{
		Origin: api.Vec3{X: float64(pos.X) + 0.5, Y: float64(pos.Y) + 0.5, Z: float64(pos.Z) + 0.5}, HasOrigin: true,
		State: st, HasState: true, Tool: tool, HasTool: true, This: breaker,
	}
	if explosion > 0 {
		ctx.ExplosionRadius, ctx.HasExplosion = explosion, true
	}
	return w.RollLoot(loot.Get(id), ctx)
}

// DropBlockLoot is Block.dropResources: rolls the block's loot and pops
// every stack around pos.
func (w *World) DropBlockLoot(pos api.BlockPos, st data.StateID, tool item.Stack, breaker loot.Entity, explosion float32) {
	for _, s := range w.BlockLoot(pos, st, tool, breaker, explosion) {
		w.PopResource(pos, s)
	}
}

// destroyAndAck is ServerPlayerGameMode.destroyAndAck.
func (p *Player) destroyAndAck(w *World, pos api.BlockPos) {
	if !p.destroyBlock(w, pos) {
		p.resync(w, pos)
	}
}

var gameMasterBlocks = map[string]bool{
	"command_block": true, "chain_command_block": true, "repeating_command_block": true,
	"structure_block": true, "jigsaw": true, "test_block": true, "test_instance_block": true,
}

// destroyBlock is ServerPlayerGameMode.destroyBlock.
func (p *Player) destroyBlock(w *World, pos api.BlockPos) bool {
	st := w.w.Block(pos.X, pos.Y, pos.Z)
	b := data.BlockOf(st)
	if b == nil || data.IsAir(st) {
		return false
	}
	main := p.Hand(api.MainHand)
	gm := p.GameMode()
	if gm == api.Creative && !creativeCanDestroy(main) {
		return false
	}
	if gameMasterBlocks[b.Name] && !(gm == api.Creative && p.opLevel() >= 2) {
		return false
	}
	if p.actionRestricted(w, pos, gm) {
		return false
	}
	drops := gm != api.Creative
	s := p.s
	if s.bus.HasHandlers(api.KindBlockBreak) {
		ev := &api.BlockBreakEvent{Player: p, Block: &block{w: w, pos: pos}, State: blockState(st), DropItems: drops}
		if s.bus.Dispatch(ev).(*api.BlockBreakEvent).Cancelled() {
			return false
		}
		drops = ev.DropItems
	}
	if br := s.javaBridge(); br != nil && br.HasListeners(bridge.EventBlockBreak) {
		cancelled, _ := br.FireEvent(context.Background(), bridge.Event{Name: bridge.EventBlockBreak, Fields: map[string]any{
			"player": bridge.UUID(p.prof.UUID), "world": w.name, "x": pos.X, "y": pos.Y, "z": pos.Z,
			"block": data.StateString(st), "exp": 0,
		}})
		if cancelled {
			return false
		}
	}
	w.levelEvent(p, levelBreak, pos, int32(st))
	contents := w.takeContainer(pos, p)
	if q, ok := counterpart(pos, st); ok && gm == api.Creative {
		if o := w.w.Block(q.X, q.Y, q.Z); sameBlock(o, st) {
			quietBreaks.Store(quietKey{w, q}, struct{}{})
		}
	}
	w.w.SetBlock(pos.X, pos.Y, pos.Z, data.Air)
	for _, c := range contents {
		w.PopResource(pos, c)
	}
	if gm == api.Creative {
		return true
	}
	correct := correctForDrops(main, b)
	p.onMineBlock(b.Hardness)
	if correct {
		p.exhaust(0.005)
		if drops {
			w.DropBlockLoot(pos, st, main, lootPlayer{p}, 0)
		}
	}
	return true
}

// creativeCanDestroy is Item.canDestroyBlock for a creative player: tools
// without can_destroy_blocks_in_creative (swords, maces, ...) and the debug
// stick break nothing.
func creativeCanDestroy(main item.Stack) bool {
	if t, ok := stackTool(main); ok && !t.creativeBreak {
		return false
	}
	return main.IsEmpty() || item.NameOf(main.Item) != "minecraft:debug_stick"
}

func (p *Player) opLevel() int {
	if e, ok := p.s.ops.get(p.prof.UUID); ok {
		return e.Level
	}
	return 0
}

// lootPlayer adapts a Player for loot predicates.
type lootPlayer struct{ p *Player }

func (l lootPlayer) TypeName() string   { return "minecraft:player" }
func (l lootPlayer) Position() api.Vec3 { return l.p.Location().Pos }
func (l lootPlayer) Flag(name string) bool {
	switch name {
	case "is_sneaking":
		return l.p.Sneaking()
	case "is_sprinting":
		return l.p.Sprinting()
	case "is_flying":
		return l.p.Flying()
	}
	return false
}

func (l lootPlayer) Equipment(slot string) item.Stack {
	p := l.p
	p.inv.mu.Lock()
	defer p.inv.mu.Unlock()
	switch slot {
	case "mainhand":
		return p.inv.hand(api.MainHand)
	case "offhand":
		return p.inv.get(invOffhand)
	case "feet":
		return p.inv.get(invArmorFeet)
	case "legs":
		return p.inv.get(invArmorFeet + 1)
	case "chest":
		return p.inv.get(invArmorFeet + 2)
	case "head":
		return p.inv.get(invArmorFeet + 3)
	}
	return item.Stack{}
}

func (l lootPlayer) Component(string) (json.RawMessage, bool) { return nil, false }
func (l lootPlayer) TypeSpecific(kind, field string) (any, bool) {
	if kind == "player" && field == "gamemode" {
		return strings.ToLower(l.p.GameMode().String()), true
	}
	return nil, false
}
func (l lootPlayer) Vehicle() loot.Entity { return nil }

// dropBlockNoTool drops a block's loot as if broken without a tool or
// breaker (api.Block.Break).
func (w *World) dropBlockNoTool(pos api.BlockPos, st data.StateID) {
	w.DropBlockLoot(pos, st, item.Stack{}, nil, 0)
}
