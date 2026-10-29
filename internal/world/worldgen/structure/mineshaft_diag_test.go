package structure_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/internal/world/gen"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/vanilla"
)

var msWorldSeeds = map[string]int64{"8266389061079214669-overworld": 8266389061079214669, "0-overworld": 0, "12345-overworld": 12345, "7-overworld": 7}

type msRefChunk struct {
	w      string
	cx, cz int32
	boxes  []structure.Box
	mesa   bool
	starts map[string]bool
}

// msRefChunks lists the full-status reference chunks that a mineshaft piece
// of the world intersects, with the boxes of those pieces.
func msRefChunks(t *testing.T, w string) []msRefChunk {
	ref := refStarts(t, "../../../../.refcache/26.3/worlds/"+w+"/world/dimensions/minecraft/overworld/region")
	byChunk := map[[2]int32]*msRefChunk{}
	for _, c := range ref {
		id := c["id"]
		if id != "minecraft:mineshaft" && id != "minecraft:mineshaft_mesa" {
			continue
		}
		for _, it := range c["Children"].(nbt.List).Items {
			bb := it.(nbt.Compound)["BB"].([]int32)
			b := structure.Box{MinX: bb[0], MinY: bb[1], MinZ: bb[2], MaxX: bb[3], MaxY: bb[4], MaxZ: bb[5]}
			for cx := b.MinX >> 4; cx <= b.MaxX>>4; cx++ {
				for cz := b.MinZ >> 4; cz <= b.MaxZ>>4; cz++ {
					k := [2]int32{cx, cz}
					e := byChunk[k]
					if e == nil {
						e = &msRefChunk{w: w, cx: cx, cz: cz, mesa: id == "minecraft:mineshaft_mesa", starts: map[string]bool{}}
						byChunk[k] = e
					}
					e.boxes = append(e.boxes, b)
					e.starts[fmt.Sprintf("%d,%d", c["ChunkX"], c["ChunkZ"])] = true
				}
			}
		}
	}
	var out []msRefChunk
	for _, e := range byChunk {
		_, root := refChunkMaybe(t, w, e.cx, e.cz)
		if root == nil || root["Status"] != "minecraft:full" {
			continue
		}
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].cx != out[j].cx {
			return out[i].cx < out[j].cx
		}
		return out[i].cz < out[j].cz
	})
	return out
}

// TestMineshaftDiag (KREMENMC_MSDIAG=world[:n]) compares every block of the
// mineshaft piece boxes (grown by one, down to the bottom for pillars) in
// full reference chunks with kremenmc.
func TestMineshaftDiag(t *testing.T) {
	spec := os.Getenv("KREMENMC_MSDIAG")
	if spec == "" {
		t.Skip("KREMENMC_MSDIAG not set")
	}
	w, lim := spec, 1000
	if i := strings.IndexByte(spec, ':'); i >= 0 {
		w = spec[:i]
		lim, _ = strconv.Atoi(spec[i+1:])
	}
	only := os.Getenv("KREMENMC_MSCHUNK")
	structure.RegisterMineshaftForTest()
	seed := msWorldSeeds[w]
	chunks := msRefChunks(t, w)
	t.Logf("%s: %d full chunks touch mineshafts", w, len(chunks))
	tr, _ := vanilla.NewTerrain("overworld", seed)
	stages, _ := tr.Stages("overworld", seed)
	p, _ := gen.New(gen.Config{Seed: seed, Dim: world.Overworld, Stages: stages, Logf: func(string, ...any) {}})
	reg := data.Canonical()
	tot, totSame := 0, 0
	var msCarts [2]int
	n := 0
	for _, c := range chunks {
		if only != "" && only != fmt.Sprintf("%d,%d", c.cx, c.cz) {
			continue
		}
		if n >= lim {
			break
		}
		n++
		rc, _ := refChunkMaybe(t, w, c.cx, c.cz)
		pc, err := p.Get(context.Background(), world.ChunkPos{X: c.cx, Z: c.cz}, gen.StatusFull)
		if err != nil {
			t.Fatal(err)
		}
		in := func(x, y, z int32) bool {
			for _, b := range c.boxes {
				if x >= b.MinX-1 && x <= b.MaxX+1 && z >= b.MinZ-1 && z <= b.MaxZ+1 && y >= -64 && y <= b.MaxY+1 {
					return true
				}
			}
			return false
		}
		same, total := 0, 0
		diff := map[string]int{}
		var ex []string
		for y := int32(-64); y < 120; y++ {
			for z := int32(0); z < 16; z++ {
				for x := int32(0); x < 16; x++ {
					if !in(c.cx<<4+x, y, c.cz<<4+z) {
						continue
					}
					ws, gs := rc.Block(int(x), int(y), int(z)), pc.Chunk.Block(int(x), int(y), int(z))
					wn, gn := reg.StateString(ws), reg.StateString(gs)
					if strings.HasPrefix(wn, "minecraft:oak_fence") && strings.HasPrefix(gn, "minecraft:oak_fence") || strings.HasPrefix(wn, "minecraft:dark_oak_fence") && strings.HasPrefix(gn, "minecraft:dark_oak_fence") {
						wn, gn = "fence", "fence"
					}
					if wn == gn && ws == gs || wn == gn {
						if ws != gs {
							total++
						}
						continue
					}
					total++
					msMat := func(n string) bool {
						switch strings.TrimPrefix(n, "dark_") {
						case "oak_planks", "oak_fence", "oak_log", "iron_chain", "rail", "cobweb", "wall_torch", "spawner":
							return true
						}
						return false
					}
					gname, wname := reg.BlockOf(gs).Name, reg.BlockOf(ws).Name
					inBox := false
					for _, b := range c.boxes {
						if b.Contains(c.cx<<4+x, y, c.cz<<4+z) {
							inBox = true
						}
					}
					airish := func(n string) bool { return n == "air" || n == "cave_air" }
					if !msMat(gname) && !msMat(wname) && !(inBox && airish(gname) != airish(wname)) {
						continue
					}
					diff[gname+"->"+wname]++
					if len(ex) < 40 || os.Getenv("KREMENMC_MSALL") != "" {
						ex = append(ex, fmt.Sprintf("%d,%d,%d kremenmc %s ref %s", c.cx<<4+x, y, c.cz<<4+z, strings.TrimPrefix(gn, "minecraft:"), strings.TrimPrefix(wn, "minecraft:")))
					}
				}
			}
		}
		_ = same
		// Chest minecarts: position and loot seed, kremenmc vs the reference
		// entity chunk.
		var gm, wm []string
		for _, e := range pc.Entities {
			if e["id"] == "minecraft:chest_minecart" {
				p := e["Pos"].(nbt.List).Items
				gm = append(gm, fmt.Sprint(p[0], " ", p[1], " ", p[2], " ", e["LootTableSeed"]))
			}
		}
		if er, err := anvil.OpenRegionReadOnly("../../../../.refcache/26.3/worlds/"+w+"/world/dimensions/minecraft/overworld/entities", c.cx>>5, c.cz>>5); err == nil {
			if b, err := er.Read(int(c.cx&31), int(c.cz&31)); err == nil {
				if _, root, _, err := nbt.Unmarshal(b, false); err == nil {
					for _, it := range listOf(root["Entities"]) {
						e := it.(nbt.Compound)
						if e["id"] == "minecraft:chest_minecart" {
							p := e["Pos"].(nbt.List).Items
							wm = append(wm, fmt.Sprint(p[0], " ", p[1], " ", p[2], " ", e["LootTableSeed"]))
						}
					}
				}
			}
			er.Close()
		}
		sort.Strings(gm)
		sort.Strings(wm)
		if len(gm)+len(wm) > 0 {
			msCarts[0] += len(wm)
			if fmt.Sprint(gm) == fmt.Sprint(wm) {
				msCarts[1] += len(wm)
			}
			t.Logf("chunk %d,%d chest minecarts kremenmc %v ref %v", c.cx, c.cz, gm, wm)
		}
		tot += len(ex)
		if len(diff) == 0 {
			totSame++
		}
		t.Logf("chunk %d,%d starts %v mesa=%v pieces=%d diffs %v\n  %s", c.cx, c.cz, c.starts, c.mesa, len(c.boxes), diff, strings.Join(ex, "\n  "))
	}
	t.Logf("chunks without diffs: %d/%d; chest minecarts in identical chunks %d/%d", totSame, n, msCarts[1], msCarts[0])
}

// TestMineshaftList (KREMENMC_MSLIST=world) lists the full reference chunks
// touching mineshafts with their starts.
func TestMineshaftList(t *testing.T) {
	w := os.Getenv("KREMENMC_MSLIST")
	if w == "" {
		t.Skip("KREMENMC_MSLIST not set")
	}
	for _, c := range msRefChunks(t, w) {
		t.Logf("%d,%d mesa=%v pieces=%d starts=%v", c.cx, c.cz, c.mesa, len(c.boxes), c.starts)
	}
}

func listOf(v any) []any {
	if l, ok := v.(nbt.List); ok {
		return l.Items
	}
	return nil
}
