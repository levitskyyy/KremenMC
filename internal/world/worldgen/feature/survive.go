package feature

import (
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// canSurvive is BlockState.canSurvive for the blocks features place. Blocks
// without a rule here survive anywhere.
func canSurvive(lv *Level, st data.StateID, p Pos) bool {
	rule, ok := survival[blockName(st)]
	if !ok {
		return true
	}
	return rule(lv, st, p)
}

func blockName(st data.StateID) string {
	b := data.Canonical().BlockOf(st)
	if b == nil {
		return ""
	}
	return strings.TrimPrefix(b.Name, "minecraft:")
}

type survivalRule func(lv *Level, st data.StateID, p Pos) bool

var survival = map[string]survivalRule{}

// onTag survives on a block of the tag directly below.
func onTag(tag string) survivalRule {
	set := sync.OnceValue(func() blockSet {
		rg, err := theRegistry()
		if err != nil {
			return nil
		}
		s, _ := rg.tag(tag)
		return s
	})
	return func(lv *Level, _ data.StateID, p Pos) bool {
		return set().has(lv.Block(p.Add(Pos{0, -1, 0})))
	}
}

// doublePlant: the lower half needs support, the upper half its lower half.
func doublePlant(lower survivalRule) survivalRule {
	return func(lv *Level, st data.StateID, p Pos) bool {
		if h, _ := data.Canonical().Value(st, "half"); h == "upper" {
			below := lv.Block(p.Add(Pos{0, -1, 0}))
			return data.Canonical().BlockOf(below) == data.Canonical().BlockOf(st)
		}
		return lower(lv, st, p)
	}
}

// Support rules measured with tools/refprobe kremenmc.probe.feature.Survive
// (BlockState.canSurvive over every block below, in open air and next to
// water), matched to the 26.3 support tags.
func init() {
	veg := onTag("minecraft:supports_vegetation")
	for _, n := range strings.Fields(`acacia_sapling birch_sapling cherry_sapling dark_oak_sapling jungle_sapling
		oak_sapling pale_oak_sapling poplar_sapling spruce_sapling short_grass fern bush firefly_bush pink_petals
		red_shrub sweet_berry_bush wildflowers closed_eyeblossom open_eyeblossom dandelion poppy blue_orchid allium
		azure_bluet red_tulip orange_tulip white_tulip pink_tulip oxeye_daisy cornflower lily_of_the_valley torchflower`) {
		survival[n] = veg
	}
	for _, n := range strings.Fields("tall_grass large_fern lilac rose_bush peony sunflower") {
		survival[n] = doublePlant(veg)
	}
	dry := onTag("minecraft:supports_dry_vegetation")
	survival["dead_bush"], survival["short_dry_grass"], survival["tall_dry_grass"] = dry, dry, doublePlant(dry)
	az := onTag("minecraft:supports_azalea")
	survival["azalea"], survival["flowering_azalea"], survival["mangrove_propagule"] = az, az, az
	nether := onTag("minecraft:supports_crimson_fungus")
	for _, n := range strings.Fields("crimson_fungus crimson_roots nether_sprouts warped_fungus warped_roots") {
		survival[n] = nether
	}
	survival["small_dripleaf"] = doublePlant(onTag("minecraft:supports_small_dripleaf"))
	mush := mushroomSurvival(onTag("minecraft:overrides_mushroom_light_requirement"))
	survival["brown_mushroom"], survival["red_mushroom"] = mush, mush
	cane := onTag("minecraft:supports_sugar_cane")
	survival["sugar_cane"] = func(lv *Level, st data.StateID, p Pos) bool {
		below := p.Add(Pos{0, -1, 0})
		bs := lv.Block(below)
		if blockName(bs) == "sugar_cane" {
			return true
		}
		if !cane(lv, st, p) {
			return false
		}
		for _, d := range []Pos{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}} {
			n := lv.Block(below.Add(d))
			if f := fluidOf(n); f == "water" || f == "flowing_water" || blockName(n) == "frosted_ice" {
				return true
			}
		}
		return false
	}
	sand := onTag("minecraft:supports_cactus")
	survival["cactus"] = func(lv *Level, st data.StateID, p Pos) bool {
		for _, d := range []Pos{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}} {
			n := lv.Block(p.Add(d))
			if isSolid(n) || fluidOf(n) == "lava" || fluidOf(n) == "flowing_lava" {
				return false
			}
		}
		return blockName(lv.Block(p.Add(Pos{0, -1, 0}))) == "cactus" || sand(lv, st, p)
	}
	pad := onTag("minecraft:supports_lily_pad")
	survival["lily_pad"] = func(lv *Level, st data.StateID, p Pos) bool {
		return fluidOf(lv.Block(p.Add(Pos{0, -1, 0}))) == "water" || pad(lv, st, p)
	}
}
