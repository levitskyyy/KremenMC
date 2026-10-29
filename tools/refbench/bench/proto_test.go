package bench

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/reftest"
	"github.com/levitskyyy/KremenMC/internal/world"
	"github.com/levitskyyy/KremenMC/internal/world/anvil"
	"github.com/levitskyyy/KremenMC/tools/refbench/compare"
	"github.com/levitskyyy/KremenMC/tools/refbench/oracle"
)

// keptKeys must survive DecodeProto/EncodeProto exactly (NBT-equal).
var keptKeys = []string{"DataVersion", "xPos", "zPos", "yPos", "Status", "LastUpdate", "InhabitedTime", "isLightOn",
	"structures", "PostProcessing", "block_ticks", "fluid_ticks", "entities", "block_entities"}

type protoStats struct {
	chunks   map[string]int
	hmDiff   map[string]int
	hmTotal  int
	failures int
	source   string
}

func roundTrip(t *testing.T, root nbt.Compound, dim world.Dimension, st *protoStats) {
	t.Helper()
	p, stats, err := anvil.DecodeProto(root, dim)
	if err != nil {
		t.Fatal(err)
	}
	if stats.UnknownBlocks+stats.UnknownBiomes > 0 {
		t.Errorf("chunk %v: %+v", root["xPos"], stats)
	}
	last, _ := root["LastUpdate"].(int64)
	out := anvil.EncodeProto(p, last)
	status, _ := root["Status"].(string)
	st.chunks[status]++
	where := fmt.Sprintf("chunk %v,%v (%s)", root["xPos"], root["zPos"], status)
	fail := func(format string, args ...any) {
		st.failures++
		if st.failures <= 20 {
			t.Errorf(where+": "+format, args...)
		}
	}
	for _, k := range keptKeys {
		if a, b := compare.Canon(root[k]), compare.Canon(out[k]); a != b {
			fail("%s differs:\n ref %.300s\n got %.300s", k, a, b)
		}
	}
	var ka, kb []string
	for k := range root {
		ka = append(ka, k)
	}
	for k := range out {
		kb = append(kb, k)
	}
	slices.Sort(ka)
	slices.Sort(kb)
	if !slices.Equal(ka, kb) {
		fail("root keys %v, got %v", ka, kb)
	}
	ra, err := anvil.DecodeRaw(root)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := anvil.DecodeRaw(out)
	if err != nil {
		t.Fatal(err)
	}
	r := compare.Compare(compare.Set{{}: compare.FromRaw(ra)}, compare.Set{{}: compare.FromRaw(rb)}, nil, compare.Options{Blocks: true, Biomes: true})
	if !r.Equal() {
		fail("sections differ: %v", r.First[:min(3, len(r.First))])
	}
	light := func(c nbt.Compound) map[string]string {
		m := map[string]string{}
		for _, it := range compare.Items(c["sections"]) {
			s := it.(nbt.Compound)
			for _, k := range []string{"SkyLight", "BlockLight"} {
				if v, ok := s[k]; ok {
					m[fmt.Sprint(s["Y"], k)] = compare.Canon(v)
				}
			}
		}
		return m
	}
	if status == "minecraft:full" {
		// kremenmc writes sky arrays vanilla leaves implicit; compare decoded light.
		a, _, _ := anvil.DecodeChunk(root, dim)
		b, _, _ := anvil.DecodeChunk(out, dim)
		for i := range a.SkyLight {
			if !nibEq(a.SkyLight[i], b.SkyLight[i]) || !nibEq(a.BlockLight[i], b.BlockLight[i]) {
				fail("light section %d differs", i)
				break
			}
		}
	} else if la, lb := light(root), light(out); fmt.Sprint(la) != fmt.Sprint(lb) {
		fail("light differs (%d vs %d arrays)", len(la), len(lb))
	}
	for name, hs := range ra.Heightmaps {
		hb, ok := rb.Heightmaps[name]
		if !ok {
			fail("heightmap %s missing", name)
			continue
		}
		st.hmTotal++
		if !slices.Equal(hs, hb) {
			st.hmDiff[st.source+" "+status+" "+name]++
		}
	}
	for name := range rb.Heightmaps {
		if _, ok := ra.Heightmaps[name]; !ok {
			fail("extra heightmap %s", name)
		}
	}
}

// TestProtoRoundTripReference decodes and re-encodes vanilla chunks of every
// status (reference worlds and stage-oracle dumps): the persisted generation
// state must survive NBT-equal, blocks/biomes/light exactly.
func TestProtoRoundTripReference(t *testing.T) {
	ref, err := reftest.Open()
	if err != nil {
		t.Skip(err)
	}
	st := &protoStats{chunks: map[string]int{}, hmDiff: map[string]int{}}
	worlds, _ := filepath.Glob(filepath.Join(ref.Dir, "worlds", "*-overworld", "world", "dimensions", "minecraft", "overworld", "region"))
	for _, dir := range worlds {
		cs, err := anvil.ListChunks(dir)
		if err != nil {
			t.Fatal(err)
		}
		perStatus := map[string]int{}
		st.source = "world"
		for _, p := range cs {
			root, err := anvil.ReadRawNBT(dir, p[0], p[1])
			if err != nil {
				t.Fatal(err)
			}
			s, _ := root["Status"].(string)
			if perStatus[s]++; perStatus[s] > 150 {
				continue
			}
			roundTrip(t, root, world.Overworld, st)
		}
	}
	dumps, _ := filepath.Glob(filepath.Join(ref.Dir, "stages", "*-overworld", "*.dump"))
	st.source = "oracle"
	for _, d := range dumps {
		rcs, err := oracle.ReadDump(d)
		if err != nil {
			t.Fatal(err)
		}
		for _, rc := range rcs {
			root := rc.Root
			for k := range root {
				if len(k) > 5 && k[:5] == "kremenmc_" {
					delete(root, k)
				}
			}
			roundTrip(t, root, world.Overworld, st)
		}
	}
	if len(st.chunks) == 0 {
		t.Skip("no reference chunks; run refbench gen/stages")
	}
	t.Logf("round-tripped chunks by status: %v", st.chunks)
	if len(st.hmDiff) > 0 {
		t.Logf("heightmaps recomputed from blocks that differ from vanilla's saved ones (of %d): %v", st.hmTotal, st.hmDiff)
	}
}

func nibEq(a, b world.Nibbles) bool {
	for i := range 4096 {
		if a.Get(i) != b.Get(i) {
			return false
		}
	}
	return true
}
