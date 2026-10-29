// Package loot evaluates vanilla 26.3 loot tables from the embedded data
// pack: pools, entries, conditions (predicates), modifiers (item functions)
// and number providers. Random draws match the reference implementation
// call for call, so a table rolled with the same random source and
// parameters yields the same stacks and leaves the source in the same state.
package loot

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Entity is what entity predicates and enchantment lookups read from a
// context entity. Implementations live in the server.
type Entity interface {
	// TypeName is the namespaced entity type, e.g. "minecraft:zombie".
	TypeName() string
	Position() api.Vec3
	// Flag reports an entity flag predicate key (is_on_fire, is_baby,
	// is_sneaking, is_sprinting, is_swimming, is_flying, is_fall_flying).
	Flag(name string) bool
	// Equipment returns the stack in "mainhand", "offhand", "head",
	// "chest", "legs", "feet" or "body".
	Equipment(slot string) item.Stack
	// Component returns an entity data component as its JSON value, e.g.
	// Component("minecraft:sheep/color") = `"black"`.
	Component(name string) (json.RawMessage, bool)
	// TypeSpecific returns a type_specific sub-predicate field, e.g.
	// TypeSpecific("cube_mob", "size") = 2.
	TypeSpecific(kind, field string) (any, bool)
	Vehicle() Entity
}

// DamageSource describes the damage that killed the entity.
type DamageSource struct {
	Type   string // damage type, e.g. "minecraft:player_attack"
	Tags   func(tag string) bool
	Source Entity // causing entity
	Direct Entity // direct entity
}

// BlockEntity exposes the data components a block entity carries
// (BlockEntity.collectComponents), keyed by component type.
type BlockEntity interface {
	Components() map[item.Type][]byte
}

// World is the block access location_check needs.
type World interface {
	Block(x, y, z int) data.StateID
}

// Context is LootParams plus the random source of a LootContext.
type Context struct {
	Rand rng.Source

	Origin    api.Vec3
	HasOrigin bool
	State     data.StateID
	HasState  bool
	Tool      item.Stack
	HasTool   bool

	ExplosionRadius float32
	HasExplosion    bool

	This, Attacker, DirectAttacker, LastPlayer Entity
	Damage                                     *DamageSource
	BlockEntity                                BlockEntity
	World                                      World
	Luck                                       float32

	// Dynamic produces the drops of a minecraft:dynamic entry (e.g. sherds).
	Dynamic func(name string, emit func(item.Stack))

	visited map[*Table]bool
}

// Table is a compiled loot table.
type Table struct {
	ID        string
	Sequence  string // random_sequence, or ""
	pools     []*pool
	modifiers []*modifier
}

var (
	cacheMu sync.Mutex
	cache   = map[string]*Table{}
	missing = map[string]bool{}
)

// Get returns the table with the given id ("minecraft:blocks/stone"), or nil.
func Get(id string) *Table {
	if !strings.Contains(id, ":") {
		id = "minecraft:" + id
	}
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if t, ok := cache[id]; ok || missing[id] {
		return t
	}
	t, err := load(id)
	if err != nil {
		missing[id] = true
		return nil
	}
	cache[id] = t
	return t
}

func load(id string) (*Table, error) {
	pk, err := wgdata.Load()
	if err != nil {
		return nil, fmt.Errorf("loot: %w", err)
	}
	raw, ok := pk.JSON("loot_table", id)
	if !ok {
		return nil, fmt.Errorf("loot: no table %s", id)
	}
	t, err := Parse(id, raw)
	if err != nil {
		return nil, fmt.Errorf("loot: %s: %w", id, err)
	}
	return t, nil
}

// Parse compiles a loot table from its data-pack JSON.
func Parse(id string, raw []byte) (*Table, error) {
	var j struct {
		Pools    []json.RawMessage `json:"pools"`
		Modifier json.RawMessage   `json:"modifier"`
		Sequence string            `json:"random_sequence"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	t := &Table{ID: id, Sequence: j.Sequence}
	for _, p := range j.Pools {
		pl, err := parsePool(p)
		if err != nil {
			return nil, err
		}
		t.pools = append(t.pools, pl)
	}
	var err error
	if t.modifiers, err = parseModifiers(j.Modifier); err != nil {
		return nil, err
	}
	return t, nil
}

// Roll is LootTable.getRandomItems(params, random): the generated stacks,
// split to their max stack size. Like the reference it may contain empty
// stacks (e.g. fully decayed by an explosion); callers skip them.
func (t *Table) Roll(ctx *Context) []item.Stack {
	var out []item.Stack
	t.RollRaw(ctx, func(s item.Stack) {
		s = s.Norm()
		max := s.MaxStackSize()
		for s.Count > max {
			out = append(out, s.WithCount(max))
			s = s.WithCount(s.Count - max)
		}
		out = append(out, s)
	})
	return out
}

// RollRaw is LootTable.getRandomItemsRaw: unsplit stacks to emit.
func (t *Table) RollRaw(ctx *Context, emit func(item.Stack)) {
	if ctx.visited == nil {
		ctx.visited = map[*Table]bool{}
	}
	if ctx.visited[t] {
		return
	}
	ctx.visited[t] = true
	defer delete(ctx.visited, t)
	out := decorate(t.modifiers, emit, ctx)
	for _, p := range t.pools {
		p.addRandomItems(out, ctx)
	}
}

// Roll rolls the table id; an unknown table drops nothing.
func Roll(id string, ctx *Context) []item.Stack {
	t := Get(id)
	if t == nil {
		return nil
	}
	return t.Roll(ctx)
}

// pool is a LootPool.
type pool struct {
	conds     []*cond
	modifiers []*modifier
	rolls     number
	bonus     number
	entries   []*entry
}

func parsePool(raw json.RawMessage) (*pool, error) {
	var j struct {
		Rolls     json.RawMessage   `json:"rolls"`
		Bonus     json.RawMessage   `json:"bonus_rolls"`
		Entries   []json.RawMessage `json:"entries"`
		Condition json.RawMessage   `json:"condition"`
		Modifier  json.RawMessage   `json:"modifier"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	p := &pool{}
	var err error
	if p.rolls, err = parseNumber(j.Rolls); err != nil {
		return nil, err
	}
	if j.Bonus != nil {
		if p.bonus, err = parseNumber(j.Bonus); err != nil {
			return nil, err
		}
	} else {
		p.bonus = constant(0)
	}
	if p.conds, err = parseConds(j.Condition); err != nil {
		return nil, err
	}
	if p.modifiers, err = parseModifiers(j.Modifier); err != nil {
		return nil, err
	}
	for _, e := range j.Entries {
		en, err := parseEntry(e)
		if err != nil {
			return nil, err
		}
		p.entries = append(p.entries, en)
	}
	return p, nil
}

// addRandomItems is LootPool.addRandomItems.
func (p *pool) addRandomItems(emit func(item.Stack), ctx *Context) {
	if !allConds(p.conds, ctx) {
		return
	}
	out := decorate(p.modifiers, emit, ctx)
	n := p.rolls.Int(ctx) + floorF(p.bonus.Float(ctx)*ctx.Luck)
	for range n {
		p.addRandomItem(out, ctx)
	}
}

func (p *pool) addRandomItem(emit func(item.Stack), ctx *Context) {
	var cands []*entry
	total := int32(0)
	for _, e := range p.entries {
		e.expand(ctx, func(x *entry) {
			if w := x.weightFor(ctx.Luck); w > 0 {
				cands = append(cands, x)
				total += w
			}
		})
	}
	switch len(cands) {
	case 0:
		return
	case 1:
		cands[0].create(emit, ctx)
		return
	}
	j := ctx.Rand.NextIntn(total)
	for _, e := range cands {
		j -= e.weightFor(ctx.Luck)
		if j < 0 {
			e.create(emit, ctx)
			return
		}
	}
}

func floorF(f float32) int32 {
	i := int32(f)
	if f < float32(i) {
		i--
	}
	return i
}

// decorate wraps emit so every stack passes through mods first.
func decorate(mods []*modifier, emit func(item.Stack), ctx *Context) func(item.Stack) {
	if len(mods) == 0 {
		return emit
	}
	return func(s item.Stack) {
		for _, m := range mods {
			s = m.apply(s, ctx)
		}
		emit(s)
	}
}

// oneOrMany decodes a JSON value that is either one element or a list.
func oneOrMany(raw json.RawMessage) []json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var list []json.RawMessage
	if raw[0] == '[' && json.Unmarshal(raw, &list) == nil {
		return list
	}
	return []json.RawMessage{raw}
}

func typeOf(raw json.RawMessage) string {
	var t struct {
		Type string `json:"type"`
	}
	json.Unmarshal(raw, &t)
	return strings.TrimPrefix(t.Type, "minecraft:")
}

func qualify(id string) string {
	if strings.Contains(id, ":") {
		return id
	}
	return "minecraft:" + id
}

// BlockTable returns the loot table id of a block (Block.getLootTable):
// "minecraft:blocks/<name>" when such a table exists.
func BlockTable(b *data.Block) string {
	if b == nil {
		return ""
	}
	id, ok := blockTableOverrides[b.Name]
	if !ok {
		id = "minecraft:blocks/" + b.Name
	}
	if id == "" || Get(id) == nil {
		return ""
	}
	return id
}

// SequenceSource is RandomSequences.get(id, worldSeed) with the default
// settings (salt 0, world seed and sequence id both included): the source
// a table with a random_sequence draws from in that world.
func SequenceSource(worldSeed int64, id string) rng.Source {
	h := rng.SeedFromHashOf(qualify(id))
	return rng.NewXoroshiroSeed(rng.UpgradeSeedTo128bitUnmixed(worldSeed).Xor(h.Lo, h.Hi).Mixed())
}
