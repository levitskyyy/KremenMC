package server

import (
	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/bridge"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// SendPacket sends a packet that a Java plugin built through the bridge's
// NMS layer, such as tab list, scoreboard and team updates.
func (h BridgeHost) SendPacket(to bridge.UUID, kind string, f bridge.Fields) {
	p, ok := h.s.Player(api.UUID(to))
	if !ok {
		return
	}
	pk := bridgePacket(kind, f)
	if pk == nil {
		h.s.log.Debug("bridge packet dropped", "kind", kind)
		return
	}
	p.(*Player).send(pk)
}

func bridgePacket(kind string, f bridge.Fields) packets.Packet {
	switch kind {
	case bridge.PacketTabList:
		return &packets.ClientboundTabList{Header: bridgeText(f, "header"), Footer: bridgeText(f, "footer")}
	case bridge.PacketPlayerInfoUpdate:
		pk := &packets.ClientboundPlayerInfoUpdate{Action: packets.ClientboundPlayerInfoUpdateAction(f.Int("actions"))}
		for _, v := range f.List("entries") {
			e := bridge.Fields(bridge.ArgMap([]any{v}, 0))
			u, _ := e.UUID("uuid")
			en := packets.ClientboundPlayerInfoUpdateDataEntry{
				UUID:         u,
				Player:       packets.GameProfileNameProp{Name: e.String("name")},
				Gamemode:     int32(e.Int("gameMode")),
				Listed:       e.Bool("listed"),
				Latency:      int32(e.Int("latency")),
				DisplayName:  bridgeOptText(e, "displayName"),
				ListPriority: int32(e.Int("listOrder")),
				ShowHat:      e.Bool("showHat"),
			}
			for _, pv := range e.List("properties") {
				pf := bridge.Fields(bridge.ArgMap([]any{pv}, 0))
				gp := packets.GameProfileProperty{Name: pf.String("name"), Value: pf.String("value")}
				if s := pf.String("signature"); s != "" {
					gp.Signature = proto.Some(s)
				}
				en.Player.Properties = append(en.Player.Properties, gp)
			}
			pk.Data = append(pk.Data, en)
		}
		return pk
	case bridge.PacketPlayerInfoRemove:
		pk := &packets.ClientboundPlayerInfoRemove{}
		for _, v := range f.List("uuids") {
			if u, ok := v.(bridge.UUID); ok {
				pk.Players = append(pk.Players, u)
			}
		}
		return pk
	case bridge.PacketSetObjective:
		pk := &packets.ClientboundSetObjective{
			Name: f.String("name"), Action: int8(f.Int("method")),
			DisplayText: bridgeText(f, "displayName"), Type: int32(f.Int("renderType")),
		}
		pk.NumberFormat, pk.Styling.Value = bridgeNumberFormat(f.Map("numberFormat"))
		return pk
	case bridge.PacketSetDisplayObjective:
		return &packets.ClientboundSetDisplayObjective{Position: int32(f.Int("slot")), Name: f.String("name")}
	case bridge.PacketSetScore:
		pk := &packets.ClientboundSetScore{
			ItemName: f.String("owner"), ScoreName: f.String("objective"), Value: int32(f.Int("score")),
			DisplayName: bridgeOptText(f, "displayName"),
		}
		pk.NumberFormat, pk.Styling = bridgeNumberFormat(f.Map("numberFormat"))
		return pk
	case bridge.PacketResetScore:
		pk := &packets.ClientboundResetScore{EntityName: f.String("owner")}
		if f.Has("objective") {
			pk.ObjectiveName = proto.Some(f.String("objective"))
		}
		return pk
	case bridge.PacketSetPlayerTeam:
		pk := &packets.ClientboundSetPlayerTeam{Team: f.String("name"), Mode: int8(f.Int("method"))}
		for _, v := range f.List("players") {
			if s, ok := v.(string); ok {
				pk.Players = append(pk.Players, s)
			}
		}
		if t := f.Map("params"); t != nil {
			pk.Parameters = packets.TeamParameters{
				Name: bridgeText(t, "displayName"), Prefix: bridgeText(t, "prefix"), Suffix: bridgeText(t, "suffix"),
				NameTagVisibility: int32(t.Int("visibility")), CollisionRule: int32(t.Int("collision")),
				Flags: packets.TeamParametersFlags(t.Int("options")),
			}
			if c := t.Int("color"); c >= 0 {
				pk.Parameters.Color = proto.Some(int32(c))
			}
		}
		return pk
	}
	return nil
}

func bridgeText(f bridge.Fields, k string) proto.Text {
	var t proto.Text
	if s := f.String(k); s != "" {
		if err := t.UnmarshalJSON([]byte(s)); err != nil {
			t = proto.Text{Text: s}
		}
	}
	return t
}

func bridgeOptText(f bridge.Fields, k string) proto.Opt[proto.Text] {
	if !f.Has(k) {
		return proto.Opt[proto.Text]{}
	}
	return proto.Some(bridgeText(f, k))
}

// bridgeNumberFormat returns the number format id (0 blank, 1 styled, 2 fixed) and its NBT payload.
func bridgeNumberFormat(nf bridge.Fields) (proto.Opt[int32], proto.RawNBT) {
	if nf == nil {
		return proto.Opt[int32]{}, nil
	}
	typ := int32(nf.Int("type"))
	if typ == 0 {
		return proto.Some(typ), nil
	}
	t := bridgeText(nf, "value")
	var w proto.Writer
	t.Encode(&w)
	return proto.Some(typ), proto.RawNBT(w.B)
}
