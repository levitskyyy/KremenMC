package server

import (
	"math"
	"math/rand/v2"
	"strings"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
)

// otherHalf is the matching half of a two-block plant or door at pos.
func otherHalf(w *World, pos api.BlockPos, st data.StateID) (api.BlockPos, data.StateID, bool) {
	reg := data.Canonical()
	h, ok := reg.Value(st, "half")
	if !ok || h != "upper" && h != "lower" {
		return pos, 0, false
	}
	q := pos.Add(0, 1, 0)
	want := "upper"
	if h == "upper" {
		q, want = pos.Add(0, -1, 0), "lower"
	}
	o := w.w.Block(q.X, q.Y, q.Z)
	if data.BlockOf(o) != data.BlockOf(st) {
		return q, o, false
	}
	if v, _ := reg.Value(o, "half"); v != want {
		return q, o, false
	}
	return q, o, true
}

// useBlock toggles hand-operated openables (doors, trapdoors, fence gates)
// and levers; it reports whether the click was consumed.
func (p *Player) useBlock(w *World, pos api.BlockPos) bool {
	st := w.w.Block(pos.X, pos.Y, pos.Z)
	b := data.BlockOf(st)
	if b == nil {
		return false
	}
	reg := data.Canonical()
	name := b.Name
	switch {
	case name == "lever":
		on := propBool(reg, st, "powered")
		ns, _ := data.With(st, "powered", boolStr(!on))
		w.w.SetBlock(pos.X, pos.Y, pos.Z, ns)
		pitch := float32(0.5)
		if !on {
			pitch = 0.6
		}
		w.playSoundAt(center(pos), "block.lever.click", 4, 0.3, pitch)
		return true
	case name == "iron_door" || name == "iron_trapdoor":
		return false
	case strings.HasSuffix(name, "_trapdoor"), strings.HasSuffix(name, "_door"), strings.HasSuffix(name, "_fence_gate"):
	default:
		return false
	}
	open := !propBool(reg, st, "open")
	ns, ok := data.With(st, "open", boolStr(open))
	if !ok {
		return false
	}
	if open && strings.HasSuffix(name, "_fence_gate") {
		look := horizontal[int(math.Floor(float64(p.Location().Yaw)/90+0.5))&3]
		if f, _ := reg.Value(ns, "facing"); f == oppositeDir(look) {
			ns, _ = data.With(ns, "facing", look)
		}
	}
	if q, o, ok := otherHalf(w, pos, st); ok {
		if os, ok := data.With(o, "open", boolStr(open)); ok {
			w.w.SetBlock(q.X, q.Y, q.Z, os)
		}
	}
	w.w.SetBlock(pos.X, pos.Y, pos.Z, ns)
	w.playSoundAt(center(pos), openSound(name, open), 4, 1, rand.Float32()*0.1+0.9)
	return true
}

func openSound(name string, open bool) string {
	kind := "door"
	switch {
	case strings.HasSuffix(name, "_trapdoor"):
		kind = "trapdoor"
	case strings.HasSuffix(name, "_fence_gate"):
		kind = "fence_gate"
	}
	wood := "wooden_"
	switch {
	case strings.Contains(name, "copper"):
		wood = "copper_"
	case strings.HasPrefix(name, "cherry_"):
		wood = "cherry_wood_"
	case strings.HasPrefix(name, "bamboo_"):
		wood = "bamboo_wood_"
	case strings.HasPrefix(name, "crimson_"), strings.HasPrefix(name, "warped_"):
		wood = "nether_wood_"
	}
	if kind == "fence_gate" && wood == "wooden_" {
		wood = ""
	}
	act := ".close"
	if open {
		act = ".open"
	}
	s := "block." + wood + kind + act
	if soundID(s) < 0 {
		if kind == "fence_gate" {
			return "block.fence_gate" + act
		}
		return "block.wooden_" + kind + act
	}
	return s
}

func propBool(reg *data.Registry, st data.StateID, prop string) bool {
	v, _ := reg.Value(st, prop)
	return v == "true"
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func oppositeDir(d string) string {
	switch d {
	case "north":
		return "south"
	case "south":
		return "north"
	case "east":
		return "west"
	}
	return "east"
}

func center(p api.BlockPos) api.Vec3 {
	return api.Vec3{X: float64(p.X) + 0.5, Y: float64(p.Y) + 0.5, Z: float64(p.Z) + 0.5}
}
