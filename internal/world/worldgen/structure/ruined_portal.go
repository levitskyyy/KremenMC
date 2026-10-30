package structure

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/surface"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Ruined portal starts (minecraft:ruined_portal*), inferred from the
// black-box probe tools/refprobe/java/kremenmc/probe/structlegacy/Starts.java
// (findGenerationPoint of the real structure on the real noise generator
// with a draw-logging start random; testdata/ruined_portal). Placement is in
// ruined_portal_place.go.
//
// Draw order: [setup nextFloat when several setups] giant nextFloat (<0.05:
// giant_portal_1..3 by nextInt(3), else portal_1..10 by nextInt(10)),
// rotation nextInt(4), mirror nextFloat (<0.5 NONE, else FRONT_BACK), then
// the vertical placement's draw. Template origin is the chunk's min corner,
// pivot (sizeX/2, sizeZ/2).
//
// Height: h = base height at the box centre (min + (size)/2) for the
// placement's heightmap; a start y is chosen, then the y goes down until at
// least 3 of the 4 box-corner noise columns are solid at y (WORLD_SURFACE:
// non-air; OCEAN_FLOOR: non-air and no fluid).
type rpSetup struct {
	Placement     string  `json:"placement"`
	AirPocketProb float32 `json:"air_pocket_probability"`
	Mossiness     float32 `json:"mossiness"`
	Overgrown     bool    `json:"overgrown"`
	Vines         bool    `json:"vines"`
	CanBeCold     bool    `json:"can_be_cold"`
	Blackstone    bool    `json:"replace_with_blackstone"`
	Weight        float32 `json:"weight"`
}

type ruinedPortalType struct {
	setups []rpSetup
}

func newRuinedPortalType(raw json.RawMessage) (*ruinedPortalType, error) {
	var j struct {
		Setups []rpSetup `json:"setups"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	if len(j.Setups) == 0 {
		return nil, fmt.Errorf("ruined portal without setups")
	}
	return &ruinedPortalType{setups: j.Setups}, nil
}

func (t *ruinedPortalType) Step() int { return 4 }

// rpStart is a generated ruined portal start before placement.
type rpStart struct {
	Template           string
	Rot                Rotation
	Mirror             Mirror
	OX, OY, OZ         int32
	Box                Box
	Placement          string
	AirPocket          bool
	Mossiness          float32
	Overgrown, Vines   bool
	Blackstone, CanBeC bool
	Cold               bool
}

// rpColumn is the noise-only block at x,y,z (start time).
type rpColumn func(x, y, z int32) data.StateID

func (t *ruinedPortalType) start(r *rng.WorldgenRandom, cx, cz int32, height func(x, z int32, hm string) int32, col rpColumn, biome func(x, y, z int32) data.BiomeID, minY int32) (*rpStart, error) {
	su := t.setups[0]
	if len(t.setups) > 1 {
		var total float32
		for _, s := range t.setups {
			total += s.Weight
		}
		f := r.NextFloat() * total
		for _, s := range t.setups {
			f -= s.Weight
			if f < 0 {
				su = s
				break
			}
		}
	}
	st := &rpStart{Placement: su.Placement, Mossiness: su.Mossiness, Overgrown: su.Overgrown, Vines: su.Vines,
		Blackstone: su.Blackstone, CanBeC: su.CanBeCold}
	switch {
	case su.AirPocketProb >= 1:
		st.AirPocket = true
	case su.AirPocketProb > 0:
		st.AirPocket = r.NextFloat() < su.AirPocketProb // no vanilla setup uses this (unverified)
	}
	if r.NextFloat() < 0.05 {
		st.Template = fmt.Sprintf("minecraft:ruined_portal/giant_portal_%d", r.NextIntn(3)+1)
	} else {
		st.Template = fmt.Sprintf("minecraft:ruined_portal/portal_%d", r.NextIntn(10)+1)
	}
	tm, err := LoadTemplate(st.Template)
	if err != nil {
		return nil, err
	}
	st.Rot = Rotation(r.NextIntn(4))
	if r.NextFloat() >= 0.5 {
		st.Mirror = MirrorFrontBack
	}
	st.OX, st.OZ = cx<<4, cz<<4
	b := TemplateBox(tm, st.OX, 0, st.OZ, st.Mirror, st.Rot, tm.SizeX/2, tm.SizeZ/2)
	hm := "WORLD_SURFACE_WG"
	if su.Placement == "on_ocean_floor" {
		hm = "OCEAN_FLOOR_WG"
	}
	ch := height(b.MinX+(b.MaxX-b.MinX+1)/2, b.MinZ+(b.MaxZ-b.MinZ+1)/2, hm)
	size := tm.SizeY
	var y0 int32
	switch su.Placement {
	case "on_land_surface", "on_ocean_floor":
		y0 = ch - 1
	case "partly_buried":
		y0 = ch - size + 1 + r.NextIntn(7)
	case "underground":
		y0 = r.NextIntBetweenInclusive(minY+15, ch-1-size)
	case "in_mountain":
		y0 = ch - 1 - size
		if y0 >= 70 {
			y0 = r.NextIntBetweenInclusive(70, y0)
		}
	case "in_nether":
		// Draws after the base height: nextFloat < 0.5 → 27 + nextInt(3),
		// else 29 + nextInt(72) (NTStarts draw logs, 183 reference starts),
		// then the corner scan below.
		if r.NextFloat() < 0.5 {
			y0 = 27 + r.NextIntn(3)
		} else {
			y0 = 29 + r.NextIntn(72)
		}
	default:
		return nil, fmt.Errorf("ruined portal placement %s not implemented", su.Placement)
	}
	reg := data.Canonical()
	corners := [4][2]int32{{b.MinX, b.MinZ}, {b.MaxX, b.MinZ}, {b.MinX, b.MaxZ}, {b.MaxX, b.MaxZ}}
	y := y0
	for ; y > minY; y-- {
		n := 0
		for _, c := range corners {
			s := col(c[0], y, c[1])
			if !reg.IsAir(s) && (hm == "WORLD_SURFACE_WG" || !reg.HasFluid(s)) {
				n++
			}
		}
		if n >= 3 {
			break
		}
	}
	st.OY = y
	if su.CanBeCold {
		// Biome at the final origin, height-adjusted temperature (probe:
		// cave biomes below snowy ones and some frozen_ocean spots are not cold).
		pk, err := wgdata.Load()
		if err != nil {
			return nil, err
		}
		st.Cold, err = surface.ColdEnoughToSnow(pk, reg.Biomes[biome(st.OX, y, st.OZ)].Name, st.OX, y, st.OZ, 63)
		if err != nil {
			return nil, err
		}
	}
	st.Box = TemplateBox(tm, st.OX, y, st.OZ, st.Mirror, st.Rot, tm.SizeX/2, tm.SizeZ/2)
	return st, nil
}

// RuinedPortalStartForTest generates the start of ruined portal structure
// key at chunk cx,cz and returns its piece fields in probe order.
func RuinedPortalStartForTest(key string, seed int64, cx, cz int32, height func(x, z int32, hm string) int32, col func(x, y, z int32) data.StateID, biome func(x, y, z int32) data.BiomeID) (string, error) {
	pk, err := wgdata.Load()
	if err != nil {
		return "", err
	}
	raw, ok := pk.JSON("worldgen/structure", key)
	if !ok {
		return "", fmt.Errorf("no structure %s", key)
	}
	t, err := newRuinedPortalType(raw)
	if err != nil {
		return "", err
	}
	r := rng.NewWorldgenRandom(rng.NewLegacy(0))
	r.SetLargeFeatureSeed(seed, cx, cz)
	st, err := t.start(r, cx, cz, height, col, biome, -64)
	if err != nil {
		return "", err
	}
	b2i := map[bool]int{false: 0, true: 1}
	return fmt.Sprintf("BB:[I;%d,%d,%d,%d,%d,%d],GD:0,Mirror:%q,O:2,Properties:{air_pocket:%db,cold:%db,mossiness:%sf,overgrown:%db,replace_with_blackstone:%db,vines:%db},Rotation:%q,TPX:%d,TPY:%d,TPZ:%d,Template:%q,VerticalPlacement:%q",
		st.Box.MinX, st.Box.MinY, st.Box.MinZ, st.Box.MaxX, st.Box.MaxY, st.Box.MaxZ, mirrorNames[st.Mirror],
		b2i[st.AirPocket], b2i[st.Cold], javaFloat(st.Mossiness), b2i[st.Overgrown], b2i[st.Blackstone], b2i[st.Vines],
		rotNames[st.Rot], st.OX, st.OY, st.OZ, st.Template, st.Placement), nil
}

func javaFloat(f float32) string {
	s := strconv.FormatFloat(float64(f), 'f', -1, 32)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}
