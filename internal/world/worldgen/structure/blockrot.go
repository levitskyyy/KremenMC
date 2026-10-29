package structure

import (
	"strconv"

	"github.com/levitskyyy/KremenMC/internal/data"
)

var horiz = [4]string{"north", "east", "south", "west"}

func horizIndex(s string) int {
	for i, h := range horiz {
		if h == s {
			return i
		}
	}
	return -1
}

func mirrorDir(s string, m Mirror) string {
	switch {
	case m == MirrorLeftRight && s == "north":
		return "south"
	case m == MirrorLeftRight && s == "south":
		return "north"
	case m == MirrorFrontBack && s == "east":
		return "west"
	case m == MirrorFrontBack && s == "west":
		return "east"
	}
	return s
}

func rotDir(s string, rot Rotation) string {
	i := horizIndex(s)
	if i < 0 {
		return s
	}
	return horiz[(i+int(rot))&3]
}

// TransformState applies mirror then rotation to the directional properties
// of s: facing, axis, rotation (16 steps), and north/east/south/west
// connection properties.
func TransformState(s data.StateID, m Mirror, rot Rotation) data.StateID {
	if m == MirrorNone && rot == RotNone {
		return s
	}
	reg := data.Canonical()
	b := reg.BlockOf(s)
	if b == nil || len(b.Props) == 0 {
		return s
	}
	get := func(name string) (string, bool) {
		for i := range b.Props {
			if b.Props[i].Name == name {
				return b.Props[i].Values[b.ValueIndex(s, i)], true
			}
		}
		return "", false
	}
	vals := map[string]string{}
	if f, ok := get("facing"); ok {
		vals["facing"] = rotDir(mirrorDir(f, m), rot)
	}
	if a, ok := get("axis"); ok && (rot == Rot90 || rot == Rot270) {
		switch a {
		case "x":
			vals["axis"] = "z"
		case "z":
			vals["axis"] = "x"
		}
	}
	if r, ok := get("rotation"); ok {
		n, _ := strconv.Atoi(r)
		switch m {
		case MirrorLeftRight:
			n = (16 - n) & 15
		case MirrorFrontBack:
			n = (24 - n) & 15
		}
		n = (n + 4*int(rot)) & 15
		vals["rotation"] = strconv.Itoa(n)
	}
	if _, ok := get("north"); ok {
		var old [4]string
		for i, h := range horiz {
			old[i], _ = get(h)
		}
		for i, h := range horiz {
			nd := rotDir(mirrorDir(h, m), rot)
			vals[nd] = old[i]
		}
	}
	if h, ok := get("hinge"); ok && m != MirrorNone {
		if h == "left" {
			vals["hinge"] = "right"
		} else {
			vals["hinge"] = "left"
		}
	}
	if sh, ok := get("shape"); ok && m != MirrorNone {
		switch sh {
		case "inner_left":
			vals["shape"] = "inner_right"
		case "inner_right":
			vals["shape"] = "inner_left"
		case "outer_left":
			vals["shape"] = "outer_right"
		case "outer_right":
			vals["shape"] = "outer_left"
		}
	}
	out := s
	for i := range b.Props {
		if v, ok := vals[b.Props[i].Name]; ok {
			if vi := b.Props[i].ValueIndex(v); vi >= 0 {
				out = b.WithIndex(out, i, vi)
			}
		}
	}
	return out
}
