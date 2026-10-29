package packets

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/proto"
)

func TestRegistry(t *testing.T) {
	counts := []struct {
		s    State
		d    Direction
		want int
	}{
		{Handshake, Clientbound, 0}, {Handshake, Serverbound, 1},
		{Status, Clientbound, 2}, {Status, Serverbound, 2},
		{Login, Clientbound, 6}, {Login, Serverbound, 5},
		{Configuration, Clientbound, 21}, {Configuration, Serverbound, 10},
		{Play, Clientbound, 144}, {Play, Serverbound, 69},
	}
	for _, c := range counts {
		if n := Count(c.s, c.d); n != c.want {
			t.Errorf("Count(%d,%d) = %d, want %d", c.s, c.d, n, c.want)
		}
		for id := int32(0); id < int32(c.want); id++ {
			p := New(c.s, c.d, id)
			if p == nil || p.ID() != id || Name(c.s, c.d, id) == "" {
				t.Errorf("New(%d,%d,%d) = %T", c.s, c.d, id, p)
			}
		}
		if New(c.s, c.d, int32(c.want)) != nil || New(c.s, c.d, -1) != nil {
			t.Errorf("New(%d,%d) out of range not nil", c.s, c.d)
		}
	}
	ids := []struct {
		p    Packet
		want int32
	}{
		{&ClientboundLogin{}, 0x32}, {&ClientboundLevelChunkWithLight{}, 0x2E}, {&ClientboundPlayerPosition{}, 0x49},
		{&ClientboundSystemChat{}, 0x7C}, {&ClientboundSetChunkCacheCenter{}, 0x60}, {&ClientboundKeepAlive{}, 0x2D},
		{&ClientboundConfigSelectKnownPacks{}, 0x0F}, {&ClientboundConfigFinishConfiguration{}, 0x03},
		{&ServerboundMovePlayerPos{}, 0x1E}, {&ServerboundUseItemOn{}, 0x42}, {&ServerboundPunch{}, 0x2E},
		{&ServerboundCustomClickAction{}, 0x44}, {&ClientboundSwingAnimation{}, 0x7B}, {&ClientboundShowDialog{}, 0x8F},
	}
	for _, c := range ids {
		if c.p.ID() != c.want {
			t.Errorf("%T.ID() = 0x%02X, want 0x%02X", c.p, c.p.ID(), c.want)
		}
	}
}

var (
	textT        = reflect.TypeFor[proto.Text]()
	rawT         = reflect.TypeFor[proto.RawNBT]()
	lpT          = reflect.TypeFor[proto.LpVec3]()
	sampleNBT, _ = nbt.Marshal(nil, "", nbt.Compound{"k": int32(1), "s": "v"}, true)
)

func fill(v reflect.Value, rng *rand.Rand, depth int) {
	switch v.Type() {
	case textT:
		t := proto.Text{Text: fmt.Sprintf("t%d", rng.IntN(100))}
		if rng.IntN(2) == 0 {
			t.Color = proto.Red
			t.Extra = []proto.Text{proto.Plain("x")}
		}
		v.Set(reflect.ValueOf(t))
		return
	case rawT:
		if rng.IntN(2) == 0 {
			v.Set(reflect.ValueOf(proto.RawNBT(sampleNBT)))
		}
		return
	case lpT:
		vals := []float64{0, 1, -1}
		v.Set(reflect.ValueOf(proto.LpVec3{X: vals[rng.IntN(3)], Y: vals[rng.IntN(3)], Z: vals[rng.IntN(3)]}))
		return
	}
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(rng.IntN(2) == 0)
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if rng.IntN(2) == 0 {
			v.SetInt(int64(rng.IntN(4)))
		} else {
			v.SetInt(int64(rng.Uint64()))
		}
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if rng.IntN(2) == 0 {
			v.SetUint(uint64(rng.IntN(4)))
		} else {
			v.SetUint(rng.Uint64())
		}
	case reflect.Float32, reflect.Float64:
		v.SetFloat(rng.NormFloat64() * 100)
	case reflect.String:
		v.SetString(fmt.Sprintf("s%x", rng.Uint32()))
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			fill(v.Index(i), rng, depth)
		}
	case reflect.Slice:
		n := rng.IntN(4)
		if depth > 4 {
			n = 0
		}
		s := reflect.MakeSlice(v.Type(), n, n)
		for i := 0; i < n; i++ {
			fill(s.Index(i), rng, depth+1)
		}
		v.Set(s)
	case reflect.Pointer:
		if depth > 4 || rng.IntN(2) == 0 {
			return
		}
		p := reflect.New(v.Type().Elem())
		fill(p.Elem(), rng, depth+1)
		v.Set(p)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			fill(v.Field(i), rng, depth+1)
		}
	}
}

func dirName(d Direction) string {
	if d == Clientbound {
		return "clientbound"
	}
	return "serverbound"
}

// Every packet must survive encode -> decode -> encode with identical bytes.
func TestRoundTripAll(t *testing.T) {
	for s := Handshake; s <= Play; s++ {
		for d := Clientbound; d <= Serverbound; d++ {
			for id := int32(0); id < int32(Count(s, d)); id++ {
				for seed := uint64(0); seed < 40; seed++ {
					rng := rand.New(rand.NewPCG(seed, uint64(id)))
					p := New(s, d, id)
					fill(reflect.ValueOf(p).Elem(), rng, 0)
					var w proto.Writer
					Marshal(&w, p)
					q, err := Unmarshal(s, d, w.B)
					if err != nil {
						t.Fatalf("%s/%s %s seed %d: %v\n%+v", dirName(d), Name(s, d, id), reflect.TypeOf(p), seed, err, p)
					}
					var w2 proto.Writer
					Marshal(&w2, q)
					if !bytes.Equal(w.B, w2.B) {
						t.Fatalf("%T seed %d: re-encode differs\n% X\n% X", p, seed, w.B, w2.B)
					}
				}
			}
		}
	}
}

func uuid(b byte) [16]byte {
	var u [16]byte
	for i := range u {
		u[i] = b + byte(i)
	}
	return u
}

func corePackets() []struct {
	s State
	d Direction
	p Packet
} {
	type sp = struct {
		s State
		d Direction
		p Packet
	}
	nbtRaw := proto.RawNBT(sampleNBT)
	spawn := SpawnInfo{Dimension: 0, Name: "minecraft:overworld", HashedSeed: -42, Gamemode: 1, PreviousGamemode: 0,
		IsFlat: true, Death: proto.Some(GlobalPos{DimensionName: "minecraft:the_nether", Location: proto.BlockPos{X: 1, Y: -64, Z: -3}}),
		PortalCooldown: 20, SeaLevel: 63}
	return []sp{
		{Handshake, Serverbound, &ServerboundHandshakeIntention{ProtocolVersion: 777, ServerHost: "localhost", ServerPort: 25565, NextState: 2}},
		{Status, Clientbound, &ClientboundStatusResponse{Response: `{"version":{"name":"26.3","protocol":777}}`}},
		{Status, Clientbound, &ClientboundStatusPongResponse{Time: 99}},
		{Status, Serverbound, &ServerboundStatusRequest{}},
		{Status, Serverbound, &ServerboundStatusPingRequest{Time: 99}},
		{Login, Clientbound, &ClientboundLoginDisconnect{Reason: `{"text":"bye"}`}},
		{Login, Clientbound, &ClientboundLoginHello{ServerID: "", PublicKey: []byte{1, 2, 3}, VerifyToken: []byte{4, 5, 6, 7}, ShouldAuthenticate: true}},
		{Login, Clientbound, &ClientboundLoginFinished{UUID: uuid(1), Username: "Steve",
			Properties: []GameProfileProperty{{Name: "textures", Value: "e30=", Signature: proto.Some("sig")}, {Name: "x", Value: "y"}}, SessionID: uuid(9)}},
		{Login, Clientbound, &ClientboundLoginCompression{Threshold: 256}},
		{Login, Clientbound, &ClientboundLoginCustomQuery{MessageID: 7, Channel: "velocity:player_info", Data: []byte{1, 2}}},
		{Login, Serverbound, &ServerboundLoginHello{Username: "Steve", PlayerUUID: uuid(1)}},
		{Login, Serverbound, &ServerboundLoginKey{SharedSecret: bytes.Repeat([]byte{1}, 128), VerifyToken: bytes.Repeat([]byte{2}, 128)}},
		{Login, Serverbound, &ServerboundLoginCustomQueryAnswer{MessageID: 7, Data: proto.Some([]byte{9, 9})}},
		{Login, Serverbound, &ServerboundLoginCustomQueryAnswer{MessageID: 8}},
		{Login, Serverbound, &ServerboundLoginAcknowledged{}},
		{Configuration, Serverbound, &ServerboundConfigClientInformation{Locale: "en_us", ViewDistance: 12, ChatColors: true, SkinParts: 0x7f, MainHand: 1, EnableServerListing: true, ParticleStatus: 2}},
		{Configuration, Clientbound, &ClientboundConfigSelectKnownPacks{Packs: []ClientboundConfigSelectKnownPacksPack{{Namespace: "minecraft", ID: "core", Version: "26.3"}}}},
		{Configuration, Serverbound, &ServerboundConfigSelectKnownPacks{Packs: []ServerboundConfigSelectKnownPacksPack{{Namespace: "minecraft", ID: "core", Version: "26.3"}}}},
		{Configuration, Clientbound, &ClientboundConfigRegistryData{Registry: "minecraft:dimension_type", Entries: []ClientboundConfigRegistryDataEntry{
			{Key: "minecraft:overworld"}, {Key: "custom:x", Value: proto.Some(nbtRaw)}}}},
		{Configuration, Clientbound, &ClientboundConfigUpdateEnabledFeatures{Features: []string{"minecraft:vanilla"}}},
		{Configuration, Clientbound, &ClientboundConfigUpdateTags{Tags: []ClientboundConfigUpdateTagsTag{{TagType: "minecraft:block",
			Tags: []Tag{{TagName: "minecraft:logs", Entries: []int32{1, 2, 300}}}}}}},
		{Configuration, Clientbound, &ClientboundConfigFinishConfiguration{}},
		{Configuration, Serverbound, &ServerboundConfigFinishConfiguration{}},
		{Configuration, Clientbound, &ClientboundConfigKeepAlive{KeepAliveID: -5}},
		{Configuration, Serverbound, &ServerboundConfigKeepAlive{KeepAliveID: -5}},
		{Configuration, Clientbound, &ClientboundConfigPing{Payload: 3}},
		{Configuration, Serverbound, &ServerboundConfigPong{Payload: 3}},
		{Configuration, Clientbound, &ClientboundConfigCustomPayload{Channel: "minecraft:brand", Data: []byte("\x08kremenmc")}},
		{Configuration, Serverbound, &ServerboundConfigCustomPayload{Channel: "minecraft:brand", Data: []byte("\x07vanilla")}},
		{Configuration, Clientbound, &ClientboundConfigDisconnect{Reason: proto.Plain("kicked")}},
		{Configuration, Clientbound, &ClientboundConfigPostEffects{Effects: []string{"minecraft:creeper"}}},
		{Play, Clientbound, &ClientboundLogin{EntityID: 42, WorldNames: []string{"minecraft:overworld", "minecraft:the_nether"}, MaxPlayers: 20,
			ViewDistance: 10, SimulationDistance: 8, EnableRespawnScreen: true, WorldState: spawn, OnlineMode: true, EnforcesSecureChat: true}},
		{Play, Clientbound, &ClientboundRespawn{WorldState: spawn, CopyMetadata: 3}},
		{Play, Clientbound, &ClientboundLevelChunkWithLight{X: -3, Z: 7,
			Heightmaps:    []ClientboundLevelChunkWithLightHeightmap{{Type: 4, Data: []int64{1, 2, 3}}},
			ChunkData:     bytes.Repeat([]byte{0, 1, 2}, 100),
			BlockEntities: []ChunkBlockEntity{{X: 15, Z: 3, Y: -60, Type: 7, NBTData: nbtRaw}, {X: 1, Z: 2, Y: 3, Type: 1, NBTData: proto.RawNBT{0}}},
			SkyLightMask:  []byte{0xff}, EmptySkyLightMask: []byte{1}, EmptyBlockLightMask: []byte{2},
			SkyLight: [][]byte{bytes.Repeat([]byte{0xff}, 2048)}}},
		{Play, Clientbound, &ClientboundLightUpdate{ChunkX: 1, ChunkZ: -1, SkyLightMask: []byte{3}, SkyLight: [][]byte{make([]byte, 2048), make([]byte, 2048)}}},
		{Play, Clientbound, &ClientboundForgetLevelChunk{ChunkZ: -9, ChunkX: 4}},
		{Play, Clientbound, &ClientboundSetChunkCacheCenter{ChunkX: -1, ChunkZ: 2}},
		{Play, Clientbound, &ClientboundSetChunkCacheRadius{ViewDistance: 12}},
		{Play, Clientbound, &ClientboundChunkBatchStart{}},
		{Play, Clientbound, &ClientboundChunkBatchFinished{BatchSize: 25}},
		{Play, Serverbound, &ServerboundChunkBatchReceived{ChunksPerTick: 9.5}},
		{Play, Clientbound, &ClientboundBlockUpdate{Location: proto.BlockPos{X: -1, Y: 70, Z: 5}, Type: 1}},
		{Play, Clientbound, &ClientboundSectionBlocksUpdate{ChunkCoordinates: ClientboundSectionBlocksUpdateChunkCoordinates{X: -2, Z: 3, Y: -4},
			Records: []int64{1<<12 | 0x123, 25000<<12 | 0xfff}}},
		{Play, Clientbound, &ClientboundPlayerPosition{TeleportID: 1, X: 0.5, Y: 64, Z: -0.5, Yaw: 90, Pitch: -10, Flags: PositionUpdateRelativesYaw | PositionUpdateRelativesPitch}},
		{Play, Serverbound, &ServerboundAcceptTeleportation{TeleportID: 1, X: 0.5, Y: 64, Z: -0.5, Yaw: 90, Pitch: -10}},
		{Play, Serverbound, &ServerboundMovePlayerPos{X: 1, Y: 2, Z: 3, Flags: MovementFlagsOnGround}},
		{Play, Serverbound, &ServerboundMovePlayerPosRot{X: 1, Y: 2, Z: 3, Yaw: 4, Pitch: 5, Flags: MovementFlagsHasHorizontalCollision}},
		{Play, Serverbound, &ServerboundMovePlayerRot{Yaw: 4, Pitch: 5}},
		{Play, Serverbound, &ServerboundMovePlayerStatusOnly{Flags: MovementFlagsOnGround}},
		{Play, Clientbound, &ClientboundKeepAlive{KeepAliveID: 123456789}},
		{Play, Serverbound, &ServerboundKeepAlive{KeepAliveID: 123456789}},
		{Play, Clientbound, &ClientboundPing{Payload: 5}},
		{Play, Serverbound, &ServerboundPong{Payload: 5}},
		{Play, Serverbound, &ServerboundPingRequest{Payload: 77}},
		{Play, Clientbound, &ClientboundPongResponse{Payload: 77}},
		{Play, Clientbound, &ClientboundSystemChat{Content: proto.Text{Text: "hello", Color: proto.Yellow}, IsActionBar: true}},
		{Play, Clientbound, &ClientboundPlayerChat{GlobalIndex: 3, SenderUUID: uuid(2), Index: 1, Signature: proto.Some([256]byte{1, 2}),
			PlainMessage: "hi", Timestamp: 1700000000000, Salt: -7,
			PreviousMessages:    []PreviousMessage{{ID: 0, Signature: [256]byte{3}}, {ID: 5}},
			UnsignedChatContent: proto.Some(proto.Plain("hi!")), FilterType: 2, FilterTypeMask: []int64{5},
			Type: proto.Holder[ChatTypes]{ID: 0}, NetworkName: proto.Plain("Steve"), NetworkTargetName: proto.Some(proto.Plain("Alex"))}},
		{Play, Clientbound, &ClientboundPlayerChat{Type: proto.Holder[ChatTypes]{Value: &ChatTypes{
			Chat:      ChatType{TranslationKey: "chat.type.text", Parameters: []int32{1, 0}, Style: nbtRaw},
			Narration: ChatType{TranslationKey: "chat.type.text.narrate", Parameters: []int32{1}, Style: proto.RawNBT{0}}}},
			NetworkName: proto.Plain("Steve")}},
		{Play, Clientbound, &ClientboundDisguisedChat{Message: proto.Plain("m"), Type: proto.Holder[ChatTypes]{ID: 2}, Name: proto.Plain("n")}},
		{Play, Serverbound, &ServerboundChatCommand{Command: "gamemode creative"}},
		{Play, Serverbound, &ServerboundChat{Message: "hello", Timestamp: 1, Salt: 2, Signature: proto.Some([256]byte{7}), Offset: 3, Acknowledged: [3]byte{1, 0, 1}, Checksum: 9}},
		{Play, Clientbound, &ClientboundGameEvent{Reason: 3, GameMode: 1}},
		{Play, Clientbound, &ClientboundPlayerInfoUpdate{Action: ClientboundPlayerInfoUpdateActionAddPlayer | ClientboundPlayerInfoUpdateActionUpdateListed |
			ClientboundPlayerInfoUpdateActionUpdateLatency | ClientboundPlayerInfoUpdateActionUpdateDisplayName | ClientboundPlayerInfoUpdateActionUpdateHat,
			Data: []ClientboundPlayerInfoUpdateDataEntry{{UUID: uuid(1), Player: GameProfileNameProp{Name: "Steve",
				Properties: []GameProfileProperty{{Name: "textures", Value: "v"}}}, Listed: true, Latency: 50,
				DisplayName: proto.Some(proto.Text{Text: "[A] Steve", Color: proto.Red}), ShowHat: true}}}},
		{Play, Clientbound, &ClientboundPlayerInfoUpdate{Action: ClientboundPlayerInfoUpdateActionInitializeChat | ClientboundPlayerInfoUpdateActionUpdateGameMode | ClientboundPlayerInfoUpdateActionUpdateListOrder,
			Data: []ClientboundPlayerInfoUpdateDataEntry{{UUID: uuid(3), ChatSession: proto.Some(ChatSession{UUID: uuid(4),
				PublicKey: ChatSessionPublicKey{ExpireTime: 1, KeyBytes: []byte{1}, KeySignature: []byte{2}}}), Gamemode: 3, ListPriority: -1}}}},
		{Play, Clientbound, &ClientboundPlayerInfoRemove{Players: [][16]byte{uuid(1), uuid(2)}}},
		{Play, Clientbound, &ClientboundAddEntity{EntityID: 9, ObjectUUID: uuid(5), Type: 151, X: 1.5, Y: 64, Z: -2.25, Velocity: proto.LpVec3{X: 1, Y: 0, Z: -1}, Pitch: 10, Yaw: -20, HeadPitch: 30, ObjectData: 0}},
		{Play, Clientbound, &ClientboundRemoveEntities{EntityIDs: []int32{1, 2, 3}}},
		{Play, Clientbound, &ClientboundMoveEntityPos{EntityID: 9, Delta: proto.MoveDelta{DX: 4096, DY: -40, DZ: 3, OnGround: true}}},
		{Play, Clientbound, &ClientboundMoveEntityPos{EntityID: 9, Delta: proto.MoveDelta{Steps: []proto.MoveStep{{TickOffset: 1, DX: 1, DY: 2, DZ: 3}}}}},
		{Play, Clientbound, &ClientboundMoveEntityPosRot{EntityID: 9, Delta: proto.MoveDelta{DX: 1}, Yaw: 64, Pitch: -64}},
		{Play, Clientbound, &ClientboundMoveEntityRot{EntityID: 9, OnGround: true, Yaw: 1, Pitch: 2}},
		{Play, Clientbound, &ClientboundEntityPositionSync{EntityID: 9, PathType: 0, Position: Vec3f64{X: 1, Y: 2, Z: 3}, Yaw: 45, Pitch: 10, OnGround: true}},
		{Play, Clientbound, &ClientboundEntityPositionSync{EntityID: 9, PathType: 1, Steps: []PositionStep{{X: 1, Y: 2, Z: 3, TickOffset: 1}, {X: 2, Y: 2, Z: 3, TickOffset: 2}}}},
		{Play, Clientbound, &ClientboundTeleportEntity{EntityID: 9, X: 1, Y: 2, Z: 3, Dx: 0.1, Yaw: 5, Pitch: 6, Flags: PositionUpdateRelativesX, OnGround: true}},
		{Play, Clientbound, &ClientboundSetEntityMotion{EntityID: 9, Velocity: proto.LpVec3{X: 0, Y: -1, Z: 0}}},
		{Play, Clientbound, &ClientboundRotateHead{EntityID: 9, HeadYaw: -128}},
		{Play, Clientbound, &ClientboundSetEntityData{EntityID: 9, Raw: []byte{0, 0, 1, 0xff}}},
		{Play, Clientbound, &ClientboundSetEquipment{EntityID: 9, Raw: []byte{0, 0}}},
		{Play, Clientbound, &ClientboundCommands{Raw: []byte{1, 0, 0, 0}}},
		{Play, Clientbound, &ClientboundCommandSuggestions{TransactionID: 1, Start: 1, Length: 3,
			Matches: []ClientboundCommandSuggestionsMatch{{Match: "help"}, {Match: "home", Tooltip: proto.Some(proto.Plain("teleport home"))}}}},
		{Play, Serverbound, &ServerboundCommandSuggestion{TransactionID: 1, Text: "/he"}},
		{Play, Serverbound, &ServerboundPlayerAction{Status: 2, Location: proto.BlockPos{X: 10, Y: -5, Z: 3}, Face: 1, Sequence: 4}},
		{Play, Serverbound, &ServerboundUseItemOn{Hand: 1, Location: proto.BlockPos{X: 1, Y: 2, Z: 3}, Direction: 5, CursorX: 0.5, CursorY: 1, CursorZ: 0.25, InsideBlock: true, Sequence: 9}},
		{Play, Serverbound, &ServerboundUseItem{Hand: 0, Sequence: 3, Rotation: Vec2f{X: 90, Y: 45}}},
		{Play, Serverbound, &ServerboundPunch{}},
		{Play, Clientbound, &ClientboundSwingAnimation{EntityID: 9, Hand: 1, Animation: 2, Duration: 6}},
		{Play, Clientbound, &ClientboundAnimate{EntityID: 9, Animation: 1}},
		{Play, Serverbound, &ServerboundSetCarriedItem{SlotID: 4}},
		{Play, Clientbound, &ClientboundSetHeldSlot{Slot: 4}},
		{Play, Clientbound, &ClientboundContainerSetSlot{WindowID: 0, StateID: 1, Slot: 36, Raw: []byte{0}}},
		{Play, Clientbound, &ClientboundContainerSetContent{WindowID: 1, StateID: 2, Raw: []byte{1, 0, 0}}},
		{Play, Serverbound, &ServerboundContainerClick{WindowID: 1, StateID: 2, Slot: 3, MouseButton: 0, Mode: 1, Raw: []byte{0, 0}}},
		{Play, Clientbound, &ClientboundContainerClose{WindowID: 1}},
		{Play, Serverbound, &ServerboundContainerClose{WindowID: 1}},
		{Play, Clientbound, &ClientboundDisconnect{Reason: proto.Translatable("multiplayer.disconnect.kicked")}},
		{Play, Clientbound, &ClientboundPlayerAbilities{Flags: 0x0f, FlyingSpeed: 0.05, WalkingSpeed: 0.1}},
		{Play, Clientbound, &ClientboundSetHealth{Health: 20, Food: 20, FoodSaturation: 5}},
		{Play, Clientbound, &ClientboundSetTime{Age: 1000, ClockUpdates: []ClientboundSetTimeClockUpdate{{ID: 0, TotalTicks: 6000, PartialTick: 0.5, Rate: 1}}}},
		{Play, Clientbound, &ClientboundSetDefaultSpawnPosition{GlobalPos: GlobalPos{DimensionName: "minecraft:overworld", Location: proto.BlockPos{Y: 64}}, Yaw: 1, Pitch: 2}},
		{Play, Clientbound, &ClientboundInitializeBorder{X: 0, Z: 0, OldDiameter: 100, NewDiameter: 200, Speed: 1 << 40, PortalTeleportBoundary: 29999984, WarningBlocks: 5, WarningTime: 15}},
		{Play, Clientbound, &ClientboundCustomPayload{Channel: "minecraft:brand", Data: []byte("\x08kremenmc")}},
		{Play, Serverbound, &ServerboundCustomPayload{Channel: "minecraft:brand", Data: []byte("\x07vanilla")}},
		{Play, Serverbound, &ServerboundClientTickEnd{}},
		{Play, Serverbound, &ServerboundClientInformation{Locale: "de_de", ViewDistance: 2}},
		{Play, Clientbound, &ClientboundStartConfiguration{}},
		{Play, Serverbound, &ServerboundConfigurationAcknowledged{}},
		{Play, Clientbound, &ClientboundAddTransientBlock{Location: proto.BlockPos{X: 1}, BlockState: 5}},
		{Play, Clientbound, &ClientboundOpenSignEditor{Location: proto.BlockPos{X: 1}, TextSlot: 1}},
		{Play, Serverbound, &ServerboundSignUpdate{Location: proto.BlockPos{X: 1}, Text1: "a", Text4: "d", TextSlot: 1}},
	}
}

func TestCorePacketsRoundTrip(t *testing.T) {
	for _, c := range corePackets() {
		var w proto.Writer
		Marshal(&w, c.p)
		q, err := Unmarshal(c.s, c.d, w.B)
		if err != nil {
			t.Fatalf("%T: %v", c.p, err)
		}
		if reflect.TypeOf(q) != reflect.TypeOf(c.p) {
			t.Fatalf("%T decoded as %T", c.p, q)
		}
		if !reflect.DeepEqual(q, c.p) {
			t.Errorf("%T roundtrip mismatch", c.p)
		}
	}
}

func TestGoldenBytes(t *testing.T) {
	tests := []struct {
		p    Packet
		want []byte
	}{
		{&ServerboundHandshakeIntention{ProtocolVersion: 777, ServerHost: "a", ServerPort: 25565, NextState: 2},
			[]byte{0x00, 0x89, 0x06, 0x01, 'a', 0x63, 0xDD, 0x02}},
		{&ClientboundKeepAlive{KeepAliveID: 1}, []byte{0x2D, 0, 0, 0, 0, 0, 0, 0, 1}},
		{&ClientboundSetChunkCacheCenter{ChunkX: -1, ChunkZ: 1}, []byte{0x60, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F, 0x01}},
		{&ClientboundMoveEntityRot{EntityID: 1, OnGround: true, Yaw: 2, Pitch: 3}, []byte{0x39, 0x01, 0x01, 0x02, 0x03}},
		{&ClientboundMoveEntityPos{EntityID: 1, Delta: proto.MoveDelta{DX: 1, OnGround: true}}, []byte{0x36, 0x01, 0x01, 0, 1, 0, 0, 0, 0}},
		{&ClientboundSectionBlocksUpdate{ChunkCoordinates: ClientboundSectionBlocksUpdateChunkCoordinates{X: 1, Z: 2, Y: -1}, Records: []int64{1<<12 | 0x10}},
			[]byte{0x56, 0x00, 0x00, 0x04, 0x00, 0x00, 0x2F, 0xFF, 0xFF, 0x01, 0x90, 0x20}},
		{&ClientboundLoginFinished{UUID: [16]byte{15: 1}, Username: "S", SessionID: [16]byte{15: 2}},
			append(append([]byte{0x02}, append(make([]byte, 15), 1)...), append([]byte{0x01, 'S', 0x00}, append(make([]byte, 15), 2)...)...)},
		{&ClientboundSystemChat{Content: proto.Plain("hi"), IsActionBar: false}, []byte{0x7C, 0x08, 0x00, 0x02, 'h', 'i', 0x00}},
		{&ServerboundPunch{}, []byte{0x2E}},
		{&ServerboundAcceptTeleportation{TeleportID: 1}, append([]byte{0x00, 0x01}, make([]byte, 32)...)},
	}
	for _, tt := range tests {
		var w proto.Writer
		Marshal(&w, tt.p)
		if !bytes.Equal(w.B, tt.want) {
			t.Errorf("%T = % X, want % X", tt.p, w.B, tt.want)
		}
	}
}

func TestUnmarshalErrors(t *testing.T) {
	if _, err := Unmarshal(Play, Clientbound, []byte{0x7F, 0x01}); err == nil {
		t.Error("unknown packet accepted")
	}
	if _, err := Unmarshal(Play, Clientbound, []byte{0x2C, 0, 0, 0, 0, 0, 0, 0, 1, 0xEE}); err == nil {
		t.Error("trailing data accepted")
	}
	if _, err := Unmarshal(Play, Clientbound, []byte{0x2C, 0, 0}); err == nil {
		t.Error("truncated packet accepted")
	}
	if _, err := Unmarshal(Play, Clientbound, []byte{0x67, 0xFF, 0xFF, 0xFF, 0xFF, 0x07}); err == nil {
		t.Error("huge array count accepted")
	}
}

func chunkPacket() *ClientboundLevelChunkWithLight {
	return &ClientboundLevelChunkWithLight{
		X: 3, Z: -4,
		Heightmaps: []ClientboundLevelChunkWithLightHeightmap{
			{Type: 1, Data: make([]int64, 37)}, {Type: 4, Data: make([]int64, 37)}, {Type: 5, Data: make([]int64, 37)},
		},
		ChunkData:    bytes.Repeat([]byte{1, 2, 3, 4}, 8<<10),
		SkyLightMask: []byte{0xff, 0xff, 0xff, 0x03}, EmptyBlockLightMask: []byte{0xff, 0xff, 0xff, 0x03},
		SkyLight: func() [][]byte {
			l := make([][]byte, 26)
			for i := range l {
				l[i] = make([]byte, 2048)
			}
			return l
		}(),
	}
}

func TestChunkEncodeAllocs(t *testing.T) {
	p := chunkPacket()
	w := proto.Writer{B: make([]byte, 0, 128<<10)}
	allocs := testing.AllocsPerRun(100, func() {
		w.Reset()
		Marshal(&w, p)
	})
	if allocs != 0 {
		t.Errorf("chunk encode allocs = %v, want 0", allocs)
	}
	mv := &ClientboundMoveEntityPosRot{EntityID: 1, Delta: proto.MoveDelta{DX: 5}, Yaw: 1}
	if a := testing.AllocsPerRun(100, func() { w.Reset(); Marshal(&w, mv) }); a != 0 {
		t.Errorf("move encode allocs = %v", a)
	}
	var in ServerboundMovePlayerPosRot
	var bw proto.Writer
	(&ServerboundMovePlayerPosRot{X: 1, Y: 2, Z: 3}).Encode(&bw)
	var r proto.Reader
	if a := testing.AllocsPerRun(100, func() { r.B, r.Off = bw.B, 0; _ = DecodeBody(&in, &r) }); a != 0 {
		t.Errorf("move decode allocs = %v", a)
	}
}

func BenchmarkEncodeChunk(b *testing.B) {
	p := chunkPacket()
	w := proto.Writer{B: make([]byte, 0, 128<<10)}
	b.ReportAllocs()
	for b.Loop() {
		w.Reset()
		Marshal(&w, p)
	}
	b.SetBytes(int64(len(w.B)))
}

func BenchmarkEncodeChunkFramed(b *testing.B) {
	p := chunkPacket()
	w := proto.Writer{B: make([]byte, 0, 128<<10)}
	fw := proto.NewFrameWriter(discard{})
	fw.SetCompression(256, 6)
	b.ReportAllocs()
	for b.Loop() {
		w.Reset()
		Marshal(&w, p)
		if err := fw.WriteFrame(w.B); err != nil {
			b.Fatal(err)
		}
		fw.Flush()
	}
	b.SetBytes(int64(len(w.B)))
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func BenchmarkEncodeMoveEntity(b *testing.B) {
	p := &ClientboundMoveEntityPosRot{EntityID: 1234, Delta: proto.MoveDelta{DX: 100, DY: -3, DZ: 7, OnGround: true}, Yaw: 12, Pitch: 3}
	w := proto.Writer{B: make([]byte, 0, 64)}
	b.ReportAllocs()
	for b.Loop() {
		w.Reset()
		Marshal(&w, p)
	}
}

func BenchmarkDecodeMovePlayer(b *testing.B) {
	var bw proto.Writer
	(&ServerboundMovePlayerPosRot{X: 1, Y: 2, Z: 3, Yaw: 4, Pitch: 5, Flags: 1}).Encode(&bw)
	var p ServerboundMovePlayerPosRot
	var r proto.Reader
	b.ReportAllocs()
	for b.Loop() {
		r.B, r.Off = bw.B, 0
		if err := DecodeBody(&p, &r); err != nil {
			b.Fatal(err)
		}
	}
}
