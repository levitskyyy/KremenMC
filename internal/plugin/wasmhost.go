package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	wapi "github.com/tetratelabs/wazero/api"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/sdk/go/wire"
)

const (
	i32 = wapi.ValueTypeI32
	i64 = wapi.ValueTypeI64
)

const maxGuestString = 1 << 20

func (p *wasmPlugin) buildHost(ctx context.Context) error {
	b := p.rt.NewHostModuleBuilder(wire.HostModule)
	def := func(name string, params, results []wapi.ValueType, fn func(m wapi.Module, s []uint64)) {
		b.NewFunctionBuilder().
			WithGoModuleFunction(wapi.GoModuleFunc(func(_ context.Context, m wapi.Module, s []uint64) { fn(m, s) }), params, results).
			Export(name)
	}
	// reentrant marks host functions that may synchronously fire events or
	// run commands, which could call back into this plugin.
	reentrant := func(fn func(m wapi.Module, s []uint64)) func(m wapi.Module, s []uint64) {
		return func(m wapi.Module, s []uint64) {
			p.hostGID.Store(goid())
			defer p.hostGID.Store(0)
			fn(m, s)
		}
	}
	none := []wapi.ValueType{}
	ret := []wapi.ValueType{i32}

	def(wire.ImportLog, []wapi.ValueType{i32, i32, i32}, none, p.hLog)
	def(wire.ImportResult, []wapi.ValueType{i32}, none, p.hResult)
	def(wire.ImportSendMessage, []wapi.ValueType{i32, i32, i32}, ret, p.hSendMessage)
	def(wire.ImportBroadcast, []wapi.ValueType{i32, i32}, none, p.hBroadcast)
	def(wire.ImportPlayers, none, ret, p.hPlayers)
	def(wire.ImportPlayer, []wapi.ValueType{i32}, ret, p.hPlayer)
	def(wire.ImportPlayerByName, []wapi.ValueType{i32, i32}, ret, p.hPlayerByName)
	def(wire.ImportTeleport, []wapi.ValueType{i32, i32, i32}, ret, reentrant(p.hTeleport))
	def(wire.ImportKick, []wapi.ValueType{i32, i32, i32}, ret, reentrant(p.hKick))
	def(wire.ImportHasPermission, []wapi.ValueType{i32, i32, i32}, ret, p.hHasPermission)
	def(wire.ImportGetBlock, []wapi.ValueType{i32, i32, i32, i32, i32}, ret, p.hGetBlock)
	def(wire.ImportSetBlock, []wapi.ValueType{i32, i32, i32, i32, i32, i32, i32}, ret, reentrant(p.hSetBlock))
	def(wire.ImportRegisterCommand, []wapi.ValueType{i32, i32}, ret, p.hRegisterCommand)
	def(wire.ImportSubscribe, []wapi.ValueType{i32, i32, i32}, ret, p.hSubscribe)
	def(wire.ImportSchedule, []wapi.ValueType{i32, i64, i64, i32}, ret, p.hSchedule)
	def(wire.ImportCancelTask, []wapi.ValueType{i32}, none, p.hCancelTask)
	def(wire.ImportDispatchCommand, []wapi.ValueType{i32, i32, i32}, ret, reentrant(p.hDispatchCommand))
	def(wire.ImportKVGet, []wapi.ValueType{i32, i32}, ret, p.hKVGet)
	def(wire.ImportKVSet, []wapi.ValueType{i32, i32, i32, i32}, ret, p.hKVSet)
	def(wire.ImportKVDelete, []wapi.ValueType{i32, i32}, ret, p.hKVDelete)
	def(wire.ImportWorlds, none, ret, p.hWorlds)
	_, err := b.Instantiate(ctx)
	return err
}

func read(m wapi.Module, ptr, n uint64) ([]byte, bool) {
	if n > maxGuestString {
		return nil, false
	}
	return m.Memory().Read(uint32(ptr), uint32(n))
}

func readString(m wapi.Module, ptr, n uint64) (string, bool) {
	b, ok := read(m, ptr, n)
	return string(b), ok
}

func readUUID(m wapi.Module, ptr uint64) (api.UUID, bool) {
	var u api.UUID
	b, ok := m.Memory().Read(uint32(ptr), 16)
	if !ok {
		return u, false
	}
	copy(u[:], b)
	return u, true
}

func setI32(s []uint64, v int32) { s[0] = wapi.EncodeI32(v) }

// stage holds variable-length output for the guest to fetch via result().
func (p *wasmPlugin) stage(s []uint64, data []byte) {
	p.result = append(p.result[:0], data...)
	setI32(s, int32(len(data)))
}

func (p *wasmPlugin) hLog(m wapi.Module, s []uint64) {
	msg, ok := readString(m, s[1], s[2])
	if !ok {
		return
	}
	p.log.Log(context.Background(), slog.Level(wapi.DecodeI32(s[0])), msg)
}

func (p *wasmPlugin) hResult(m wapi.Module, s []uint64) {
	if !m.Memory().Write(uint32(s[0]), p.result) {
		p.log.Warn("result(): out of bounds buffer")
	}
	p.result = p.result[:0]
}

func (p *wasmPlugin) player(m wapi.Module, ptr uint64) (api.Player, bool) {
	id, ok := readUUID(m, ptr)
	if !ok {
		return nil, false
	}
	return p.env.Server.Player(id)
}

func (p *wasmPlugin) hSendMessage(m wapi.Module, s []uint64) {
	data, ok := read(m, s[1], s[2])
	if !ok {
		setI32(s, wire.BadArgument)
		return
	}
	r := wire.NewReader(data)
	msg := r.Component()
	if r.Err != nil {
		setI32(s, wire.BadArgument)
		return
	}
	if s[0] == 0 {
		p.env.Server.Console().SendMessage(msg)
		setI32(s, wire.OK)
		return
	}
	pl, ok := p.player(m, s[0])
	if !ok {
		setI32(s, wire.NotFound)
		return
	}
	pl.SendMessage(msg)
	setI32(s, wire.OK)
}

func (p *wasmPlugin) hBroadcast(m wapi.Module, s []uint64) {
	data, ok := read(m, s[0], s[1])
	if !ok {
		return
	}
	r := wire.NewReader(data)
	if msg := r.Component(); r.Err == nil {
		p.env.Server.Broadcast(msg)
	}
}

func (p *wasmPlugin) hPlayers(_ wapi.Module, s []uint64) {
	players := p.env.Server.Players()
	var w wire.Writer
	w.Uvarint(uint64(len(players)))
	for _, pl := range players {
		w.Player(playerRef(pl))
	}
	p.stage(s, w.B)
}

func (p *wasmPlugin) hPlayer(m wapi.Module, s []uint64) {
	pl, ok := p.player(m, s[0])
	if !ok {
		setI32(s, wire.NotFound)
		return
	}
	var w wire.Writer
	w.Player(playerRef(pl))
	p.stage(s, w.B)
}

func (p *wasmPlugin) hPlayerByName(m wapi.Module, s []uint64) {
	name, ok := readString(m, s[0], s[1])
	if !ok {
		setI32(s, wire.BadArgument)
		return
	}
	pl, ok := p.env.Server.PlayerByName(name)
	if !ok {
		setI32(s, wire.NotFound)
		return
	}
	var w wire.Writer
	w.Player(playerRef(pl))
	p.stage(s, w.B)
}

func (p *wasmPlugin) hTeleport(m wapi.Module, s []uint64) {
	pl, ok := p.player(m, s[0])
	if !ok {
		setI32(s, wire.NotFound)
		return
	}
	data, ok := read(m, s[1], s[2])
	if !ok {
		setI32(s, wire.BadArgument)
		return
	}
	r := wire.NewReader(data)
	loc := r.Location()
	if r.Err != nil {
		setI32(s, wire.BadArgument)
		return
	}
	if loc.World == "" {
		loc.World = pl.Location().World
	}
	if pl.Teleport(loc, api.TeleportPlugin) {
		setI32(s, wire.OK)
	} else {
		setI32(s, wire.Fail)
	}
}

func (p *wasmPlugin) hKick(m wapi.Module, s []uint64) {
	pl, ok := p.player(m, s[0])
	if !ok {
		setI32(s, wire.NotFound)
		return
	}
	data, ok := read(m, s[1], s[2])
	if !ok {
		setI32(s, wire.BadArgument)
		return
	}
	r := wire.NewReader(data)
	msg := r.Component()
	if r.Err != nil {
		setI32(s, wire.BadArgument)
		return
	}
	pl.Kick(msg)
	setI32(s, wire.OK)
}

func (p *wasmPlugin) hHasPermission(m wapi.Module, s []uint64) {
	perm, ok := readString(m, s[1], s[2])
	if !ok {
		setI32(s, wire.BadArgument)
		return
	}
	var subject api.Permissible
	if s[0] == 0 {
		subject = p.env.Server.Console()
	} else if pl, ok := p.player(m, s[0]); ok {
		subject = pl
	} else {
		setI32(s, wire.NotFound)
		return
	}
	if subject.HasPermission(perm) {
		setI32(s, 1)
	} else {
		setI32(s, 0)
	}
}

func (p *wasmPlugin) world(m wapi.Module, ptr, n uint64) (api.World, bool) {
	name, ok := readString(m, ptr, n)
	if !ok {
		return nil, false
	}
	if name == "" {
		w := p.env.Server.DefaultWorld()
		return w, w != nil
	}
	return p.env.Server.World(name)
}

func posArgs(s []uint64) api.BlockPos {
	return api.BlockPos{X: int(wapi.DecodeI32(s[0])), Y: int(wapi.DecodeI32(s[1])), Z: int(wapi.DecodeI32(s[2]))}
}

func (p *wasmPlugin) hGetBlock(m wapi.Module, s []uint64) {
	w, ok := p.world(m, s[0], s[1])
	if !ok {
		setI32(s, wire.NotFound)
		return
	}
	st := w.BlockState(posArgs(s[2:5]))
	if st == nil {
		setI32(s, wire.NotFound)
		return
	}
	p.stage(s, []byte(st.String()))
}

func (p *wasmPlugin) hSetBlock(m wapi.Module, s []uint64) {
	w, ok := p.world(m, s[0], s[1])
	if !ok {
		setI32(s, wire.NotFound)
		return
	}
	str, ok := readString(m, s[5], s[6])
	if !ok {
		setI32(s, wire.BadArgument)
		return
	}
	st, err := p.env.Server.Blocks().Parse(str)
	if err != nil {
		setI32(s, wire.BadArgument)
		return
	}
	if w.SetBlockState(posArgs(s[2:5]), st, api.UpdateDefault) {
		setI32(s, wire.OK)
	} else {
		setI32(s, wire.Fail)
	}
}

func (p *wasmPlugin) hSubscribe(_ wapi.Module, s []uint64) {
	kind := api.EventKind(uint32(s[0]) & 0xffff)
	tag := uint32(s[0]) &^ 0xffff
	prio := api.Priority(uint32(s[1]))
	ignore := uint32(s[2]) != 0
	if kind >= api.BuiltinKinds || kind == api.KindInvalid || prio > api.Monitor {
		setI32(s, wire.BadArgument)
		return
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if !p.enabled {
		setI32(s, wire.Fail)
		return
	}
	sub, err := SubscribeKind(p.env.Bus.For(p.m.ID), kind, prio, ignore, func(e api.Event) { p.handleEvent(e, tag, prio) })
	if err != nil {
		setI32(s, wire.BadArgument)
		return
	}
	p.subs = append(p.subs, sub)
	setI32(s, wire.OK)
}

func (p *wasmPlugin) hSchedule(_ wapi.Module, s []uint64) {
	id := uint32(s[0])
	delay, period := int64(s[1]), int64(s[2])
	async := uint32(s[3]) != 0
	if delay < 0 || period < 0 {
		setI32(s, wire.BadArgument)
		return
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if !p.enabled {
		setI32(s, wire.Fail)
		return
	}
	if old, ok := p.tasks[id]; ok {
		old.Cancel()
	}
	sched := p.env.Server.Scheduler()
	repeating := period > 0
	run := func(api.Task) { p.runTask(id, repeating) }
	owner := p.m.ID
	var t api.Task
	switch {
	case async && repeating:
		t = sched.Async().RunAtFixedRate(owner, ticks(delay), ticks(period), run)
	case async:
		t = sched.Async().RunDelayed(owner, ticks(delay), run)
	case repeating:
		t = sched.Global().RunRepeating(owner, api.Ticks(delay), api.Ticks(period), run)
	default:
		t = sched.Global().RunDelayed(owner, api.Ticks(delay), run)
	}
	p.tasks[id] = t
	setI32(s, wire.OK)
}

func ticks(n int64) time.Duration { return time.Duration(n) * 50 * time.Millisecond }

func (p *wasmPlugin) hCancelTask(_ wapi.Module, s []uint64) {
	id := uint32(s[0])
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if t, ok := p.tasks[id]; ok {
		t.Cancel()
		delete(p.tasks, id)
	}
}

func (p *wasmPlugin) hDispatchCommand(m wapi.Module, s []uint64) {
	line, ok := readString(m, s[1], s[2])
	if !ok {
		setI32(s, wire.BadArgument)
		return
	}
	var sender api.CommandSender = p.env.Server.Console()
	if s[0] != 0 {
		pl, ok := p.player(m, s[0])
		if !ok {
			setI32(s, wire.NotFound)
			return
		}
		sender = pl
	}
	n, err := p.env.Server.Commands().Dispatch(sender, line)
	if err != nil {
		setI32(s, wire.Fail)
		return
	}
	setI32(s, int32(n))
}

func (p *wasmPlugin) hKVGet(m wapi.Module, s []uint64) {
	key, ok := readString(m, s[0], s[1])
	if !ok || p.kv == nil {
		setI32(s, wire.BadArgument)
		return
	}
	v, found, err := p.kv.Get(key)
	if err != nil || !found {
		setI32(s, wire.NotFound)
		return
	}
	p.stage(s, v)
}

func (p *wasmPlugin) hKVSet(m wapi.Module, s []uint64) {
	key, ok1 := readString(m, s[0], s[1])
	val, ok2 := read(m, s[2], s[3])
	if !ok1 || !ok2 || p.kv == nil {
		setI32(s, wire.BadArgument)
		return
	}
	if err := p.kv.Set(key, val); err != nil {
		p.log.Warn("kv set", "key", key, "err", err)
		setI32(s, wire.Fail)
		return
	}
	setI32(s, wire.OK)
}

func (p *wasmPlugin) hKVDelete(m wapi.Module, s []uint64) {
	key, ok := readString(m, s[0], s[1])
	if !ok || p.kv == nil {
		setI32(s, wire.BadArgument)
		return
	}
	if err := p.kv.Delete(key); err != nil {
		setI32(s, wire.Fail)
		return
	}
	setI32(s, wire.OK)
}

func (p *wasmPlugin) hWorlds(_ wapi.Module, s []uint64) {
	worlds := p.env.Server.Worlds()
	names := make([]string, len(worlds))
	for i, w := range worlds {
		names[i] = w.Name()
	}
	var w wire.Writer
	w.Strings(names)
	p.stage(s, w.B)
}

func (p *wasmPlugin) hRegisterCommand(m wapi.Module, s []uint64) {
	data, ok := read(m, s[0], s[1])
	if !ok {
		setI32(s, wire.BadArgument)
		return
	}
	r := wire.NewReader(data)
	aliases := r.Strings()
	root := p.decodeNode(r, nil, 0)
	if r.Err != nil || root == nil || root.Kind != api.NodeLiteral {
		setI32(s, wire.BadArgument)
		return
	}
	if mc, ok := p.m.Commands[root.Name]; ok {
		aliases = append(aliases, mc.Aliases...)
		if root.Permission == "" {
			root.Permission = mc.Permission
		}
		if root.Description == "" {
			root.Description = mc.Description
		}
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if !p.enabled {
		setI32(s, wire.Fail)
		return
	}
	if err := p.env.Server.Commands().Register(p.m.ID, root, aliases...); err != nil {
		p.log.Warn("register command", "command", root.Name, "err", err)
		setI32(s, wire.Fail)
		return
	}
	p.commands = true
	setI32(s, wire.OK)
}

type argSpec struct {
	name string
	t    api.ArgumentType
}

const maxCommandDepth = 32

func (p *wasmPlugin) decodeNode(r *wire.Reader, path []argSpec, depth int) *api.CommandNode {
	if depth > maxCommandDepth {
		r.Err = fmt.Errorf("command tree too deep")
		return nil
	}
	n := &api.CommandNode{Kind: api.NodeKind(r.U8()), Name: r.String()}
	if n.Kind == api.NodeArgument {
		n.Type.Parser = api.ParserID(r.String())
		flags := r.U8()
		n.Type.Min, n.Type.Max = r.F64(), r.F64()
		n.Type.StringMode = api.StringMode(r.U8())
		n.Type.Registry = api.Key(r.String())
		n.Type.HasMin = flags&wire.ArgHasMin != 0
		n.Type.HasMax = flags&wire.ArgHasMax != 0
		n.Type.Single = flags&wire.ArgSingle != 0
		n.Type.PlayersOnly = flags&wire.ArgPlayersOnly != 0
		path = append(path[:len(path):len(path)], argSpec{n.Name, n.Type})
	} else if n.Kind != api.NodeLiteral {
		r.Err = fmt.Errorf("bad node kind %d", n.Kind)
		return nil
	}
	n.Permission = r.String()
	handler := uint32(r.Uvarint())
	if suggestions := r.Strings(); len(suggestions) > 0 {
		n.Suggest = staticSuggestions(suggestions)
	}
	n.Description = r.String()
	if handler != 0 {
		args := path
		n.Executor = func(ctx api.CommandContext) (int, error) {
			return p.runCommand(handler, encodeCommandContext(ctx, args))
		}
	}
	count := r.Count()
	for i := 0; i < count && r.Err == nil; i++ {
		if c := p.decodeNode(r, path, depth+1); c != nil {
			n.Children = append(n.Children, c)
		}
	}
	return n
}

func staticSuggestions(list []string) api.SuggestionProvider {
	return func(_ api.CommandContext, remaining string) []api.Suggestion {
		var out []api.Suggestion
		for _, s := range list {
			if len(s) >= len(remaining) && s[:len(remaining)] == remaining {
				out = append(out, api.Suggestion{Text: s})
			}
		}
		return out
	}
}

func encodeCommandContext(ctx api.CommandContext, args []argSpec) []byte {
	var w wire.Writer
	if pl, ok := ctx.Sender().(api.Player); ok {
		w.U8(wire.SenderPlayer)
		w.String(pl.Name())
		w.UUID(pl.UUID())
	} else {
		w.U8(wire.SenderConsole)
		name := ""
		if s := ctx.Sender(); s != nil {
			name = s.Name()
		}
		w.String(name)
	}
	w.String(ctx.Input())
	w.String(ctx.Label())
	present := make([]argSpec, 0, len(args))
	for _, a := range args {
		if ctx.Has(a.name) {
			present = append(present, a)
		}
	}
	w.Uvarint(uint64(len(present)))
	for _, a := range present {
		w.String(a.name)
		switch a.t.Parser {
		case api.ParserInteger, api.ParserLong:
			w.U8(wire.ValInt)
			w.Varint(ctx.Int(a.name))
		case api.ParserFloat, api.ParserDouble:
			w.U8(wire.ValFloat)
			w.F64(ctx.Float(a.name))
		case api.ParserBool:
			w.U8(wire.ValBool)
			w.Bool(ctx.Bool(a.name))
		case api.ParserEntity, api.ParserGameProfile:
			w.U8(wire.ValUUIDs)
			if a.t.PlayersOnly || a.t.Parser == api.ParserGameProfile {
				ps := ctx.Players(a.name)
				w.Uvarint(uint64(len(ps)))
				for _, pl := range ps {
					w.UUID(pl.UUID())
				}
			} else {
				es := ctx.Entities(a.name)
				w.Uvarint(uint64(len(es)))
				for _, e := range es {
					w.UUID(e.UUID())
				}
			}
		case api.ParserBlockPos:
			w.U8(wire.ValBlockPos)
			w.BlockPos(ctx.BlockPos(a.name))
		case api.ParserVec3:
			w.U8(wire.ValVec3)
			w.Vec3(ctx.Vec3(a.name))
		case api.ParserBlockState:
			w.U8(wire.ValString)
			w.String(stateString(ctx.BlockState(a.name)))
		case api.ParserItemStack:
			w.U8(wire.ValString)
			w.String(string(ctx.Item(a.name).Type))
		case api.ParserComponent:
			w.U8(wire.ValString)
			w.String(ctx.Component(a.name).Plain())
		default:
			w.U8(wire.ValString)
			w.String(ctx.String(a.name))
		}
	}
	return w.B
}
