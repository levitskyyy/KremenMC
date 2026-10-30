package server

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/bridge"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

func TestBridgePacket(t *testing.T) {
	alice := bridge.UUID{1, 2, 3}
	red := `{"text":"Red","color":"#FF0000"}`
	tests := []struct {
		kind  string
		f     bridge.Fields
		check func(t *testing.T, p packets.Packet)
	}{
		{bridge.PacketTabList, bridge.Fields{"header": red, "footer": nil}, func(t *testing.T, p packets.Packet) {
			m := p.(*packets.ClientboundTabList)
			if m.Header.Text != "Red" || m.Header.Color != "#FF0000" || m.Footer.Text != "" {
				t.Fatalf("%+v", m)
			}
		}},
		{bridge.PacketPlayerInfoUpdate, bridge.Fields{"actions": int64(0x21), "entries": []any{map[string]any{
			"uuid": alice, "name": "Alice", "listed": true, "displayName": red,
			"properties": []any{map[string]any{"name": "textures", "value": "v", "signature": "s"}},
		}}}, func(t *testing.T, p packets.Packet) {
			m := p.(*packets.ClientboundPlayerInfoUpdate)
			e := m.Data[0]
			if m.Action != 0x21 || e.UUID != alice || e.Player.Name != "Alice" || !e.DisplayName.Ok || len(e.Player.Properties) != 1 || e.Player.Properties[0].Signature.V != "s" {
				t.Fatalf("%+v", m)
			}
		}},
		{bridge.PacketPlayerInfoRemove, bridge.Fields{"uuids": []any{alice}}, func(t *testing.T, p packets.Packet) {
			if m := p.(*packets.ClientboundPlayerInfoRemove); len(m.Players) != 1 || m.Players[0] != alice {
				t.Fatalf("%+v", m)
			}
		}},
		{bridge.PacketSetObjective, bridge.Fields{"name": "o", "method": int64(0), "displayName": red, "renderType": int64(1),
			"numberFormat": map[string]any{"type": int64(2), "value": red}}, func(t *testing.T, p packets.Packet) {
			m := p.(*packets.ClientboundSetObjective)
			if m.Name != "o" || m.Type != 1 || m.NumberFormat != proto.Some(int32(2)) || m.Styling.Value.IsEmpty() {
				t.Fatalf("%+v", m)
			}
		}},
		{bridge.PacketSetDisplayObjective, bridge.Fields{"slot": int64(1), "name": "o"}, func(t *testing.T, p packets.Packet) {
			if m := p.(*packets.ClientboundSetDisplayObjective); m.Position != 1 || m.Name != "o" {
				t.Fatalf("%+v", m)
			}
		}},
		{bridge.PacketSetScore, bridge.Fields{"owner": "Alice", "objective": "o", "score": int64(7), "numberFormat": map[string]any{"type": int64(0)}}, func(t *testing.T, p packets.Packet) {
			if m := p.(*packets.ClientboundSetScore); m.Value != 7 || m.DisplayName.Ok || m.NumberFormat != proto.Some(int32(0)) {
				t.Fatalf("%+v", m)
			}
		}},
		{bridge.PacketResetScore, bridge.Fields{"owner": "Alice", "objective": nil}, func(t *testing.T, p packets.Packet) {
			if m := p.(*packets.ClientboundResetScore); m.EntityName != "Alice" || m.ObjectiveName.Ok {
				t.Fatalf("%+v", m)
			}
		}},
		{bridge.PacketSetPlayerTeam, bridge.Fields{"name": "t", "method": int64(0), "players": []any{"Alice"},
			"params": map[string]any{"prefix": red, "visibility": int64(1), "collision": int64(2), "color": int64(-1), "options": int64(3)}}, func(t *testing.T, p packets.Packet) {
			m := p.(*packets.ClientboundSetPlayerTeam)
			if m.Team != "t" || m.Players[0] != "Alice" || m.Parameters.Prefix.Text != "Red" || m.Parameters.Color.Ok || m.Parameters.Flags != 3 || m.Parameters.CollisionRule != 2 {
				t.Fatalf("%+v", m)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			p := bridgePacket(tt.kind, tt.f)
			if p == nil {
				t.Fatal("nil packet")
			}
			tt.check(t, p)
			var w proto.Writer
			packets.Marshal(&w, p)
			back, err := packets.Unmarshal(packets.Play, packets.Clientbound, w.B)
			if err != nil {
				t.Fatalf("roundtrip: %v", err)
			}
			tt.check(t, back)
		})
	}
	if bridgePacket("unknown", nil) != nil {
		t.Fatal("unknown kind built a packet")
	}
}
