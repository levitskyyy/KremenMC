package server

import (
	"fmt"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/bridge"
)

func (h BridgeHost) sender(f bridge.Fields, k string) api.CommandSender {
	if u, ok := f.UUID(k); ok {
		if p, ok := h.s.Player(api.UUID(u)); ok {
			return p
		}
	}
	return h.s.console
}

func (h BridgeHost) player(f bridge.Fields) (*Player, bool) {
	u, ok := f.UUID("player")
	if !ok {
		return nil, false
	}
	p, ok := h.s.Player(api.UUID(u))
	if !ok {
		return nil, false
	}
	return p.(*Player), true
}

// Query answers read-only lookups from Java plugins.
func (h BridgeHost) Query(kind string, f bridge.Fields) (any, error) {
	switch kind {
	case bridge.QuerySelect:
		es, err := h.s.cmds.Select(h.sender(f, "sender"), f.String("input"))
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, len(es))
		for _, e := range es {
			out = append(out, bridge.UUID(e.UUID()))
		}
		return out, nil
	case bridge.QueryPlayerState:
		p, ok := h.player(f)
		if !ok {
			return nil, nil
		}
		return map[string]any{
			"health": p.Health(), "maxHealth": p.MaxHealth(), "food": p.Food(), "saturation": float64(p.Saturation()),
			"exp": float64(p.Exp()), "level": p.Level(), "flying": p.Flying(), "allowFlight": p.AllowFlight(),
		}, nil
	case bridge.QueryTPS:
		return map[string]any{"tps": h.s.TPS(), "mspt": h.s.MSPT()}, nil
	}
	if r, ok, err := h.worldQuery(kind, f); ok {
		return r, err
	}
	return nil, fmt.Errorf("unknown query %q", kind)
}

// Action applies mutations requested by Java plugins. State changes run as
// player tasks on the owning tick context.
func (h BridgeHost) Action(kind string, f bridge.Fields) {
	if h.worldAction(kind, f) {
		return
	}
	p, ok := h.player(f)
	if !ok {
		return
	}
	switch kind {
	case bridge.ActionPlayerSet:
		field, v := f.String("field"), f
		p.tasks.add(pending{t: h.s.sched.newTask("bridge", false), run: func(*task) { setPlayerField(p, field, v) }})
	case bridge.ActionTitle:
		if f.Bool("clear") {
			p.ClearTitle()
			return
		}
		p.ShowTitle(api.Title{
			Title: componentFromJSON(f.String("title")), Subtitle: componentFromJSON(f.String("subtitle")),
			FadeIn: api.Ticks(f.Int("fadeIn")), Stay: api.Ticks(f.Int("stay")), FadeOut: api.Ticks(f.Int("fadeOut")),
		})
	case bridge.ActionActionBar:
		p.SendActionBar(componentFromJSON(f.String("text")))
	case bridge.ActionSound:
		snd := api.Sound{Name: api.Key(f.String("name")), Category: f.String("category"),
			Volume: float32(f.Float("volume")), Pitch: float32(f.Float("pitch")), Seed: f.Int("seed")}
		if f.Has("x") {
			snd.At = &api.Vec3{X: f.Float("x"), Y: f.Float("y"), Z: f.Float("z")}
		}
		p.PlaySound(snd)
	default:
		h.s.log.Debug("unknown bridge action", "kind", kind)
	}
}

func setPlayerField(p *Player, field string, v bridge.Fields) {
	switch field {
	case "health":
		p.SetHealth(v.Float("value"))
	case "food":
		p.SetFood(int(v.Int("value")))
	case "saturation":
		p.SetSaturation(float32(v.Float("value")))
	case "exp":
		p.SetExp(int(v.Int("level")), float32(v.Float("progress")))
	case "gameMode":
		for g := api.Survival; g <= api.Spectator; g++ {
			if gameModeName(g) == v.String("value") {
				p.SetGameMode(g)
			}
		}
	case "flying":
		p.SetFlying(v.Bool("value"))
	case "allowFlight":
		p.SetAllowFlight(v.Bool("value"))
	}
}
