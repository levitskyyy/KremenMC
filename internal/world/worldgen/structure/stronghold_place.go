package structure

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// Stronghold piece placement: recorded ops (stronghold_ops.txt.gz, from
// TestShOpsGen) of every piece variant facing north, from a full-clip run
// of the black-box probe kremenmc.probe.temple.TScene on solid stone with
// every chance write forced; other orientations use the legacy frame. The
// op semantics were fitted on a scrambled scene (generateBox reads skip
// air, cave air included; everything else is replaced) and a per-chunk
// run (reads, and the draws of read-checked boxes, happen only inside the
// chunk; always-replace selector boxes, torch and cobweb chances and the
// twelve eye draws draw everywhere). Thresholds (forced-float probes):
// stone brick selector f < 0.2 cracked, < 0.5 mossy, < 0.55 infested,
// else plain; torch f < 0.1; cobweb f <= 0.07; eye f > 0.9.

//go:embed stronghold_ops.txt.gz
var shOpsGz []byte

type shOp struct {
	kind       byte
	frame      int8
	x, y, z    int32
	s          data.StateID
	loot       string
	door, mark bool
}

var shOpTable = sync.OnceValues(func() (map[string][]shOp, error) {
	zr, err := gzip.NewReader(bytes.NewReader(shOpsGz))
	if err != nil {
		return nil, fmt.Errorf("stronghold ops: %w", err)
	}
	reg := data.Canonical()
	out := map[string][]shOp{}
	var cur string
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		l := sc.Text()
		if k, ok := strings.CutPrefix(l, "PIECE "); ok {
			cur = k
			continue
		}
		f := strings.Fields(l)
		o := shOp{kind: f[0][0]}
		if o.kind == 'F' {
			var i int8
			fmt.Sscan(f[0][1:], &i)
			o.frame = i
		}
		if len(f) >= 4 {
			fmt.Sscan(f[1], &o.x)
			fmt.Sscan(f[2], &o.y)
			fmt.Sscan(f[3], &o.z)
		}
		for _, a := range f[min(len(f), 4):] {
			switch {
			case a == "d":
				o.door = true
			case a == "m":
				o.mark = true
			case o.kind == 'K':
				o.loot = a
			default:
				st, ok := reg.ParseState(a)
				if !ok {
					return nil, fmt.Errorf("stronghold ops: bad state %s", a)
				}
				o.s = st
			}
		}
		out[cur] = append(out[cur], o)
	}
	return out, sc.Err()
})

func (p *shPiece) variant() string {
	b := func(v bool) string {
		if v {
			return "1b"
		}
		return "0b"
	}
	n := strings.TrimPrefix(shIDs[p.kind], "minecraft:")
	switch p.kind {
	case shStraight:
		return n + " Left=" + b(p.left) + ",Right=" + b(p.right)
	case shChest:
		return n + " Chest=0b"
	case shFiller:
		return fmt.Sprintf("%s Steps=%d", n, p.steps)
	case shStairs:
		return n + " Source=0b"
	case shStart:
		return n + " Source=1b"
	case shFive:
		return n + " leftLow=" + b(p.leftLow) + ",leftHigh=" + b(p.leftHigh) + ",rightLow=" + b(p.rightLow) + ",rightHigh=" + b(p.rightHigh)
	case shRoom:
		return fmt.Sprintf("%s Type=%d", n, p.roomType)
	case shLibrary:
		return n + " Tall=" + b(p.tall)
	case shPortal:
		return n + " Mob=0b"
	case shLeft, shRight:
		// The turns place their side opening by orientation (recorded
		// facing north and south; west follows south, east north).
		if p.dir == msSouth || p.dir == msWest {
			return n + " S"
		}
	}
	return n
}

// shPieceOps is the piece's recorded op list with its entry door (the
// door ops on the entrance face, z 0) replaced by its door type's.
func shPieceOps(p *shPiece) ([]shOp, error) {
	t, err := shOpTable()
	if err != nil {
		return nil, err
	}
	ops, ok := t[p.variant()]
	if !ok {
		return nil, fmt.Errorf("stronghold: no ops for %s", p.variant())
	}
	if p.door == shOpening || p.kind == shPortal {
		return ops, nil
	}
	start, end := -1, -1
	for i, o := range ops {
		if o.door && o.z == 0 {
			if start < 0 {
				start = i
			}
			end = i + 1
		} else if start >= 0 {
			break
		}
	}
	if start < 0 {
		return ops, nil
	}
	ox, oy := ops[start].x, ops[start].y
	for _, o := range ops[start:end] {
		ox, oy = min(ox, o.x), min(oy, o.y)
	}
	// The door recordings are left turns with their entry door at 1,1,0.
	out := make([]shOp, 0, len(ops)+16)
	out = append(out, ops[:start]...)
	for _, o := range t["door "+shDoorNames[p.door]] {
		if !o.door || o.z > 1 || o.z < -1 || o.y > 3 {
			continue
		}
		o.x += ox - 1
		o.y += oy - 1
		out = append(out, o)
	}
	return append(out, ops[end:]...), nil
}

var shStates = sync.OnceValue(func() (st struct{ cracked, mossy, infested, plain, chest, spawner data.StateID }) {
	reg := data.Canonical()
	d := func(n string) data.StateID { s, _ := reg.ParseState(n); return s }
	st.cracked, st.mossy, st.infested, st.plain = d("minecraft:cracked_stone_bricks"), d("minecraft:mossy_stone_bricks"), d("minecraft:infested_stone_bricks"), d("minecraft:stone_bricks")
	st.chest, st.spawner = d("minecraft:chest"), d("minecraft:spawner")
	return st
})

// shTrace, when set, receives every placement draw (tests).
var shTrace func(what string, v any)

func shDraw(r *rng.WorldgenRandom, what string) float32 {
	f := r.NextFloat()
	if shTrace != nil {
		shTrace(what, f)
	}
	return f
}

func shSelect(f float32) data.StateID {
	st := shStates()
	switch {
	case f < 0.2:
		return st.cracked
	case f < 0.5:
		return st.mossy
	case f < 0.55:
		return st.infested
	}
	return st.plain
}

func (p *shPiece) Place(lv Level, r *rng.WorldgenRandom, clip Box, cx, cz int32) error {
	ops, err := shPieceOps(p)
	if err != nil {
		return err
	}
	reg := data.Canonical()
	st := shStates()
	fr := newLegacyFrame(p.box, msDir2D[p.dir])
	var eyes [12]bool
	all := false
	set := func(x, y, z int32, s data.StateID, mark bool) {
		s = fr.state(s)
		lv.SetBlock(x, y, z, s)
		if mark {
			lv.MarkPostProcess(x, y, z)
		}
		if b := reg.BlockOf(s); b != nil && (b.Name == "lava" || b.Name == "water") {
			scheduleTick(lv, x, y, z, "minecraft:"+b.Name, 0, true)
		}
	}
	for _, o := range ops {
		x, y, z := fr.world(o.x, o.y, o.z)
		in := clip.Contains(x, y, z)
		switch o.kind {
		case 'A':
			if in {
				set(x, y, z, o.s, o.mark)
			}
		case 'C':
			if in && !reg.IsAir(lv.Block(x, y, z)) {
				set(x, y, z, o.s, o.mark)
			}
		case 'X':
			s := shSelect(shDraw(r, "X"))
			if in {
				set(x, y, z, s, false)
			}
		case 'Y':
			if in && !reg.IsAir(lv.Block(x, y, z)) {
				set(x, y, z, shSelect(shDraw(r, "Y")), false)
			}
		case 'M':
			if shDraw(r, "M") < 0.1 && in {
				set(x, y, z, o.s, o.mark)
			}
		case 'W':
			if shDraw(r, "W") <= 0.07 && in {
				set(x, y, z, o.s, o.mark)
			}
		case 'K':
			if !in || reg.BlockID(lv.Block(x, y, z)) == reg.BlockID(st.chest) || p.kind == shChest && p.chest {
				continue
			}
			face := reorientChest(lv, reg, x, y, z, "north")
			cs, _ := reg.ParseState("minecraft:chest[facing=" + face + ",type=single,waterlogged=false]")
			lv.SetBlock(x, y, z, cs)
			lv.SetBlockEntity(x, y, z, "minecraft:chest", nbt.Compound{"LootTable": o.loot, "LootTableSeed": r.NextLong()})
			if p.kind == shChest {
				p.chest = true
			}
		case 'E':
			all = true
			for i := range eyes {
				eyes[i] = shDraw(r, "E") > 0.9
				all = all && eyes[i]
			}
		case 'F':
			if in {
				s := o.s
				if eyes[o.frame] {
					s = withProp(s, "eye", "true")
				}
				set(x, y, z, s, o.mark)
			}
		case 'P':
			if all && in {
				set(x, y, z, o.s, o.mark)
			}
		case 'S':
			if in && !p.mob {
				p.mob = true
				lv.SetBlock(x, y, z, st.spawner)
				lv.SetBlockEntity(x, y, z, "minecraft:mob_spawner", nbt.Compound{
					"SpawnData": nbt.Compound{"entity": nbt.Compound{"id": "minecraft:silverfish"}}})
			}
		}
	}
	return nil
}
