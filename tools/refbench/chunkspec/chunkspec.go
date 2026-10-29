// Package chunkspec parses chunk set specifications for the reference bench.
//
// A spec is a '+'-separated list of terms; the result keeps first-seen order
// and drops duplicates:
//
//	border                corner:0,0 + corner:512,-512 (chunk and region borders)
//	spawn                 corner at the world spawn, radius 3 (needs the spawn)
//	far                   corner:30000,100000 + corner:-30000,-100000 + corner:1000000,1000000
//	all                   border + spawn + far
//	corner:BX,BZ[,R]      chunks c-R..c+R-1 on both axes around the chunk c holding block BX,BZ (R defaults to 2)
//	rect:X0,Z0,X1,Z1      chunk rectangle, inclusive
//	chunk:X,Z             one chunk
//	spiral:BX,BZ,R        chunks within Chebyshev radius R of the chunk holding block BX,BZ, ring by ring
//	                      (the order the old vanilla worldgen backend pregenerated in)
//
// Rectangles are listed row by row (z outer, x inner).
package chunkspec

import (
	"fmt"
	"strconv"
	"strings"
)

type Pos struct{ X, Z int32 }

func (p Pos) String() string { return fmt.Sprintf("%d,%d", p.X, p.Z) }

var presets = map[string]string{
	"border": "corner:0,0+corner:512,-512",
	"far":    "corner:30000,100000+corner:-30000,-100000+corner:1000000,1000000",
	"all":    "border+spawn+far",
}

// NeedsSpawn reports whether resolving spec requires the world spawn.
func NeedsSpawn(spec string) bool {
	for _, t := range expand(spec, 0) {
		if t == "spawn" {
			return true
		}
	}
	return false
}

func expand(spec string, depth int) []string {
	var out []string
	for t := range strings.SplitSeq(spec, "+") {
		t = strings.TrimSpace(t)
		if p, ok := presets[t]; ok && depth < 4 {
			out = append(out, expand(p, depth+1)...)
		} else if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Parse resolves spec. spawn is the world spawn in block coordinates; it is
// only used (and required) by the spawn term.
func Parse(spec string, spawn *[2]int) ([]Pos, error) {
	var out []Pos
	seen := map[Pos]bool{}
	add := func(p Pos) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	rect := func(x0, z0, x1, z1 int32) {
		for z := z0; z <= z1; z++ {
			for x := x0; x <= x1; x++ {
				add(Pos{x, z})
			}
		}
	}
	for _, t := range expand(spec, 0) {
		kind, args, _ := strings.Cut(t, ":")
		var n []int64
		if args != "" {
			for a := range strings.SplitSeq(args, ",") {
				v, err := strconv.ParseInt(strings.TrimSpace(a), 10, 32)
				if err != nil {
					return nil, fmt.Errorf("chunkspec: %q: %w", t, err)
				}
				n = append(n, v)
			}
		}
		switch {
		case kind == "spawn" && len(n) == 0:
			if spawn == nil {
				return nil, fmt.Errorf("chunkspec: %q needs the world spawn", t)
			}
			cx, cz := int32(spawn[0])>>4, int32(spawn[1])>>4
			rect(cx-3, cz-3, cx+2, cz+2)
		case kind == "corner" && (len(n) == 2 || len(n) == 3):
			r := int64(2)
			if len(n) == 3 {
				r = n[2]
			}
			if r < 1 || r > 64 {
				return nil, fmt.Errorf("chunkspec: %q: radius out of range 1-64", t)
			}
			cx, cz := int32(n[0])>>4, int32(n[1])>>4
			rect(cx-int32(r), cz-int32(r), cx+int32(r)-1, cz+int32(r)-1)
		case kind == "rect" && len(n) == 4:
			x0, z0, x1, z1 := int32(n[0]), int32(n[1]), int32(n[2]), int32(n[3])
			if x1 < x0 || z1 < z0 || int64(x1-x0+1)*int64(z1-z0+1) > 1<<16 {
				return nil, fmt.Errorf("chunkspec: %q: bad rectangle", t)
			}
			rect(x0, z0, x1, z1)
		case kind == "spiral" && len(n) == 3:
			if n[2] < 0 || n[2] > 128 {
				return nil, fmt.Errorf("chunkspec: %q: radius out of range 0-128", t)
			}
			cx, cz := int32(n[0])>>4, int32(n[1])>>4
			for _, o := range Spiral(int(n[2])) {
				add(Pos{cx + o[0], cz + o[1]})
			}
		case kind == "chunk" && len(n) == 2:
			add(Pos{int32(n[0]), int32(n[1])})
		default:
			return nil, fmt.Errorf("chunkspec: unknown term %q", t)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("chunkspec: %q selects no chunks", spec)
	}
	return out, nil
}

// Spiral returns chunk offsets ring by ring, so the first (2r+1)^2 entries
// of any larger spiral cover radius r.
func Spiral(radius int) [][2]int32 {
	out := make([][2]int32, 0, (2*radius+1)*(2*radius+1))
	out = append(out, [2]int32{0, 0})
	for r := int32(1); r <= int32(radius); r++ {
		for x := -r; x <= r; x++ {
			out = append(out, [2]int32{x, -r})
		}
		for z := -r + 1; z <= r; z++ {
			out = append(out, [2]int32{r, z})
		}
		for x := r - 1; x >= -r; x-- {
			out = append(out, [2]int32{x, r})
		}
		for z := r - 1; z > -r; z-- {
			out = append(out, [2]int32{-r, z})
		}
	}
	return out
}
