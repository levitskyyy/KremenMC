package command

import (
	"math"
	"strconv"

	"github.com/levitskyyy/KremenMC/api"
)

func timeArg() api.ArgumentType { return api.ArgumentType{Parser: api.ParserTime, HasMin: true} }

func (b *builtins) time() *api.CommandNode {
	set := func(get func(api.CommandContext) int64) api.CommandExecutor {
		return func(c api.CommandContext) (int, error) {
			t := get(c)
			b.onGlobal(func() {
				for _, w := range b.srv.Worlds() {
					w.SetTime(t)
				}
			})
			b.success(c, textf("Set the time to %d", t), true)
			return int(t % 24000), nil
		}
	}
	fixed := func(t int64) func(api.CommandContext) int64 { return func(api.CommandContext) int64 { return t } }
	add := func(c api.CommandContext) (int, error) {
		w, err := b.senderWorld(c)
		if err != nil {
			return 0, err
		}
		dt := c.Int("time")
		result := (w.Time() + dt) % 24000
		b.onGlobal(func() {
			for _, w := range b.srv.Worlds() {
				w.SetTime(w.Time() + dt)
			}
		})
		b.success(c, textf("Set the time to %d", result), true)
		return int(result), nil
	}
	query := func(kind string) api.CommandExecutor {
		return func(c api.CommandContext) (int, error) {
			w, err := b.senderWorld(c)
			if err != nil {
				return 0, err
			}
			var v int64
			switch kind {
			case "daytime":
				v = w.Time() % 24000
			case "gametime":
				v = w.FullTime() % math.MaxInt32
			case "day":
				v = w.FullTime() / 24000 % math.MaxInt32
			}
			c.Sender().SendMessage(textf("The time is %d", v))
			return int(v), nil
		}
	}
	return api.Literal("time").Describe("Changes or queries the world's game time").Then(
		api.Literal("set").Then(
			api.Literal("day").Executes(set(fixed(1000))),
			api.Literal("noon").Executes(set(fixed(6000))),
			api.Literal("night").Executes(set(fixed(13000))),
			api.Literal("midnight").Executes(set(fixed(18000))),
			api.Argument("time", timeArg()).Executes(set(func(c api.CommandContext) int64 { return c.Int("time") })),
		),
		api.Literal("add").Then(api.Argument("time", timeArg()).Executes(add)),
		api.Literal("query").Then(
			api.Literal("daytime").Executes(query("daytime")),
			api.Literal("gametime").Executes(query("gametime")),
			api.Literal("day").Executes(query("day")),
		),
	)
}

func (b *builtins) weather() *api.CommandNode {
	set := func(storm, thunder bool, msg string) api.CommandExecutor {
		return func(c api.CommandContext) (int, error) {
			w, err := b.senderWorld(c)
			if err != nil {
				return 0, err
			}
			b.onGlobal(func() {
				w.SetStorming(storm)
				w.SetThundering(thunder)
			})
			b.success(c, api.Text("Set the weather to "+msg), true)
			return 1, nil
		}
	}
	mode := func(name string, storm, thunder bool, msg string) *api.CommandNode {
		return api.Literal(name).Executes(set(storm, thunder, msg)).
			Then(api.Argument("duration", api.ArgumentType{Parser: api.ParserTime, HasMin: true, Min: 1}).Executes(set(storm, thunder, msg)))
	}
	return api.Literal("weather").Describe("Sets the weather").Then(
		mode("clear", false, false, "clear"),
		mode("rain", true, false, "rain"),
		mode("thunder", true, true, "rain & thunder"),
	)
}

func (b *builtins) difficulty() *api.CommandNode {
	query := func(c api.CommandContext) (int, error) {
		w, err := b.senderWorld(c)
		if err != nil {
			return 0, err
		}
		d := w.Difficulty()
		c.Sender().SendMessage(textf("The difficulty is %s", titleCase(d.String())))
		return int(d), nil
	}
	node := api.Literal("difficulty").Describe("Sets the difficulty level").Executes(query)
	for _, d := range []api.Difficulty{api.DifficultyPeaceful, api.DifficultyEasy, api.DifficultyNormal, api.DifficultyHard} {
		node.Then(api.Literal(d.String()).Executes(func(c api.CommandContext) (int, error) {
			w, err := b.senderWorld(c)
			if err != nil {
				return 0, err
			}
			if w.Difficulty() == d {
				return 0, fail("The difficulty did not change; it is already set to %s", titleCase(d.String()))
			}
			b.onGlobal(func() {
				for _, w := range b.srv.Worlds() {
					w.SetDifficulty(d)
				}
			})
			b.success(c, textf("The difficulty has been set to %s", titleCase(d.String())), true)
			return 0, nil
		}))
	}
	return node
}

type placeMode uint8

const (
	placeReplace placeMode = iota
	placeDestroy
	placeKeep
	placeStrict
	placeHollow
	placeOutline
)

func (m placeMode) flags() api.BlockUpdateFlags {
	if m == placeStrict {
		return api.UpdateClients
	}
	return api.UpdateDefault
}

// place sets one block and reports whether it changed.
func place(w api.World, pos api.BlockPos, st api.BlockState, mode placeMode) bool {
	cur := w.BlockState(pos)
	if cur != nil && cur.ID() == st.ID() {
		return false
	}
	if mode == placeKeep && cur != nil && !cur.IsAir() {
		return false
	}
	if mode == placeDestroy && cur != nil && !cur.IsAir() {
		if blk := w.Block(pos); blk != nil {
			blk.Break(true)
		}
	}
	return w.SetBlockState(pos, st, mode.flags())
}

func (b *builtins) checkPlaceable(w api.World, pos api.BlockPos) error {
	if pos.Y < w.MinY() || pos.Y >= w.MaxY() || pos.X < -30000000 || pos.X >= 30000000 || pos.Z < -30000000 || pos.Z >= 30000000 {
		return fail("That position is out of this world!")
	}
	if !w.IsChunkLoaded(pos.X>>4, pos.Z>>4) {
		return fail("That position is not loaded")
	}
	return nil
}

func (b *builtins) setblock() *api.CommandNode {
	run := func(mode placeMode) api.CommandExecutor {
		return func(c api.CommandContext) (int, error) {
			w, err := b.senderWorld(c)
			if err != nil {
				return 0, err
			}
			pos := c.BlockPos("pos")
			if err := b.checkPlaceable(w, pos); err != nil {
				return 0, err
			}
			st := c.BlockState("block")
			b.forChunks(w, []chunkKey{{pos.X >> 4, pos.Z >> 4}}, func(chunkKey) {
				if place(w, pos, st, mode) {
					b.success(c, textf("Changed the block at %d, %d, %d", pos.X, pos.Y, pos.Z), true)
				} else {
					c.Sender().SendMessage(ErrorComponent(fail("Could not set the block"), ""))
				}
			}, func() {})
			return 1, nil
		}
	}
	return api.Literal("setblock").Describe("Changes a block to another block").
		Then(api.Argument("pos", api.BlockPosArg()).
			Then(api.Argument("block", api.BlockStateArg()).Executes(run(placeReplace)).Then(
				api.Literal("destroy").Executes(run(placeDestroy)),
				api.Literal("keep").Executes(run(placeKeep)),
				api.Literal("replace").Executes(run(placeReplace)),
				api.Literal("strict").Executes(run(placeStrict)),
			)))
}

const defaultFillLimit = 32768

func (b *builtins) fillLimit(w api.World) int64 {
	if v, ok := w.GameRule("max_block_modifications"); ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return defaultFillLimit
}

func (b *builtins) fill() *api.CommandNode {
	run := func(mode placeMode, filtered bool) api.CommandExecutor {
		return func(c api.CommandContext) (int, error) {
			w, err := b.senderWorld(c)
			if err != nil {
				return 0, err
			}
			from, to := c.BlockPos("from"), c.BlockPos("to")
			lo := api.BlockPos{X: min(from.X, to.X), Y: min(from.Y, to.Y), Z: min(from.Z, to.Z)}
			hi := api.BlockPos{X: max(from.X, to.X), Y: max(from.Y, to.Y), Z: max(from.Z, to.Z)}
			vol := int64(hi.X-lo.X+1) * int64(hi.Y-lo.Y+1) * int64(hi.Z-lo.Z+1)
			if limit := b.fillLimit(w); vol > limit {
				return 0, fail("Too many blocks in the specified area (maximum %d, specified %d)", limit, vol)
			}
			if err := b.checkPlaceable(w, lo); err != nil {
				return 0, err
			}
			if err := b.checkPlaceable(w, hi); err != nil {
				return 0, err
			}
			var chunks []chunkKey
			for cx := lo.X >> 4; cx <= hi.X>>4; cx++ {
				for cz := lo.Z >> 4; cz <= hi.Z>>4; cz++ {
					if !w.IsChunkLoaded(cx, cz) {
						return 0, fail("That position is not loaded")
					}
					chunks = append(chunks, chunkKey{cx, cz})
				}
			}
			st := c.BlockState("block")
			var filter *blockPredicate
			if filtered {
				v, _ := cctx(c).lookup("filter")
				filter = v.(*blockPredicate)
			}
			var air api.BlockState
			if reg := b.srv.Blocks(); reg != nil {
				air = reg.Air()
			}
			total := &counter{}
			b.forChunks(w, chunks, func(ck chunkKey) {
				n := 0
				for x := max(lo.X, ck.x<<4); x <= min(hi.X, ck.x<<4+15); x++ {
					for z := max(lo.Z, ck.z<<4); z <= min(hi.Z, ck.z<<4+15); z++ {
						for y := lo.Y; y <= hi.Y; y++ {
							pos := api.BlockPos{X: x, Y: y, Z: z}
							border := x == lo.X || x == hi.X || y == lo.Y || y == hi.Y || z == lo.Z || z == hi.Z
							target, m := st, mode
							switch mode {
							case placeOutline:
								if !border {
									continue
								}
								m = placeReplace
							case placeHollow:
								if !border {
									if air == nil {
										continue
									}
									target = air
								}
								m = placeReplace
							}
							if filter != nil && !filter.test(w.BlockState(pos)) {
								continue
							}
							if place(w, pos, target, m) {
								n++
							}
						}
					}
				}
				total.add(n)
			}, func() {
				if n := total.get(); n > 0 {
					b.success(c, textf("Successfully filled %d block(s)", n), true)
				} else {
					c.Sender().SendMessage(ErrorComponent(fail("No blocks were filled"), ""))
				}
			})
			return int(vol), nil
		}
	}
	return api.Literal("fill").Describe("Fills a region with a specific block").
		Then(api.Argument("from", api.BlockPosArg()).
			Then(api.Argument("to", api.BlockPosArg()).
				Then(api.Argument("block", api.BlockStateArg()).Executes(run(placeReplace, false)).Then(
					api.Literal("replace").Executes(run(placeReplace, false)).
						Then(api.Argument("filter", api.ArgumentType{Parser: ParserBlockPredicate}).Executes(run(placeReplace, true))),
					api.Literal("destroy").Executes(run(placeDestroy, false)),
					api.Literal("hollow").Executes(run(placeHollow, false)),
					api.Literal("outline").Executes(run(placeOutline, false)),
					api.Literal("keep").Executes(run(placeKeep, false)),
					api.Literal("strict").Executes(run(placeStrict, false)),
				))))
}

func (b *builtins) setWorldSpawn() *api.CommandNode {
	run := func(c api.CommandContext) (int, error) {
		x := cctx(c)
		w, err := b.senderWorld(c)
		if err != nil {
			return 0, err
		}
		origin := x.origin()
		pos := origin.Pos.BlockPos()
		if c.Has("pos") {
			pos = c.BlockPos("pos")
		}
		var yaw, pitch float32
		if v, ok := x.lookup("rotation"); ok {
			r := v.(*coords).raw(origin)
			yaw, pitch = float32(r.X), float32(r.Y)
		}
		loc := api.Location{World: w.Name(), Pos: api.Vec3{X: float64(pos.X) + 0.5, Y: float64(pos.Y), Z: float64(pos.Z) + 0.5}, Yaw: yaw, Pitch: pitch}
		b.onGlobal(func() { w.SetSpawn(loc) })
		b.success(c, textf("Set the world spawn point to %d, %d, %d [%s, %s] in %s", pos.X, pos.Y, pos.Z,
			fmtNum(float64(yaw)), fmtNum(float64(pitch)), w.Key().Normalize()), true)
		return 1, nil
	}
	return api.Literal("setworldspawn").Describe("Sets the world spawn").Executes(run).
		Then(api.Argument("pos", api.BlockPosArg()).Executes(run).
			Then(api.Argument("rotation", api.ArgumentType{Parser: api.ParserRotation}).Executes(run)))
}

// GameRules26_3 are the game rule names of Java 26.3 in registry order.
var GameRules26_3 = []string{
	"advance_time", "advance_weather", "allow_entering_nether_using_portals", "block_drops",
	"block_explosion_drop_decay", "command_blocks_work", "command_block_output", "drowning_damage",
	"elytra_movement_check", "ender_pearls_vanish_on_death", "entity_drops", "fall_damage", "fire_damage",
	"fire_spread_radius_around_player", "forgive_dead_players", "freeze_damage", "global_sound_events",
	"immediate_respawn", "keep_inventory", "lava_source_conversion", "limited_crafting", "locator_bar",
	"log_admin_commands", "max_block_modifications", "max_command_forks", "max_command_sequence_length",
	"max_entity_cramming", "max_minecart_speed", "max_snow_accumulation_height", "mob_drops",
	"mob_explosion_drop_decay", "mob_griefing", "natural_health_regeneration", "player_movement_check",
	"players_nether_portal_creative_delay", "players_nether_portal_default_delay", "players_sleeping_percentage",
	"projectiles_can_break_blocks", "pvp", "raids", "random_tick_speed", "reduced_debug_info", "respawn_radius",
	"send_command_feedback", "show_advancement_messages", "show_death_messages", "spawner_blocks_work",
	"spawn_mobs", "spawn_monsters", "spawn_patrols", "spawn_phantoms", "spawn_wandering_traders",
	"spawn_wardens", "spectators_generate_chunks", "spread_vines", "tnt_explodes", "tnt_explosion_drop_decay",
	"universal_anger", "water_source_conversion",
}

func (b *builtins) gamerule() *api.CommandNode {
	ruleName := func(c api.CommandContext) string { return api.Key(c.String("rule")).Path() }
	rules := suggestStrings(func(c api.CommandContext) []string {
		w := senderWorld(b.srv, c.Sender())
		var out []string
		for _, r := range GameRules26_3 {
			if w == nil {
				out = append(out, r)
			} else if _, ok := w.GameRule(r); ok {
				out = append(out, r)
			}
		}
		return out
	})
	query := func(c api.CommandContext) (int, error) {
		w, err := b.senderWorld(c)
		if err != nil {
			return 0, err
		}
		rule := ruleName(c)
		v, ok := w.GameRule(rule)
		if !ok {
			return 0, fail("Unknown game rule: %s", rule)
		}
		c.Sender().SendMessage(textf("Gamerule %s is currently set to: %s", rule, v))
		n, _ := strconv.Atoi(v)
		if v == "true" {
			n = 1
		}
		return n, nil
	}
	set := func(c api.CommandContext) (int, error) {
		w, err := b.senderWorld(c)
		if err != nil {
			return 0, err
		}
		rule, value := ruleName(c), c.String("value")
		if _, ok := w.GameRule(rule); !ok {
			return 0, fail("Unknown game rule: %s", rule)
		}
		if !w.SetGameRule(rule, value) {
			return 0, fail("Invalid value '%s' for game rule %s", value, rule)
		}
		for _, o := range b.srv.Worlds() {
			if o.Name() != w.Name() {
				o.SetGameRule(rule, value)
			}
		}
		b.success(c, textf("Gamerule %s is now set to: %s", rule, value), true)
		return 1, nil
	}
	return api.Literal("gamerule").Describe("Sets or queries a game rule value").
		Then(api.Argument("rule", api.ResourceArg()).Suggests(rules).Executes(query).
			Then(api.Argument("value", api.WordArg()).Executes(set)))
}

func (b *builtins) seed() *api.CommandNode {
	return api.Literal("seed").Describe("Displays the world seed").Executes(func(c api.CommandContext) (int, error) {
		w, err := b.senderWorld(c)
		if err != nil {
			return 0, err
		}
		s := strconv.FormatInt(w.Seed(), 10)
		val := api.Colored(s, "green")
		val.Click = &api.ClickEvent{Action: api.ClickCopyToClipboard, Value: s}
		hover := api.Text("Click to Copy to Clipboard")
		val.Hover = &api.HoverEvent{Text: &hover}
		c.Sender().SendMessage(api.Text("Seed: ").Append(api.Text("["), val, api.Text("]")))
		return int(int32(w.Seed())), nil
	})
}
