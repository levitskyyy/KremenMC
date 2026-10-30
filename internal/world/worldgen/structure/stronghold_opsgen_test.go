package structure_test

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestShOpsGen (KREMENMC_SHOPS=log@spec@out) turns a full-clip
// kremenmc.probe.temple.TScene log of the recording scene (TestShSceneGen
// with spec, run with FORCEF so every chance write happens) into the
// stronghold op list: per piece variant its writes in piece-local
// coordinates (x = wx-minX, y = wy-minY, z = maxZ-wz facing north; key
// suffix " S" facing south, z = wz-minZ),
// classified by the reference call path. Kinds: A always, C unless air
// (generateBox read), X selector always, Y selector unless air, M torch
// chance (< 0.1), W cobweb chance (<= 0.07), K chest + loot table, E the
// twelve eye draws, F frame (eye index), P end portal block, S spawner; a
// trailing "d" marks the entry door's ops, "m" a post-process mark.
func TestShOpsGen(t *testing.T) {
	v := os.Getenv("KREMENMC_SHOPS")
	if v == "" {
		t.Skip("KREMENMC_SHOPS not set")
	}
	p := strings.Split(v, "@")
	specB, err := os.ReadFile(p[1])
	if err != nil {
		t.Fatal(err)
	}
	spec := strings.Split(strings.TrimSpace(string(specB)), ";")
	type pc struct {
		key              string
		minX, minZ, maxZ int32
		south            bool
		ops              []string
	}
	pcs := make([]*pc, len(spec))
	for i, s := range spec {
		f := strings.Split(s, ":")
		sz := shKindSizes[f[0]]
		if f[0] == "shfc" {
			var n int32
			fmt.Sscanf(f[3], "Steps=%d", &n)
			sz[2] = n
		}
		key := f[0]
		if len(f) > 3 {
			key += " " + f[3]
		}
		if f[0] == "shlt" && f[2] != "OPENING" || f[0] == "shlt" && i == len(spec)-4 {
			key = "door " + f[2]
		}
		south := f[1] == "0"
		if south {
			key += " S"
		}
		pcs[i] = &pc{key: key, minX: int32(i)*24 + 4, minZ: 4, maxZ: 4 + sz[2] - 1, south: south}
	}
	lootRe := regexp.MustCompile(`^BE (-?\d+) (-?\d+) (-?\d+) .*LootTable:"([^"]+)"`)
	f, err := os.Open(p[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var lines []string
	loot := map[string]string{}
	for sc.Scan() {
		l := sc.Text()
		if m := lootRe.FindStringSubmatch(l); m != nil {
			loot[m[1]+","+m[2]+","+m[3]] = m[4]
		}
		lines = append(lines, l)
	}
	path := func(l string) string {
		if i := strings.LastIndex(l, " @"); i >= 0 {
			return l[i+2:]
		}
		return ""
	}
	var pendRead, pend string
	eyes, frame := 0, 0
	for i, l := range lines {
		fs := strings.Fields(l)
		switch {
		case strings.HasPrefix(l, "W getBlockState") && strings.HasPrefix(path(l), "StructurePiece.getBlock<StructurePiece.generateBox"):
			pendRead = fs[2]
		case strings.HasPrefix(l, "R0 "):
			ph := path(l)
			switch {
			case strings.HasPrefix(ph, "SmoothStoneSelector.next"):
				pend = "sel"
			case strings.HasPrefix(ph, "StructurePiece.maybeGenerateBlock"):
				pend = "M"
			case strings.HasPrefix(ph, "StructurePiece.generateMaybeBox"):
				pend = "W"
			case strings.HasPrefix(ph, "PortalRoom.postProcess<StructureStart"):
				eyes++
				if eyes%12 == 0 {
					// attach to the piece of the next write
					pend = "E"
				}
			case strings.Contains(ph, "createChest"):
			default:
				t.Fatalf("unknown draw %s", l)
			}
		case strings.HasPrefix(l, "W setBlock"):
			var x, y, z int32
			fmt.Sscanf(fs[2], "%d,%d,%d", &x, &y, &z)
			pi := int(x / 24)
			pp := pcs[pi]
			lx, ly, lz := x-pp.minX, y, pp.maxZ-z
			if pp.south {
				lz = z - pp.minZ
			}
			st := "minecraft:" + fs[3]
			ph := path(l)
			kind := ""
			switch {
			case strings.HasPrefix(ph, "StructurePiece.createChest"):
				kind = "K"
				st = loot[fmt.Sprintf("%d,%d,%d", x, y, z)]
			case strings.HasPrefix(ph, "PortalRoom.postProcess"):
				kind = "S"
			case strings.HasPrefix(fs[3], "end_portal_frame"):
				kind = fmt.Sprintf("F%d", frame%12)
				frame++
				st = strings.Replace(st, "eye=true", "eye=false", 1)
			case fs[3] == "end_portal":
				kind = "P"
			case pend == "E":
				t.Fatalf("eye draws not followed by a frame: %s", l)
			case pend == "sel":
				kind, st = "X", ""
				if pendRead == fs[2] {
					kind = "Y"
				}
			case pend == "M" || pend == "W":
				kind = pend
			default:
				kind = "A"
				if pendRead == fs[2] {
					kind = "C"
				}
			}
			if pend == "E" {
				pp.ops = append(pp.ops, "E")
			}
			pend, pendRead = "", ""
			op := fmt.Sprintf("%s %d %d %d", kind, lx, ly, lz)
			if st != "" {
				op += " " + st
			}
			if strings.Contains(ph, "generateSmallDoor") {
				op += " d"
			}
			if i+2 < len(lines) && (strings.HasPrefix(lines[i+1], "W getChunk "+fs[2]+" ") || strings.HasPrefix(lines[i+2], "W getChunk "+fs[2]+" ")) {
				op += " m"
			}
			pp.ops = append(pp.ops, op)
		}
	}
	out, err := os.Create(p[2])
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	zw := gzip.NewWriter(out)
	defer zw.Close()
	for _, pp := range pcs {
		fmt.Fprintf(zw, "PIECE %s\n", pp.key)
		for _, o := range pp.ops {
			fmt.Fprintln(zw, o)
		}
		t.Logf("%s: %d ops", pp.key, len(pp.ops))
	}
}
