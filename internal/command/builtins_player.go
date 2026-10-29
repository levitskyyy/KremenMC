package command

import (
	"fmt"
	"math"

	"github.com/levitskyyy/KremenMC/api"
)

func (b *builtins) teleport() *api.CommandNode {
	toPos := func(withRot, facing, facingEntity bool) api.CommandExecutor {
		return func(c api.CommandContext) (int, error) {
			targets, err := b.entities(c, "targets")
			if err != nil {
				return 0, err
			}
			x := cctx(c)
			origin := x.origin()
			pos := c.Vec3("location")
			var rot *coords
			if withRot {
				r, _ := x.lookup("rotation")
				rot = r.(*coords)
			}
			var faceAt *api.Vec3
			if facing {
				var at api.Vec3
				if facingEntity {
					fe, err := b.entities(c, "facingEntity")
					if err != nil {
						return 0, err
					}
					at = fe[0].Location().Pos
					if c.String("facingAnchor") == "eyes" {
						if le, ok := fe[0].(api.LivingEntity); ok {
							at = le.EyeLocation().Pos
						}
					}
				} else {
					at = c.Vec3("facingLocation")
				}
				faceAt = &at
			}
			world := origin.World
			if world == "" {
				if w := x.world(); w != nil {
					world = w.Name()
				}
			}
			b.forEntities(targets, func(e api.Entity) {
				cur := e.Location()
				to := api.Location{World: world, Pos: pos, Yaw: cur.Yaw, Pitch: cur.Pitch}
				if rot != nil {
					r := rot.raw(api.Location{Yaw: cur.Yaw, Pitch: cur.Pitch})
					to.Yaw, to.Pitch = float32(r.X), float32(r.Y)
				}
				if faceAt != nil {
					to.Yaw, to.Pitch = lookAt(pos, *faceAt)
				}
				e.Teleport(to, api.TeleportCommand)
			}, nil)
			if len(targets) == 1 {
				b.success(c, textf("Teleported %s to %f, %f, %f", entityName(targets[0]), pos.X, pos.Y, pos.Z), true)
			} else {
				b.success(c, textf("Teleported %d entities to %f, %f, %f", len(targets), pos.X, pos.Y, pos.Z), true)
			}
			return len(targets), nil
		}
	}
	toEntity := func(c api.CommandContext) (int, error) {
		targets, err := b.entities(c, "targets")
		if err != nil {
			return 0, err
		}
		dest, err := b.entities(c, "destination")
		if err != nil {
			return 0, err
		}
		to := dest[0].Location()
		if w := dest[0].World(); w != nil && to.World == "" {
			to.World = w.Name()
		}
		b.forEntities(targets, func(e api.Entity) { e.Teleport(to, api.TeleportCommand) }, nil)
		if len(targets) == 1 {
			b.success(c, textf("Teleported %s to %s", entityName(targets[0]), entityName(dest[0])), true)
		} else {
			b.success(c, textf("Teleported %d entities to %s", len(targets), entityName(dest[0])), true)
		}
		return len(targets), nil
	}
	return api.Literal("teleport").Describe("Teleports entities").Then(
		api.Argument("location", api.Vec3Arg()).Executes(toPos(false, false, false)),
		api.Argument("destination", api.ArgumentType{Parser: api.ParserEntity, Single: true}).Executes(toEntity),
		api.Argument("targets", api.EntitiesArg()).Then(
			api.Argument("location", api.Vec3Arg()).Executes(toPos(false, false, false)).Then(
				api.Argument("rotation", api.ArgumentType{Parser: api.ParserRotation}).Executes(toPos(true, false, false)),
				api.Literal("facing").Then(
					api.Literal("entity").Then(
						api.Argument("facingEntity", api.ArgumentType{Parser: api.ParserEntity, Single: true}).Executes(toPos(false, true, true)).
							Then(api.Argument("facingAnchor", api.ArgumentType{Parser: ParserEntityAnchor}).Executes(toPos(false, true, true))),
					),
					api.Argument("facingLocation", api.Vec3Arg()).Executes(toPos(false, true, false)),
				),
			),
			api.Argument("destination", api.ArgumentType{Parser: api.ParserEntity, Single: true}).Executes(toEntity),
		),
	)
}

func lookAt(from, to api.Vec3) (yaw, pitch float32) {
	dx, dy, dz := to.X-from.X, to.Y-from.Y, to.Z-from.Z
	h := math.Sqrt(dx*dx + dz*dz)
	yaw = float32(wrapDegrees(math.Atan2(dz, dx)*180/math.Pi - 90))
	pitch = float32(wrapDegrees(-(math.Atan2(dy, h) * 180 / math.Pi)))
	return
}

func wrapDegrees(v float64) float64 {
	v = math.Mod(v, 360)
	if v >= 180 {
		v -= 360
	}
	if v < -180 {
		v += 360
	}
	return v
}

func (b *builtins) gamemode() *api.CommandNode {
	run := func(c api.CommandContext) (int, error) {
		ps, err := b.players(c, "target")
		if err != nil {
			return 0, err
		}
		v, _ := c.Arg("gamemode")
		mode := v.(api.GameMode)
		n := 0
		for _, p := range ps {
			if p.GameMode() == mode {
				continue
			}
			n++
			b.forEntities([]api.Entity{p}, func(api.Entity) { p.SetGameMode(mode) }, nil)
			if sameSender(p, c.Sender()) {
				b.success(c, textf("Set own game mode to %s", modeName(mode)), true)
				continue
			}
			p.SendMessage(textf("Your game mode has been updated to %s", modeName(mode)))
			b.success(c, textf("Set %s's game mode to %s", p.Name(), modeName(mode)), true)
		}
		return n, nil
	}
	return api.Literal("gamemode").Describe("Sets a player's game mode").
		Then(api.Argument("gamemode", api.GameModeArg()).Executes(run).
			Then(api.Argument("target", api.PlayersArg()).Executes(run)))
}

func (b *builtins) defaultGamemode() *api.CommandNode {
	return api.Literal("defaultgamemode").Describe("Sets the default game mode").
		Then(api.Argument("gamemode", api.GameModeArg()).Executes(func(c api.CommandContext) (int, error) {
			s, ok := b.srv.(DefaultGameModeServer)
			if !ok {
				return 0, fail("Not supported by this server")
			}
			v, _ := c.Arg("gamemode")
			mode := v.(api.GameMode)
			if s.DefaultGameMode() == mode {
				return 0, fail("Nothing changed. The default game mode is already %s", modeName(mode))
			}
			s.SetDefaultGameMode(mode)
			b.success(c, textf("The default game mode is now %s", modeName(mode)), true)
			return 1, nil
		}))
}

func (b *builtins) itemName(k api.Key) string { return titleCase(k.Path()) }

func (b *builtins) give() *api.CommandNode {
	run := func(c api.CommandContext) (int, error) {
		ps, err := b.players(c, "targets")
		if err != nil {
			return 0, err
		}
		item := c.Item("item")
		count := int64(1)
		if c.Has("count") {
			count = c.Int("count")
		}
		maxStack := int64(64)
		if items := b.srv.Items(); items != nil {
			if m := items.MaxStackSize(item.Type); m > 0 {
				maxStack = int64(m)
			}
		}
		if count > maxStack*100 {
			return 0, fail("Can't give more than %d of %s", maxStack*100, b.itemName(item.Type))
		}
		es := make([]api.Entity, len(ps))
		for i, p := range ps {
			es[i] = p
		}
		b.forEntities(es, func(e api.Entity) {
			p := e.(api.Player)
			inv := p.Inventory()
			for left := count; left > 0; {
				n := min(left, maxStack)
				left -= n
				stack := item
				stack.Count = int32(n)
				var rest []api.ItemStack
				if inv != nil {
					rest = inv.AddItem(stack)
				} else {
					rest = []api.ItemStack{stack}
				}
				for _, r := range rest {
					if w := p.World(); w != nil && !r.IsEmpty() {
						_, _ = w.DropItem(p.Location().Pos, r)
					}
				}
			}
		}, nil)
		name := b.itemName(item.Type)
		if len(ps) == 1 {
			b.success(c, textf("Gave %d [%s] to %s", count, name, ps[0].Name()), true)
		} else {
			b.success(c, textf("Gave %d [%s] to %d players", count, name, len(ps)), true)
		}
		return len(ps), nil
	}
	return api.Literal("give").Describe("Gives an item to a player").
		Then(api.Argument("targets", api.PlayersArg()).
			Then(api.Argument("item", api.ItemArg()).Executes(run).
				Then(api.Argument("count", api.ArgumentType{Parser: api.ParserInteger, Min: 1, HasMin: true}).Executes(run))))
}

func (b *builtins) clear() *api.CommandNode {
	run := func(c api.CommandContext) (int, error) {
		ps, err := b.players(c, "targets")
		if err != nil {
			return 0, err
		}
		pred := &itemPredicate{any: true}
		if v, ok := cctx(c).lookup("item"); ok {
			pred = v.(*itemPredicate)
		}
		maxCount := int64(-1)
		if c.Has("maxCount") {
			maxCount = c.Int("maxCount")
		}
		es := make([]api.Entity, len(ps))
		for i, p := range ps {
			es[i] = p
		}
		total := &counter{}
		b.forEntities(es, func(e api.Entity) {
			total.add(clearInventory(e.(api.Player).Inventory(), pred, maxCount))
		}, func() {
			n := total.get()
			sender := c.Sender()
			switch {
			case n == 0 && len(ps) == 1:
				sender.SendMessage(ErrorComponent(fail("No items were found on player %s", ps[0].Name()), ""))
			case n == 0:
				sender.SendMessage(ErrorComponent(fail("No items were found on %d players", len(ps)), ""))
			case maxCount == 0 && len(ps) == 1:
				b.success(c, textf("Found %d matching item(s) on player %s", n, ps[0].Name()), true)
			case maxCount == 0:
				b.success(c, textf("Found %d matching item(s) on %d players", n, len(ps)), true)
			case len(ps) == 1:
				b.success(c, textf("Removed %d item(s) from player %s", n, ps[0].Name()), true)
			default:
				b.success(c, textf("Removed %d item(s) from %d players", n, len(ps)), true)
			}
		})
		return len(ps), nil
	}
	return api.Literal("clear").Describe("Clears items from player inventory").Executes(run).
		Then(api.Argument("targets", api.PlayersArg()).Executes(run).
			Then(api.Argument("item", api.ArgumentType{Parser: ParserItemPredicate}).Executes(run).
				Then(api.Argument("maxCount", api.ArgumentType{Parser: api.ParserInteger, Min: 0, HasMin: true}).Executes(run))))
}

// clearInventory removes up to maxCount matching items (all when negative,
// none when zero) and returns how many matched or were removed.
func clearInventory(inv api.PlayerInventory, pred *itemPredicate, maxCount int64) int {
	if inv == nil {
		return 0
	}
	n := 0
	for slot, it := range inv.Contents() {
		if !pred.test(it) {
			continue
		}
		take := int64(it.Count)
		if maxCount > 0 {
			take = min(take, maxCount-int64(n))
		}
		if maxCount == 0 {
			n += int(take)
			continue
		}
		if take <= 0 {
			break
		}
		it.Count -= int32(take)
		if it.Count <= 0 {
			it = api.ItemStack{}
		}
		inv.SetItem(slot, it)
		n += int(take)
	}
	if cur := inv.Cursor(); pred.test(cur) && maxCount != 0 && (maxCount < 0 || int64(n) < maxCount) {
		take := int64(cur.Count)
		if maxCount > 0 {
			take = min(take, maxCount-int64(n))
		}
		cur.Count -= int32(take)
		if cur.Count <= 0 {
			cur = api.ItemStack{}
		}
		inv.SetCursor(cur)
		n += int(take)
	}
	return n
}

func (b *builtins) kill() *api.CommandNode {
	run := func(c api.CommandContext) (int, error) {
		es, err := b.entities(c, "targets")
		if err != nil {
			return 0, err
		}
		b.forEntities(es, func(e api.Entity) {
			if l, ok := e.(api.LivingEntity); ok {
				l.Damage(math.MaxFloat32, api.DamageSource{Type: "minecraft:generic_kill"})
				return
			}
			e.Remove()
		}, nil)
		if len(es) == 1 {
			b.success(c, textf("Killed %s", entityName(es[0])), true)
		} else {
			b.success(c, textf("Killed %d entities", len(es)), true)
		}
		return len(es), nil
	}
	return api.Literal("kill").Describe("Kills entities").Executes(run).
		Then(api.Argument("targets", api.EntitiesArg()).Executes(run))
}

func (b *builtins) spawnpoint() *api.CommandNode {
	run := func(c api.CommandContext) (int, error) {
		ps, err := b.players(c, "targets")
		if err != nil {
			return 0, err
		}
		x := cctx(c)
		origin := x.origin()
		pos := origin.Pos.BlockPos()
		if c.Has("pos") {
			pos = c.BlockPos("pos")
		}
		yaw := float32(0)
		if c.Has("angle") {
			yaw = float32(c.Float("angle"))
		}
		w := x.world()
		if w == nil {
			return 0, fail("No world is loaded")
		}
		loc := api.Location{World: w.Name(), Pos: api.Vec3{X: float64(pos.X) + 0.5, Y: float64(pos.Y), Z: float64(pos.Z) + 0.5}, Yaw: yaw}
		for _, p := range ps {
			p.SetRespawnLocation(&loc)
		}
		where := fmt.Sprintf("%d, %d, %d [%s] in %s", pos.X, pos.Y, pos.Z, fmtNum(float64(yaw)), w.Key().Normalize())
		if len(ps) == 1 {
			b.success(c, textf("Set spawn point to %s for %s", where, ps[0].Name()), true)
		} else {
			b.success(c, textf("Set spawn point to %s for %d players", where, len(ps)), true)
		}
		return len(ps), nil
	}
	return api.Literal("spawnpoint").Describe("Sets the spawn point for a player").Executes(run).
		Then(api.Argument("targets", api.PlayersArg()).Executes(run).
			Then(api.Argument("pos", api.BlockPosArg()).Executes(run).
				Then(api.Argument("angle", api.ArgumentType{Parser: ParserAngle}).Executes(run))))
}
