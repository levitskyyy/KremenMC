package server

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/bridge"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

const (
	maxMoveSq          = 100.0
	reachSq            = 7.0 * 7.0
	digStart           = 0
	digChangeDirection = 1
	digAbort           = 2
	digFinish          = 3
	swingWhack         = 1
	swingTicks         = 6
	levelBreak         = 2001
	maxChatLen         = 256
	inputShift         = packets.ServerboundPlayerInputInputsShift
	cmdSprint          = 1
	cmdUnsprint        = 2
)

// process handles every queued packet; runs in the player phase.
func (p *Player) process() {
	in := p.drain()
	for _, pk := range in {
		if p.quit.Load() {
			break
		}
		p.handle(pk)
	}
	p.recycle(in)
	p.tasks.run(p.s.CurrentTick())
	p.digTick()
	p.dataTick()
	p.tickGameplay()
	p.tickCombat()
	p.mu.Lock()
	p.ticks++
	p.mu.Unlock()
}

func (p *Player) handle(pk packets.Packet) {
	switch m := pk.(type) {
	case *packets.ServerboundAcceptTeleportation:
		p.mu.Lock()
		if m.TeleportID == p.pendingTP {
			p.pendingTP = 0
		}
		p.mu.Unlock()
	case *packets.ServerboundMovePlayerPos:
		p.move(&api.Vec3{X: m.X, Y: m.Y, Z: m.Z}, nil, m.Flags)
	case *packets.ServerboundMovePlayerPosRot:
		p.move(&api.Vec3{X: m.X, Y: m.Y, Z: m.Z}, &[2]float32{m.Yaw, m.Pitch}, m.Flags)
	case *packets.ServerboundMovePlayerRot:
		p.move(nil, &[2]float32{m.Yaw, m.Pitch}, m.Flags)
	case *packets.ServerboundMovePlayerStatusOnly:
		p.move(nil, nil, m.Flags)
	case *packets.ServerboundChunkBatchReceived:
		p.unacked = max(p.unacked-1, 0)
		v := m.ChunksPerTick
		if v != v {
			v = 0.01
		}
		p.cpt = min(max(v, 0.01), 64)
		if p.unacked == 0 {
			p.quota = 1
		}
		p.maxUnacked = 10
	case *packets.ServerboundPlayerLoaded:
		p.loaded = true
	case *packets.ServerboundChat:
		p.Chat(m.Message)
	case *packets.ServerboundChatCommand:
		p.command(m.Command)
	case *packets.ServerboundChatCommandSigned:
		p.command(m.Command)
	case *packets.ServerboundCommandSuggestion:
		p.suggest(m.TransactionID, m.Text)
	case *packets.ServerboundPlayerAction:
		p.dig(m)
	case *packets.ServerboundUseItemOn:
		p.useItemOn(m)
	case *packets.ServerboundUseItem:
		p.handleUseItem(m)
	case *packets.ServerboundPunch:
		p.swing(0)
	case *packets.ServerboundSetCarriedItem:
		p.setSelected(int32(m.SlotID))
	case *packets.ServerboundSetCreativeModeSlot:
		p.handleCreativeSlot(m)
	case *packets.ServerboundContainerClick:
		p.handleContainerClick(m)
	case *packets.ServerboundContainerClose:
		p.handleContainerClose(m)
	case *packets.ServerboundPickItemFromBlock:
		p.handlePickBlock(m)
	case *packets.ServerboundPickItemFromEntity:
		p.handlePickEntity(m)
	case *packets.ServerboundPlayerInput:
		p.mu.Lock()
		sneak := m.Inputs&inputShift != 0
		if sneak != p.sneaking {
			p.sneaking, p.metaDirty = sneak, true
		}
		p.mu.Unlock()
	case *packets.ServerboundPlayerCommand:
		p.mu.Lock()
		switch m.ActionID {
		case cmdSprint:
			p.sprinting, p.metaDirty = true, true
		case cmdUnsprint:
			p.sprinting, p.metaDirty = false, true
		}
		p.mu.Unlock()
	case *packets.ServerboundPlayerAbilities:
		p.mu.Lock()
		p.flying = m.Flags&0x02 != 0 && p.allowFlight
		p.mu.Unlock()
	case *packets.ServerboundChangeGameMode:
		if p.HasPermission("minecraft.command.gamemode") && m.Mode >= 0 && m.Mode <= 3 {
			p.SetGameMode(api.GameMode(m.Mode))
		}
	case *packets.ServerboundClientCommand:
		if m.ActionID == 0 {
			p.performRespawn()
		}
	case *packets.ServerboundPingRequest:
		p.send(&packets.ClientboundPongResponse{Payload: m.Payload})
	case *packets.ServerboundInteract:
		p.interactEntity(m.Target, api.Hand(m.Hand), false, api.Vec3{X: m.Location.X, Y: m.Location.Y, Z: m.Location.Z})
	case *packets.ServerboundAttack:
		p.swing(0)
		p.interactEntity(m.EntityID, api.MainHand, true, api.Vec3{})
	case *packets.ServerboundCustomPayload:
		if p.s.bus.HasHandlers(api.KindPluginMessage) {
			p.s.bus.Dispatch(&api.PluginMessageEvent{Player: p, Channel: api.Key(m.Channel), Data: m.Data})
		}
	case *packets.ServerboundClientInformation:
		p.mu.Lock()
		p.metaDirty = true
		p.mu.Unlock()
	}
}

func (p *Player) move(pos *api.Vec3, rot *[2]float32, flags packets.MovementFlags) {
	w := p.world.Load()
	p.mu.Lock()
	if p.pendingTP != 0 {
		p.mu.Unlock()
		return
	}
	from := p.locationLocked()
	to := from
	if pos != nil {
		to.Pos = *pos
	}
	if rot != nil {
		to.Yaw, to.Pitch = rot[0], max(min(rot[1], 90), -90)
	}
	spectator := p.gm == api.Spectator
	flying := p.flying
	verdict := entity.MoveOK
	if pos != nil {
		verdict = entity.CheckPlayerMove(w.w, p.e, to.Pos, maxMoveSq, spectator)
	}
	p.mu.Unlock()
	if verdict != entity.MoveOK {
		if verdict == entity.MoveInvalid {
			p.Kick(api.Translatable("multiplayer.disconnect.invalid_player_movement"))
			return
		}
		p.s.log.Debug("rejected move", "player", p.prof.Name, "verdict", verdict.String())
		p.teleport(from)
		return
	}
	if from.Pos != to.Pos || from.Yaw != to.Yaw || from.Pitch != to.Pitch {
		if p.s.bus.HasHandlers(api.KindPlayerMove) {
			ev := &api.PlayerMoveEvent{Player: p, From: from, To: to}
			p.s.bus.Dispatch(ev)
			if ev.Cancelled() {
				p.teleport(from)
				return
			}
			if ev.To != to {
				p.teleport(ev.To)
				return
			}
		}
		if b := p.s.javaBridge(); b != nil && from.Pos != to.Pos && b.HasListeners(bridge.EventMove) {
			cancelled, mut := b.FireEvent(context.Background(), bridge.Event{Name: bridge.EventMove, Fields: map[string]any{
				"player": bridge.UUID(p.prof.UUID), "from": bridgeLoc(from).Map(), "to": bridgeLoc(to).Map(),
			}})
			if cancelled {
				p.teleport(from)
				return
			}
			if m, ok := mut["to"].(map[string]any); ok {
				l := bridge.LocationFromMap(m)
				p.teleport(api.Location{World: to.World, Pos: api.Vec3{X: l.X, Y: l.Y, Z: l.Z}, Yaw: l.Yaw, Pitch: l.Pitch})
				return
			}
		}
	}
	var fell float64
	p.mu.Lock()
	if pos != nil {
		fell = p.e.ApplyPlayerMove(to.Pos, flags&packets.MovementFlagsOnGround != 0, flying || spectator)
	} else {
		p.e.OnGround = flags&packets.MovementFlagsOnGround != 0
	}
	p.e.Yaw, p.e.Pitch, p.e.HeadYaw = to.Yaw, to.Pitch, to.Yaw
	p.mu.Unlock()
	p.onMoved(from.Pos, to.Pos, flags&packets.MovementFlagsOnGround != 0)
	if pos != nil {
		p.resetFallInFluid()
		p.onPlayerLanded(fell)
	}
}

func bridgeLoc(l api.Location) bridge.Location {
	return bridge.Location{World: l.World, X: l.Pos.X, Y: l.Pos.Y, Z: l.Pos.Z, Yaw: l.Yaw, Pitch: l.Pitch}
}

func (p *Player) chatLoop() {
	for msg := range p.chatCh {
		p.chat(msg)
	}
}

func validChat(s string) bool {
	if s == "" || utf8.RuneCountInString(s) > maxChatLen {
		return false
	}
	for _, r := range s {
		if r == '§' || r < ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

// chat runs on the player's chat goroutine; chat events are asynchronous.
func (p *Player) chat(msg string) {
	if !validChat(msg) {
		p.Kick(api.Translatable("multiplayer.disconnect.illegal_characters"))
		return
	}
	s := p.s
	recipients := make([]api.Player, 0, 16)
	for _, o := range s.onlinePlayers() {
		recipients = append(recipients, o)
	}
	var render func(api.Player, api.Component, api.Component, api.Audience) api.Component
	if s.bus.HasHandlers(api.KindPlayerChat) {
		ev := &api.PlayerChatEvent{Player: p, Message: msg, Recipients: recipients}
		s.bus.Dispatch(ev)
		if ev.Cancelled() {
			return
		}
		msg, recipients, render = ev.Message, ev.Recipients, ev.Renderer
	}
	body := api.Text(msg)
	var rendered *api.Component
	if b := s.javaBridge(); b != nil && b.HasListeners(bridge.EventChat) {
		cancelled, mut := b.FireEvent(context.Background(), bridge.Event{Name: bridge.EventChat, Async: true, Fields: map[string]any{
			"player": bridge.UUID(p.prof.UUID), "message": msg,
		}})
		if cancelled {
			return
		}
		if j, ok := mut["message"].(string); ok {
			body = componentFromJSON(j)
		}
		if j, ok := mut["rendered"].(string); ok {
			c := componentFromJSON(j)
			rendered = &c
		}
	}
	name := p.DisplayName()
	line := api.Translatable("chat.type.text", name, body)
	if rendered != nil {
		line = *rendered
	}
	var shared []byte
	if render == nil {
		shared = systemChat(line, false)
	}
	for _, r := range recipients {
		rp, ok := r.(*Player)
		if !ok {
			continue
		}
		if render != nil {
			rp.c.SendRaw(systemChat(render(p, name, body, rp), false))
		} else {
			rp.c.SendRaw(shared)
		}
	}
	s.log.Info(line.Plain(), "source", "chat")
}

func (p *Player) command(line string) {
	s := p.s
	if s.bus.HasHandlers(api.KindPlayerCommand) {
		ev := &api.PlayerCommandEvent{Player: p, Line: line}
		s.bus.Dispatch(ev)
		if ev.Cancelled() {
			return
		}
		line = ev.Line
	}
	if b := s.javaBridge(); b != nil && b.HasListeners(bridge.EventCommandPreprocess) {
		cancelled, mut := b.FireEvent(context.Background(), bridge.Event{Name: bridge.EventCommandPreprocess, Fields: map[string]any{
			"player": bridge.UUID(p.prof.UUID), "message": "/" + line,
		}})
		if cancelled {
			return
		}
		if m, ok := mut["message"].(string); ok {
			line = strings.TrimPrefix(m, "/")
		}
	}
	s.log.Info(p.prof.Name+" issued server command: /"+line, "source", "command")
	s.cmds.Dispatch(p, line)
}

func (p *Player) suggest(tx int32, text string) {
	c := p.s.cmds.Suggest(p, text)
	resp := &packets.ClientboundCommandSuggestions{TransactionID: tx, Start: int32(utf16Len(text[:min(c.Start, len(text))])), Length: int32(utf16Len(text[min(c.Start, len(text)):min(c.Start+c.Length, len(text))]))}
	for _, sg := range c.Suggestions {
		m := packets.ClientboundCommandSuggestionsMatch{Match: sg.Text}
		if sg.Tooltip != nil {
			m.Tooltip = proto.Some(toText(*sg.Tooltip))
		}
		resp.Matches = append(resp.Matches, m)
	}
	p.send(resp)
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func (p *Player) ack(seq int32) {
	if seq > p.ackSeq {
		p.ackSeq = seq
	}
}

// flushAck sends the block change acknowledgement after the tick's block
// updates went out, so client predictions settle on server state.
func (p *Player) flushAck() {
	if p.ackSeq >= 0 {
		p.send(&packets.ClientboundBlockChangedAck{SequenceID: p.ackSeq})
		p.ackSeq = -1
	}
}

func (p *Player) swing(hand int32) {
	raw := encode(&packets.ClientboundSwingAnimation{EntityID: p.e.ID, Hand: hand, Animation: swingWhack, Duration: swingTicks})
	p.mu.Lock()
	p.extra = append(p.extra, raw)
	p.mu.Unlock()
}

func toAPIPos(b proto.BlockPos) api.BlockPos {
	return api.BlockPos{X: int(b.X), Y: int(b.Y), Z: int(b.Z)}
}

func (p *Player) inReach(b api.BlockPos) bool {
	eye := p.EyeLocation().Pos
	return eye.DistanceSq(b.Center()) <= reachSq
}

func (p *Player) resync(w *World, pos api.BlockPos) {
	p.SendBlockChange(pos, blockState(w.w.Block(pos.X, pos.Y, pos.Z)))
}

func (p *Player) dig(m *packets.ServerboundPlayerAction) {
	w := p.world.Load()
	pos := toAPIPos(m.Location)
	switch m.Status {
	case digStart, digChangeDirection, digAbort, digFinish:
	case actionDropAll, actionDrop:
		p.handleDrop(m.Status == actionDropAll)
		return
	case actionRelease:
		p.releaseUsing()
		return
	case actionSwapHands:
		p.handleSwapOffhand()
		return
	default:
		p.ack(m.Sequence)
		return
	}
	defer p.ack(m.Sequence)
	p.digAction(w, pos, m.Status, int32(m.Face))
}

// interact fires PlayerInteractEvent; it reports whether the action was denied.
func (p *Player) interact(action api.InteractAction, b *block, face api.BlockFace, hand api.Hand, cursor api.Vec3) bool {
	s := p.s
	denied := false
	if s.bus.HasHandlers(api.KindPlayerInteract) {
		ev := &api.PlayerInteractEvent{Player: p, Action: action, Face: face, Hand: hand, Item: p.handItem(hand), Cursor: cursor}
		if b != nil {
			ev.Block = b
		}
		denied = s.bus.Dispatch(ev).(*api.PlayerInteractEvent).Cancelled()
	}
	if br := s.javaBridge(); br != nil && br.HasListeners(bridge.EventInteract) {
		actions := [...]string{"LEFT_CLICK_AIR", "LEFT_CLICK_BLOCK", "RIGHT_CLICK_AIR", "RIGHT_CLICK_BLOCK", "PHYSICAL"}
		faces := [...]string{"DOWN", "UP", "NORTH", "SOUTH", "WEST", "EAST", "SELF"}
		hands := [...]string{"HAND", "OFF_HAND"}
		f := map[string]any{"player": bridge.UUID(p.prof.UUID), "action": actions[action], "face": faces[min(int(face), 6)], "hand": hands[min(int(hand), 1)]}
		if b != nil {
			f["block"] = map[string]any{"world": b.w.name, "x": b.pos.X, "y": b.pos.Y, "z": b.pos.Z, "block": b.State().String()}
		}
		if cancelled, _ := br.FireEvent(context.Background(), bridge.Event{Name: bridge.EventInteract, Fields: f}); cancelled {
			denied = true
		}
	}
	return denied
}

func (p *Player) handItem(h api.Hand) api.ItemStack { return p.Inventory().ItemInHand(h) }

var faceOffsets = [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

func (p *Player) useItemOn(m *packets.ServerboundUseItemOn) {
	defer p.ack(m.Sequence)
	w := p.world.Load()
	clicked := toAPIPos(m.Location)
	if m.Direction < 0 || m.Direction > 5 {
		return
	}
	hand := api.Hand(m.Hand)
	face := api.BlockFace(m.Direction)
	cursor := api.Vec3{X: float64(m.CursorX), Y: float64(m.CursorY), Z: float64(m.CursorZ)}
	gm := p.GameMode()
	if !p.inReach(clicked) {
		p.resync(w, clicked)
		return
	}
	p.swing(int32(hand))
	if p.interact(api.RightClickBlock, &block{w: w, pos: clicked}, face, hand, cursor) {
		p.resync(w, clicked)
		return
	}
	if gm != api.Spectator && (!p.Sneaking() || p.Hand(api.MainHand).IsEmpty() && p.Hand(api.OffHand).IsEmpty()) && (p.openChest(w, clicked) || p.useBlock(w, clicked)) {
		return
	}
	if gm == api.Spectator || gm == api.Adventure {
		p.resync(w, clicked)
		return
	}
	held := p.Hand(hand)
	if held.IsEmpty() {
		return
	}
	if p.useEyeOnFrame(w, clicked, hand, held) || p.igniteBlock(w, clicked, face, hand, held) {
		return
	}
	item := stackToAPI(held)
	o := faceOffsets[face]
	if p.spawnEgg(w, item, clicked.Add(o[0], o[1], o[2]), hand) {
		return
	}
	target, old, placed, upperPos, upper, ok := p.placeState(w, string(item.Type), clicked, face, cursor)
	if !ok {
		p.resync(w, target)
		return
	}
	if p.obstructed(w, target, placed) {
		p.resync(w, target)
		return
	}
	s := p.s
	if s.bus.HasHandlers(api.KindBlockPlace) {
		ev := &api.BlockPlaceEvent{Player: p, Block: &block{w: w, pos: target}, Placed: blockState(placed), Replaced: blockState(old), Against: clicked, Hand: hand, Item: item, CanBuild: true}
		s.bus.Dispatch(ev)
		if ev.Cancelled() || !ev.CanBuild {
			p.resync(w, target)
			return
		}
	}
	if b := s.javaBridge(); b != nil && b.HasListeners(bridge.EventBlockPlace) {
		hands := [...]string{"HAND", "OFF_HAND"}
		cancelled, mut := b.FireEvent(context.Background(), bridge.Event{Name: bridge.EventBlockPlace, Fields: map[string]any{
			"player": bridge.UUID(p.prof.UUID), "world": w.name, "x": target.X, "y": target.Y, "z": target.Z,
			"block": data.StateString(placed), "replaced": data.StateString(old),
			"against": map[string]any{"x": clicked.X, "y": clicked.Y, "z": clicked.Z, "block": data.StateString(w.w.Block(clicked.X, clicked.Y, clicked.Z))},
			"hand":    hands[min(int(hand), 1)],
		}})
		if build, ok := mut["build"].(bool); cancelled || ok && !build {
			p.resync(w, target)
			return
		}
	}
	w.w.SetBlock(target.X, target.Y, target.Z, placed)
	w.placeSound(target, placed, p)
	if upper != 0 {
		w.w.SetBlock(upperPos.X, upperPos.Y, upperPos.Z, upper)
	}
	if gm == api.Survival {
		p.inv.mu.Lock()
		if cur := p.inv.hand(hand); sameItem(cur, held) {
			cur.Shrink(1)
			p.inv.setHand(hand, cur)
		}
		p.inv.mu.Unlock()
	}
}

// obstructed reports whether placing st at pos would intersect a player.
func (p *Player) obstructed(w *World, pos api.BlockPos, st data.StateID) bool {
	shapes := data.Collision(st)
	if len(shapes) == 0 {
		return false
	}
	for _, o := range w.playerList() {
		if o.GameMode() == api.Spectator {
			continue
		}
		mn, mx := o.BoundingBox()
		box := entity.AABB{MinX: mn.X, MinY: mn.Y, MinZ: mn.Z, MaxX: mx.X, MaxY: mx.Y, MaxZ: mx.Z}
		for _, s := range shapes {
			b := entity.AABB{MinX: float64(pos.X) + s.MinX, MinY: float64(pos.Y) + s.MinY, MinZ: float64(pos.Z) + s.MinZ,
				MaxX: float64(pos.X) + s.MaxX, MaxY: float64(pos.Y) + s.MaxY, MaxZ: float64(pos.Z) + s.MaxZ}
			if b.Intersects(box) {
				return true
			}
		}
	}
	return false
}

var horizontal = [4]string{"south", "west", "north", "east"}
var faceNames = [6]string{"down", "up", "north", "south", "west", "east"}
var opposite = [6]api.BlockFace{api.FaceUp, api.FaceDown, api.FaceSouth, api.FaceNorth, api.FaceEast, api.FaceWest}

func (p *Player) interactEntity(id int32, hand api.Hand, attack bool, at api.Vec3) {
	w := p.world.Load()
	var target api.Entity
	for _, o := range w.playerList() {
		if o.e.ID == id {
			target = o
		}
	}
	if target == nil {
		for _, m := range w.mobList() {
			if m.e.ID == id {
				target = m
			}
		}
	}
	if target != nil && attack {
		p.attackEntity(target)
	}
	if target == nil {
		return
	}
	if p.s.bus.HasHandlers(api.KindPlayerInteractEntity) {
		ev := &api.PlayerInteractEntityEvent{Player: p, Target: target, Hand: hand, Attack: attack, At: at}
		if p.s.bus.Dispatch(ev).(*api.PlayerInteractEntityEvent).Cancelled() {
			return
		}
	}
	if m, ok := target.(*Mob); ok && !attack && p.interactMob(m, hand) {
		p.swing(int32(hand))
	}
}
